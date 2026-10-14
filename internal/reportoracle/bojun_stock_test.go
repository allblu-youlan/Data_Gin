package reportoracle

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func TestBojunStockQueryHasExactProjectionAndBindings(t *testing.T) {
	projection := strings.TrimSpace(strings.TrimPrefix(strings.Split(bojunStockQuerySQL, "FROM")[0], "\nSELECT "))
	got := strings.Split(projection, ",")
	for index := range got {
		got[index] = strings.TrimSpace(got[index])
	}
	want := []string{
		"a.CODE", "a.STORE_NAME", "a.M_PRODUCT_ALIAS_NO", "a.PROD_COLOR", "a.M_PRODUCT_NAME", "a.M_PRODUCT_VALUE",
		"a.M_ATTR_VALUE1", "a.M_ATTR_VALUE2_CODE", "a.M_ATTR_VALUE2", "a.PRICELIST", "a.QTY", "a.AMT_PRICEQTY",
		"a.QTY_BAS", "a.QTYPREOUT", "a.QTY_BAS_PREOUT", "a.AMT_PRICEQTYPREOUT", "a.QTYPREIN", "a.AMT_PRICEQTYPREIN",
		"a.QTY_FREEZE", "a.QTY_OMS", "a.QTYVALID", "a.AMT_PRICEQTYVALID", "a.QTYCONSIGN", "a.JST_PREOUT", "a.JST_DIFF_QTY",
		"a.QTYCAN", "a.M_ATTRIBUTESETINSTANCE_ID", "a.ISACTIVE",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{
		"FROM " + BojunStockView + " a", "WHERE a.M_PRODUCT_ALIAS_NO = :1\n  AND a.CODE = :2",
		"ORDER BY a.C_STORE_ID,\n         a.M_PRODUCTALIAS_ID,\n         a.M_ATTRIBUTESETINSTANCE_ID",
		"OFFSET :3 ROWS FETCH NEXT :4 ROWS ONLY",
	} {
		if !strings.Contains(bojunStockQuerySQL, fragment) {
			t.Fatalf("missing %q", fragment)
		}
	}
	for _, unwanted := range []string{"SELECT *", "a.*", "a.QTY >", "AND a.ISACTIVE", "TO_CHAR", ";"} {
		if strings.Contains(bojunStockQuerySQL, unwanted) {
			t.Fatalf("unexpected SQL fragment %q", unwanted)
		}
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
		if ctx != t.Context() || statement != bojunStockQuerySQL || strings.Contains(statement, productCode) || len(arguments) != 5 ||
			arguments[0].Value != productCode || arguments[1].Value != "001" || arguments[2].Value != 100 || arguments[3].Value != 2 {
			t.Fatalf("unexpected context, SQL or bindings: %s %#v", statement, arguments)
		}
		if option, ok := arguments[4].Value.(godror.Option); !ok || option == nil {
			t.Fatalf("missing godror number option: %#v", arguments[4])
		}
		return rows, nil
	})
	items, err := adapter.QueryBojunStocks(t.Context(), BojunStockQuery{ProductCode: " " + productCode + " ", StoreCode: " 001 ", PageSize: 1, Offset: 100})
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
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("invalid query reached database")
		return nil, nil
	})
	for _, query := range []BojunStockQuery{
		{StoreCode: "001", PageSize: 1}, {ProductCode: "SKU", PageSize: 1},
		{ProductCode: "SKU", StoreCode: " ", PageSize: 1},
		{ProductCode: strings.Repeat("a", 81), StoreCode: "001", PageSize: 1},
		{ProductCode: "SKU", StoreCode: strings.Repeat("a", 81), PageSize: 1},
		{ProductCode: "SKU", StoreCode: "001", PageSize: 0},
		{ProductCode: "SKU", StoreCode: "001", PageSize: 101},
		{ProductCode: "SKU", StoreCode: "001", PageSize: 1, Offset: -1},
	} {
		if _, err := adapter.QueryBojunStocks(t.Context(), query); err == nil {
			t.Fatalf("invalid query succeeded: %#v", query)
		}
	}
	query := BojunStockQuery{ProductCode: "SKU", StoreCode: "001", PageSize: 1}
	if _, err := adapter.QueryBojunStocks(nil, query); err == nil {
		t.Fatal("nil context succeeded")
	}
	if _, err := (*Adapter)(nil).QueryBojunStocks(t.Context(), query); err == nil {
		t.Fatal("closed adapter succeeded")
	}
}
