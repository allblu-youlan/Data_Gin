package data_dao

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestClaimDAOsRetryOnlyFailedBegin(t *testing.T) {
	for _, operation := range []string{"outbox", "lease"} {
		for _, failure := range []string{"begin", "update", "commit"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				state := &claimConnectionTestState{failure: failure}
				db := newClaimConnectionTestDB(t, state)
				now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
				var err error
				if operation == "outbox" {
					rows, claimErr := NewAsyncJobOutboxDAO(db).ClaimBatch(t.Context(), "worker-1", []string{"test:task"}, now, time.Minute, 10)
					err = claimErr
					if err == nil && (len(rows) != 1 || rows[0].ID != 7 || rows[0].LockedBy != "worker-1") {
						t.Fatalf("claim result=%+v", rows)
					}
					if err != nil && rows != nil {
						t.Fatalf("failed claim returned rows=%+v", rows)
					}
				} else {
					lease, acquired, claimErr := NewBojunOracleSyncStateDAO(db).AcquireLease(t.Context(), "etl01", "token-1", now, time.Minute)
					err = claimErr
					if err == nil && (!acquired || lease == nil || lease.LastRetailID != 87347 || lease.LeaseToken != "token-1") {
						t.Fatalf("lease=%+v acquired=%t", lease, acquired)
					}
					if err != nil && (lease != nil || acquired) {
						t.Fatalf("failed acquire returned lease=%+v acquired=%t", lease, acquired)
					}
				}
				wantBegins, wantCommits := 1, 1
				if failure == "begin" {
					wantBegins = 2
					if err != nil {
						t.Fatalf("failed BEGIN was not recovered: %v", err)
					}
				} else if !errors.Is(err, mysqlDriver.ErrInvalidConn) {
					t.Fatalf("lost original failure: %v", err)
				}
				if failure == "update" {
					wantCommits = 0
				}
				if state.begins != wantBegins || state.writes != 1 || state.commits != wantCommits {
					t.Fatalf("business write was skipped or replayed: failure=%s state=%+v error=%v", failure, state, err)
				}
				wantReads := 0
				if failure == "begin" {
					wantReads = 1
				}
				if operation == "lease" && state.reads != wantReads {
					t.Fatalf("lease lookup must occur only after successful commit: state=%+v", state)
				}
			})
		}
	}
}

type claimConnectionTestState struct {
	failure                        string
	begins, writes, commits, reads int
	autocommit                     bool
	affected                       int64
	updateQuery                    string
	updateArguments                []driver.NamedValue
}

func newClaimConnectionTestDB(t *testing.T, state *claimConnectionTestState) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(claimConnectionTestConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type claimConnectionTestConnector struct{ state *claimConnectionTestState }

func (c claimConnectionTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &claimConnectionTestConn{state: c.state, valid: true}, nil
}
func (claimConnectionTestConnector) Driver() driver.Driver { return claimConnectionTestDriver{} }

type claimConnectionTestDriver struct{}

func (claimConnectionTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use claim test connector")
}

type claimConnectionTestConn struct {
	state       *claimConnectionTestState
	valid, inTx bool
}

func (c *claimConnectionTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *claimConnectionTestConn) Close() error                        { c.valid = false; return nil }
func (c *claimConnectionTestConn) IsValid() bool                       { return c.valid }
func (c *claimConnectionTestConn) Begin() (driver.Tx, error) {
	c.state.begins++
	if c.state.failure == "begin" && c.state.begins == 1 {
		c.valid = false
		return nil, mysqlDriver.ErrInvalidConn
	}
	c.inTx = true
	return claimConnectionTestTx{c}, nil
}
func (c *claimConnectionTestConn) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.inTx == c.state.autocommit || !strings.HasPrefix(query, "UPDATE ") {
		return nil, errors.New("unexpected transaction mode for claim UPDATE")
	}
	c.state.writes++
	c.state.updateQuery = query
	c.state.updateArguments = append([]driver.NamedValue(nil), arguments...)
	if c.state.failure == "update" {
		return nil, mysqlDriver.ErrInvalidConn
	}
	if c.state.failure == "timeout" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.state.autocommit {
		return driver.RowsAffected(c.state.affected), nil
	}
	return driver.RowsAffected(1), nil
}
func (c *claimConnectionTestConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.reads++
	if strings.Contains(query, "`async_job_outbox`") {
		if !c.inTx || !strings.Contains(query, "FOR UPDATE SKIP LOCKED") {
			return nil, errors.New("outbox row must be locked before claiming")
		}
		return &claimConnectionTestRows{columns: []string{"id"}, values: []driver.Value{int64(7)}}, nil
	}
	if strings.Contains(query, "`bojun_oracle_sync_states`") {
		if c.inTx || c.state.commits != 1 {
			return nil, errors.New("lease lookup must remain outside the committed update")
		}
		return &claimConnectionTestRows{columns: []string{"id", "source_code", "initialized", "last_retail_id", "lease_token"}, values: []driver.Value{int64(1), "etl01", true, int64(87347), "token-1"}}, nil
	}
	return nil, errors.New("unexpected claim query")
}

type claimConnectionTestTx struct{ conn *claimConnectionTestConn }

func (tx claimConnectionTestTx) Commit() error {
	tx.conn.inTx = false
	tx.conn.state.commits++
	if tx.conn.state.failure == "commit" {
		return mysqlDriver.ErrInvalidConn
	}
	return nil
}
func (tx claimConnectionTestTx) Rollback() error { tx.conn.inTx = false; return nil }

type claimConnectionTestRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (rows *claimConnectionTestRows) Columns() []string { return rows.columns }
func (rows *claimConnectionTestRows) Close() error      { return nil }
func (rows *claimConnectionTestRows) Next(values []driver.Value) error {
	if rows.done {
		return io.EOF
	}
	rows.done = true
	copy(values, rows.values)
	return nil
}
