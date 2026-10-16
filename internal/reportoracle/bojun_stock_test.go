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

func TestQueryBojunStocksBindsBySQLPlaceholderOrder(t *testing.T) {
	placeholderPattern := regexp.MustCompile(`:[0-9]+`)
	for _, count := range []int{1, 2, 100} {
		values := make([]string, count)
		wantArguments := []any{"001"}
		for index := range values {
			values[index] = fmt.Sprintf("SKU%03d", index)
			wantArguments = append(wantArguments, values[index])
		}
		wantArguments = append(wantArguments, 25, 26)
		for _, filter := range []struct {
			name  string
			query BojunStockQuery
		}{
			{"barcode", BojunStockQuery{ProductCode: values}},
			{"color", BojunStockQuery{ProductColor: values}},
			{"name", BojunStockQuery{ProductName: values}},
		} {
			t.Run(fmt.Sprintf("%s/%d", filter.name, count), func(t *testing.T) {
				adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
					placeholders := placeholderPattern.FindAllString(statement, -1)
					if len(placeholders) != count+3 || len(arguments) != len(placeholders)+1 {
						t.Fatalf("placeholder/argument count mismatch: %v %#v", placeholders, arguments)
					}
					for index, placeholder := range placeholders {
						if placeholder != fmt.Sprintf(":%d", index+1) || arguments[index].Ordinal != index+1 ||
							arguments[index].Name != "" || arguments[index].Value != wantArguments[index] {
							t.Fatalf("SQL bind position %d: placeholder=%s argument=%#v want=%#v", index+1, placeholder, arguments[index], wantArguments[index])
						}
					}
					if option, ok := arguments[len(placeholders)].Value.(godror.Option); !ok || option == nil {
						t.Fatal("missing trailing number option")
					}
					return &bojunQueryTestRows{columns: make([]string, 28)}, nil
				})
				filter.query.StoreCode, filter.query.PageSize, filter.query.Offset = "001", 25, 25
				if _, err := adapter.QueryBojunStocks(t.Context(), filter.query); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestBojunStockQueryHasExactProjectionAndBindings(t *testing.T) {
	want := []string{
		"a.CODE", "a.STORE_NAME", "a.M_PRODUCT_ALIAS_NO", "a.PROD_COLOR", "a.M_PRODUCT_NAME", "a.M_PRODUCT_VALUE",
		"a.M_ATTR_VALUE1", "a.M_ATTR_VALUE2_CODE", "a.M_ATTR_VALUE2", "a.PRICELIST", "a.QTY", "a.AMT_PRICEQTY",
		"a.QTY_BAS", "a.QTYPREOUT", "a.QTY_BAS_PREOUT", "a.AMT_PRICEQTYPREOUT", "a.QTYPREIN", "a.AMT_PRICEQTYPREIN",
		"a.QTY_FREEZE", "a.QTY_OMS", "a.QTYVALID", "a.AMT_PRICEQTYVALID", "a.QTYCONSIGN", "a.JST_PREOUT", "a.JST_DIFF_QTY",
		"a.QTYCAN", "a.M_ATTRIBUTESETINSTANCE_ID", "a.ISACTIVE",
	}
	for _, test := range []struct {
		name      string
		query     BojunStockQuery
		predicate string
	}{
		{"barcode", BojunStockQuery{ProductCode: []string{"SKU1", "SKU2"}}, "a.M_PRODUCT_ALIAS_NO IN (:2, :3)"},
		{"color", BojunStockQuery{ProductColor: []string{"COLOR1", "COLOR2"}}, "a.PROD_COLOR IN (:2, :3)"},
		{"name", BojunStockQuery{ProductName: []string{"STYLE1", "STYLE2"}}, "a.M_PRODUCT_NAME IN (:2, :3)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.StoreCode, test.query.PageSize = "001", 50
			statement, _, err := buildBojunStockQuery(test.query)
			if err != nil {
				t.Fatal(err)
			}
			projection := strings.TrimSpace(strings.TrimPrefix(strings.Split(statement, "FROM")[0], "\nSELECT "))
			got := strings.Split(projection, ",")
			for index := range got {
				got[index] = strings.TrimSpace(got[index])
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("projection = %#v, want %#v", got, want)
			}
			for _, fragment := range []string{
				"FROM " + BojunStockView + " a", "WHERE a.CODE = :1\n  AND " + test.predicate,
				"ORDER BY a.C_STORE_ID,\n         a.M_PRODUCTALIAS_ID,\n         a.M_ATTRIBUTESETINSTANCE_ID",
				"OFFSET :4 ROWS FETCH NEXT :5 ROWS ONLY",
			} {
				if !strings.Contains(statement, fragment) {
					t.Fatalf("missing %q", fragment)
				}
			}
			for _, unwanted := range []string{"SELECT *", "a.*", "a.QTY >", "AND a.ISACTIVE", "TO_CHAR", " LIKE ", ";"} {
				if strings.Contains(statement, unwanted) {
					t.Fatalf("unexpected SQL fragment %q", unwanted)
				}
			}
		})
	}
}

func TestQueryBojunStocksTrimsDeduplicatesAndBindsOneProductList(t *testing.T) {
	const productValue = "001aB' OR 1=1 --"
	values := []string{" " + productValue + " ", "0002aB", productValue, "0002Ab"}
	for _, test := range []struct {
		name      string
		query     BojunStockQuery
		predicate string
	}{
		{"barcode", BojunStockQuery{ProductCode: values, ProductColor: []string{}}, "a.M_PRODUCT_ALIAS_NO IN (:2, :3, :4)"},
		{"color", BojunStockQuery{ProductCode: []string{}, ProductColor: values}, "a.PROD_COLOR IN (:2, :3, :4)"},
		{"name", BojunStockQuery{ProductColor: []string{}, ProductName: values}, "a.M_PRODUCT_NAME IN (:2, :3, :4)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
				calls++
				where := strings.Split(strings.Split(statement, "WHERE ")[1], "\nORDER BY")[0]
				if where != "a.CODE = :1\n  AND "+test.predicate || strings.Contains(statement, productValue) || strings.Contains(statement, "0002aB") {
					t.Fatalf("unexpected product predicate or interpolated value: %s", statement)
				}
				if ctx != t.Context() || len(arguments) != 7 || arguments[0].Value != "0001" ||
					arguments[1].Value != productValue || arguments[2].Value != "0002aB" || arguments[3].Value != "0002Ab" ||
					arguments[4].Value != 50 || arguments[5].Value != 51 {
					t.Fatalf("unexpected context or bindings: %#v", arguments)
				}
				if option, ok := arguments[6].Value.(godror.Option); !ok || option == nil {
					t.Fatalf("missing godror number option: %#v", arguments[6])
				}
				return &bojunQueryTestRows{columns: make([]string, 28)}, nil
			})
			test.query.StoreCode, test.query.PageSize, test.query.Offset = " 0001 ", 50, 50
			if _, err := adapter.QueryBojunStocks(t.Context(), test.query); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || values[0] != " "+productValue+" " || len(values) != 4 {
				t.Fatalf("unexpected query count or mutated input: calls=%d values=%#v", calls, values)
			}
		})
	}
}

func TestQueryBojunStocksPaginatesAllProductsInOneQuery(t *testing.T) {
	for _, test := range []struct {
		name  string
		query BojunStockQuery
	}{
		{"barcode", BojunStockQuery{ProductCode: []string{"000SKU-S", "000SKU-M"}}},
		{"color", BojunStockQuery{ProductColor: []string{"001aB-Red", "001aB-Blue"}}},
		{"name", BojunStockQuery{ProductName: []string{"001aB", "001aC"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var values [][]driver.Value
			for index, productCode := range []string{"000SKU-S", "000SKU-M", "000SKU-M"} {
				row := make([]driver.Value, 28)
				row[0], row[2], row[3], row[4], row[26] = "0001", productCode, "001aB-Red", "001aB", int64(index+1)
				values = append(values, row)
			}
			rows := &bojunQueryTestRows{columns: make([]string, 28), values: values}
			calls := 0
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
				calls++
				if strings.Count(statement, "SELECT ") != 1 || strings.Count(statement, "ORDER BY ") != 1 ||
					strings.Count(statement, "OFFSET ") != 1 || !strings.Contains(statement, "IN (:2, :3)") ||
					arguments[0].Value != "0001" || arguments[3].Value != 4 || arguments[4].Value != 3 {
					t.Fatalf("missing batch-wide ordering or pagination: %s %#v", statement, arguments)
				}
				return rows, nil
			})
			test.query.StoreCode, test.query.PageSize, test.query.Offset = "0001", 2, 4
			items, err := adapter.QueryBojunStocks(t.Context(), test.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 3 || !rows.closed || calls != 1 {
				t.Fatalf("SKU rows lost, not closed or query repeated: items=%#v closed=%v calls=%d", items, rows.closed, calls)
			}
			for index, item := range items {
				if item.ProductCode != values[index][2] || item.AttributeSetInstanceID == nil || *item.AttributeSetInstanceID != int64(index+1) {
					t.Fatalf("SKU identity changed: %#v", item)
				}
			}
		})
	}
}

func TestQueryBojunStocksPreservesDecimalsNullsAndBindings(t *testing.T) {
	const productCode = "001' OR 1=1 --"
	values := []driver.Value{
		"001", " 店仓 ", "000SKU", " 款色 ", " 款号 ", " 品名 ", " 颜色 ", " 001 ", " 尺寸 ",
		"123456789012345678.1234567890", "-12.5000000001", "-99.25", "0", "1.1", "2.2", "3.3", "4.4", "5.5",
		"6.1234567890", nil, "7.7", "8.8", "9.9", "123456789012345678", "-10", "11.0000000001", "12345", "Y ",
	}
	rows := &bojunQueryTestRows{columns: make([]string, 28), values: [][]driver.Value{values, make([]driver.Value, 28)}}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() || !strings.Contains(statement, "a.M_PRODUCT_ALIAS_NO IN (:2, :3)") ||
			strings.Contains(statement, productCode) || len(arguments) != 6 || arguments[0].Value != "001" ||
			arguments[1].Value != productCode || arguments[2].Value != "000Other" || arguments[3].Value != 100 || arguments[4].Value != 2 {
			t.Fatalf("unexpected context, SQL or bindings: %s %#v", statement, arguments)
		}
		if option, ok := arguments[5].Value.(godror.Option); !ok || option == nil {
			t.Fatalf("missing godror number option: %#v", arguments[5])
		}
		return rows, nil
	})
	items, err := adapter.QueryBojunStocks(t.Context(), BojunStockQuery{ProductCode: []string{" " + productCode + " ", "000Other"}, StoreCode: " 001 ", PageSize: 1, Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !rows.closed {
		t.Fatalf("extra row lost or rows not closed: len=%d closed=%v", len(items), rows.closed)
	}
	item := items[0]
	if item.StoreCode != "001" || item.ProductCode != "000SKU" || item.AttributeSetInstanceID == nil || *item.AttributeSetInstanceID != 12345 {
		t.Fatalf("identifiers changed: %#v", item)
	}
	fields := []*string{item.StoreName, item.ProductColor, item.ProductName, item.Value, item.Value1, item.SizeCode, item.Value2,
		item.PriceList, item.Qty, item.AmtPriceQty, item.QtyBas, item.QtyPreOut, item.QtyBasPreOut, item.AmtPriceQtyPreOut,
		item.QtyPreIn, item.AmtPriceQtyPreIn, item.QtyFreeze, item.QtyOms, item.QtyValid, item.AmtPriceQtyValid,
		item.QtyConsign, item.JstPreOut, item.JstDiffQty, item.QtyCan, item.IsActive}
	columnIndexes := []int{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 27}
	for index, field := range fields {
		value := values[columnIndexes[index]]
		if value == nil {
			if field != nil {
				t.Fatalf("column %d: NULL became %q", columnIndexes[index], *field)
			}
		} else if field == nil || *field != strings.TrimSpace(value.(string)) {
			t.Fatalf("column %d: decimal or text changed: %v, want %v", columnIndexes[index], field, value)
		}
	}
	if items[1] != (BojunStockRow{}) {
		t.Fatalf("NULL row changed: %#v", items[1])
	}
}

func TestQueryBojunStocksRejectsInvalidQueries(t *testing.T) {
	for _, test := range []struct {
		name  string
		query BojunStockQuery
	}{
		{"no product filter", BojunStockQuery{StoreCode: "001", PageSize: 1}},
		{"empty product lists", BojunStockQuery{ProductCode: []string{}, ProductColor: []string{}, ProductName: []string{}, StoreCode: "001", PageSize: 1}},
		{"blank barcode", BojunStockQuery{ProductCode: []string{" "}, StoreCode: "001", PageSize: 1}},
		{"blank color", BojunStockQuery{ProductColor: []string{"\t"}, StoreCode: "001", PageSize: 1}},
		{"blank name", BojunStockQuery{ProductName: []string{"\n"}, StoreCode: "001", PageSize: 1}},
		{"empty list element", BojunStockQuery{ProductCode: []string{"SKU", ""}, StoreCode: "001", PageSize: 1}},
		{"blank list element", BojunStockQuery{ProductName: []string{"STYLE", " "}, StoreCode: "001", PageSize: 1}},
		{"barcode and color", BojunStockQuery{ProductCode: []string{"SKU"}, ProductColor: []string{"color"}, StoreCode: "001", PageSize: 1}},
		{"barcode and name", BojunStockQuery{ProductCode: []string{"SKU"}, ProductName: []string{"name"}, StoreCode: "001", PageSize: 1}},
		{"color and name", BojunStockQuery{ProductColor: []string{"color"}, ProductName: []string{"name"}, StoreCode: "001", PageSize: 1}},
		{"all product filters", BojunStockQuery{ProductCode: []string{"SKU"}, ProductColor: []string{"color"}, ProductName: []string{"name"}, StoreCode: "001", PageSize: 1}},
		{"blank list counts as set", BojunStockQuery{ProductCode: []string{"SKU"}, ProductColor: []string{" "}, StoreCode: "001", PageSize: 1}},
		{"missing store", BojunStockQuery{ProductCode: []string{"SKU"}, PageSize: 1}},
		{"blank store", BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: " ", PageSize: 1}},
		{"barcode over 80 bytes", BojunStockQuery{ProductCode: []string{strings.Repeat("a", 81)}, StoreCode: "001", PageSize: 1}},
		{"color over 80 bytes", BojunStockQuery{ProductColor: []string{strings.Repeat("色", 27)}, StoreCode: "001", PageSize: 1}},
		{"name over 80 bytes", BojunStockQuery{ProductName: []string{strings.Repeat("款", 27)}, StoreCode: "001", PageSize: 1}},
		{"long store", BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: strings.Repeat("a", 81), PageSize: 1}},
		{"zero page size", BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: "001", PageSize: 0}},
		{"large page size", BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: "001", PageSize: 101}},
		{"negative offset", BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: "001", PageSize: 1, Offset: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("invalid query reached database")
				return nil, nil
			})
			if _, err := adapter.QueryBojunStocks(t.Context(), test.query); err == nil {
				t.Fatalf("invalid query succeeded: %#v", test.query)
			}
		})
	}
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("invalid query reached database")
		return nil, nil
	})
	query := BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: "001", PageSize: 1}
	if _, err := adapter.QueryBojunStocks(nil, query); err == nil {
		t.Fatal("nil context succeeded")
	}
	if _, err := (*Adapter)(nil).QueryBojunStocks(t.Context(), query); err == nil {
		t.Fatal("closed adapter succeeded")
	}
}

func TestQueryBojunStocksAccepts80ByteProductFilters(t *testing.T) {
	productValue := strings.Repeat("款", 26) + "aB"
	for _, test := range []struct {
		name  string
		query BojunStockQuery
	}{
		{"barcode", BojunStockQuery{ProductCode: []string{" " + productValue + " "}}},
		{"color", BojunStockQuery{ProductColor: []string{" " + productValue + " "}}},
		{"name", BojunStockQuery{ProductName: []string{" " + productValue + " "}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, _ string, arguments []driver.NamedValue) (driver.Rows, error) {
				if arguments[1].Value != productValue {
					t.Fatalf("product filter changed: %#v", arguments[1])
				}
				return &bojunQueryTestRows{columns: make([]string, 28)}, nil
			})
			test.query.StoreCode, test.query.PageSize = "0001", 1
			if _, err := adapter.QueryBojunStocks(t.Context(), test.query); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueryBojunStocksLimitsProductListsBeforeDeduplication(t *testing.T) {
	for _, batch := range []struct {
		name       string
		count      int
		duplicates bool
	}{
		{"one", 1, false},
		{"100 unique", 100, false},
		{"100 duplicates", 100, true},
		{"101 unique", 101, false},
		{"101 duplicates", 101, true},
	} {
		values := make([]string, batch.count)
		for index := range values {
			if batch.duplicates {
				values[index] = " SKU "
			} else {
				values[index] = fmt.Sprintf("%03dSKU", index)
			}
		}
		for _, filter := range []struct {
			name  string
			query BojunStockQuery
		}{
			{"barcode", BojunStockQuery{ProductCode: values}},
			{"color", BojunStockQuery{ProductColor: values}},
			{"name", BojunStockQuery{ProductName: values}},
		} {
			t.Run(filter.name+"/"+batch.name, func(t *testing.T) {
				calls := 0
				adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
					calls++
					if batch.count > 100 {
						t.Fatal("oversized input reached database")
					}
					unique := batch.count
					if batch.duplicates {
						unique = 1
					}
					if len(arguments) != unique+4 || !strings.Contains(statement, fmt.Sprintf(":%d)", unique+1)) {
						t.Fatalf("incorrect product bindings: %s %#v", statement, arguments)
					}
					for index := 0; index < unique; index++ {
						if arguments[index+1].Value != strings.TrimSpace(values[index]) {
							t.Fatalf("product %d changed: %#v", index, arguments[index+1])
						}
					}
					return &bojunQueryTestRows{columns: make([]string, 28)}, nil
				})
				filter.query.StoreCode, filter.query.PageSize = "001", 50
				_, err := adapter.QueryBojunStocks(t.Context(), filter.query)
				if batch.count > 100 {
					if err == nil || calls != 0 {
						t.Fatalf("oversized input accepted: error=%v calls=%d", err, calls)
					}
				} else if err != nil || calls != 1 {
					t.Fatalf("valid batch failed: error=%v calls=%d", err, calls)
				}
			})
		}
	}
}
