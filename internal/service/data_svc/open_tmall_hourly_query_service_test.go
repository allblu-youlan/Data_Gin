package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

func TestOpenTmallHourlyQueryAuthorizesAndPreservesBatch(t *testing.T) {
	for _, test := range []struct {
		dataset    reportoracle.TmallDataset
		permission string
		columns    int
	}{
		{reportoracle.TmallQztgHourly, model.PermissionTmallQztgRead, 92},
		{reportoracle.TmallSycmHourly, model.PermissionTmallSycmRead, 47},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			permissions := &tmallTestPermissions{allowed: true}
			page, size, calls := 2, 1, 0
			service := newOpenTmallQueryService(func(ctx context.Context, dataset reportoracle.TmallDataset, query reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
				calls++
				if dataset != test.dataset || query.BatchNo != "202611041300" || query.StartDate != "" || query.EndDate != "" || query.Filters["PRODUCT_ID"] != "00123" || query.Offset != 1 || query.PageSize != 1 {
					t.Fatalf("unexpected query: %s %+v", dataset, query)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("query must have a bounded deadline")
				}
				return []reportoracle.TmallRow{{"PAY_AMT": nil}, {}}, nil
			}, permissions, time.Now)
			result, err := service.Query(t.Context(), 17, test.dataset, requestbody.OpenTmallQueryRequest{
				BatchNo: "202611041300", Filters: map[string]string{"PRODUCT_ID": "00123"}, Page: &page, PageSize: &size,
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || permissions.permission != test.permission || len(result.Items) != 1 || len(result.Columns) != test.columns || result.Pagination != (OpenBojunQueryPagination{Page: 2, PageSize: 1, HasMore: true}) {
				t.Fatalf("permission=%s calls=%d result=%+v", permissions.permission, calls, result)
			}
		})
	}
}

func TestOpenTmallHourlyQueryRejectsInvalidBatchBeforeOracle(t *testing.T) {
	for _, dataset := range []reportoracle.TmallDataset{reportoracle.TmallQztgHourly, reportoracle.TmallSycmHourly} {
		for _, batch := range []string{"", "2026110413", "202611041330", "202602301300", "202611042400"} {
			t.Run(string(dataset)+"/"+batch, func(t *testing.T) {
				service := newOpenTmallQueryService(func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
					t.Fatal("invalid batch must not query Oracle")
					return nil, nil
				}, &tmallTestPermissions{allowed: true}, time.Now)
				if _, err := service.Query(t.Context(), 17, dataset, requestbody.OpenTmallQueryRequest{BatchNo: batch}); !errors.Is(err, ErrOpenTmallInvalidQuery) {
					t.Fatalf("error=%v", err)
				}
			})
		}
	}
}

func TestOpenTmallHourlyQueryHandlesAccessOutageAndEmptyResult(t *testing.T) {
	for _, test := range []struct {
		name           string
		allowed        bool
		queryErr, want error
		wantCalls      int
	}{
		{"not granted", false, nil, ErrOpenTmallForbidden, 0},
		{"Oracle unavailable", true, errors.New("offline"), ErrOpenTmallUnavailable, 1},
		{"no data for batch", true, nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service := newOpenTmallQueryService(func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
				calls++
				return nil, test.queryErr
			}, &tmallTestPermissions{allowed: test.allowed}, time.Now)
			result, err := service.Query(t.Context(), 17, reportoracle.TmallSycmHourly, requestbody.OpenTmallQueryRequest{BatchNo: "202611041300"})
			if !errors.Is(err, test.want) || calls != test.wantCalls {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if err == nil && (result.Items == nil || result.Pagination != (OpenBojunQueryPagination{Page: 1, PageSize: 50})) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
