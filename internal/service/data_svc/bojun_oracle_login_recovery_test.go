package data_svc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/shanghaimall"
)

type bojunRecoveryConnection struct {
	*fakeBojunOracleConnection
	afterIDs []uint64
	queryErr error
}

func (connection *bojunRecoveryConnection) QueryBojunRetailAfterID(_ context.Context, afterID uint64, limit int) ([]reportoracle.BojunRetailRow, error) {
	connection.afterIDs = append(connection.afterIDs, afterID)
	if len(connection.afterIDs) > 1 && connection.queryErr != nil {
		return nil, connection.queryErr
	}
	var rows []reportoracle.BojunRetailRow
	for _, row := range connection.rows {
		if row.RetailID > afterID {
			rows = append(rows, row)
			if len(rows) == limit {
				break
			}
		}
	}
	return rows, nil
}

func newBojunRecoveryTestService(count int) (*BojunOracleOrderService, *bojunRecoveryConnection, *fakeBojunOracleStateStore) {
	connection := &bojunRecoveryConnection{fakeBojunOracleConnection: &fakeBojunOracleConnection{maxID: uint64(10 + count)}}
	for i := 0; i < count; i++ {
		connection.rows = append(connection.rows, reportoracle.BojunRetailRow{
			RetailID: uint64(11 + i), DocNo: fmt.Sprintf("RECOVERY-%d", 11+i), StoreCode: "ABCN001A004",
			StatusTime: time.Date(2026, 11, 6, 18, 0, 0, 0, time.Local), IsToShop: "Y",
		})
	}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection.fakeBojunOracleConnection, state)
	service.openOracle = func(context.Context, reportoracle.Config) (bojunOracleConnection, error) { return connection, nil }
	service.batchSize = 2
	return service, connection, state
}

func bojunRecoveryTestLoginError(t *testing.T) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/login" {
			t.Error("login failure unexpectedly sent a sales request")
		}
		writer.Header().Set("Content-Type", "text/html")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(writer, "<html>bad gateway</html>")
	}))
	t.Cleanup(server.Close)
	setupBojunKerryLoginTestConfig(t, server.URL)
	_, err := shanghaimall.PushJialiCheng(t.Context(), shanghaimall.RetailOrder{DocNo: "LOGIN-ERROR", SaleTime: "2026-11-06 18:00:00"})
	if err == nil {
		t.Fatal("HTTP 502 did not produce a login failure")
	}
	return err
}

func TestBojunOracleRetryableLoginFailureContinuesAcrossPagesAndRecovers(t *testing.T) {
	loginErr := bojunRecoveryTestLoginError(t)
	service, connection, state := newBojunRecoveryTestService(3)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	pushCalls := make(map[string]int)
	recovered := false
	service.pushService = bojunPageTestPusher(func(order *model.BojunRetailOrder, _ int, _ OrderPushSkipPolicy) bojunOrderPushResult {
		pushCalls[order.DocNo]++
		if order.DocNo == "RECOVERY-11" && !recovered {
			return bojunOrderPushResult{Error: loginErr}
		}
		return bojunOrderPushResult{Success: true}
	})

	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, loginErr) || result.TotalCount != 3 || result.FailedCount != 1 || result.WatermarkAfter != 10 || result.SourceCaughtUp {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !slices.Equal(connection.afterIDs, []uint64{10, 12}) || state.advancedTo != 0 || store.orders["RECOVERY-11"].Synced != 2 || store.orders["RECOVERY-13"].Synced != 1 {
		t.Fatalf("query cursors=%v watermark=%d orders=%+v", connection.afterIDs, state.advancedTo, store.orders)
	}

	recovered = true
	result, err = service.SyncIncremental(t.Context())
	if err != nil || result.WatermarkAfter != 13 || !result.SourceCaughtUp || state.advancedTo != 13 {
		t.Fatalf("recovery result=%+v error=%v state=%+v", result, err, state)
	}
	if pushCalls["RECOVERY-11"] != 2 || pushCalls["RECOVERY-12"] != 1 || pushCalls["RECOVERY-13"] != 1 {
		t.Fatalf("already successful sales were repeated: %v", pushCalls)
	}
}

func TestBojunOracleRetryableLoginFailurePreservesCompletedPageWatermark(t *testing.T) {
	loginErr := bojunRecoveryTestLoginError(t)
	service, connection, state := newBojunRecoveryTestService(5)
	service.pushService = bojunPageTestPusher(func(order *model.BojunRetailOrder, _ int, _ OrderPushSkipPolicy) bojunOrderPushResult {
		if order.DocNo == "RECOVERY-13" {
			return bojunOrderPushResult{Error: loginErr}
		}
		return bojunOrderPushResult{Success: true}
	})
	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, loginErr) || result.TotalCount != 5 || result.WatermarkAfter != 12 || state.advancedTo != 12 || !slices.Equal(connection.afterIDs, []uint64{10, 12, 14}) {
		t.Fatalf("result=%+v error=%v state=%+v cursors=%v", result, err, state, connection.afterIDs)
	}
}

func TestBojunOracleRetryableLoginFailureDoesNotContinueAfterPersistenceFailure(t *testing.T) {
	loginErr := bojunRecoveryTestLoginError(t)
	for _, stage := range []string{"local status", "Oracle write-back"} {
		t.Run(stage, func(t *testing.T) {
			service, connection, state := newBojunRecoveryTestService(3)
			store := &bojunStatusRecordingStore{fakeBojunOracleRetailStore: service.retailOrderDAO.(*fakeBojunOracleRetailStore)}
			service.retailOrderDAO = store
			if stage == "local status" {
				store.failuresLeft = 1
			} else {
				connection.writeBackErr = errors.New("Oracle unavailable")
			}
			service.pushService = &fakeBojunOraclePusher{result: bojunOrderPushResult{Error: loginErr}}
			result, err := service.SyncIncremental(t.Context())
			if !errors.Is(err, loginErr) || result.TotalCount != 1 || state.advancedTo != 0 || store.orders["RECOVERY-12"] != nil {
				t.Fatalf("result=%+v error=%v orders=%+v", result, err, store.orders)
			}
		})
	}
}

func TestBojunOracleRetryableLoginFailureStopsOnInfrastructureError(t *testing.T) {
	loginErr := bojunRecoveryTestLoginError(t)
	for _, stage := range []string{"context", "lease", "query", "later row"} {
		t.Run(stage, func(t *testing.T) {
			service, connection, state := newBojunRecoveryTestService(3)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("later row unavailable")
			pushCalls := 0
			service.pushService = bojunPageTestPusher(func(_ *model.BojunRetailOrder, _ int, _ OrderPushSkipPolicy) bojunOrderPushResult {
				pushCalls++
				if pushCalls == 1 {
					switch stage {
					case "context":
						failure = context.Canceled
						cancel()
					case "lease":
						failure = data_dao.ErrBojunOracleSyncLeaseLost
						state.renewErr = failure
						service.now = func() time.Time { return time.Now().Add(service.leaseTTL) }
					case "query":
						connection.queryErr = failure
					}
					return bojunOrderPushResult{Error: loginErr}
				}
				if stage == "later row" {
					return bojunOrderPushResult{Error: failure}
				}
				return bojunOrderPushResult{Success: true}
			})
			result, err := service.SyncIncremental(ctx)
			if !errors.Is(err, loginErr) || !errors.Is(err, failure) || state.advancedTo != 0 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			wantCalls := 2
			if stage == "context" || stage == "lease" {
				wantCalls = 1
			}
			if pushCalls != wantCalls {
				t.Fatalf("push calls=%d want=%d", pushCalls, wantCalls)
			}
		})
	}
}

func TestBojunOracleRetryableLoginFailureHonorsPageLimit(t *testing.T) {
	loginErr := bojunRecoveryTestLoginError(t)
	service, connection, _ := newBojunRecoveryTestService(5)
	service.maxPages = 2
	service.pushService = &fakeBojunOraclePusher{result: bojunOrderPushResult{Error: loginErr}}
	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, loginErr) || result.TotalCount != 4 || result.FailedCount != 4 || result.FetchPages != 2 || result.WatermarkAfter != 10 || len(connection.afterIDs) != 2 {
		t.Fatalf("result=%+v error=%v queries=%v", result, err, connection.afterIDs)
	}
}

func TestBojunOracleDeferredLoginKeepsStatusesPending(t *testing.T) {
	service, connection, state := newBojunRecoveryTestService(3)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	existing, err := buildBojunRetailOrderFromOracle(connection.rows[0])
	if err != nil {
		t.Fatal(err)
	}
	existing.ID, existing.Synced = 11, 2
	store.orders[existing.DocNo] = existing
	deferred := &bojunLoginDeferredError{retryAfter: time.Minute}
	service.pushService = bojunPageTestPusher(func(order *model.BojunRetailOrder, _ int, _ OrderPushSkipPolicy) bojunOrderPushResult {
		if order.DocNo != "RECOVERY-13" {
			return bojunOrderPushResult{Error: deferred}
		}
		return bojunOrderPushResult{Success: true}
	})
	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, deferred) || result.TotalCount != 3 || result.FailedCount != 0 || result.WatermarkAfter != 10 || state.advancedTo != 0 {
		t.Fatalf("result=%+v error=%v state=%+v", result, err, state)
	}
	if strings.Contains(err.Error(), "stopped before reaching") || store.orders["RECOVERY-11"].Synced != 2 || store.orders["RECOVERY-12"].Synced != 0 || !slices.Equal(connection.writeBackIDs, []uint64{13}) {
		t.Fatalf("deferred orders were marked failed: orders=%+v writebacks=%v error=%v", store.orders, connection.writeBackIDs, err)
	}
}

func TestBojunOracleMixedDeferredAndStorageErrorStopsPage(t *testing.T) {
	service, _, state := newBojunRecoveryTestService(3)
	storageErr := errors.New("storage unavailable")
	deferred := &bojunLoginDeferredError{retryAfter: time.Minute}
	service.pushService = &fakeBojunOraclePusher{result: bojunOrderPushResult{Error: errors.Join(deferred, storageErr)}}
	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, storageErr) || result.TotalCount != 1 || state.advancedTo != 0 {
		t.Fatalf("result=%+v error=%v state=%+v", result, err, state)
	}
}

type bojunRecoveryGate struct{ open bool }

func (gate *bojunRecoveryGate) Allow(context.Context) (bool, time.Duration, error) {
	return !gate.open, time.Minute, nil
}

func (gate *bojunRecoveryGate) ReportSuccess(context.Context) error {
	gate.open = false
	return nil
}

func (gate *bojunRecoveryGate) ReportFailure(context.Context) error {
	gate.open = true
	return nil
}

func (gate *bojunRecoveryGate) ReportRateLimited(ctx context.Context, _ time.Duration) error {
	return gate.ReportFailure(ctx)
}

func TestBojunOracleHTTPLoginCooldownContinuesOtherStoreAcrossPages(t *testing.T) {
	var recovered atomic.Bool
	var loginCalls, kerrySales, henglongSales atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			loginCalls.Add(1)
			if !recovered.Load() {
				writer.Header().Set("Content-Type", "text/html")
				writer.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(writer, "<html>bad gateway</html>")
				return
			}
			_, _ = io.WriteString(writer, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
		case "/sales":
			kerrySales.Add(1)
			_, _ = io.WriteString(writer, `{"error":false}`)
		case "/henglong":
			henglongSales.Add(1)
			_, _ = io.WriteString(writer, "<responsecode>0</responsecode>")
		default:
			t.Errorf("unexpected HTTP endpoint %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	setupBojunHenglongContextTestConfig(t, server.URL+"/henglong")
	service, connection, state := newBojunRecoveryTestService(3)
	connection.rows[2].StoreCode = "ABCN002A001"
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	gate := &bojunRecoveryGate{}
	logs := &fakeDeliveryLogCreator{}
	service.pushService = &BojunOrderPushService{
		retailOrderDAO: store, logDAO: logs, pipelineRunDAO: &fakeBojunPipelineRunRecorder{}, loginCircuit: gate,
	}

	result, err := service.SyncIncremental(t.Context())
	if err == nil || result.TotalCount != 3 || result.FailedCount != 1 || result.WatermarkAfter != 10 || !slices.Equal(connection.afterIDs, []uint64{10, 12}) {
		t.Fatalf("first result=%+v error=%v cursors=%v", result, err, connection.afterIDs)
	}
	if loginCalls.Load() != 1 || kerrySales.Load() != 0 || henglongSales.Load() != 1 || len(logs.logs) != 2 || store.orders["RECOVERY-12"].Synced != 0 {
		t.Fatalf("login=%d Kerry=%d Henglong=%d logs=%d deferred=%+v", loginCalls.Load(), kerrySales.Load(), henglongSales.Load(), len(logs.logs), store.orders["RECOVERY-12"])
	}

	result, err = service.SyncIncremental(t.Context())
	if err == nil || result.FailedCount != 0 || result.TotalCount != 3 || result.WatermarkAfter != 10 || loginCalls.Load() != 1 || len(logs.logs) != 2 {
		t.Fatalf("cooldown result=%+v error=%v login=%d logs=%d", result, err, loginCalls.Load(), len(logs.logs))
	}

	recovered.Store(true)
	gate.open = false
	result, err = service.SyncIncremental(t.Context())
	if err != nil || !result.SourceCaughtUp || result.WatermarkAfter != 13 || state.advancedTo != 13 {
		t.Fatalf("recovery result=%+v error=%v state=%+v", result, err, state)
	}
	if loginCalls.Load() != 3 || kerrySales.Load() != 2 || henglongSales.Load() != 1 || len(logs.logs) != 4 {
		t.Fatalf("recovered requests login=%d Kerry=%d Henglong=%d logs=%d", loginCalls.Load(), kerrySales.Load(), henglongSales.Load(), len(logs.logs))
	}
}

func TestBojunOracleHistoryPushDoesNotRequireIncrementalLoginCircuit(t *testing.T) {
	var salesCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sales" {
			salesCalls.Add(1)
			_, _ = io.WriteString(writer, `{"error":false}`)
			return
		}
		_, _ = io.WriteString(writer, `{"content":{"status":"SUCCESS","loginSecretId":"test-token"}}`)
	}))
	defer server.Close()
	setupBojunKerryLoginTestConfig(t, server.URL)
	service, _, _ := newBojunRecoveryTestService(1)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	configured := NewBojunOracleOrderService()
	pusher := configured.pushService.(*BojunOrderPushService)
	pusher.retailOrderDAO = store
	pusher.logDAO = &fakeDeliveryLogCreator{}
	pusher.pipelineRunDAO = &fakeBojunPipelineRunRecorder{}
	service.pushService = pusher

	result, err := service.SyncByStatusTime(t.Context(), "2026-11-06 17:00:00", "2026-11-06 19:00:00")
	if err != nil || result.FailedCount != 0 || salesCalls.Load() != 1 || store.orders["RECOVERY-11"].Synced != 1 {
		t.Fatalf("history push incorrectly acquired the incremental gate: result=%+v error=%v sales=%d", result, err, salesCalls.Load())
	}
}

func TestBojunOracleLoginCircuitAppliesOnlyToIncrementalSync(t *testing.T) {
	service, connection, state := newBojunRecoveryTestService(1)
	deferred := &bojunLoginDeferredError{retryAfter: time.Minute}
	protectedPusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Error: deferred}}
	historyPusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = historyPusher
	service.incrementalPushService = protectedPusher
	result, err := service.SyncIncremental(t.Context())
	if !errors.Is(err, deferred) || protectedPusher.calls != 1 || historyPusher.calls != 0 || state.advancedTo != 0 {
		t.Fatalf("incremental result=%+v error=%v protected=%d history=%d", result, err, protectedPusher.calls, historyPusher.calls)
	}
	connection.rows[0].DocNo = "HISTORY-12"
	connection.rows[0].RetailID = 12
	result, err = service.SyncByStatusTime(t.Context(), "2026-11-06 17:00:00", "2026-11-06 19:00:00")
	if err != nil || historyPusher.calls != 1 || protectedPusher.calls != 1 || service.pushService != historyPusher {
		t.Fatalf("history result=%+v error=%v protected=%d history=%d", result, err, protectedPusher.calls, historyPusher.calls)
	}
}
