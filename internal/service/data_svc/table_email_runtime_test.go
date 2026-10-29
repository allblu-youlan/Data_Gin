package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/job"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Only the external SQL and SMTP boundaries are substituted. The real service,
// GORM transactions, state transitions and outbox creation run in these tests.
type tableEmailSQLFixture struct {
	mode           string
	deliveryStatus string
	ruleEnabled    bool
	failOutbox     bool
	checked        bool
	commits        int
	rollbacks      int
	writes         []string
	values         [][]driver.NamedValue
}
type tableEmailConnector struct{ fixture *tableEmailSQLFixture }

func (c tableEmailConnector) Connect(context.Context) (driver.Conn, error) {
	return &tableEmailSQLConn{fixture: c.fixture}, nil
}
func (tableEmailConnector) Driver() driver.Driver { return tableEmailDriver{} }

type tableEmailDriver struct{}

func (tableEmailDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type tableEmailSQLConn struct{ fixture *tableEmailSQLFixture }

func (*tableEmailSQLConn) Prepare(string) (driver.Stmt, error)      { return nil, errors.New("unsupported") }
func (*tableEmailSQLConn) Close() error                             { return nil }
func (c *tableEmailSQLConn) Begin() (driver.Tx, error)              { return tableEmailSQLTx{c.fixture}, nil }
func (*tableEmailSQLConn) CheckNamedValue(*driver.NamedValue) error { return nil }

type tableEmailSQLTx struct{ fixture *tableEmailSQLFixture }

func (tx tableEmailSQLTx) Commit() error   { tx.fixture.commits++; return nil }
func (tx tableEmailSQLTx) Rollback() error { tx.fixture.rollbacks++; return nil }
func (c *tableEmailSQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.fixture.mode == "oracle" {
		return &tableEmailSQLRows{columns: []string{"ID", "EMAIL", "NAME"}, rows: [][]driver.Value{{"11", "buyer@example.com", "张三"}}}, nil
	}
	if strings.Contains(query, "`table_email_deliveries`") {
		return &tableEmailSQLRows{columns: []string{"id", "rule_id", "generation", "status", "retry_version", "recipients_json", "subject", "body", "attempts"}, rows: [][]driver.Value{{int64(19), int64(7), int64(1), c.fixture.deliveryStatus, int64(0), `["buyer@example.com"]`, "新订单", "<张三>\n已下单", int64(0)}}}, nil
	}
	if strings.Contains(query, "`table_email_rules`") {
		result := &tableEmailSQLRows{columns: []string{"id", "generation", "enabled", "owner", "table_name", "cursor_column", "cursor_value", "poll_interval_seconds", "recipients_template", "subject_template", "body_template"}}
		if c.fixture.mode != "scan" || !c.fixture.checked {
			result.rows = [][]driver.Value{{int64(7), int64(1), c.fixture.ruleEnabled, "ERP", "ORDERS", "ID", "10", int64(60), "{{EMAIL}}", "新订单 {{ID}}", "{{NAME}}"}}
		}
		c.fixture.checked = true
		return result, nil
	}
	return nil, errors.New("unexpected SQL query")
}
func (c *tableEmailSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.fixture.writes = append(c.fixture.writes, query)
	c.fixture.values = append(c.fixture.values, append([]driver.NamedValue(nil), args...))
	if c.fixture.failOutbox && strings.Contains(query, "`async_job_outbox`") {
		return nil, errors.New("outbox unavailable")
	}
	return tableEmailSQLResult{}, nil
}

type tableEmailSQLResult struct{}

func (tableEmailSQLResult) LastInsertId() (int64, error) { return 19, nil }
func (tableEmailSQLResult) RowsAffected() (int64, error) { return 1, nil }

type tableEmailSQLRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *tableEmailSQLRows) Columns() []string { return r.columns }
func (*tableEmailSQLRows) Close() error        { return nil }
func (r *tableEmailSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func tableEmailTestDB(t *testing.T, fixture *tableEmailSQLFixture) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(tableEmailConnector{fixture})
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func (fixture *tableEmailSQLFixture) wroteValue(value string) bool {
	for _, values := range fixture.values {
		for _, arg := range values {
			if arg.Value == value {
				return true
			}
		}
	}
	return false
}

func TestTableEmailDeliveryCommitsSuccessAndSkipsCompletedTask(t *testing.T) {
	for _, status := range []string{"queued", "sent"} {
		t.Run(status, func(t *testing.T) {
			fixture := &tableEmailSQLFixture{deliveryStatus: status, ruleEnabled: true}
			calls := 0
			service := &TableEmailService{db: tableEmailTestDB(t, fixture), now: time.Now, send: func(to []string, subject, body string) error {
				calls++
				if len(to) != 1 || to[0] != "buyer@example.com" || subject != "新订单" || body != "&lt;张三&gt;<br>\n已下单" {
					t.Fatalf("wrong mail: %v %s %s", to, subject, body)
				}
				return nil
			}}
			if err := service.Deliver(t.Context(), job.TableEmailDeliveryPayload{DeliveryID: 19}, true); err != nil {
				t.Fatal(err)
			}
			if status == "queued" && (calls != 1 || !fixture.wroteValue("sent") || fixture.commits != 1) {
				t.Fatalf("success not committed: %+v", fixture)
			}
			if status == "sent" && (calls != 0 || len(fixture.writes) != 0) {
				t.Fatal("completed delivery was repeated")
			}
		})
	}
}
func TestTableEmailFailurePersistsBeforeQueueRetry(t *testing.T) {
	fixture := &tableEmailSQLFixture{deliveryStatus: "queued", ruleEnabled: true}
	service := &TableEmailService{db: tableEmailTestDB(t, fixture), now: time.Now, send: func([]string, string, string) error { return errors.New("private SMTP failure") }}
	err := service.Deliver(t.Context(), job.TableEmailDeliveryPayload{DeliveryID: 19}, true)
	if err == nil || strings.Contains(err.Error(), "private") || fixture.commits != 1 || fixture.rollbacks != 0 || !fixture.wroteValue("retrying") {
		t.Fatalf("failure state lost: %+v, %v", fixture, err)
	}
}
func TestTableEmailPausedRuleCancelsQueuedMail(t *testing.T) {
	fixture := &tableEmailSQLFixture{deliveryStatus: "queued", ruleEnabled: false}
	service := &TableEmailService{db: tableEmailTestDB(t, fixture), now: time.Now, send: func([]string, string, string) error { t.Fatal("paused mail sent"); return nil }}
	if err := service.Deliver(t.Context(), job.TableEmailDeliveryPayload{DeliveryID: 19}, true); err != nil {
		t.Fatal(err)
	}
	if !fixture.wroteValue("cancelled") {
		t.Fatal("missing cancelled status")
	}
}

type tableEmailTestOracle struct{ db *sql.DB }

func (o tableEmailTestOracle) Close() error { return nil }
func (o tableEmailTestOracle) QuerySelect(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return o.db.QueryContext(ctx, q, args...)
}
func (tableEmailTestOracle) ListResultTables(context.Context, reportoracle.ResultTableCatalogQuery) ([]reportoracle.ResultTableSummary, error) {
	return nil, nil
}
func (tableEmailTestOracle) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	return nil, nil
}
func TestTableEmailScanCommitsCursorWithOutboxOrRollsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "commit"
		if fail {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			fixture := &tableEmailSQLFixture{mode: "scan", ruleEnabled: true, failOutbox: fail}
			oracleDB := sql.OpenDB(tableEmailConnector{&tableEmailSQLFixture{mode: "oracle"}})
			defer oracleDB.Close()
			service := &TableEmailService{db: tableEmailTestDB(t, fixture), now: time.Now, open: func(context.Context, uint) (tableEmailOracle, error) { return tableEmailTestOracle{oracleDB}, nil }}
			err := service.Scan(t.Context())
			if fail {
				if err == nil || fixture.rollbacks != 1 || fixture.commits != 0 {
					t.Fatalf("outbox failure did not roll back: %+v %v", fixture, err)
				}
				for _, query := range fixture.writes {
					if strings.HasPrefix(query, "UPDATE `table_email_rules`") {
						t.Fatal("cursor advanced before durable outbox")
					}
				}
			} else {
				if err != nil || fixture.rollbacks != 0 || !fixture.wroteValue("11") || len(fixture.writes) != 3 {
					t.Fatalf("incomplete scan: %+v %v", fixture, err)
				}
				if !strings.Contains(fixture.writes[0], "table_email_deliveries") || !strings.Contains(fixture.writes[1], "async_job_outbox") || !strings.Contains(fixture.writes[2], "table_email_rules") {
					t.Fatalf("wrong write order: %v", fixture.writes)
				}
			}
		})
	}
}
