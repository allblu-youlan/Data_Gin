package data_svc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/youzan"
)

type youzanTokenTestFailure struct {
	cause error
	delay time.Duration
}

func (err *youzanTokenTestFailure) Error() string             { return err.cause.Error() }
func (err *youzanTokenTestFailure) Unwrap() error             { return err.cause }
func (err *youzanTokenTestFailure) RetryDelay() time.Duration { return err.delay }

func TestYouzanDistributionTokenFailurePreservesCauseAndRetryHintWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		delay time.Duration
	}{
		{name: "provider cooldown", cause: errors.New("token temporarily unavailable"), delay: 2 * time.Minute},
		{name: "cancellation", cause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeYouzanDistributionClient{}
			writer := &fakeYouzanDistributionWriter{}
			failure := &youzanTokenTestFailure{cause: test.cause, delay: test.delay}
			service := newYouzanDistributionOrderService(client, writer, func(context.Context) (string, error) { return "", failure })
			result, err := service.SyncRange(t.Context(), youzan.OrderTimeFilterCreated, "2026-11-07 00:00:00", "2026-11-07 23:59:59")
			var hint interface{ RetryDelay() time.Duration }
			if !errors.Is(err, test.cause) || !errors.As(err, &hint) || hint.RetryDelay() != test.delay {
				t.Fatalf("token error lost cause or retry hint: %v", err)
			}
			if result.FetchPages != 0 || result.SavedCount != 0 || len(client.timeFilters) != 0 || len(writer.batches) != 0 {
				t.Fatalf("token failure continued syncing: result=%+v pages=%v batches=%v", result, client.timeFilters, writer.batches)
			}
		})
	}
}

func TestYouzanDistributionCancellationStopsTokenHTTPBeforeFetchingOrders(t *testing.T) {
	initYouzanDistributionTestLogger()
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	var orderRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			orderRequests.Add(1)
			_, _ = io.WriteString(writer, `{"success":true,"code":200,"data":{"full_order_info_list":[],"paginator":{"has_next":false}}}`)
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		close(started)
		select {
		case <-request.Context().Done():
			close(canceled)
		case <-release:
			_, _ = io.WriteString(writer, `{"success":true,"code":200,"data":{"access_token":"test-token"}}`)
		}
	}))
	defer server.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	setupYouzanDistributionTokenTestConfig(t, server.URL)
	service := NewYouzanDistributionOrderService()
	writer := &fakeYouzanDistributionWriter{}
	service.writer = writer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		result *YouzanDistributionSyncResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.SyncRange(ctx, youzan.OrderTimeFilterCreated, "2026-11-07 00:00:00", "2026-11-07 23:59:59")
		done <- outcome{result, err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("token HTTP request did not start")
	}
	cancel()
	var completed outcome
	select {
	case completed = <-done:
	case <-time.After(time.Second):
		close(release)
		released = true
		completed = <-done
		t.Fatalf("task cancellation did not stop the token HTTP request: %v", completed.err)
	}
	if !errors.Is(completed.err, context.Canceled) || completed.result.FetchPages != 0 || completed.result.SavedCount != 0 || orderRequests.Load() != 0 || len(writer.batches) != 0 {
		t.Fatalf("canceled token continued syncing: result=%+v error=%v order requests=%d writes=%d", completed.result, completed.err, orderRequests.Load(), len(writer.batches))
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("token HTTP server did not observe request cancellation")
	}
}

func setupYouzanDistributionTokenTestConfig(t *testing.T, endpoint string) {
	t.Helper()
	directory := t.TempDir()
	contents := "Youzan:\n  TokenURL: " + endpoint + "/token\n  OrdersURL: " + endpoint + "/orders\n  DecryptURL: " + endpoint + "/decrypt\n  ClientID: test-client\n  ClientSecret: test-secret\n  GrantID: test-grant\n"
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
}
