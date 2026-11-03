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

func TestQueryTmallBindsDimensionsAndPreservesPrecisionAndNulls(t *testing.T) {
	const product = "001' OR 1=1 --"
	values := make([]driver.Value, 36)
	values[0], values[1], values[3], values[19], values[34] = "9007199254740993123", "20261001", "000123", "123456789012.123456", "2026-10-02 09:10:11"
	rows := &bojunQueryTestRows{columns: make([]string, 36), values: [][]driver.Value{values}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() || !strings.Contains(statement, "FROM YL_DBS.ODS_TM_SYCM_PRODUCTORDER a") || !strings.Contains(statement, "a.PRODUCT_ID = :") || strings.Contains(statement, product) || strings.Contains(statement, "SELECT *") {
			t.Fatalf("unexpected query: %s", statement)
		}
		found := false
		for _, arg := range args {
			if arg.Value == product {
				found = true
			}
		}
		if !found || args[len(args)-3].Value != 50 || args[len(args)-2].Value != 51 {
			t.Fatalf("unexpected binds: %#v", args)
		}
		if _, ok := args[len(args)-1].Value.(godror.Option); !ok {
			t.Fatal("Oracle numbers must be returned as strings")
		}
		return rows, nil
	})
	result, err := adapter.QueryTmall(t.Context(), TmallSycm, TmallQuery{StartDate: "2026-10-01", EndDate: "2026-10-01", Filters: map[string]string{"PRODUCT_ID": product}, Offset: 50, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || len(result[0]) != 36 || *result[0]["ID"] != "9007199254740993123" || *result[0]["PAY_DATE"] != "20261001" || *result[0]["PAY_AMT"] != "123456789012.123456" || *result[0]["PRODUCT_ID"] != "000123" || result[0]["PRODUCT_NAME"] != nil || *result[0]["CREATED_ODS"] != "2026-10-02 09:10:11" || !rows.closed {
		t.Fatalf("result=%+v closed=%v", result, rows.closed)
	}
}

func TestTmallRejectsUnknownDatasetAndUnapprovedFilters(t *testing.T) {
	for _, test := range []struct {
		name    string
		dataset TmallDataset
		filters map[string]string
	}{
		{"unknown dataset", TmallDataset("ODS_OTHER_TABLE"), nil},
		{"SQL in column name", TmallSycm, map[string]string{"PRODUCT_ID OR 1=1": "1"}},
		{"wrong dataset dimension", TmallSycm, map[string]string{"ENTITY_ID": "1"}},
		{"removed entity ID", TmallQztg, map[string]string{"ENTITY_ID": "1"}},
		{"removed entity type", TmallQztg, map[string]string{"ENTITY_TYPE": "商品"}},
		{"removed entity name", TmallQztg, map[string]string{"ENTITY_NAME": "商品名称"}},
		{"metric filter", TmallQztg, map[string]string{"COST": "100"}},
		{"empty filter", TmallTot, map[string]string{"PRODUCT_ID": " "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("invalid query reached Oracle")
				return nil, nil
			})
			if _, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{StartDate: "2026-10-01", EndDate: "2026-10-01", Filters: test.filters, PageSize: 50}); err == nil {
				t.Fatalf("accepted %+v", test)
			}
		})
	}
}

func TestQueryTmallClosesRowsAndPropagatesReadFailure(t *testing.T) {
	failure := errors.New("connection interrupted")
	rows := &bojunQueryTestRows{columns: make([]string, 30), nextErr: failure}
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return rows, nil })
	_, err := adapter.QueryTmall(t.Context(), TmallTot, TmallQuery{StartDate: "2026-10-01", EndDate: "2026-10-01", PageSize: 50})
	if !errors.Is(err, failure) || !rows.closed {
		t.Fatalf("error=%v closed=%v", err, rows.closed)
	}
}

func TestTmallColumnsPreserveDatabaseComments(t *testing.T) {
	columns := TmallColumns(TmallSycm)
	if len(columns) != 36 || columns[19].Name != "PAY_AMT" || columns[19].Comment != "支付金额(元)" || columns[19].DataType != "NUMBER" {
		t.Fatalf("unexpected metadata: %+v", columns)
	}
	columns[19].Comment = "changed"
	if TmallColumns(TmallSycm)[19].Comment != "支付金额(元)" {
		t.Fatal("metadata shares mutable storage")
	}
}

func TestTmallQueriesUseDateIndexAndBindFiltersInOrder(t *testing.T) {
	for _, test := range []struct {
		dataset     TmallDataset
		table       string
		columns     int
		filters     map[string]string
		wantFilters []any
		wantDates   []any
	}{
		{TmallSycm, "ODS_TM_SYCM_PRODUCTORDER", 36, map[string]string{"PRODUCT_ID": "001", "ITEM_NO": "A01"}, []any{"A01", "001"}, []any{"20251231", "20260101"}},
		{TmallQztg, "ODS_TM_QZTG_PRODUCT", 90, map[string]string{"PLAN_ID": "001", "PRODUCT_TYPE": "商品"}, []any{"001", "商品"}, []any{"2025-12-31", "2026-01-01"}},
		{TmallTot, "ODS_TM_TOT_PRODUCT", 30, map[string]string{"ITEM_NO": "A01", "CATEGORY": "外套"}, []any{"外套", "A01"}, []any{"2025-12-31", "2026-01-01"}},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				for _, fragment := range []string{"FROM YL_DBS." + test.table + " a", "a.PAY_DATE >= :1 AND a.PAY_DATE < :2", "ORDER BY a.PAY_DATE, a.ID", "OFFSET :5 ROWS FETCH NEXT :6 ROWS ONLY"} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				if strings.Contains(statement, "TO_DATE(a.PAY_DATE") || strings.Contains(statement, "TO_CHAR(a.PAY_DATE") || strings.Contains(statement, "COUNT(") {
					t.Fatalf("query disables index or adds a count: %s", statement)
				}
				want := append(append([]any{}, test.wantDates...), test.wantFilters...)
				want = append(want, 0, 51)
				got := make([]any, len(args)-1)
				for i := range got {
					got[i] = args[i].Value
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("binds=%v want=%v", got, want)
				}
				return &bojunQueryTestRows{columns: make([]string, test.columns)}, nil
			})
			rows, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{StartDate: "2025-12-31", EndDate: "2025-12-31", Filters: test.filters, PageSize: 50})
			if err != nil || rows == nil {
				t.Fatalf("rows=%v error=%v", rows, err)
			}
		})
	}
}

func TestQueryTmallBindsSourceDateRanges(t *testing.T) {
	for _, test := range []struct {
		dataset                              TmallDataset
		name, start, end, wantStart, wantEnd string
	}{
		{TmallSycm, "same day", "2025-01-01", "2025-01-01", "20250101", "20250102"},
		{TmallSycm, "month boundary", "2025-01-30", "2025-01-31", "20250130", "20250201"},
		{TmallSycm, "year boundary", "2025-12-30", "2025-12-31", "20251230", "20260101"},
		{TmallQztg, "same day", "2025-01-01", "2025-01-01", "2025-01-01", "2025-01-02"},
		{TmallQztg, "month boundary", "2025-01-30", "2025-01-31", "2025-01-30", "2025-02-01"},
		{TmallQztg, "year boundary", "2025-12-30", "2025-12-31", "2025-12-30", "2026-01-01"},
		{TmallTot, "same day", "2025-01-01", "2025-01-01", "2025-01-01", "2025-01-02"},
		{TmallTot, "month boundary", "2025-01-30", "2025-01-31", "2025-01-30", "2025-02-01"},
		{TmallTot, "year boundary", "2025-12-30", "2025-12-31", "2025-12-30", "2026-01-01"},
	} {
		t.Run(string(test.dataset)+"/"+test.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				if !strings.Contains(statement, "WHERE a.PAY_DATE >= :1 AND a.PAY_DATE < :2") {
					t.Fatalf("date range must compare the source column directly: %s", statement)
				}
				if args[0].Value != test.wantStart || args[1].Value != test.wantEnd {
					t.Fatalf("date binds = [%v %v], want [%s %s]", args[0].Value, args[1].Value, test.wantStart, test.wantEnd)
				}
				return &bojunQueryTestRows{columns: make([]string, len(TmallColumns(test.dataset)))}, nil
			})
			_, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{StartDate: test.start, EndDate: test.end, PageSize: 50})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueryTmallReturnsCurrentColumnsAndExactDecimals(t *testing.T) {
	for _, test := range []struct {
		dataset TmallDataset
		count   int
		values  map[int]string
		want    map[string]string
		filters map[string]string
	}{
		{
			dataset: TmallSycm, count: 36,
			values: map[int]string{1: "20261001", 9: "0.123456", 16: "0.234567", 20: "0.345678", 30: "0.456789"},
			want:   map[string]string{"PAY_DATE": "20261001", "DETAIL_BOUNCE_RATE": "0.123456", "ORDER_CONV_RATE": "0.234567", "PAY_CONV_RATE": "0.345678", "SEARCH_GUIDE_PAY_CONV_RATE": "0.456789"},
		},
		{
			dataset: TmallQztg, count: 90,
			values:  map[int]string{1: "2026-10-01", 8: "000123", 9: "商品", 10: "测试商品"},
			want:    map[string]string{"PAY_DATE": "2026-10-01", "PRODUCT_ID": "000123", "PRODUCT_TYPE": "商品", "PRODUCT_NAME": "测试商品"},
			filters: map[string]string{"PRODUCT_ID": "000123", "PRODUCT_TYPE": "商品", "PRODUCT_NAME": "测试商品"},
		},
		{
			dataset: TmallTot, count: 30,
			values: map[int]string{1: "2026-10-01", 25: "123456789012.123456"},
			want:   map[string]string{"PAY_DATE": "2026-10-01", "TOMORROW_QZTG_BUDGET": "123456789012.123456"},
		},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			values := make([]driver.Value, test.count)
			for index, value := range test.values {
				values[index] = value
			}
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				for _, oldName := range []string{"STAT_DATE", "ENTITY_ID", "ENTITY_TYPE", "ENTITY_NAME"} {
					if strings.Contains(statement, "a."+oldName) {
						t.Fatalf("query contains removed column %s: %s", oldName, statement)
					}
				}
				if test.dataset == TmallQztg {
					for _, fragment := range []string{"a.PRODUCT_ID = :3", "a.PRODUCT_NAME = :4", "a.PRODUCT_TYPE = :5"} {
						if !strings.Contains(statement, fragment) {
							t.Fatalf("missing bound product filter %s: %s", fragment, statement)
						}
					}
					if args[2].Value != "000123" || args[3].Value != "测试商品" || args[4].Value != "商品" {
						t.Fatalf("unexpected product filter binds: %v", args)
					}
				}
				return &bojunQueryTestRows{columns: make([]string, test.count), values: [][]driver.Value{values}}, nil
			})
			rows, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{StartDate: "2026-10-01", EndDate: "2026-10-01", Filters: test.filters, PageSize: 50})
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%v error=%v", rows, err)
			}
			for name, want := range test.want {
				if got := rows[0][name]; got == nil || *got != want {
					t.Errorf("%s=%v want %q", name, got, want)
				}
			}
			for _, oldName := range []string{"STAT_DATE", "ENTITY_ID", "ENTITY_TYPE", "ENTITY_NAME"} {
				if _, exists := rows[0][oldName]; exists {
					t.Errorf("response contains removed column %s", oldName)
				}
			}
		})
	}
}
