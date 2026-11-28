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

func TestQimaiSalesSyncQuerySelectsOnlyRequiredColumnsInStableOrder(t *testing.T) {
	state := &qimaiSalesQueryState{}
	dao := &QimaiDataDAO{db: newQimaiSalesQueryDB(t, state)}
	rows, err := dao.FindByShopCodeAndStatus(t.Context(), "YXHZR001", "70")
	if err != nil || len(rows) != 2 || rows[0].ID != 3 || rows[1].ID != 9 || rows[0].OrderNo != "order-3" || rows[0].ActualAmount != 1280 || rows[0].CompletedAt == nil || rows[1].CompletedAt != nil {
		t.Fatalf("sales fields or row order changed: rows=%+v err=%v", rows, err)
	}
	wantSQL := "SELECT `id`,`order_no`,`actual_amount`,`completed_at` FROM `qimai_order_data` WHERE shop_code = ? AND status = ? AND synced = 0 ORDER BY id ASC"
	if state.query != wantSQL || state.calls != 1 || len(state.args) != 2 || state.args[0].Value != "YXHZR001" || state.args[1].Value != "70" {
		t.Fatalf("query must be narrow, bound and ordered without pagination: SQL=%s args=%v calls=%d", state.query, state.args, state.calls)
	}
}

func TestQimaiSalesSyncQueryDiscardsPartialRowsAndDoesNotRetry(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "query failure"
		if partial {
			name = "partial row failure"
		}
		t.Run(name, func(t *testing.T) {
			state := &qimaiSalesQueryState{cause: mysqlDriver.ErrInvalidConn, partial: partial}
			dao := &QimaiDataDAO{db: newQimaiSalesQueryDB(t, state)}
			rows, err := dao.FindByShopCodeAndStatus(t.Context(), "YXHZR001", "70")
			if !errors.Is(err, state.cause) || rows != nil || state.calls != 1 {
				t.Fatalf("failed read leaked partial orders or retried: rows=%+v error=%v calls=%d", rows, err, state.calls)
			}
		})
	}
}

func TestQimaiSalesSyncQueryHonorsCancelledContext(t *testing.T) {
	state := &qimaiSalesQueryState{}
	dao := &QimaiDataDAO{db: newQimaiSalesQueryDB(t, state)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rows, err := dao.FindByShopCodeAndStatus(ctx, "YXHZR001", "70")
	if !errors.Is(err, context.Canceled) || rows != nil || state.calls != 0 {
		t.Fatalf("cancelled query ran or returned orders: rows=%+v error=%v calls=%d", rows, err, state.calls)
	}
}

type qimaiSalesQueryState struct {
	query   string
	args    []driver.NamedValue
	calls   int
	cause   error
	partial bool
}

func newQimaiSalesQueryDB(t *testing.T, state *qimaiSalesQueryState) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(qimaiSalesQueryConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type qimaiSalesQueryConnector struct{ state *qimaiSalesQueryState }

func (c qimaiSalesQueryConnector) Connect(context.Context) (driver.Conn, error) {
	return &qimaiSalesQueryConn{c.state}, nil
}
func (qimaiSalesQueryConnector) Driver() driver.Driver { return qimaiSalesQueryDriver{} }

type qimaiSalesQueryDriver struct{}

func (qimaiSalesQueryDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use test connector")
}

type qimaiSalesQueryConn struct{ state *qimaiSalesQueryState }

func (*qimaiSalesQueryConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*qimaiSalesQueryConn) Close() error                        { return nil }
func (*qimaiSalesQueryConn) Begin() (driver.Tx, error) {
	return nil, errors.New("sales query must not start a transaction")
}
func (c *qimaiSalesQueryConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(query, "SELECT ") {
		return nil, errors.New("sales query must remain read-only")
	}
	c.state.calls++
	c.state.query, c.state.args = query, append([]driver.NamedValue(nil), args...)
	if c.state.cause != nil && !c.state.partial {
		return nil, c.state.cause
	}
	return &qimaiSalesQueryRows{state: c.state}, nil
}

type qimaiSalesQueryRows struct {
	state    *qimaiSalesQueryState
	position int
}

func (*qimaiSalesQueryRows) Columns() []string {
	return []string{"id", "order_no", "actual_amount", "completed_at"}
}
func (*qimaiSalesQueryRows) Close() error { return nil }
func (r *qimaiSalesQueryRows) Next(values []driver.Value) error {
	if r.position == 1 && r.state.partial {
		return r.state.cause
	}
	if r.position >= 2 {
		return io.EOF
	}
	if r.position == 0 {
		copy(values, []driver.Value{int64(3), "order-3", int64(1280), time.Date(2026, 11, 28, 13, 0, 0, 0, time.UTC)})
	} else {
		copy(values, []driver.Value{int64(9), "order-9", int64(1380), nil})
	}
	r.position++
	return nil
}
