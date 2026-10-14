package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

func TestFeishuMentionMessageDetailsUsesOfficialMgetProtocol(t *testing.T) {
	ids := []string{"om_first", "om_second", "om_missing"}
	httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Path != "/open-apis/im/v1/messages/mget" ||
			query.Get("with_sender_name") != "true" || query.Get("card_msg_content_type") != "raw_card_content" || !reflect.DeepEqual(query["message_ids"], ids) {
			t.Fatalf("unexpected mget request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatal("missing tenant token")
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > messageDetailsTimeout {
			t.Fatal("message lookup must have a bounded deadline")
		}
		return botUserNamesResponse(200, `{"code":0,"data":{"items":[
			{"message_id":"om_second","sender":{"sender_name":"易理志","name":"不是源字段"},"message_app_link":"https://applink.feishu.cn/client/message/link/open?token=official","body":{"content":"changed remote body"}},
			{"message_id":"om_first","sender":{"sender_name":"李慧娜"},"chat_id":"oc_chat","message_position":0},
			{"message_id":"om_unrequested","sender":{"sender_name":"无关发送人"}},
			{"message_id":"om_missing","sender":{"name":"不可猜测字段"},"chat_id":"oc_chat"}
		]}}`), nil
	})}
	client, err := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "tenant-token"}, httpClient, false)
	if err != nil {
		t.Fatal(err)
	}
	details, err := client.MessageDetails(t.Context(), ids)
	want := map[string]MessageDetail{
		"om_first":   {SenderName: "李慧娜", OriginalURL: "https://applink.feishu.cn/client/chat/open?openChatId=oc_chat&position=0"},
		"om_second":  {SenderName: "易理志", OriginalURL: "https://applink.feishu.cn/client/message/link/open?token=official"},
		"om_missing": {},
	}
	if err != nil || !reflect.DeepEqual(details, want) {
		t.Fatalf("details=%v err=%v", details, err)
	}
}

func TestFeishuMentionMessageDetailsBuildsOnlySupportedLinks(t *testing.T) {
	for _, tc := range []struct {
		name, input, path string
		query             url.Values
	}{
		{"ordinary_string", `{"chat_id":"oc_chat","message_position":"17"}`, "/client/chat/open", url.Values{"openChatId": {"oc_chat"}, "position": {"17"}}},
		{"zero", `{"chat_id":"oc_chat","message_position":0}`, "/client/chat/open", url.Values{"openChatId": {"oc_chat"}, "position": {"0"}}},
		{"negative", `{"chat_id":"oc_chat","message_position":-1}`, "/client/chat/open", url.Values{"openChatId": {"oc_chat"}, "position": {"-1"}}},
		{"thread", `{"chat_id":"oc_chat","thread_id":"omt_thread","message_position":9,"thread_message_position":"0"}`, "/client/thread/open", url.Values{"openthreadid": {"omt_thread"}, "openchatid": {"oc_chat"}, "open_thread_id": {"omt_thread"}, "open_chat_id": {"oc_chat"}, "thread_position": {"0"}}},
		{"missing_position", `{"chat_id":"oc_chat","message_id":"om_cannot_be_position"}`, "", nil},
		{"missing_chat", `{"message_position":9}`, "", nil},
		{"empty_position", `{"chat_id":"oc_chat","message_position":" "}`, "", nil},
		{"null_position", `{"chat_id":"oc_chat","message_position":null}`, "", nil},
		{"invalid_position", `{"chat_id":"oc_chat","message_position":true}`, "", nil},
		{"unsafe_provider_link", `{"message_app_link":"javascript:alert(1)"}`, "", nil},
		{"foreign_provider_link", `{"message_app_link":"https://example.test/message"}`, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var item messageDetailResponseItem
			if err := json.Unmarshal([]byte(tc.input), &item); err != nil {
				t.Fatal(err)
			}
			link := messageDetailOriginalURL(item)
			if tc.path == "" {
				if link != "" {
					t.Fatalf("unexpected link: %s", link)
				}
				return
			}
			u, err := url.Parse(link)
			if err != nil || u.Scheme != "https" || u.Host != "applink.feishu.cn" || u.Path != tc.path || !reflect.DeepEqual(u.Query(), tc.query) {
				t.Fatalf("unexpected link: %s", link)
			}
		})
	}
}

func TestFeishuMentionMessageDetailsRefreshesTokenAndRedactsFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := &fakeBotTokenProvider{token: "stale", refreshToken: "fresh"}
			calls := 0
			httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return botUserNamesResponse(status, `{"code":99991663,"msg":"private-token private-message"}`), nil
				}
				if request.Header.Get("Authorization") != "Bearer fresh" {
					t.Fatal("refresh was not used")
				}
				return botUserNamesResponse(200, `{"code":0,"data":{"items":[]}}`), nil
			})}
			client, _ := newBotClient(defaultBaseURL, provider, httpClient, false)
			if _, err := client.MessageDetails(t.Context(), []string{"om_message"}); err != nil || calls != 2 || provider.refreshes != 1 {
				t.Fatalf("err=%v calls=%d refreshes=%d", err, calls, provider.refreshes)
			}
		})
	}
	for _, tc := range []struct {
		status int
		body   string
		class  providerhttp.ErrorClass
		retry  bool
	}{
		{403, `private-token private-message`, providerhttp.ErrorClassAuth, false},
		{429, `private-token private-message`, providerhttp.ErrorClassRateLimited, true},
		{200, `{"code":99991400,"msg":"private-token private-message"}`, providerhttp.ErrorClassRateLimited, true},
		{503, `private-token private-message`, providerhttp.ErrorClassProvider, true},
		{200, `private-token private-message`, providerhttp.ErrorClassResponse, false},
		{200, `{"code":0}`, providerhttp.ErrorClassResponse, false},
	} {
		httpClient := &http.Client{Transport: botUserNamesTransport(func(*http.Request) (*http.Response, error) {
			response := botUserNamesResponse(tc.status, tc.body)
			response.Header.Set("Retry-After", "2")
			return response, nil
		})}
		client, _ := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "private-token"}, httpClient, false)
		_, err := client.MessageDetails(t.Context(), []string{"om_message"})
		var botErr *BotError
		if !errors.As(err, &botErr) || botErr.Class != tc.class || botErr.Retryable != tc.retry {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "private-message") {
			t.Fatal("provider body leaked")
		}
		if tc.retry && botErr.RetryDelay() != 2*time.Second {
			t.Fatal("Retry-After was discarded")
		}
	}
}

func TestFeishuMentionMessageDetailsHonorsTimeoutAndBatchLimit(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: botUserNamesTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	client, _ := newBotClient(defaultBaseURL, &fakeBotTokenProvider{token: "token"}, httpClient, false)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := client.MessageDetails(ctx, []string{"om_message"})
	var botErr *BotError
	if !errors.As(err, &botErr) || botErr.Class != providerhttp.ErrorClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
	for _, ids := range [][]string{nil, make([]string, 51), {"om_"}, {"oc_wrong"}, {"om_../invalid"}} {
		if _, err := client.MessageDetails(t.Context(), ids); err == nil {
			t.Fatal("invalid message batch accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("invalid input made %d requests", calls-1)
	}
}
