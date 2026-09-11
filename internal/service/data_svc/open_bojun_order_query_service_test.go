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
			ItemsJSON:      `[{"no":"SKU001","mProductName":"商品","qty":2,"totAmtActual":446.4,"vipno":"secret"}]`,
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
		result.Items[0].Items[0].SKUNo != "SKU001" {
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
	for _, sensitive := range []string{"member-secret", "must-not-leak", "vipno"} {
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
	items := openBojunOrderLines(`{"vipno":"secret"}`)
	if items == nil || len(items) != 0 {
		t.Fatalf("items=%+v", items)
	}
}
