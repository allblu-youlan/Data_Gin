package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type bojunQueryTestFunc func(context.Context, string, []driver.NamedValue) (driver.Rows, error)

type bojunQueryTestConnector struct{ query bojunQueryTestFunc }

func (connector bojunQueryTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &bojunQueryTestConnection{query: connector.query}, nil
}
func (connector bojunQueryTestConnector) Driver() driver.Driver {
	return bojunQueryTestDriver{query: connector.query}
}

type bojunQueryTestDriver struct{ query bojunQueryTestFunc }

func (testDriver bojunQueryTestDriver) Open(string) (driver.Conn, error) {
	return &bojunQueryTestConnection{query: testDriver.query}, nil
}

type bojunQueryTestConnection struct{ query bojunQueryTestFunc }

func (*bojunQueryTestConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*bojunQueryTestConnection) Close() error                        { return nil }
func (*bojunQueryTestConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (*bojunQueryTestConnection) CheckNamedValue(*driver.NamedValue) error {
	return nil
}
func (connection *bojunQueryTestConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return connection.query(ctx, query, args)
}

type bojunQueryTestRows struct {
	columns  []string
	values   [][]driver.Value
	index    int
	nextErr  error
	closeErr error
	closed   bool
}

func (rows *bojunQueryTestRows) Columns() []string { return rows.columns }
func (rows *bojunQueryTestRows) Close() error {
	rows.closed = true
	return rows.closeErr
}
func (rows *bojunQueryTestRows) Next(values []driver.Value) error {
	if rows.index >= len(rows.values) {
		if rows.nextErr != nil {
			return rows.nextErr
		}
		return io.EOF
	}
	copy(values, rows.values[rows.index])
	rows.index++
	return nil
}

func newBojunQueryTestAdapter(t *testing.T, query bojunQueryTestFunc) *Adapter {
	t.Helper()
	db := sql.OpenDB(bojunQueryTestConnector{query: query})
	t.Cleanup(func() { _ = db.Close() })
	return &Adapter{db: db}
}

func TestBojunStoreQueryProjectionAndOptionalBindings(t *testing.T) {
	if got := strings.Fields(strings.Split(bojunStoreQueryBaseSQL, "FROM")[0]); !reflect.DeepEqual(got,
		[]string{"SELECT", "s.ID,", "s.CODE,", "s.NAME,", "s.ISACTIVE,", "s.ISSTOP"}) {
		t.Fatalf("unexpected projection: %v", got)
	}
	if !strings.Contains(bojunStoreQueryBaseSQL, "FROM "+BojunStoreTable+" s\nWHERE s.CODE IS NOT NULL") {
		t.Fatal("store source or non-null code condition is missing")
	}
	for _, test := range []struct {
		name      string
		query     BojunStoreQuery
		fragments []string
		arguments []any
	}{
		{
			name: "default active stores", query: BojunStoreQuery{IsActive: "Y", PageSize: 50},
			fragments: []string{"AND s.ISACTIVE = :1", "OFFSET :2 ROWS FETCH NEXT :3 ROWS ONLY"},
			arguments: []any{"Y", 0, 51},
		},
		{
			name: "all filters", query: BojunStoreQuery{IsActive: "N", StoreCode: "001' OR 1=1 --", NamePrefix: "店!%_", IsStop: "Y", PageSize: 20, Offset: 40},
			fragments: []string{"AND s.ISACTIVE = :1", "AND s.CODE = :2", "AND s.NAME LIKE :3 ESCAPE '!'", "AND s.ISSTOP = :4", "OFFSET :5 ROWS FETCH NEXT :6 ROWS ONLY"},
			arguments: []any{"N", "001' OR 1=1 --", "店!!!%!_%", "Y", 40, 21},
		},
		{
			name: "all statuses renumber", query: BojunStoreQuery{IsActive: "ALL", StoreCode: "001", IsStop: "N", PageSize: 100},
			fragments: []string{"AND s.CODE = :1", "AND s.ISSTOP = :2", "OFFSET :3 ROWS FETCH NEXT :4 ROWS ONLY"},
			arguments: []any{"001", "N", 0, 101},
		},
		{
			name: "no filters", query: BojunStoreQuery{IsActive: "ALL", PageSize: 1},
			fragments: []string{"OFFSET :1 ROWS FETCH NEXT :2 ROWS ONLY"}, arguments: []any{0, 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			statement, arguments := buildBojunStoreQuery(test.query)
			if !reflect.DeepEqual(arguments, test.arguments) {
				t.Fatalf("arguments = %#v, want %#v", arguments, test.arguments)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(statement, fragment) {
					t.Fatalf("missing %q: %s", fragment, statement)
				}
			}
			if strings.Contains(statement, "OR 1=1") || strings.Contains(statement, "店") ||
				strings.Contains(statement, "SELECT *") || strings.Contains(statement, ";") ||
				!strings.Contains(statement, "ORDER BY s.ID") ||
				(test.query.IsActive == "ALL" && strings.Contains(statement, "AND s.ISACTIVE")) {
				t.Fatalf("unsafe or incorrect SQL: %s", statement)
			}
		})
	}
}

func TestQueryBojunStoresMapsRowsAndCloses(t *testing.T) {
	rows := &bojunQueryTestRows{
		columns: []string{"ID", "CODE", "NAME", "ISACTIVE", "ISSTOP"},
		values: [][]driver.Value{
			{int64(1), "001", " 仓库一 ", "Y ", nil},
			{int64(2), "002", "仓库二", "N", " N "},
		},
	}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() || !strings.Contains(query, "AND s.ISACTIVE = :1") || len(args) != 4 ||
			args[0].Value != "Y" || args[1].Value != "001" || args[2].Value != 2 || args[3].Value != 2 {
			t.Fatalf("unexpected context, SQL or arguments: %s %#v", query, args)
		}
		return rows, nil
	})
	items, err := adapter.QueryBojunStores(t.Context(), BojunStoreQuery{StoreCode: " 001 ", PageSize: 1, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !rows.closed || items[0].StoreID != 1 || items[0].StoreCode != "001" ||
		items[0].StoreName != "仓库一" || items[0].IsActive != "Y" || items[0].IsStop != nil ||
		items[1].IsActive != "N" || items[1].IsStop == nil || *items[1].IsStop != "N" {
		t.Fatalf("unexpected result: %#v, closed=%v", items, rows.closed)
	}
}

func TestQueryBojunStoresRejectsInvalidQueries(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("invalid query reached database")
		return nil, nil
	})
	for _, query := range []BojunStoreQuery{
		{PageSize: 0}, {PageSize: 101}, {PageSize: 1, Offset: -1},
		{PageSize: 1, IsActive: "all"}, {PageSize: 1, IsStop: "ALL"},
		{PageSize: 1, StoreCode: strings.Repeat("a", 81)}, {PageSize: 1, NamePrefix: strings.Repeat("a", 256)},
	} {
		if _, err := adapter.QueryBojunStores(t.Context(), query); err == nil {
			t.Fatalf("invalid query succeeded: %#v", query)
		}
	}
	if _, err := adapter.QueryBojunStores(nil, BojunStoreQuery{PageSize: 1}); err == nil {
		t.Fatal("nil context succeeded")
	}
	if _, err := (*Adapter)(nil).QueryBojunStores(t.Context(), BojunStoreQuery{PageSize: 1}); err == nil {
		t.Fatal("closed adapter succeeded")
	}
}

func TestBojunQueriesReturnEmptyListsAndPropagateDatabaseErrors(t *testing.T) {
	for _, operation := range []struct {
		name    string
		columns []string
		valid   []driver.Value
		run     func(*Adapter) (int, bool, error)
	}{
		{"stores", []string{"ID", "CODE", "NAME", "ISACTIVE", "ISSTOP"}, []driver.Value{int64(1), "001", "仓库", "Y", nil},
			func(adapter *Adapter) (int, bool, error) {
				items, err := adapter.QueryBojunStores(t.Context(), BojunStoreQuery{PageSize: 50})
				return len(items), items == nil, err
			}},
		{"stocks", make([]string, 28), make([]driver.Value, 28),
			func(adapter *Adapter) (int, bool, error) {
				items, err := adapter.QueryBojunStocks(t.Context(), BojunStockQuery{ProductCode: []string{"SKU"}, StoreCode: "001", PageSize: 50})
				return len(items), items == nil, err
			}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for _, failure := range []string{"empty", "query", "scan", "iteration", "close"} {
				t.Run(failure, func(t *testing.T) {
					failureErr := errors.New(failure + " failed")
					rows := &bojunQueryTestRows{columns: operation.columns}
					if failure != "empty" {
						rows.values = [][]driver.Value{operation.valid}
					}
					switch failure {
					case "scan":
						rows.columns = []string{"wrong column count"}
					case "iteration":
						rows.nextErr = failureErr
					case "close":
						rows.closeErr = failureErr
					}
					adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
						if failure == "query" {
							return nil, failureErr
						}
						return rows, nil
					})
					length, isNil, err := operation.run(adapter)
					if failure == "empty" {
						if err != nil || length != 0 || isNil {
							t.Fatalf("empty result: len=%d nil=%v err=%v", length, isNil, err)
						}
					} else if err == nil || !isNil || (failure != "scan" && !errors.Is(err, failureErr)) {
						t.Fatalf("error or partial result was lost: len=%d nil=%v err=%v", length, isNil, err)
					}
					if failure != "query" && !rows.closed {
						t.Fatal("rows were not closed")
					}
				})
			}
		})
	}
}
