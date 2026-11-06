package shanghaimall

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMallLoginTransportErrorHidesURLCredentials(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(provider.name+"/"+cause.Error(), func(t *testing.T) {
				client := &http.Client{Transport: kerryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					return nil, &url.Error{Op: "Post", URL: "https://login.test/?pwd=private-login-password", Err: cause}
				})}
				token, err := provider.login(t.Context(), "https://login.test/?pwd=private-login-password", client)
				if token != "" || !errors.Is(err, cause) {
					t.Fatalf("transport failure token=%q error=%v, want cause=%v", token, err, cause)
				}
				if strings.Contains(err.Error(), "private-login-password") || strings.Contains(err.Error(), "login.test") {
					t.Fatalf("transport error exposed URL credentials: %q", err.Error())
				}
			})
		}
	}
}

func TestMallLoginReportsHTTPAndResponseFailuresSafely(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		for _, test := range []struct {
			name       string
			status     int
			body       string
			class      string
			retryAfter string
			wantDelay  time.Duration
		}{
			{name: "gateway HTML", status: http.StatusBadGateway, body: "<html>private-login-token</html>", class: "provider_unavailable"},
			{name: "authentication status takes precedence over token", status: http.StatusForbidden, body: provider.response, class: "authentication"},
			{name: "rate limit HTML", status: http.StatusTooManyRequests, body: "<html>private-login-token</html>", class: "rate_limited", retryAfter: "75", wantDelay: 75 * time.Second},
			{name: "successful status with HTML", status: http.StatusOK, body: "<html>private-login-token</html>", class: "invalid_response"},
			{name: "malformed JSON", status: http.StatusOK, body: `{"token":private-login-token}`, class: "invalid_response"},
		} {
			t.Run(provider.name+"/"+test.name, func(t *testing.T) {
				client := &http.Client{Transport: kerryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: test.status, Header: http.Header{
						"Content-Type": {"text/html; token=private-header-token"}, "Retry-After": {test.retryAfter},
					}, Body: io.NopCloser(strings.NewReader(test.body)), Request: request}, nil
				})}
				token, err := provider.login(t.Context(), "https://login.test/?password=private-url-password", client)
				if token != "" || err == nil {
					t.Fatalf("login token=%q error=%v, want failure", token, err)
				}
				for _, fragment := range []string{provider.name, "stage=login", "http_status=" + strconv.Itoa(test.status), "class=" + test.class, "content_type=text/html"} {
					if !strings.Contains(err.Error(), fragment) {
						t.Errorf("error=%q, missing diagnostic %q", err.Error(), fragment)
					}
				}
				for _, secret := range []string{"private-login-token", "private-url-password", "private-header-token", "test-token", "login.test"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error exposed credentials: %q", err.Error())
					}
				}
				var hint interface{ RetryDelay() time.Duration }
				if !errors.As(err, &hint) || hint.RetryDelay() != test.wantDelay {
					t.Errorf("retry delay hint=%v, want %s", hint, test.wantDelay)
				}
			})
		}
	}
}

func TestMallLoginDrainsSmallErrorResponsesForConnectionReuse(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		t.Run(provider.name, func(t *testing.T) {
			var connections atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				writer.Header().Set("Content-Type", "text/html")
				writer.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(writer, "<html>"+strings.Repeat("gateway unavailable ", 1024)+"</html>")
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			server.Start()
			defer server.Close()
			for range 3 {
				if token, err := provider.login(t.Context(), server.URL, server.Client()); err == nil || token != "" {
					t.Fatalf("gateway failure returned token=%q error=%v", token, err)
				}
			}
			if connections.Load() != 1 {
				t.Fatalf("small gateway responses used %d connections, want one", connections.Load())
			}
		})
	}
}

func TestMallLoginLimitsResponseReadSize(t *testing.T) {
	for _, provider := range mallLoginTestCases() {
		t.Run(provider.name, func(t *testing.T) {
			body := &countedLoginReader{reader: strings.NewReader(provider.response + strings.Repeat(" ", 2<<20))}
			client := &http.Client{Transport: kerryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body), Request: request}, nil
			})}
			token, err := provider.login(t.Context(), "https://login.test", client)
			if token != "" || err == nil || !strings.Contains(err.Error(), "response_too_large") {
				t.Fatalf("large response token=%q error=%v, want bounded failure", token, err)
			}
			if body.read > (1<<20)+1 {
				t.Fatalf("read %d bytes, exceeded bounded response size", body.read)
			}
		})
	}
}

type countedLoginReader struct {
	reader io.Reader
	read   int
}

func (reader *countedLoginReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	reader.read += n
	return n, err
}
