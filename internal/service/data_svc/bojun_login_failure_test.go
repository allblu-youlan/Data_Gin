package data_svc

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
)

func TestBojunOracleKerryLoginFailurePreservesStatusAndWatermark(t *testing.T) {
	var loginCalls, salesCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/login" {
			salesCalls.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		loginCalls.Add(1)
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read login request: %v", err)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(writer, "<html>upstream-proxy-secret loginSecretId=returned-token-secret "+string(requestBody)+"</html>")
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)

	const failedDocNo = "KERRY-LOGIN-11"
	statusTime := time.Date(2026, 11, 6, 16, 43, 28, 0, time.Local)
	connection := &fakeBojunOracleConnection{maxID: 12, rows: []reportoracle.BojunRetailRow{
		{RetailID: 11, DocNo: failedDocNo, StoreCode: "ABCN001A004", StatusTime: statusTime, PaidAmount: 88, PushAmount: 88, IsToShop: "Y"},
		{RetailID: 12, DocNo: "KERRY-NEXT-12", StoreCode: "ABCN001A004", StatusTime: statusTime, PaidAmount: 99, PushAmount: 99, IsToShop: "N"},
	}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	logs := &fakeDeliveryLogCreator{}
	runs := &fakeBojunPipelineRunRecorder{}
	service.pushService = &BojunOrderPushService{retailOrderDAO: store, logDAO: logs, pipelineRunDAO: runs}

	result, err := service.SyncIncremental(t.Context())
	if err == nil {
		t.Fatal("login HTTP 502 unexpectedly completed the sync")
	}
	for _, detail := range []string{"kerry login", "http_status=502"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("sync error %q is missing %q", err, detail)
		}
	}
	if loginCalls.Load() != 1 || salesCalls.Load() != 0 {
		t.Errorf("HTTP calls login=%d sales=%d, want 1 and 0", loginCalls.Load(), salesCalls.Load())
	}
	if result.FailedCount != 1 || result.TotalCount != 2 || result.WatermarkAfter != 10 || state.advancedTo != 0 || store.orders["KERRY-NEXT-12"] == nil {
		t.Errorf("failed login advanced past the order: result=%+v watermark=%d", result, state.advancedTo)
	}
	order := store.orders[failedDocNo]
	if order == nil || order.Synced != 2 || !slices.Equal(connection.writeBackOK, []bool{false}) {
		t.Errorf("failed login status order=%+v Oracle statuses=%v", order, connection.writeBackOK)
	}
	if runs.finishStatus != "failed" || len(logs.logs) != 1 {
		t.Fatalf("pipeline status=%q delivery logs=%d", runs.finishStatus, len(logs.logs))
	}
	log := logs.logs[0]
	if log.HTTPStatus != http.StatusBadGateway || log.Success || log.DestinationCode != "jialicheng" || log.BusinessKey != failedDocNo {
		t.Errorf("login failure delivery metadata=%+v", log)
	}
	for _, detail := range []string{"login", "502"} {
		if !strings.Contains(log.ResponseBody, detail) || !strings.Contains(log.ErrorMessage, detail) {
			t.Errorf("delivery log lost login diagnostics: response=%q error=%q", log.ResponseBody, log.ErrorMessage)
		}
	}
	serializedLog, marshalErr := json.Marshal(log)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	logged := string(serializedLog) + err.Error() + runs.finishError
	for _, secret := range []string{
		"activation-test-secret", "device-test-secret", "location-test-secret", "program-test-secret",
		"url-query-secret", "returned-token-secret", "upstream-proxy-secret", "<html>",
	} {
		if strings.Contains(logged, secret) {
			t.Errorf("login failure diagnostics leaked %q", secret)
		}
	}
}

func setupBojunKerryLoginTestConfig(t *testing.T, endpoint string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("ShanghaiMall:\n  TimeoutSeconds: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
	for key, value := range map[string]string{
		"LOGIN_URL":    endpoint + "/login?token=url-query-secret",
		"SALES_URL":    endpoint + "/sales",
		"PROGRAM_NAME": "program-test-secret", "DEVICE_ID": "device-test-secret",
		"ACTIVATION_CODE": "activation-test-secret", "LOCATION_CODE": "location-test-secret",
		"ITEM_CODE": "item", "CASHIER": "cashier", "STORE_CODE": "store", "TILL_ID": "till", "STAFF_CODE": "staff",
	} {
		t.Setenv("SHANGHAI_JIALICHENG_KERRY_"+key, value)
	}
	t.Setenv("SHANGHAI_MALL_TIMEOUT_SECONDS", "2")
}
