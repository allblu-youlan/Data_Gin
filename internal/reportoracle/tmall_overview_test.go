package reportoracle

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func TestNormalizeTmallOverviewDimensions(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		valid   bool
	}{
		{"all source dimensions", map[string]string{"SECTION_NAME": "首页", "MENU_NAME": "数据概览", "DATA_POINT": "实时", "DIMENSION_NAME": "本店", "DATE_RANGE": "2026-11-13"}, true},
		{"100 Chinese characters", map[string]string{"SECTION_NAME": strings.Repeat("栏", 100)}, true},
		{"50 dimension characters", map[string]string{"DIMENSION_NAME": strings.Repeat("维", 50)}, true},
		{"too many ASCII characters", map[string]string{"SECTION_NAME": strings.Repeat("a", 101)}, false},
		{"too many Chinese characters", map[string]string{"DIMENSION_NAME": strings.Repeat("维", 51)}, false},
		{"unknown column", map[string]string{"PRODUCT_ID": "1"}, false},
		{"metric", map[string]string{"PAYMENT_AMOUNT": "1"}, false},
		{"batch in filters", map[string]string{"BATCH_NO": "202611131700"}, false},
		{"empty dimension", map[string]string{"SECTION_NAME": " "}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeTmallQuery(TmallSycmOverviewHourly, TmallQuery{BatchNo: "202611131700", Filters: test.filters, PageSize: 50})
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestQueryTmallOverviewBindsHourAndPreservesSourceValues(t *testing.T) {
	const section = "首页' OR 1=1 --"
	values := make([]driver.Value, 60)
	values[0], values[1], values[6] = "900719925474099312", "首页", "2026-11-13 17:41:48"
	values[7], values[8], values[9] = "123456789012345678.90", "-0.29980000", "200000000000000000.00"
	values[10], values[16], values[37] = "999999999999999999", "0.06050000", "123.1234567890123456789"
	values[55], values[56], values[57], values[58], values[59] = "202611131700", "2026-11-13 17:42:00", "2026-11-13 17:43:00", "2026-11-13 17:43:00", "2026-11-13 17:43:00"
	rows := &bojunQueryTestRows{columns: make([]string, 60), values: [][]driver.Value{values, values}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
		for _, fragment := range []string{
			"FROM YL_DBS.ODS_TM_SYCM_REALTIME_OVERVIEW a",
			"WHERE a.BATCH_NO = :1", "a.DIMENSION_NAME = :2", "a.SECTION_NAME = :3",
			"ORDER BY a.ID\nOFFSET :4 ROWS FETCH NEXT :5 ROWS ONLY",
		} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("missing %q in %s", fragment, statement)
			}
		}
		for _, name := range []string{"EXPORT_TIME", "DOWNLOAD_TIME", "WRITE_TIME", "CREATED_TIME", "MODIFIED_TIME"} {
			if !strings.Contains(statement, "TO_CHAR(a."+name+", 'YYYY-MM-DD HH24:MI:SS')") {
				t.Errorf("date %s is not formatted", name)
			}
		}
		if ctx != t.Context() || strings.Contains(statement, section) || strings.Contains(statement, "STAT_DATE") || strings.Contains(statement, "SELECT *") || strings.Contains(statement, "COUNT(") {
			t.Fatalf("unexpected overview SQL: %s", statement)
		}
		want := []any{"202611131700", "本店", section, 1, 2}
		got := make([]any, len(args)-1)
		for i := range got {
			got[i] = args[i].Value
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("binds=%#v want=%#v", got, want)
		}
		if _, ok := args[len(args)-1].Value.(godror.Option); !ok {
			t.Fatal("Oracle numbers must be returned as strings")
		}
		return rows, nil
	})
	result, err := adapter.QueryTmall(t.Context(), TmallSycmOverviewHourly, TmallQuery{
		BatchNo: "202611131700", Filters: map[string]string{"SECTION_NAME": section, "DIMENSION_NAME": "本店"}, Offset: 1, PageSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || len(result[0]) != 60 || !rows.closed {
		t.Fatalf("rows=%d closed=%v", len(result), rows.closed)
	}
	for name, want := range map[string]string{
		"ID": "900719925474099312", "SECTION_NAME": "首页", "EXPORT_TIME": "2026-11-13 17:41:48",
		"PAYMENT_AMOUNT": "123456789012345678.90", "PAYMENT_AMOUNT_CHG": "-0.29980000", "PAYMENT_AMOUNT_YDAY": "200000000000000000.00",
		"VISITOR_COUNT": "999999999999999999", "PAY_CONVERSION_RATE": "0.06050000", "AVG_ORDER_VALUE": "123.1234567890123456789",
		"BATCH_NO": "202611131700", "DOWNLOAD_TIME": "2026-11-13 17:42:00", "WRITE_TIME": "2026-11-13 17:43:00",
		"CREATED_TIME": "2026-11-13 17:43:00", "MODIFIED_TIME": "2026-11-13 17:43:00",
	} {
		if got := result[0][name]; got == nil || *got != want {
			t.Errorf("%s=%v want=%q", name, got, want)
		}
	}
	if result[0]["DATE_RANGE"] != nil || result[0]["SHOP_CUSTOMER_COUNT_YDAY"] != nil {
		t.Fatal("source NULL values must be preserved")
	}
}

func TestTmallOverviewColumnsDescribeSourceData(t *testing.T) {
	columns := TmallColumns(TmallSycmOverviewHourly)
	if len(columns) != 60 {
		t.Fatalf("columns=%d want=60", len(columns))
	}
	if columns[0].Name != "ID" || columns[0].Nullable || columns[len(columns)-1].Name != "MODIFIED_TIME" {
		t.Fatalf("unexpected source column order: %+v", columns)
	}
	filters := []string{}
	for _, column := range columns {
		if column.Comment == "" {
			t.Errorf("missing comment for %s", column.Name)
		}
		if column.Filterable {
			filters = append(filters, column.Name)
		}
		if strings.HasSuffix(column.Name, "_CHG") || strings.Contains(column.Name, "RATE") {
			if column.DataType != "NUMBER" || !strings.Contains(column.Comment, "小数") {
				t.Errorf("rate must describe decimal source values: %+v", column)
			}
		}
	}
	if want := []string{"SECTION_NAME", "MENU_NAME", "DATA_POINT", "DIMENSION_NAME", "DATE_RANGE"}; !reflect.DeepEqual(filters, want) {
		t.Fatalf("filters=%v want=%v", filters, want)
	}
}
