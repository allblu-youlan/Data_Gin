package shanghaimall

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type mallLoginTestCase struct {
	name     string
	response string
	login    func(context.Context, string, *http.Client) (string, error)
}

func mallLoginTestCases() []mallLoginTestCase {
	return []mallLoginTestCase{
		{
			name: "xian", response: `{"token":"test-token"}`,
			login: func(ctx context.Context, endpoint string, client *http.Client) (string, error) {
				return getXianToken(ctx, xianConfig{TokenURL: endpoint, Account: "test", Password: "test", Client: client})
			},
		},
		{
			name: "kerry", response: `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`,
			login: func(ctx context.Context, endpoint string, client *http.Client) (string, error) {
				return getKerryAPIKey(ctx, kerryConfig{LoginURL: endpoint, Client: client})
			},
		},
	}
}

func TestMallLoginConsumesResponseForConnectionReuse(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		for _, tail := range []struct{ name, value string }{
			{name: "compact"},
			{name: "long whitespace", value: strings.Repeat(" ", 16*1024)},
			{name: "ignored trailing content", value: strings.Repeat(" ", 16*1024) + "trailing content"},
		} {
			t.Run(provider.name+"/"+tail.name, func(t *testing.T) {
				var connections atomic.Int32
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					_, _ = io.Copy(io.Discard, request.Body)
					_, _ = io.WriteString(writer, provider.response+tail.value)
				}))
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						connections.Add(1)
					}
				}
				server.Start()
				defer server.Close()
				client := server.Client()
				for range 5 {
					token, err := provider.login(t.Context(), server.URL, client)
					if err != nil || token != "test-token" {
						t.Fatalf("login token=%q error=%v", token, err)
					}
				}
				if connections.Load() != 1 {
					t.Fatalf("five logins opened %d connections, want 1", connections.Load())
				}
			})
		}
	}
}

func TestMallLoginReturnsResponseErrors(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		for _, body := range []string{`{`, `{}`, `{"content":{"status":"FAILED"}}`, `{"content":{"status":"SUCCESS"}}`} {
			t.Run(provider.name+"/"+body, func(t *testing.T) {
				client := kerryTestClient(body)
				token, err := provider.login(t.Context(), "https://login.test", client)
				if err == nil || token != "" {
					t.Fatalf("invalid response token=%q error=%v", token, err)
				}
				if body == "{" && !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("malformed JSON error=%v, want unexpected EOF", err)
				}
			})
		}
	}
}

func TestMallLoginHonorsContextDuringResponseDrain(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		t.Run(provider.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				_, _ = io.WriteString(writer, provider.response)
				writer.(http.Flusher).Flush()
				<-request.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			token, err := provider.login(ctx, server.URL, server.Client())
			if !errors.Is(err, context.DeadlineExceeded) || token != "" {
				t.Fatalf("stalled response token=%q error=%v, want context deadline", token, err)
			}

			canceledCtx, cancelRequest := context.WithCancel(t.Context())
			cancelRequest()
			if _, err := provider.login(canceledCtx, server.URL, server.Client()); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled login error=%v", err)
			}
		})
	}
}
