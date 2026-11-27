package data_svc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"gin-biz-web-api/pkg/providerhttp"
)

func TestBojunOracleQiantanLoginTimeoutDefersOnlyQiantanAndRecovers(t *testing.T) {
	var recovered atomic.Bool
	var qiantanLogins, qiantanSales, kerryLogins, kerrySales atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/qiantan/login":
			qiantanLogins.Add(1)
			if !recovered.Load() {
				<-r.Context().Done()
				return
			}
			_, _ = io.WriteString(w, `{"token":"test-token"}`)
		case "/qiantan/sales":
			qiantanSales.Add(1)
			_, _ = io.WriteString(w, `{"result":"1"}`)
		case "/login":
			kerryLogins.Add(1)
			_, _ = io.WriteString(w, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
		case "/sales":
			kerrySales.Add(1)
			_, _ = io.WriteString(w, `{"error":false}`)
		default:
			t.Errorf("unexpected HTTP endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	t.Setenv("SHANGHAI_MALL_TIMEOUT_SECONDS", "1")
	t.Setenv("SHANGHAI_QIANTAN_XIAN_TOKEN_URL", server.URL+"/qiantan/login")
	t.Setenv("SHANGHAI_QIANTAN_XIAN_POST_URL", server.URL+"/qiantan/sales")
	t.Setenv("SHANGHAI_QIANTAN_XIAN_ACCOUNT", "test-account")
	t.Setenv("SHANGHAI_QIANTAN_XIAN_PASSWORD", "test-password")
	service, connection, state := newBojunRecoveryTestService(3)
	connection.rows[0].StoreCode = "ABCN001P012"
	connection.rows[1].StoreCode = "ABCN001P012"
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	gate := &bojunRecoveryGate{}
	logs := &fakeDeliveryLogCreator{}
	service.incrementalPushService = &BojunOrderPushService{
		retailOrderDAO: store, logDAO: logs, pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
		loginCircuits: map[string]providerhttp.ProviderCircuitBreaker{"qiantan": gate, "jialicheng": &bojunRecoveryGate{}},
	}

	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) || result.TotalCount != 3 || result.FailedCount != 1 || result.WatermarkAfter != 10 || state.advancedTo != 0 {
		t.Fatalf("first result=%+v error=%v watermark=%d", result, err, state.advancedTo)
	}
	if qiantanLogins.Load() != 1 || qiantanSales.Load() != 0 || kerryLogins.Load() != 1 || kerrySales.Load() != 1 || len(logs.logs) != 2 {
		t.Fatalf("requests: Qiantan login=%d sales=%d Kerry login=%d sales=%d logs=%d", qiantanLogins.Load(), qiantanSales.Load(), kerryLogins.Load(), kerrySales.Load(), len(logs.logs))
	}
	if store.orders["RECOVERY-11"].Synced != 2 || store.orders["RECOVERY-12"].Synced != 0 || store.orders["RECOVERY-13"].Synced != 1 || slices.Contains(connection.writeBackIDs, uint64(12)) {
		t.Fatalf("unexpected order state: failed=%d deferred=%d successful=%d writebacks=%v", store.orders["RECOVERY-11"].Synced, store.orders["RECOVERY-12"].Synced, store.orders["RECOVERY-13"].Synced, connection.writeBackIDs)
	}

	result, err = service.SyncIncremental(t.Context())
	if err == nil || !strings.Contains(err.Error(), "target=qiantan") || result.FailedCount != 0 || result.WatermarkAfter != 10 || state.advancedTo != 0 {
		t.Fatalf("cooldown result=%+v error=%v watermark=%d", result, err, state.advancedTo)
	}
	if qiantanLogins.Load() != 1 || len(logs.logs) != 2 || store.orders["RECOVERY-11"].Synced != 2 || store.orders["RECOVERY-12"].Synced != 0 {
		t.Fatalf("cooldown performed new work: login=%d logs=%d failed=%d deferred=%d", qiantanLogins.Load(), len(logs.logs), store.orders["RECOVERY-11"].Synced, store.orders["RECOVERY-12"].Synced)
	}

	recovered.Store(true)
	gate.open = false
	result, err = service.SyncIncremental(t.Context())
	if err != nil || !result.SourceCaughtUp || result.WatermarkAfter != 13 || state.advancedTo != 13 || store.orders["RECOVERY-11"].Synced != 1 || store.orders["RECOVERY-12"].Synced != 1 {
		t.Fatalf("recovery result=%+v error=%v watermark=%d", result, err, state.advancedTo)
	}
	if qiantanLogins.Load() != 3 || qiantanSales.Load() != 2 || kerryLogins.Load() != 1 || kerrySales.Load() != 1 || len(logs.logs) != 4 {
		t.Fatalf("recovered requests: Qiantan login=%d sales=%d Kerry login=%d sales=%d logs=%d", qiantanLogins.Load(), qiantanSales.Load(), kerryLogins.Load(), kerrySales.Load(), len(logs.logs))
	}
}
