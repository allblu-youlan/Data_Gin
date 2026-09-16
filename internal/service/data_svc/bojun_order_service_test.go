package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/orderpush"
	"gin-biz-web-api/pkg/shanghaimall"
)

type fakeBojunRawDataCreator struct {
	created int
	nextID  uint
}

func (f *fakeBojunRawDataCreator) Create(ctx context.Context, rawData *model.RawData) (uint, error) {
	_ = ctx
	f.created++
	if f.nextID == 0 {
		f.nextID = 100
	}
	return f.nextID, nil
}

type fakeBojunRetailOrderWriter struct {
	existing map[string]bool
	orders   map[string]*model.BojunRetailOrder
	created  []string
	updated  []string
	failFind bool
}

func (f *fakeBojunRetailOrderWriter) ExistsByDocNo(ctx context.Context, docNo string) (bool, error) {
	_ = ctx
	if f.failFind {
		return false, errors.New("find failed")
	}
	return f.existing[docNo], nil
}

func (f *fakeBojunRetailOrderWriter) CreateIfNotExists(ctx context.Context, order *model.BojunRetailOrder) (bool, error) {
	_ = ctx
	if f.existing[order.DocNo] {
		return false, nil
	}
	f.created = append(f.created, order.DocNo)
	f.existing[order.DocNo] = true
	if f.orders == nil {
		f.orders = make(map[string]*model.BojunRetailOrder)
	}
	copyOrder := *order
	f.orders[order.DocNo] = &copyOrder
	return true, nil
}

func (f *fakeBojunRetailOrderWriter) FindByDocNo(_ context.Context, docNo string) (*model.BojunRetailOrder, error) {
	if order := f.orders[docNo]; order != nil {
		copyOrder := *order
		return &copyOrder, nil
	}
	if f.existing[docNo] {
		return &model.BojunRetailOrder{DocNo: docNo}, nil
	}
	return nil, errors.New("order not found")
}

func (f *fakeBojunRetailOrderWriter) UpdateAPIBusinessFields(_ context.Context, order *model.BojunRetailOrder) error {
	if f.orders == nil {
		f.orders = make(map[string]*model.BojunRetailOrder)
	}
	copyOrder := *order
	f.orders[order.DocNo] = &copyOrder
	f.updated = append(f.updated, order.DocNo)
	return nil
}

type fakeBojunSyncUpdater struct {
	updates map[uint]int
}

func (f *fakeBojunSyncUpdater) UpdateSyncStatus(ctx context.Context, id uint, synced int) error {
	_ = ctx
	if f.updates == nil {
		f.updates = map[uint]int{}
	}
	f.updates[id] = synced
	return nil
}

type fakeDeliveryLogCreator struct {
	logs []model.DeliveryLog
}

type fakeBojunPipelineRunRecorder struct {
	finishContextErr  error
	finishHasDeadline bool
	finishStatus      string
	finishError       string
}

func (f *fakeBojunPipelineRunRecorder) Create(context.Context, *model.PipelineRun) (uint, error) {
	return 1, nil
}

func (f *fakeBojunPipelineRunRecorder) Finish(
	ctx context.Context,
	_ uint,
	status string,
	_, _ int,
	errorMessage string,
) error {
	f.finishContextErr = ctx.Err()
	_, f.finishHasDeadline = ctx.Deadline()
	f.finishStatus = status
	f.finishError = errorMessage
	return nil
}

func (f *fakeDeliveryLogCreator) Create(ctx context.Context, log *model.DeliveryLog) (uint, error) {
	_ = ctx
	f.logs = append(f.logs, *log)
	return uint(len(f.logs)), nil
}

func TestBuildBojunOrderRequestBody(t *testing.T) {
	body := buildBojunOrderRequestBody(
		2,
		100,
		"2026-07-03 12:00:00",
		"2026-07-03 12:01:00",
	)

	if body["current"] != 2 {
		t.Fatalf("current = %v", body["current"])
	}
	if body["pageSize"] != 100 {
		t.Fatalf("pageSize = %v", body["pageSize"])
	}
	if body["startTime"] != "2026-07-03 12:00:00" || body["endTime"] != "2026-07-03 12:01:00" {
		t.Fatalf("time range = %v/%v", body["startTime"], body["endTime"])
	}
}

func TestFinishBojunOrderRunUsesCleanupContextAfterCancellation(t *testing.T) {
	recorder := &fakeBojunPipelineRunRecorder{}
	service := &BojunOrderService{pipelineRunDAO: recorder}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.finishBojunOrderRunIfNeeded(ctx, 17, &BojunOrderSyncResult{}, context.Canceled)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
	if recorder.finishContextErr != nil {
		t.Fatalf("finish context error=%v, want active cleanup context", recorder.finishContextErr)
	}
	if !recorder.finishHasDeadline {
		t.Fatal("finish context has no deadline")
	}
	if recorder.finishStatus != "failed" || recorder.finishError != context.Canceled.Error() {
		t.Fatalf("finish status=%q error=%q", recorder.finishStatus, recorder.finishError)
	}
}

func TestFinishBojunOrderRunMarksInterruptedWorkPartialSuccess(t *testing.T) {
	recorder := &fakeBojunPipelineRunRecorder{}
	service := &BojunOrderService{pipelineRunDAO: recorder}

	err := service.finishBojunOrderRunIfNeeded(
		context.Background(),
		17,
		&BojunOrderSyncResult{RetailCount: 1},
		context.Canceled,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
	if recorder.finishStatus != "partial_success" {
		t.Fatalf("finish status=%q, want partial_success", recorder.finishStatus)
	}
}

func TestExtractBojunOrderRecords(t *testing.T) {
	payload := map[string]interface{}{
		"code": float64(200),
		"data": map[string]interface{}{
			"current":   float64(1),
			"total":     float64(3),
			"totalPage": float64(2),
			"records": []interface{}{
				map[string]interface{}{"docno": "A001", "totQty": float64(2)},
				map[string]interface{}{"docno": "A002", "totQty": float64(1)},
			},
		},
	}

	records, pageInfo, err := extractBojunOrderRecords(payload)
	if err != nil {
		t.Fatalf("extractBojunOrderRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records length = %d", len(records))
	}
	if pageInfo.Current != 1 || pageInfo.TotalPage != 2 || pageInfo.Total != 3 {
		t.Fatalf("pageInfo = %+v", pageInfo)
	}
}

func TestBuildBojunOrderRawDataMarksSource(t *testing.T) {
	record := map[string]interface{}{
		"docno":      "ABCN001P012P12607031240270004",
		"cStoreCode": "ABCN001P012",
	}

	rawData, err := buildBojunOrderRawData(
		record,
		defaultBojunOrderMethod,
		"2026-07-03 12:00:00",
		"2026-07-03 12:01:00",
		1,
	)
	if err != nil {
		t.Fatalf("buildBojunOrderRawData returned error: %v", err)
	}
	if rawData.Source != bojunOrderSource || rawData.Remark != bojunOrderSource {
		t.Fatalf("source/remark = %s/%s", rawData.Source, rawData.Remark)
	}
	if rawData.ExternalID != "ABCN001P012P12607031240270004" {
		t.Fatalf("external_id = %s", rawData.ExternalID)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(rawData.Metadata), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["source"] != bojunOrderSource || metadata["remark"] != bojunOrderSource {
		t.Fatalf("metadata = %v", metadata)
	}

	var rawContent map[string]interface{}
	if err := json.Unmarshal([]byte(rawData.RawContent), &rawContent); err != nil {
		t.Fatal(err)
	}
	if rawContent["docno"] != "ABCN001P012P12607031240270004" {
		t.Fatalf("raw docno = %v", rawContent["docno"])
	}
}

func TestBojunOrderPushSkipsByConfiguredPolicy(t *testing.T) {
	updater := &fakeBojunSyncUpdater{}
	logCreator := &fakeDeliveryLogCreator{}
	service := &BojunOrderPushService{
		retailOrderDAO: updater,
		logDAO:         logCreator,
	}

	result := service.PushNewOrderWithPolicy(context.Background(), &model.BojunRetailOrder{
		BaseModel:      model.BaseModel{ID: 7},
		DocNo:          "B005",
		StoreCode:      "ABCN001P012",
		StoreName:      "前滩",
		TotalQty:       1,
		TotalAmtActual: 99,
	}, 5, OrderPushSkipPolicy{Cycle: 5, Skip: 1})

	if !result.Success || !result.Skipped || result.Error != nil {
		t.Fatalf("result = %+v, want successful skipped push", result)
	}
	if updater.updates[7] != 1 {
		t.Fatalf("sync update = %v, want id 7 synced to 1", updater.updates)
	}
	if len(logCreator.logs) != 1 {
		t.Fatalf("logs length = %d, want 1", len(logCreator.logs))
	}
	if logCreator.logs[0].ResponseBody != "skipped_by_order_push_policy" || !logCreator.logs[0].Success {
		t.Fatalf("log = %+v, want successful policy skip log", logCreator.logs[0])
	}
	if logCreator.logs[0].SourceCode != "" {
		t.Fatalf("source_code = %q, want empty", logCreator.logs[0].SourceCode)
	}
	if logCreator.logs[0].DatasetKind != bojunOrderDatasetKind {
		t.Fatalf("dataset_kind = %q, want %q", logCreator.logs[0].DatasetKind, bojunOrderDatasetKind)
	}
}

func TestBojunOrderPushWithoutTargetDoesNotWriteDeliveryLog(t *testing.T) {
	logCreator := &fakeDeliveryLogCreator{}
	service := &BojunOrderPushService{logDAO: logCreator}

	result := service.PushNewOrderWithPolicy(context.Background(), &model.BojunRetailOrder{
		BaseModel: model.BaseModel{ID: 8},
		DocNo:     "B006",
		StoreCode: "UNKNOWN",
	}, 1, OrderPushSkipPolicy{})

	if !result.Skipped || result.Error == nil {
		t.Fatalf("result = %+v, want unmatched target skip", result)
	}
	if len(logCreator.logs) != 0 {
		t.Fatalf("logs length = %d, want no delivery log", len(logCreator.logs))
	}
}

func TestBojunOrderDeliveryLogDoesNotWriteSourceCode(t *testing.T) {
	log := newBojunOrderDeliveryLog(deliveryLogPayload{
		TraceID: "trace-1",
		Target:  bojunOrderPushTarget{Code: "target-1", Name: "目标一"},
		Order: &model.BojunRetailOrder{
			BaseModel: model.BaseModel{ID: 8},
			DocNo:     "B006",
		},
		Success: true,
	}, time.Date(2026, time.August, 12, 12, 0, 0, 0, time.Local))

	if log.SourceCode != "" {
		t.Fatalf("source_code = %q, want empty", log.SourceCode)
	}
	if log.DatasetKind != bojunOrderDatasetKind {
		t.Fatalf("dataset_kind = %q, want %q", log.DatasetKind, bojunOrderDatasetKind)
	}
}

func TestIsBojunOrderDeliveryLog(t *testing.T) {
	tests := []struct {
		name string
		log  model.DeliveryLog
		want bool
	}{
		{name: "historical source marker", log: model.DeliveryLog{SourceCode: bojunOrderPushSource}, want: true},
		{name: "new internal marker", log: model.DeliveryLog{DatasetKind: bojunOrderDatasetKind}, want: true},
		{name: "other source", log: model.DeliveryLog{SourceCode: "qimai_order", DatasetKind: bojunOrderDatasetKind}, want: false},
		{name: "configured destination with internal marker", log: model.DeliveryLog{DestinationID: 9, DatasetKind: bojunOrderDatasetKind}, want: false},
		{name: "unknown empty source", log: model.DeliveryLog{DestinationCode: string(shanghaimall.TargetQiantan)}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isBojunOrderDeliveryLog(&test.log); got != test.want {
				t.Fatalf("isBojunOrderDeliveryLog() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestProcessBojunOrderRecordPreviewDoesNotWrite(t *testing.T) {
	rawCreator := &fakeBojunRawDataCreator{}
	orderWriter := &fakeBojunRetailOrderWriter{existing: map[string]bool{}}
	service := &BojunOrderService{
		rawDataDAO:     rawCreator,
		retailOrderDAO: orderWriter,
	}
	result := &BojunOrderSyncResult{}
	record := map[string]interface{}{
		"docno":        "B001",
		"cStoreCode":   "ABCN001P012",
		"cStoreName":   "前滩",
		"totQty":       float64(1),
		"totAmtActual": float64(99.5),
	}

	service.processBojunOrderRecord(context.Background(), record, defaultBojunOrderMethod, "2026-07-08 10:00:00", "2026-07-08 10:30:00", 1, false, result, OrderPushSkipConfig{})

	if result.TotalCount != 1 || result.PreviewCount != 1 || result.WritableCount != 1 || result.RetailCount != 0 {
		t.Fatalf("result = %+v, want preview without retail write", result)
	}
	if rawCreator.created != 0 || len(orderWriter.created) != 0 {
		t.Fatalf("preview wrote data: raw=%d orders=%v", rawCreator.created, orderWriter.created)
	}
	if len(result.Samples) != 1 || result.Samples[0].Status != "pending" {
		t.Fatalf("samples = %+v, want pending preview sample", result.Samples)
	}
}

func TestProcessBojunOrderRecordReturnsInfrastructureError(t *testing.T) {
	service := &BojunOrderService{
		retailOrderDAO: &fakeBojunRetailOrderWriter{failFind: true},
	}
	result := &BojunOrderSyncResult{}

	err := service.processBojunOrderRecord(
		context.Background(),
		map[string]interface{}{"docno": "B001"},
		defaultBojunOrderMethod,
		"",
		"",
		1,
		true,
		result,
		OrderPushSkipConfig{},
	)

	if err == nil || !strings.Contains(err.Error(), "find failed") {
		t.Fatalf("error=%v, want infrastructure error", err)
	}
	if result.FailedCount != 1 || len(result.FailedSamples) != 1 {
		t.Fatalf("result=%+v, want one recorded failure", result)
	}
}

func TestProcessBojunOrderRecordConfirmWritesOnlyNewRows(t *testing.T) {
	rawCreator := &fakeBojunRawDataCreator{}
	orderWriter := &fakeBojunRetailOrderWriter{existing: map[string]bool{}}
	service := &BojunOrderService{
		rawDataDAO:     rawCreator,
		retailOrderDAO: orderWriter,
		pushService:    nil,
	}
	result := &BojunOrderSyncResult{}
	record := map[string]interface{}{
		"docno":        "B001",
		"cStoreCode":   "ABCN001P012",
		"cStoreName":   "前滩",
		"totQty":       float64(1),
		"totAmtActual": float64(99.5),
	}

	service.processBojunOrderRecord(context.Background(), record, defaultBojunOrderMethod, "2026-07-08 10:00:00", "2026-07-08 10:30:00", 1, true, result, OrderPushSkipConfig{})

	if result.TotalCount != 1 || result.SavedCount != 1 || result.RetailCount != 1 || result.FailedCount != 0 {
		t.Fatalf("result = %+v, want saved retail row", result)
	}
	if rawCreator.created != 1 || len(orderWriter.created) != 1 || orderWriter.created[0] != "B001" {
		t.Fatalf("writes raw=%d orders=%v", rawCreator.created, orderWriter.created)
	}
	if len(result.Samples) != 1 || result.Samples[0].Status != "created" {
		t.Fatalf("samples = %+v, want created sample", result.Samples)
	}
}

func TestProcessBojunOrderRecordSkipsExistingRows(t *testing.T) {
	rawCreator := &fakeBojunRawDataCreator{}
	orderWriter := &fakeBojunRetailOrderWriter{
		existing: map[string]bool{"B001": true},
		orders: map[string]*model.BojunRetailOrder{
			"B001": {DocNo: "B001", OrderTypeCode: "CMR", OrderTypeName: "正常零售"},
		},
	}
	service := &BojunOrderService{
		rawDataDAO:     rawCreator,
		retailOrderDAO: orderWriter,
	}
	result := &BojunOrderSyncResult{}

	service.processBojunOrderRecord(context.Background(), map[string]interface{}{"docno": "B001"}, defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{})

	if result.SkippedCount != 1 || result.ExistingCount != 1 || result.RetailCount != 0 {
		t.Fatalf("result = %+v, want existing skip", result)
	}
	if rawCreator.created != 0 || len(orderWriter.created) != 0 {
		t.Fatalf("existing row wrote data: raw=%d orders=%v", rawCreator.created, orderWriter.created)
	}
}

func TestProcessBojunOrderRecordInvalidDocNoDoesNotWriteRawData(t *testing.T) {
	rawCreator := &fakeBojunRawDataCreator{}
	orderWriter := &fakeBojunRetailOrderWriter{existing: map[string]bool{}}
	service := &BojunOrderService{
		rawDataDAO:     rawCreator,
		retailOrderDAO: orderWriter,
	}
	result := &BojunOrderSyncResult{}

	service.processBojunOrderRecord(context.Background(), map[string]interface{}{"cStoreCode": "ABCN001P012"}, defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{})

	if result.FailedCount != 1 || len(result.FailedSamples) != 1 {
		t.Fatalf("result = %+v, want failed sample", result)
	}
	if rawCreator.created != 0 || len(orderWriter.created) != 0 {
		t.Fatalf("invalid row wrote data: raw=%d orders=%v", rawCreator.created, orderWriter.created)
	}
}

func TestNormalizeBojunOrderMethodDefaultsToAsyncEndpoint(t *testing.T) {
	if got := normalizeBojunOrderMethod(""); got != defaultBojunOrderMethod {
		t.Fatalf("method = %s", got)
	}
}

func TestNormalizeBojunOrderMethodUpgradesLegacyEndpoint(t *testing.T) {
	if got := normalizeBojunOrderMethod("/retail/retail.query"); got != defaultBojunOrderMethod {
		t.Fatalf("method = %s", got)
	}
}

func TestNormalizeBojunOrderMethodStripsStandardPrefix(t *testing.T) {
	got := normalizeBojunOrderMethod("/bos/standard/retail/middleretail.query")
	if got != defaultBojunOrderMethod {
		t.Fatalf("method = %s", got)
	}
}

func TestNormalizeBojunOrderTimeRangeAcceptsDatetimeLocal(t *testing.T) {
	start, end, err := normalizeBojunOrderTimeRange("2026-07-08T10:00", "2026-07-08T10:30")
	if err != nil {
		t.Fatalf("normalizeBojunOrderTimeRange returned error: %v", err)
	}
	if start != "2026-07-08 10:00:00" || end != "2026-07-08 10:30:00" {
		t.Fatalf("range = %s/%s", start, end)
	}
}

func TestNormalizeBojunOrderTimeRangeRequiresStartBeforeEnd(t *testing.T) {
	_, _, err := normalizeBojunOrderTimeRange("2026-07-08 10:30:00", "2026-07-08 10:00:00")
	if err == nil {
		t.Fatal("normalizeBojunOrderTimeRange returned nil error, want range error")
	}
}

func TestBuildBojunRetailOrderMapsNormalOrder(t *testing.T) {
	record := map[string]interface{}{
		"docno":           "ABCN001P012P12607031240270004",
		"billdate":        float64(20260703),
		"extendedFields1": "2026-07-03 12:40:27",
		"retailbilltype":  "CMR",
		"retailsaletype":  "CMR",
		"cStoreCode":      "ABCN001P012",
		"cStoreName":      "ALLBLU幼岚（上海浦东新区晶耀前滩店）",
		"totQty":          float64(2),
		"totAmtActual":    float64(446.4),
		"items":           []interface{}{map[string]interface{}{"no": "SKU001"}},
		"payItems":        []interface{}{map[string]interface{}{"cPaywayName": "微信"}},
	}

	order, err := buildBojunRetailOrder(9, record)
	if err != nil {
		t.Fatalf("buildBojunRetailOrder returned error: %v", err)
	}
	if order.RawDataID != 9 || order.DocNo != "ABCN001P012P12607031240270004" {
		t.Fatalf("order key = %d/%s", order.RawDataID, order.DocNo)
	}
	if order.CompletedAt == nil || order.CompletedAt.Format("2006-01-02 15:04:05") != "2026-07-03 12:40:27" {
		t.Fatalf("completed at = %v", order.CompletedAt)
	}
	if order.OrderTypeCode != "CMR" || order.OrderTypeName != "正常零售" {
		t.Fatalf("order type = %s/%s", order.OrderTypeCode, order.OrderTypeName)
	}
	if order.RelatedNormalNo != "" {
		t.Fatalf("related normal docno = %s", order.RelatedNormalNo)
	}
	if order.TotalQty != 2 || order.TotalAmtActual != 446.4 {
		t.Fatalf("totals = %d/%v", order.TotalQty, order.TotalAmtActual)
	}
}

func TestBuildBojunRetailOrderAllowsMissingCompletedAt(t *testing.T) {
	order, err := buildBojunRetailOrder(9, map[string]interface{}{
		"docno": "B001", "items": []interface{}{}, "payItems": []interface{}{},
	})
	if err != nil || order.CompletedAt != nil {
		t.Fatalf("order=%+v error=%v", order, err)
	}
}

func TestBuildBojunRetailOrderIgnoresInvalidCompletedAt(t *testing.T) {
	order, err := buildBojunRetailOrder(9, map[string]interface{}{
		"docno": "B001", "extendedFields1": "2026-07-03T12:40:27",
	})
	if err != nil || order.CompletedAt != nil {
		t.Fatalf("order=%+v error=%v", order, err)
	}
}

func TestProcessBojunOrderRecordCountsInvalidCompletedAtWithoutDroppingOrder(t *testing.T) {
	rawCreator := &fakeBojunRawDataCreator{}
	orderWriter := &fakeBojunRetailOrderWriter{existing: map[string]bool{}}
	service := &BojunOrderService{rawDataDAO: rawCreator, retailOrderDAO: orderWriter}
	result := &BojunOrderSyncResult{}
	service.processBojunOrderRecord(
		context.Background(),
		map[string]interface{}{"docno": "B001", "extendedFields1": map[string]interface{}{"bad": true}},
		defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{},
	)
	if result.InvalidCompletedAtCount != 1 || result.RetailCount != 1 || result.FailedCount != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestProcessBojunOrderRecordBackfillsExistingCompletedAt(t *testing.T) {
	orderWriter := &fakeBojunRetailOrderWriter{existing: map[string]bool{"B001": true}}
	service := &BojunOrderService{retailOrderDAO: orderWriter}
	result := &BojunOrderSyncResult{}
	service.processBojunOrderRecord(
		context.Background(),
		map[string]interface{}{"docno": "B001", "extendedFields1": "2026-07-03 12:40:27"},
		defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{},
	)
	if result.UpdatedCount != 1 || result.ExistingCount != 0 || result.FailedCount != 0 {
		t.Fatalf("result=%+v", result)
	}
	if len(orderWriter.updated) != 1 || orderWriter.orders["B001"].CompletedAt == nil ||
		orderWriter.orders["B001"].CompletedAt.Format("2006-01-02 15:04:05") != "2026-07-03 12:40:27" {
		t.Fatalf("updated=%v order=%+v", orderWriter.updated, orderWriter.orders["B001"])
	}
}

func TestProcessBojunOrderRecordReconcilesExistingBusinessFields(t *testing.T) {
	orderWriter := &fakeBojunRetailOrderWriter{
		existing: map[string]bool{"B001": true},
		orders: map[string]*model.BojunRetailOrder{
			"B001": {
				DocNo: "B001", OrderTypeCode: "CMR", OrderTypeName: "正常零售",
				TotalAmtActual: 10, ItemsJSON: `[{"no":"OLD"}]`, PayItemsJSON: `[]`,
			},
		},
	}
	service := &BojunOrderService{retailOrderDAO: orderWriter}
	result := &BojunOrderSyncResult{}
	err := service.processBojunOrderRecord(
		context.Background(),
		map[string]interface{}{
			"docno": "B001", "totAmtActual": 20.5,
			"items":    []interface{}{map[string]interface{}{"no": "NEW", "totAmtActual": 20.5}},
			"payItems": []interface{}{map[string]interface{}{"cPaywayName": "支付宝", "payamount": 20.5}},
		},
		defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{},
	)
	if err != nil {
		t.Fatalf("processBojunOrderRecord() error=%v", err)
	}
	updated := orderWriter.orders["B001"]
	if result.UpdatedCount != 1 || result.FailedCount != 0 || updated.TotalAmtActual != 20.5 ||
		!strings.Contains(updated.ItemsJSON, `"no":"NEW"`) || !strings.Contains(updated.PayItemsJSON, `"cPaywayName":"支付宝"`) {
		t.Fatalf("result=%+v updated=%+v", result, updated)
	}
}

func TestProcessBojunOrderRecordDoesNotClearFieldsMissingFromPayload(t *testing.T) {
	completedAt := time.Date(2026, 7, 3, 12, 40, 27, 0, time.Local)
	existing := &model.BojunRetailOrder{
		DocNo: "B001", OtherDocNo: "EXT-1", BillDate: 20260703, CompletedAt: &completedAt,
		StoreCode: "STORE-1", StoreName: "门店一", RetailSaleType: "RET",
		OrderTypeCode: "RET", OrderTypeName: "退货", TotalLines: 1, TotalQty: -1,
		TotalAmtList: -100, TotalAmtActual: -90, AvgDiscount: 0.9, TotalAmtAcc: -90, TotalAmtAcc1: -90,
		RelatedNormalNo: "NORMAL-1", ItemsJSON: `[{"no":"SKU-1"}]`, PayItemsJSON: `[{"cPaywayName":"支付宝"}]`,
	}
	orderWriter := &fakeBojunRetailOrderWriter{
		existing: map[string]bool{"B001": true}, orders: map[string]*model.BojunRetailOrder{"B001": existing},
	}
	service := &BojunOrderService{retailOrderDAO: orderWriter}
	result := &BojunOrderSyncResult{}
	err := service.processBojunOrderRecord(
		context.Background(), map[string]interface{}{"docno": "B001"},
		defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{},
	)
	if err != nil {
		t.Fatalf("processBojunOrderRecord() error=%v", err)
	}
	if result.SkippedCount != 1 || result.UpdatedCount != 0 || len(orderWriter.updated) != 0 {
		t.Fatalf("result=%+v updated=%v", result, orderWriter.updated)
	}
}

func TestProcessBojunOrderRecordUpdatesRelatedOrderFromO2OSODocNo(t *testing.T) {
	orderWriter := &fakeBojunRetailOrderWriter{
		existing: map[string]bool{"R001": true},
		orders: map[string]*model.BojunRetailOrder{
			"R001": {DocNo: "R001", RetailSaleType: "RET", OrderTypeCode: "RET", OrderTypeName: "退货", RelatedNormalNo: "OLD"},
		},
	}
	service := &BojunOrderService{retailOrderDAO: orderWriter}
	result := &BojunOrderSyncResult{}
	err := service.processBojunOrderRecord(
		context.Background(), map[string]interface{}{
			"docno": "R001", "retailsaletype": "RET", "o2oSoDocno": "NEW",
		},
		defaultBojunOrderMethod, "", "", 1, true, result, OrderPushSkipConfig{},
	)
	if err != nil || result.UpdatedCount != 1 || orderWriter.orders["R001"].RelatedNormalNo != "NEW" {
		t.Fatalf("error=%v result=%+v order=%+v", err, result, orderWriter.orders["R001"])
	}
}

func TestBuildBojunRetailOrderMapsRefundOrder(t *testing.T) {
	record := map[string]interface{}{
		"docno":          "ABCN001A004P12606301701270020",
		"description":    "由单据ABCN001A004P12606301638550019退货产生",
		"retailsaletype": "RET",
		"items": []interface{}{
			map[string]interface{}{"orgdocno": "ABCN001A004P12606301638550019", "qty": float64(-1)},
		},
	}

	order, err := buildBojunRetailOrder(10, record)
	if err != nil {
		t.Fatalf("buildBojunRetailOrder returned error: %v", err)
	}
	if order.OrderTypeCode != "RET" || order.OrderTypeName != "退货" {
		t.Fatalf("order type = %s/%s", order.OrderTypeCode, order.OrderTypeName)
	}
	if order.RelatedNormalNo != "ABCN001A004P12606301638550019" {
		t.Fatalf("related normal docno = %s", order.RelatedNormalNo)
	}
}

func TestBuildBojunRetailOrderMapsExchangeOrder(t *testing.T) {
	record := map[string]interface{}{
		"docno":          "ABCN001A001P12607011137100006",
		"description":    "由单据E20260629145733101806231退货产生",
		"retailsaletype": "EXP",
		"items": []interface{}{
			map[string]interface{}{"orgdocno": "E20260629145733101806231", "qty": float64(-1)},
			map[string]interface{}{"qty": float64(1)},
		},
	}

	order, err := buildBojunRetailOrder(11, record)
	if err != nil {
		t.Fatalf("buildBojunRetailOrder returned error: %v", err)
	}
	if order.OrderTypeCode != "EXP" || order.OrderTypeName != "换货" {
		t.Fatalf("order type = %s/%s", order.OrderTypeCode, order.OrderTypeName)
	}
	if order.RelatedNormalNo != "E20260629145733101806231" {
		t.Fatalf("related normal docno = %s", order.RelatedNormalNo)
	}
}

func TestBojunTargetForStoreMapsPushUnits(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "")

	cases := map[string]string{
		"ABCN001A001": string(shanghaimall.TargetShangsheng),
		"ABCN001A004": string(shanghaimall.TargetJialiCheng),
		"ABCN001A005": string(shanghaimall.TargetPanlong),
		"ABCN001A003": string(shanghaimall.TargetXintiandi),
		"ABCN001P012": string(shanghaimall.TargetQiantan),
		"ABCN002A001": bojunPushTargetHangzhouHenglong,
	}

	for storeCode, wantTarget := range cases {
		target, ok := bojunTargetForStore(storeCode)
		if !ok {
			t.Fatalf("store %s did not resolve target", storeCode)
		}
		if target.Code != wantTarget {
			t.Fatalf("store %s target = %s, want %s", storeCode, target.Code, wantTarget)
		}
	}
}

func TestBojunTargetForStoreRejectsUnknownStore(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "")

	if _, ok := bojunTargetForStore("UNKNOWN"); ok {
		t.Fatal("unknown store unexpectedly resolved target")
	}
}

func TestBojunTargetForStoreMapsConfiguredXinjiaCenter(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", " abcn001p014 ")

	target, ok := bojunTargetForStore("ABCN001P014")
	if !ok {
		t.Fatal("configured xinjia center store did not resolve target")
	}
	if target.Code != string(shanghaimall.TargetXinjiaCenter) || target.Name != "新嘉中心" || target.Store != "ABCN001P014" {
		t.Fatalf("target = %+v", target)
	}
}

func TestBojunTargetForStoreDoesNotOverrideBuiltinStore(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "ABCN001P012")

	target, ok := bojunTargetForStore("ABCN001P012")
	if !ok || target.Code != string(shanghaimall.TargetQiantan) {
		t.Fatalf("target = %+v, resolved = %v", target, ok)
	}
}

func TestRetailOrderForXinjiaCenterUsesCompletedAt(t *testing.T) {
	completedAt := time.Date(2026, 8, 21, 14, 30, 45, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	order := &model.BojunRetailOrder{
		DocNo:          "SALE-001",
		BillDate:       20260821,
		CompletedAt:    &completedAt,
		OrderTypeCode:  "CMR",
		TotalAmtActual: 128.50,
	}

	got := retailOrderForTarget(order, string(shanghaimall.TargetXinjiaCenter))
	if got.SaleTime != "2026-08-21 14:30:45" {
		t.Fatalf("sale time = %q", got.SaleTime)
	}

	legacy := retailOrderForTarget(order, string(shanghaimall.TargetQiantan))
	if legacy.SaleTime != "2026-08-21 00:00:00" {
		t.Fatalf("legacy sale time = %q", legacy.SaleTime)
	}
}

func TestBojunHangzhouHenglongCodesAreSeparatedFromQimai(t *testing.T) {
	if bojunPushTargetHangzhouHenglong != orderpush.TargetBojunHangzhouHenglong {
		t.Fatalf("bojun hangzhou target = %s, want %s", bojunPushTargetHangzhouHenglong, orderpush.TargetBojunHangzhouHenglong)
	}
	if bojunPushTargetHangzhouHenglong == orderpush.TargetQimaiHangzhouHenglong {
		t.Fatalf("bojun and qimai hangzhou target codes must differ: %s", bojunPushTargetHangzhouHenglong)
	}
	if bojunHangzhouHenglongStoreCode != "416201" {
		t.Fatalf("bojun hangzhou henglong store code = %s, want 416201", bojunHangzhouHenglongStoreCode)
	}
	if bojunHangzhouHenglongItemCode != "E6600000099" {
		t.Fatalf("bojun hangzhou henglong item code = %s, want E6600000099", bojunHangzhouHenglongItemCode)
	}
}

func TestMarshalBojunJSONDistinguishesMissingFromEmptyDetails(t *testing.T) {
	missing, err := marshalBojunJSON(nil)
	if err != nil || missing != "null" {
		t.Fatalf("marshalBojunJSON(nil) = %q, %v", missing, err)
	}
	empty, err := marshalBojunJSON([]interface{}{})
	if err != nil || empty != "[]" {
		t.Fatalf("marshalBojunJSON(empty) = %q, %v", empty, err)
	}
}
