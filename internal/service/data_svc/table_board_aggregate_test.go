package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/tableboard"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func boardTestMetric() tableboard.Metric {
	return tableboard.Metric{ID: "gmv", Name: "GMV", Aggregate: "SUM", DatasourceID: 1, Owner: "OPS", TableName: "ORDERS", ValueField: "AMOUNT", TimeField: "PAID_AT", Filters: []tableboard.Filter{{Field: "STATUS", Op: "EQ", Value: "PAID"}}, Dimensions: []string{"STORE"}}
}
func boardTestRelation() boardRelation {
	rel := boardRelation{from: "board_orders"}
	for name, typ := range map[string]string{"AMOUNT": "NUMBER", "PAID_AT": "DATE", "CREATED_AT": "TIMESTAMP(6)", "STATUS": "VARCHAR2", "STORE": "VARCHAR2", "ORDER_NO": "VARCHAR2", "BILLDAY": "VARCHAR2"} {
		rel.schema = append(rel.schema, reportoracle.ResultColumn{Name: name, DataType: typ})
	}
	return rel
}

func TestBoardAggregateFieldValidation(t *testing.T) {
	m := boardTestMetric()
	rel := boardTestRelation()
	for _, field := range []string{"STORE", "DELETED"} {
		m.ValueField = field
		if err := validateMetricFields(m, rel); err == nil {
			t.Fatal("invalid sum field", field)
		}
	}
	m = boardTestMetric()
	m.TimeField = "BILLDAY"
	if err := validateMetricFields(m, rel); err == nil {
		t.Fatal("implicit encoded date allowed")
	}
	m.TimeFormat = "YYYYMMDD"
	if err := validateMetricFields(m, rel); err != nil {
		t.Fatal(err)
	}
	expr := boardDateExpression(m, rel)
	if expr != `TO_DATE("BILLDAY", 'FXYYYYMMDD')` {
		t.Fatal(expr)
	}
}

func TestMySQLBoardAggregateAcceptance(t *testing.T) {
	dsn := os.Getenv("TABLE_BOARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set TABLE_BOARD_TEST_DSN for aggregate integration")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("dedicated test database required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SET SESSION sql_mode='ANSI_QUOTES'")
	exec("CREATE TEMPORARY TABLE board_orders(ORDER_NO VARCHAR(32), AMOUNT INT, STATUS VARCHAR(10), PAID_AT DATETIME, CREATED_AT DATETIME, STORE VARCHAR(12))")
	exec("INSERT INTO board_orders VALUES ('X',6280,'PAID','2026-11-05','2026-10-31','0007'), ('Y',3000,'PAID','2026-11-06','2026-11-05','0007'), ('Y',3120,'PAID','2026-11-06','2026-11-05','0012'), ('Z',NULL,'PAID','2026-11-06','2026-11-05','nulls'), ('NO',9000,'UNPAID','2026-11-06','2026-11-05','0007')")
	for i := range 150 {
		exec("INSERT INTO board_orders VALUES (?,1,'PAID','2027-01-01 01:00','2027-01-01','0007')", fmt.Sprint(i))
	}
	m := boardTestMetric()
	c := tableboard.Cell{Kind: "formula", MetricID: m.ID, Period: "MONTH_TO_DATE"}
	changed := c
	changed.Overrides = map[string]json.RawMessage{"timeField": json.RawMessage(`"CREATED_AT"`)}
	count := c
	count.Overrides = map[string]json.RawMessage{"aggregate": json.RawMessage(`"COUNT_DISTINCT"`), "valueField": json.RawMessage(`"ORDER_NO"`)}
	shop := func(s string) tableboard.Cell {
		cell := c
		cell.Filters = []tableboard.Filter{{Field: "STORE", Op: "EQ", Value: s}}
		return cell
	}
	offline := m
	offline.ID = "offline"
	offline.DatasourceID = 2
	bad := c
	bad.MetricID = offline.ID
	layout := tableboard.Layout{Metrics: []tableboard.Metric{m, offline}, Sections: []tableboard.Section{{ID: "s", Cells: [][]tableboard.Cell{{c, changed, count, c, shop("0007"), shop("0012"), shop("missing"), shop("nulls"), bad}}}}}
	// Retain the generated aggregate, predicates, and binds. Only Oracle's
	// timestamp constructor and named placeholder syntax need adapting to MySQL.
	timestamp := regexp.MustCompile(`TO_TIMESTAMP\((:board[0-9]+), 'YYYY-MM-DD HH24:MI:SS.FF9'\)`)
	bind := regexp.MustCompile(`:board[0-9]+`)
	var calls atomic.Int32
	run := func(ctx context.Context, _ uint, r aggregateRequest, asOf time.Time) (map[string]BoardCellResult, error) {
		calls.Add(1)
		if r.metric.DatasourceID == 2 {
			return nil, errors.New("offline")
		}
		start, end, e := r.metric.Bounds(r.period, asOf)
		if e != nil {
			return nil, e
		}
		rel := boardTestRelation()
		statement, args, e := compileBoardAggregate(r, rel, start, end)
		if e != nil {
			return nil, e
		}
		if !reportoracle.ValidateSelect(statement) {
			return nil, fmt.Errorf("Oracle SELECT validator rejected %s", statement)
		}
		statement = timestamp.ReplaceAllString(statement, "${1}")
		vals := map[string]any{}
		for _, a := range args {
			named := a.(sql.NamedArg)
			vals[":"+named.Name] = named.Value
		}
		var params []any
		statement = bind.ReplaceAllStringFunc(statement, func(b string) string { params = append(params, vals[b]); return "?" })
		rows, e := conn.QueryContext(ctx, statement, params...)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		return readBoardAggregate(rows, r, rel, start, end)
	}
	asOf := time.Date(2026, 11, 26, 4, 0, 0, 0, time.UTC)
	rows := evaluateBoard(ctx, 1, layout, asOf, run, nil)
	for i, expected := range []string{"12400", "6120", "3", "12400", "9280", "3120", "0"} {
		if rows[i].Status != "ok" || rows[i].Value == nil || *rows[i].Value != expected {
			t.Fatalf("cell %d: %+v, want %s", i, rows[i], expected)
		}
	}
	if rows[0].End != rows[1].End || rows[7].Status != "null" || rows[8].Status != "error" || calls.Load() != 5 {
		t.Fatal(rows, calls.Load())
	}
	c.Period = "YEAR_TO_DATE"
	layout.Sections[0].Cells = [][]tableboard.Cell{{c}}
	rows = evaluateBoard(ctx, 1, layout, time.Date(2027, 1, 1, 2, 0, 0, 0, time.UTC), run, nil)
	if rows[0].Value == nil || *rows[0].Value != "150" || rows[0].Matched != 150 || rows[0].Start != "2027-01-01T00:00:00+08:00" {
		t.Fatal(rows)
	}
}

func TestBoardScopeValidationOnExplicitStore(t *testing.T) {
	rel := boardTestRelation()
	rel.scopeField = "STORE"
	rel.scopeValues = map[string]bool{"0007": true}
	for _, tc := range []struct {
		value string
		ok    bool
	}{{"0007", true}, {"7", false}, {"0012", false}} {
		_, err := boardFilters(rel, []tableboard.Filter{{Field: "STORE", Op: "EQ", Value: tc.value}})
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
}
