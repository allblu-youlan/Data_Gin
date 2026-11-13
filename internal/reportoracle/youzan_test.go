package reportoracle

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func TestNormalizeYouzanQuery(t *testing.T) {
	query := YouzanQuery{
		StartTime: " 2026-10-01 00:00:00 ", EndTime: "2026-11-01 00:00:00",
		Filters: map[string]string{"TID": " E001 "}, PageSize: 100, Offset: 99900,
	}
	got, err := NormalizeYouzanQuery(YouzanOrders, query)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeField != "modifiedDwd" || got.StartTime != "2026-10-01 00:00:00" || got.Filters["TID"] != "E001" {
		t.Fatalf("unexpected normalization: %#v", got)
	}
	got.Filters["TID"] = "changed"
	if query.Filters["TID"] != " E001 " {
		t.Fatal("normalization mutated the caller's filters")
	}
	for _, dataset := range []YouzanDataset{YouzanOrders, YouzanOrderItems} {
		got, err := NormalizeYouzanQuery(dataset, YouzanQuery{Filters: map[string]string{"TID": "E001"}, PageSize: 1})
		if err != nil || got.TimeField != "modifiedDwd" {
			t.Fatalf("exact TID lookup for %s failed: %#v, %v", dataset, got, err)
		}
	}
}

func TestQueryYouzanRejectsInvalidQueriesBeforeDatabase(t *testing.T) {
	valid := YouzanQuery{StartTime: "2026-10-01 00:00:00", EndTime: "2026-11-01 00:00:00", PageSize: 20}
	for _, test := range []struct {
		name    string
		dataset YouzanDataset
		change  func(*YouzanQuery)
	}{
		{name: "unknown dataset", dataset: "other"},
		{name: "missing range", change: func(q *YouzanQuery) { q.StartTime, q.EndTime = "", "" }},
		{name: "partial range with TID", change: func(q *YouzanQuery) { q.EndTime = ""; q.Filters = map[string]string{"TID": "E001"} }},
		{name: "missing start", change: func(q *YouzanQuery) { q.StartTime = "" }},
		{name: "too long", change: func(q *YouzanQuery) { q.EndTime = "2026-11-01 00:00:01" }},
		{name: "empty range", change: func(q *YouzanQuery) { q.EndTime = q.StartTime }},
		{name: "reversed range", change: func(q *YouzanQuery) { q.EndTime = "2026-09-30 00:00:00" }},
		{name: "invalid time", change: func(q *YouzanQuery) { q.StartTime = "2026-09-31 00:00:00" }},
		{name: "noncanonical time", change: func(q *YouzanQuery) { q.StartTime = "2026-10-01 0:00:00" }},
		{name: "unknown time field", change: func(q *YouzanQuery) { q.TimeField = "created; DROP TABLE x" }},
		{name: "unknown filter", change: func(q *YouzanQuery) { q.Filters = map[string]string{"BUYER_PHONE": "123"} }},
		{name: "empty filter", change: func(q *YouzanQuery) { q.Filters = map[string]string{"TID": " "} }},
		{name: "long TID", change: func(q *YouzanQuery) { q.Filters = map[string]string{"TID": strings.Repeat("a", 201)} }},
		{name: "long status", change: func(q *YouzanQuery) { q.Filters = map[string]string{"STATUS": strings.Repeat("a", 101)} }},
		{name: "main cannot filter SKU", change: func(q *YouzanQuery) { q.Filters = map[string]string{"SKU_NO": "a"} }},
		{name: "items cannot filter status", dataset: YouzanOrderItems, change: func(q *YouzanQuery) { q.Filters = map[string]string{"STATUS": "a"} }},
		{name: "zero page size", change: func(q *YouzanQuery) { q.PageSize = 0 }},
		{name: "large page size", change: func(q *YouzanQuery) { q.PageSize = 101 }},
		{name: "negative offset", change: func(q *YouzanQuery) { q.Offset = -1 }},
		{name: "too deep page", change: func(q *YouzanQuery) { q.Offset = 1000 * q.PageSize }},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := valid
			if test.change != nil {
				test.change(&query)
			}
			dataset := test.dataset
			if dataset == "" {
				dataset = YouzanOrders
			}
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("invalid query reached the database")
				return nil, nil
			})
			if _, err := adapter.QueryYouzan(t.Context(), dataset, query); err == nil {
				t.Fatal("expected query validation error")
			}
		})
	}
}

func TestQueryYouzanProjectionBindingsAndExactValues(t *testing.T) {
	for _, test := range []struct {
		name       string
		dataset    YouzanDataset
		table      string
		columns    string
		dates      string
		timeField  string
		timeColumn string
		filters    map[string]string
		bindings   []any
	}{
		{
			name: "orders modified time", dataset: YouzanOrders, table: "YL_DBS.DWD_YOUZAN_ORDERS",
			columns:    "ID,TID,STATUS,STATUS_STR,TYPE,NODE_KDT_ID,ROOT_KDT_ID,SHOP_NAME,CREATED,UPDATE_TIME,PAY_TIME,SUCCESS_TIME,CONSIGN_TIME,CONFIRM_TIME,TOTAL_FEE,POST_FEE,PAYMENT,REFUND_STATE,CREATED_DWD,MODIFIED_DWD",
			dates:      "CREATED,UPDATE_TIME,PAY_TIME,SUCCESS_TIME,CONSIGN_TIME,CONFIRM_TIME,CREATED_DWD,MODIFIED_DWD",
			timeColumn: "MODIFIED_DWD", filters: map[string]string{"TID": "001' OR 1=1 --", "STATUS": "WAIT_BUYER_PAY", "ROOT_KDT_ID": "002", "NODE_KDT_ID": "001"},
			bindings: []any{"2026-10-01 00:00:00", "2026-10-02 00:00:00", "001", "002", "WAIT_BUYER_PAY", "001' OR 1=1 --", 40, 21},
		},
		{
			name: "items created time", dataset: YouzanOrderItems, table: "YL_DBS.DWD_YOUZAN_ORDERS_ITEM",
			columns: "ID,TID,OID,TITLE,NUM,ITEM_ID,SKU_ID,ITEM_NO,SKU_NO,ITEM_BARCODE,SKU_BARCODE,PRICE,DISCOUNT_PRICE,TOTAL_FEE,PAYMENT,CREATED,UPDATE_TIME,CREATED_DWD,MODIFIED_DWD",
			dates:   "CREATED,UPDATE_TIME,CREATED_DWD,MODIFIED_DWD", timeField: "created", timeColumn: "CREATED",
			filters:  map[string]string{"TID": "E001", "OID": "0002", "ITEM_ID": "0003", "SKU_ID": "0004", "SKU_NO": "SKU-5"},
			bindings: []any{"2026-10-01 00:00:00", "2026-10-02 00:00:00", "0003", "0002", "0004", "SKU-5", "E001", 40, 21},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			columns := strings.Split(test.columns, ",")
			values := make([]driver.Value, len(columns))
			projection := make([]string, len(columns))
			for i, column := range columns {
				projection[i] = "a." + column
				if strings.Contains(","+test.dates+",", ","+column+",") {
					projection[i] = "TO_CHAR(a." + column + ", 'YYYY-MM-DD HH24:MI:SS')"
				}
				switch column {
				case "ID":
					values[i] = "12345678901234567890"
				case "PAYMENT":
					values[i] = "123456789012.12345678"
				case "TID":
					values[i] = "001"
				}
			}
			rows := &bojunQueryTestRows{columns: columns, values: [][]driver.Value{values}}
			adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				if ctx != t.Context() {
					t.Fatal("query did not propagate request context")
				}
				wantPrefix := "SELECT " + strings.Join(projection, ", ") + "\nFROM " + test.table + " a\n"
				if !strings.HasPrefix(statement, wantPrefix) {
					t.Fatalf("unexpected projection or table: %s", statement)
				}
				for _, fragment := range []string{
					"WHERE a." + test.timeColumn + " >= TO_DATE(:1, 'YYYY-MM-DD HH24:MI:SS') AND a." + test.timeColumn + " < TO_DATE(:2, 'YYYY-MM-DD HH24:MI:SS')",
					"ORDER BY a." + test.timeColumn + ", a.ID\nOFFSET",
				} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				if strings.Contains(statement, "OR 1=1") || strings.Contains(statement, "COUNT(") {
					t.Fatalf("unexpected interpolation or count query: %s", statement)
				}
				assertYouzanBindings(t, args, test.bindings)
				return rows, nil
			})
			got, err := adapter.QueryYouzan(t.Context(), test.dataset, YouzanQuery{
				StartTime: "2026-10-01 00:00:00", EndTime: "2026-10-02 00:00:00", TimeField: test.timeField,
				Filters: test.filters, Offset: 40, PageSize: 20,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || len(got[0]) != len(columns) || !rows.closed || *got[0]["ID"] != "12345678901234567890" ||
				*got[0]["PAYMENT"] != "123456789012.12345678" || *got[0]["TID"] != "001" || got[0]["UPDATE_TIME"] != nil {
				t.Fatalf("unexpected rows or precision/NULL mapping: %#v", got)
			}
		})
	}
}

func TestQueryYouzanExactTIDWithoutTime(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(statement, "\nWHERE a.TID = :1\nORDER BY a.ID\nOFFSET :2 ROWS FETCH NEXT :3 ROWS ONLY") || strings.Contains(statement, "TO_DATE(") {
			t.Fatalf("unexpected exact TID query: %s", statement)
		}
		assertYouzanBindings(t, args, []any{"E001", 0, 11})
		return &bojunQueryTestRows{}, nil
	})
	got, err := adapter.QueryYouzan(t.Context(), YouzanOrderItems, YouzanQuery{Filters: map[string]string{"TID": "E001"}, PageSize: 10})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("expected a non-nil empty result: %#v, %v", got, err)
	}
}

func TestQueryYouzanPropagatesDatabaseErrorsAndClosesRows(t *testing.T) {
	wantErr := errors.New("database unavailable")
	for _, test := range []struct {
		name     string
		queryErr error
		rows     *bojunQueryTestRows
	}{
		{name: "query", queryErr: wantErr},
		{name: "iteration", rows: &bojunQueryTestRows{nextErr: wantErr}},
		{name: "close", rows: &bojunQueryTestRows{closeErr: wantErr}},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				if test.queryErr != nil {
					return nil, test.queryErr
				}
				return test.rows, nil
			})
			_, err := adapter.QueryYouzan(t.Context(), YouzanOrders, YouzanQuery{Filters: map[string]string{"TID": "E001"}, PageSize: 1})
			if !errors.Is(err, wantErr) || (test.rows != nil && !test.rows.closed) {
				t.Fatalf("expected closed rows and wrapped database error, got %v", err)
			}
		})
	}
}

func assertYouzanBindings(t *testing.T, args []driver.NamedValue, want []any) {
	t.Helper()
	if len(args) != len(want)+1 {
		t.Fatalf("unexpected arguments: %#v", args)
	}
	got := make([]any, len(want))
	for i := range want {
		got[i] = args[i].Value
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings = %#v, want %#v", got, want)
	}
	if _, ok := args[len(args)-1].Value.(godror.Option); !ok {
		t.Fatal("query is missing godror.NumberAsString")
	}
}
