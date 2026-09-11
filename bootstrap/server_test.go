package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gin-biz-web-api/pkg/logger"
)

type shutdownContextRecorder struct {
	calls      int
	contextErr error
}

func (recorder *shutdownContextRecorder) Shutdown(ctx context.Context) error {
	recorder.calls++
	recorder.contextErr = ctx.Err()
	return nil
}

func TestShutdownServerStartsWhileCrontabIsStillStopping(t *testing.T) {
	cron := &fakeStoppableCron{stopped: make(chan struct{})}
	startCrontabLifecycle(cron)
	server := &shutdownContextRecorder{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if err := shutdownServerAndCrontab(ctx, server); err != nil {
		t.Fatalf("shutdownServerAndCrontab() error = %v", err)
	}
	if server.calls != 1 {
		t.Fatalf("server shutdown calls = %d, want 1", server.calls)
	}
	if server.contextErr != nil {
		t.Fatalf("server shutdown started with expired context: %v", server.contextErr)
	}
	if ctx.Err() == nil {
		t.Fatal("test cron did not consume the shared shutdown deadline")
	}
}

func TestMallWeatherExportContentRequestMatcher(t *testing.T) {
	const validPath = "/api/v1/weather-exports/00000000-0000-4000-8000-000000000017/content"
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{name: "GET content", method: http.MethodGet, path: validPath, want: true},
		{name: "POST content", method: http.MethodPost, path: validPath, want: true},
		{name: "unsupported method", method: http.MethodPut, path: validPath, want: false},
		{name: "job", method: http.MethodGet, path: "/api/v1/weather-exports/00000000-0000-4000-8000-000000000017", want: false},
		{name: "invalid UUID", method: http.MethodGet, path: "/api/v1/weather-exports/not-a-uuid/content", want: false},
		{name: "noncanonical UUID", method: http.MethodGet, path: "/api/v1/weather-exports/00000000000040008000000000000017/content", want: false},
		{name: "extra segment", method: http.MethodGet, path: validPath + "/extra", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if got := isMallWeatherExportContentRequest(request); got != test.want {
				t.Fatalf("isMallWeatherExportContentRequest()=%t, want %t", got, test.want)
			}
		})
	}
}

func TestMallWeatherExportContentUsesDedicatedWriteDeadline(t *testing.T) {
	const path = "/api/v1/weather-exports/00000000-0000-4000-8000-000000000017/content"
	const timeout = 16 * time.Minute
	nextCalls := 0
	handler := withMallWeatherExportDownloadWriteDeadline(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			nextCalls++
			w.WriteHeader(http.StatusNoContent)
		},
	), timeout)
	recorder := &writeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now().Add(timeout)

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))

	after := time.Now().Add(timeout)
	if nextCalls != 1 || recorder.Code != http.StatusNoContent ||
		recorder.deadline.Before(before) || recorder.deadline.After(after) {
		t.Fatalf(
			"nextCalls=%d status=%d deadline=%v, want between %v and %v",
			nextCalls,
			recorder.Code,
			recorder.deadline,
			before,
			after,
		)
	}
}

func TestRegularAPIKeepsServerWriteDeadline(t *testing.T) {
	handler := withMallWeatherExportDownloadWriteDeadline(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	), 16*time.Minute)
	recorder := &writeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/malls", nil))

	if recorder.Code != http.StatusNoContent || !recorder.deadline.IsZero() {
		t.Fatalf("status=%d deadline=%v", recorder.Code, recorder.deadline)
	}
}

func TestInitServerWiresMallWeatherExportWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	const path = "/api/v1/weather-exports/00000000-0000-4000-8000-000000000017/content"
	router.GET(path, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	server := initServer(router)
	if server.ReadHeaderTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("ReadHeaderTimeout=%s IdleTimeout=%s must both be positive", server.ReadHeaderTimeout, server.IdleTimeout)
	}
	recorder := &writeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now().Add(mallWeatherExportDownloadWriteTimeout)

	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	after := time.Now().Add(mallWeatherExportDownloadWriteTimeout)
	if recorder.Code != http.StatusNoContent ||
		recorder.deadline.Before(before) || recorder.deadline.After(after) {
		t.Fatalf(
			"status=%d deadline=%v, want between %v and %v",
			recorder.Code,
			recorder.deadline,
			before,
			after,
		)
	}
}

func TestGlobalMiddlewareKeepsDownloadContextClientScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLogger := logger.Logger
	logger.Logger = zap.NewNop()
	t.Cleanup(func() {
		logger.Logger = previousLogger
	})
	router := gin.New()
	registerGlobalMiddleWare(router)
	const path = "/api/v1/weather-exports/00000000-0000-4000-8000-000000000017/content"
	hasDeadline := true
	router.GET(path, func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Data(
			http.StatusOK,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			[]byte("PK\x03\x04xlsx"),
		)
	})
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	if recorder.Code != http.StatusOK || hasDeadline || recorder.Body.String() != "PK\x03\x04xlsx" {
		t.Fatalf(
			"status=%d hasDeadline=%t body=%q",
			recorder.Code,
			hasDeadline,
			recorder.Body.String(),
		)
	}
}

type writeDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (recorder *writeDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	recorder.deadline = deadline
	return nil
}
