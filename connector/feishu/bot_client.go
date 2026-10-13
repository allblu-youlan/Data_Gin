package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gin-biz-web-api/pkg/providerhttp"
)

const (
	maxBotResponseBodyBytes = 1024 * 1024
	maxBotFileBytes         = 30 * 1024 * 1024
)

var (
	botReceiveIDPattern = regexp.MustCompile(`^[^\s\x00-\x1f]{1,255}$`)
	botUUIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type botTokenProvider interface {
	Token(context.Context) (string, error)
	Refresh(context.Context) (string, error)
}

type BotError struct {
	Class      providerhttp.ErrorClass
	Retryable  bool
	HTTPCode   int
	Code       int
	RetryAfter time.Duration
	cause      error
}

func (err *BotError) RetryDelay() time.Duration {
	if err == nil {
		return 0
	}
	return err.RetryAfter
}

func (err *BotError) Error() string {
	if err == nil {
		return "feishu bot: unknown error"
	}
	return fmt.Sprintf("feishu bot: request failed (%s)", err.Class)
}

func (err *BotError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

type BotClient struct {
	baseURL *url.URL
	tokens  botTokenProvider
	http    *http.Client
}

func NewBotClient(tokens *TenantTokenProvider, httpClient *http.Client) (*BotClient, error) {
	return newBotClient(defaultBaseURL, tokens, httpClient, false)
}

func newBotClient(baseURL string, tokens botTokenProvider, httpClient *http.Client, allowLoopbackHTTP bool) (*BotClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && !(allowLoopbackHTTP && parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()))) || tokens == nil {
		return nil, fmt.Errorf("feishu bot: invalid client configuration")
	}
	if httpClient == nil {
		httpClient, err = providerhttp.NewClient(providerhttp.ClientConfig{})
		if err != nil {
			return nil, fmt.Errorf("feishu bot: create HTTP client: %w", err)
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &BotClient{baseURL: parsed, tokens: tokens, http: httpClient}, nil
}

func (client *BotClient) UploadFile(ctx context.Context, path, fileName string) (string, error) {
	if client == nil || client.baseURL == nil || client.tokens == nil || client.http == nil || ctx == nil {
		return "", fmt.Errorf("feishu bot: invalid file upload request")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBotFileBytes {
		return "", fmt.Errorf("feishu bot: invalid upload file")
	}
	fileName = strings.TrimSpace(fileName)
	if !validBotUploadFileName(fileName) {
		return "", fmt.Errorf("feishu bot: invalid upload file name")
	}
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 2; attempt++ {
		fileKey, status, requestErr := client.uploadFileOnce(ctx, token, path, fileName)
		if shouldRefreshBotToken(status, requestErr) && attempt == 0 {
			token, err = client.tokens.Refresh(ctx)
			if err != nil {
				return "", err
			}
			continue
		}
		return fileKey, requestErr
	}
	return "", &BotError{Class: providerhttp.ErrorClassAuth, HTTPCode: http.StatusUnauthorized}
}

func validBotUploadFileName(fileName string) bool {
	return fileName != "" && fileName != "." && filepath.Base(fileName) == fileName &&
		utf8.ValidString(fileName) && len(fileName) <= 255 && !strings.ContainsAny(fileName, "/\\") &&
		strings.IndexFunc(fileName, unicode.IsControl) < 0 && strings.HasSuffix(strings.ToLower(fileName), ".xlsx")
}

func (client *BotClient) SendText(ctx context.Context, receiveIDType, receiveID, text, requestUUID string) (string, error) {
	if strings.TrimSpace(text) == "" || len(text) > 150_000 {
		return "", fmt.Errorf("feishu bot: invalid text message")
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("feishu bot: encode text content: %w", err)
	}
	return client.send(ctx, receiveIDType, receiveID, "text", string(content), requestUUID)
}

func (client *BotClient) SendFile(ctx context.Context, receiveIDType, receiveID, fileKey, requestUUID string) (string, error) {
	if !botReceiveIDPattern.MatchString(fileKey) {
		return "", fmt.Errorf("feishu bot: invalid file key")
	}
	content, err := json.Marshal(map[string]string{"file_key": fileKey})
	if err != nil {
		return "", fmt.Errorf("feishu bot: encode file content: %w", err)
	}
	return client.send(ctx, receiveIDType, receiveID, "file", string(content), requestUUID)
}

func (client *BotClient) send(ctx context.Context, receiveIDType, receiveID, messageType, content, requestUUID string) (string, error) {
	if client == nil || client.baseURL == nil || client.tokens == nil || client.http == nil || ctx == nil ||
		!validReceiveIDType(receiveIDType) || !botReceiveIDPattern.MatchString(receiveID) || !botUUIDPattern.MatchString(requestUUID) {
		return "", fmt.Errorf("feishu bot: invalid message request")
	}
	payload, err := json.Marshal(struct {
		ReceiveID string `json:"receive_id"`
		Message   string `json:"msg_type"`
		Content   string `json:"content"`
		UUID      string `json:"uuid"`
	}{ReceiveID: receiveID, Message: messageType, Content: content, UUID: requestUUID})
	if err != nil {
		return "", fmt.Errorf("feishu bot: encode message request: %w", err)
	}
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 2; attempt++ {
		messageID, status, requestErr := client.sendOnce(ctx, token, receiveIDType, payload)
		if shouldRefreshBotToken(status, requestErr) && attempt == 0 {
			token, err = client.tokens.Refresh(ctx)
			if err != nil {
				return "", err
			}
			continue
		}
		return messageID, requestErr
	}
	return "", &BotError{Class: providerhttp.ErrorClassAuth, HTTPCode: http.StatusUnauthorized}
}

func (client *BotClient) uploadFileOnce(ctx context.Context, token, path, fileName string) (string, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("feishu bot: open upload file: %w", err)
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("file_type", "stream"); err != nil {
		return "", 0, fmt.Errorf("feishu bot: write file type: %w", err)
	}
	if err := writer.WriteField("file_name", fileName); err != nil {
		return "", 0, fmt.Errorf("feishu bot: write file name: %w", err)
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", 0, fmt.Errorf("feishu bot: create file part: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", 0, fmt.Errorf("feishu bot: read upload file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", 0, fmt.Errorf("feishu bot: close multipart request: %w", err)
	}
	endpoint := *client.baseURL
	endpoint.Path += "/open-apis/im/v1/files"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), &body)
	if err != nil {
		return "", 0, fmt.Errorf("feishu bot: create file request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return client.do(request, true)
}

func (client *BotClient) sendOnce(ctx context.Context, token, receiveIDType string, payload []byte) (string, int, error) {
	endpoint := *client.baseURL
	endpoint.Path += "/open-apis/im/v1/messages"
	query := endpoint.Query()
	query.Set("receive_id_type", receiveIDType)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("feishu bot: create message request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	return client.do(request, false)
}

func (client *BotClient) do(request *http.Request, upload bool) (string, int, error) {
	response, err := client.http.Do(request)
	if err != nil {
		classification := providerhttp.ClassifyRetry(0, err)
		return "", 0, &BotError{Class: classification.Class, Retryable: classification.Retryable, cause: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBotResponseBodyBytes+1))
	if err != nil || len(body) > maxBotResponseBodyBytes {
		return "", response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, Retryable: true, HTTPCode: response.StatusCode, cause: err}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(response.StatusCode, nil)
		return "", response.StatusCode, &BotError{Class: classification.Class, Retryable: classification.Retryable, HTTPCode: response.StatusCode, RetryAfter: botRetryAfter(response.Header, time.Now())}
	}
	var decoded struct {
		Code *int `json:"code"`
		Data struct {
			FileKey   string `json:"file_key"`
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&decoded); err != nil {
		return "", response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode, cause: err}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode, cause: fmt.Errorf("response contains trailing data")}
	}
	if decoded.Code == nil {
		return "", response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode}
	}
	if *decoded.Code != 0 {
		classification := classifyBotBusinessCode(*decoded.Code)
		return "", response.StatusCode, &BotError{Class: classification.Class, Retryable: classification.Retryable, HTTPCode: response.StatusCode, Code: *decoded.Code, RetryAfter: botRetryAfter(response.Header, time.Now())}
	}
	if request.Method == http.MethodPatch {
		return "", response.StatusCode, nil
	}
	value := decoded.Data.MessageID
	if upload {
		value = decoded.Data.FileKey
	}
	if !botReceiveIDPattern.MatchString(value) {
		return "", response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode, cause: fmt.Errorf("response identifier is invalid")}
	}
	return value, response.StatusCode, nil
}

func validReceiveIDType(value string) bool {
	switch value {
	case "chat_id", "open_id", "user_id", "union_id", "email":
		return true
	default:
		return false
	}
}
