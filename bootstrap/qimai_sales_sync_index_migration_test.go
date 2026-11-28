package bootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testQimaiSalesSyncIndexVersion = "2026-11-28-qimai-sales-sync-index-v1"

func TestQimaiSalesSyncManualDDLMatchesMigration(t *testing.T) {
	content, err := os.ReadFile("../etc/mysql_qimai_sales_sync_index.sql")
	if err != nil {
		t.Fatal(err)
	}
	statement, err := buildAddQueryIndexSQL(qimaiSalesSyncQueryIndex())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), statement+";") || !strings.Contains(string(content), qimaiSalesSyncIndexMigrationVersion) {
		t.Fatal("manual script differs from the registered online index migration")
	}
}

func TestQimaiSalesSyncIndexMigrationRunsAfterOldSchemaVersions(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprintf("index already exists=%t", exists), func(t *testing.T) {
			state := &qimaiIndexMigrationState{exists: exists, lockWait: 40}
			db := newQimaiIndexMigrationDB(t, state)
			for i := 0; i < 2; i++ {
				if err := applySupplementalSchemaMigrations(db); err != nil {
					t.Fatal(err)
				}
			}
			wantDDL := 1
			if exists {
				wantDDL = 0
			}
			if state.ddl != wantDDL || state.markers != 1 || !state.applied || state.lockWait != 40 {
				t.Fatalf("old schema skipped new index or reran completed migration: %+v", state)
			}
		})
	}
}

func TestQimaiSalesSyncIndexMigrationRecordsOnlyVerifiedSuccess(t *testing.T) {
	cause := errors.New("index DDL failed")
	state := &qimaiIndexMigrationState{ddlError: cause, lockWait: 40}
	db := newQimaiIndexMigrationDB(t, state)
	err := applySupplementalSchemaMigrations(db)
	if !errors.Is(err, cause) || state.markers != 0 || state.applied || state.lockWait != 40 {
		t.Fatalf("failed migration was hidden or recorded: error=%v state=%+v", err, state)
	}
	state.ddlError = nil
	if err := applySupplementalSchemaMigrations(db); err != nil {
		t.Fatal(err)
	}
	if state.ddl != 2 || state.markers != 1 || !state.applied || state.lockWait != 40 {
		t.Fatalf("failed DDL cannot be resumed: %+v", state)
	}
}

type qimaiIndexMigrationState struct {
	exists, applied bool
	ddl, markers    int
	lockWait        int64
	ddlError        error
}

func newQimaiIndexMigrationDB(t *testing.T, state *qimaiIndexMigrationState) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(qimaiIndexMigrationConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db.WithContext(t.Context())
}

type qimaiIndexMigrationConnector struct{ state *qimaiIndexMigrationState }

func (c qimaiIndexMigrationConnector) Connect(context.Context) (driver.Conn, error) {
	return &qimaiIndexMigrationConn{c.state}, nil
}
func (qimaiIndexMigrationConnector) Driver() driver.Driver { return qimaiIndexMigrationDriver{} }

type qimaiIndexMigrationDriver struct{}

func (qimaiIndexMigrationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use test connector")
}

type qimaiIndexMigrationConn struct{ state *qimaiIndexMigrationState }

func (*qimaiIndexMigrationConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*qimaiIndexMigrationConn) Close() error                        { return nil }
func (*qimaiIndexMigrationConn) Begin() (driver.Tx, error)           { return qimaiIndexMigrationTx{}, nil }

func (c *qimaiIndexMigrationConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(query, "`app_schema_versions`"):
		// All old migrations are already applied. Only this incremental marker is absent.
		count := int64(1)
		if len(args) == 1 && args[0].Value == testQimaiSalesSyncIndexVersion && !c.state.applied {
			count = 0
		}
		return &qimaiIndexMigrationRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	case query == "SELECT @@SESSION.lock_wait_timeout":
		return &qimaiIndexMigrationRows{columns: []string{"lock_wait_timeout"}, values: [][]driver.Value{{c.state.lockWait}}}, nil
	case strings.Contains(query, "FROM information_schema.STATISTICS"):
		if len(args) != 2 || args[0].Value != "qimai_order_data" || args[1].Value != "idx_qimai_sales_sync" {
			return nil, fmt.Errorf("unexpected index inspection: %v", args)
		}
		rows := &qimaiIndexMigrationRows{columns: []string{"NON_UNIQUE", "COLUMN_NAME", "COLLATION", "INDEX_TYPE", "IS_VISIBLE", "SUB_PART"}}
		if c.state.exists {
			for _, column := range []string{"shop_code", "status", "synced"} {
				rows.values = append(rows.values, []driver.Value{int64(1), column, "A", "BTREE", "YES", nil})
			}
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (c *qimaiIndexMigrationConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case query == "SET SESSION lock_wait_timeout = 5":
		c.state.lockWait = 5
	case query == "SET SESSION lock_wait_timeout = ?":
		c.state.lockWait = args[0].Value.(int64)
	case query == "ALTER TABLE `qimai_order_data` ADD INDEX `idx_qimai_sales_sync` (`shop_code` ASC, `status` ASC, `synced` ASC), ALGORITHM=INPLACE, LOCK=NONE":
		c.state.ddl++
		if c.state.ddlError != nil {
			return nil, c.state.ddlError
		}
		c.state.exists = true
	case strings.HasPrefix(query, "INSERT INTO `app_schema_versions`"):
		if !c.state.exists || len(args) != 2 || args[0].Value != testQimaiSalesSyncIndexVersion {
			return nil, fmt.Errorf("marker written before index or for old migration: %v", args)
		}
		c.state.applied = true
		c.state.markers++
	default:
		return nil, fmt.Errorf("unexpected mutation: %s", query)
	}
	return driver.RowsAffected(1), nil
}

type qimaiIndexMigrationTx struct{}

func (qimaiIndexMigrationTx) Commit() error   { return nil }
func (qimaiIndexMigrationTx) Rollback() error { return nil }

type qimaiIndexMigrationRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *qimaiIndexMigrationRows) Columns() []string { return r.columns }
func (*qimaiIndexMigrationRows) Close() error        { return nil }
func (r *qimaiIndexMigrationRows) Next(values []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(values, r.values[0])
	r.values = r.values[1:]
	return nil
}
