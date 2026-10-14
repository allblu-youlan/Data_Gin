package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const userNamesTimeout = 10 * time.Second

// UserNames resolves at most ten open IDs using contact:user.basic_profile:readonly.
// Missing users are omitted; callers may retain their existing display fallback.
func (client *BotClient) UserNames(ctx context.Context, openIDs []string) (map[string]string, error) {
	if client == nil || client.http == nil || client.tokens == nil || client.baseURL == nil || ctx == nil || len(openIDs) < 1 || len(openIDs) > 10 {
		return nil, fmt.Errorf("feishu bot: invalid user name request")
	}
	requested := make(map[string]bool, len(openIDs))
	for _, id := range openIDs {
		if !ValidOAuthOpenID(id) {
			return nil, fmt.Errorf("feishu bot: invalid user open ID")
		}
		requested[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, userNamesTimeout)
	defer cancel()
	payload, err := json.Marshal(struct {
		UserIDs []string `json:"user_ids"`
	}{UserIDs: openIDs})
	if err != nil {
		return nil, err
	}
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, status, requestErr := client.userNamesOnce(ctx, token, payload, requested)
		if attempt == 0 && shouldRefreshBotToken(status, requestErr) {
			token, err = client.tokens.Refresh(ctx)
			if err != nil {
				return nil, err
			}
			continue
		}
		return result, requestErr
	}
	return nil, &BotError{Class: providerhttp.ErrorClassAuth}
}

func (client *BotClient) userNamesOnce(ctx context.Context, token string, payload []byte, requested map[string]bool) (map[string]string, int, error) {
	endpoint := *client.baseURL
	endpoint.Path += "/open-apis/contact/v3/users/basic_batch"
	query := endpoint.Query()
	query.Set("user_id_type", "open_id")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, &BotError{Class: providerhttp.ErrorClassRequest}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpClient := *client.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httpClient.Do(request)
	if err != nil {
		classification := providerhttp.ClassifyRetry(0, err)
		return nil, 0, &BotError{Class: classification.Class, Retryable: classification.Retryable, cause: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBotResponseBodyBytes+1))
	if err != nil || len(body) > maxBotResponseBodyBytes {
		return nil, response.StatusCode, &BotError{Class: providerhttp.ErrorClassResponse, Retryable: true, HTTPCode: response.StatusCode}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(response.StatusCode, nil)
		return nil, response.StatusCode, &BotError{Class: classification.Class, Retryable: classification.Retryable, HTTPCode: response.StatusCode, RetryAfter: botRetryAfter(response.Header, time.Now())}
	}
	var decoded struct {
		Code *int `json:"code"`
		Data *struct {
			Users []struct {
				UserID string `json:"user_id"`
				Name   string `json:"name"`
			} `json:"users"`
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
	names := make(map[string]string, len(decoded.Data.Users))
	for _, user := range decoded.Data.Users {
		// The field is user_id even when user_id_type is open_id.
		if name := strings.TrimSpace(user.Name); requested[user.UserID] && name != "" {
			names[user.UserID] = name
		}
	}
	return names, response.StatusCode, nil
}
