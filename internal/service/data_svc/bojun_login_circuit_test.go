package data_svc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/shanghaimall"
)

type fakeBojunLoginCircuit struct {
	allowed                         bool
	wait                            time.Duration
	allowErr                        error
	reportErr                       error
	failures, successes, rateLimits int
	rateDelay                       time.Duration
}

func (c *fakeBojunLoginCircuit) Allow(context.Context) (bool, time.Duration, error) {
	return c.allowed, c.wait, c.allowErr
}
func (c *fakeBojunLoginCircuit) ReportFailure(context.Context) error {
	c.failures++
	return c.reportErr
}
func (c *fakeBojunLoginCircuit) ReportSuccess(context.Context) error {
	c.successes++
	return c.reportErr
}
func (c *fakeBojunLoginCircuit) ReportRateLimited(_ context.Context, delay time.Duration) error {
	c.rateLimits++
	c.rateDelay = delay
	return c.reportErr
}

func TestBojunLoginCircuitDefersWithoutHTTPRequestOrStatusWrites(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.WriteString(w, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"},"error":false}`)
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	store, logs, runs := &fakeBojunSyncUpdater{}, &fakeDeliveryLogCreator{}, &fakeBojunPipelineRunRecorder{}
	service := &BojunOrderPushService{retailOrderDAO: store, logDAO: logs, pipelineRunDAO: runs,
		loginCircuit: &fakeBojunLoginCircuit{wait: 30 * time.Second}}
	result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "COOLDOWN-1", BillDate: 20261106})
	var deferred *bojunLoginDeferredError
	if !errors.As(result.Error, &deferred) || !deferred.TaskDeferred() || deferred.RetryDelay() < 30*time.Second || result.Success || result.Skipped {
		t.Fatalf("result=%+v, want deferred unsuccessful order", result)
	}
	if requests != 0 || len(store.updates) != 0 || len(logs.logs) != 0 || runs.finishStatus != "" {
		t.Fatalf("deferral performed work: requests=%d status=%v logs=%d pipeline=%s", requests, store.updates, len(logs.logs), runs.finishStatus)
	}
}

func TestBojunLoginCircuitReportsOnlyRealLoginFailures(t *testing.T) {
	for _, test := range []struct {
		name                                    string
		loginStatus, salesStatus                int
		retryAfter                              string
		wantFailure, wantSuccess, wantRateLimit int
		wantPushSuccess                         bool
	}{
		{name: "login bad gateway", loginStatus: 502, wantFailure: 1},
		{name: "long retry after", loginStatus: 503, retryAfter: "180", wantRateLimit: 1},
		{name: "login recovered but sales failed", loginStatus: 200, salesStatus: 502, wantSuccess: 1},
		{name: "login and sales succeeded", loginStatus: 200, salesStatus: 200, wantSuccess: 1, wantPushSuccess: true},
		{name: "non transient login rejection releases probe", loginStatus: 403, wantSuccess: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var loginCalls, salesCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					loginCalls++
					w.Header().Set("Retry-After", test.retryAfter)
					w.WriteHeader(test.loginStatus)
					if test.loginStatus == 200 {
						_, _ = io.WriteString(w, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
					} else {
						_, _ = io.WriteString(w, "<html>upstream unavailable</html>")
					}
					return
				}
				salesCalls++
				w.WriteHeader(test.salesStatus)
				if test.salesStatus == 200 {
					_, _ = io.WriteString(w, `{"error":false}`)
				} else {
					_, _ = io.WriteString(w, "<html>sales unavailable</html>")
				}
			}))
			defer server.Close()
			setupBojunKerryLoginTestConfig(t, server.URL)
			circuit := &fakeBojunLoginCircuit{allowed: true}
			service := &BojunOrderPushService{retailOrderDAO: &fakeBojunSyncUpdater{}, logDAO: &fakeDeliveryLogCreator{}, pipelineRunDAO: &fakeBojunPipelineRunRecorder{}, loginCircuit: circuit}
			result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "REPORT-1", BillDate: 20261106})
			if result.Success != test.wantPushSuccess || circuit.failures != test.wantFailure || circuit.successes != test.wantSuccess || circuit.rateLimits != test.wantRateLimit {
				t.Fatalf("result=%+v circuit=%+v", result, circuit)
			}
			if loginCalls != 1 || salesCalls > 1 || (test.loginStatus != 200 && salesCalls != 0) {
				t.Fatalf("calls login=%d sales=%d", loginCalls, salesCalls)
			}
			if test.wantRateLimit > 0 && circuit.rateDelay != 3*time.Minute {
				t.Fatalf("shared cooldown=%s, want 3m", circuit.rateDelay)
			}
		})
	}
}

func TestBojunLoginCircuitReportErrorDoesNotReplaySuccessfulSales(t *testing.T) {
	var salesCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sales" {
			salesCalls++
			_, _ = io.WriteString(w, `{"error":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	circuit := &fakeBojunLoginCircuit{allowed: true, reportErr: errors.New("redis unavailable")}
	service := &BojunOrderPushService{retailOrderDAO: &fakeBojunSyncUpdater{}, logDAO: &fakeDeliveryLogCreator{}, pipelineRunDAO: &fakeBojunPipelineRunRecorder{}, loginCircuit: circuit}
	result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "SUCCESS-1", BillDate: 20261106})
	if !result.Success || result.Error != nil || !result.SyncStatusSaved || salesCalls != 1 || circuit.successes != 1 {
		t.Fatalf("result=%+v sales=%d circuit=%+v", result, salesCalls, circuit)
	}
}

func TestBojunLoginCircuitRedisFailureIsNotDeferral(t *testing.T) {
	cause := errors.New("redis unavailable")
	service := &BojunOrderPushService{retailOrderDAO: &fakeBojunSyncUpdater{}, logDAO: &fakeDeliveryLogCreator{}, pipelineRunDAO: &fakeBojunPipelineRunRecorder{}, loginCircuit: &fakeBojunLoginCircuit{allowErr: cause}}
	result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "REDIS-1", BillDate: 20261106})
	var deferred *bojunLoginDeferredError
	if !errors.Is(result.Error, cause) || errors.As(result.Error, &deferred) {
		t.Fatalf("result=%+v, want real Redis failure", result)
	}
}

func TestBojunLoginCircuitPreservesFailedCooldownWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	cause := errors.New("redis cooldown write failed")
	service := &BojunOrderPushService{retailOrderDAO: &fakeBojunSyncUpdater{}, logDAO: &fakeDeliveryLogCreator{},
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{}, loginCircuit: &fakeBojunLoginCircuit{allowed: true, reportErr: cause}}
	result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "FAILED-STATE-1", BillDate: 20261106})
	if result.Success || !errors.Is(result.Error, cause) || shanghaimall.IsRetryableLoginFailure(result.Error) {
		t.Fatalf("cooldown persistence failure was hidden: %+v", result)
	}
}

func TestBojunAPIPushKeepsExistingBehaviorWithoutRedis(t *testing.T) {
	var salesCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sales" {
			salesCalls++
			_, _ = io.WriteString(w, `{"error":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	service := NewBojunOrderPushService()
	service.retailOrderDAO, service.logDAO, service.pipelineRunDAO = &fakeBojunSyncUpdater{}, &fakeDeliveryLogCreator{}, &fakeBojunPipelineRunRecorder{}
	result := service.PushNewOrder(t.Context(), &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1}, StoreCode: "ABCN001A004", DocNo: "API-1", BillDate: 20261106})
	if !result.Success || result.Error != nil || salesCalls != 1 {
		t.Fatalf("API push acquired an Oracle-only gate: result=%+v sales=%d", result, salesCalls)
	}
}

func TestBojunLoginFailureClassificationRejectsJoinedNonLoginError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	_, err := shanghaimall.PushJialiCheng(t.Context(), shanghaimall.RetailOrder{DocNo: "CLASSIFY-1", SaleTime: "2026-11-06 18:23:00"})
	if !shanghaimall.IsRetryableLoginFailure(err) {
		t.Fatalf("real login failure not classified: %v", err)
	}
	if shanghaimall.IsRetryableLoginFailure(errors.Join(err, errors.New("failed to persist cooldown"))) {
		t.Fatal("mixed operational error classified as pure login failure")
	}
}
