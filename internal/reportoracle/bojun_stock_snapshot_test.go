package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/godror/godror"
)

type bojunSnapshotTestState struct {
	t                   *testing.T
	connections         int
	sessions            []int
	queries, executions int
	values              [][]driver.Value
	message             string
	queryArguments      []any
	selectors           [3]string
}

type bojunSnapshotTestConnector struct{ state *bojunSnapshotTestState }

func (connector bojunSnapshotTestConnector) Connect(context.Context) (driver.Conn, error) {
	connector.state.connections++
	return &bojunSnapshotTestConnection{state: connector.state, session: connector.state.connections}, nil
}
func (connector bojunSnapshotTestConnector) Driver() driver.Driver {
	return bojunSnapshotTestDriver{connector}
}

type bojunSnapshotTestDriver struct{ connector bojunSnapshotTestConnector }

func (testDriver bojunSnapshotTestDriver) Open(string) (driver.Conn, error) {
	return testDriver.connector.Connect(context.Background())
}

type bojunSnapshotTestConnection struct {
	state   *bojunSnapshotTestState
	session int
}

func (*bojunSnapshotTestConnection) Prepare(string) (driver.Stmt, error)      { return nil, driver.ErrSkip }
func (*bojunSnapshotTestConnection) Close() error                             { return nil }
func (*bojunSnapshotTestConnection) Begin() (driver.Tx, error)                { return nil, driver.ErrSkip }
func (*bojunSnapshotTestConnection) CheckNamedValue(*driver.NamedValue) error { return nil }

func (connection *bojunSnapshotTestConnection) QueryContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	state := connection.state
	state.sessions = append(state.sessions, connection.session)
	state.queries++
	upper := strings.ToUpper(statement)
	for _, table := range []string{"C_STORE@LINK_TO_BOJUN", "M_PRODUCT@LINK_TO_BOJUN", "M_PRODUCT_COLOR@LINK_TO_BOJUN"} {
		if !strings.Contains(upper, table) {
			state.t.Fatalf("snapshot query is missing remote source %s: %s", table, statement)
		}
	}
	if state.executions != 1 || state.queries != 1 || !strings.Contains(upper, "BOSNDS3.YL_RP_NC_HISSTORAGE01@LINK_TO_BOJUN") ||
		!strings.Contains(upper, "M_PRODUCT_ALIAS@LINK_TO_BOJUN") || strings.Contains(upper, "SELECT *") {
		state.t.Fatalf("result must read the generated remote snapshot with explicit columns: %s", statement)
	}
	options := 0
	var bound []any
	for _, arg := range args {
		if option, ok := arg.Value.(godror.Option); ok && option != nil {
			options++
		} else {
			bound = append(bound, arg.Value)
		}
	}
	if options == 0 || state.queryArguments != nil && !reflect.DeepEqual(bound, state.queryArguments) {
		state.t.Fatalf("incorrect numeric result bindings/options: bound=%#v want=%#v options=%d", bound, state.queryArguments, options)
	}
	placeholders := regexp.MustCompile(`:[0-9]+`).FindAllString(statement, -1)
	if len(placeholders) != len(bound) {
		state.t.Fatalf("bind count mismatch: placeholders=%v arguments=%#v", placeholders, bound)
	}
	for index, placeholder := range placeholders {
		if placeholder != fmt.Sprintf(":%d", index+1) {
			state.t.Fatalf("result bindings are not in occurrence order: %v", placeholders)
		}
	}
	return &bojunQueryTestRows{columns: make([]string, 10), values: state.values}, nil
}

func (connection *bojunSnapshotTestConnection) ExecContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	state := connection.state
	state.sessions = append(state.sessions, connection.session)
	state.executions++
	if state.queries != 0 || state.executions != 1 || len(args) != 5 || !strings.Contains(strings.ToUpper(statement), "YL_DBS.RP_NC_HISSTORAGE_GENERATE") {
		state.t.Fatalf("unexpected procedure call order or target: %s", statement)
	}
	if args[0].Value != 20261026 {
		state.t.Fatalf("incorrect procedure date: %#v", args[0])
	}
	selection := regexp.MustCompile(`^SELECT ID FROM (C_STORE|M_PRODUCT|M_PRODUCT_COLOR) WHERE ID IN\s*\(\s*[0-9]+(\s*,\s*[0-9]+)*\s*\)$`)
	tables := make(map[string]bool)
	output := false
	for _, arg := range args {
		switch value := arg.Value.(type) {
		case string:
			normalized := strings.ToUpper(strings.Join(strings.Fields(value), " "))
			match := selection.FindStringSubmatch(normalized)
			if len(match) == 0 {
				state.t.Fatalf("procedure selector must contain only SQL over validated numeric IDs: %q", value)
			}
			tables[match[1]] = true
			if arg.Ordinal >= 2 && arg.Ordinal <= 4 && state.selectors[arg.Ordinal-2] != "" && normalized != state.selectors[arg.Ordinal-2] {
				state.t.Fatalf("selector ID set changed: got=%q want=%q", normalized, state.selectors[arg.Ordinal-2])
			}
		case sql.Out:
			output = true
			switch destination := value.Dest.(type) {
			case *string:
				*destination = state.message
			case *sql.NullString:
				*destination = sql.NullString{String: state.message, Valid: state.message != ""}
			default:
				state.t.Fatalf("unsupported R_MESSAGE output destination: %T", destination)
			}
		}
	}
	if len(tables) != 3 || !output {
		state.t.Fatalf("missing selector or R_MESSAGE output binding: %#v", args)
	}
	return driver.RowsAffected(1), nil
}

func newBojunSnapshotTestAdapter(t *testing.T, state *bojunSnapshotTestState) *Adapter {
	t.Helper()
	state.t = t
	db := sql.OpenDB(bojunSnapshotTestConnector{state})
	// Releasing a session between calls forces a different connection, exposing
	// implementations that accidentally use pool-level Exec/Query separately.
	db.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = db.Close() })
	return &Adapter{db: db, prefetchRows: 1000, fetchArraySize: 1000}
}

func TestQueryBojunStockSnapshotUsesOneSessionAndNumericProcedureSelectors(t *testing.T) {
	state := &bojunSnapshotTestState{
		message: "Y", queryArguments: []any{int64(10), int64(21), int64(20), int64(31), int64(30), 20261026},
		selectors: [3]string{"SELECT ID FROM C_STORE WHERE ID IN (10)", "SELECT ID FROM M_PRODUCT WHERE ID IN (20,21)", "SELECT ID FROM M_PRODUCT_COLOR WHERE ID IN (30,31)"},
		values:    [][]driver.Value{{int64(20261026), "001", "门店", "SKU", "款号", "红色", "9007199254740993.1234567890", "-12.5000000001", nil, "0"}},
	}
	query := validBojunSnapshotTestQuery()
	query.ProductID, query.ProductColorID = []int64{21, 20, 21}, []int64{31, 30, 31}
	items, err := newBojunSnapshotTestAdapter(t, state).QueryBojunStockSnapshot(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || state.connections != 1 || state.queries != 1 || state.executions != 1 || fmt.Sprint(state.sessions) != "[1 1]" {
		t.Fatalf("unexpected results or session use: items=%#v state=%+v", items, state)
	}
	item := items[0]
	if item.ChangeDate != 20261026 || item.StoreCode != "001" || item.ProductCode != "SKU" ||
		item.StoreName == nil || *item.StoreName != "门店" || item.ProductName == nil || *item.ProductName != "款号" ||
		item.ProductColor == nil || *item.ProductColor != "红色" ||
		item.PriceList == nil || *item.PriceList != "9007199254740993.1234567890" ||
		item.Total == nil || *item.Total != "-12.5000000001" || item.AmtList != nil ||
		item.QtyPreIn == nil || *item.QtyPreIn != "0" {
		t.Fatalf("snapshot identifiers, decimal precision, NULL or zero changed: %#v", item)
	}
}

func validBojunSnapshotTestQuery() BojunStockSnapshotQuery {
	return BojunStockSnapshotQuery{ChangeDate: 20261026, StoreID: 10, ProductID: []int64{20}, ProductColorID: []int64{30}}
}

func TestQueryBojunStockSnapshotProcedureFailureDoesNotReadResult(t *testing.T) {
	for _, message := range []string{"false,ORA-20000: generation failed", "", "N"} {
		t.Run(message, func(t *testing.T) {
			state := &bojunSnapshotTestState{message: message}
			items, err := newBojunSnapshotTestAdapter(t, state).QueryBojunStockSnapshot(t.Context(), validBojunSnapshotTestQuery())
			if err == nil || len(items) != 0 || state.queries != 0 || state.executions != 1 {
				t.Fatalf("failed procedure exposed stale results: items=%#v error=%v state=%+v", items, err, state)
			}
		})
	}
}

func TestQueryBojunStockSnapshotRejectsInvalidIDsAndDatesBeforeConnecting(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*BojunStockSnapshotQuery)
	}{
		{"invalid date", func(q *BojunStockSnapshotQuery) { q.ChangeDate = 20260229 }},
		{"zero date", func(q *BojunStockSnapshotQuery) { q.ChangeDate = 0 }},
		{"zero store", func(q *BojunStockSnapshotQuery) { q.StoreID = 0 }},
		{"negative store", func(q *BojunStockSnapshotQuery) { q.StoreID = -1 }},
		{"large store", func(q *BojunStockSnapshotQuery) { q.StoreID = 10000000000 }},
		{"empty products", func(q *BojunStockSnapshotQuery) { q.ProductID = nil }},
		{"empty colors", func(q *BojunStockSnapshotQuery) { q.ProductColorID = []int64{} }},
		{"zero product", func(q *BojunStockSnapshotQuery) { q.ProductID = []int64{0} }},
		{"negative product", func(q *BojunStockSnapshotQuery) { q.ProductID = []int64{-1} }},
		{"large product", func(q *BojunStockSnapshotQuery) { q.ProductID = []int64{10000000000} }},
		{"zero color", func(q *BojunStockSnapshotQuery) { q.ProductColorID = []int64{0} }},
		{"negative color", func(q *BojunStockSnapshotQuery) { q.ProductColorID = []int64{-1} }},
		{"large color", func(q *BojunStockSnapshotQuery) { q.ProductColorID = []int64{10000000000} }},
		{"too many products", func(q *BojunStockSnapshotQuery) { q.ProductID = repeatBojunSnapshotTestID(101) }},
		{"too many colors", func(q *BojunStockSnapshotQuery) { q.ProductColorID = repeatBojunSnapshotTestID(101) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &bojunSnapshotTestState{message: "Y"}
			query := validBojunSnapshotTestQuery()
			test.mutate(&query)
			items, err := newBojunSnapshotTestAdapter(t, state).QueryBojunStockSnapshot(t.Context(), query)
			if err == nil || len(items) != 0 || state.connections != 0 || state.executions != 0 || state.queries != 0 {
				t.Fatalf("invalid query reached Oracle: items=%#v error=%v state=%+v", items, err, state)
			}
		})
	}
}

func repeatBojunSnapshotTestID(count int) []int64 {
	values := make([]int64, count)
	for index := range values {
		values[index] = 1
	}
	return values
}

func TestQueryBojunStockSnapshotAcceptsIDLimitsAndEmptyResults(t *testing.T) {
	query := validBojunSnapshotTestQuery()
	query.StoreID, query.ProductID, query.ProductColorID = 9999999999, repeatBojunSnapshotTestID(100), []int64{9999999999}
	state := &bojunSnapshotTestState{message: "Y", queryArguments: []any{int64(9999999999), int64(1), int64(9999999999), 20261026}}
	items, err := newBojunSnapshotTestAdapter(t, state).QueryBojunStockSnapshot(t.Context(), query)
	if err != nil || items == nil || len(items) != 0 || state.executions != 1 || state.queries != 1 {
		t.Fatalf("valid limits or empty result failed: items=%#v error=%v state=%+v", items, err, state)
	}
}
