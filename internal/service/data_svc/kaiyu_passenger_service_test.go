package data_svc

import (
	"context"
	"errors"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
	"testing"
	"time"
)

type kaiyuTestMalls []model.Mall

func (m kaiyuTestMalls) List(_ context.Context, f data_dao.MallListFilter) ([]model.Mall, error) {
	if f.AfterID > 0 {
		return nil, nil
	}
	return m, nil
}

type kaiyuTestClient struct {
	rows   map[string][]kaiyu.HourlyPassenger
	failed map[string]bool
}

func (c kaiyuTestClient) HourlyPassengers(_ context.Context, id string, _ time.Time) ([]kaiyu.HourlyPassenger, error) {
	if c.failed[id] {
		return nil, errors.New("provider unavailable")
	}
	return c.rows[id], nil
}

type kaiyuTestStore struct{ rows []model.KaiyuPassengerHourly }

func (s *kaiyuTestStore) UpsertKaiyuPassengerHour(_ context.Context, r model.KaiyuPassengerHourly) error {
	s.rows = append(s.rows, r)
	return nil
}

func TestKaiyuSyncUsesBoundMallsAndOnlyRequestedHour(t *testing.T) {
	hour := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	malls := kaiyuTestMalls{{BaseModel: model.BaseModel{ID: 1}, KaiyuShopID: "shop1", NameCN: "本地名称"}, {BaseModel: model.BaseModel{ID: 2}}}
	client := kaiyuTestClient{rows: map[string][]kaiyu.HourlyPassenger{"shop1": {
		{ShopID: "shop1", ShopName: "监控名称", Dateline: "2026-10-27 23:00", Traffic: 4},
		{ShopID: "shop1", ShopName: "监控名称", Dateline: "2026-10-28 00:00", Traffic: 15, Passby: 30, TrafficCount: 12},
		{ShopID: "shop1", Dateline: "2026-10-28 01:00", Traffic: 99},
	}}}
	store := &kaiyuTestStore{}
	result, err := NewKaiyuPassengerService(malls, client, store).SyncHour(t.Context(), hour)
	if err != nil || result.Saved != 1 || result.Skipped != 1 || len(store.rows) != 1 {
		t.Fatalf("result=%+v rows=%+v err=%v", result, store.rows, err)
	}
	row := store.rows[0]
	if row.MallID != 1 || row.Traffic != 15 || row.ShopName != "监控名称" || !row.HourStart.Equal(hour) {
		t.Fatalf("row=%+v", row)
	}
}

func TestKaiyuSyncContinuesAfterFailureAndDoesNotInventMissingHour(t *testing.T) {
	malls := kaiyuTestMalls{{BaseModel: model.BaseModel{ID: 1}, KaiyuShopID: "failed"}, {BaseModel: model.BaseModel{ID: 2}, KaiyuShopID: "empty"}, {BaseModel: model.BaseModel{ID: 3}, KaiyuShopID: "ok"}}
	client := kaiyuTestClient{failed: map[string]bool{"failed": true}, rows: map[string][]kaiyu.HourlyPassenger{"ok": {{ShopID: "ok", Dateline: "2026-10-28 09:00", Traffic: 0}}}}
	store := &kaiyuTestStore{}
	result, err := NewKaiyuPassengerService(malls, client, store).SyncHour(t.Context(), time.Date(2026, 10, 28, 9, 0, 0, 0, time.FixedZone("CST", 8*3600)))
	if err == nil || result.Saved != 1 || result.Failed != 2 || len(store.rows) != 1 || store.rows[0].ShopID != "ok" {
		t.Fatalf("result=%+v rows=%+v err=%v", result, store.rows, err)
	}
}

func TestKaiyuPreviousHourUsesShanghaiCalendarAcrossMidnight(t *testing.T) {
	got := KaiyuPreviousHour(time.Date(2026, 10, 27, 16, 5, 0, 0, time.UTC))
	if got.Format("2006-01-02 15:04 -0700") != "2026-10-27 23:00 +0800" {
		t.Fatal(got)
	}
}
