package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTransactionWithBeginRetryRecoversBeforeCallback(t *testing.T) {
	state := &beginRetryTestState{beginErrors: []error{mysqlDriver.ErrInvalidConn}}
	db := newBeginRetryTestDB(t, state)
	calls := 0
	err := TransactionWithBeginRetry(db, func(tx *gorm.DB) error {
		calls++
		return tx.Exec("UPDATE claims SET version = version + 1").Error
	})
	if err != nil || state.begins != 2 || calls != 1 || state.writes != 1 || state.commits != 1 || state.connections != 2 {
		t.Fatalf("error=%v callback=%d state=%+v", err, calls, state)
	}
	if applicationAvailability.state.Load() != databaseStateClosed {
		t.Fatal("recovered BEGIN must not leave the application availability gate open")
	}
}

func TestTransactionWithBeginRetryNeverReplaysStartedWork(t *testing.T) {
	for _, test := range []struct {
		name, stage                                    string
		beginErrors                                    []error
		writeError, commitError                        error
		wantBegins, wantCalls, wantWrites, wantCommits int
	}{
		{name: "persistent begin failure", stage: "begin", beginErrors: []error{mysqlDriver.ErrInvalidConn, mysqlDriver.ErrInvalidConn}, wantBegins: 2},
		{name: "write response lost", stage: "body", writeError: mysqlDriver.ErrInvalidConn, wantBegins: 1, wantCalls: 1, wantWrites: 1},
		{name: "commit response lost", stage: "commit", commitError: mysqlDriver.ErrInvalidConn, wantBegins: 1, wantCalls: 1, wantWrites: 1, wantCommits: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &beginRetryTestState{beginErrors: test.beginErrors, writeError: test.writeError, commitError: test.commitError}
			db := newBeginRetryTestDB(t, state)
			calls := 0
			err := TransactionWithBeginRetry(db, func(tx *gorm.DB) error {
				calls++
				return tx.Exec("UPDATE claims SET version = version + 1").Error
			})
			if !errors.Is(err, mysqlDriver.ErrInvalidConn) || !strings.Contains(err.Error(), test.stage) || state.begins != test.wantBegins || calls != test.wantCalls || state.writes != test.wantWrites || state.commits != test.wantCommits {
				t.Fatalf("error=%v callback=%d state=%+v", err, calls, state)
			}
		})
	}
}

func TestTransactionWithBeginRetryDoesNotRetryOtherBeginErrors(t *testing.T) {
	for _, cause := range []error{&mysqlDriver.MySQLError{Number: 1040, Message: "too many connections"}, context.DeadlineExceeded} {
		state := &beginRetryTestState{beginErrors: []error{cause}}
		db := newBeginRetryTestDB(t, state)
		calls := 0
		err := TransactionWithBeginRetry(db, func(*gorm.DB) error { calls++; return nil })
		if !errors.Is(err, cause) || state.begins != 1 || calls != 0 {
			t.Fatalf("error=%v callback=%d state=%+v", err, calls, state)
		}
	}
}

func TestTransactionWithBeginRetryCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := &beginRetryTestState{beginErrors: []error{mysqlDriver.ErrInvalidConn}, afterBegin: cancel}
	db := newBeginRetryTestDB(t, state).WithContext(ctx)
	calls := 0
	err := TransactionWithBeginRetry(db, func(*gorm.DB) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || state.begins != 1 || calls != 0 {
		t.Fatalf("error=%v callback=%d state=%+v", err, calls, state)
	}
}

func TestTransactionWithBeginRetryPreservesExternalTransaction(t *testing.T) {
	for _, failSavepoint := range []bool{false, true} {
		state := &beginRetryTestState{}
		if failSavepoint {
			state.savepointError = mysqlDriver.ErrInvalidConn
		}
		db := newBeginRetryTestDB(t, state)
		outer := db.Begin()
		if outer.Error != nil {
			t.Fatal(outer.Error)
		}
		calls := 0
		err := TransactionWithBeginRetry(outer, func(tx *gorm.DB) error {
			calls++
			return tx.Exec("UPDATE claims SET version = version + 1").Error
		})
		if state.begins != 1 || state.savepoints != 1 || state.commits != 0 {
			t.Fatalf("external transaction was restarted or committed: error=%v state=%+v", err, state)
		}
		if failSavepoint {
			if !errors.Is(err, mysqlDriver.ErrInvalidConn) || calls != 0 {
				t.Fatalf("error=%v callback=%d", err, calls)
			}
		} else if err != nil || calls != 1 || state.writes != 1 {
			t.Fatalf("error=%v callback=%d state=%+v", err, calls, state)
		}
		_ = outer.Rollback()
	}
}

type beginRetryTestState struct {
	beginErrors                                                 []error
	writeError, commitError, savepointError                     error
	afterBegin                                                  func()
	begins, writes, commits, rollbacks, connections, savepoints int
}

func newBeginRetryTestDB(t *testing.T, state *beginRetryTestState) *gorm.DB {
	t.Helper()
	original := applicationAvailability
	applicationAvailability = newAvailabilityGate()
	t.Cleanup(func() { applicationAvailability = original })
	sqlDB := sql.OpenDB(beginRetryTestConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db.WithContext(t.Context())
}

type beginRetryTestConnector struct{ state *beginRetryTestState }

func (c beginRetryTestConnector) Connect(context.Context) (driver.Conn, error) {
	c.state.connections++
	return &beginRetryTestConn{state: c.state, valid: true}, nil
}
func (beginRetryTestConnector) Driver() driver.Driver { return beginRetryTestDriver{} }

type beginRetryTestDriver struct{}

func (beginRetryTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use test connector")
}

type beginRetryTestConn struct {
	state *beginRetryTestState
	valid bool
}

func (c *beginRetryTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *beginRetryTestConn) Close() error                        { c.valid = false; return nil }
func (c *beginRetryTestConn) IsValid() bool                       { return c.valid }
func (c *beginRetryTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *beginRetryTestConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.begins++
	if c.state.afterBegin != nil {
		c.state.afterBegin()
	}
	if c.state.begins <= len(c.state.beginErrors) {
		err := c.state.beginErrors[c.state.begins-1]
		if errors.Is(err, mysqlDriver.ErrInvalidConn) {
			c.valid = false
		}
		return nil, err
	}
	return beginRetryTestTx{c.state}, nil
}
func (c *beginRetryTestConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(query, "SAVEPOINT ") {
		c.state.savepoints++
		return driver.RowsAffected(0), c.state.savepointError
	}
	if strings.HasPrefix(query, "ROLLBACK TO SAVEPOINT ") {
		return driver.RowsAffected(0), nil
	}
	c.state.writes++
	return driver.RowsAffected(1), c.state.writeError
}

type beginRetryTestTx struct{ state *beginRetryTestState }

func (tx beginRetryTestTx) Commit() error   { tx.state.commits++; return tx.state.commitError }
func (tx beginRetryTestTx) Rollback() error { tx.state.rollbacks++; return nil }
