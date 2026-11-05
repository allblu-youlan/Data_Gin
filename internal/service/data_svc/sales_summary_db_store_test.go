package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type salesSummarySQLFixture struct {
	records  []map[string]driver.Value
	queryErr error
	rowErr   error
	closed   bool
}

type salesSummarySQLConnector struct{ fixture *salesSummarySQLFixture }

func (connector salesSummarySQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &salesSummarySQLConn{fixture: connector.fixture}, nil
}

func (salesSummarySQLConnector) Driver() driver.Driver { return salesSummarySQLDriver{} }

type salesSummarySQLDriver struct{}

func (salesSummarySQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type salesSummarySQLConn struct{ fixture *salesSummarySQLFixture }

func (*salesSummarySQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}

func (*salesSummarySQLConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (*salesSummarySQLConn) Close() error              { return nil }

func (connection *salesSummarySQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if connection.fixture.queryErr != nil {
		return nil, connection.fixture.queryErr
	}
	projection, _, _ := strings.Cut(strings.TrimPrefix(query, "SELECT "), " FROM ")
	columns := strings.Split(projection, ",")
	for index, column := range columns {
		columns[index] = strings.Trim(strings.TrimSpace(column), "`")
	}
	return &salesSummarySQLRows{fixture: connection.fixture, columns: columns}, nil
}

type salesSummarySQLRows struct {
	fixture *salesSummarySQLFixture
	columns []string
	index   int
}

func (rows *salesSummarySQLRows) Columns() []string { return rows.columns }
func (rows *salesSummarySQLRows) Close() error {
	rows.fixture.closed = true
	return nil
}

func (rows *salesSummarySQLRows) Next(values []driver.Value) error {
	if rows.index == len(rows.fixture.records) {
		if rows.fixture.rowErr != nil {
			return rows.fixture.rowErr
		}
		return io.EOF
	}
	for index, column := range rows.columns {
		values[index] = rows.fixture.records[rows.index][column]
	}
	rows.index++
	return nil
}

func salesSummaryTestDB(testingT testing.TB, fixture *salesSummarySQLFixture) *gorm.DB {
	testingT.Helper()
	sqlDB := sql.OpenDB(salesSummarySQLConnector{fixture: fixture})
	testingT.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		testingT.Fatal(err)
	}
	return db
}

func TestSalesSummaryReadPushedOrdersPreservesNullableValues(t *testing.T) {
	fixture := &salesSummarySQLFixture{records: []map[string]driver.Value{
		{},
		{
			"docno": []byte("API-1"), "order_type_code": []byte("EXP"),
			"push_amount": []byte("0"), "tot_amt_actual": []byte("88.50"),
			"tot_qty": []byte("-3"), "items_json": []byte(`[{"qty":"-3"}]`),
		},
		{"oracle_retail_id": []byte("0"), "push_amount": []byte("12.30")},
		{"oracle_retail_id": []byte("18446744073709551615"), "push_amount": []byte("99.99")},
		{},
	}}
	store := &salesSummaryDBStore{db: salesSummaryTestDB(t, fixture)}
	var orders []model.BojunRetailOrder
	err := store.ReadPushedOrders(t.Context(), "ABCN001A003", 20261001, 20261031, func(order model.BojunRetailOrder) error {
		orders = append(orders, order)
		return nil
	})
	if err != nil || len(orders) != 5 {
		t.Fatalf("orders=%d error=%v", len(orders), err)
	}
	if !reflect.DeepEqual(orders[0], model.BojunRetailOrder{}) || !reflect.DeepEqual(orders[4], model.BojunRetailOrder{}) {
		t.Fatalf("NULL rows retained values: first=%+v last=%+v", orders[0], orders[4])
	}
	wantAPI := model.BojunRetailOrder{
		DocNo: "API-1", OrderTypeCode: "EXP", TotalAmtActual: 88.5, TotalQty: -3, ItemsJSON: `[{"qty":"-3"}]`,
	}
	if !reflect.DeepEqual(orders[1], wantAPI) {
		t.Fatalf("API row=%+v, want %+v", orders[1], wantAPI)
	}
	if orders[2].OracleRetailID == nil || *orders[2].OracleRetailID != 0 || orders[2].PushAmount != 12.3 {
		t.Fatalf("zero Oracle ID lost its presence: %+v", orders[2])
	}
	if orders[3].OracleRetailID == nil || *orders[3].OracleRetailID != math.MaxUint64 || orders[3].PushAmount != 99.99 {
		t.Fatalf("unsigned Oracle ID was changed: %+v", orders[3])
	}
	if orders[2].OracleRetailID == orders[3].OracleRetailID || !fixture.closed {
		t.Fatal("streamed rows share an Oracle ID pointer or leave SQL rows open")
	}
}

func TestSalesSummaryReadPushedOrdersReturnsStreamErrors(t *testing.T) {
	failure := errors.New("read failed")
	for _, mode := range []string{"query", "row", "visitor", "invalid number"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &salesSummarySQLFixture{records: []map[string]driver.Value{{"tot_qty": int64(2)}}}
			switch mode {
			case "query":
				fixture.queryErr = failure
			case "row":
				fixture.rowErr = failure
			case "invalid number":
				fixture.records[0]["tot_qty"] = []byte("invalid")
			}
			store := &salesSummaryDBStore{db: salesSummaryTestDB(t, fixture)}
			err := store.ReadPushedOrders(t.Context(), "ABCN001A003", 20261001, 20261031, func(model.BojunRetailOrder) error {
				if mode == "visitor" {
					return failure
				}
				return nil
			})
			if err == nil || (mode != "invalid number" && !errors.Is(err, failure)) {
				t.Fatalf("stream error = %v", err)
			}
			if mode != "query" && !fixture.closed {
				t.Fatal("SQL rows were not closed after failure")
			}
		})
	}
}

func TestSalesSummaryReadPushedOrdersAllocationBudget(t *testing.T) {
	store := &salesSummaryDBStore{db: salesSummaryTestDB(t, salesSummaryBenchmarkFixture(100))}
	var metrics SalesSummaryMetrics
	var err error
	allocations := testing.AllocsPerRun(3, func() {
		metrics = SalesSummaryMetrics{}
		err = store.ReadPushedOrders(t.Context(), "ABCN001A003", 20261001, 20261105, func(order model.BojunRetailOrder) error {
			return addSalesSummaryOrder(&metrics, order)
		})
	})
	if err != nil || metrics.OrderCount != 100 || metrics.SalesQuantity != 200 {
		t.Fatalf("streamed aggregation = %+v, error = %v", metrics, err)
	}
	if allocations > 2000 {
		t.Fatalf("100-row aggregation allocated %.0f times, want at most 2000", allocations)
	}
}

func salesSummaryBenchmarkFixture(count int) *salesSummarySQLFixture {
	record := map[string]driver.Value{
		"id": int64(1), "docno": []byte("ORDER-1"), "oracle_retail_id": []byte("35"),
		"order_type_code": []byte("CMR"), "billdate": int64(20261105),
		"push_amount": []byte("288.35"), "tot_amt_actual": []byte("300.00"),
		"tot_qty": int64(2), "items_json": []byte(`[{"qty":2}]`),
	}
	fixture := &salesSummarySQLFixture{records: make([]map[string]driver.Value, count)}
	for index := range fixture.records {
		fixture.records[index] = record
	}
	return fixture
}
