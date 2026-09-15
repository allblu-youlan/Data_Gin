package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
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
}

type fakeBojunOracleRetailStore struct {
	orders          map[string]*model.BojunRetailOrder
	nextID          uint
	updates         map[uint]int
	supplementCalls int
	supplementNoop  bool
	backfillUpdates int
}

func (store *fakeBojunOracleRetailStore) ExistsByDocNo(_ context.Context, docNo string) (bool, error) {
	_, exists := store.orders[docNo]
	return exists, nil
}

func (store *fakeBojunOracleRetailStore) FindByDocNo(_ context.Context, docNo string) (*model.BojunRetailOrder, error) {
	order, exists := store.orders[docNo]
	if !exists {
		return nil, errors.New("order not found")
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

func (store *fakeBojunOracleRetailStore) UpdateDetailJSONByDocNo(
	_ context.Context,
	docNo string,
	itemsJSON string,
	payItemsJSON string,
) error {
	order, exists := store.orders[docNo]
	if !exists {
		return nil
	}
	store.backfillUpdates++
	order.ItemsJSON = itemsJSON
	order.PayItemsJSON = payItemsJSON
	return nil
}

type fakeBojunOraclePusher struct {
	result bojunOrderPushResult
	calls  int
	orders []model.BojunRetailOrder
}

func (pusher *fakeBojunOraclePusher) PushNewOrderWithPolicy(
	_ context.Context,
	order *model.BojunRetailOrder,
	_ int,
	_ OrderPushSkipPolicy,
) bojunOrderPushResult {
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

func (store *fakeBojunOracleStateStore) AcquireLease(context.Context, string, string, time.Time, time.Duration) (*model.BojunOracleSyncState, bool, error) {
	state := store.state
	return &state, store.leaseAcquired, nil
}

func (store *fakeBojunOracleStateStore) Advance(_ context.Context, _ string, _ string, expected, next uint64, _ time.Time, _ time.Duration) error {
	store.advancedFrom = expected
	store.advancedTo = next
	return nil
}

func (store *fakeBojunOracleStateStore) RenewLease(context.Context, string, string, time.Time, time.Duration) error {
	store.renewCalls++
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
	if connection.queryAfter != 10 || state.advancedFrom != 10 || state.advancedTo != 11 || state.renewCalls != 1 || state.releaseCalls != 1 {
		t.Fatalf("query/state = after:%d from:%d to:%d renews:%d releases:%d", connection.queryAfter, state.advancedFrom, state.advancedTo, state.renewCalls, state.releaseCalls)
	}
	if result.RetailCount != 1 || result.WatermarkAfter != 11 || !result.LeaseAcquired {
		t.Fatalf("result = %+v", result)
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
}

func TestBojunOracleExistingAPIOrderSupplementsFieldsAndOnlyWritesBack(t *testing.T) {
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
	if updated.StoreCode != "API-STORE" || updated.RetailSaleType != "API-TYPE" || updated.RawDataID != 99 || updated.ItemsJSON != `[{"sku":"API-SKU"}]` ||
		updated.CompletedAt == nil || !updated.CompletedAt.Equal(completedAt) {
		t.Fatalf("existing base fields were overwritten: %+v", updated)
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
}

func TestBojunOracleDetailBackfillUpdatesOnlyJSONFields(t *testing.T) {
	connection := &fakeBojunOracleConnection{rows: []reportoracle.BojunRetailRow{{
		RetailID:     132,
		DocNo:        "SALE-132",
		StatusTime:   time.Date(2026, 8, 25, 10, 30, 0, 0, time.Local),
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
	if order.PaidAmount != 100 || order.PushAmount != 60 || order.Synced != 3 ||
		store.supplementCalls != 0 || pusher.calls != 0 || len(connection.writeBackIDs) != 0 {
		t.Fatalf("detail backfill changed unrelated order state: %+v", order)
	}
	if result.TotalCount != 1 || result.ExistingCount != 1 || result.WritableCount != 1 ||
		result.UpdatedCount != 1 || result.RetailCount != 0 || store.backfillUpdates != 1 {
		t.Fatalf("result=%+v updates=%d", result, store.backfillUpdates)
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
