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
	values[0], values[1], values[3], values[19], values[34] = "9007199254740993123", "2026-10-01", "000123", "123456789012.123456", "2026-10-02 09:10:11"
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
	if len(result) != 1 || len(result[0]) != 36 || *result[0]["ID"] != "9007199254740993123" || *result[0]["PAY_AMT"] != "123456789012.123456" || *result[0]["PRODUCT_ID"] != "000123" || result[0]["PRODUCT_NAME"] != nil || *result[0]["CREATED_ODS"] != "2026-10-02 09:10:11" || !rows.closed {
		t.Fatalf("result=%+v closed=%v", result, rows.closed)
	}
}

func TestTmallRejectsUnknownDatasetAndUnapprovedFilters(t *testing.T) {
	for _, test := range []struct {
		dataset TmallDataset
		filters map[string]string
	}{
		{TmallDataset("ODS_OTHER_TABLE"), nil},
		{TmallSycm, map[string]string{"PRODUCT_ID OR 1=1": "1"}},
		{TmallSycm, map[string]string{"ENTITY_ID": "1"}},
		{TmallQztg, map[string]string{"COST": "100"}},
		{TmallTot, map[string]string{"PRODUCT_ID": " "}},
	} {
		adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			t.Fatal("invalid query reached Oracle")
			return nil, nil
		})
		if _, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{StartDate: "2026-10-01", EndDate: "2026-10-01", Filters: test.filters, PageSize: 50}); err == nil {
			t.Fatalf("accepted %+v", test)
		}
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
	}{
		{TmallSycm, "ODS_TM_SYCM_PRODUCTORDER", 36, map[string]string{"PRODUCT_ID": "001", "ITEM_NO": "A01"}, []any{"A01", "001"}},
		{TmallQztg, "ODS_TM_QZTG_PRODUCT", 90, map[string]string{"PLAN_ID": "001", "ENTITY_TYPE": "商品"}, []any{"商品", "001"}},
		{TmallTot, "ODS_TM_TOT_PRODUCT", 30, map[string]string{"ITEM_NO": "A01", "CATEGORY": "外套"}, []any{"外套", "A01"}},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				for _, fragment := range []string{"FROM YL_DBS." + test.table + " a", "a.STAT_DATE >= :1 AND a.STAT_DATE < :2", "ORDER BY a.STAT_DATE, a.ID", "OFFSET :5 ROWS FETCH NEXT :6 ROWS ONLY"} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				if strings.Contains(statement, "TO_DATE(a.STAT_DATE") || strings.Contains(statement, "TO_CHAR(a.STAT_DATE") || strings.Contains(statement, "COUNT(") {
					t.Fatalf("query disables index or adds a count: %s", statement)
				}
				want := append([]any{"2025-12-31", "2026-01-01"}, test.wantFilters...)
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
