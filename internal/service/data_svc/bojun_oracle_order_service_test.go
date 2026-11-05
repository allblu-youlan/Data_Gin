package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

type fakeBojunOracleDatasourceStore struct {
	datasource model.ReportDatasource
}

func (store *fakeBojunOracleDatasourceStore) FindEnabledReportDatasourceByCode(_ context.Context, code string) (*model.ReportDatasource, error) {
	if code != bojunOracleDatasourceCode {
		return nil, errors.New("unexpected datasource code")
	}
	datasource := store.datasource
	return &datasource, nil
}

type fakeBojunOracleDecryptor struct{}

func (fakeBojunOracleDecryptor) Decrypt(string, string) (string, error) { return "password", nil }

type fakeBojunOracleConnection struct {
	rows          []reportoracle.BojunRetailRow
	pages         [][]reportoracle.BojunRetailRow
	maxID         uint64
	queryAfter    uint64
	queryCalls    int
	closed        int
	writeBackIDs  []uint64
	writeBackOK   []bool
	writeBackDate []int
	writeBackErr  error
	statusStart   time.Time
	statusEnd     time.Time
	statusAfter   uint64
	statusCalls   int
}

func (connection *fakeBojunOracleConnection) QueryBojunRetailAfterID(_ context.Context, afterID uint64, _ int) ([]reportoracle.BojunRetailRow, error) {
	connection.queryAfter = afterID
	connection.queryCalls++
	if connection.pages != nil {
		return append([]reportoracle.BojunRetailRow(nil), connection.pages[connection.queryCalls-1]...), nil
	}
	return append([]reportoracle.BojunRetailRow(nil), connection.rows...), nil
}

func (connection *fakeBojunOracleConnection) QueryBojunRetailByStatusTime(_ context.Context, start, end time.Time, afterID uint64, _ int) ([]reportoracle.BojunRetailRow, error) {
	connection.statusStart = start
	connection.statusEnd = end
	connection.statusAfter = afterID
	connection.statusCalls++
	return append([]reportoracle.BojunRetailRow(nil), connection.rows...), nil
}

func (connection *fakeBojunOracleConnection) MaxBojunRetailID(context.Context) (uint64, error) {
	return connection.maxID, nil
}

func (connection *fakeBojunOracleConnection) UpdateBojunRetailPushStatus(_ context.Context, retailID uint64, success bool, pushDate int) error {
	connection.writeBackIDs = append(connection.writeBackIDs, retailID)
	connection.writeBackOK = append(connection.writeBackOK, success)
	connection.writeBackDate = append(connection.writeBackDate, pushDate)
	return connection.writeBackErr
}

func (connection *fakeBojunOracleConnection) Close() error {
	connection.closed++
	return nil
}

type fakeBojunOracleStateStore struct {
	state         model.BojunOracleSyncState
	getErr        error
	initializedAt uint64
	leaseAcquired bool
	advancedFrom  uint64
	advancedTo    uint64
	renewCalls    int
	releaseCalls  int
	renewTimes    []time.Time
	renewErr      error
	checkExpiry   bool
	leaseExpires  time.Time
}

type fakeBojunOracleRetailStore struct {
	orders          map[string]*model.BojunRetailOrder
	nextID          uint
	updates         map[uint]int
	supplementCalls int
	supplementNoop  bool
	backfillUpdates int
	findCalls       int
	existsCalls     int
	findErr         error
}

func (store *fakeBojunOracleRetailStore) ExistsByDocNo(_ context.Context, docNo string) (bool, error) {
	store.existsCalls++
	if store.findErr != nil {
		return false, store.findErr
	}
	_, exists := store.orders[docNo]
	return exists, nil
}

func (store *fakeBojunOracleRetailStore) FindByDocNo(_ context.Context, docNo string) (*model.BojunRetailOrder, error) {
	store.findCalls++
	if store.findErr != nil {
		return nil, store.findErr
	}
	order, exists := store.orders[docNo]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	copyOrder := *order
	return &copyOrder, nil
}

func (store *fakeBojunOracleRetailStore) CreateIfNotExists(_ context.Context, order *model.BojunRetailOrder) (bool, error) {
	if _, exists := store.orders[order.DocNo]; exists {
		return false, nil
	}
	if store.nextID == 0 {
		store.nextID = 101
	}
	order.ID = store.nextID
	copyOrder := *order
	store.orders[order.DocNo] = &copyOrder
	return true, nil
}

func (store *fakeBojunOracleRetailStore) SupplementOracleFieldsIfMissing(
	_ context.Context,
	localOrderID uint,
	order *model.BojunRetailOrder,
) (bool, error) {
	store.supplementCalls++
	if store.supplementNoop {
		return false, nil
	}
	existing, exists := store.orders[order.DocNo]
	if !exists || existing.ID != localOrderID || existing.OracleRetailID != nil {
		return false, nil
	}
	applyBojunOracleSupplement(existing, order)
	return true, nil
}

func (store *fakeBojunOracleRetailStore) UpdateSyncStatus(_ context.Context, id uint, synced int) error {
	if store.updates == nil {
		store.updates = make(map[uint]int)
	}
	store.updates[id] = synced
	for _, order := range store.orders {
		if order.ID == id {
			order.Synced = synced
		}
	}
	return nil
}

func (store *fakeBojunOracleRetailStore) UpdateOracleBusinessFields(
	_ context.Context,
	incoming *model.BojunRetailOrder,
) error {
	order, exists := store.orders[incoming.DocNo]
	if !exists {
		return nil
	}
	store.backfillUpdates++
	applyBojunOracleBusinessFields(order, incoming)
	return nil
}

type fakeBojunOraclePusher struct {
	result     bojunOrderPushResult
	calls      int
	orders     []model.BojunRetailOrder
	beforePush func()
}

func TestPreserveMissingBojunOracleFieldsKeepsExistingValues(t *testing.T) {
	existing := &model.BojunRetailOrder{
		StoreCode: "STORE-1", StoreName: "门店一", RetailBillType: "RET",
		RetailSaleType: "RET", OrderTypeCode: "RET", OrderTypeName: "退货",
		OrderPhone: "18616613488", IsToShop: "Y",
	}
	incoming := &model.BojunRetailOrder{RetailSaleType: "CMR", OrderTypeCode: "CMR", OrderTypeName: "正常零售"}
	preserveMissingBojunOracleFields(incoming, existing)
	if incoming.StoreCode != existing.StoreCode || incoming.StoreName != existing.StoreName ||
		incoming.RetailBillType != existing.RetailBillType || incoming.RetailSaleType != existing.RetailSaleType ||
		incoming.OrderTypeCode != existing.OrderTypeCode || incoming.OrderTypeName != existing.OrderTypeName ||
		incoming.OrderPhone != existing.OrderPhone || incoming.IsToShop != existing.IsToShop {
		t.Fatalf("incoming=%+v", incoming)
	}
}

func TestBojunOracleSupplementPreservesExistingValuesWhenSourceIsEmpty(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 140, DocNo: "API-ORDER-140", StatusTime: statusTime,
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 139, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["API-ORDER-140"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 1400}, DocNo: "API-ORDER-140",
		StoreCode: "STORE-1", StoreName: "门店一", RetailBillType: "RET",
		RetailSaleType: "RET", OrderTypeCode: "RET", OrderTypeName: "退货",
		OrderPhone: "18616613488", IsToShop: "Y", Synced: 0,
	}
	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error=%v", err)
	}
	updated := store.orders["API-ORDER-140"]
	if updated.StoreCode != "STORE-1" || updated.StoreName != "门店一" || updated.RetailBillType != "RET" ||
		updated.RetailSaleType != "RET" || updated.OrderPhone != "18616613488" || updated.IsToShop != "Y" ||
		updated.Synced != 0 || result.UpdatedCount != 1 {
		t.Fatalf("updated=%+v result=%+v", updated, result)
	}
}

func TestBojunOracleSyncFailsBeforeAdvancingPastUnreachedSourceHead(t *testing.T) {
	connection := &fakeBojunOracleConnection{
		maxID: 200,
		rows: []reportoracle.BojunRetailRow{{
			RetailID: 101, DocNo: "SALE-101", StatusTime: time.Date(2026, 8, 25, 10, 0, 0, 0, time.Local),
			ItemsJSON: `[]`, PayItemsJSON: `[]`,
		}},
	}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 100, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 1
	service.maxPages = 1
	result, err := service.SyncIncremental(t.Context())
	if err == nil || result.SourceCaughtUp || result.WindowExhausted || result.SourceHead != 200 || result.WatermarkAfter != 101 {
		t.Fatalf("error=%v result=%+v", err, result)
	}
}

func (pusher *fakeBojunOraclePusher) PushNewOrderWithPolicy(
	_ context.Context,
	order *model.BojunRetailOrder,
	_ int,
	_ OrderPushSkipPolicy,
) bojunOrderPushResult {
	if pusher.beforePush != nil {
		pusher.beforePush()
	}
	pusher.calls++
	pusher.orders = append(pusher.orders, *order)
	return pusher.result
}

func (store *fakeBojunOracleStateStore) Get(context.Context, string) (*model.BojunOracleSyncState, error) {
	if store.getErr != nil {
		return nil, store.getErr
	}
	state := store.state
	return &state, nil
}

func (store *fakeBojunOracleStateStore) Initialize(_ context.Context, _ string, retailID uint64, _ time.Time) (*model.BojunOracleSyncState, bool, error) {
	store.initializedAt = retailID
	store.state = model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: retailID, Initialized: true}
	return &store.state, true, nil
}

func (store *fakeBojunOracleStateStore) AcquireLease(_ context.Context, _, _ string, now time.Time, ttl time.Duration) (*model.BojunOracleSyncState, bool, error) {
	store.leaseExpires = now.Add(ttl)
	state := store.state
	return &state, store.leaseAcquired, nil
}

func (store *fakeBojunOracleStateStore) Advance(_ context.Context, _ string, _ string, expected, next uint64, now time.Time, ttl time.Duration) error {
	if store.checkExpiry && !now.Before(store.leaseExpires) {
		return data_dao.ErrBojunOracleSyncLeaseLost
	}
	store.advancedFrom = expected
	store.advancedTo = next
	store.leaseExpires = now.Add(ttl)
	return nil
}

func (store *fakeBojunOracleStateStore) RenewLease(_ context.Context, _, _ string, now time.Time, ttl time.Duration) error {
	store.renewCalls++
	store.renewTimes = append(store.renewTimes, now)
	if store.renewErr != nil {
		return store.renewErr
	}
	if store.checkExpiry && !now.Before(store.leaseExpires) {
		return data_dao.ErrBojunOracleSyncLeaseLost
	}
	store.leaseExpires = now.Add(ttl)
	return nil
}

func (store *fakeBojunOracleStateStore) ReleaseLease(context.Context, string, string, time.Time) error {
	store.releaseCalls++
	return nil
}

func TestBuildBojunRetailOrderFromOracleMapsConfirmedFields(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	order, err := buildBojunRetailOrderFromOracle(reportoracle.BojunRetailRow{
		RetailID: 45077, StoreCode: "ABCN001A001", StoreName: " 商场一店 ", DocNo: "SALE-45077", RetailSaleType: " ret ",
		StatusTime: statusTime, OrderPhone: "18616613488", PaidAmount: 470.83, PushAmount: 0,
		IsToShop: "Y", ItemsJSON: "",
		PayItemsJSON: `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":470.83}]`,
	})
	if err != nil {
		t.Fatalf("buildBojunRetailOrderFromOracle() error = %v", err)
	}
	if order.OracleRetailID == nil || *order.OracleRetailID != 45077 || order.CompletedAt == nil || !order.CompletedAt.Equal(statusTime) {
		t.Fatalf("Oracle identity/time mapping = %+v", order)
	}
	if order.OrderPhone != "18616613488" || order.PaidAmount != 470.83 || order.PushAmount != 0 {
		t.Fatalf("phone/amount mapping = %+v", order)
	}
	if order.TotalAmtActual != 470.83 || order.TotalAmtList != 470.83 || order.TotalAmtAcc != 470.83 || order.TotalAmtAcc1 != 470.83 {
		t.Fatalf("legacy paid amount fields = %+v", order)
	}
	if order.OrderTypeCode != "RET" || order.TotalQty != -1 || order.BillDate != 20260825 || order.ItemsJSON != "[]" {
		t.Fatalf("type/date/items mapping = %+v", order)
	}
	if order.PayItemsJSON != `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":470.83}]` {
		t.Fatalf("pay items mapping = %s", order.PayItemsJSON)
	}
	if order.RetailBillType != "ret" || order.RetailSaleType != "RET" || order.StoreName != "商场一店" {
		t.Fatalf("retail bill type/store name mapping = %+v", order)
	}
	var rawPayload map[string]interface{}
	if err := json.Unmarshal([]byte(order.RawContentJSON), &rawPayload); err != nil {
		t.Fatalf("unmarshal raw content: %v", err)
	}
	if rawPayload["STORE_NAME"] != " 商场一店 " || rawPayload["RETAILSALETYPE"] != " ret " {
		t.Fatalf("raw payload mapping = %+v", rawPayload)
	}
	if payItems, ok := rawPayload["PAY_ITEMS"].([]interface{}); !ok || len(payItems) != 1 {
		t.Fatalf("raw pay items mapping = %+v", rawPayload["PAY_ITEMS"])
	}
}

func TestBuildBojunRetailOrderFromOracleDefaultsPayItemsToEmptyArray(t *testing.T) {
	order, err := buildBojunRetailOrderFromOracle(reportoracle.BojunRetailRow{
		RetailID: 45077, DocNo: "SALE-45077", StatusTime: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PayItemsJSON != "[]" {
		t.Fatalf("PayItemsJSON = %q, want []", order.PayItemsJSON)
	}
}

func TestBojunOracleIncrementalInitializesAtCurrentMaximumWithoutFetching(t *testing.T) {
	connection := &fakeBojunOracleConnection{maxID: 45094}
	state := &fakeBojunOracleStateStore{getErr: data_dao.ErrBojunOracleSyncStateNotInitialized}
	service := newTestBojunOracleOrderService(connection, state)

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	if !result.WatermarkInitialized || result.WatermarkAfter != 45094 || state.initializedAt != 45094 {
		t.Fatalf("initialization result=%+v state=%+v", result, state)
	}
	if connection.queryCalls != 0 || connection.closed != 1 {
		t.Fatalf("query calls=%d close calls=%d", connection.queryCalls, connection.closed)
	}
}

func TestBojunOracleIncrementalAdvancesAfterPersistingBatch(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 11, StoreCode: "ABCN001A001", DocNo: "SALE-11", StatusTime: statusTime,
		PaidAmount: 88.8, PushAmount: 80, IsToShop: "N",
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	if connection.queryAfter != 10 || state.advancedFrom != 10 || state.advancedTo != 11 || state.renewCalls != 0 || state.releaseCalls != 1 {
		t.Fatalf("query/state = after:%d from:%d to:%d renews:%d releases:%d", connection.queryAfter, state.advancedFrom, state.advancedTo, state.renewCalls, state.releaseCalls)
	}
	if result.RetailCount != 1 || result.WatermarkAfter != 11 || !result.LeaseAcquired {
		t.Fatalf("result = %+v", result)
	}
}

func TestBojunOracleIncrementalRenewsLeaseByElapsedTime(t *testing.T) {
	renewFailure := errors.New("lease renewal failed")
	tests := []struct {
		name          string
		rowCounts     []int
		clockSeconds  []int
		renewSeconds  []int
		renewErr      error
		wantErr       error
		wantWatermark uint64
		wantRows      int
	}{
		{
			name: "fast page", rowCounts: []int{3},
			clockSeconds:  []int{0, 1, 2, 3, 4, 4},
			wantWatermark: 13, wantRows: 3,
		},
		{
			name: "slow page renews each third of ttl", rowCounts: []int{3},
			clockSeconds: []int{0, 40, 60, 80, 81, 81}, renewSeconds: []int{40, 80},
			wantWatermark: 13, wantRows: 3,
		},
		{
			name: "advance refreshes renewal interval", rowCounts: []int{4, 1},
			clockSeconds:  []int{0, 5, 15, 25, 35, 35, 50, 50, 50},
			wantWatermark: 15, wantRows: 5,
		},
		{
			name: "renew failure stops page", rowCounts: []int{3},
			clockSeconds: []int{0, 40, 40}, renewSeconds: []int{40},
			renewErr: renewFailure, wantErr: renewFailure, wantWatermark: 10, wantRows: 0,
		},
		{
			name: "slow query still renews before expiry", rowCounts: []int{1},
			clockSeconds: []int{0, 110, 110, 110}, renewSeconds: []int{110},
			wantWatermark: 11, wantRows: 1,
		},
		{
			name: "expired lease after slow query fails cas", rowCounts: []int{1},
			clockSeconds: []int{0, 120, 120}, renewSeconds: []int{120},
			wantErr: data_dao.ErrBojunOracleSyncLeaseLost, wantWatermark: 10, wantRows: 0,
		},
		{
			name: "expired lease at advance fails cas", rowCounts: []int{1},
			clockSeconds: []int{0, 30, 120, 120},
			wantErr:      data_dao.ErrBojunOracleSyncLeaseLost, wantWatermark: 10, wantRows: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startedAt := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
			connection := &fakeBojunOracleConnection{}
			retailID := uint64(10)
			for _, rowCount := range tt.rowCounts {
				var rows []reportoracle.BojunRetailRow
				for range rowCount {
					retailID++
					rows = append(rows, reportoracle.BojunRetailRow{
						RetailID: retailID, DocNo: "SALE-" + strconv.FormatUint(retailID, 10),
						StatusTime: startedAt, IsToShop: "N",
					})
				}
				connection.pages = append(connection.pages, rows)
			}
			connection.maxID = retailID
			state := &fakeBojunOracleStateStore{
				state: model.BojunOracleSyncState{
					SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true,
				},
				leaseAcquired: true, checkExpiry: true, renewErr: tt.renewErr,
			}
			service := newTestBojunOracleOrderService(connection, state)
			service.batchSize = 4
			clockCalls := 0
			service.now = func() time.Time {
				if clockCalls >= len(tt.clockSeconds) {
					t.Fatalf("unexpected clock call %d", clockCalls+1)
				}
				current := startedAt.Add(time.Duration(tt.clockSeconds[clockCalls]) * time.Second)
				clockCalls++
				return current
			}

			result, err := service.SyncIncremental(t.Context())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error=%v, want %v", err, tt.wantErr)
			}
			var renewSeconds []int
			for _, renewedAt := range state.renewTimes {
				renewSeconds = append(renewSeconds, int(renewedAt.Sub(startedAt)/time.Second))
			}
			if !reflect.DeepEqual(renewSeconds, tt.renewSeconds) {
				t.Fatalf("renewed at seconds %v, want %v", renewSeconds, tt.renewSeconds)
			}
			if result.WatermarkAfter != tt.wantWatermark || result.TotalCount != tt.wantRows || state.releaseCalls != 1 {
				t.Fatalf("result=%+v releases=%d, want watermark=%d rows=%d and one release", result, state.releaseCalls, tt.wantWatermark, tt.wantRows)
			}
			if service.rawDataDAO.(*fakeBojunRawDataCreator).created != tt.wantRows || result.RetailCount != tt.wantRows {
				t.Fatalf("result=%+v, want exactly %d persisted orders", result, tt.wantRows)
			}
		})
	}
}

func TestBojunOracleSlowQueryRenewsLeaseBeforePush(t *testing.T) {
	startedAt := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 11, DocNo: "SALE-11", StatusTime: startedAt, IsToShop: "Y",
	}}}
	state := &fakeBojunOracleStateStore{
		state: model.BojunOracleSyncState{
			SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true,
		},
		leaseAcquired: true, checkExpiry: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	clockCalls := 0
	service.now = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return startedAt
		}
		return startedAt.Add(110 * time.Second)
	}
	pusher := &fakeBojunOraclePusher{
		result: bojunOrderPushResult{Success: true},
		beforePush: func() {
			if state.renewCalls != 1 || !state.leaseExpires.Equal(startedAt.Add(230*time.Second)) {
				t.Fatalf("push started before lease renewal, renews=%d expires=%v", state.renewCalls, state.leaseExpires)
			}
		},
	}
	service.pushService = pusher

	result, err := service.SyncIncremental(t.Context())
	if err != nil || result.WatermarkAfter != 11 || pusher.calls != 1 || len(connection.writeBackIDs) != 1 {
		t.Fatalf("result=%+v error=%v pushes=%d write-backs=%v", result, err, pusher.calls, connection.writeBackIDs)
	}
}

func TestBojunOracleIncrementalWritesSuccessfulPushBackToOracle(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 12, StoreCode: "ABCN001A001", DocNo: "SALE-12", StatusTime: statusTime,
		PaidAmount: 88.8, PushAmount: 0, IsToShop: "Y",
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 11, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	if pusher.calls != 1 || len(connection.writeBackIDs) != 1 || connection.writeBackIDs[0] != 12 || !connection.writeBackOK[0] || connection.writeBackDate[0] != 20260826 {
		t.Fatalf("push/write-back = calls:%d ids:%v status:%v dates:%v", pusher.calls, connection.writeBackIDs, connection.writeBackOK, connection.writeBackDate)
	}
	if result.WatermarkAfter != 12 || store.updates[101] != 1 {
		t.Fatalf("result=%+v local updates=%v", result, store.updates)
	}
	if store.findCalls != 1 || store.existsCalls != 0 {
		t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
	}
}

func TestBojunOracleExistingSuccessfulOrderRetriesOnlyWriteBack(t *testing.T) {
	retailID := uint64(13)
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: retailID, StoreCode: "ABCN001A001", DocNo: "SALE-13", StatusTime: statusTime,
		PaidAmount: 20, PushAmount: 20, IsToShop: "Y", PushStatus: 0,
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 12, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["SALE-13"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 77}, OracleRetailID: &retailID, DocNo: "SALE-13", Synced: 3,
	}

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	if pusher.calls != 0 || len(connection.writeBackIDs) != 1 || connection.writeBackIDs[0] != retailID {
		t.Fatalf("pusher calls=%d write-backs=%v", pusher.calls, connection.writeBackIDs)
	}
	if store.updates[77] != 1 || result.WatermarkAfter != retailID {
		t.Fatalf("updates=%v result=%+v", store.updates, result)
	}
	if store.findCalls != 1 || store.existsCalls != 0 {
		t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
	}
}

func TestBojunOracleExistingAPIOrderReconcilesFieldsAndOnlyWritesBack(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 130, StoreCode: "ORACLE-STORE", StoreName: "Oracle 商场", DocNo: "API-ORDER-130", RetailSaleType: "RET", StatusTime: statusTime,
		OrderPhone: "18616613488", PaidAmount: 88.8, PushAmount: 80, IsToShop: "Y",
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 129, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	completedAt := statusTime.Add(-time.Hour)
	store.orders["API-ORDER-130"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 1300}, DocNo: "API-ORDER-130", StoreCode: "API-STORE",
		StoreName: "API 商场", RetailSaleType: "API-TYPE", CompletedAt: &completedAt,
		ItemsJSON: `[{"sku":"API-SKU"}]`, RawDataID: 99, Synced: 1,
	}

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	updated := store.orders["API-ORDER-130"]
	if store.supplementCalls != 1 || updated.OracleRetailID == nil || *updated.OracleRetailID != 130 {
		t.Fatalf("supplement calls=%d updated=%+v", store.supplementCalls, updated)
	}
	if updated.OrderPhone != "18616613488" || updated.PaidAmount != 88.8 || updated.PushAmount != 80 || updated.IsToShop != "Y" {
		t.Fatalf("supplemented Oracle fields=%+v", updated)
	}
	if updated.RetailBillType != "RET" || updated.StoreName != "Oracle 商场" {
		t.Fatalf("supplemented retail bill type/store name=%+v", updated)
	}
	if updated.TotalAmtList != 88.8 || updated.TotalAmtActual != 88.8 || updated.TotalAmtAcc != 88.8 || updated.TotalAmtAcc1 != 88.8 {
		t.Fatalf("supplemented amount fields=%+v", updated)
	}
	if updated.StoreCode != "ORACLE-STORE" || updated.RetailSaleType != "RET" || updated.RawDataID != 99 || updated.ItemsJSON != `[]` ||
		updated.CompletedAt == nil || !updated.CompletedAt.Equal(statusTime) {
		t.Fatalf("business fields were not reconciled: %+v", updated)
	}
	if pusher.calls != 0 || len(connection.writeBackIDs) != 1 || connection.writeBackIDs[0] != 130 || result.WatermarkAfter != 130 {
		t.Fatalf("push/write-back/result = calls:%d ids:%v result:%+v", pusher.calls, connection.writeBackIDs, result)
	}
	if result.UpdatedCount != 1 {
		t.Fatalf("updated count=%d, want 1", result.UpdatedCount)
	}
}

func TestBojunOracleExistingUnpushedOrderUsesSupplementedPushAmount(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 131, StoreCode: "ABCN001A001", DocNo: "API-ORDER-131", StatusTime: statusTime,
		PaidAmount: 100, PushAmount: 0, IsToShop: "Y",
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 130, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["API-ORDER-131"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 1310}, DocNo: "API-ORDER-131", StoreCode: "ABCN001A001", Synced: 0,
	}

	result, err := service.SyncIncremental(t.Context())
	if err != nil {
		t.Fatalf("SyncIncremental() error = %v", err)
	}
	if pusher.calls != 1 || len(pusher.orders) != 1 || pusher.orders[0].PushAmount != 0 ||
		pusher.orders[0].OracleRetailID == nil || *pusher.orders[0].OracleRetailID != 131 {
		t.Fatalf("pushed orders=%+v calls=%d", pusher.orders, pusher.calls)
	}
	if len(connection.writeBackIDs) != 1 || connection.writeBackIDs[0] != 131 || store.updates[1310] != 1 || result.WatermarkAfter != 131 {
		t.Fatalf("write-back=%v updates=%v result=%+v", connection.writeBackIDs, store.updates, result)
	}
}

func TestBojunOracleSupplementCASNoopDoesNotAdvanceWatermark(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 133, DocNo: "API-ORDER-133", StatusTime: statusTime,
		PaidAmount: 100, PushAmount: 60, IsToShop: "Y",
	}}}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 132, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.supplementNoop = true
	store.orders["API-ORDER-133"] = &model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 1330}, DocNo: "API-ORDER-133"}

	result, err := service.SyncIncremental(t.Context())
	if err == nil {
		t.Fatal("SyncIncremental() unexpectedly succeeded")
	}
	if state.advancedTo != 0 || result.WatermarkAfter != 132 || result.FailedCount != 1 {
		t.Fatalf("watermark advanced after incomplete supplement: state=%+v result=%+v", state, result)
	}
}

func TestBojunOraclePreviewDoesNotSupplementExistingAPIOrder(t *testing.T) {
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 132, DocNo: "API-ORDER-132", StatusTime: time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local),
		ItemsJSON: `[{"no":"SKU-132"}]`, PayItemsJSON: `[]`,
	}}}
	service := newTestBojunOracleOrderService(connection, &fakeBojunOracleStateStore{})
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["API-ORDER-132"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 1320}, DocNo: "API-ORDER-132", ItemsJSON: `[{"no":"OLD"}]`, PayItemsJSON: `[]`,
	}

	result, err := service.PreviewByStatusTime(t.Context(), "2026-08-25T10:00", "2026-08-25T11:00")
	if err != nil {
		t.Fatalf("PreviewByStatusTime() error = %v", err)
	}
	if store.supplementCalls != 0 || store.backfillUpdates != 0 || store.orders["API-ORDER-132"].ItemsJSON != `[{"no":"OLD"}]` || result.PreviewCount != 1 ||
		pusher.calls != 0 || len(connection.writeBackIDs) != 0 {
		t.Fatalf(
			"preview mutated existing order: supplements=%d detail updates=%d order=%+v pushes=%d write-backs=%v result=%+v",
			store.supplementCalls,
			store.backfillUpdates,
			store.orders["API-ORDER-132"],
			pusher.calls,
			connection.writeBackIDs,
			result,
		)
	}
	if store.findCalls != 1 || store.existsCalls != 0 {
		t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
	}
}

func TestBojunOracleDetailBackfillReconcilesBusinessFields(t *testing.T) {
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID:   132,
		DocNo:      "SALE-132",
		StatusTime: time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local),
		PaidAmount: 250.2, PushAmount: 200.2, OrderPhone: "18616613488",
		ItemsJSON:    `[{"no":"SKU-132"}]`,
		PayItemsJSON: `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":250.2}]`,
	}}}
	service := newTestBojunOracleOrderService(connection, &fakeBojunOracleStateStore{})
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["SALE-132"] = &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 1320}, DocNo: "SALE-132",
		PaidAmount: 100, PushAmount: 60, Synced: 3,
		ItemsJSON: `[{"no":"OLD"}]`, PayItemsJSON: `[{"cPaywayName":"OLD"}]`,
	}

	result, err := service.SyncByStatusTime(t.Context(), "2026-08-25T10:00", "2026-08-25T11:00")
	if err != nil {
		t.Fatalf("SyncByStatusTime() error = %v", err)
	}
	order := store.orders["SALE-132"]
	if order.ItemsJSON != `[{"no":"SKU-132"}]` ||
		order.PayItemsJSON != `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":250.2}]` {
		t.Fatalf("detail JSON was not updated: %+v", order)
	}
	if order.PaidAmount != 250.2 || order.PushAmount != 200.2 || order.OrderPhone != "18616613488" || order.Synced != 3 ||
		store.supplementCalls != 0 || pusher.calls != 0 || len(connection.writeBackIDs) != 0 {
		t.Fatalf("detail backfill changed unrelated order state: %+v", order)
	}
	if result.TotalCount != 1 || result.ExistingCount != 1 || result.WritableCount != 1 ||
		result.UpdatedCount != 1 || result.RetailCount != 0 || store.backfillUpdates != 1 {
		t.Fatalf("result=%+v updates=%d", result, store.backfillUpdates)
	}
	if store.findCalls != 1 || store.existsCalls != 0 {
		t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
	}
}

func TestBojunOracleDetailBackfillCreatesAndPushesMissingOrder(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local)
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 134, StoreCode: "ABCN001A001", DocNo: "SALE-134", StatusTime: statusTime,
		PaidAmount: 250.2, PushAmount: 250.2, IsToShop: "Y",
		ItemsJSON:    `[{"no":"SKU-134"}]`,
		PayItemsJSON: `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":250.2}]`,
	}}}
	service := newTestBojunOracleOrderService(connection, &fakeBojunOracleStateStore{})
	service.batchSize = 2
	pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	service.pushService = pusher
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	rawStore := service.rawDataDAO.(*fakeBojunRawDataCreator)

	result, err := service.SyncByStatusTime(t.Context(), "2026-08-25T10:00", "2026-08-25T11:00")
	if err != nil {
		t.Fatalf("SyncByStatusTime() error = %v", err)
	}
	order := store.orders["SALE-134"]
	if order == nil || order.OracleRetailID == nil || *order.OracleRetailID != 134 ||
		order.ItemsJSON != `[{"no":"SKU-134"}]` ||
		order.PayItemsJSON != `[{"cPaywayId":25,"cPaywayName":"支付宝","payamount":250.2}]` {
		t.Fatalf("created order = %+v", order)
	}
	if rawStore.created != 1 || pusher.calls != 1 || len(connection.writeBackIDs) != 1 || connection.writeBackIDs[0] != 134 {
		t.Fatalf("raw=%d push=%d write-backs=%v", rawStore.created, pusher.calls, connection.writeBackIDs)
	}
	if result.TotalCount != 1 || result.WritableCount != 1 || result.RetailCount != 1 || result.ExistingCount != 0 {
		t.Fatalf("result=%+v", result)
	}
	if store.findCalls != 1 || store.existsCalls != 0 {
		t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
	}
}

func TestBojunOracleOrderLookupFailureStopsBeforeWriting(t *testing.T) {
	lookupFailure := errors.New("order lookup failed")
	tests := []struct {
		name string
		sync func(*BojunOracleOrderService) (*BojunOrderSyncResult, error)
	}{
		{
			name: "incremental",
			sync: func(service *BojunOracleOrderService) (*BojunOrderSyncResult, error) {
				return service.SyncIncremental(t.Context())
			},
		},
		{
			name: "detail backfill",
			sync: func(service *BojunOracleOrderService) (*BojunOrderSyncResult, error) {
				return service.SyncByStatusTime(t.Context(), "2026-08-25T10:00", "2026-08-25T11:00")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
				RetailID: 12, StoreCode: "ABCN001A001", DocNo: "SALE-12",
				StatusTime: time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local), IsToShop: "Y",
			}}}
			state := &fakeBojunOracleStateStore{
				state: model.BojunOracleSyncState{
					SourceCode: bojunOracleDatasourceCode, LastRetailID: 11, Initialized: true,
				},
				leaseAcquired: true,
			}
			service := newTestBojunOracleOrderService(connection, state)
			store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
			store.findErr = lookupFailure
			pusher := &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
			service.pushService = pusher

			result, err := tt.sync(service)
			if !errors.Is(err, lookupFailure) {
				t.Fatalf("error=%v, want the lookup failure", err)
			}
			if result.TotalCount != 1 || result.FailedCount != 1 || result.WritableCount != 0 || result.ExistingCount != 0 {
				t.Fatalf("result=%+v, want one failed order lookup", result)
			}
			if service.rawDataDAO.(*fakeBojunRawDataCreator).created != 0 || len(store.orders) != 0 ||
				store.backfillUpdates != 0 || pusher.calls != 0 || len(connection.writeBackIDs) != 0 || state.advancedTo != 0 {
				t.Fatal("lookup failure wrote order data, pushed an order or advanced the watermark")
			}
			if store.findCalls != 1 || store.existsCalls != 0 {
				t.Fatalf("find calls=%d existence calls=%d, want one order lookup", store.findCalls, store.existsCalls)
			}
		})
	}
}

func TestBojunOracleSuccessfulPushWithFailedWriteBackDoesNotAdvance(t *testing.T) {
	statusTime := time.Date(2026, 8, 25, 15, 42, 21, 0, time.Local)
	connection := &fakeBojunOracleConnection{
		rows: []reportoracle.BojunRetailRow{{
			RetailID: 14, StoreCode: "ABCN001A001", DocNo: "SALE-14", StatusTime: statusTime,
			PaidAmount: 20, PushAmount: 20, IsToShop: "Y",
		}},
		writeBackErr: errors.New("Oracle update failed"),
	}
	state := &fakeBojunOracleStateStore{
		state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 13, Initialized: true},
		leaseAcquired: true,
	}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	service.pushService = &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: true}}
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)

	result, err := service.SyncIncremental(t.Context())
	if err == nil {
		t.Fatal("SyncIncremental() unexpectedly succeeded")
	}
	if state.advancedTo != 0 || result.WatermarkAfter != 13 {
		t.Fatalf("watermark advanced after failed write-back: state=%+v result=%+v", state, result)
	}
	if store.updates[101] != 3 {
		t.Fatalf("local sync status = %d, want pending write-back 3", store.updates[101])
	}
}

func TestBojunOraclePreviewQueriesStatusTimeWithoutWritingOrAdvancing(t *testing.T) {
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID: 15, DocNo: "SALE-15", StatusTime: time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local),
		ItemsJSON: `[]`, PayItemsJSON: `[]`,
	}}}
	state := &fakeBojunOracleStateStore{}
	service := newTestBojunOracleOrderService(connection, state)
	service.batchSize = 2
	rawStore := service.rawDataDAO.(*fakeBojunRawDataCreator)
	store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
	store.orders["SALE-15"] = &model.BojunRetailOrder{DocNo: "SALE-15"}
	var openedConfig reportoracle.Config
	service.openOracle = func(_ context.Context, config reportoracle.Config) (bojunOracleConnection, error) {
		openedConfig = config
		return connection, nil
	}

	result, err := service.PreviewByStatusTime(t.Context(), "2026-08-25T10:00", "2026-08-25T11:00")
	if err != nil {
		t.Fatalf("PreviewByStatusTime() error = %v", err)
	}
	if connection.statusCalls != 1 || connection.statusAfter != 0 || connection.queryCalls != 0 {
		t.Fatalf("Oracle queries = status:%d after:%d incremental:%d", connection.statusCalls, connection.statusAfter, connection.queryCalls)
	}
	if connection.statusStart.Hour() != 10 || connection.statusEnd.Hour() != 11 {
		t.Fatalf("Oracle range query start=%v end=%v", connection.statusStart, connection.statusEnd)
	}
	if openedConfig.Host != "default-oracle" || openedConfig.Username != "default-user" {
		t.Fatalf("opened Oracle config = %+v", openedConfig)
	}
	if result.PreviewCount != 1 || result.WritableCount != 1 || rawStore.created != 0 || state.advancedTo != 0 {
		t.Fatalf("result=%+v raw writes=%d state=%+v", result, rawStore.created, state)
	}
}

func newTestBojunOracleOrderService(connection bojunOracleConnection, state bojunOracleSyncStateStore) *BojunOracleOrderService {
	return &BojunOracleOrderService{
		datasourceStore: &fakeBojunOracleDatasourceStore{datasource: model.ReportDatasource{
			Code: bojunOracleDatasourceCode, Driver: model.ReportDatasourceDriverOracle, Enabled: true,
			CredentialKeyVersion: "v1", PasswordCiphertext: "ciphertext", QueryTimeoutSeconds: 1,
		}},
		decryptor:  fakeBojunOracleDecryptor{},
		openOracle: func(context.Context, reportoracle.Config) (bojunOracleConnection, error) { return connection, nil },
		detailBackfillConfig: appConfig.ReportInputOracleConfig{
			Host: "default-oracle", Port: 1521, ServiceName: "ORCL", Username: "default-user", Password: "password",
			QueryTimeout: time.Second,
		},
		stateStore:     state,
		rawDataDAO:     &fakeBojunRawDataCreator{nextID: 100},
		retailOrderDAO: &fakeBojunOracleRetailStore{orders: map[string]*model.BojunRetailOrder{}},
		now:            func() time.Time { return time.Date(2026, 8, 26, 10, 0, 0, 0, time.Local) },
		newLeaseToken:  func() string { return "lease-token" },
		batchSize:      100, maxPages: 20, leaseTTL: 2 * time.Minute,
	}
}
