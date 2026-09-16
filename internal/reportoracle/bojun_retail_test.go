package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBojunRetailSQLUsesFixedTableAndBoundFilters(t *testing.T) {
	const expectedTable = "YL_DBS.BJ_REPORT_RETAIL_SF"
	if BojunRetailTable != expectedTable {
		t.Fatalf("BojunRetailTable = %q, want %q", BojunRetailTable, expectedTable)
	}

	for name, statement := range map[string]string{
		"incremental": bojunRetailAfterIDSQL,
		"time range":  bojunRetailStatusTimeRangeSQL,
		"maximum id":  bojunRetailMaxIDSQL,
		"push status": bojunRetailPushStatusSQL,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(statement, BojunRetailTable) {
				t.Fatalf("SQL does not use fixed table %s", BojunRetailTable)
			}
			if strings.Contains(statement, "%s") || strings.Contains(statement, ";") {
				t.Fatalf("SQL permits dynamic or stacked statements: %s", statement)
			}
		})
	}
	for _, fragment := range []string{"M_RETAIL_ID > :1", "ROWNUM <= :2", "ORDER BY M_RETAIL_ID"} {
		if !strings.Contains(bojunRetailAfterIDSQL, fragment) {
			t.Fatalf("incremental SQL is missing %q", fragment)
		}
	}
	for _, fragment := range []string{"STATUSTIME >= :1", "STATUSTIME < :2", "M_RETAIL_ID > :3", "ROWNUM <= :4"} {
		if !strings.Contains(bojunRetailStatusTimeRangeSQL, fragment) {
			t.Fatalf("time range SQL is missing %q", fragment)
		}
	}
	if strings.Contains(bojunRetailStatusTimeRangeSQL, "MODIFIEDDATE") {
		t.Fatal("time range SQL must filter by STATUSTIME instead of MODIFIEDDATE")
	}
	for _, statement := range []string{bojunRetailAfterIDSQL, bojunRetailStatusTimeRangeSQL} {
		for _, fragment := range []string{
			bojunRetailSourceHeadTable, "h.ID = r.M_RETAIL_ID", "h.STATUS = 2", "h.ISACTIVE = 'Y'",
			"STORE_NAME", "DM_VP_C_VIP_MOBILE", "TOT_AMT_SF", "TOT_AMT_TS", "IS_TOSHOP",
			"NVL(r.STATUS, '0') AS STATUS",
		} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("retail SQL is missing %q", fragment)
			}
		}
		if strings.Contains(statement, "JSON_ITEM") {
			t.Fatal("retail SQL must derive items from the retail item query")
		}
	}
	for _, fragment := range []string{
		bojunRetailSourceHeadTable, "h.ID = r.M_RETAIL_ID", "h.STATUS = 2", "h.ISACTIVE = 'Y'",
	} {
		if !strings.Contains(bojunRetailMaxIDSQL, fragment) {
			t.Fatalf("maximum id SQL is missing %q", fragment)
		}
	}
}

func TestBojunRetailItemsSQLUsesFixedSourcesAndBoundIDs(t *testing.T) {
	orders := []BojunRetailRow{{RetailID: 45077}, {RetailID: 45078}, {RetailID: 45077}}
	statement, arguments, err := buildBojunRetailItemsQuery(orders)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		BojunRetailTable,
		bojunRetailItemTable,
		BojunProductView,
		"b.ISACTIVE = 'Y'",
		"a.M_RETAIL_ID IN (:1, :2)",
		"ORDER BY a.M_RETAIL_ID, c.NO",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("retail item SQL is missing %q: %s", fragment, statement)
		}
	}
	if strings.Contains(statement, "45077") || strings.Contains(statement, "45078") {
		t.Fatalf("retail item SQL contains unbound IDs: %s", statement)
	}
	if len(arguments) != 2 || arguments[0] != uint64(45077) || arguments[1] != uint64(45078) {
		t.Fatalf("retail item query arguments = %#v", arguments)
	}
}

func TestBojunRetailPayItemsSQLUsesFixedSourcesAndBoundIDs(t *testing.T) {
	orders := []BojunRetailRow{{RetailID: 45077}, {RetailID: 45078}, {RetailID: 45077}}
	statement, arguments, err := buildBojunRetailPayItemsQuery(orders)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		bojunRetailPayItemTable,
		bojunRetailPaywayTable,
		bojunRetailSourceHeadTable,
		"k.ID = a.C_PAYWAY_ID",
		"a.ISACTIVE = 'Y'",
		"b.STATUS = 2",
		"b.ISACTIVE = 'Y'",
		"b.ID IN (:1, :2)",
		"GROUP BY b.ID, a.C_PAYWAY_ID, k.NAME",
		"ORDER BY b.ID, a.C_PAYWAY_ID",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("retail pay item SQL is missing %q: %s", fragment, statement)
		}
	}
	if strings.Contains(statement, "45077") || strings.Contains(statement, "45078") {
		t.Fatalf("retail pay item SQL contains unbound IDs: %s", statement)
	}
	if len(arguments) != 2 || arguments[0] != uint64(45077) || arguments[1] != uint64(45078) {
		t.Fatalf("retail pay item query arguments = %#v", arguments)
	}
}

const bojunRetailQueryTestDriverName = "bojun-retail-query-test"

var registerBojunRetailQueryTestDriver sync.Once
var activeBojunRetailQueryTestState *bojunRetailQueryTestState

type bojunRetailQueryTestState struct {
	headerCalls  int
	itemCalls    int
	payItemCalls int
	headerRows   *bojunRetailQueryTestRows
	itemRows     *bojunRetailQueryTestRows
}

type bojunRetailQueryTestDriver struct{}

func (bojunRetailQueryTestDriver) Open(string) (driver.Conn, error) {
	return &bojunRetailQueryTestConn{state: activeBojunRetailQueryTestState}, nil
}

type bojunRetailQueryTestConn struct {
	state *bojunRetailQueryTestState
}

func (*bojunRetailQueryTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*bojunRetailQueryTestConn) Close() error                        { return nil }
func (*bojunRetailQueryTestConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (*bojunRetailQueryTestConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (connection *bojunRetailQueryTestConn) QueryContext(
	_ context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Rows, error) {
	if connection.state == nil {
		return nil, fmt.Errorf("query test state is unavailable")
	}
	statusTime := time.Date(2026, 6, 16, 13, 27, 18, 0, time.Local)
	switch {
	case query == bojunRetailAfterIDSQL || query == bojunRetailStatusTimeRangeSQL:
		connection.state.headerCalls++
		rows := &bojunRetailQueryTestRows{
			columns: []string{
				"M_RETAIL_ID", "STORE_CODE", "STORE_NAME", "DOCNO", "RETAILSALETYPE", "STATUSTIME",
				"DM_VP_C_VIP_MOBILE", "TOT_AMT_SF", "TOT_AMT_TS", "IS_TOSHOP", "STATUS",
			},
			values: [][]driver.Value{
				{int64(45077), "STORE-1", "一店", "ORDER-1", "CMR", statusTime, "", 160.65, 160.65, "Y", int64(0)},
				{int64(45078), "STORE-2", "二店", "ORDER-2", "CMR", statusTime, "", 100.0, 100.0, "N", int64(0)},
			},
		}
		connection.state.headerRows = rows
		return rows, nil
	case strings.HasPrefix(query, bojunRetailItemsSQLPrefix):
		connection.state.itemCalls++
		if connection.state.headerRows == nil || !connection.state.headerRows.closed {
			return nil, fmt.Errorf("retail header rows were not closed before item query")
		}
		if len(arguments) != 4 || arguments[0].Value != uint64(45077) || arguments[1].Value != uint64(45078) {
			return nil, fmt.Errorf("retail item query arguments = %#v", arguments)
		}
		rows := &bojunRetailQueryTestRows{
			columns: []string{
				"M_RETAIL_ID", "DOCNO", "TYPE", "MARKDIS", "PRODUCT_NAME", "PRODUCT_VALUE", "PROD_COLOR", "NO",
				"VALUE1", "VALUE2", "QTY", "DM_AMT_RETAIL", "TOT_AMT_ACTUAL", "TOT_AMT_LIST", "AMT_ACC",
				"TOT_AMT_ACC", "DISCOUNT", "PRICELIST", "PRICEACTUAL",
			},
			values: [][]driver.Value{
				{int64(45077), "ORDER-1", 1.0, 0.0, "C41H1079A", "儿童|家居服", "C41H1079AG265", "C41H1079AG265130", "中灰蓝", "130CM", 1.0, nil, 160.65, 189.0, 0.0, 0.0, 0.85, 189.0, 160.65},
			},
		}
		connection.state.itemRows = rows
		return rows, nil
	case strings.HasPrefix(query, bojunRetailPayItemsSQLPrefix):
		connection.state.payItemCalls++
		if connection.state.itemRows == nil || !connection.state.itemRows.closed {
			return nil, fmt.Errorf("retail item rows were not closed before pay item query")
		}
		if len(arguments) != 4 || arguments[0].Value != uint64(45077) || arguments[1].Value != uint64(45078) {
			return nil, fmt.Errorf("retail pay item query arguments = %#v", arguments)
		}
		return &bojunRetailQueryTestRows{
			columns: []string{"M_RETAIL_ID", "C_PAYWAY_ID", "C_PAYWAY_NAME", "TOT_AMT_CX"},
			values: [][]driver.Value{
				{int64(45077), int64(25), " 支付宝 ", 250.2},
				{int64(45077), int64(24), "微信", 20.5},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected statement: %s", query)
	}
}

type bojunRetailQueryTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
	closed  bool
}

func (rows *bojunRetailQueryTestRows) Columns() []string { return rows.columns }
func (rows *bojunRetailQueryTestRows) Close() error {
	rows.closed = true
	return nil
}
func (rows *bojunRetailQueryTestRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.index])
	rows.index++
	return nil
}

func TestQueryBojunRetailAfterIDBuildsDetailJSONWithOneBatchQueryEach(t *testing.T) {
	registerBojunRetailQueryTestDriver.Do(func() {
		sql.Register(bojunRetailQueryTestDriverName, bojunRetailQueryTestDriver{})
	})
	state := &bojunRetailQueryTestState{}
	activeBojunRetailQueryTestState = state
	defer func() { activeBojunRetailQueryTestState = nil }()
	db, err := sql.Open(bojunRetailQueryTestDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adapter := &Adapter{db: db, prefetchRows: 100, fetchArraySize: 100}
	orders, err := adapter.QueryBojunRetailAfterID(t.Context(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	if state.headerCalls != 1 || state.itemCalls != 1 || state.payItemCalls != 1 {
		t.Fatalf("query calls = header:%d items:%d pay items:%d, want 1 each", state.headerCalls, state.itemCalls, state.payItemCalls)
	}
	if !strings.Contains(orders[0].ItemsJSON, `"no":"C41H1079AG265130"`) ||
		!strings.Contains(orders[0].ItemsJSON, `"mProductName":"C41H1079A"`) {
		t.Fatalf("first order ItemsJSON = %s", orders[0].ItemsJSON)
	}
	if orders[1].ItemsJSON != "[]" {
		t.Fatalf("second order ItemsJSON = %q, want []", orders[1].ItemsJSON)
	}
	var payItems []map[string]interface{}
	if err := json.Unmarshal([]byte(orders[0].PayItemsJSON), &payItems); err != nil {
		t.Fatalf("unmarshal generated pay items JSON: %v", err)
	}
	if len(payItems) != 2 || payItems[0]["cPaywayId"] != float64(25) ||
		payItems[0]["cPaywayName"] != "支付宝" || payItems[0]["payamount"] != 250.2 ||
		payItems[1]["cPaywayName"] != "微信" {
		t.Fatalf("first order PayItemsJSON = %s", orders[0].PayItemsJSON)
	}
	if orders[1].PayItemsJSON != "[]" {
		t.Fatalf("second order PayItemsJSON = %q, want []", orders[1].PayItemsJSON)
	}
}

func TestQueryBojunRetailByStatusTimeUsesOneBatchQueryForEachDetailType(t *testing.T) {
	registerBojunRetailQueryTestDriver.Do(func() {
		sql.Register(bojunRetailQueryTestDriverName, bojunRetailQueryTestDriver{})
	})
	state := &bojunRetailQueryTestState{}
	activeBojunRetailQueryTestState = state
	defer func() { activeBojunRetailQueryTestState = nil }()
	db, err := sql.Open(bojunRetailQueryTestDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adapter := &Adapter{db: db, prefetchRows: 100, fetchArraySize: 100}
	start := time.Date(2026, 6, 16, 0, 0, 0, 0, time.Local)
	orders, err := adapter.QueryBojunRetailByStatusTime(t.Context(), start, start.Add(24*time.Hour), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 || state.headerCalls != 1 || state.itemCalls != 1 || state.payItemCalls != 1 {
		t.Fatalf(
			"orders=%d query calls=header:%d items:%d pay items:%d, want 2 and 1 each",
			len(orders), state.headerCalls, state.itemCalls, state.payItemCalls,
		)
	}
	if orders[0].ItemsJSON == "[]" || orders[0].PayItemsJSON == "[]" ||
		orders[1].ItemsJSON != "[]" || orders[1].PayItemsJSON != "[]" {
		t.Fatalf("detail JSON mapping = %#v", orders)
	}
}

type bojunRetailScannerFunc func(dest ...interface{}) error

func (scan bojunRetailScannerFunc) Scan(dest ...interface{}) error {
	return scan(dest...)
}

func TestScanBojunRetailRowMapsStoreName(t *testing.T) {
	statusTime := time.Date(2026, 8, 27, 10, 30, 0, 0, time.Local)
	row, err := scanBojunRetailRow(bojunRetailScannerFunc(func(dest ...interface{}) error {
		if len(dest) != 11 {
			return fmt.Errorf("scan destinations = %d, want 11", len(dest))
		}
		*dest[0].(*int64) = 45077
		*dest[1].(*sql.NullString) = sql.NullString{String: " STORE-01 ", Valid: true}
		*dest[2].(*sql.NullString) = sql.NullString{String: " 商场一店 ", Valid: true}
		*dest[3].(*sql.NullString) = sql.NullString{String: " SALE-45077 ", Valid: true}
		*dest[4].(*sql.NullString) = sql.NullString{String: " CMR ", Valid: true}
		*dest[5].(*sql.NullTime) = sql.NullTime{Time: statusTime, Valid: true}
		*dest[6].(*sql.NullString) = sql.NullString{String: "18616613488", Valid: true}
		*dest[7].(*sql.NullFloat64) = sql.NullFloat64{Float64: 88.8, Valid: true}
		*dest[8].(*sql.NullFloat64) = sql.NullFloat64{Float64: 80, Valid: true}
		*dest[9].(*sql.NullString) = sql.NullString{String: "Y", Valid: true}
		*dest[10].(*sql.NullInt64) = sql.NullInt64{Int64: 0, Valid: true}
		return nil
	}))
	if err != nil {
		t.Fatalf("scanBojunRetailRow() error = %v", err)
	}
	if row.StoreCode != "STORE-01" || row.StoreName != "商场一店" || row.DocNo != "SALE-45077" {
		t.Fatalf("store/order mapping = %+v", row)
	}
}

func TestScanBojunRetailItemBuildsCompatibleJSON(t *testing.T) {
	item, err := scanBojunRetailItem(bojunRetailScannerFunc(func(dest ...interface{}) error {
		if len(dest) != 19 {
			return fmt.Errorf("scan destinations = %d, want 19", len(dest))
		}
		*dest[0].(*int64) = 45077
		for index, value := range map[int]string{
			1: " E20260616132718078200083 ", 4: " C41H1079A ", 5: " 儿童|家居服 ",
			6: " C41H1079AG265 ", 7: " C41H1079AG265130 ", 8: " 中灰蓝 ", 9: " 130CM ",
		} {
			*dest[index].(*sql.NullString) = sql.NullString{String: value, Valid: true}
		}
		for index, value := range map[int]float64{
			2: 1, 3: 0, 10: 1, 12: 160.65, 13: 189, 14: 0, 15: 0,
			16: 0.85, 17: 189, 18: 160.65,
		} {
			*dest[index].(*sql.NullFloat64) = sql.NullFloat64{Float64: value, Valid: true}
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	orders := []BojunRetailRow{{RetailID: 45077}}
	if err := setBojunRetailItemsJSON(orders, map[uint64][]bojunRetailItem{45077: {item}}); err != nil {
		t.Fatal(err)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(orders[0].ItemsJSON), &items); err != nil {
		t.Fatalf("unmarshal generated items JSON: %v", err)
	}
	if len(items) != 1 || items[0]["no"] != "C41H1079AG265130" ||
		items[0]["mProductName"] != "C41H1079A" || items[0]["qty"] != float64(1) ||
		items[0]["totAmtActual"] != 160.65 || items[0]["dmAmtRetail"] != nil {
		t.Fatalf("generated items JSON = %s", orders[0].ItemsJSON)
	}
}

func TestSetBojunRetailItemsJSONUsesEmptyArrayWhenNoActiveItems(t *testing.T) {
	orders := []BojunRetailRow{{RetailID: 45077}}
	if err := setBojunRetailItemsJSON(orders, nil); err != nil {
		t.Fatal(err)
	}
	if orders[0].ItemsJSON != "[]" {
		t.Fatalf("ItemsJSON = %q, want []", orders[0].ItemsJSON)
	}
}

func TestSetBojunRetailPayItemsJSONUsesEmptyArrayWhenNoActiveItems(t *testing.T) {
	orders := []BojunRetailRow{{RetailID: 45077}}
	if err := setBojunRetailPayItemsJSON(orders, nil); err != nil {
		t.Fatal(err)
	}
	if orders[0].PayItemsJSON != "[]" {
		t.Fatalf("PayItemsJSON = %q, want []", orders[0].PayItemsJSON)
	}
}

func TestBojunRetailBatchSizeValidation(t *testing.T) {
	for _, limit := range []int{1, maxBojunRetailBatchSize} {
		if err := validateBojunRetailBatchSize(limit); err != nil {
			t.Fatalf("validateBojunRetailBatchSize(%d) error = %v", limit, err)
		}
	}
	for _, limit := range []int{0, -1, maxBojunRetailBatchSize + 1} {
		if err := validateBojunRetailBatchSize(limit); err == nil {
			t.Fatalf("validateBojunRetailBatchSize(%d) unexpectedly succeeded", limit)
		}
	}
}

func TestBojunRetailAdapterRejectsClosedConnection(t *testing.T) {
	var adapter *Adapter
	if _, err := adapter.QueryBojunRetailAfterID(t.Context(), 0, 100); err == nil {
		t.Fatal("QueryBojunRetailAfterID() unexpectedly succeeded")
	}
	if _, err := adapter.MaxBojunRetailID(t.Context()); err == nil {
		t.Fatal("MaxBojunRetailID() unexpectedly succeeded")
	}
	if err := adapter.UpdateBojunRetailPushStatus(t.Context(), 1, true, 20260826); err == nil {
		t.Fatal("UpdateBojunRetailPushStatus() unexpectedly succeeded")
	}
}

const bojunRetailUpdateTestDriverName = "bojun-retail-update-test"

var registerBojunRetailUpdateTestDriver sync.Once

type bojunRetailUpdateTestDriver struct{}

func (bojunRetailUpdateTestDriver) Open(name string) (driver.Conn, error) {
	rowsAffected, err := strconv.ParseInt(name, 10, 64)
	if err != nil {
		return nil, err
	}
	return &bojunRetailUpdateTestConn{rowsAffected: rowsAffected}, nil
}

type bojunRetailUpdateTestConn struct {
	rowsAffected int64
}

func (*bojunRetailUpdateTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*bojunRetailUpdateTestConn) Close() error                        { return nil }
func (*bojunRetailUpdateTestConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (connection *bojunRetailUpdateTestConn) ExecContext(
	_ context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Result, error) {
	if query != bojunRetailPushStatusSQL {
		return nil, fmt.Errorf("unexpected statement: %s", query)
	}
	if len(arguments) != 3 {
		return nil, fmt.Errorf("update arguments = %d, want 3", len(arguments))
	}
	return driver.RowsAffected(connection.rowsAffected), nil
}

func TestUpdateBojunRetailPushStatusAcceptsAllMatchingRows(t *testing.T) {
	registerBojunRetailUpdateTestDriver.Do(func() {
		sql.Register(bojunRetailUpdateTestDriverName, bojunRetailUpdateTestDriver{})
	})

	tests := []struct {
		name         string
		rowsAffected int64
		wantError    bool
	}{
		{name: "no matching row", rowsAffected: 0, wantError: true},
		{name: "one matching row", rowsAffected: 1},
		{name: "duplicate matching rows", rowsAffected: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open(bojunRetailUpdateTestDriverName, strconv.FormatInt(test.rowsAffected, 10))
			if err != nil {
				t.Fatalf("open test database: %v", err)
			}
			defer db.Close()

			adapter := &Adapter{db: db}
			err = adapter.UpdateBojunRetailPushStatus(t.Context(), 46038, true, 20260829)
			if test.wantError {
				if err == nil {
					t.Fatal("UpdateBojunRetailPushStatus() unexpectedly succeeded")
				}
				if !strings.Contains(err.Error(), "retail id 46038 not found") {
					t.Fatalf("UpdateBojunRetailPushStatus() error = %v", err)
				}
			}
			if !test.wantError && err != nil {
				t.Fatalf("UpdateBojunRetailPushStatus() error = %v", err)
			}
		})
	}
}
