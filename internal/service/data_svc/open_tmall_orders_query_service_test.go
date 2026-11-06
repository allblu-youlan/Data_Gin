package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
)

func TestTmallOrdersServiceNormalizesQueryAndPaginates(t *testing.T) {
	permissions := &tmallTestPermissions{allowed: true}
	size := 1
	service := newOpenTmallQueryService(func(ctx context.Context, dataset reportoracle.TmallDataset, q reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
		if dataset != reportoracle.TmallOrders || q.TimeField != "created" || q.StartTime != "2026-11-06 00:00:00" || q.EndTime != "2026-11-07 00:00:00" || q.PageSize != 1 {
			t.Fatalf("query=%+v dataset=%s", q, dataset)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("missing deadline")
		}
		payment := "123456789.12345678"
		return []reportoracle.TmallRow{{"PAYMENT": &payment}, {"PAYMENT": nil}}, nil
	}, permissions, time.Now)
	result, err := service.Query(t.Context(), 17, reportoracle.TmallOrders, requestbody.OpenTmallQueryRequest{StartTime: " 2026-11-06 00:00:00 ", EndTime: "2026-11-07 00:00:00", PageSize: &size})
	if err != nil {
		t.Fatal(err)
	}
	if permissions.permission != "tmall.orders.read" || len(result.Items) != 1 || !result.Pagination.HasMore || *result.Items[0]["PAYMENT"] != "123456789.12345678" {
		t.Fatalf("result=%+v permission=%s", result, permissions.permission)
	}
}

func TestTmallOrdersServiceRejectsMissingRangeAndUnauthorized(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		service := newOpenTmallQueryService(func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
			t.Fatal("rejected request reached Oracle")
			return nil, nil
		}, &tmallTestPermissions{allowed: allowed}, time.Now)
		_, err := service.Query(t.Context(), 17, reportoracle.TmallOrders, requestbody.OpenTmallQueryRequest{})
		want := ErrOpenTmallForbidden
		if allowed {
			want = ErrOpenTmallInvalidQuery
		}
		if !errors.Is(err, want) {
			t.Fatalf("error=%v want=%v", err, want)
		}
	}
}
