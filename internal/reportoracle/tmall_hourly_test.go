package reportoracle

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func TestNormalizeTmallHourlyQueryRequiresDataHour(t *testing.T) {
	for _, dataset := range []TmallDataset{TmallQztgHourly, TmallSycmHourly, TmallSycmOverviewHourly} {
		t.Run(string(dataset), func(t *testing.T) {
			for _, batch := range []string{"202611041300", "202611040000", "202402292300", " 202611041300 "} {
				t.Run(batch, func(t *testing.T) {
					query, err := NormalizeTmallQuery(dataset, TmallQuery{BatchNo: batch, PageSize: 50})
					if err != nil || query.BatchNo != strings.TrimSpace(batch) {
						t.Fatalf("batch=%q query=%+v error=%v", batch, query, err)
					}
				})
			}
			for _, batch := range []string{"", "2026110413", "202611041330", "202602291300", "202611042400", "202613041300", "000011041300", "20261104aa00", "202611041300' OR 1=1"} {
				t.Run("invalid_"+batch, func(t *testing.T) {
					if _, err := NormalizeTmallQuery(dataset, TmallQuery{BatchNo: batch, PageSize: 50}); err == nil || !strings.Contains(err.Error(), "batchNo") {
						t.Fatalf("batch=%q error=%v", batch, err)
					}
				})
			}
			for _, query := range []TmallQuery{
				{BatchNo: "202611041300", StartDate: "2026-11-04", PageSize: 50},
				{BatchNo: "202611041300", EndDate: "2026-11-04", PageSize: 50},
			} {
				if _, err := NormalizeTmallQuery(dataset, query); err == nil {
					t.Fatalf("accepted date range with hourly batch: %+v", query)
				}
			}
		})
	}
}

func TestNormalizeTmallDailyQueryRejectsBatch(t *testing.T) {
	for _, dataset := range []TmallDataset{TmallSycm, TmallQztg, TmallTot} {
		t.Run(string(dataset), func(t *testing.T) {
			_, err := NormalizeTmallQuery(dataset, TmallQuery{BatchNo: "202611041300", StartDate: "2026-11-04", EndDate: "2026-11-04", PageSize: 50})
			if err == nil || !strings.Contains(err.Error(), "batchNo") {
				t.Fatalf("daily query silently ignores batch: %v", err)
			}
		})
	}
}

func TestTmallHourlyQueryValidatesDimensionsAndPagination(t *testing.T) {
	for _, test := range []struct {
		dataset TmallDataset
		filters map[string]string
	}{
		{TmallQztgHourly, map[string]string{"PRODUCT_ID": "001", "PLAN_ID": "002", "PLAN_GROUP_ID": "003", "STATUS": "正在推广"}},
		{TmallSycmHourly, map[string]string{"PRODUCT_ID": "001", "ITEM_NO": "A01", "CATEGORY_NAME": "外套", "PRODUCT_STATUS": "在售"}},
		{TmallSycmOverviewHourly, map[string]string{"SECTION_NAME": "首页", "MENU_NAME": "数据概览", "DATA_POINT": "实时", "DIMENSION_NAME": "本店", "DATE_RANGE": "2026-11-13"}},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			if _, err := NormalizeTmallQuery(test.dataset, TmallQuery{BatchNo: "202611041300", Filters: test.filters, PageSize: 100, Offset: 99900}); err != nil {
				t.Fatalf("allowed dimensions and last page rejected: %v", err)
			}
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("invalid hourly query reached Oracle")
				return nil, nil
			})
			for _, filters := range []map[string]string{
				{"PRODUCT_ID OR 1=1": "1"}, {"BATCH_NO": "202611041300"}, {"COST": "1"},
				{"PRODUCT_ID": " "}, {"PRODUCT_ID": strings.Repeat("1", 33)},
				{"PRODUCT_CODE": "A01"}, {"ITEM_STATUS": "在售"},
			} {
				_, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{BatchNo: "202611041300", Filters: filters, PageSize: 50})
				if err == nil || !strings.Contains(err.Error(), "dimension") {
					t.Fatalf("filters=%v error=%v", filters, err)
				}
			}
			for _, page := range []struct{ offset, size int }{{0, 0}, {0, 101}, {-1, 50}, {50000, 50}} {
				_, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{BatchNo: "202611041300", Offset: page.offset, PageSize: page.size})
				if err == nil || !strings.Contains(err.Error(), "pagination") {
					t.Fatalf("page=%+v error=%v", page, err)
				}
			}
		})
	}
}

func TestQueryTmallHourlyBindsBatchAndPreservesSourceValues(t *testing.T) {
	const product = "001' OR 1=1 --"
	for _, test := range []struct {
		dataset    TmallDataset
		table      string
		columns    int
		values     map[int]driver.Value
		want       map[string]string
		nullColumn string
		removed    []string
		filter     string
	}{
		{
			dataset: TmallQztgHourly, table: "YL_DBS.ODS_TM_QZTG_PRODUCT_HOUR", columns: 102,
			values: map[int]driver.Value{0: "900719925474099312", 9: "000123", 33: "1234567890.12345678", 36: "0.06100000", 87: "202611041300", 88: "2026-11-04 13:12:00", 89: "2026-11-04 13:20:00",
				40: "123", 46: "12", 49: "3", 50: "123.45", 52: "5", 53: "456.78", 54: "2", 55: "3", 56: "111.11", 57: "345.67", 62: "101.01", 63: "1", 64: "4.5678", 67: "23.45", 71: "14",
				92: "9", 93: "1.2345", 94: "12345678901234.5678", 95: "0.1234", 96: "8", 97: "2.3456", 98: "3.4567", 99: "10", 100: "12.34", 101: "56.78",
			},
			want: map[string]string{"ID": "900719925474099312", "PRODUCT_ID": "000123", "COST": "1234567890.12345678", "CLICK_RATE": "0.06100000", "BATCH_NO": "202611041300", "DOWNLOAD_TIME": "2026-11-04 13:12:00", "WRITE_TIME": "2026-11-04 13:20:00",
				"PLATFORM_BOOST_CLICK_CNT": "123", "FAV_SHOP_CNT": "12", "TOTAL_PRESALE_PAY_CNT": "3", "TOTAL_PRESALE_PAY_AMT": "123.45", "TOTAL_PAY_CNT": "5", "TOTAL_PAY_AMT": "456.78", "DIRECT_PAY_CNT": "2", "INDIRECT_PAY_CNT": "3", "DIRECT_PAY_AMT": "111.11", "INDIRECT_PAY_AMT": "345.67", "DIRECT_PRESALE_PAY_AMT": "101.01", "DIRECT_PRESALE_PAY_CNT": "1", "PRESALE_ROI": "4.5678", "TOTAL_PAY_COST": "23.45", "FAV_ITEM_CNT": "14",
				"DIRECT_FAV_ITEM_CNT": "9", "DIRECT_NET_ACTUAL_ROI": "1.2345", "DIRECT_NET_DEAL_AMT": "12345678901234.5678", "DIRECT_NET_DEAL_AMT_RATIO": "0.1234", "DIRECT_NET_DEAL_CNT": "8", "DIRECT_NET_PRESEAL_ROI": "2.3456", "DIRECT_PRESEAL_ROI": "3.4567", "DIRECT_ITEM_FAV_CART_CNT": "10", "PLATFORM_SUBSIDY_AMT": "12.34", "SUBSIDY_GUIDE_PAY_AMT": "56.78",
			},
			nullColumn: "PRODUCT_NAME", filter: "PLAN_ID",
			removed: []string{"SUBJECT_NAME", "TOTAL_PRESEAL_AMT", "TOTAL_PRESEAL_CNT", "DIRECT_PRESEAL_AMT", "DIRECT_PRESEAL_CNT", "DIRECT_DEAL_AMT", "INDIRECT_DEAL_AMT", "TOTAL_DEAL_AMT", "TOTAL_DEAL_CNT", "DIRECT_DEAL_CNT", "INDIRECT_DEAL_CNT", "PRESEAL_ROI", "TOTAL_DEAL_COST", "TOTAL_FAV_CNT", "SHOP_FAV_CNT", "BOOST_CLICK"},
		},
		{
			dataset: TmallSycmHourly, table: "YL_DBS.ODS_TM_SYCM_PRODUCTORDER_HOUR", columns: 47,
			values:     map[int]driver.Value{0: "900719925474099312", 12: "000123", 15: "2026-11-04 13:08:00", 16: "1234567890123456.78", 17: "-20.00000000", 22: "4.32000000", 42: "202611041300", 43: "2026-11-04 13:12:00", 44: "2026-11-04 13:20:00", 13: "C44H3049A", 14: "在售", 18: "10", 24: "20", 26: "15", 28: "8", 34: "100"},
			want:       map[string]string{"ID": "900719925474099312", "PRODUCT_ID": "000123", "EXPORT_TIME": "2026-11-04 13:08:00", "PAY_AMT": "1234567890123456.78", "PAY_AMT_RATIO": "-20.00000000", "PAY_CONV_RATE": "4.32000000", "BATCH_NO": "202611041300", "DOWNLOAD_TIME": "2026-11-04 13:12:00", "WRITE_TIME": "2026-11-04 13:20:00", "ITEM_NO": "C44H3049A", "PRODUCT_STATUS": "在售", "PAY_ITEM_CNT": "10", "CART_ITEM_CNT": "20", "CART_USER_CNT": "15", "FAVORITE_CNT": "8", "VISITOR_CNT": "100"},
			nullColumn: "PRODUCT_NAME", filter: "ITEM_NO",
			removed: []string{"PRODUCT_CODE", "ITEM_STATUS", "PAY_CNT", "ADD_CART_CNT", "ADD_CART_BUYER_CNT", "FAV_CNT", "UV_CNT"},
		},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			values := make([]driver.Value, test.columns)
			for index, value := range test.values {
				values[index] = value
			}
			rows := &bojunQueryTestRows{columns: make([]string, test.columns), values: [][]driver.Value{values, values}}
			adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				for _, fragment := range []string{"FROM " + test.table + " a", "WHERE a.BATCH_NO = :1", "ORDER BY a.ID\nOFFSET :4 ROWS FETCH NEXT :5 ROWS ONLY", "TO_CHAR(a.DOWNLOAD_TIME, 'YYYY-MM-DD HH24:MI:SS')", "TO_CHAR(a.WRITE_TIME, 'YYYY-MM-DD HH24:MI:SS')"} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				if ctx != t.Context() || strings.Contains(statement, "STAT_DATE") || strings.Contains(statement, product) || strings.Contains(statement, "SELECT *") || strings.Contains(statement, "COUNT(") {
					t.Fatalf("unexpected hourly query: %s", statement)
				}
				want := []any{"202611041300", "dimension", product, 1, 2}
				if test.dataset == TmallSycmHourly {
					if !strings.Contains(statement, "TO_CHAR(a.EXPORT_TIME, 'YYYY-MM-DD HH24:MI:SS')") {
						t.Fatal("export time is not formatted")
					}
				}
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
			result, err := adapter.QueryTmall(t.Context(), test.dataset, TmallQuery{BatchNo: "202611041300", Filters: map[string]string{"PRODUCT_ID": product, test.filter: "dimension"}, Offset: 1, PageSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 2 || len(result[0]) != test.columns || !rows.closed {
				t.Fatalf("rows=%d closed=%v", len(result), rows.closed)
			}
			for name, want := range test.want {
				if got := result[0][name]; got == nil || *got != want {
					t.Errorf("%s=%v want=%q", name, got, want)
				}
			}
			for _, name := range test.removed {
				if _, exists := result[0][name]; exists {
					t.Errorf("returned removed column %s", name)
				}
			}
			if result[0][test.nullColumn] != nil {
				t.Errorf("NULL %s is not preserved", test.nullColumn)
			}
		})
	}
}

func TestTmallHourlyColumnsDescribeSourceData(t *testing.T) {
	for _, test := range []struct {
		dataset TmallDataset
		count   int
		rate    string
		comment string
		last    string
	}{
		{"qztg_hourly", 102, "CLICK_RATE", "点击率(小数,0.061表示6.1%)", "SUBSIDY_GUIDE_PAY_AMT"},
		{"sycm_hourly", 47, "PAY_CONV_RATE", "支付转化率(百分数原值,去掉%,如4.32表示4.32%)", "MODIFIED_TIME"},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			columns := TmallColumns(test.dataset)
			if len(columns) != test.count {
				t.Fatalf("columns=%d want=%d", len(columns), test.count)
			}
			if columns[0].Name != "ID" || columns[0].Nullable || columns[len(columns)-1].Name != test.last {
				t.Fatalf("unexpected source column order: %+v", columns)
			}
			byName := make(map[string]TmallColumn, len(columns))
			for _, column := range columns {
				if column.Comment == "" {
					t.Errorf("missing source comment for %s", column.Name)
				}
				byName[column.Name] = column
			}
			if len(byName) != test.count || byName[test.rate].Comment != test.comment || byName[test.rate].DataType != "NUMBER" {
				t.Fatalf("source rate metadata lost: %+v", byName[test.rate])
			}
			batch := byName["BATCH_NO"]
			if batch.Filterable || batch.Length != 32 || !strings.Contains(batch.Comment, "数据所属整点") || !strings.Contains(batch.Comment, "yyyyMMddHHmm") || !strings.Contains(batch.Comment, "00") {
				t.Fatalf("incorrect batch definition: %+v", batch)
			}
			if !byName["PRODUCT_ID"].Filterable || byName["DOWNLOAD_TIME"].DataType != "DATE" || !byName["DOWNLOAD_TIME"].Nullable {
				t.Fatalf("incorrect dimension/date metadata: %+v", byName)
			}
			columns[0].Comment = "changed"
			if TmallColumns(test.dataset)[0].Comment == "changed" {
				t.Fatal("column metadata shares mutable storage")
			}
		})
	}
}
