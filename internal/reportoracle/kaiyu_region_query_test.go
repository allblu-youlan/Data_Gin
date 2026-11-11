package reportoracle

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKaiyuRegionQueryBindsScopeAndShanghaiDates(t *testing.T) {
	start := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	rows := &bojunQueryTestRows{columns: make([]string, 10), values: [][]driver.Value{{"shop1", "2026-11-11", int64(7), "门店一", "童装", int64(100), int64(0), float64(0), float64(3.38), "2026-11-11 09:20:00"}}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() || !strings.Contains(q, "MALL_ID IN (:3,:4)") || !strings.Contains(q, "STAT_DATE<TO_DATE(:2") || !strings.Contains(q, "FETCH FIRST 100001 ROWS ONLY") {
			t.Fatalf("query=%s", q)
		}
		actual := make([]any, len(args))
		for i, arg := range args {
			actual[i] = arg.Value
		}
		if !reflect.DeepEqual(actual, []any{"2026-11-11", "2026-11-13", uint(7), uint(9)}) {
			t.Fatalf("args=%v", actual)
		}
		return rows, nil
	})
	items, err := adapter.QueryKaiyuRegionDays(t.Context(), start, start.AddDate(0, 0, 2), []uint{7, 9})
	if err != nil {
		t.Fatal(err)
	}
	if !rows.closed || len(items) != 1 || items[0].AvgStayTime != 3.38 || items[0].StatDate.Format(time.RFC3339) != "2026-11-11T00:00:00+08:00" || items[0].FetchedAt.Format(time.RFC3339) != "2026-11-11T09:20:00+08:00" {
		t.Fatalf("items=%+v closed=%v", items, rows.closed)
	}
}

func TestKaiyuRegionSyncQueryRetainsSuccessAndNullableErrors(t *testing.T) {
	rows := &bojunQueryTestRows{columns: make([]string, 9), values: [][]driver.Value{
		{"shop1", "2026-11-11", int64(7), "门店一", "empty", int64(0), "2026-11-11 10:20:00", "2026-11-11 09:20:00", nil},
		{"shop2", "2026-11-11", int64(9), "门店二", "failed", int64(0), "2026-11-11 10:20:00", nil, "source unavailable"},
	}}
	adapter := newBojunQueryTestAdapter(t, func(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(q, "YLZ_USER.KAIYU_REGION_SYNC_DAY") {
			t.Fatal(q)
		}
		return rows, nil
	})
	start := time.Date(2026, 11, 11, 0, 0, 0, 0, kaiyuRegionLocation)
	items, err := adapter.QueryKaiyuRegionSyncDays(t.Context(), start, start.AddDate(0, 0, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].State != "empty" || items[0].LastSuccessAt == nil || items[0].LastSuccessAt.Format(time.RFC3339) != "2026-11-11T09:20:00+08:00" || items[0].ErrorMessage != "" || items[1].LastSuccessAt != nil || items[1].ErrorMessage != "source unavailable" || !rows.closed {
		t.Fatalf("items=%+v", items)
	}
}

func TestKaiyuRegionQueryRejectsOversizedResultsWithoutTruncating(t *testing.T) {
	row := []driver.Value{"shop1", "2026-11-11", int64(7), "门店一", "童装", int64(100), int64(10), float64(.1), float64(3.38), "2026-11-11 09:20:00"}
	values := make([][]driver.Value, KaiyuRegionMaxRows+1)
	for i := range values {
		values[i] = row
	}
	rows := &bojunQueryTestRows{columns: make([]string, 10), values: values}
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return rows, nil })
	start := time.Date(2026, 11, 11, 0, 0, 0, 0, kaiyuRegionLocation)
	items, err := adapter.QueryKaiyuRegionDays(t.Context(), start, start.AddDate(0, 0, 1), nil)
	if !errors.Is(err, ErrKaiyuRegionTooManyRows) || items != nil || !rows.closed {
		t.Fatalf("items=%d err=%v closed=%v", len(items), err, rows.closed)
	}
}

func TestKaiyuRegionDateScopeSplitsOracleInLimit(t *testing.T) {
	start := time.Now()
	ids := make([]uint, 1001)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	query, args, err := kaiyuRegionDateScope(start, start.AddDate(0, 0, 1), ids)
	if err != nil || len(args) != 1003 || !strings.Contains(query, ") OR MALL_ID IN (:1003)") {
		t.Fatalf("args=%d query=%s error=%v", len(args), query, err)
	}
	if _, _, err := kaiyuRegionDateScope(start, start, ids); err == nil {
		t.Fatal("empty date range accepted")
	}
	if _, _, err := kaiyuRegionDateScope(start, start.AddDate(0, 0, 1), []uint{0}); err == nil {
		t.Fatal("zero mall ID accepted")
	}
}
