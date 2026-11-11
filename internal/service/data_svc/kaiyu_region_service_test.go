package data_svc

import (
	"bytes"
	"context"
	"errors"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
	"github.com/xuri/excelize/v2"
	"strconv"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/service/auth_svc"
)

func TestKaiyuRegionRangeAndRollingWindow(t *testing.T) {
	for _, tc := range []struct {
		start, end string
		valid      bool
	}{
		{"2026-10-01", "2026-10-31", true}, {"2026-10-01", "2026-11-01", false},
		{"2026-11-02", "2026-11-01", false}, {"2026-02-30", "2026-03-01", false},
	} {
		_, _, err := parseKaiyuRegionRange(KaiyuRegionQuery{StartDate: tc.start, EndDate: tc.end})
		if (err == nil) != tc.valid {
			t.Errorf("range %s %s: %v", tc.start, tc.end, err)
		}
	}
	start, end := KaiyuRegionRollingRange(time.Date(2026, 11, 3, 17, 0, 0, 0, time.UTC))
	if start != "2026-11-01" || end != "2026-11-04" {
		t.Fatalf("Shanghai range: %s %s", start, end)
	}
}

func TestKaiyuRegionNameLimitCountsUnicodeCharacters(t *testing.T) {
	for _, count := range []int{255, 256} {
		_, _, err := parseKaiyuRegionRange(KaiyuRegionQuery{StartDate: "2026-11-01", EndDate: "2026-11-01", RegionName: strings.Repeat("区", count)})
		if (err == nil) != (count <= 255) {
			t.Errorf("region name with %d characters: %v", count, err)
		}
	}
}

func TestKaiyuRegionRejectsBindingConflictsBeforeBatchAndCursor(t *testing.T) {
	var statuses []KaiyuRegionStatusItem
	for i := 1; i <= 21; i++ {
		statuses = append(statuses, KaiyuRegionStatusItem{MallID: uint(i), ShopID: strconv.Itoa(i), StatDate: "2026-11-01", State: "unqueried"})
	}
	statuses[20].ShopID = statuses[0].ShopID
	for _, cursor := range []string{"", "2026-11-01:20"} {
		store := &kaiyuRegionMemoryStore{}
		called := false
		source := kaiyuRegionSourceFunc(func(context.Context, time.Time, []string) ([]kaiyu.RegionStayShop, error) {
			called = true
			return nil, nil
		})
		_, err := fillKaiyuRegionBatch(t.Context(), source, store, statuses, KaiyuRegionBackfillRequest{Force: true, Cursor: cursor}, time.Now())
		if !errors.Is(err, ErrKaiyuRegionBindingConflict) || called || len(store.saved) != 0 {
			t.Fatalf("cursor=%q conflict err=%v called=%v saved=%v", cursor, err, called, store.saved)
		}
	}
}

func TestKaiyuRegionAllowsSameBindingAcrossDates(t *testing.T) {
	statuses := []KaiyuRegionStatusItem{{MallID: 1, ShopID: "shop", StatDate: "2026-11-01", State: "unqueried"}, {MallID: 1, ShopID: "shop", StatDate: "2026-11-02", State: "unqueried"}}
	source := kaiyuRegionSourceFunc(func(context.Context, time.Time, []string) ([]kaiyu.RegionStayShop, error) {
		return []kaiyu.RegionStayShop{{ShopID: "shop", ShopName: "门店", Regions: []kaiyu.RegionStay{{RegionName: "区域"}}}}, nil
	})
	result, err := fillKaiyuRegionBatch(t.Context(), source, &kaiyuRegionMemoryStore{}, statuses, KaiyuRegionBackfillRequest{}, time.Now())
	if err != nil || result.SavedDays != 1 || result.Done {
		t.Fatalf("multi-date binding: result=%+v err=%v", result, err)
	}
}

func TestKaiyuRegionDeniesScopeBeforeOpeningStore(t *testing.T) {
	opened := false
	service := &KaiyuRegionService{access: &KaiyuPassengerQueryService{scope: kaiyuQueryScopeFunc(func(context.Context, uint, []uint) ([]uint, error) { return nil, auth_svc.ErrMallScopeForbidden })}, open: func(context.Context) (kaiyuRegionStore, error) { opened = true; return nil, nil }}
	_, err := service.Query(t.Context(), 7, KaiyuRegionQuery{StartDate: "2026-11-01", EndDate: "2026-11-02", MallID: 99})
	if !errors.Is(err, auth_svc.ErrMallScopeForbidden) || opened {
		t.Fatalf("denial err=%v opened=%v", err, opened)
	}
}

type kaiyuRegionMemoryStore struct {
	rows     []model.KaiyuRegionStayDaily
	statuses []model.KaiyuRegionSyncDay
	saved    []model.KaiyuRegionSyncDay
	ids      []uint
}

func (store *kaiyuRegionMemoryStore) QueryKaiyuRegionDays(_ context.Context, _, _ time.Time, ids []uint) ([]model.KaiyuRegionStayDaily, error) {
	store.ids = ids
	return store.rows, nil
}
func (store *kaiyuRegionMemoryStore) QueryKaiyuRegionSyncDays(_ context.Context, _, _ time.Time, ids []uint) ([]model.KaiyuRegionSyncDay, error) {
	store.ids = ids
	return store.statuses, nil
}
func (store *kaiyuRegionMemoryStore) SaveKaiyuRegionDay(_ context.Context, status model.KaiyuRegionSyncDay, rows []model.KaiyuRegionStayDaily) error {
	store.saved = append(store.saved, status)
	if status.State == model.KaiyuRegionSyncSuccess {
		store.rows = rows
	}
	return nil
}

type kaiyuRegionSourceFunc func(context.Context, time.Time, []string) ([]kaiyu.RegionStayShop, error)

func (f kaiyuRegionSourceFunc) RegionStayByShops(ctx context.Context, date time.Time, shops []string) ([]kaiyu.RegionStayShop, error) {
	return f(ctx, date, shops)
}

func TestKaiyuRegionBatchKeepsEmptyAndMissingDistinct(t *testing.T) {
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, kaiyuLocation)
	store := &kaiyuRegionMemoryStore{}
	source := kaiyuRegionSourceFunc(func(_ context.Context, _ time.Time, ids []string) ([]kaiyu.RegionStayShop, error) {
		if len(ids) != 3 {
			t.Fatalf("ids=%v", ids)
		}
		return []kaiyu.RegionStayShop{{ShopID: "a", Regions: []kaiyu.RegionStay{{RegionName: "入口", ExposureCount: 0, StayCount: 0, StayRate: 0, AvgStayTime: 5}}}, {ShopID: "b", Regions: []kaiyu.RegionStay{}}}, nil
	})
	statuses := []KaiyuRegionStatusItem{{MallID: 1, MallName: "门店A", ShopID: "a", StatDate: "2026-11-01", State: "unqueried"}, {MallID: 2, MallName: "门店B", ShopID: "b", StatDate: "2026-11-01", State: "unqueried"}, {MallID: 3, ShopID: "c", StatDate: "2026-11-01", State: "unqueried"}}
	result, err := fillKaiyuRegionBatch(t.Context(), source, store, statuses, KaiyuRegionBackfillRequest{}, date)
	if err != nil || result.SavedDays != 1 || result.EmptyDays != 1 || result.SourceMissingDays != 1 || result.FailedDays != 1 || !result.Done {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(store.rows) != 1 || store.rows[0].AvgStayTime != 5 || store.rows[0].ShopName != "门店A" || store.saved[1].ShopName != "门店B" {
		t.Fatalf("source zero must be stored, empty/missing must retain snapshot: %+v", store.rows)
	}
	if store.saved[0].State != "success" || store.saved[1].State != "empty" || store.saved[2].State != "failed" {
		t.Fatalf("statuses=%+v", store.saved)
	}
}

func TestKaiyuRegionForceRefreshesAndCursorLimitsTwentyShops(t *testing.T) {
	var statuses []KaiyuRegionStatusItem
	for i := 1; i <= 21; i++ {
		statuses = append(statuses, KaiyuRegionStatusItem{MallID: uint(i), ShopID: strconv.Itoa(i), StatDate: "2026-11-01", State: "success"})
	}
	var sizes []int
	source := kaiyuRegionSourceFunc(func(_ context.Context, _ time.Time, ids []string) ([]kaiyu.RegionStayShop, error) {
		sizes = append(sizes, len(ids))
		rows := make([]kaiyu.RegionStayShop, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, kaiyu.RegionStayShop{ShopID: id, Regions: []kaiyu.RegionStay{{RegionName: "A", ExposureCount: 99}}})
		}
		return rows, nil
	})
	store := &kaiyuRegionMemoryStore{}
	skipped, err := fillKaiyuRegionBatch(t.Context(), source, store, statuses, KaiyuRegionBackfillRequest{}, time.Now())
	if err != nil || skipped.ProcessedDays != 0 || len(sizes) != 0 {
		t.Fatalf("skip=%+v %v", skipped, err)
	}
	first, err := fillKaiyuRegionBatch(t.Context(), source, store, statuses, KaiyuRegionBackfillRequest{Force: true}, time.Now())
	if err != nil || first.Done || first.ProcessedDays != 20 || first.NextCursor != "2026-11-01:20" {
		t.Fatalf("first=%+v %v", first, err)
	}
	last, err := fillKaiyuRegionBatch(t.Context(), source, store, statuses, KaiyuRegionBackfillRequest{Force: true, Cursor: first.NextCursor}, time.Now())
	if err != nil || !last.Done || last.ProcessedDays != 1 || len(sizes) != 2 || sizes[0] != 20 || sizes[1] != 1 {
		t.Fatalf("last=%+v sizes=%v err=%v", last, sizes, err)
	}
	if store.rows[0].ExposureCount != 99 {
		t.Fatal("force refresh did not replace daily metric")
	}
}

func TestKaiyuRegionMissingUsesShopDateAndNeverFillsZero(t *testing.T) {
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, kaiyuLocation)
	store := &kaiyuRegionMemoryStore{statuses: []model.KaiyuRegionSyncDay{{MallID: 1, ShopID: "a", StatDate: day, State: "empty", FetchedAt: day}, {MallID: 2, ShopID: "b", StatDate: day, State: "failed", ErrorMessage: "secret response", FetchedAt: day}}}
	malls := []KaiyuPassengerMallOption{{MallID: 1, ShopID: "a"}, {MallID: 2, ShopID: "b"}, {MallID: 3, ShopID: ""}}
	result, err := queryKaiyuRegionStatus(t.Context(), store, KaiyuRegionQuery{}, day, day.AddDate(0, 0, 2), []uint{1, 2, 3}, malls)
	if err != nil || result.ExpectedDays != 4 || result.EmptyDays != 1 || result.FailedDays != 1 || result.UnqueriedDays != 2 || result.UnboundMalls != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Items[1].ErrorMessage, "secret") {
		t.Fatal("source error exposed")
	}
}

func TestKaiyuRegionQueryScopesAndKeepsDailyAverage(t *testing.T) {
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, kaiyuLocation)
	store := &kaiyuRegionMemoryStore{rows: []model.KaiyuRegionStayDaily{{MallID: 7, ShopID: "shop", StatDate: day, RegionName: "A", ExposureCount: 10, StayCount: 2, AvgStayTime: 4, FetchedAt: day}, {MallID: 7, ShopID: "shop", StatDate: day.AddDate(0, 0, 1), RegionName: "A", ExposureCount: 30, StayCount: 9, AvgStayTime: 8, FetchedAt: day}}}
	access := &KaiyuPassengerQueryService{scope: kaiyuQueryScopeFunc(func(_ context.Context, actor uint, ids []uint) ([]uint, error) {
		if actor != 5 || len(ids) != 1 || ids[0] != 7 {
			t.Fatalf("actor=%d ids=%v", actor, ids)
		}
		return ids, nil
	}), malls: kaiyuQueryMallsFunc(func(context.Context, data_dao.MallListFilter) ([]model.Mall, error) {
		return []model.Mall{{BaseModel: model.BaseModel{ID: 7}, NameCN: "门店", KaiyuShopID: "shop"}}, nil
	})}
	service := &KaiyuRegionService{access: access, store: store}
	result, err := service.Query(t.Context(), 5, KaiyuRegionQuery{StartDate: "2026-11-01", EndDate: "2026-11-02", MallID: 7, RegionName: "A"})
	if err != nil || result.Summary.ExposureCount != 40 || result.Summary.StayCount != 11 || result.Summary.RegionCount != 1 || len(store.ids) != 1 || store.ids[0] != 7 {
		t.Fatalf("result=%+v ids=%v err=%v", result, store.ids, err)
	}
	if result.Items[0].AvgStayTime != 4 || result.Items[1].AvgStayTime != 8 {
		t.Fatalf("daily averages changed: %+v", result.Items)
	}
}

func TestKaiyuRegionExportRetainsNumericMetricsAndShanghaiTime(t *testing.T) {
	at := time.Date(2026, 11, 1, 16, 1, 2, 0, time.UTC)
	data, err := renderKaiyuRegionWorkbook(t.Context(), &KaiyuRegionResult{StartDate: "2026-11-01", EndDate: "2026-11-01", Items: []KaiyuRegionItem{{MallID: 1, MallName: "门店", ShopID: "001", StatDate: "2026-11-01", RegionName: "=1+2", ExposureCount: 20, StayCount: 5, StayRate: .25, AvgStayTime: 8.5, FetchedAt: at}}}, &KaiyuRegionMissingResult{Items: []KaiyuRegionStatusItem{{MallID: 1, State: "empty", FetchedAt: &at}}})
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for cell, want := range map[string]string{"C2": "001", "E2": "=1+2", "F2": "20", "H2": "25.00%", "J2": "2026-11-02 00:01:02"} {
		got, err := file.GetCellValue("区域明细", cell)
		if err != nil || got != want {
			t.Errorf("%s=%q want=%q err=%v", cell, got, want, err)
		}
	}
	if formula, _ := file.GetCellFormula("区域明细", "E2"); formula != "" {
		t.Fatal("region name became formula")
	}
	if status, _ := file.GetCellValue("同步状态", "E2"); status != "上游返回空区域" {
		t.Fatalf("status=%s", status)
	}
}
