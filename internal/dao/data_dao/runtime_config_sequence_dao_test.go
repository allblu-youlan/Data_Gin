package data_dao

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRuntimeConfigNextSequencePersistsAcrossDAOInstances(t *testing.T) {
	store := &runtimeSequenceTestStore{rows: make(map[string]runtimeSequenceTestRow)}
	key := "excel:oracle-import:batch"
	first := NewRuntimeConfigDAO(newRuntimeSequenceTestDB(t, store))
	if value, err := first.NextSequence(t.Context(), key); err != nil || value != 1 {
		t.Fatalf("first sequence=%d error=%v, want 1", value, err)
	}
	// A second connection pool represents another application instance. Its
	// allocation must use the committed row rather than process-local state.
	second := NewRuntimeConfigDAO(newRuntimeSequenceTestDB(t, store))
	if value, err := second.NextSequence(t.Context(), key); err != nil || value != 2 {
		t.Fatalf("second sequence=%d error=%v, want 2", value, err)
	}
	if store.rows[key].value != "2" || store.commits != 2 {
		t.Fatalf("sequence was not committed: row=%+v commits=%d", store.rows[key], store.commits)
	}
	for _, call := range store.calls {
		if strings.Contains(call.statement, key) {
			t.Fatalf("sequence key was interpolated into SQL: %s", call.statement)
		}
		if strings.HasPrefix(call.statement, "SELECT") {
			if !strings.Contains(call.statement, "FOR UPDATE") || len(call.args) == 0 || call.args[0].Value != key {
				t.Fatalf("sequence read must lock the bound key: SQL=%s args=%+v", call.statement, call.args)
			}
		}
	}
}

func TestRuntimeConfigNextSequenceDoesNotReturnAnUncommittedNumber(t *testing.T) {
	for _, failure := range []string{"write", "commit"} {
		t.Run(failure, func(t *testing.T) {
			want := errors.New("sequence " + failure + " failed")
			key := "excel:oracle-import:batch"
			store := &runtimeSequenceTestStore{rows: map[string]runtimeSequenceTestRow{key: {id: 1, value: "7"}}}
			if failure == "write" {
				store.writeErr = want
			} else {
				store.commitErr = want
			}
			dao := NewRuntimeConfigDAO(newRuntimeSequenceTestDB(t, store))
			value, err := dao.NextSequence(t.Context(), key)
			if !errors.Is(err, want) || value != 0 {
				t.Fatalf("failed allocation returned sequence=%d error=%v, want 0 and %v", value, err, want)
			}
			if store.rows[key].value != "7" || store.commits != 0 {
				t.Fatalf("failed allocation changed committed state: row=%+v commits=%d", store.rows[key], store.commits)
			}
		})
	}
}

type runtimeSequenceTestRow struct {
	id    int64
	value string
}

type runtimeSequenceTestCall struct {
	statement string
	args      []driver.NamedValue
}

type runtimeSequenceTestStore struct {
	rows      map[string]runtimeSequenceTestRow
	calls     []runtimeSequenceTestCall
	writeErr  error
	commitErr error
	commits   int
}

func newRuntimeSequenceTestDB(t *testing.T, store *runtimeSequenceTestStore) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(runtimeSequenceTestConnector{store: store})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type runtimeSequenceTestConnector struct{ store *runtimeSequenceTestStore }

func (connector runtimeSequenceTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &runtimeSequenceTestConn{store: connector.store}, nil
}
func (runtimeSequenceTestConnector) Driver() driver.Driver { return runtimeSequenceTestDriver{} }

type runtimeSequenceTestDriver struct{}

func (runtimeSequenceTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use sequence test connector")
}

type runtimeSequenceTestConn struct {
	store   *runtimeSequenceTestStore
	pending map[string]runtimeSequenceTestRow
}

func (*runtimeSequenceTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*runtimeSequenceTestConn) Close() error                        { return nil }
func (connection *runtimeSequenceTestConn) Begin() (driver.Tx, error) {
	if connection.pending != nil {
		return nil, errors.New("sequence transaction already open")
	}
	connection.pending = make(map[string]runtimeSequenceTestRow, len(connection.store.rows))
	for key, row := range connection.store.rows {
		connection.pending[key] = row
	}
	return &runtimeSequenceTestTx{connection: connection}, nil
}

func (connection *runtimeSequenceTestConn) record(statement string, args []driver.NamedValue) error {
	if connection.pending == nil {
		return errors.New("sequence statement executed outside a transaction")
	}
	connection.store.calls = append(connection.store.calls, runtimeSequenceTestCall{statement: statement, args: append([]driver.NamedValue(nil), args...)})
	return nil
}

func (connection *runtimeSequenceTestConn) ExecContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	if err := connection.record(statement, args); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(statement, "INSERT"):
		if !strings.Contains(statement, "ON DUPLICATE KEY UPDATE") || len(args) < 2 {
			return nil, fmt.Errorf("sequence initialization is not idempotent: %s", statement)
		}
		key, ok := args[0].Value.(string)
		if !ok || args[1].Value != "0" {
			return nil, fmt.Errorf("unexpected sequence initialization values: %+v", args)
		}
		row, exists := connection.pending[key]
		if !exists {
			row = runtimeSequenceTestRow{id: int64(len(connection.pending) + 1), value: "0"}
			connection.pending[key] = row
		}
		return runtimeSequenceTestResult{lastID: row.id}, nil
	case strings.HasPrefix(statement, "UPDATE"):
		if connection.store.writeErr != nil {
			return nil, connection.store.writeErr
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("unscoped sequence update: %s", statement)
		}
		for key, row := range connection.pending {
			last := args[len(args)-1].Value
			if last == key || last == row.id {
				value, ok := args[0].Value.(string)
				if !ok {
					return nil, fmt.Errorf("counter is not stored as JSON integer text: %+v", args)
				}
				row.value = value
				connection.pending[key] = row
				return runtimeSequenceTestResult{}, nil
			}
		}
	}
	return nil, fmt.Errorf("unexpected sequence statement: %s", statement)
}

func (connection *runtimeSequenceTestConn) QueryContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if err := connection.record(statement, args); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(statement, "SELECT") || !strings.Contains(statement, "FOR UPDATE") || len(args) == 0 {
		return nil, fmt.Errorf("sequence read did not lock its row: %s", statement)
	}
	key, ok := args[0].Value.(string)
	row, exists := connection.pending[key]
	if !ok || !exists {
		return nil, fmt.Errorf("sequence read did not bind the initialized key: %+v", args)
	}
	return &runtimeSequenceTestRows{row: row}, nil
}

type runtimeSequenceTestTx struct{ connection *runtimeSequenceTestConn }

func (tx *runtimeSequenceTestTx) Commit() error {
	connection := tx.connection
	defer func() { connection.pending = nil }()
	if connection.store.commitErr != nil {
		return connection.store.commitErr
	}
	connection.store.rows = connection.pending
	connection.store.commits++
	return nil
}
func (tx *runtimeSequenceTestTx) Rollback() error {
	tx.connection.pending = nil
	return nil
}

type runtimeSequenceTestResult struct{ lastID int64 }

func (result runtimeSequenceTestResult) LastInsertId() (int64, error) { return result.lastID, nil }
func (runtimeSequenceTestResult) RowsAffected() (int64, error)        { return 1, nil }

type runtimeSequenceTestRows struct {
	row  runtimeSequenceTestRow
	done bool
}

func (*runtimeSequenceTestRows) Columns() []string { return []string{"id", "config_json"} }
func (*runtimeSequenceTestRows) Close() error      { return nil }
func (rows *runtimeSequenceTestRows) Next(values []driver.Value) error {
	if rows.done {
		return io.EOF
	}
	rows.done = true
	values[0], values[1] = rows.row.id, rows.row.value
	return nil
}
