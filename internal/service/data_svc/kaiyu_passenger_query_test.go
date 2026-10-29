package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
)

type kaiyuQueryScopeFunc func(context.Context, uint, []uint) ([]uint, error)

func (f kaiyuQueryScopeFunc) ConstrainMallIDs(ctx context.Context, actor uint, ids []uint) ([]uint, error) {
	return f(ctx, actor, ids)
}

type kaiyuQueryReaderFunc func(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuPassengerHourly, error)

func (f kaiyuQueryReaderFunc) QueryKaiyuPassengerHours(ctx context.Context, start, end time.Time, ids []uint) ([]model.KaiyuPassengerHourly, error) {
	return f(ctx, start, end, ids)
}

type kaiyuQueryMallsFunc func(context.Context, data_dao.MallListFilter) ([]model.Mall, error)

func (f kaiyuQueryMallsFunc) List(ctx context.Context, filter data_dao.MallListFilter) ([]model.Mall, error) {
	return f(ctx, filter)
}

func TestKaiyuPassengerQueryRestrictsScopeAndAggregates(t *testing.T) {
	hour := time.Date(2026, 10, 28, 9, 0, 0, 0, kaiyuLocation)
	rows := []model.KaiyuPassengerHourly{
		{MallID: 7, ShopID: "s7", ShopName: "外部门店七", HourStart: hour, Passby: 20, Traffic: 10, TrafficCount: 8, TrafficBatch: 5},
		{MallID: 9, ShopID: "s9", ShopName: "外部门店九", HourStart: hour, Passby: 80, Traffic: 30, TrafficCount: 25, TrafficBatch: 15},
		{MallID: 7, ShopID: "s7", ShopName: "外部门店七", HourStart: hour.Add(time.Hour), Passby: 100, Traffic: 20, TrafficCount: 18, TrafficBatch: 10},
	}
	service := newKaiyuPassengerQueryService(
		kaiyuQueryScopeFunc(func(_ context.Context, actor uint, ids []uint) ([]uint, error) {
			if actor != 11 || len(ids) != 0 {
				t.Fatalf("scope actor=%d ids=%v", actor, ids)
			}
			return []uint{7, 9}, nil
		}),
		kaiyuQueryMallsFunc(func(_ context.Context, f data_dao.MallListFilter) ([]model.Mall, error) {
			if f.ActorUserID != 11 {
				t.Fatal("mall identities were not scoped")
			}
			return []model.Mall{{BaseModel: model.BaseModel{ID: 7}, NameCN: "门店七", KaiyuShopID: "s7"}, {BaseModel: model.BaseModel{ID: 9}, NameCN: "门店九", KaiyuShopID: "s9"}}, nil
		}),
		func(context.Context) (kaiyuPassengerReader, error) {
			return kaiyuQueryReaderFunc(func(_ context.Context, start, end time.Time, ids []uint) ([]model.KaiyuPassengerHourly, error) {
				if start.Format(time.RFC3339) != "2026-10-28T00:00:00+08:00" || end.Format(time.RFC3339) != "2026-10-30T00:00:00+08:00" || !reflect.DeepEqual(ids, []uint{7, 9}) {
					t.Fatalf("range=%v..%v ids=%v", start, end, ids)
				}
				return rows, nil
			}), nil
		},
	)
	result, err := service.Query(t.Context(), 11, KaiyuPassengerQuery{StartDate: "2026-10-28", EndDate: "2026-10-29"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Traffic != 60 || result.Summary.Passby != 200 || result.Summary.CaptureRate != .3 || result.Summary.RecordCount != 3 || result.Summary.MallCount != 2 || result.Summary.LatestHour == nil || !result.Summary.LatestHour.Equal(hour.Add(time.Hour)) {
		t.Fatalf("summary=%+v", result.Summary)
	}
	if len(result.Trend) != 2 || result.Trend[0].Traffic != 40 || result.Trend[0].RecordCount != 2 || result.Trend[0].CaptureRate != .4 {
		t.Fatalf("trend=%+v", result.Trend)
	}
	if len(result.Malls) != 2 || result.Malls[0].MallID != 7 || result.Malls[0].MallName != "门店七" || result.Malls[0].Traffic != 30 || result.Malls[0].CaptureRate != .25 {
		t.Fatalf("malls=%+v", result.Malls)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["items"]; exists {
		t.Fatal("query response exposes unnecessary raw rows")
	}
}

func TestKaiyuPassengerQueryRejectsBeforeOpeningOracle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    KaiyuPassengerQuery
		scopeErr error
		want     error
	}{
		{"invalid date", KaiyuPassengerQuery{StartDate: "2026-02-30", EndDate: "2026-03-01"}, nil, ErrKaiyuPassengerInvalid},
		{"reversed", KaiyuPassengerQuery{StartDate: "2026-10-29", EndDate: "2026-10-28"}, nil, ErrKaiyuPassengerInvalid},
		{"more than 31 days", KaiyuPassengerQuery{StartDate: "2026-10-01", EndDate: "2026-11-01"}, nil, ErrKaiyuPassengerInvalid},
		{"outside selected scope", KaiyuPassengerQuery{StartDate: "2026-10-28", EndDate: "2026-10-28", MallID: 9}, auth_svc.ErrMallScopeForbidden, auth_svc.ErrMallScopeForbidden},
		{"scope failure", KaiyuPassengerQuery{StartDate: "2026-10-28", EndDate: "2026-10-28"}, errors.New("mysql unavailable"), ErrKaiyuPassengerUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(_ context.Context, _ uint, ids []uint) ([]uint, error) {
				if tc.query.MallID > 0 && !reflect.DeepEqual(ids, []uint{tc.query.MallID}) {
					t.Fatalf("requested ids=%v", ids)
				}
				return nil, tc.scopeErr
			}), kaiyuTestMalls(nil), func(context.Context) (kaiyuPassengerReader, error) {
				t.Fatal("rejected request reached Oracle")
				return nil, nil
			})
			if _, err := service.Query(t.Context(), 11, tc.query); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestKaiyuPassengerMallOptionsIncludeUnboundScopedMalls(t *testing.T) {
	service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(context.Context, uint, []uint) ([]uint, error) { return []uint{7}, nil }), kaiyuQueryMallsFunc(func(_ context.Context, f data_dao.MallListFilter) ([]model.Mall, error) {
		if f.ActorUserID != 11 {
			t.Fatal("unscoped mall list")
		}
		return []model.Mall{{BaseModel: model.BaseModel{ID: 7}, NameCN: "未绑定店"}}, nil
	}), func(context.Context) (kaiyuPassengerReader, error) {
		t.Fatal("mall options should not need Oracle")
		return nil, nil
	})
	result, err := service.ListMalls(t.Context(), 11)
	if err != nil || len(result.Items) != 1 || result.Items[0].MallID != 7 || result.Items[0].ShopID != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestKaiyuPassengerQueryAllows31DaysAndReturnsEmptyArrays(t *testing.T) {
	service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(context.Context, uint, []uint) ([]uint, error) { return nil, nil }), kaiyuTestMalls(nil), func(context.Context) (kaiyuPassengerReader, error) {
		return kaiyuQueryReaderFunc(func(_ context.Context, start, end time.Time, ids []uint) ([]model.KaiyuPassengerHourly, error) {
			if len(ids) != 0 || end.Sub(start) != 31*24*time.Hour {
				t.Fatalf("range=%v ids=%v", end.Sub(start), ids)
			}
			return nil, nil
		}), nil
	})
	result, err := service.Query(t.Context(), 11, KaiyuPassengerQuery{StartDate: "2026-10-01", EndDate: "2026-10-31"})
	if err != nil || result == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if result.Trend == nil || result.Malls == nil || result.Summary.RecordCount != 0 || result.Summary.LatestHour != nil || result.Summary.CaptureRate != 0 {
		t.Fatalf("result=%+v", result)
	}
}
