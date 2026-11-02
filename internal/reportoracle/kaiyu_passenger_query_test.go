package reportoracle

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

type kaiyuHourQuerier interface {
	QueryKaiyuPassengerHours(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuPassengerHourly, error)
}

func TestKaiyuPassengerQueryBindsScopeAndShanghaiRange(t *testing.T) {
	start := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	rows := &bojunQueryTestRows{
		columns: make([]string, 16),
		values:  [][]driver.Value{{"shop1", "2026-10-28 09:00:00", int64(7), "门店一", int64(20), int64(10), int64(8), int64(5), float64(.5), float64(12), float64(.3), float64(.1), float64(100), float64(2), float64(50), float64(.2)}},
	}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		for _, fragment := range []string{"FROM YLZ_USER.KAIYU_PASSENGER_HOURLY", "HOUR_START >= TO_TIMESTAMP(:1", "HOUR_START < TO_TIMESTAMP(:2", "MALL_ID IN (:3,:4)", "ORDER BY HOUR_START, MALL_ID, SHOP_ID"} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("missing %q in %s", fragment, query)
			}
		}
		values := make([]any, len(args))
		for i, arg := range args {
			values[i] = arg.Value
		}
		if ctx != t.Context() || !reflect.DeepEqual(values, []any{"2026-10-28 00:00:00", "2026-10-29 00:00:00", uint(7), uint(9)}) {
			t.Fatalf("context or bindings: %#v", values)
		}
		if strings.Contains(query, "2026-10") {
			t.Fatal("date interpolated into SQL")
		}
		return rows, nil
	})
	reader, ok := any(adapter).(kaiyuHourQuerier)
	if !ok {
		t.Fatal("Oracle adapter has no scoped passenger query")
	}
	items, err := reader.QueryKaiyuPassengerHours(t.Context(), start.UTC(), start.AddDate(0, 0, 1).UTC(), []uint{7, 9})
	if err != nil {
		t.Fatal(err)
	}
	if !rows.closed || len(items) != 1 || items[0].MallID != 7 || items[0].Traffic != 10 || items[0].ShopperYield != .2 || items[0].HourStart.Format(time.RFC3339) != "2026-10-28T09:00:00+08:00" {
		t.Fatalf("items=%+v closed=%v", items, rows.closed)
	}
}

func TestKaiyuPassengerQueryPropagatesFailure(t *testing.T) {
	want := errors.New("oracle unavailable")
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return nil, want })
	reader, ok := any(adapter).(kaiyuHourQuerier)
	if !ok {
		t.Fatal("Oracle adapter has no scoped passenger query")
	}
	start := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	if _, err := reader.QueryKaiyuPassengerHours(t.Context(), start, start.Add(24*time.Hour), nil); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}

func TestKaiyuPassengerQueryRejectsOversizedResultsWithoutTruncating(t *testing.T) {
	row := []driver.Value{"shop1", "2026-10-28 09:00:00", int64(7), "门店一", int64(20), int64(10), int64(8), int64(5), float64(.5), float64(12), float64(.3), float64(.1), float64(100), float64(2), float64(50), float64(.2)}
	values := make([][]driver.Value, 100_001)
	for i := range values {
		values[i] = row
	}
	rows := &bojunQueryTestRows{columns: make([]string, 16), values: values}
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return rows, nil })
	start := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	items, err := adapter.QueryKaiyuPassengerHours(t.Context(), start, start.Add(24*time.Hour), nil)
	if !errors.Is(err, ErrKaiyuPassengerTooManyRows) || items != nil || !rows.closed {
		t.Fatalf("items=%d err=%v closed=%v", len(items), err, rows.closed)
	}
}
