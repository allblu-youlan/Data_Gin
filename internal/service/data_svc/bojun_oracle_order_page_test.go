package data_svc

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/orderpush"
)

func TestBojunOraclePagePrefetchQueryCounts(t *testing.T) {
	for _, mode := range []string{"incremental", "detail backfill"} {
		for _, existingCount := range []int{100, 50, 0} {
			t.Run(fmt.Sprintf("%s/existing_%d", mode, existingCount), func(t *testing.T) {
				rows := make([]reportoracle.BojunRetailRow, 100)
				connection := &fakeBojunOracleConnection{maxID: 110}
				state := &fakeBojunOracleStateStore{
					state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
					leaseAcquired: true,
				}
				service := newTestBojunOracleOrderService(connection, state)
				service.maxPages = 1
				store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
				for i := range rows {
					rows[i] = reportoracle.BojunRetailRow{
						RetailID: uint64(11 + i), DocNo: fmt.Sprintf("SALE-%03d", i),
						StatusTime: time.Date(2026, 11, 5, 10, 30, 0, 0, time.Local), IsToShop: "N",
					}
					if i < existingCount {
						order, err := buildBojunRetailOrderFromOracle(rows[i])
						if err != nil {
							t.Fatal(err)
						}
						order.ID = uint(i + 1)
						store.orders[order.DocNo] = order
					}
				}
				connection.rows = rows

				var result *BojunOrderSyncResult
				var err error
				if mode == "incremental" {
					result, err = service.SyncIncremental(t.Context())
				} else {
					result, err = service.SyncByStatusTime(t.Context(), "2026-11-05 10:00:00", "2026-11-05 11:00:00")
				}
				if err != nil {
					t.Fatal(err)
				}
				if store.batchFindCalls != 1 || store.findCalls != 100-existingCount {
					t.Fatalf("batch reads=%d single reads=%d, want 1 and %d", store.batchFindCalls, store.findCalls, 100-existingCount)
				}
				if result.TotalCount != 100 || result.ExistingCount != existingCount || result.RetailCount != 100-existingCount ||
					result.SavedCount != 100-existingCount || result.FailedCount != 0 {
					t.Fatalf("result=%+v", result)
				}
			})
		}
	}
}

func TestBojunOraclePagePrefetchDuplicateObservesSuccessfulPush(t *testing.T) {
	row := reportoracle.BojunRetailRow{
		RetailID: 11, DocNo: "SALE-11", StatusTime: time.Date(2026, 11, 5, 10, 30, 0, 0, time.Local), IsToShop: "Y",
	}
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{row, row}, maxID: 11}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	order, err := buildBojunRetailOrderFromOracle(row)
	if err != nil {
		t.Fatal(err)
	}
	order.ID = 11
	store.orders[order.DocNo] = order
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher

	result, err := service.SyncIncremental(t.Context())
	if err != nil || result.WatermarkAfter != 11 || result.ExistingCount != 2 || pusher.calls != 1 ||
		len(connection.writeBackIDs) != 2 || store.batchFindCalls != 1 || store.findCalls != 1 {
		t.Fatalf("result=%+v error=%v pushes=%d write-backs=%v batch=%d finds=%d", result, err, pusher.calls, connection.writeBackIDs, store.batchFindCalls, store.findCalls)
	}
}

func TestBojunOraclePagePrefetchDetailDuplicateObservesBusinessUpdate(t *testing.T) {
	row := reportoracle.BojunRetailRow{
		RetailID: 11, DocNo: "SALE-11", StoreName: "updated", PaidAmount: 1,
		StatusTime: time.Date(2026, 11, 5, 10, 30, 0, 0, time.Local), IsToShop: "N",
	}
	second := row
	second.StoreName = ""
	second.PaidAmount = 2
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{row, second}}
	service := newTestBojunOracleOrderService(connection, &fakeBojunOracleStateStore{})
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders[row.DocNo] = &model.BojunRetailOrder{DocNo: row.DocNo, StoreName: "old"}

	result, err := service.SyncByStatusTime(t.Context(), "2026-11-05 10:00:00", "2026-11-05 11:00:00")
	order := store.orders[row.DocNo]
	if err != nil || result.UpdatedCount != 2 || order.StoreName != "updated" || order.PaidAmount != 2 ||
		store.batchFindCalls != 1 || store.findCalls != 1 {
		t.Fatalf("result=%+v error=%v order=%+v batch=%d finds=%d", result, err, order, store.batchFindCalls, store.findCalls)
	}
}

func TestBojunOraclePagePrefetchFailuresStopBeforeWatermarkAdvance(t *testing.T) {
	queryFailure := errors.New("batch query failed")
	pushFailure := errors.New("push failed")
	for _, kind := range []string{"batch query", "push"} {
		t.Run(kind, func(t *testing.T) {
			connection := &fakeBojunOracleConnection{maxID: 12, rows: []reportoracle.BojunRetailRow{
				{RetailID: 11, DocNo: "SALE-11", StatusTime: time.Date(2026, 11, 5, 10, 30, 0, 0, time.Local), IsToShop: "Y"},
				{RetailID: 12, DocNo: "SALE-12", StatusTime: time.Date(2026, 11, 5, 10, 31, 0, 0, time.Local), IsToShop: "Y"},
			}}
			state := &fakeBojunOracleStateStore{
				state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
				leaseAcquired: true,
			}
			service := newTestBojunOracleOrderService(connection, state)
			store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
			pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Error: pushFailure}}
			service.pushService = pusher
			wantErr := pushFailure
			wantWrites := 1
			if kind == "batch query" {
				store.batchFindErr = queryFailure
				wantErr = queryFailure
				wantWrites = 0
			}

			result, err := service.SyncIncremental(t.Context())
			if !errors.Is(err, wantErr) || result.TotalCount != 1 || result.FailedCount != 1 || result.WatermarkAfter != 10 || state.advancedTo != 0 {
				t.Fatalf("result=%+v error=%v state=%+v", result, err, state)
			}
			if pusher.calls != wantWrites || service.rawDataDAO.(*fakeBojunRawDataCreator).created != wantWrites ||
				store.orders["SALE-12"] != nil || store.batchFindCalls != 1 {
				t.Fatalf("pushes=%d store=%+v, want %d writes before stopping", pusher.calls, store, wantWrites)
			}
		})
	}
}

func TestBojunOraclePagePrefetchChecksLeaseBeforePush(t *testing.T) {
	for _, elapsed := range []time.Duration{110 * time.Second, 120 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			startedAt := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
			row := reportoracle.BojunRetailRow{RetailID: 11, DocNo: "SALE-11", StatusTime: startedAt, IsToShop: "Y"}
			connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{row}, maxID: 11}
			state := &fakeBojunOracleStateStore{
				state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
				leaseAcquired: true, checkExpiry: true,
			}
			service := newTestBojunOracleOrderService(connection, state)
			store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
			order, err := buildBojunRetailOrderFromOracle(row)
			if err != nil {
				t.Fatal(err)
			}
			order.ID = 11
			store.orders[row.DocNo] = order
			now := startedAt
			service.now = func() time.Time { return now }
			store.afterBatchRead = func() { now = startedAt.Add(elapsed) }
			pusher := &fakeBojunOraclePusher{
				result: bojunOrderPushResult{Success: true},
				beforePush: func() {
					if state.renewCalls != 1 || !state.leaseExpires.Equal(now.Add(service.leaseTTL)) {
						t.Fatalf("push preceded renewal after page lookup, state=%+v", state)
					}
				},
			}
			service.pushService = pusher

			result, err := service.SyncIncremental(t.Context())
			if elapsed < service.leaseTTL {
				if err != nil || result.WatermarkAfter != 11 || pusher.calls != 1 {
					t.Fatalf("result=%+v error=%v pushes=%d", result, err, pusher.calls)
				}
			} else if !errors.Is(err, data_dao.ErrBojunOracleSyncLeaseLost) || result.TotalCount != 0 ||
				result.WatermarkAfter != 10 || pusher.calls != 0 || len(connection.writeBackIDs) != 0 || state.advancedTo != 0 {
				t.Fatalf("result=%+v error=%v pushes=%d state=%+v", result, err, pusher.calls, state)
			}
			if store.batchFindCalls != 1 || store.findCalls != 0 {
				t.Fatalf("batch reads=%d single reads=%d", store.batchFindCalls, store.findCalls)
			}
		})
	}
}

func TestBojunOraclePagePrefetchPreservesPushPositionsAcrossPages(t *testing.T) {
	rows := make([]reportoracle.BojunRetailRow, 3)
	for i := range rows {
		rows[i] = reportoracle.BojunRetailRow{
			RetailID: uint64(11 + i), DocNo: fmt.Sprintf("SALE-%d", i), StoreCode: "ABCN001P012",
			StatusTime: time.Date(2026, 11, 5, 10, 30, 0, 0, time.Local), IsToShop: "Y",
		}
	}
	connection := &fakeBojunOracleConnection{pages: [][]reportoracle.BojunRetailRow{rows[:2], rows[2:]}, maxID: 13}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	service.skipPolicy = bojunPageTestSkipConfig{config: OrderPushSkipConfig{Targets: []orderpush.TargetSkipPolicy{
		{TargetCode: "qiantan", Cycle: 2, Skip: 1},
	}}}
	var positions []int
	var skipped []bool
	service.pushService = bojunPageTestPusher(func(_ *model.BojunRetailOrder, position int, policy OrderPushSkipPolicy) bojunOrderPushResult {
		positions = append(positions, position)
		skipped = append(skipped, policy.ShouldSkip(position))
		return bojunOrderPushResult{Success: true, Skipped: policy.ShouldSkip(position)}
	})

	result, err := service.SyncIncremental(t.Context())
	if err != nil || result.WatermarkAfter != 13 || result.RetailCount != 3 || result.FetchPages != 2 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !reflect.DeepEqual(positions, []int{1, 2, 3}) || !reflect.DeepEqual(skipped, []bool{false, true, false}) {
		t.Fatalf("positions=%v skipped=%v", positions, skipped)
	}
	if store := service.retailOrderDAO.(*fakeBojunOracleRetailStore); store.batchFindCalls != 2 || store.findCalls != 3 {
		t.Fatalf("batch reads=%d single reads=%d", store.batchFindCalls, store.findCalls)
	}
}
