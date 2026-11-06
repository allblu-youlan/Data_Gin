package youzan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDistributionClientReportsProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		want       []string
		wantDelay  time.Duration
	}{
		{
			name: "gateway numeric rate limit", status: http.StatusOK,
			body: `{"gw_err_resp":{"err_code":4101,"err_msg":"请求太频繁，请控制请求频率或检查应用API账单是否欠费","trace_id":"yz-test-4101"}}`,
			want: []string{"http_status=200", "code=4101", "请求太频繁", "应用API账单是否欠费", "trace_id=yz-test-4101"}, wantDelay: time.Minute,
		},
		{
			name: "gateway string rate limit", status: http.StatusOK,
			body: `{"gw_err_resp":{"err_code":"4101","err_msg":"请求太频繁","trace_id":"yz-string-4101"}}`,
			want: []string{"code=4101", "trace_id=yz-string-4101"}, wantDelay: time.Minute,
		},
		{
			name: "gateway permission error", status: http.StatusOK,
			body: `{"gw_err_resp":{"err_code":4005,"err_msg":"非法的API","trace_id":"yz-permission"}}`,
			want: []string{"code=4005", "非法的API", "trace_id=yz-permission"},
		},
		{
			name: "business rate limit", status: http.StatusOK,
			body: `{"success":false,"code":4101,"message":"请求太频繁","trace_id":"yz-business"}`,
			want: []string{"code=4101", "请求太频繁", "trace_id=yz-business"}, wantDelay: time.Minute,
		},
		{
			name: "HTTP rate limit with longer cooldown", status: http.StatusTooManyRequests,
			body:       `{"gw_err_resp":{"err_code":4101,"err_msg":"请求太频繁","trace_id":"yz-http"}}`,
			retryAfter: "120", want: []string{"http_status=429", "code=4101", "trace_id=yz-http"}, wantDelay: 2 * time.Minute,
		},
		{
			name: "HTTP rate limit without JSON", status: http.StatusTooManyRequests,
			body: "private-response-body", want: []string{"http_status=429"}, wantDelay: time.Minute,
		},
		{
			name: "server error cooldown", status: http.StatusServiceUnavailable,
			body:       `{"success":false,"code":500,"message":"temporarily unavailable","trace_id":"yz-unavailable"}`,
			retryAfter: "120", want: []string{"http_status=503", "code=500", "trace_id=yz-unavailable"}, wantDelay: 2 * time.Minute,
		},
		{
			name: "permanent HTTP error ignores cooldown", status: http.StatusUnauthorized,
			body:       `{"success":false,"code":401,"message":"unauthorized"}`,
			retryAfter: "120", want: []string{"http_status=401", "code=401"},
		},
		{
			name: "unknown response envelope", status: http.StatusOK,
			body: `{"unrecognized":true}`, want: []string{"response"},
		},
		{
			name: "successful envelope missing data", status: http.StatusOK,
			body: `{"success":true,"code":200}`, want: []string{"response"},
		},
		{
			name: "successful envelope null data", status: http.StatusOK,
			body: `{"success":true,"code":200,"data":null}`, want: []string{"response"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", test.retryAfter)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := NewDistributionClient(server.URL, server.URL, server.Client())
			operations := map[string]func() error{
				"orders": func() error {
					_, _, err := client.FetchOrderPage(context.Background(), "test-token", OrderTimeFilterCreated,
						"2026-11-05 00:00:00", "2026-11-05 23:59:59", 1, 100)
					return err
				},
				"decrypt": func() error {
					_, err := client.DecryptBatch(context.Background(), "test-token", []string{"encrypted"})
					return err
				},
			}
			for name, operation := range operations {
				t.Run(name, func(t *testing.T) {
					err := operation()
					if err == nil {
						t.Fatal("provider failure was accepted as success")
					}
					for _, want := range test.want {
						if !strings.Contains(err.Error(), want) {
							t.Errorf("error=%v, want %q", err, want)
						}
					}
					if strings.Contains(err.Error(), "code=0 message=") || strings.Contains(err.Error(), "private-response-body") {
						t.Errorf("unsafe or uninformative error=%v", err)
					}
					var hint interface{ RetryDelay() time.Duration }
					var delay time.Duration
					if errors.As(fmt.Errorf("fetch page 1: %w", err), &hint) {
						delay = hint.RetryDelay()
					}
					if delay != test.wantDelay {
						t.Errorf("retry delay=%s, want %s", delay, test.wantDelay)
					}
				})
			}
		})
	}
}

func TestDistributionClientRedactsGatewayFailure(t *testing.T) {
	const token = "private-token+/="
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"gw_err_resp":{"err_code":4101,"err_msg":"access_token=%s original %s","trace_id":"%s"}}`, url.QueryEscape(token), token, token)
	}))
	defer server.Close()
	client := NewDistributionClient(server.URL, server.URL, server.Client())
	_, _, err := client.FetchOrderPage(context.Background(), token, OrderTimeFilterCreated,
		"2026-11-05 00:00:00", "2026-11-05 23:59:59", 1, 100)
	if err == nil || !strings.Contains(err.Error(), "code=4101") {
		t.Fatalf("gateway error=%v", err)
	}
	for _, secret := range []string{token, url.QueryEscape(token)} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("gateway error exposed the access token")
		}
	}
}

func TestDistributionClientRedactsMalformedGatewayResponse(t *testing.T) {
	const token = "private-token+/="
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"gw_err_resp":{"err_code":%q}}`, token)
	}))
	defer server.Close()
	client := NewDistributionClient(server.URL, server.URL, server.Client())
	_, _, err := client.FetchOrderPage(context.Background(), token, OrderTimeFilterCreated,
		"2026-11-05 00:00:00", "2026-11-05 23:59:59", 1, 100)
	if err == nil {
		t.Fatal("malformed gateway response was accepted")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatal("JSON decoder error exposed the access token")
	}
}
