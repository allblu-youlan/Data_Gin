package reportoracle

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func monitoringTestHourValues() []driver.Value {
	return []driver.Value{
		"2026-11-01 00:00:00", int64(101), int64(32), int64(28), int64(15),
		0.32, 124.5, 0.4, 0.2, 1500.25, 8.0, 187.53, 46.88, "2026-11-02 01:02:03",
	}
}

func monitoringTestAreaValues() []driver.Value {
	return []driver.Value{"试衣区", int64(20), int64(5), 0.25, 63.5, "2026-11-02 01:02:03"}
}

func TestQueryMonitoringHoursBindsDistinctShanghaiDaysAndMapsRows(t *testing.T) {
	shopID := "shop' OR 1=1 --"
	dates := []time.Time{
		time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	}
	original := append([]time.Time(nil), dates...)
	rows := &bojunQueryTestRows{columns: make([]string, 14), values: [][]driver.Value{monitoringTestHourValues()}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() {
			t.Fatal("request context was replaced")
		}
		for _, fragment := range []string{
			"FROM YLZ_USER.KAIYU_PASSENGER_HOURLY", "SHOP_ID = :1", "MALL_ID = :2",
			"HOUR_START >= TO_TIMESTAMP(:3, 'YYYY-MM-DD HH24:MI:SS')",
			"HOUR_START < TO_TIMESTAMP(:4, 'YYYY-MM-DD HH24:MI:SS')",
			"HOUR_START >= TO_TIMESTAMP(:5, 'YYYY-MM-DD HH24:MI:SS')",
			"HOUR_START < TO_TIMESTAMP(:6, 'YYYY-MM-DD HH24:MI:SS')",
			"ROWNUM <= 745", "ORDER BY HOUR_START",
			"TO_CHAR(HOUR_START, 'YYYY-MM-DD HH24:MI:SS')",
			"TO_CHAR(FETCHED_AT, 'YYYY-MM-DD HH24:MI:SS')",
		} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("missing %q in %s", fragment, statement)
			}
		}
		for _, column := range []string{"CAPTURE_RATE", "AVG_VISIT_DURATION", "NEW_VISITORS_RATIO", "DEEP_VISIT_RATIO", "NET_SALES", "SALES_TRANSACTIONS", "ATV", "SHOPPER_YIELD"} {
			if !strings.Contains(statement, "NVL("+column+", 0)") {
				t.Fatalf("nullable hourly metric %s lacks default", column)
			}
		}
		if strings.Contains(statement, shopID) || strings.Contains(statement, "TRUNC(") || strings.Count(statement, "HOUR_START >=") != 2 {
			t.Fatalf("unsafe or non-indexed day filter: %s", statement)
		}
		want := []any{shopID, int64(7), "2026-11-01 00:00:00", "2026-11-02 00:00:00", "2026-11-03 00:00:00", "2026-11-04 00:00:00"}
		got := make([]any, len(args))
		for i := range args {
			got[i] = args[i].Value
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("binds = %#v, want %#v", got, want)
		}
		return rows, nil
	})
	items, err := adapter.QueryMonitoringHours(t.Context(), shopID, 7, dates)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %#v, error = %v", items, err)
	}
	if !rows.closed || !reflect.DeepEqual(dates, original) {
		t.Fatal("rows were not closed or input dates were changed")
	}
	encoded, err := json.Marshal(items[0])
	wantJSON := `{"hourStart":"2026-11-01T00:00:00+08:00","passby":101,"traffic":32,"trafficCount":28,"trafficBatch":15,"captureRate":0.32,"avgVisitDuration":124.5,"newVisitorsRatio":0.4,"deepVisitRatio":0.2,"netSales":1500.25,"salesTransactions":8,"atv":187.53,"shopperYield":46.88,"fetchedAt":"2026-11-02T01:02:03+08:00"}`
	if err != nil || string(encoded) != wantJSON {
		t.Fatalf("hourly JSON = %s, error = %v", encoded, err)
	}
}

func TestQueryMonitoringAreasBindsExactRegionAndMapsRows(t *testing.T) {
	for _, region := range []string{"", "区域' OR 1=1 --"} {
		t.Run(region, func(t *testing.T) {
			rows := &bojunQueryTestRows{columns: make([]string, 6), values: [][]driver.Value{monitoringTestAreaValues()}}
			adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				if ctx != t.Context() {
					t.Fatal("request context was replaced")
				}
				for _, fragment := range []string{
					"FROM YLZ_USER.KAIYU_REGION_STAY_DAILY", "SHOP_ID = :1", "MALL_ID = :2",
					"STAT_DATE >= TO_DATE(:3, 'YYYY-MM-DD')", "STAT_DATE < TO_DATE(:4, 'YYYY-MM-DD')",
					"ROWNUM <= 1001", "ORDER BY REGION_NAME",
				} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				want := []any{"shop-7", int64(7), "2026-11-01", "2026-11-02"}
				if region != "" {
					want = append(want, region)
					if !strings.Contains(statement, "REGION_NAME = :5") || strings.Contains(statement, region) {
						t.Fatalf("region filter is not bound: %s", statement)
					}
				} else if strings.Contains(statement, "REGION_NAME =") {
					t.Fatalf("empty region unexpectedly filters rows: %s", statement)
				}
				got := make([]any, len(args))
				for i := range args {
					got[i] = args[i].Value
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("binds = %#v, want %#v", got, want)
				}
				return rows, nil
			})
			date := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
			items, err := adapter.QueryMonitoringAreas(t.Context(), "shop-7", 7, date, region)
			if err != nil || len(items) != 1 || !rows.closed {
				t.Fatalf("items = %#v, error = %v, closed = %v", items, err, rows.closed)
			}
			encoded, err := json.Marshal(items[0])
			wantJSON := `{"regionName":"试衣区","exposureCount":20,"stayCount":5,"stayRate":0.25,"avgStayTime":63.5,"fetchedAt":"2026-11-02T01:02:03+08:00"}`
			if err != nil || string(encoded) != wantJSON {
				t.Fatalf("area JSON = %s, error = %v", encoded, err)
			}
		})
	}
}

func TestMonitoringQueriesRejectInvalidInputsBeforeOracle(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("invalid query reached Oracle")
		return nil, nil
	})
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"no dates", func() error { _, err := adapter.QueryMonitoringHours(t.Context(), "shop", 7, nil); return err }},
		{"too many dates", func() error {
			_, err := adapter.QueryMonitoringHours(t.Context(), "shop", 7, make([]time.Time, 32))
			return err
		}},
		{"zero date", func() error {
			_, err := adapter.QueryMonitoringHours(t.Context(), "shop", 7, []time.Time{{}})
			return err
		}},
		{"empty shop", func() error {
			_, err := adapter.QueryMonitoringHours(t.Context(), "  ", 7, []time.Time{date})
			return err
		}},
		{"missing mall", func() error {
			_, err := adapter.QueryMonitoringHours(t.Context(), "shop", 0, []time.Time{date})
			return err
		}},
		{"nil context", func() error { _, err := adapter.QueryMonitoringHours(nil, "shop", 7, []time.Time{date}); return err }},
		{"closed adapter", func() error {
			_, err := (*Adapter)(nil).QueryMonitoringHours(t.Context(), "shop", 7, []time.Time{date})
			return err
		}},
		{"area zero date", func() error {
			_, err := adapter.QueryMonitoringAreas(t.Context(), "shop", 7, time.Time{}, "")
			return err
		}},
		{"area empty shop", func() error { _, err := adapter.QueryMonitoringAreas(t.Context(), "", 7, date, ""); return err }},
		{"area missing mall", func() error { _, err := adapter.QueryMonitoringAreas(t.Context(), "shop", 0, date, ""); return err }},
		{"area nil context", func() error { _, err := adapter.QueryMonitoringAreas(nil, "shop", 7, date, ""); return err }},
		{"area closed adapter", func() error {
			_, err := (&Adapter{}).QueryMonitoringAreas(t.Context(), "shop", 7, date, "")
			return err
		}},
		{"area overlong name", func() error {
			_, err := adapter.QueryMonitoringAreas(t.Context(), "shop", 7, date, strings.Repeat("区", 256))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("invalid query succeeded")
			}
		})
	}
}

func TestMonitoringQueriesReturnEmptyListsAndDiscardPartialErrors(t *testing.T) {
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, operation := range []struct {
		name  string
		value []driver.Value
		run   func(*Adapter) (int, bool, error)
	}{
		{"hours", monitoringTestHourValues(), func(adapter *Adapter) (int, bool, error) {
			items, err := adapter.QueryMonitoringHours(t.Context(), "shop", 7, []time.Time{date})
			return len(items), items == nil, err
		}},
		{"areas", monitoringTestAreaValues(), func(adapter *Adapter) (int, bool, error) {
			items, err := adapter.QueryMonitoringAreas(t.Context(), "shop", 7, date, "")
			return len(items), items == nil, err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for _, failure := range []string{"empty", "query", "scan", "timestamp", "iteration", "close"} {
				t.Run(failure, func(t *testing.T) {
					failureErr := errors.New("Oracle " + failure + " failed")
					rows := &bojunQueryTestRows{columns: make([]string, len(operation.value))}
					if failure != "empty" {
						rows.values = [][]driver.Value{append([]driver.Value(nil), operation.value...)}
					}
					switch failure {
					case "scan":
						rows.columns = []string{"wrong column count"}
					case "timestamp":
						rows.values[0][len(operation.value)-1] = "invalid timestamp"
					case "iteration":
						rows.nextErr = failureErr
					case "close":
						rows.closeErr = failureErr
					}
					adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
						if failure == "query" {
							return nil, failureErr
						}
						return rows, nil
					})
					count, isNil, err := operation.run(adapter)
					if failure == "empty" {
						if err != nil || count != 0 || isNil {
							t.Fatalf("empty result: count=%d nil=%v err=%v", count, isNil, err)
						}
					} else if err == nil || !isNil || (failure != "scan" && failure != "timestamp" && !errors.Is(err, failureErr)) {
						t.Fatalf("error or partial result was lost: count=%d nil=%v err=%v", count, isNil, err)
					}
					if failure != "query" && !rows.closed {
						t.Fatal("rows were not closed")
					}
				})
			}
		})
	}
}

func TestMonitoringQueriesEnforceRowLimits(t *testing.T) {
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, operation := range []struct {
		name  string
		limit int
		value []driver.Value
		run   func(*Adapter) (int, error)
	}{
		{"hours", 744, monitoringTestHourValues(), func(adapter *Adapter) (int, error) {
			items, err := adapter.QueryMonitoringHours(t.Context(), "shop", 7, []time.Time{date})
			return len(items), err
		}},
		{"areas", 1000, monitoringTestAreaValues(), func(adapter *Adapter) (int, error) {
			items, err := adapter.QueryMonitoringAreas(t.Context(), "shop", 7, date, "")
			return len(items), err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for _, count := range []int{operation.limit, operation.limit + 1} {
				rows := &bojunQueryTestRows{columns: make([]string, len(operation.value)), values: make([][]driver.Value, count)}
				for i := range rows.values {
					rows.values[i] = operation.value
				}
				adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return rows, nil })
				length, err := operation.run(adapter)
				if count == operation.limit && (err != nil || length != count) {
					t.Fatalf("valid boundary: count=%d err=%v", length, err)
				}
				if count > operation.limit && (!errors.Is(err, ErrMonitoringTooManyRows) || length != 0) {
					t.Fatalf("overflow returned partial results: count=%d err=%v", length, err)
				}
				if !rows.closed {
					t.Fatal("rows were not closed")
				}
			}
		})
	}
}

func TestMonitoringQueriesPreserveCancellationAndDeadlines(t *testing.T) {
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, operation := range []struct {
		name string
		run  func(context.Context, *Adapter) error
	}{
		{"hours", func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.QueryMonitoringHours(ctx, "shop", 7, []time.Time{date})
			return err
		}},
		{"areas", func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.QueryMonitoringAreas(ctx, "shop", 7, date, "")
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("canceled context reached Oracle")
				return nil, nil
			})
			if err := operation.run(ctx, adapter); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error = %v", err)
			}
			ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			adapter = newBojunQueryTestAdapter(t, func(queryContext context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
				if _, ok := queryContext.Deadline(); !ok {
					t.Fatal("request deadline was lost")
				}
				<-queryContext.Done()
				return nil, queryContext.Err()
			})
			if err := operation.run(ctx, adapter); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error = %v", err)
			}
		})
	}
}
