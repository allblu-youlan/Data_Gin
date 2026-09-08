package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"gin-biz-web-api/pkg/providerhttp"
)

const defaultUploadTimeout = 4 * time.Minute

type Config struct {
	BaseURL   string
	Username  string
	Password  string
	Directory string
}

type Client struct {
	httpClient *http.Client
}

type UploadError struct {
	StatusCode int
	retryable  bool
	cause      error
}

func (err *UploadError) Error() string {
	if err.StatusCode == 0 {
		return "webdav upload: request failed"
	}
	return fmt.Sprintf("webdav upload: unexpected HTTP status %d", err.StatusCode)
}

func (err *UploadError) Unwrap() error { return err.cause }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		var err error
		httpClient, err = providerhttp.NewClient(providerhttp.ClientConfig{Timeout: defaultUploadTimeout})
		if err != nil {
			panic("webdav upload: invalid default HTTP client configuration")
		}
	}
	return &Client{httpClient: httpClient}
}

func (client *Client) UploadFile(ctx context.Context, config Config, localPath, fileName string) error {
	if client == nil || client.httpClient == nil || ctx == nil {
		return errors.New("webdav upload: client is unavailable")
	}
	destination, err := destinationURL(config.BaseURL, config.Directory, fileName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(config.Username) == "" || config.Password == "" {
		return errors.New("webdav upload: credentials are required")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("webdav upload: open file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("webdav upload: stat file: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, destination, file)
	if err != nil {
		return fmt.Errorf("webdav upload: create request: %w", err)
	}
	request.ContentLength = info.Size()
	request.Header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	request.SetBasicAuth(config.Username, config.Password)
	response, err := client.httpClient.Do(request)
	if err != nil {
		classification := providerhttp.ClassifyRetry(0, err)
		return &UploadError{retryable: classification.Retryable, cause: err}
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10)); err != nil {
		return fmt.Errorf("webdav upload: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(response.StatusCode, nil)
		return &UploadError{StatusCode: response.StatusCode, retryable: classification.Retryable}
	}
	return nil
}

func Retryable(err error) bool {
	var uploadError *UploadError
	return !errors.As(err, &uploadError) || uploadError.retryable
}

func destinationURL(baseURL, directory, fileName string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("webdav upload: invalid base URL")
	}
	directory, err = NormalizeDirectory(directory)
	if err != nil {
		return "", err
	}
	if fileName == "" || strings.TrimSpace(fileName) != fileName || strings.ContainsAny(fileName, "/\\") || strings.IndexFunc(fileName, unicode.IsControl) >= 0 {
		return "", errors.New("webdav upload: invalid file name")
	}
	parsed.Path = path.Join(parsed.Path, strings.TrimPrefix(directory, "/"), fileName)
	parsed.RawPath = ""
	return parsed.String(), nil
}

func NormalizeDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/", nil
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("webdav upload: invalid directory")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("webdav upload: invalid directory")
		}
	}
	normalized := path.Clean(value)
	if normalized == "." {
		normalized = "/"
	}
	return normalized, nil
}
