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

	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQimaiCreateLocksRawRecordAndPreservesUnicode(t *testing.T) {
	store := &qimaiCreateTestStore{rawExists: true}
	dao := &QimaiDataDAO{db: newQimaiCreateTestDB(t, store)}
	row := &model.QIMAI_ORDER_DATA{RawDataID: 240056, OrderNo: "order-240056", ContactName: "客人🍭"}
	id, err := dao.Create(t.Context(), row)
	if err != nil || id != 77 || row.ID != id || store.inserts != 1 || store.commits != 1 {
		t.Fatalf("create id=%d row_id=%d error=%v inserts=%d commits=%d", id, row.ID, err, store.inserts, store.commits)
	}
	if store.contactName != row.ContactName {
		t.Fatalf("bound contact name=%q, want %q", store.contactName, row.ContactName)
	}
}

func TestQimaiCreateReusesExistingRawRecordWithoutUpdatingSynced(t *testing.T) {
	store := &qimaiCreateTestStore{rawExists: true, existingID: 77}
	dao := &QimaiDataDAO{db: newQimaiCreateTestDB(t, store)}
	row := &model.QIMAI_ORDER_DATA{RawDataID: 240056, OrderNo: "order-240056", Synced: 0}
	id, err := dao.Create(t.Context(), row)
	if err != nil || id != 77 || row.ID != id || store.inserts != 0 || store.commits != 1 {
		t.Fatalf("repeat create id=%d row_id=%d error=%v inserts=%d commits=%d", id, row.ID, err, store.inserts, store.commits)
	}
	// The driver rejects UPDATE statements, so an existing synced value cannot
	// be overwritten by the default zero in this replayed input.
}

func TestQimaiCreateRollsBackAndReturnsCause(t *testing.T) {
	writeErr := errors.New("Error 1366: Incorrect string value")
	for _, test := range []struct {
		name  string
		store qimaiCreateTestStore
		want  error
	}{
		{name: "missing raw record", store: qimaiCreateTestStore{}, want: gorm.ErrRecordNotFound},
		{name: "insert failed", store: qimaiCreateTestStore{rawExists: true, writeErr: writeErr}, want: writeErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			dao := &QimaiDataDAO{db: newQimaiCreateTestDB(t, &test.store)}
			id, err := dao.Create(t.Context(), &model.QIMAI_ORDER_DATA{RawDataID: 240056, OrderNo: "order-240056"})
			if !errors.Is(err, test.want) || id != 0 || test.store.commits != 0 || test.store.rollbacks != 1 {
				t.Fatalf("failed create id=%d err=%v commits=%d rollbacks=%d", id, err, test.store.commits, test.store.rollbacks)
			}
		})
	}
}

type qimaiCreateTestStore struct {
	rawExists   bool
	existingID  int64
	writeErr    error
	inserts     int
	commits     int
	rollbacks   int
	contactName string
}

func newQimaiCreateTestDB(t *testing.T, store *qimaiCreateTestStore) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(qimaiCreateTestConnector{store: store})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type qimaiCreateTestConnector struct{ store *qimaiCreateTestStore }

func (connector qimaiCreateTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &qimaiCreateTestConn{store: connector.store}, nil
}
func (qimaiCreateTestConnector) Driver() driver.Driver { return qimaiCreateTestDriver{} }

type qimaiCreateTestDriver struct{}

func (qimaiCreateTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use qimai test connector")
}

type qimaiCreateTestConn struct {
	store     *qimaiCreateTestStore
	inTx      bool
	rawLocked bool
	orderRead bool
}

func (*qimaiCreateTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*qimaiCreateTestConn) Close() error                        { return nil }
func (conn *qimaiCreateTestConn) Begin() (driver.Tx, error) {
	conn.inTx = true
	return &qimaiCreateTestTx{conn: conn}, nil
}

func (conn *qimaiCreateTestConn) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !conn.inTx || len(args) != 1 || args[0].Value != int64(240056) {
		return nil, fmt.Errorf("expected transactional read with bound raw_data_id: %s %+v", statement, args)
	}
	switch {
	case strings.Contains(statement, "FROM `raw_data`"):
		if !strings.Contains(statement, "FOR UPDATE") {
			return nil, errors.New("raw record must be locked before deduplication")
		}
		conn.rawLocked = true
		if !conn.store.rawExists {
			return &qimaiCreateTestRows{}, nil
		}
		return &qimaiCreateTestRows{id: 240056}, nil
	case strings.Contains(statement, "FROM `qimai_order_data`"):
		if !conn.rawLocked || !strings.Contains(statement, "raw_data_id") {
			return nil, errors.New("order deduplication must follow the raw record lock")
		}
		conn.orderRead = true
		return &qimaiCreateTestRows{id: conn.store.existingID}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", statement)
	}
}

func (conn *qimaiCreateTestConn) ExecContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !conn.inTx || !conn.rawLocked || !conn.orderRead {
		return nil, errors.New("insert must follow raw row lock and duplicate check in the same transaction")
	}
	if !strings.HasPrefix(statement, "INSERT INTO `qimai_order_data`") {
		return nil, fmt.Errorf("unexpected write (existing synced orders must not change): %s", statement)
	}
	if conn.store.writeErr != nil {
		return nil, conn.store.writeErr
	}
	start, end := strings.Index(statement, "("), strings.Index(statement, ")")
	for index, column := range strings.Split(statement[start+1:end], ",") {
		if strings.Trim(column, "` ") == "contact_name" {
			conn.store.contactName, _ = args[index].Value.(string)
		}
	}
	conn.store.inserts++
	return qimaiCreateTestResult{}, nil
}

type qimaiCreateTestTx struct{ conn *qimaiCreateTestConn }

func (tx *qimaiCreateTestTx) Commit() error {
	tx.conn.inTx = false
	tx.conn.store.commits++
	return nil
}
func (tx *qimaiCreateTestTx) Rollback() error {
	tx.conn.inTx = false
	tx.conn.store.rollbacks++
	return nil
}

type qimaiCreateTestResult struct{}

func (qimaiCreateTestResult) LastInsertId() (int64, error) { return 77, nil }
func (qimaiCreateTestResult) RowsAffected() (int64, error) { return 1, nil }

type qimaiCreateTestRows struct {
	id   int64
	done bool
}

func (*qimaiCreateTestRows) Columns() []string { return []string{"id"} }
func (*qimaiCreateTestRows) Close() error      { return nil }
func (rows *qimaiCreateTestRows) Next(values []driver.Value) error {
	if rows.done || rows.id == 0 {
		return io.EOF
	}
	rows.done = true
	values[0] = rows.id
	return nil
}
