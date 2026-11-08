package youzan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenClientValidatesResponsesWithoutExposingCredentials(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType, retryAfter, reason, class string
		status                                             int
		delay                                              time.Duration
		wantToken                                          bool
	}{
		{name: "success", status: 200, body: `{"success":true,"code":200,"data":{"access_token":"private-token"}}`, wantToken: true},
		{name: "unavailable HTML", status: 503, contentType: "text/html; credential=private-client-secret", body: "<html>private-client-secret private-token</html>", class: "provider_unavailable", reason: "http_failure", delay: time.Minute},
		{name: "rate limit", status: 429, retryAfter: "120", body: "<html>private-client-secret</html>", class: "rate_limited", reason: "http_failure", delay: 2 * time.Minute},
		{name: "rate limit minimum", status: 429, retryAfter: "2", class: "rate_limited", reason: "http_failure", delay: time.Minute},
		{name: "gateway retry hint", status: 502, retryAfter: "180", class: "provider_unavailable", reason: "http_failure", delay: 3 * time.Minute},
		{name: "non-success status with token", status: 403, body: `{"success":true,"code":200,"data":{"access_token":"private-token"}}`, class: "authentication", reason: "http_failure"},
		{name: "successful status HTML", status: 200, body: "<html>private-client-secret private-token</html>", class: "invalid_response", reason: "invalid_json"},
		{name: "rejected token", status: 200, body: `{"success":false,"code":500,"message":"private-client-secret private-token"}`, class: "invalid_response", reason: "api_failure"},
		{name: "non-success API code", status: 200, body: `{"success":true,"code":400,"data":{"access_token":"private-token"}}`, class: "invalid_response", reason: "api_failure"},
		{name: "empty token", status: 200, body: `{"success":true,"code":200,"data":{"access_token":" "}}`, class: "invalid_response", reason: "missing_token"},
		{name: "malformed code", status: 200, body: `{"success":false,"code":"private-token"}`, class: "invalid_response", reason: "invalid_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || body["authorize_type"] != "silent" || body["refresh"] != false || body["client_secret"] != "private-client-secret" || body["grant_id"] != "test-grant" {
					t.Error("token request changed the existing protocol")
				}
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Retry-After", test.retryAfter)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := NewTokenClient(server.Client())
			if err != nil {
				t.Fatal(err)
			}
			token, err := client.AccessToken(t.Context(), tokenTestConfig(server.URL))
			if requests.Load() != 1 {
				t.Fatalf("requests=%d, want one without local retries", requests.Load())
			}
			if test.wantToken {
				if err != nil || token != "private-token" {
					t.Fatalf("valid token response failed: token length=%d error=%v", len(token), err)
				}
				return
			}
			if err == nil || token != "" {
				t.Fatalf("invalid response accepted: token length=%d error=%v", len(token), err)
			}
			for _, field := range []string{"stage=token", "class=" + test.class, "reason=" + test.reason} {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("missing %s: %v", field, err)
				}
			}
			assertTokenErrorSafe(t, err)
			var hint interface{ RetryDelay() time.Duration }
			if !errors.As(err, &hint) || hint.RetryDelay() != test.delay {
				t.Errorf("retry delay=%v, want %s", hint, test.delay)
			}
		})
	}
}

func TestTokenClientHonorsContextAndClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, test := range []struct {
		name    string
		timeout bool
	}{
		{name: "caller cancellation"},
		{name: "HTTP client timeout", timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			httpClient := server.Client()
			httpClient.Timeout = 40 * time.Millisecond
			client, err := NewTokenClient(httpClient)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.DeadlineExceeded
			if !test.timeout {
				cancel()
				want = context.Canceled
			}
			_, err = client.AccessToken(ctx, tokenTestConfig(server.URL))
			if !errors.Is(err, want) {
				t.Fatalf("error=%v, want %v", err, want)
			}
			if test.timeout && ctx.Err() != nil {
				t.Error("client timeout canceled the caller context")
			}
		})
	}
}

func TestTokenClientBoundsResponseReadsAndClosesBody(t *testing.T) {
	body := &tokenCountingBody{reader: strings.NewReader(strings.Repeat(" ", 2<<20))}
	client, err := NewTokenClient(&http.Client{Transport: tokenRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AccessToken(t.Context(), tokenTestConfig("https://token.test"))
	if err == nil || !strings.Contains(err.Error(), "reason=response_too_large") || body.read > (1<<20)+1 || !body.closed {
		t.Fatalf("error=%v read=%d closed=%t", err, body.read, body.closed)
	}
}

func TestTokenClientReusesConnectionsForSuccessAndSmallFailures(t *testing.T) {
	var connections, requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "<html>unavailable</html>")
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"code":200,"data":{"access_token":"private-token"}}`+strings.Repeat(" ", 16*1024))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	client, err := NewTokenClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.httpClient.CloseIdleConnections()
	if client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("default timeout=%s", client.httpClient.Timeout)
	}
	for attempt := range 4 {
		token, err := client.AccessToken(t.Context(), tokenTestConfig(server.URL))
		if (attempt == 0 && err == nil) || (attempt > 0 && (err != nil || token == "")) {
			t.Fatalf("attempt=%d token length=%d error=%v", attempt, len(token), err)
		}
	}
	if connections.Load() != 1 || requests.Load() != 4 {
		t.Fatalf("connections=%d requests=%d, want 1/4 without token caching", connections.Load(), requests.Load())
	}
}

func TestTokenClientHidesTransportCredentialsAndPreservesCause(t *testing.T) {
	client, err := NewTokenClient(&http.Client{Transport: tokenRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Post", URL: "https://private-client-secret@token.test/?access_token=private-token", Err: context.DeadlineExceeded}
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AccessToken(t.Context(), tokenTestConfig("https://token.test/?client_secret=private-client-secret"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want preserved deadline", err)
	}
	assertTokenErrorSafe(t, err)
	_, err = client.AccessToken(t.Context(), tokenTestConfig("https://private-client-secret@token.test/%bad%"))
	if err == nil {
		t.Fatal("invalid URL accepted")
	}
	assertTokenErrorSafe(t, err)
}

func tokenTestConfig(endpoint string) TokenConfig {
	return TokenConfig{URL: endpoint, ClientID: "test-client", ClientSecret: "private-client-secret", GrantID: "test-grant"}
}

func assertTokenErrorSafe(t *testing.T, err error) {
	t.Helper()
	for current := err; current != nil; current = errors.Unwrap(current) {
		for _, secret := range []string{"private-token", "private-client-secret", "<html>", "token.test"} {
			if strings.Contains(current.Error(), secret) {
				t.Error("error chain exposed credentials, URL or HTML")
			}
		}
	}
}

type tokenRoundTripper func(*http.Request) (*http.Response, error)

func (f tokenRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type tokenCountingBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *tokenCountingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *tokenCountingBody) Close() error {
	b.closed = true
	return nil
}
