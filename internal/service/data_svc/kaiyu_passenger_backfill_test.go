package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
)

func TestKaiyuMissingHoursUseCurrentBindingAndCompletedHours(t *testing.T) {
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, kaiyuLocation)
	malls := []KaiyuPassengerMallOption{{MallID: 1, ShopID: "current"}, {MallID: 2}, {MallID: 3, ShopID: "s3"}}
	rows := []model.KaiyuPassengerHourly{
		{MallID: 1, ShopID: "current", HourStart: start}, // Recorded zero is present.
		{MallID: 1, ShopID: "old", HourStart: start.Add(time.Hour)},
		{MallID: 3, ShopID: "s3", HourStart: start.Add(2 * time.Hour)},
	}
	result, groups := planKaiyuMissingHours(start, start.Add(2*time.Hour), malls, rows)
	if result.ExpectedHours != 4 || result.ExistingHours != 1 || result.MissingHours != 3 || result.UnboundMalls != 1 || len(groups) != 2 {
		t.Fatalf("result=%+v groups=%+v", result, groups)
	}
	if groups[0].mall.MallID != 1 || len(groups[0].hours) != 1 || !groups[0].hours[0].Equal(start.Add(time.Hour)) {
		t.Fatalf("group=%+v", groups[0])
	}
	if result.Malls[1].MissingHours != 2 || !result.Malls[1].LastMissingHour.Equal(start.Add(time.Hour)) {
		t.Fatalf("malls=%+v", result.Malls)
	}
}

func TestKaiyuMissingScopeRejectsAndClipsCurrentHour(t *testing.T) {
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, kaiyuLocation)
	for _, tc := range []struct {
		name    string
		allowed bool
	}{{"authorized", true}, {"forbidden", false}} {
		t.Run(tc.name, func(t *testing.T) {
			service := newKaiyuPassengerQueryService(kaiyuQueryScopeFunc(func(_ context.Context, actor uint, ids []uint) ([]uint, error) {
				if actor != 17 || !reflect.DeepEqual(ids, []uint{1}) {
					t.Fatalf("actor=%d ids=%v", actor, ids)
				}
				if !tc.allowed {
					return nil, auth_svc.ErrMallScopeForbidden
				}
				return []uint{1}, nil
			}), kaiyuQueryMallsFunc(func(_ context.Context, filter data_dao.MallListFilter) ([]model.Mall, error) {
				if filter.ActorUserID != 17 {
					t.Fatalf("unscoped mall request: %+v", filter)
				}
				return []model.Mall{{BaseModel: model.BaseModel{ID: 1}, KaiyuShopID: "s1"}, {BaseModel: model.BaseModel{ID: 2}, KaiyuShopID: "s2"}}, nil
			}), func(context.Context) (kaiyuPassengerReader, error) {
				if !tc.allowed {
					t.Fatal("forbidden request opened Oracle")
				}
				return kaiyuQueryReaderFunc(func(_ context.Context, from, to time.Time, ids []uint) ([]model.KaiyuPassengerHourly, error) {
					if !from.Equal(start) || !to.Equal(start.Add(2*time.Hour)) || !reflect.DeepEqual(ids, []uint{1}) {
						t.Fatalf("range=%v..%v ids=%v", from, to, ids)
					}
					return nil, nil
				}), nil
			})
			result, _, _, err := service.missingPlan(t.Context(), 17, KaiyuPassengerQuery{StartDate: "2026-10-29", EndDate: "2026-10-30", MallID: 1}, start.Add(2*time.Hour+10*time.Minute))
			if !tc.allowed {
				if !errors.Is(err, auth_svc.ErrMallScopeForbidden) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || result.ExpectedHours != 2 || len(result.Malls) != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

type kaiyuBackfillClientFunc func(context.Context, string, time.Time) ([]kaiyu.HourlyPassenger, error)

func (f kaiyuBackfillClientFunc) HourlyPassengers(ctx context.Context, id string, day time.Time) ([]kaiyu.HourlyPassenger, error) {
	return f(ctx, id, day)
}

func TestKaiyuBackfillFetchesEachDayOnceAndOnlySavesMissingHours(t *testing.T) {
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, kaiyuLocation)
	groups := []kaiyuMissingDay{{mall: KaiyuPassengerMallOption{MallID: 1, ShopID: "s1", MallName: "门店一"}, day: start, hours: []time.Time{start, start.Add(2 * time.Hour), start.Add(3 * time.Hour)}}}
	calls := 0
	client := kaiyuBackfillClientFunc(func(_ context.Context, id string, day time.Time) ([]kaiyu.HourlyPassenger, error) {
		calls++
		if id != "s1" || !day.Equal(start) {
			t.Fatalf("id=%s day=%v", id, day)
		}
		return []kaiyu.HourlyPassenger{
			{ShopID: "s1", Dateline: "2026-10-29 00:00", Traffic: 0},
			{ShopID: "s1", Dateline: "2026-10-29 01:00", Traffic: 99}, // Already present, must not overwrite.
			{ShopID: "s1", Dateline: "2026-10-29 02:00:00", Traffic: 20},
			{ShopID: "wrong", Dateline: "2026-10-29 03:00", Traffic: 99},
		}, nil
	})
	store := &kaiyuTestStore{}
	result, err := fillKaiyuMissingDays(t.Context(), groups, 3, "", 5, client, store)
	if err != nil || calls != 1 || result.SavedHours != 2 || result.SourceMissingHours != 1 || result.RemainingHours != 1 || result.ScannedDays != 1 || result.FailedDays != 0 || result.NextCursor != "" {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	if len(store.rows) != 2 || store.rows[0].Traffic != 0 || store.rows[1].Traffic != 20 || store.rows[0].ShopName != "门店一" {
		t.Fatalf("rows=%+v", store.rows)
	}
	missing, _ := planKaiyuMissingHours(start, start.Add(4*time.Hour), []KaiyuPassengerMallOption{groups[0].mall}, append(store.rows, model.KaiyuPassengerHourly{MallID: 1, ShopID: "s1", HourStart: start.Add(time.Hour)}))
	if missing.MissingHours != 1 || !missing.Malls[0].FirstMissingHour.Equal(start.Add(3*time.Hour)) {
		t.Fatalf("missing=%+v", missing)
	}
}

func TestKaiyuBackfillCursorAdvancesPastUnavailableSourceDays(t *testing.T) {
	start := time.Date(2026, 10, 28, 0, 0, 0, 0, kaiyuLocation)
	groups := []kaiyuMissingDay{
		{mall: KaiyuPassengerMallOption{MallID: 1, ShopID: "empty"}, day: start, hours: []time.Time{start}},
		{mall: KaiyuPassengerMallOption{MallID: 2, ShopID: "failed"}, day: start, hours: []time.Time{start}},
		{mall: KaiyuPassengerMallOption{MallID: 1, ShopID: "empty"}, day: start.AddDate(0, 0, 1), hours: []time.Time{start.AddDate(0, 0, 1)}},
	}
	client := kaiyuBackfillClientFunc(func(_ context.Context, id string, _ time.Time) ([]kaiyu.HourlyPassenger, error) {
		if id == "failed" {
			return nil, errors.New("upstream unavailable")
		}
		return nil, nil
	})
	first, err := fillKaiyuMissingDays(t.Context(), groups, 3, "", 2, client, &kaiyuTestStore{})
	if err != nil || first.NextCursor != "2026-10-28:2" || first.SourceMissingHours != 1 || first.FailedDays != 1 || first.FailedHours != 1 || first.ScannedDays != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := fillKaiyuMissingDays(t.Context(), groups, 3, first.NextCursor, 2, client, &kaiyuTestStore{})
	if err != nil || second.NextCursor != "" || second.ScannedDays != 1 || second.SourceMissingHours != 1 || second.RemainingHours != 3 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestKaiyuBackfillRejectsInvalidCursorAndCancelledWork(t *testing.T) {
	for _, cursor := range []string{"bad", "2026-02-30:1", "2026-10-29:0", "2026-10-29:1:2"} {
		t.Run(cursor, func(t *testing.T) {
			if _, _, err := parseKaiyuBackfillCursor(cursor); !errors.Is(err, ErrKaiyuPassengerInvalid) {
				t.Fatalf("cursor=%s err=%v", cursor, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fillKaiyuMissingDays(ctx, []kaiyuMissingDay{{day: time.Now()}}, 1, "", 5, nil, nil)
	if !errors.Is(err, ErrKaiyuPassengerUnavailable) {
		t.Fatal(err)
	}
}
