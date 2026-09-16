package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

type fakeOpenBojunOrderReader struct {
	query  data_dao.OpenBojunOrderQuery
	orders []model.BojunRetailOrder
	calls  int
}

func (reader *fakeOpenBojunOrderReader) CountOpenOrders(context.Context, data_dao.OpenBojunOrderQuery) (int64, error) {
	return int64(len(reader.orders)), nil
}

func (reader *fakeOpenBojunOrderReader) ListOpenOrders(
	_ context.Context,
	query data_dao.OpenBojunOrderQuery,
) ([]model.BojunRetailOrder, error) {
	reader.calls++
	reader.query = query
	return reader.orders, nil
}

type fakeOpenBojunPermissionReader struct {
	allowed    bool
	permission string
}

func (reader *fakeOpenBojunPermissionReader) HasPermission(
	_ context.Context,
	_ uint,
	permission string,
	_ time.Time,
) (bool, error) {
	reader.permission = permission
	return reader.allowed, nil
}

func TestOpenBojunOrderQueryServiceReturnsSanitizedCursorPage(t *testing.T) {
	completedAt := time.Date(2026, 7, 3, 12, 40, 27, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	olderCompletedAt := completedAt.Add(-time.Hour)
	orders := &fakeOpenBojunOrderReader{orders: []model.BojunRetailOrder{
		{
			BaseModel: model.BaseModel{ID: 9}, DocNo: "B001", OtherDocNo: "EXT001",
			OrderPhone: "18616613488",
			BillDate:   20260703, CompletedAt: &completedAt, StoreCode: "ABCN001P012", StoreName: "前滩",
			OrderTypeCode: "CMR", OrderTypeName: "正常零售", TotalLines: 1, TotalQty: 2,
			TotalAmtList: 500, TotalAmtActual: 446.4, AvgDiscount: 0.8928,
			ItemsJSON: `[{
				"no":"SKU001","mProductName":"商品","qty":2,"totAmtActual":446.4,"vipno":"secret",
				"type":1,"docno":"B001","amtAcc":446.4,"value1":"中灰","value2":"130CM",
				"markdis":0,"discount":0.8928,"pricelist":250,"prodColor":"SKU001-COLOR",
				"totAmtAcc":446.4,"totAmtList":500,"dmAmtRetail":null,"priceactual":223.2,
				"productValue":"儿童|长裤"
			}]`,
			PayItemsJSON:   `[{"cPaywayId":25,"payamount":446.4,"cPaywayName":"支付宝"}]`,
			RawContentJSON: `{"secret":"must-not-leak"}`, VIPNo: "member-secret",
		},
		{BaseModel: model.BaseModel{ID: 8}, DocNo: "B002", BillDate: 20260702, CompletedAt: &olderCompletedAt},
	}}
	permissions := &fakeOpenBojunPermissionReader{allowed: true}
	service := newOpenBojunOrderQueryService(orders, permissions, time.Now)
	result, err := service.Query(t.Context(), 17, requestbody.OpenBojunOrderQueryRequest{
		StartTime: "2026-07-01 00:00:00", EndTime: "2026-08-01 00:00:00",
		MallCodes:  []string{" abcn001p012 ", "ABCN001P012"},
		OrderTypes: []string{"cmr"}, PageSize: 1,
	})
	if err != nil {
		t.Fatalf("Query() error=%v", err)
	}
	if permissions.permission != model.PermissionBojunOrderRead || orders.calls != 1 {
		t.Fatalf("permission=%q calls=%d", permissions.permission, orders.calls)
	}
	if orders.query.StartCompletedAt.Format(openBojunOrderDateTimeFormat) != "2026-07-01 00:00:00" ||
		orders.query.EndCompletedAt.Format(openBojunOrderDateTimeFormat) != "2026-08-01 00:00:00" ||
		len(orders.query.StoreCodes) != 1 || orders.query.StoreCodes[0] != "ABCN001P012" ||
		orders.query.Limit != 2 {
		t.Fatalf("query=%+v", orders.query)
	}
	if len(result.Items) != 1 || result.Items[0].OrderDate != "2026-07-03 00:00:00" ||
		result.Items[0].OrderPhone != "18616613488" ||
		result.Items[0].CompletedAt == nil || *result.Items[0].CompletedAt != "2026-07-03 12:40:27" ||
		result.Items[0].MallCode != "ABCN001P012" || result.Items[0].MallName != "前滩" ||
		result.Items[0].ActualAmount != "446.40" || len(result.Items[0].Items) != 1 ||
		result.Items[0].Items[0].SKUNo != "SKU001" ||
		result.Items[0].Items[0].ProductName != "商品" ||
		result.Items[0].Items[0].Quantity != "2" ||
		result.Items[0].Items[0].ActualAmount != "446.40" ||
		result.Items[0].Items[0].Type == nil || *result.Items[0].Items[0].Type != "1" ||
		result.Items[0].Items[0].DocNo != "B001" ||
		result.Items[0].Items[0].AmtAcc == nil || *result.Items[0].Items[0].AmtAcc != "446.40" ||
		result.Items[0].Items[0].Value1 != "中灰" ||
		result.Items[0].Items[0].Value2 != "130CM" ||
		result.Items[0].Items[0].MarkDis == nil || *result.Items[0].Items[0].MarkDis != "0" ||
		result.Items[0].Items[0].Discount == nil || *result.Items[0].Items[0].Discount != "0.8928" ||
		result.Items[0].Items[0].PriceList == nil || *result.Items[0].Items[0].PriceList != "250.00" ||
		result.Items[0].Items[0].ProductColor != "SKU001-COLOR" ||
		result.Items[0].Items[0].TotalAmtAcc == nil || *result.Items[0].Items[0].TotalAmtAcc != "446.40" ||
		result.Items[0].Items[0].TotalListAmount == nil || *result.Items[0].Items[0].TotalListAmount != "500.00" ||
		result.Items[0].Items[0].DMAmtRetail != nil ||
		result.Items[0].Items[0].ActualPrice == nil || *result.Items[0].Items[0].ActualPrice != "223.20" ||
		result.Items[0].Items[0].ProductValue != "儿童|长裤" ||
		!result.Items[0].ItemsMeta.Complete || result.Items[0].ItemsMeta.Status != "COMPLETE" ||
		result.Items[0].ItemsMeta.ExpectedCount == nil || *result.Items[0].ItemsMeta.ExpectedCount != 1 ||
		result.Items[0].ItemsMeta.SourceCount == nil || *result.Items[0].ItemsMeta.SourceCount != 1 ||
		result.Items[0].ItemsMeta.ReturnedCount != 1 ||
		len(result.Items[0].Payments) != 1 ||
		result.Items[0].Payments[0].PaymentMethodName != "支付宝" ||
		result.Items[0].Payments[0].Amount == nil || *result.Items[0].Payments[0].Amount != "446.40" ||
		!result.Items[0].PaymentsMeta.Complete || result.Items[0].PaymentsMeta.Status != "COMPLETE" ||
		result.Items[0].PaymentsMeta.SourceCount == nil || *result.Items[0].PaymentsMeta.SourceCount != 1 ||
		result.Items[0].PaymentsMeta.ReturnedCount != 1 {
		t.Fatalf("result=%+v", result)
	}
	if !result.Pagination.HasMore || result.Pagination.NextCursor == "" {
		t.Fatalf("pagination=%+v", result.Pagination)
	}
	if result.Pagination.Page != 1 || result.Pagination.PageSize != 1 || result.Pagination.CurrentItems != 1 ||
		result.Pagination.TotalItems != 2 || result.Pagination.TotalPages != 2 {
		t.Fatalf("pagination totals=%+v", result.Pagination)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(payload), `"order_phone":"18616613488"`) {
		t.Fatalf("response missing order_phone: %s", payload)
	}
	for _, field := range []string{
		`"type":"1"`, `"docNo":"B001"`, `"amtAcc":"446.40"`, `"value1":"中灰"`,
		`"value2":"130CM"`, `"markDis":"0"`, `"discount":"0.8928"`,
		`"priceList":"250.00"`, `"productColor":"SKU001-COLOR"`,
		`"totalAmtAcc":"446.40"`, `"totalListAmount":"500.00"`, `"dmAmtRetail":null`,
		`"actualPrice":"223.20"`, `"productValue":"儿童|长裤"`,
		`"itemsMeta":{"complete":true,"status":"COMPLETE","expectedCount":1,"sourceCount":1,"returnedCount":1}`,
		`"payments":[`, `"paymentMethodName":"支付宝"`, `"amount":"446.40"`,
		`"paymentsMeta":{"complete":true,"status":"COMPLETE","expectedCount":null,"sourceCount":1,"returnedCount":1}`,
	} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("response missing %s: %s", field, payload)
		}
	}
	for _, sensitive := range []string{"member-secret", "must-not-leak", "vipno", "cPaywayId"} {
		if strings.Contains(string(payload), sensitive) {
			t.Fatalf("response leaked %q: %s", sensitive, payload)
		}
	}
	cursor, err := decodeOpenBojunOrderCursor(result.Pagination.NextCursor)
	if err != nil || cursor.Version != 2 || len(cursor.QueryHash) != sha256.Size*2 ||
		cursor.CompletedAtUnix != completedAt.Unix() || cursor.BillDate != 0 || cursor.ID != 9 || cursor.Page != 2 {
		t.Fatalf("cursor=%+v error=%v", cursor, err)
	}
}

func TestOpenBojunOrderQueryServiceRejectsInvalidFiltersBeforeDAO(t *testing.T) {
	orders := &fakeOpenBojunOrderReader{}
	service := newOpenBojunOrderQueryService(
		orders,
		&fakeOpenBojunPermissionReader{allowed: true},
		time.Now,
	)
	tests := []requestbody.OpenBojunOrderQueryRequest{
		{},
		{StartTime: "2026-07-01 00:00:00"},
		{StartTime: "2026-07-31 00:00:00", EndTime: "2026-07-01 00:00:00"},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-08-01 00:00:01"},
		{StartTime: "2026-07-01", EndTime: "2026-07-31 00:00:00"},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-07-31 00:00:00", MallCodes: []string{"bad code"}},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-07-31 00:00:00", MallCodes: []string{"A"}, StoreCodes: []string{"B"}},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-07-31 00:00:00", StartDate: "2026-07-01", EndDate: "2026-07-31"},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-07-31 00:00:00", OrderTypes: []string{"OTHER"}},
		{StartTime: "2026-07-01 00:00:00", EndTime: "2026-07-31 00:00:00", PageSize: 101},
	}
	for _, request := range tests {
		if _, err := service.Query(t.Context(), 17, request); err == nil {
			t.Fatalf("Query(%+v) error=nil", request)
		}
	}
	if orders.calls != 0 {
		t.Fatalf("DAO calls=%d", orders.calls)
	}
}

func TestOpenBojunOrderQueryServiceAllowsOmittedMallCodes(t *testing.T) {
	orders := &fakeOpenBojunOrderReader{orders: []model.BojunRetailOrder{}}
	service := newOpenBojunOrderQueryService(
		orders,
		&fakeOpenBojunPermissionReader{allowed: true},
		time.Now,
	)
	result, err := service.Query(t.Context(), 17, requestbody.OpenBojunOrderQueryRequest{
		StartTime: "2026-07-11 00:00:00",
		EndTime:   "2026-07-12 00:00:00",
	})
	if err != nil {
		t.Fatalf("Query() error=%v", err)
	}
	if len(orders.query.StoreCodes) != 0 || result.Pagination.CurrentItems != 0 || result.Items == nil {
		t.Fatalf("query=%+v result=%+v", orders.query, result)
	}
}

func TestNormalizeOpenBojunOrderQueryAcceptsLegacyAliases(t *testing.T) {
	query, page, pageSize, err := normalizeOpenBojunOrderQuery(requestbody.OpenBojunOrderQueryRequest{
		StartDate:  "2026-07-01",
		EndDate:    "2026-07-31",
		StoreCodes: []string{"abcn001p012"},
	})
	if err != nil {
		t.Fatalf("normalizeOpenBojunOrderQuery() error=%v", err)
	}
	if query.StartBillDate != 20260701 || query.EndBillDate != 20260731 ||
		len(query.StoreCodes) != 1 || query.StoreCodes[0] != "ABCN001P012" || page != 1 || pageSize != 50 {
		t.Fatalf("query=%+v page=%d pageSize=%d", query, page, pageSize)
	}
}

func TestNormalizeOpenBojunOrderQueryRejectsCursorAfterFiltersChange(t *testing.T) {
	request := requestbody.OpenBojunOrderQueryRequest{
		StartTime: "2026-07-11 00:00:00",
		EndTime:   "2026-07-12 00:00:00",
		MallCodes: []string{"ABCN001P014"},
		PageSize:  10,
	}
	query, _, pageSize, err := normalizeOpenBojunOrderQuery(request)
	if err != nil {
		t.Fatalf("normalize initial query: %v", err)
	}
	completedAt := time.Date(2026, 7, 11, 10, 31, 22, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	request.Cursor, err = encodeOpenBojunOrderCursor(openBojunOrderCursor{
		Version:         2,
		QueryHash:       openBojunOrderQueryHash(query, pageSize),
		CompletedAtUnix: completedAt.Unix(),
		ID:              9,
		Page:            2,
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	request.PageSize = 20
	if _, _, _, err := normalizeOpenBojunOrderQuery(request); err == nil {
		t.Fatal("normalizeOpenBojunOrderQuery() accepted a cursor for different filters")
	}
}

func TestNormalizeOpenBojunOrderQueryRestoresCompletedAtCursor(t *testing.T) {
	request := requestbody.OpenBojunOrderQueryRequest{
		StartTime: "2026-07-11 00:00:00",
		EndTime:   "2026-07-12 00:00:00",
		PageSize:  10,
	}
	query, _, pageSize, err := normalizeOpenBojunOrderQuery(request)
	if err != nil {
		t.Fatalf("normalize initial query: %v", err)
	}
	completedAt := time.Date(2026, 7, 11, 10, 31, 22, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	request.Cursor, err = encodeOpenBojunOrderCursor(openBojunOrderCursor{
		Version:         2,
		QueryHash:       openBojunOrderQueryHash(query, pageSize),
		CompletedAtUnix: completedAt.Unix(),
		ID:              9,
		Page:            2,
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	continued, page, _, err := normalizeOpenBojunOrderQuery(request)
	if err != nil {
		t.Fatalf("normalize continued query: %v", err)
	}
	if continued.BeforeCompletedAt == nil || !continued.BeforeCompletedAt.Equal(completedAt) ||
		continued.BeforeID != 9 || page != 2 {
		t.Fatalf("query=%+v page=%d", continued, page)
	}
}

func TestNormalizeOpenBojunOrderQueryRejectsLegacyCursorForCompletedAtQuery(t *testing.T) {
	cursor, err := encodeOpenBojunOrderCursor(openBojunOrderCursor{
		BillDate: 20260711,
		ID:       9,
		Page:     2,
	})
	if err != nil {
		t.Fatalf("encode legacy cursor: %v", err)
	}
	_, _, _, err = normalizeOpenBojunOrderQuery(requestbody.OpenBojunOrderQueryRequest{
		StartTime: "2026-07-11 00:00:00",
		EndTime:   "2026-07-12 00:00:00",
		Cursor:    cursor,
	})
	if err == nil {
		t.Fatal("normalizeOpenBojunOrderQuery() accepted a legacy cursor for completed-at mode")
	}
}

func TestFormatOpenBojunCompletedAtUsesShanghaiTime(t *testing.T) {
	completedAt := time.Date(2026, 7, 11, 2, 31, 22, 0, time.UTC)
	formatted := formatOpenBojunCompletedAt(&completedAt)
	if formatted == nil || *formatted != "2026-07-11 10:31:22" {
		t.Fatalf("formatted=%v", formatted)
	}
}

func TestOpenBojunOrderQueryServiceRequiresDedicatedPermission(t *testing.T) {
	orders := &fakeOpenBojunOrderReader{}
	service := newOpenBojunOrderQueryService(
		orders,
		&fakeOpenBojunPermissionReader{allowed: false},
		time.Now,
	)
	_, err := service.Query(t.Context(), 17, requestbody.OpenBojunOrderQueryRequest{})
	if err == nil || !errors.Is(err, ErrOpenBojunOrderForbidden) || orders.calls != 0 {
		t.Fatalf("error=%v DAO calls=%d", err, orders.calls)
	}
}

func TestOpenBojunOrderLinesFailsClosedOnInvalidJSON(t *testing.T) {
	items, meta := openBojunOrderLines(`{"vipno":"secret"}`)
	if items == nil || len(items) != 0 || meta.Complete || meta.Status != openBojunDetailInvalid || meta.SourceCount != nil {
		t.Fatalf("items=%+v meta=%+v", items, meta)
	}
}

func TestOpenBojunOrderPaymentsFailsClosedOnInvalidJSON(t *testing.T) {
	payments, meta := openBojunOrderPayments(`{"cPaywayName":"支付宝"}`)
	if payments == nil || len(payments) != 0 || meta.Complete || meta.Status != openBojunDetailInvalid || meta.SourceCount != nil {
		t.Fatalf("payments=%+v meta=%+v", payments, meta)
	}
}

func TestOpenBojunOrderDTOReportsItemCountMismatch(t *testing.T) {
	dto := openBojunOrderDTO(&model.BojunRetailOrder{TotalLines: 2, ItemsJSON: `[{"no":"SKU"}]`, PayItemsJSON: `[]`})
	if dto.ItemsMeta.Complete || dto.ItemsMeta.Status != openBojunDetailCountMismatch ||
		dto.ItemsMeta.ExpectedCount == nil || *dto.ItemsMeta.ExpectedCount != 2 ||
		dto.ItemsMeta.SourceCount == nil || *dto.ItemsMeta.SourceCount != 1 || dto.ItemsMeta.ReturnedCount != 1 {
		t.Fatalf("items meta=%+v", dto.ItemsMeta)
	}
}

func TestOpenBojunDetailMetaReportsEmptyMissingLargeAndTruncatedPayloads(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantStatus   string
		wantComplete bool
		wantSource   *int
		wantReturned int
	}{
		{name: "empty array", raw: `[]`, wantStatus: openBojunDetailComplete, wantComplete: true, wantSource: intPointer(0)},
		{name: "missing", raw: "", wantStatus: openBojunDetailMissing},
		{name: "null", raw: "null", wantStatus: openBojunDetailMissing},
		{name: "too large", raw: strings.Repeat(" ", openBojunOrderMaxItemsBytes+1), wantStatus: openBojunDetailTooLarge},
		{name: "truncated", raw: bojunDetailJSON(t, openBojunOrderMaxLines+1), wantStatus: openBojunDetailTruncated, wantSource: intPointer(openBojunOrderMaxLines + 1), wantReturned: openBojunOrderMaxLines},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, meta := openBojunOrderLines(tt.raw)
			if meta.Status != tt.wantStatus || meta.Complete != tt.wantComplete || meta.ReturnedCount != tt.wantReturned ||
				!equalOptionalInt(meta.SourceCount, tt.wantSource) || len(items) != tt.wantReturned {
				t.Fatalf("items=%d meta=%+v", len(items), meta)
			}
		})
	}
}

func bojunDetailJSON(t *testing.T, count int) string {
	t.Helper()
	values := make([]map[string]interface{}, count)
	for index := range values {
		values[index] = map[string]interface{}{"no": "SKU"}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal detail JSON: %v", err)
	}
	return string(payload)
}

func intPointer(value int) *int { return &value }

func equalOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func TestFormatOpenBojunOrderNullableNumberRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "nil", value: nil},
		{name: "invalid string", value: "abc"},
		{name: "NaN", value: "NaN"},
		{name: "boolean", value: true},
		{name: "object", value: map[string]interface{}{"bad": true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatOpenBojunOrderNullableNumber(test.value, 2); got != nil {
				t.Fatalf("formatOpenBojunOrderNullableNumber(%v)=%q, want nil", test.value, *got)
			}
		})
	}
	zero := formatOpenBojunOrderNullableNumber(0, 2)
	if zero == nil || *zero != "0.00" {
		t.Fatalf("zero=%v", zero)
	}
}
