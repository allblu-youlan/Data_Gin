package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

type monitoringTestMalls struct {
	mall *model.Mall
	err  error
	code string
}

func (m *monitoringTestMalls) FindByCode(_ context.Context, code string) (*model.Mall, error) {
	m.code = code
	return m.mall, m.err
}

type monitoringTestScope struct {
	allowed bool
	err     error
	mallID  uint
}

func (s *monitoringTestScope) CanAccess(_ context.Context, _ uint, mallID uint) (bool, error) {
	s.mallID = mallID
	return s.allowed, s.err
}

func monitoringTestService(t *testing.T) *OpenMonitoringQueryService {
	t.Helper()
	return &OpenMonitoringQueryService{
		permissions: &tmallTestPermissions{allowed: true},
		malls:       &monitoringTestMalls{mall: &model.Mall{BaseModel: model.BaseModel{ID: 7}, MallCode: "SH01", NameCN: "上海店", KaiyuShopID: "kaiyu-42"}},
		scopes:      &monitoringTestScope{allowed: true}, now: time.Now,
		hours: func(context.Context, string, uint, []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
			return nil, nil
		},
		areas: func(context.Context, string, uint, time.Time, string) ([]reportoracle.MonitoringAreaRow, error) {
			return nil, nil
		},
	}
}

func TestOpenMonitoringNormalizesDatesAndResolvesBusinessCode(t *testing.T) {
	s := monitoringTestService(t)
	s.hours = func(ctx context.Context, shop string, mallID uint, dates []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
		if shop != "kaiyu-42" || mallID != 7 || len(dates) != 2 || dates[0].Format(time.DateOnly) != "2026-11-01" || dates[1].Format(time.DateOnly) != "2026-11-03" || dates[0].Location().String() != "Asia/Shanghai" {
			t.Fatalf("incorrect mapped query: %s %d %v", shop, mallID, dates)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("missing bounded query deadline")
		}
		return []reportoracle.MonitoringHourlyRow{{HourStart: dates[0].Add(9 * time.Hour), Traffic: 12}}, nil
	}
	result, err := s.QueryHourly(t.Context(), 17, requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: " sh01 ", Dates: []string{"2026-11-03", " 2026-11-01 ", "2026-11-03"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ShopCode != "SH01" || result.Timezone != "Asia/Shanghai" || !reflect.DeepEqual(result.MissingDates, []string{"2026-11-03"}) || len(result.Items) != 1 || result.Coverage[0].Hours != 1 || result.Coverage[0].Complete {
		t.Fatalf("unexpected result: %+v", result)
	}
	if s.permissions.(*tmallTestPermissions).permission != model.PermissionMonitoringHourlyRead || s.malls.(*monitoringTestMalls).code != "SH01" || s.scopes.(*monitoringTestScope).mallID != 7 {
		t.Fatal("authorization or mapping bypassed")
	}
}

func TestOpenMonitoringRejectsInvalidQueriesBeforeOracle(t *testing.T) {
	for _, request := range []requestbody.OpenMonitoringHourlyQueryRequest{
		{ShopCode: "SH01"},
		{ShopCode: "SH01", Dates: []string{"2026-02-30"}},
		{ShopCode: "SH01", Dates: []string{"2026-11-01"}, StartDate: "2026-11-01", EndDate: "2026-11-02"},
		{ShopCode: "SH01", StartDate: "2026-11-02", EndDate: "2026-11-01"},
		{ShopCode: "SH01", StartDate: "2026-10-01", EndDate: "2026-11-01"},
		{ShopCode: "SH01", StartDate: "2026-11-01"},
		{Dates: []string{"2026-11-01"}},
	} {
		s := monitoringTestService(t)
		s.hours = func(context.Context, string, uint, []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
			t.Fatal("invalid request reached Oracle")
			return nil, nil
		}
		if _, err := s.QueryHourly(t.Context(), 17, request); !errors.Is(err, ErrOpenMonitoringInvalidQuery) {
			t.Fatalf("request=%+v err=%v", request, err)
		}
	}
}

func TestOpenMonitoringDailySumsHourlyCountsWithoutInventingMissingDays(t *testing.T) {
	s := monitoringTestService(t)
	s.hours = func(_ context.Context, _ string, _ uint, days []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
		rows := make([]reportoracle.MonitoringHourlyRow, 24)
		for i := range rows {
			rows[i] = reportoracle.MonitoringHourlyRow{HourStart: days[0].Add(time.Duration(i) * time.Hour), Passby: 2, Traffic: 3, TrafficCount: 4, TrafficBatch: 5}
		}
		return rows, nil
	}
	result, err := s.QueryDaily(t.Context(), 17, requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: "SH01", StartDate: "2026-11-01", EndDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || !result.Items[0].Complete || result.Items[0].Hours != 24 || result.Items[0].Passby != 48 || result.Items[0].Traffic != 72 || result.Items[0].TrafficCount != 96 || result.Items[0].TrafficBatch != 120 || !reflect.DeepEqual(result.MissingDates, []string{"2026-11-02"}) {
		t.Fatalf("result=%+v", result)
	}
	if s.permissions.(*tmallTestPermissions).permission != model.PermissionMonitoringDailyRead {
		t.Fatal("wrong daily permission")
	}
}

func TestOpenMonitoringAreasBindDateAndRegion(t *testing.T) {
	s := monitoringTestService(t)
	s.areas = func(ctx context.Context, shop string, mallID uint, day time.Time, region string) ([]reportoracle.MonitoringAreaRow, error) {
		if shop != "kaiyu-42" || mallID != 7 || day.Format(time.DateOnly) != "2026-11-01" || day.Hour() != 0 || region != "试衣区" {
			t.Fatalf("incorrect area query: %s %d %v %q", shop, mallID, day, region)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing timeout")
		}
		return nil, nil
	}
	result, err := s.QueryAreas(t.Context(), 17, requestbody.OpenMonitoringAreaQueryRequest{ShopCode: "SH01", Date: "2026-11-01", RegionName: " 试衣区 "})
	if err != nil || result.Items == nil || result.HasData || result.Date != "2026-11-01" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if s.permissions.(*tmallTestPermissions).permission != model.PermissionMonitoringAreaRead {
		t.Fatal("wrong area permission")
	}
}

func TestOpenMonitoringAuthorizationAndFailureClassification(t *testing.T) {
	offline := errors.New("offline")
	for _, tc := range []struct {
		name  string
		setup func(*OpenMonitoringQueryService)
		actor uint
		want  error
	}{
		{"anonymous", func(*OpenMonitoringQueryService) {}, 0, ErrOpenMonitoringForbidden},
		{"permission denied", func(s *OpenMonitoringQueryService) { s.permissions.(*tmallTestPermissions).allowed = false }, 17, ErrOpenMonitoringForbidden},
		{"permission unavailable", func(s *OpenMonitoringQueryService) { s.permissions.(*tmallTestPermissions).err = offline }, 17, ErrOpenMonitoringUnavailable},
		{"missing shop", func(s *OpenMonitoringQueryService) { s.malls.(*monitoringTestMalls).err = data_dao.ErrMallNotFound }, 17, ErrOpenMonitoringNotFound},
		{"missing mapping", func(s *OpenMonitoringQueryService) { s.malls.(*monitoringTestMalls).mall.KaiyuShopID = "" }, 17, ErrOpenMonitoringNotFound},
		{"shop unavailable", func(s *OpenMonitoringQueryService) { s.malls.(*monitoringTestMalls).err = offline }, 17, ErrOpenMonitoringUnavailable},
		{"scope denied", func(s *OpenMonitoringQueryService) { s.scopes.(*monitoringTestScope).allowed = false }, 17, ErrOpenMonitoringForbidden},
		{"scope unavailable", func(s *OpenMonitoringQueryService) { s.scopes.(*monitoringTestScope).err = offline }, 17, ErrOpenMonitoringUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := monitoringTestService(t)
			tc.setup(s)
			s.hours = func(context.Context, string, uint, []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
				t.Fatal("rejected request reached Oracle")
				return nil, nil
			}
			_, err := s.QueryHourly(t.Context(), tc.actor, requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: "SH01", Dates: []string{"2026-11-01"}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestOpenMonitoringClassifiesOracleErrors(t *testing.T) {
	for _, tc := range []struct{ queryErr, want error }{{errors.New("oracle offline"), ErrOpenMonitoringUnavailable}, {reportoracle.ErrMonitoringTooManyRows, ErrOpenMonitoringInvalidQuery}, {context.Canceled, ErrOpenMonitoringUnavailable}} {
		s := monitoringTestService(t)
		s.hours = func(context.Context, string, uint, []time.Time) ([]reportoracle.MonitoringHourlyRow, error) {
			return nil, tc.queryErr
		}
		_, err := s.QueryDaily(t.Context(), 17, requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: "SH01", Dates: []string{"2026-11-01"}})
		if !errors.Is(err, tc.want) {
			t.Fatalf("err=%v want=%v", err, tc.want)
		}
	}
}
