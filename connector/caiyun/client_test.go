package caiyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

func TestClientFetchWeatherUsesSignedRequestAndRedactsRawBody(t *testing.T) {
	var called bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		called = true
		if request.URL.EscapedPath() != "/v2.6/test_app_key_2026/121.4551234,31.2285678/weather" || request.URL.Query().Get("unit") != "metric:v2" {
			t.Fatalf("request URL=%s", request.URL)
		}
		if request.Header.Get("x-cy-signature") == "" || request.Header.Get("x-cy-app-key") != "" {
			t.Fatalf("headers=%#v", request.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","server_time":1784688000,"result":{},"echo_key":%q,"echo_secret":%q,"echo_nonce":%q,"echo_signature":%q}`,
			testAppKey, testAppSecret, request.Header.Get("x-cy-nonce"), request.Header.Get("x-cy-signature"))
	})
	client := testClient(t, handler)
	response, err := client.FetchWeather(context.Background(), WeatherRequest{
		Longitude: 121.4551234, Latitude: 31.2285678,
		HourlySteps: 72, DailySteps: 7, Alert: true, Unit: "metric:v2",
	})
	if err != nil {
		t.Fatalf("FetchWeather() error=%v", err)
	}
	if !called || response.EndpointKind != EndpointWeatherV26 || response.HTTPStatus != http.StatusOK || response.ProviderStatus != "ok" {
		t.Fatalf("response=%+v", response)
	}
	if !json.Valid(response.RawBody) || !strings.Contains(string(response.RawBody), `"server_time":1784688000`) {
		t.Fatalf("raw body=%s", response.RawBody)
	}
	for _, forbidden := range []string{testAppKey, testAppSecret, testNonce, requestSignatureForTest(t)} {
		if strings.Contains(string(response.RawBody), forbidden) {
			t.Fatalf("raw body leaked %q: %s", forbidden, response.RawBody)
		}
	}
}

func TestClientClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		class     providerhttp.ErrorClass
		retryable bool
	}{
		{name: "authentication", status: http.StatusUnauthorized, class: providerhttp.ErrorClassAuth},
		{name: "rate limit", status: http.StatusTooManyRequests, class: providerhttp.ErrorClassRateLimited, retryable: true},
		{name: "server failure", status: http.StatusBadGateway, class: providerhttp.ErrorClassProvider, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, `{"status":"error"}`)
			})
			response, err := testClient(t, handler).FetchWeather(context.Background(), validWeatherRequest())
			providerError := requireProviderError(t, err)
			if providerError.Class != test.class || providerError.Retryable != test.retryable || providerError.HTTPStatus != test.status {
				t.Fatalf("ProviderError=%+v", providerError)
			}
			if response == nil || response.HTTPStatus != test.status || !json.Valid(response.RawBody) {
				t.Fatalf("response=%+v", response)
			}
		})
	}
}

func TestClientHTTPFailureRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		header string
		want   time.Duration
	}{
		{name: "rate limit seconds", status: http.StatusTooManyRequests, header: "90", want: 90 * time.Second},
		{name: "server failure seconds", status: http.StatusServiceUnavailable, header: "120", want: 2 * time.Minute},
		{name: "permanent authentication", status: http.StatusUnauthorized, header: "90"},
		{name: "missing header", status: http.StatusTooManyRequests},
		{name: "invalid header", status: http.StatusTooManyRequests, header: "invalid-header-secret"},
		{name: "duration overflow", status: http.StatusTooManyRequests, header: "9223372037"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", test.header)
				w.WriteHeader(test.status)
				fmt.Fprint(w, "Rate limit exceeded")
			}))
			response, err := client.FetchWeather(t.Context(), validWeatherRequest())
			providerError := requireProviderError(t, err)
			var hint interface{ RetryDelay() time.Duration }
			if !errors.As(err, &hint) {
				t.Fatal("HTTP failure does not expose a retry delay")
			}
			if got := hint.RetryDelay(); got != test.want {
				t.Fatalf("RetryDelay()=%v want %v", got, test.want)
			}
			if response == nil || response.HTTPStatus != test.status || string(response.RawBody) != "Rate limit exceeded" {
				t.Fatalf("response=%+v", response)
			}
			if providerError.HTTPStatus != test.status || strings.Contains(err.Error(), "invalid-header-secret") {
				t.Fatalf("unsafe or incorrect error=%v", err)
			}
		})
	}
}

func TestClientHTTPFailureRetryAfterDate(t *testing.T) {
	retryAt := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", retryAt.Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Rate limit exceeded")
	}))
	startedAt := time.Now()
	_, err := client.FetchWeather(t.Context(), validWeatherRequest())
	finishedAt := time.Now()
	var hint interface{ RetryDelay() time.Duration }
	if !errors.As(err, &hint) {
		t.Fatal("HTTP failure does not expose a retry delay")
	}
	if got := hint.RetryDelay(); got < retryAt.Sub(finishedAt) || got > retryAt.Sub(startedAt) {
		t.Fatalf("RetryDelay()=%v outside HTTP-date interval", got)
	}
}

func TestClientRejectsProviderBusinessAndMalformedEnvelopes(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		class  providerhttp.ErrorClass
		status string
		code   string
	}{
		{name: "weather provider rejected", body: `{"status":"failed","result":{}}`, class: providerhttp.ErrorClassProvider, status: "failed", code: "failed"},
		{name: "weather missing result", body: `{"status":"ok"}`, class: providerhttp.ErrorClassResponse, status: "ok"},
		{name: "weather invalid status", body: `{"status":"bad\nstatus","result":{}}`, class: providerhttp.ErrorClassResponse},
		{name: "malformed JSON", body: `not-json`, class: providerhttp.ErrorClassResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) })
			client := testClient(t, handler)
			response, err := client.FetchWeather(context.Background(), validWeatherRequest())
			providerError := requireProviderError(t, err)
			if providerError.Class != test.class || providerError.Code != test.code {
				t.Fatalf("ProviderError=%+v", providerError)
			}
			if response == nil || response.ProviderStatus != test.status {
				t.Fatalf("response=%+v", response)
			}
			if strings.Contains(err.Error(), "invalid app key") || strings.Contains(err.Error(), "bad status") {
				t.Fatalf("error leaked provider detail: %v", err)
			}
		})
	}
}

func TestClientRejectsOversizedOrMissingResponse(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 64))
	}))
	client.maxResponseBytes = 16
	if response, err := client.FetchWeather(context.Background(), validWeatherRequest()); requireProviderError(t, err).Class != providerhttp.ErrorClassResponse || response != nil {
		t.Fatalf("response=%+v error=%v", response, err)
	}

	client = testClientWithDoer(t, nilResponseDoer{})
	if _, err := client.FetchWeather(context.Background(), validWeatherRequest()); requireProviderError(t, err).Class != providerhttp.ErrorClassResponse {
		t.Fatalf("error=%v", err)
	}

	client = testClientWithDoer(t, closeErrorDoer{})
	_, err := client.FetchWeather(context.Background(), validWeatherRequest())
	providerError := requireProviderError(t, err)
	if providerError.Class != providerhttp.ErrorClassTransport || !providerError.Retryable {
		t.Fatalf("close ProviderError=%+v", providerError)
	}
}

func TestClientTransportErrorCannotLeakSignedURL(t *testing.T) {
	client := testClientWithDoer(t, leakingCaiyunDoer{})
	_, err := client.FetchWeather(context.Background(), validWeatherRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchWeather() error=%v want context.Canceled", err)
	}
	for _, forbidden := range []string{testAppKey, testAppSecret, "x-cy-signature", "metric%3Av2"} {
		if strings.Contains(fmt.Sprintf("%+v", err), forbidden) {
			t.Fatalf("error leaked %q: %v", forbidden, err)
		}
	}
}

func TestClientRejectsInvalidInputAndRedactsFormatting(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("provider called for invalid input")
	}))
	if _, err := client.FetchWeather(context.Background(), WeatherRequest{}); requireProviderError(t, err).Class != providerhttp.ErrorClassRequest {
		t.Fatalf("error=%v", err)
	}
	for _, rendered := range []string{fmt.Sprint(client), fmt.Sprintf("%+v", client), fmt.Sprintf("%#v", client)} {
		if strings.Contains(rendered, testAppKey) || strings.Contains(rendered, testAppSecret) {
			t.Fatalf("formatted client leaked credentials: %s", rendered)
		}
	}
	encoded, err := json.Marshal(client)
	if err != nil || strings.Contains(string(encoded), testAppKey) || strings.Contains(string(encoded), testAppSecret) {
		t.Fatalf("serialized client=%s error=%v", encoded, err)
	}
}

func TestNewClientWithConfigAppliesBoundedLimits(t *testing.T) {
	builder := fixedRequestBuilder(t)
	client, err := NewClientWithConfig(builder, caiyunHandlerDoer{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}, ClientConfig{
		Timeout: 3 * time.Second, MaxResponseBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig() error=%v", err)
	}
	if client.timeout != 3*time.Second || client.maxResponseBytes != 1024 {
		t.Fatalf("client limits timeout=%v max=%d", client.timeout, client.maxResponseBytes)
	}
	for _, config := range []ClientConfig{
		{Timeout: time.Millisecond, MaxResponseBytes: 1024},
		{Timeout: time.Second, MaxResponseBytes: defaultMaxResponseBytes + 1},
	} {
		if _, err := NewClientWithConfig(builder, caiyunHandlerDoer{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}, config); err == nil {
			t.Fatalf("NewClientWithConfig(%+v) error=nil", config)
		}
	}
}

type caiyunHandlerDoer struct{ handler http.Handler }

func (doer caiyunHandlerDoer) Do(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	doer.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

type leakingCaiyunDoer struct{}

func (leakingCaiyunDoer) Do(request *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("send %s x-cy-signature=%s: %w", request.URL, request.Header.Get("x-cy-signature"), context.Canceled)
}

type nilResponseDoer struct{}

func (nilResponseDoer) Do(*http.Request) (*http.Response, error) { return nil, nil }

type closeErrorDoer struct{}

func (closeErrorDoer) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       closeErrorBody{Reader: strings.NewReader(`{"status":"ok","result":{}}`)},
		Request:    request,
	}, nil
}

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error { return errors.New("close failed") }

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	return testClientWithDoer(t, caiyunHandlerDoer{handler: handler})
}

func testClientWithDoer(t *testing.T, doer HTTPDoer) *Client {
	t.Helper()
	client, err := NewClient(fixedRequestBuilder(t), doer)
	if err != nil {
		t.Fatalf("NewClient() error=%v", err)
	}
	return client
}

func validWeatherRequest() WeatherRequest {
	return WeatherRequest{Longitude: 121, Latitude: 31, HourlySteps: 24, DailySteps: 1, Alert: true, Unit: "metric:v2"}
}

func requireProviderError(t *testing.T, err error) *ProviderError {
	t.Helper()
	var providerError *ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("error=%v want ProviderError", err)
	}
	return providerError
}

func requestSignatureForTest(t *testing.T) string {
	t.Helper()
	request, err := fixedRequestBuilder(t).NewWeatherRequest(context.Background(), WeatherRequest{
		Longitude: 121.4551234, Latitude: 31.2285678,
		HourlySteps: 72, DailySteps: 7, Alert: true, Unit: "metric:v2",
	})
	if err != nil {
		t.Fatalf("NewWeatherRequest() error=%v", err)
	}
	return request.Header.Get("x-cy-signature")
}
