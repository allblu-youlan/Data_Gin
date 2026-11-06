package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

type tmallTestPermissions struct {
	allowed    bool
	err        error
	permission string
}

func (p *tmallTestPermissions) HasPermission(_ context.Context, _ uint, permission string, _ time.Time) (bool, error) {
	p.permission = permission
	return p.allowed, p.err
}

func TestOpenTmallQueryAuthorizesEachDatasetAndPaginates(t *testing.T) {
	for _, test := range []struct {
		dataset    reportoracle.TmallDataset
		permission string
	}{
		{reportoracle.TmallSycm, model.PermissionTmallSycmRead},
		{reportoracle.TmallQztg, model.PermissionTmallQztgRead},
		{reportoracle.TmallTot, model.PermissionTmallTotRead},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			permissions := &tmallTestPermissions{allowed: true}
			page, size := 2, 1
			calls := 0
			service := newOpenTmallQueryService(func(ctx context.Context, dataset reportoracle.TmallDataset, query reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
				calls++
				if dataset != test.dataset || query.StartDate != "2026-10-01" || query.EndDate != "2026-10-31" || query.Offset != 1 || query.PageSize != 1 {
					t.Fatalf("unexpected query: %s %+v", dataset, query)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("query must have a bounded deadline")
				}
				return []reportoracle.TmallRow{{}, {}}, nil
			}, permissions, time.Now)
			result, err := service.Query(t.Context(), 17, test.dataset, requestbody.OpenTmallQueryRequest{StartDate: " 2026-10-01 ", EndDate: "2026-10-31", Page: &page, PageSize: &size})
			if err != nil {
				t.Fatal(err)
			}
			if permissions.permission != test.permission || calls != 1 || len(result.Items) != 1 || len(result.Columns) == 0 || !reflect.DeepEqual(result.Pagination, OpenBojunQueryPagination{Page: 2, PageSize: 1, HasMore: true}) {
				t.Fatalf("unexpected result: permission=%s result=%+v calls=%d", permissions.permission, result, calls)
			}
		})
	}
}

func TestOpenTmallQueryRejectsInvalidDateRangeBeforeOracle(t *testing.T) {
	zero, tooLarge := 0, 101
	for _, request := range []requestbody.OpenTmallQueryRequest{
		{},
		{StartDate: "2026-02-30", EndDate: "2026-03-01"},
		{StartDate: "2026-10-02", EndDate: "2026-10-01"},
		{StartDate: "2026-09-01", EndDate: "2026-10-31"},
		{StartDate: "2026-10-01", EndDate: "2026-10-01", Page: &zero},
		{StartDate: "2026-10-01", EndDate: "2026-10-01", PageSize: &tooLarge},
	} {
		service := newOpenTmallQueryService(func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
			t.Fatal("invalid input must not query Oracle")
			return nil, nil
		}, &tmallTestPermissions{allowed: true}, time.Now)
		if _, err := service.Query(t.Context(), 17, reportoracle.TmallSycm, request); !errors.Is(err, ErrOpenTmallInvalidQuery) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

func TestOpenTmallQueryDeniesUnauthorizedAndClassifiesOutages(t *testing.T) {
	request := requestbody.OpenTmallQueryRequest{StartDate: "2026-10-01", EndDate: "2026-10-01"}
	for _, test := range []struct {
		name                    string
		actor                   uint
		allowed                 bool
		authErr, queryErr, want error
		wantCalls               int
	}{
		{"anonymous", 0, true, nil, nil, ErrOpenTmallForbidden, 0},
		{"not granted", 17, false, nil, nil, ErrOpenTmallForbidden, 0},
		{"permission store offline", 17, true, errors.New("offline"), nil, ErrOpenTmallUnavailable, 0},
		{"Oracle offline", 17, true, nil, errors.New("offline"), ErrOpenTmallUnavailable, 1},
		{"empty result", 17, true, nil, nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service := newOpenTmallQueryService(func(context.Context, reportoracle.TmallDataset, reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
				calls++
				return nil, test.queryErr
			}, &tmallTestPermissions{allowed: test.allowed, err: test.authErr}, time.Now)
			result, err := service.Query(t.Context(), test.actor, reportoracle.TmallSycm, request)
			if !errors.Is(err, test.want) || calls != test.wantCalls {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if err == nil && (result.Items == nil || result.Pagination.HasMore) {
				t.Fatalf("empty result=%+v", result)
			}
		})
	}
}

func TestOpenTmallQueryOmitsItemNoFromResponse(t *testing.T) {
	for _, dataset := range []reportoracle.TmallDataset{
		reportoracle.TmallSycm, reportoracle.TmallQztg, reportoracle.TmallTot,
		reportoracle.TmallQztgHourly, reportoracle.TmallSycmHourly,
	} {
		t.Run(string(dataset), func(t *testing.T) {
			id, itemNo := "9007199254740993", "A01"
			request := requestbody.OpenTmallQueryRequest{StartDate: "2026-10-01", EndDate: "2026-10-01"}
			if dataset == reportoracle.TmallQztgHourly || dataset == reportoracle.TmallSycmHourly {
				request = requestbody.OpenTmallQueryRequest{BatchNo: "202611041300"}
			}
			if dataset == reportoracle.TmallSycm || dataset == reportoracle.TmallTot {
				request.Filters = map[string]string{"ITEM_NO": itemNo}
			}
			originalColumns := reportoracle.TmallColumns(dataset)
			service := newOpenTmallQueryService(
				func(_ context.Context, _ reportoracle.TmallDataset, query reportoracle.TmallQuery) ([]reportoracle.TmallRow, error) {
					if request.Filters != nil && query.Filters["ITEM_NO"] != itemNo {
						t.Fatal("ITEM_NO filtering must remain supported")
					}
					return []reportoracle.TmallRow{
						{"ID": &id, "ITEM_NO": &itemNo},
						{"ID": &id, "ITEM_NO": nil},
					}, nil
				},
				&tmallTestPermissions{allowed: true},
				time.Now,
			)
			result, err := service.Query(t.Context(), 17, dataset, request)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != 2 {
				t.Fatalf("items=%+v", result.Items)
			}
			for _, item := range result.Items {
				if _, exists := item["ITEM_NO"]; exists {
					t.Error("ITEM_NO must not appear in response items, including NULL values")
				}
				if item["ID"] == nil || *item["ID"] != id {
					t.Fatalf("other fields must be preserved: %+v", item)
				}
			}
			wantColumns := make([]reportoracle.TmallColumn, 0, len(originalColumns))
			for _, column := range originalColumns {
				if column.Name != "ITEM_NO" {
					wantColumns = append(wantColumns, column)
				}
			}
			if !reflect.DeepEqual(result.Columns, wantColumns) {
				t.Fatalf("response columns=%+v, want=%+v", result.Columns, wantColumns)
			}
			if !reflect.DeepEqual(reportoracle.TmallColumns(dataset), originalColumns) {
				t.Fatal("the internal report column catalog must remain unchanged")
			}
		})
	}
}
