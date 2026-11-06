package reportoracle

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
)

func TestTmallOrdersQuerySelectsPublicFieldsAndBindsTimeRange(t *testing.T) {
	for _, field := range []string{"", "modified"} {
		t.Run(field, func(t *testing.T) {
			values := make([]driver.Value, 18)
			values[0], values[1], values[17] = "9007199254740993123", "2026-11-06 01:02:03", "123456789.12345678"
			rows := &bojunQueryTestRows{columns: make([]string, 18), values: [][]driver.Value{values}}
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				col := "CREATED"
				if field == "modified" {
					col = "MODIFIED"
				}
				for _, fragment := range []string{"FROM YL_DBS.DWD_TF_TM_ORDERS a", "WHERE a." + col + " >= TO_DATE(:1, 'YYYY-MM-DD HH24:MI:SS')", "a." + col + " < TO_DATE(:2, 'YYYY-MM-DD HH24:MI:SS')", "a.TID = :3", "ORDER BY a." + col + ", a.ID", "OFFSET :4 ROWS FETCH NEXT :5 ROWS ONLY"} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				projection := strings.Split(statement, "\nFROM")[0]
				for _, private := range []string{"a.ID", "ETL_PLATFORM_JSON_ID", "PRICE_PRE", "OUTER_IID"} {
					if strings.Contains(projection, private) {
						t.Fatalf("private column in projection: %s", projection)
					}
				}
				if strings.Contains(statement, "COUNT(") || strings.Contains(statement, "9007199254740993123") {
					t.Fatalf("unbounded/unsafe query: %s", statement)
				}
				want := []any{"2026-11-06 00:00:00", "2026-11-07 00:00:00", "9007199254740993123", 50, 51}
				got := make([]any, len(args)-1)
				for i := range got {
					got[i] = args[i].Value
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("binds=%v want=%v", got, want)
				}
				return rows, nil
			})
			result, err := adapter.QueryTmall(t.Context(), TmallDataset("orders"), TmallQuery{StartTime: "2026-11-06 00:00:00", EndTime: "2026-11-07 00:00:00", TimeField: field, Filters: map[string]string{"TID": "9007199254740993123"}, Offset: 50, PageSize: 50})
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 1 || len(result[0]) != 18 || *result[0]["TID"] != "9007199254740993123" || *result[0]["PAYMENT"] != "123456789.12345678" || result[0]["PAY_TIME"] != nil || !rows.closed {
				t.Fatalf("unexpected rows: %+v", result)
			}
			for _, col := range TmallColumns(TmallDataset("orders")) {
				if col.Name == "ID" || col.Name == "ETL_PLATFORM_JSON_ID" {
					t.Fatalf("private metadata: %+v", col)
				}
			}
		})
	}
}

func TestTmallOrdersRejectsInvalidTimeAndDimensionsBeforeQuery(t *testing.T) {
	valid := TmallQuery{StartTime: "2026-11-06 00:00:00", EndTime: "2026-11-07 00:00:00", PageSize: 50}
	for _, change := range []func(*TmallQuery){
		func(q *TmallQuery) { q.StartTime = "" },
		func(q *TmallQuery) { q.EndTime = q.StartTime },
		func(q *TmallQuery) { q.EndTime = "2026-11-05 00:00:00" },
		func(q *TmallQuery) { q.EndTime = "2026-12-08 00:00:00" },
		func(q *TmallQuery) { q.TimeField = "pay_time" },
		func(q *TmallQuery) { q.TimeField = "CREATED OR 1=1" },
		func(q *TmallQuery) { q.BatchNo = "202611060000" },
		func(q *TmallQuery) { q.StartDate = "2026-11-06" },
		func(q *TmallQuery) { q.Filters = map[string]string{"ID": "1"} },
		func(q *TmallQuery) { q.Filters = map[string]string{"PAYMENT": "1"} },
		func(q *TmallQuery) { q.Filters = map[string]string{"NUM_IID": "not-a-number"} },
	} {
		q := valid
		change(&q)
		adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			t.Fatal("invalid query reached Oracle")
			return nil, nil
		})
		if _, err := adapter.QueryTmall(t.Context(), TmallDataset("orders"), q); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}
