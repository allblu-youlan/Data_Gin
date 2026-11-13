package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
)

func TestOpenYouzanQueryAuthorizesDatasetAndPreservesValues(t *testing.T) {
	for _, test := range []struct {
		dataset    reportoracle.YouzanDataset
		permission string
	}{
		{reportoracle.YouzanOrders, "youzan.orders.read"},
		{reportoracle.YouzanOrderItems, "youzan.order_items.read"},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			permissions := &tmallTestPermissions{allowed: true}
			page, size := 2, 1
			id, amount := "12345678901234567890", "123456789.12345678"
			service := newOpenYouzanQueryService(func(ctx context.Context, dataset reportoracle.YouzanDataset, q reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error) {
				if dataset != test.dataset || q.TimeField != "modifiedDwd" || q.StartTime != "2026-11-12 00:00:00" || q.EndTime != "2026-11-13 00:00:00" || q.Offset != 1 || q.PageSize != 1 {
					t.Fatalf("dataset=%s query=%+v", dataset, q)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("query needs a bounded deadline")
				}
				return []reportoracle.YouzanRow{{"ID": &id, "PAYMENT": &amount, "UPDATE_TIME": nil}, {}}, nil
			}, permissions, time.Now)
			result, err := service.Query(t.Context(), 17, test.dataset, requestbody.OpenYouzanQueryRequest{
				StartTime: " 2026-11-12 00:00:00 ", EndTime: "2026-11-13 00:00:00", Page: &page, PageSize: &size,
			})
			if err != nil {
				t.Fatal(err)
			}
			if permissions.permission != test.permission || len(result.Items) != 1 || result.Pagination != (OpenBojunQueryPagination{Page: 2, PageSize: 1, HasMore: true}) || *result.Items[0]["ID"] != id || *result.Items[0]["PAYMENT"] != amount || result.Items[0]["UPDATE_TIME"] != nil {
				t.Fatalf("result=%+v permission=%s", result, permissions.permission)
			}
		})
	}
}

func TestOpenYouzanQueryRejectsInvalidInputBeforeOracle(t *testing.T) {
	zero, tooLarge, tooDeep := 0, 101, 1001
	for _, request := range []requestbody.OpenYouzanQueryRequest{
		{},
		{StartTime: "2026-11-12 00:00:00"},
		{Filters: map[string]string{"TID": "T1"}, Page: &zero},
		{Filters: map[string]string{"TID": "T1"}, PageSize: &tooLarge},
		{Filters: map[string]string{"TID": "T1"}, Page: &tooDeep},
		{Filters: map[string]string{"OID": "O1"}},
	} {
		service := newOpenYouzanQueryService(func(context.Context, reportoracle.YouzanDataset, reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error) {
			t.Fatal("invalid request reached Oracle")
			return nil, nil
		}, &tmallTestPermissions{allowed: true}, time.Now)
		if _, err := service.Query(t.Context(), 17, reportoracle.YouzanOrders, request); !errors.Is(err, ErrOpenYouzanInvalidQuery) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

func TestOpenYouzanQueryHandlesPermissionAndDependencyFailures(t *testing.T) {
	for _, test := range []struct {
		name                    string
		actor                   uint
		allowed                 bool
		authErr, queryErr, want error
		wantCalls               int
	}{
		{"anonymous", 0, true, nil, nil, ErrOpenYouzanForbidden, 0},
		{"ungranted", 17, false, nil, nil, ErrOpenYouzanForbidden, 0},
		{"permission store unavailable", 17, true, errors.New("offline"), nil, ErrOpenYouzanUnavailable, 0},
		{"Oracle unavailable", 17, true, nil, errors.New("offline"), ErrOpenYouzanUnavailable, 1},
		{"empty order", 17, true, nil, nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service := newOpenYouzanQueryService(func(_ context.Context, _ reportoracle.YouzanDataset, q reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error) {
				calls++
				if q.Filters["TID"] != "T1" || q.Offset != 0 || q.PageSize != 50 {
					t.Fatalf("query=%+v", q)
				}
				return nil, test.queryErr
			}, &tmallTestPermissions{allowed: test.allowed, err: test.authErr}, time.Now)
			result, err := service.Query(t.Context(), test.actor, reportoracle.YouzanOrderItems, requestbody.OpenYouzanQueryRequest{Filters: map[string]string{"TID": " T1 "}})
			if !errors.Is(err, test.want) || calls != test.wantCalls {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if err == nil && (result.Items == nil || len(result.Items) != 0 || result.Pagination.HasMore) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestOpenYouzanQueryPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := newOpenYouzanQueryService(func(ctx context.Context, _ reportoracle.YouzanDataset, _ reportoracle.YouzanQuery) ([]reportoracle.YouzanRow, error) {
		return nil, ctx.Err()
	}, &tmallTestPermissions{allowed: true}, time.Now)
	_, err := service.Query(ctx, 17, reportoracle.YouzanOrders, requestbody.OpenYouzanQueryRequest{Filters: map[string]string{"TID": "T1"}})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrOpenYouzanUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
