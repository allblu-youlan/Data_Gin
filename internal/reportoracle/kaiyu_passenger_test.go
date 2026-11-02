package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"gin-biz-web-api/model"
	"strings"
	"testing"
	"time"
)

type kaiyuSQLRecorder struct {
	statements []string
	args       [][]driver.NamedValue
}
type kaiyuConnector struct{ state *kaiyuSQLRecorder }

func (c kaiyuConnector) Connect(context.Context) (driver.Conn, error) {
	return &kaiyuConn{c.state}, nil
}
func (c kaiyuConnector) Driver() driver.Driver { return kaiyuDriver{c} }

type kaiyuDriver struct{ c kaiyuConnector }

func (d kaiyuDriver) Open(string) (driver.Conn, error) { return d.c.Connect(context.Background()) }

type kaiyuConn struct{ state *kaiyuSQLRecorder }

func (*kaiyuConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*kaiyuConn) Close() error                        { return nil }
func (*kaiyuConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c *kaiyuConn) ExecContext(_ context.Context, s string, args []driver.NamedValue) (driver.Result, error) {
	c.state.statements = append(c.state.statements, s)
	c.state.args = append(c.state.args, args)
	return driver.RowsAffected(1), nil
}

func TestKaiyuPassengerUpsertBindsValuesAndUsesYLZUser(t *testing.T) {
	state := &kaiyuSQLRecorder{}
	db := sql.OpenDB(kaiyuConnector{state})
	defer db.Close()
	adapter := &Adapter{db: db}
	hour := time.Date(2026, 10, 28, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	row := model.KaiyuPassengerHourly{MallID: 7, ShopID: "shop-'1", ShopName: "测试店", HourStart: hour, Traffic: 15, Passby: 30, TrafficCount: 12}
	for range 2 {
		if err := adapter.UpsertKaiyuPassengerHour(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	statement := state.statements[0]
	if !strings.Contains(statement, "MERGE INTO YLZ_USER.KAIYU_PASSENGER_HOURLY") || !strings.Contains(statement, "t.SHOP_ID = s.SHOP_ID AND t.HOUR_START = s.HOUR_START") || !strings.Contains(statement, "WHEN MATCHED THEN UPDATE") {
		t.Fatalf("not an idempotent store-hour write: %s", statement)
	}
	if strings.Contains(statement, row.ShopID) || strings.Contains(statement, row.ShopName) {
		t.Fatal("values interpolated into SQL")
	}
	if len(state.args[0]) < 16 || state.args[0][0].Value != row.ShopID {
		t.Fatalf("bindings=%v", state.args[0])
	}
}
