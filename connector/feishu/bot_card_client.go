package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

// Keep a margin below Feishu's 30 KB interactive-message limit, measured on
// the actual outer request (including its escaped content JSON string).
const MaxCardRequestBytes = 28 * 1024

var cardMessageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func CardRequestSize(receiveID, cardJSON, requestUUID string) int {
	payload, _ := json.Marshal(map[string]string{"receive_id": receiveID, "msg_type": "interactive", "content": cardJSON, "uuid": requestUUID})
	return len(payload)
}

func (client *BotClient) SendCard(ctx context.Context, receiveID, cardJSON, requestUUID string) (string, error) {
	if !json.Valid([]byte(cardJSON)) || !strings.HasPrefix(strings.TrimSpace(cardJSON), "{") || len(requestUUID) > 50 || CardRequestSize(receiveID, cardJSON, requestUUID) > MaxCardRequestBytes {
		return "", fmt.Errorf("feishu bot: invalid or oversized card")
	}
	return client.send(ctx, "open_id", receiveID, "interactive", cardJSON, requestUUID)
}

func (client *BotClient) UpdateCard(ctx context.Context, messageID, cardJSON string) error {
	if client == nil || client.http == nil || client.tokens == nil || client.baseURL == nil || ctx == nil || !cardMessageIDPattern.MatchString(messageID) || !json.Valid([]byte(cardJSON)) || !strings.HasPrefix(strings.TrimSpace(cardJSON), "{") {
		return fmt.Errorf("feishu bot: invalid card update")
	}
	payload, err := json.Marshal(map[string]string{"content": cardJSON})
	if err != nil || len(payload) > MaxCardRequestBytes {
		return fmt.Errorf("feishu bot: invalid or oversized card update")
	}
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		endpoint := *client.baseURL
		endpoint.Path += "/open-apis/im/v1/messages/" + messageID
		request, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint.String(), bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("feishu bot: create card update request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		_, status, requestErr := client.do(request, false)
		if attempt == 0 && shouldRefreshBotToken(status, requestErr) {
			token, err = client.tokens.Refresh(ctx)
			if err != nil {
				return err
			}
			continue
		}
		return requestErr
	}
	return &BotError{Class: providerhttp.ErrorClassAuth}
}

func shouldRefreshBotToken(status int, err error) bool {
	var botErr *BotError
	return status == http.StatusUnauthorized || errors.As(err, &botErr) && (botErr.Code == 99991663 || botErr.Code == 99991664 || botErr.Code == 99991668)
}

func classifyBotBusinessCode(code int) providerhttp.RetryClassification {
	switch code {
	case 99991400, 230020:
		return providerhttp.RetryClassification{Class: providerhttp.ErrorClassRateLimited, Retryable: true}
	case 99991401, 99991500, 99991501, 230002:
		return providerhttp.RetryClassification{Class: providerhttp.ErrorClassProvider, Retryable: true}
	case 99991663, 99991664, 99991668, 99991672:
		return providerhttp.RetryClassification{Class: providerhttp.ErrorClassAuth}
	default:
		return providerhttp.RetryClassification{Class: providerhttp.ErrorClassRequest}
	}
}

func botRetryAfter(header http.Header, now time.Time) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}
