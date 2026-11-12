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

type feishuAttendanceTestPermissions struct {
	allowed    bool
	err        error
	permission string
}

func (p *feishuAttendanceTestPermissions) HasPermission(_ context.Context, _ uint, permission string, _ time.Time) (bool, error) {
	p.permission = permission
	return p.allowed, p.err
}

func TestOpenFeishuAttendanceQueryAuthorizesEachDatasetAndPaginates(t *testing.T) {
	for _, test := range []struct {
		dataset    reportoracle.FeishuAttendanceDataset
		permission string
	}{
		{reportoracle.FeishuAttendanceDaily, model.PermissionFeishuAttendanceDailyRead},
		{reportoracle.FeishuAttendancePunch, model.PermissionFeishuAttendancePunchRead},
		{reportoracle.FeishuAttendanceItem, model.PermissionFeishuAttendanceItemRead},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			permissions := &feishuAttendanceTestPermissions{allowed: true}
			page, size := 2, 1
			calls := 0
			service := newOpenFeishuAttendanceQueryService(func(ctx context.Context, dataset reportoracle.FeishuAttendanceDataset, query reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error) {
				calls++
				if dataset != test.dataset || query.StartDate != "2026-10-01" || query.EndDate != "2026-10-31" || query.Offset != 1 || query.PageSize != 1 || query.Filters["USER_NAME"] != "张三" || query.Filters["DEPARTMENT_NAME"] != "运营部" {
					t.Fatalf("unexpected query: %s %+v", dataset, query)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("query must have a bounded deadline")
				}
				return []reportoracle.FeishuAttendanceRow{{}, {}}, nil
			}, permissions, time.Now)
			result, err := service.Query(t.Context(), 17, test.dataset, requestbody.OpenFeishuAttendanceQueryRequest{StartDate: " 2026-10-01 ", EndDate: "2026-10-31", Filters: map[string]string{"USER_NAME": " 张三 ", "DEPARTMENT_NAME": " 运营部 "}, Page: &page, PageSize: &size})
			if err != nil {
				t.Fatal(err)
			}
			if permissions.permission != test.permission || calls != 1 || len(result.Items) != 1 || !reflect.DeepEqual(result.Pagination, OpenBojunQueryPagination{Page: 2, PageSize: 1, HasMore: true}) {
				t.Fatalf("unexpected result: permission=%s result=%+v calls=%d", permissions.permission, result, calls)
			}
		})
	}
}

func TestOpenFeishuAttendanceQueryRejectsInvalidDateRangeBeforeOracle(t *testing.T) {
	zero, tooLarge := 0, 101
	for _, request := range []requestbody.OpenFeishuAttendanceQueryRequest{
		{},
		{StartDate: "2026-02-30", EndDate: "2026-03-01"},
		{StartDate: "2026-10-02", EndDate: "2026-10-01"},
		{StartDate: "2026-09-01", EndDate: "2026-10-31"},
		{StartDate: "2026-10-01", EndDate: "2026-10-01", Page: &zero},
		{StartDate: "2026-10-01", EndDate: "2026-10-01", PageSize: &tooLarge},
	} {
		request.Filters = map[string]string{"USER_NAME": "张三", "DEPARTMENT_NAME": "运营部"}
		service := newOpenFeishuAttendanceQueryService(func(context.Context, reportoracle.FeishuAttendanceDataset, reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error) {
			t.Fatal("invalid input must not query Oracle")
			return nil, nil
		}, &feishuAttendanceTestPermissions{allowed: true}, time.Now)
		if _, err := service.Query(t.Context(), 17, reportoracle.FeishuAttendanceDaily, request); !errors.Is(err, ErrOpenFeishuAttendanceInvalidQuery) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

func TestOpenFeishuAttendanceQueryDeniesUnauthorizedAndClassifiesOutages(t *testing.T) {
	request := requestbody.OpenFeishuAttendanceQueryRequest{StartDate: "2026-10-01", EndDate: "2026-10-01", Filters: map[string]string{"USER_NAME": "张三", "DEPARTMENT_NAME": "运营部"}}
	for _, test := range []struct {
		name                    string
		actor                   uint
		allowed                 bool
		authErr, queryErr, want error
		wantCalls               int
	}{
		{"anonymous", 0, true, nil, nil, ErrOpenFeishuAttendanceForbidden, 0},
		{"not granted", 17, false, nil, nil, ErrOpenFeishuAttendanceForbidden, 0},
		{"permission store offline", 17, true, errors.New("offline"), nil, ErrOpenFeishuAttendanceUnavailable, 0},
		{"Oracle offline", 17, true, nil, errors.New("offline"), ErrOpenFeishuAttendanceUnavailable, 1},
		{"empty result", 17, true, nil, nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service := newOpenFeishuAttendanceQueryService(func(context.Context, reportoracle.FeishuAttendanceDataset, reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error) {
				calls++
				return nil, test.queryErr
			}, &feishuAttendanceTestPermissions{allowed: test.allowed, err: test.authErr}, time.Now)
			result, err := service.Query(t.Context(), test.actor, reportoracle.FeishuAttendanceDaily, request)
			if !errors.Is(err, test.want) || calls != test.wantCalls {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if err == nil && (result.Items == nil || result.Pagination.HasMore) {
				t.Fatalf("empty result=%+v", result)
			}
		})
	}
}

func TestOpenFeishuAttendanceQueryRequiresExactNameAndDepartmentFilters(t *testing.T) {
	for _, filters := range []map[string]string{
		nil,
		{"USER_NAME": "张三"},
		{"DEPARTMENT_NAME": "运营部"},
		{"USER_NAME": "  ", "DEPARTMENT_NAME": "运营部"},
		{"USER_NAME": "张三", "DEPARTMENT_NAME": "  "},
		{"USER_NAME": "张三", "DEPARTMENT_NAME": "运营部", "USER_ID": "123"},
	} {
		service := newOpenFeishuAttendanceQueryService(func(context.Context, reportoracle.FeishuAttendanceDataset, reportoracle.FeishuAttendanceQuery) ([]reportoracle.FeishuAttendanceRow, error) {
			t.Fatal("invalid filters must not query Oracle")
			return nil, nil
		}, &feishuAttendanceTestPermissions{allowed: true}, time.Now)
		_, err := service.Query(t.Context(), 17, reportoracle.FeishuAttendanceDaily, requestbody.OpenFeishuAttendanceQueryRequest{
			StartDate: "2026-11-01", EndDate: "2026-11-11", Filters: filters,
		})
		if !errors.Is(err, ErrOpenFeishuAttendanceInvalidQuery) {
			t.Fatalf("filters=%v error=%v", filters, err)
		}
	}
}
