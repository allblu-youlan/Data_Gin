package reportoracle

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func TestBojunProductInfoQueryUsesExactProjection(t *testing.T) {
	columns := []string{
		"ID", "NO_ISACTIVE", "SKU", "NICKNAME_DESCRIPTION", "PRO_JG_NAME", "PRO_9_NAME", "SKC_13",
		"COLOR_NAME", "COLOR_CODE", "SIZE_NAME", "SIZE__NAME", "PRICELIST", "PRO_ATTR", "PRO_SERIESNAME_DIM3",
		"PRO_SXZ_DIM4", "PRO_DA_DIM5", "PRO_Z_DIM6", "PRO_X_DIM7", "PRO_YEAR_DIM2", "PRO_SEASON_DC2", "PRO_WAVE_DC1",
		"PRO_MATERIAL_DC3", "MARKETDATE", "PRO_GEN_DIM9", "COLOR_GEN_DC4", "PRO_SIZE_RANGE", "COLOR_SIZE_RANGE",
		"SERIES_DESCRIPTION", "PERCOST", "RATE_COST", "SELLING_POINT_DESCRIPTION", "SPU_8", "SKC_12", "NICKNAME",
		"PRO_M_DIM11_ID", "PRO_M_DIM12_ID", "PRO_M_DIM13_ID", "SUPPLIER_NAME", "OFFLINE_WAVE",
		"PRO_M_DIM14_ID", "PRO_M_DIM15_ID", "PRO_M_DIM16_ID",
	}
	projection := make([]string, 0, 44)
	for _, column := range columns {
		projection = append(projection, "a."+column)
	}
	projection = append(projection,
		"TO_CHAR(a.CREATED, 'YYYY-MM-DD HH24:MI:SS')",
		"TO_CHAR(a.MODIFIED, 'YYYY-MM-DD HH24:MI:SS')")
	statement, arguments, err := buildBojunProductInfoQuery(BojunProductInfoQuery{PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(strings.Split(statement, "\nFROM ")[0]), " ")
	if len(projection) != 44 || got != "SELECT "+strings.Join(projection, ", ") {
		t.Fatalf("unexpected product info projection: %s", got)
	}
	if !strings.Contains(statement, "\nFROM YL_DBS.DIM_PRODUCT_INFO_INTERFACE a\nORDER BY a.ID\nOFFSET :1 ROWS FETCH NEXT :2 ROWS ONLY") ||
		strings.Contains(statement, "WHERE") || strings.Contains(statement, "SELECT *") || strings.Contains(statement, ";") ||
		!reflect.DeepEqual(arguments, []any{0, 51}) {
		t.Fatalf("unexpected full query: %s %#v", statement, arguments)
	}
}

func TestQueryBojunProductInfosMapsAllColumnsAndPreservesNulls(t *testing.T) {
	fieldNames := []string{
		"ID", "NoIsActive", "ProductCode", "NicknameDescription", "StructureName", "ProductName", "ProductColor",
		"ColorName", "ColorCode", "SizeName", "SizeCode", "PriceList", "ProductAttribute", "ProductLine", "TopBottom",
		"CategoryLarge", "CategoryMiddle", "CategorySmall", "Year", "Season", "Wave", "MaterialName", "MarketDate",
		"ProductGender", "ColorGender", "ProductSizeRange", "ColorSizeRange", "SeriesDescription", "CostPrice", "CostRate",
		"SellingPointDescription", "Spu8", "Skc12", "Nickname", "InnerOuter", "AnalysisCategoryLarge", "AnalysisCategoryMiddle",
		"SupplierName", "OfflineWave", "AgeRange", "ProductLevel", "ProductStyle", "Created", "Modified",
	}
	values := make([]driver.Value, 44)
	for index := range values {
		values[index] = fmt.Sprintf(" value-%02d ", index)
	}
	values[0], values[1], values[2], values[10] = "000123", " Y ", "000SKU", "090"
	values[11] = "123456789012345678.1234567890"
	values[28], values[29] = "-12.5000000001", "0.0000000001"
	values[22], values[42], values[43] = "20261026", "2026-10-26 01:02:03", "2026-10-26 04:05:06"
	rows := &bojunQueryTestRows{columns: make([]string, 44), values: [][]driver.Value{values, make([]driver.Value, 44)}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() || len(arguments) != 3 || arguments[0].Value != 25 || arguments[1].Value != 2 ||
			!strings.Contains(statement, "ORDER BY a.ID\nOFFSET :1 ROWS FETCH NEXT :2 ROWS ONLY") {
			t.Fatalf("unexpected full query or bindings: %s %#v", statement, arguments)
		}
		if option, ok := arguments[2].Value.(godror.Option); !ok || option == nil {
			t.Fatal("missing godror number option")
		}
		return rows, nil
	})
	items, err := adapter.QueryBojunProductInfos(t.Context(), BojunProductInfoQuery{PageSize: 1, Offset: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !rows.closed || items[1] != (BojunProductInfoRow{}) {
		t.Fatalf("extra row, nulls or row close lost: items=%+v closed=%v", items, rows.closed)
	}
	item := reflect.ValueOf(items[0])
	if item.NumField() != 44 || len(fieldNames) != 44 {
		t.Fatalf("unexpected field count: %d", item.NumField())
	}
	for index, fieldName := range fieldNames {
		field := item.FieldByName(fieldName)
		if !field.IsValid() || field.Type() != reflect.TypeOf((*string)(nil)) || field.IsNil() ||
			field.Elem().String() != strings.TrimSpace(values[index].(string)) {
			t.Fatalf("column %d did not map to %s: %v", index, fieldName, items[0])
		}
	}
}

func TestQueryBojunProductInfosBindsProductFiltersInSQLOrder(t *testing.T) {
	const productValue = "001SKU' OR 1=1 --"
	values := []string{" " + productValue + " ", "0002aB", productValue, "0002Ab"}
	for _, test := range []struct {
		name, column string
		query        BojunProductInfoQuery
	}{
		{"barcode", "a.SKU", BojunProductInfoQuery{ProductCode: values}},
		{"color", "a.SKC_13", BojunProductInfoQuery{ProductColor: values}},
		{"name", "a.PRO_9_NAME", BojunProductInfoQuery{ProductName: values}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
				calls++
				predicate := "\nWHERE " + test.column + " IN (:1, :2, :3)\nORDER BY a.ID\nOFFSET :4 ROWS FETCH NEXT :5 ROWS ONLY"
				if !strings.HasSuffix(statement, predicate) || strings.Contains(statement, productValue) || strings.Contains(statement, "0002aB") {
					t.Fatalf("unexpected product predicate or interpolated values: %s", statement)
				}
				wantArguments := []any{productValue, "0002aB", "0002Ab", 100, 51}
				placeholders := regexp.MustCompile(`:[0-9]+`).FindAllString(statement, -1)
				if len(arguments) != len(wantArguments)+1 || len(placeholders) != len(wantArguments) {
					t.Fatalf("unexpected arguments: %#v", arguments)
				}
				for index, want := range wantArguments {
					if arguments[index].Value != want || arguments[index].Name != "" || arguments[index].Ordinal != index+1 ||
						placeholders[index] != fmt.Sprintf(":%d", index+1) {
						t.Fatalf("unexpected bind %d: %#v want=%#v", index, arguments[index], want)
					}
				}
				if option, ok := arguments[len(wantArguments)].Value.(godror.Option); !ok || option == nil {
					t.Fatal("missing godror number option")
				}
				return &bojunQueryTestRows{columns: make([]string, 44)}, nil
			})
			test.query.PageSize, test.query.Offset = 50, 100
			items, err := adapter.QueryBojunProductInfos(t.Context(), test.query)
			if err != nil || items == nil || len(items) != 0 || calls != 1 || values[0] != " "+productValue+" " {
				t.Fatalf("unexpected query result or mutated input: items=%+v error=%v calls=%d values=%#v", items, err, calls, values)
			}
		})
	}
}

func TestQueryBojunProductInfosLimitsListsBeforeDeduplication(t *testing.T) {
	for _, filter := range []struct {
		name  string
		limit int
		query func([]string) BojunProductInfoQuery
	}{
		{"barcode", 100, func(values []string) BojunProductInfoQuery {
			return BojunProductInfoQuery{ProductCode: values, PageSize: 50}
		}},
		{"color", 20, func(values []string) BojunProductInfoQuery {
			return BojunProductInfoQuery{ProductColor: values, PageSize: 50}
		}},
		{"name", 20, func(values []string) BojunProductInfoQuery {
			return BojunProductInfoQuery{ProductName: values, PageSize: 50}
		}},
	} {
		for _, batch := range []struct {
			name       string
			count      int
			duplicates bool
		}{
			{"maximum unique", filter.limit, false},
			{"maximum duplicates", filter.limit, true},
			{"too many unique", filter.limit + 1, false},
			{"too many duplicates", filter.limit + 1, true},
		} {
			t.Run(filter.name+"/"+batch.name, func(t *testing.T) {
				values := make([]string, batch.count)
				for index := range values {
					values[index] = fmt.Sprintf("%03dSKU", index)
					if batch.duplicates {
						values[index] = " SKU "
					}
				}
				calls := 0
				adapter := newBojunQueryTestAdapter(t, func(_ context.Context, _ string, arguments []driver.NamedValue) (driver.Rows, error) {
					calls++
					unique := batch.count
					if batch.duplicates {
						unique = 1
					}
					if len(arguments) != unique+3 {
						t.Fatalf("unexpected argument count: %d", len(arguments))
					}
					return &bojunQueryTestRows{columns: make([]string, 44)}, nil
				})
				_, err := adapter.QueryBojunProductInfos(t.Context(), filter.query(values))
				if batch.count > filter.limit {
					if err == nil || calls != 0 {
						t.Fatalf("oversized batch reached database: error=%v calls=%d", err, calls)
					}
				} else if err != nil || calls != 1 {
					t.Fatalf("valid batch failed: error=%v calls=%d", err, calls)
				}
			})
		}
	}
}

func TestQueryBojunProductInfosRejectsInvalidQueries(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("invalid query reached database")
		return nil, nil
	})
	for _, test := range []struct {
		name  string
		query BojunProductInfoQuery
	}{
		{"zero page size", BojunProductInfoQuery{}},
		{"large page size", BojunProductInfoQuery{PageSize: 101}},
		{"negative offset", BojunProductInfoQuery{PageSize: 1, Offset: -1}},
		{"barcode and color", BojunProductInfoQuery{ProductCode: []string{"SKU"}, ProductColor: []string{"COLOR"}, PageSize: 1}},
		{"barcode and name", BojunProductInfoQuery{ProductCode: []string{"SKU"}, ProductName: []string{"STYLE"}, PageSize: 1}},
		{"color and name", BojunProductInfoQuery{ProductColor: []string{"COLOR"}, ProductName: []string{"STYLE"}, PageSize: 1}},
		{"blank barcode", BojunProductInfoQuery{ProductCode: []string{" "}, PageSize: 1}},
		{"empty color item", BojunProductInfoQuery{ProductColor: []string{"COLOR", ""}, PageSize: 1}},
		{"long name", BojunProductInfoQuery{ProductName: []string{strings.Repeat("款", 27)}, PageSize: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := adapter.QueryBojunProductInfos(t.Context(), test.query); err == nil {
				t.Fatalf("invalid query succeeded: %+v", test.query)
			}
		})
	}
}
