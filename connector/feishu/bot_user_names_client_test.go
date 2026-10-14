package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

type botUserNamesTransport func(*http.Request) (*http.Response, error)

func (f botUserNamesTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func botUserNamesResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestFeishuMentionUserNamesUsesOpenIDBatchProtocol(t *testing.T) {
	ids := []string{"ou_first", "ou_second", "ou_missing"}
	httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != "https://open.feishu.cn/open-apis/contact/v3/users/basic_batch?user_id_type=open_id" {
			t.Fatalf("unexpected user name request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer tenant-token" || request.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatal("missing user name authentication or content type")
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > userNamesTimeout {
			t.Fatal("name lookup must have a bounded deadline")
		}
		var body struct {
			UserIDs []string `json:"user_ids"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || !reflect.DeepEqual(body.UserIDs, ids) {
			t.Fatalf("user_ids=%v err=%v", body.UserIDs, err)
		}
		return botUserNamesResponse(200, `{"code":0,"data":{"users":[{"user_id":"ou_second","name":"易理志"},{"user_id":"ou_first","name":"李慧娜"},{"user_id":"ou_other","name":"无关用户"},{"user_id":"ou_missing","name":""}]}}`), nil
	})}
	client, err := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "tenant-token"}, httpClient, false)
	if err != nil {
		t.Fatal(err)
	}
	names, err := client.UserNames(t.Context(), ids)
	if err != nil || !reflect.DeepEqual(names, map[string]string{"ou_first": "李慧娜", "ou_second": "易理志"}) {
		t.Fatalf("names=%v err=%v", names, err)
	}
}

func TestFeishuMentionUserNamesRefreshesExpiredToken(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := &fakeBotTokenProvider{token: "stale", refreshToken: "fresh"}
			calls := 0
			httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return botUserNamesResponse(status, `{"code":99991663,"msg":"private provider details"}`), nil
				}
				if request.Header.Get("Authorization") != "Bearer fresh" {
					t.Fatal("refreshed token was not used")
				}
				return botUserNamesResponse(200, `{"code":0,"data":{"users":[{"user_id":"ou_person","name":"姓名"}]}}`), nil
			})}
			client, _ := newBotClient(defaultBaseURL, provider, httpClient, false)
			names, err := client.UserNames(t.Context(), []string{"ou_person"})
			if err != nil || names["ou_person"] != "姓名" || calls != 2 || provider.refreshes != 1 {
				t.Fatalf("names=%v err=%v calls=%d refreshes=%d", names, err, calls, provider.refreshes)
			}
		})
	}
}

func TestFeishuMentionUserNamesErrorsAreClassifiedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		class  providerhttp.ErrorClass
		retry  bool
	}{
		{"permission", 403, `{"msg":"private-token secret-name"}`, providerhttp.ErrorClassAuth, false},
		{"business_permission", 200, `{"code":99991672,"msg":"private-token secret-name"}`, providerhttp.ErrorClassAuth, false},
		{"rate_limit", 429, `private-token secret-name`, providerhttp.ErrorClassRateLimited, true},
		{"business_limit", 200, `{"code":99991400,"msg":"private-token secret-name"}`, providerhttp.ErrorClassRateLimited, true},
		{"server_failure", 503, `private-token secret-name`, providerhttp.ErrorClassProvider, true},
		{"invalid_json", 200, `private-token secret-name`, providerhttp.ErrorClassResponse, false},
		{"missing_data", 200, `{"code":0}`, providerhttp.ErrorClassResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: botUserNamesTransport(func(*http.Request) (*http.Response, error) {
				response := botUserNamesResponse(tc.status, tc.body)
				response.Header.Set("Retry-After", "2")
				return response, nil
			})}
			client, _ := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "private-token"}, httpClient, false)
			names, err := client.UserNames(t.Context(), []string{"ou_person"})
			var botErr *BotError
			if names != nil || !errors.As(err, &botErr) || botErr.Class != tc.class || botErr.Retryable != tc.retry {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "secret-name") {
				t.Fatal("provider details leaked")
			}
			if tc.retry && botErr.RetryDelay() != 2*time.Second {
				t.Fatal("Retry-After was discarded")
			}
		})
	}
}

func TestFeishuMentionUserNamesHonorsCancellationAndInputLimit(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	client, _ := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "private-token"}, httpClient, false)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := client.UserNames(ctx, []string{"ou_person"})
	var botErr *BotError
	if !errors.As(err, &botErr) || botErr.Class != providerhttp.ErrorClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
	for _, ids := range [][]string{nil, make([]string, 11), {"invalid-id"}} {
		if _, err := client.UserNames(t.Context(), ids); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("invalid input made %d requests", calls-1)
	}
}
