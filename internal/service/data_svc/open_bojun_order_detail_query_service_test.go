package data_svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

type fakeOpenBojunOrderDetailReader struct {
	order *model.BojunRetailOrder
	err   error
	calls int
}

func (reader *fakeOpenBojunOrderDetailReader) FindOpenOrderDetails(
	_ context.Context,
	_ string,
) (*model.BojunRetailOrder, error) {
	reader.calls++
	return reader.order, reader.err
}

type fakeOpenBojunOrderDetailMallScope struct {
	codes []string
	err   error
}

func (scope *fakeOpenBojunOrderDetailMallScope) ConstrainMallCodes(
	_ context.Context,
	_ uint,
	_ []string,
) ([]string, error) {
	return scope.codes, scope.err
}

func TestOpenBojunOrderDetailQueryServicePagesEveryDetail(t *testing.T) {
	items := make([]map[string]interface{}, 0, 401)
	for index := 0; index < 401; index++ {
		items = append(items, map[string]interface{}{
			"no": fmt.Sprintf("SKU-%03d", index), "qty": 1, "totAmtActual": index,
		})
	}
	payments := []map[string]interface{}{
		{"cPaywayName": "支付宝", "payamount": 10},
		{"cPaywayName": "微信", "payamount": 20},
		{"cPaywayName": "现金", "payamount": 30},
	}
	reader := &fakeOpenBojunOrderDetailReader{order: &model.BojunRetailOrder{
		DocNo: "ORDER-401", StoreCode: "STORE-1",
		ItemsJSON: mustMarshalOpenBojunDetails(t, items), PayItemsJSON: mustMarshalOpenBojunDetails(t, payments),
	}}
	service := newOpenBojunOrderDetailQueryService(
		reader,
		&fakeOpenBojunPermissionReader{allowed: true},
		&fakeOpenBojunOrderDetailMallScope{codes: []string{"STORE-1"}},
		func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) },
	)
	request := requestbody.OpenBojunOrderDetailQueryRequest{OrderNo: " ORDER-401 ", PageSize: 200}
	seen := make([]string, 0, len(items))
	for expectedPage := 1; ; expectedPage++ {
		result, err := service.Query(t.Context(), 17, request)
		if err != nil {
			t.Fatalf("Query() page=%d error=%v", expectedPage, err)
		}
		if result.OrderNo != "ORDER-401" || result.Pagination.Page != expectedPage ||
			result.Pagination.PageSize != 200 || result.Pagination.Items.TotalItems != 401 ||
			result.Pagination.Items.TotalPages != 3 || result.Pagination.Payments.TotalItems != 3 ||
			result.Pagination.Payments.TotalPages != 1 {
			t.Fatalf("page=%d result=%+v", expectedPage, result)
		}
		if expectedPage == 1 && len(result.Payments) != 3 {
			t.Fatalf("page 1 payments=%d", len(result.Payments))
		}
		if expectedPage > 1 && len(result.Payments) != 0 {
			t.Fatalf("page %d payments=%d", expectedPage, len(result.Payments))
		}
		for _, item := range result.Items {
			seen = append(seen, item.SKUNo)
		}
		if !result.Pagination.HasMore {
			if result.Pagination.NextCursor != "" {
				t.Fatalf("last page nextCursor=%q", result.Pagination.NextCursor)
			}
			break
		}
		if result.Pagination.NextCursor == "" {
			t.Fatalf("page %d missing next cursor", expectedPage)
		}
		request.Cursor = result.Pagination.NextCursor
	}
	if len(seen) != 401 {
		t.Fatalf("returned items=%d", len(seen))
	}
	for index, sku := range seen {
		expected := fmt.Sprintf("SKU-%03d", index)
		if sku != expected {
			t.Fatalf("item %d=%q want=%q", index, sku, expected)
		}
	}
	if reader.calls != 3 {
		t.Fatalf("reader calls=%d", reader.calls)
	}
}

func TestOpenBojunOrderDetailQueryServiceRejectsChangedPayload(t *testing.T) {
	reader := &fakeOpenBojunOrderDetailReader{order: &model.BojunRetailOrder{
		DocNo: "ORDER-1", StoreCode: "STORE-1",
		ItemsJSON: `[{"no":"SKU-1"},{"no":"SKU-2"}]`, PayItemsJSON: `[]`,
	}}
	service := newOpenBojunOrderDetailQueryService(
		reader,
		&fakeOpenBojunPermissionReader{allowed: true},
		&fakeOpenBojunOrderDetailMallScope{codes: []string{"STORE-1"}},
		time.Now,
	)
	first, err := service.Query(t.Context(), 17, requestbody.OpenBojunOrderDetailQueryRequest{
		OrderNo: "ORDER-1", PageSize: 1,
	})
	if err != nil || first.Pagination.NextCursor == "" {
		t.Fatalf("first page=%+v error=%v", first, err)
	}
	reader.order.ItemsJSON = `[{"no":"SKU-1"},{"no":"SKU-CHANGED"}]`
	_, err = service.Query(t.Context(), 17, requestbody.OpenBojunOrderDetailQueryRequest{
		OrderNo: "ORDER-1", PageSize: 1, Cursor: first.Pagination.NextCursor,
	})
	if !errors.Is(err, ErrOpenBojunOrderInvalidQuery) {
		t.Fatalf("changed payload error=%v", err)
	}
}

func TestOpenBojunOrderDetailQueryServiceRejectsInvalidCursorAndMissingOrder(t *testing.T) {
	unknownCursor := base64.RawURLEncoding.EncodeToString([]byte(
		`{"version":1,"queryHash":"x","payloadHash":"y","page":2,"unknown":true}`,
	))
	tests := []struct {
		name    string
		request requestbody.OpenBojunOrderDetailQueryRequest
		reader  *fakeOpenBojunOrderDetailReader
		wantErr error
	}{
		{
			name: "unknown cursor field",
			request: requestbody.OpenBojunOrderDetailQueryRequest{
				OrderNo: "ORDER-1", Cursor: unknownCursor, PageSize: 100,
			},
			reader:  &fakeOpenBojunOrderDetailReader{},
			wantErr: ErrOpenBojunOrderInvalidQuery,
		},
		{
			name:    "missing order",
			request: requestbody.OpenBojunOrderDetailQueryRequest{OrderNo: "ORDER-404"},
			reader:  &fakeOpenBojunOrderDetailReader{err: gorm.ErrRecordNotFound},
			wantErr: ErrOpenBojunOrderNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newOpenBojunOrderDetailQueryService(
				test.reader,
				&fakeOpenBojunPermissionReader{allowed: true},
				&fakeOpenBojunOrderDetailMallScope{codes: []string{"STORE-1"}},
				time.Now,
			)
			_, err := service.Query(t.Context(), 17, test.request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Query() error=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func mustMarshalOpenBojunDetails(t *testing.T, values interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	return string(encoded)
}
