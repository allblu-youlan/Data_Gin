package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const (
	messageDetailsTimeout = 10 * time.Second
	// A batch includes message bodies even though we only use its metadata.
	maxMessageDetailsResponseBytes = 8 * 1024 * 1024
)

type MessageDetail struct {
	SenderName  string
	OriginalURL string
}

type messageDetailResponseItem struct {
	MessageID             string          `json:"message_id"`
	ChatID                string          `json:"chat_id"`
	ThreadID              string          `json:"thread_id"`
	MessageAppLink        string          `json:"message_app_link"`
	MessagePosition       json.RawMessage `json:"message_position"`
	ThreadMessagePosition json.RawMessage `json:"thread_message_position"`
	Sender                struct {
		SenderName string `json:"sender_name"`
	} `json:"sender"`
}

// MessageDetails looks up at most fifty messages and returns display metadata only.
// Query and link rules follow the official CLI's mget implementation:
// https://github.com/larksuite/cli/tree/017b19a793ad71794a02887ede04757f3d82e10d/shortcuts/im
func (client *BotClient) MessageDetails(ctx context.Context, messageIDs []string) (map[string]MessageDetail, error) {
	if client == nil || client.http == nil || client.tokens == nil || client.baseURL == nil || ctx == nil || len(messageIDs) < 1 || len(messageIDs) > 50 {
		return nil, fmt.Errorf("feishu bot: invalid message detail request")
	}
	requested := make(map[string]bool, len(messageIDs))
	for _, id := range messageIDs {
		if !strings.HasPrefix(id, "om_") || len(id) <= 3 || !cardMessageIDPattern.MatchString(id) {
			return nil, fmt.Errorf("feishu bot: invalid message ID")
		}
		requested[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, messageDetailsTimeout)
	defer cancel()
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		details, status, requestErr := client.messageDetailsOnce(ctx, token, messageIDs, requested)
		if attempt == 0 && shouldRefreshBotToken(status, requestErr) {
			token, err = client.tokens.Refresh(ctx)
			if err != nil {
				return nil, err
			}
			continue
		}
		return details, requestErr
	}
	return nil, &BotError{Class: providerhttp.ErrorClassAuth}
}

func (client *BotClient) messageDetailsOnce(ctx context.Context, token string, ids []string, requested map[string]bool) (map[string]MessageDetail, int, error) {
	endpoint := *client.baseURL
	endpoint.Path += "/open-apis/im/v1/messages/mget"
	query := endpoint.Query()
	query.Set("card_msg_content_type", "raw_card_content")
	query.Set("with_sender_name", "true")
	for _, id := range ids {
		query.Add("message_ids", id)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, &BotError{Class: providerhttp.ErrorClassRequest}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	httpClient := *client.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httpClient.Do(request)
	if err != nil {
		classification := providerhttp.ClassifyRetry(0, err)
		return nil, 0, &BotError{Class: classification.Class, Retryable: classification.Retryable, cause: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMessageDetailsResponseBytes+1))
	if err != nil || len(body) > maxMessageDetailsResponseBytes {
		return nil, response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, Retryable: true, HTTPCode: response.StatusCode}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(response.StatusCode, nil)
		if classification.Class == providerhttp.ErrorClassNone {
			classification.Class = providerhttp.ErrorClassResponse
		}
		return nil, response.StatusCode, &BotError{Class: classification.Class, Retryable: classification.Retryable, HTTPCode: response.StatusCode, RetryAfter: botRetryAfter(response.Header, time.Now())}
	}
	var decoded struct {
		Code *int `json:"code"`
		Data *struct {
			Items []messageDetailResponseItem `json:"items"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&decoded); err != nil {
		return nil, response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || decoded.Code == nil {
		return nil, response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode}
	}
	if *decoded.Code != 0 {
		classification := classifyBotBusinessCode(*decoded.Code)
		return nil, response.StatusCode, &BotError{Class: classification.Class, Retryable: classification.Retryable, HTTPCode: response.StatusCode, Code: *decoded.Code, RetryAfter: botRetryAfter(response.Header, time.Now())}
	}
	if decoded.Data == nil {
		return nil, response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, HTTPCode: response.StatusCode}
	}
	details := make(map[string]MessageDetail, len(decoded.Data.Items))
	for _, item := range decoded.Data.Items {
		if requested[item.MessageID] {
			details[item.MessageID] = MessageDetail{SenderName: strings.TrimSpace(item.Sender.SenderName), OriginalURL: messageDetailOriginalURL(item)}
		}
	}
	return details, response.StatusCode, nil
}

func messageDetailOriginalURL(item messageDetailResponseItem) string {
	if link := strings.TrimSpace(item.MessageAppLink); link != "" {
		parsed, err := url.Parse(link)
		if err == nil && parsed.Scheme == "https" && parsed.Host == "applink.feishu.cn" && parsed.User == nil && parsed.Fragment == "" {
			return parsed.String()
		}
	}
	if item.ChatID == "" {
		return ""
	}
	link := url.URL{Scheme: "https", Host: "applink.feishu.cn"}
	query := url.Values{}
	if position := messageDetailPosition(item.ThreadMessagePosition); item.ThreadID != "" && position != "" {
		link.Path = "/client/thread/open"
		query.Set("openthreadid", item.ThreadID)
		query.Set("openchatid", item.ChatID)
		query.Set("open_thread_id", item.ThreadID)
		query.Set("open_chat_id", item.ChatID)
		query.Set("thread_position", position)
	} else if position := messageDetailPosition(item.MessagePosition); position != "" {
		link.Path = "/client/chat/open"
		query.Set("openChatId", item.ChatID)
		query.Set("position", position)
	} else {
		return ""
	}
	link.RawQuery = query.Encode()
	return link.String()
}

func messageDetailPosition(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}
