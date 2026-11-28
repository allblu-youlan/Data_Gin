package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTableEmailScanRecoversOnlyBeforeTransactionBody(t *testing.T) {
	for _, test := range []struct {
		name, failure, phase                          string
		begins, queries, oracleCalls, writes, commits int
	}{
		{name: "BEGIN recovery", failure: "begin_once", begins: 3, queries: 2, oracleCalls: 1, writes: 3, commits: 2},
		{name: "persistent BEGIN failure", failure: "begin", phase: "phase=begin", begins: 2},
		{name: "locked rule query failure", failure: "query", phase: "claim due rule", begins: 1, queries: 1},
		{name: "delivery write failure", failure: "table_email_deliveries", phase: "create delivery snapshot", begins: 1, queries: 1, oracleCalls: 1, writes: 1},
		{name: "outbox write failure", failure: "async_job_outbox", phase: "create delivery outbox", begins: 1, queries: 1, oracleCalls: 1, writes: 2},
		{name: "cursor write failure", failure: "table_email_rules", phase: "save scan cursor", begins: 1, queries: 1, oracleCalls: 1, writes: 3},
		{name: "COMMIT response lost", failure: "commit", phase: "phase=commit", begins: 1, queries: 1, oracleCalls: 1, writes: 3, commits: 1},
		{name: "no due rules", failure: "empty", begins: 1, queries: 1, commits: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &tableEmailScanConnectionState{failure: test.failure, fixture: &tableEmailSQLFixture{mode: "scan", ruleEnabled: true, checked: test.failure == "empty"}}
			service := newTableEmailScanConnectionService(t, state)
			err := service.Scan(t.Context())
			if test.phase == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, mysqlDriver.ErrInvalidConn) || !strings.Contains(err.Error(), test.phase) {
				t.Fatalf("failure phase or cause was lost: %v", err)
			}
			if state.begins != test.begins || state.queries != test.queries || state.oracleCalls != test.oracleCalls || state.writes != test.writes || state.fixture.commits != test.commits {
				t.Fatalf("scan work was skipped or replayed: state=%+v commits=%d error=%v", state, state.fixture.commits, err)
			}
			if test.failure == "begin_once" && (!state.fixture.wroteValue("11") || len(state.fixture.writes) != 3 || !strings.Contains(state.fixture.writes[0], "table_email_deliveries") || !strings.Contains(state.fixture.writes[1], "async_job_outbox") || !strings.Contains(state.fixture.writes[2], "table_email_rules")) {
				t.Fatalf("recovered scan lost atomic snapshot/outbox/cursor order: %+v", state.fixture)
			}
		})
	}
}

func TestTableEmailScanCancelledBeforeBeginDoesNotReadOracle(t *testing.T) {
	state := &tableEmailScanConnectionState{fixture: &tableEmailSQLFixture{mode: "scan", ruleEnabled: true}}
	service := newTableEmailScanConnectionService(t, state)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := service.Scan(ctx)
	if !errors.Is(err, context.Canceled) || state.begins != 0 || state.oracleCalls != 0 || state.writes != 0 {
		t.Fatalf("cancelled scan performed work: state=%+v error=%v", state, err)
	}
}

type tableEmailScanConnectionState struct {
	fixture                              *tableEmailSQLFixture
	failure                              string
	begins, queries, oracleCalls, writes int
}

func newTableEmailScanConnectionService(t *testing.T, state *tableEmailScanConnectionState) *TableEmailService {
	t.Helper()
	sqlDB := sql.OpenDB(tableEmailScanConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	oracleDB := sql.OpenDB(tableEmailConnector{&tableEmailSQLFixture{mode: "oracle"}})
	t.Cleanup(func() { _ = oracleDB.Close() })
	return &TableEmailService{db: db, now: time.Now, open: func(context.Context, uint) (tableEmailOracle, error) {
		state.oracleCalls++
		return tableEmailTestOracle{oracleDB}, nil
	}}
}

type tableEmailScanConnector struct {
	state *tableEmailScanConnectionState
}

func (c tableEmailScanConnector) Connect(context.Context) (driver.Conn, error) {
	return &tableEmailScanConn{tableEmailSQLConn: &tableEmailSQLConn{c.state.fixture}, state: c.state, valid: true}, nil
}
func (tableEmailScanConnector) Driver() driver.Driver { return tableEmailDriver{} }

type tableEmailScanConn struct {
	*tableEmailSQLConn
	state *tableEmailScanConnectionState
	valid bool
}

func (c *tableEmailScanConn) IsValid() bool { return c.valid }
func (c *tableEmailScanConn) Close() error  { c.valid = false; return nil }
func (c *tableEmailScanConn) Begin() (driver.Tx, error) {
	c.state.begins++
	if c.state.failure == "begin" || c.state.failure == "begin_once" && c.state.begins == 1 {
		c.valid = false
		return nil, mysqlDriver.ErrInvalidConn
	}
	tx, err := c.tableEmailSQLConn.Begin()
	if err != nil {
		return nil, err
	}
	return tableEmailScanTx{Tx: tx, state: c.state}, nil
}
func (c *tableEmailScanConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries++
	if c.state.failure == "query" {
		return nil, mysqlDriver.ErrInvalidConn
	}
	if !strings.Contains(query, "FOR UPDATE SKIP LOCKED") {
		return nil, errors.New("scan must lock the rule")
	}
	return c.tableEmailSQLConn.QueryContext(ctx, query, args)
}
func (c *tableEmailScanConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.writes++
	if strings.Contains(query, "`"+c.state.failure+"`") {
		return nil, mysqlDriver.ErrInvalidConn
	}
	return c.tableEmailSQLConn.ExecContext(ctx, query, args)
}

type tableEmailScanTx struct {
	driver.Tx
	state *tableEmailScanConnectionState
}

func (tx tableEmailScanTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.state.failure == "commit" {
		return mysqlDriver.ErrInvalidConn
	}
	return nil
}
