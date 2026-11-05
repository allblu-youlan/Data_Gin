package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
)

type errorLogTestConnector struct {
	exec func(context.Context, string, []driver.NamedValue) (driver.Result, error)
}

func (connector errorLogTestConnector) Connect(context.Context) (driver.Conn, error) {
	return errorLogTestConnection{connector: connector}, nil
}

func (connector errorLogTestConnector) Driver() driver.Driver { return errorLogTestDriver{connector} }

type errorLogTestDriver struct{ connector errorLogTestConnector }

func (driver errorLogTestDriver) Open(string) (driver.Conn, error) {
	return driver.connector.Connect(context.Background())
}

type errorLogTestConnection struct{ connector errorLogTestConnector }

func (errorLogTestConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (errorLogTestConnection) Close() error                        { return nil }
func (errorLogTestConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (connection errorLogTestConnection) ExecContext(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Result, error) {
	return connection.connector.exec(ctx, statement, arguments)
}

func TestWriteErrorLogUsesExistingProcedureWithBoundValues(t *testing.T) {
	ctx := t.Context()
	want := []any{"Data_Gin", "接口失败", "service/order.go:19", "ORACLE ' quoted error", "alerts@example.com"}
	db := sql.OpenDB(errorLogTestConnector{exec: func(gotCtx context.Context, statement string, arguments []driver.NamedValue) (driver.Result, error) {
		if gotCtx != ctx || statement != "BEGIN YL_DBS.YL_ERROR_LOG_WRITE(:1, :2, :3, :4, :5); END;" {
			t.Fatalf("unexpected Oracle error log call: %s", statement)
		}
		values := make([]any, len(arguments))
		for index, argument := range arguments {
			values[index] = argument.Value
		}
		if !reflect.DeepEqual(values, want) {
			t.Fatalf("procedure values=%#v want=%#v", values, want)
		}
		return driver.RowsAffected(1), nil
	}})
	t.Cleanup(func() { _ = db.Close() })
	adapter := &Adapter{db: db}
	err := adapter.WriteErrorLog(ctx, ErrorLogEntry{
		InterfaceDirect: "Data_Gin", KeyInfoNo: "接口失败", ProInfo: "service/order.go:19",
		ErrorInfo: "ORACLE ' quoted error", Email: "alerts@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteErrorLogPreservesOracleFailure(t *testing.T) {
	want := errors.New("Oracle unavailable")
	db := sql.OpenDB(errorLogTestConnector{exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return nil, want
	}})
	t.Cleanup(func() { _ = db.Close() })
	if err := (&Adapter{db: db}).WriteErrorLog(t.Context(), ErrorLogEntry{}); !errors.Is(err, want) {
		t.Fatalf("Oracle error lost: %v", err)
	}
	if err := (&Adapter{}).WriteErrorLog(t.Context(), ErrorLogEntry{}); err == nil {
		t.Fatal("closed adapter unexpectedly succeeded")
	}
}
