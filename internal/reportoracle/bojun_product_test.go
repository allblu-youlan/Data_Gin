package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

const bojunProductTestDriverName = "bojun-product-test"

var registerBojunProductTestDriver sync.Once

type bojunProductTestDriver struct{}

func (bojunProductTestDriver) Open(string) (driver.Conn, error) {
	return &bojunProductTestConnection{}, nil
}

type bojunProductTestConnection struct{}

func (*bojunProductTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (*bojunProductTestConnection) Close() error              { return nil }
func (*bojunProductTestConnection) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }
func (*bojunProductTestConnection) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (*bojunProductTestConnection) QueryContext(
	_ context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Rows, error) {
	if query != bojunProductByCodeSQL {
		return nil, fmt.Errorf("unexpected statement: %s", query)
	}
	if len(arguments) != 1 || arguments[0].Value != "C17F2021H063090" {
		return nil, fmt.Errorf("unexpected arguments: %#v", arguments)
	}
	return &bojunProductTestRows{}, nil
}

type bojunProductTestRows struct{ read bool }

func (*bojunProductTestRows) Columns() []string {
	return []string{"PRICELIST", "PRODUCT_NAME", "VALUE", "NO", "PROD_COLOR", "VALUE1", "VALUE2"}
}
func (*bojunProductTestRows) Close() error { return nil }
func (rows *bojunProductTestRows) Next(values []driver.Value) error {
	if rows.read {
		return io.EOF
	}
	rows.read = true
	copy(values, []driver.Value{int64(179), " C17F2021 ", "儿童|半裙", "C17F2021H063090", "C17F2021H063", "甜蜜薰衣草 ", "090"})
	return nil
}

func TestBojunProductSQLUsesFixedProjectionAndBinding(t *testing.T) {
	if !strings.Contains(bojunProductByCodeSQL, "FROM "+BojunProductView) {
		t.Fatalf("SQL does not use fixed view %s", BojunProductView)
	}
	for _, column := range []string{"PRICELIST", "PRODUCT_NAME", "VALUE", "NO", "PROD_COLOR", "VALUE1", "VALUE2"} {
		if !strings.Contains(bojunProductByCodeSQL, column) {
			t.Fatalf("SQL is missing %q", column)
		}
	}
	for _, internalID := range []string{"M_PRODUCT_COLOR_ID", "M_PRODUCT_ID", "M_PRODUCTALIAS_ID", "M_ATTRIBUTESETINSTANCE_ID"} {
		if strings.Contains(bojunProductByCodeSQL, internalID) {
			t.Fatalf("SQL exposes internal column %q", internalID)
		}
	}
	if !strings.Contains(bojunProductByCodeSQL, "WHERE NO = :1") ||
		strings.Contains(bojunProductByCodeSQL, "SELECT *") || strings.Contains(bojunProductByCodeSQL, "%s") ||
		strings.Contains(bojunProductByCodeSQL, ";") {
		t.Fatalf("SQL is not a fixed parameterized query: %s", bojunProductByCodeSQL)
	}
}

func TestQueryBojunProductByCodeBindsAndMapsRow(t *testing.T) {
	registerBojunProductTestDriver.Do(func() {
		sql.Register(bojunProductTestDriverName, bojunProductTestDriver{})
	})
	db, err := sql.Open(bojunProductTestDriverName, "")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()

	adapter := &Adapter{db: db}
	item, err := adapter.QueryBojunProductByCode(t.Context(), " C17F2021H063090 ")
	if err != nil {
		t.Fatalf("QueryBojunProductByCode() error = %v", err)
	}
	if item.PriceList != "179" || item.ProductName != "C17F2021" || item.Value != "儿童|半裙" ||
		item.ProductCode != "C17F2021H063090" || item.ProdColor != "C17F2021H063" ||
		item.Value1 != "甜蜜薰衣草" || item.Value2 != "090" {
		t.Fatalf("item = %#v", item)
	}
}

func TestQueryBojunProductByCodeRejectsClosedOrInvalidQuery(t *testing.T) {
	var adapter *Adapter
	if _, err := adapter.QueryBojunProductByCode(t.Context(), "C17F2021H063090"); err == nil {
		t.Fatal("closed adapter unexpectedly succeeded")
	}
	adapter = &Adapter{db: &sql.DB{}}
	if _, err := adapter.QueryBojunProductByCode(t.Context(), ""); err == nil {
		t.Fatal("empty product code unexpectedly succeeded")
	}
}
