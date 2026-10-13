package feishu

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

func TestBotCardSendAndUpdateProtocol(t *testing.T) {
	card := `{"schema":"2.0","body":{"elements":[]}}`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["content"] != card {
			t.Errorf("card content changed: %q", payload["content"])
		}
		if r.Method == http.MethodPost {
			if r.URL.Query().Get("receive_id_type") != "open_id" || payload["msg_type"] != "interactive" || payload["receive_id"] != "ou_person" {
				t.Errorf("send payload: %#v", payload)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_card"}}`))
			return
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/open-apis/im/v1/messages/om_card" || len(payload) != 1 {
			t.Errorf("invalid update: %s %s %#v", r.Method, r.URL.Path, payload)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer server.Close()
	client, err := newBotClient(server.URL, &fakeBotTokenProvider{token: "test"}, server.Client(), true)
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.SendCard(t.Context(), "ou_person", card, "53c40328-a5d0-446c-8f04-bbd99d95a15b")
	if err != nil || id != "om_card" {
		t.Fatalf("send: %s %v", id, err)
	}
	if err := client.UpdateCard(t.Context(), id, card); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestBotCardBusinessCodesAndRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		retry  bool
		class  providerhttp.ErrorClass
	}{
		{"business throttle", 200, `{"code":230020}`, true, providerhttp.ErrorClassRateLimited},
		{"http throttle", 429, `{}`, true, providerhttp.ErrorClassRateLimited},
		{"temporary business failure", 200, `{"code":230002}`, true, providerhttp.ErrorClassProvider},
		{"permission denied", 200, `{"code":230013}`, false, providerhttp.ErrorClassRequest},
		{"missing code", 200, `{"data":{}}`, false, providerhttp.ErrorClassResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, _ := newBotClient(server.URL, &fakeBotTokenProvider{token: "test"}, server.Client(), true)
			err := client.UpdateCard(t.Context(), "om_card", `{"schema":"2.0"}`)
			var botErr *BotError
			if !errors.As(err, &botErr) || botErr.Retryable != test.retry || botErr.Class != test.class {
				t.Fatalf("err=%#v", err)
			}
			if test.retry && botErr.RetryDelay() != 17*time.Second {
				t.Fatalf("retry after %v", botErr.RetryDelay())
			}
		})
	}
}

func TestBotCardRefreshesTokenOnHTTP200BusinessError(t *testing.T) {
	calls := 0
	provider := &fakeBotTokenProvider{token: "stale", refreshToken: "fresh"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer stale" {
			_, _ = w.Write([]byte(`{"code":99991663}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_card"}}`))
	}))
	defer server.Close()
	client, _ := newBotClient(server.URL, provider, server.Client(), true)
	if _, err := client.SendCard(t.Context(), "ou_person", `{"schema":"2.0"}`, "uuid"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || provider.refreshes != 1 {
		t.Fatalf("calls=%d refresh=%d", calls, provider.refreshes)
	}
}

func TestBotCardRejectsOversizedActualBody(t *testing.T) {
	client := &BotClient{}
	card, _ := json.Marshal(map[string]string{"text": strings.Repeat("\"中", 10000)})
	if _, err := client.SendCard(t.Context(), "ou_person", string(card), "uuid"); err == nil {
		t.Fatal("oversized card accepted")
	}
	now := time.Now().UTC().Truncate(time.Second)
	if got := botRetryAfter(http.Header{"Retry-After": []string{now.Add(time.Minute).Format(http.TimeFormat)}}, now); got != time.Minute {
		t.Fatalf("date delay=%v", got)
	}
}
