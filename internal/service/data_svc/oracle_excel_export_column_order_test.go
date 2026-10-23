package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
	"github.com/xuri/excelize/v2"
)

func TestOracleExcelConfiguredColumnOrderSurvivesSchemeSQLAndWorkbook(t *testing.T) {
	schema := []reportoracle.ResultColumn{{Name: "A", DataType: "VARCHAR2"}, {Name: "B", DataType: "VARCHAR2"}, {Name: "C", DataType: "VARCHAR2"}}
	for _, maximum := range []bool{false, true} {
		name := "ordinary projection"
		if maximum {
			name = "maximum projection"
		}
		t.Run(name, func(t *testing.T) {
			columns := []OracleExcelExportColumn{{Field: "C", Title: "自定义丙列"}, {Field: "A", Title: "自定义甲列"}}
			input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Columns: columns}
			if maximum {
				input.Filters = []OracleExcelExportFilter{{Field: "B", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX"}}}
			}
			service := &OracleExcelExportSchemeService{store: &fakeOracleExcelExportSchemeStore{}, normalize: func(_ context.Context, config OracleExcelExportConfig) (OracleExcelExportConfig, error) {
				normalized, _, _, err := buildOracleExcelExportQuery(config, schema)
				return normalized, err
			}}
			saved, err := service.Save(t.Context(), "自定义顺序", input)
			if err != nil || !reflect.DeepEqual(saved.Config.Columns, columns) {
				t.Fatalf("saved columns=%v err=%v", saved, err)
			}
			connection := &oracleExcelColumnOrderConnection{schema: schema, db: sql.OpenDB(oracleExcelColumnOrderConnector{})}
			t.Cleanup(func() { _ = connection.db.Close() })
			output, count, err := executeOracleExcelExport(t.Context(), connection, saved.Config, t.TempDir(), 100, nil)
			if err != nil || count != 1 || len(connection.statements) != 1 {
				t.Fatalf("count=%d statements=%v err=%v", count, connection.statements, err)
			}
			prefix := `SELECT "C", "A" FROM "DEMO"."ORDERS"`
			if maximum {
				prefix = `SELECT "EXCEL_VALUE_001" AS "C", "EXCEL_VALUE_002" AS "A" FROM (SELECT "C" AS "EXCEL_VALUE_001", "A" AS "EXCEL_VALUE_002",`
			}
			if !strings.HasPrefix(connection.statements[0], prefix) {
				t.Fatalf("SQL changed configured order: %s", connection.statements[0])
			}
			workbook, err := excelize.OpenFile(output)
			if err != nil {
				t.Fatal(err)
			}
			defer workbook.Close()
			rows, err := workbook.GetRows("数据")
			want := [][]string{{"自定义丙列", "自定义甲列"}, {"value-c", "value-a"}}
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("workbook rows=%v want=%v err=%v", rows, want, err)
			}
		})
	}
}

type oracleExcelColumnOrderConnection struct {
	db         *sql.DB
	schema     []reportoracle.ResultColumn
	statements []string
}

func (connection *oracleExcelColumnOrderConnection) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	return connection.schema, nil
}

func (connection *oracleExcelColumnOrderConnection) QuerySelect(ctx context.Context, statement string, args ...interface{}) (*sql.Rows, error) {
	connection.statements = append(connection.statements, statement)
	return connection.db.QueryContext(ctx, statement, args...)
}

type oracleExcelColumnOrderConnector struct{}

func (oracleExcelColumnOrderConnector) Connect(context.Context) (driver.Conn, error) {
	return oracleExcelColumnOrderSQLConnection{}, nil
}
func (oracleExcelColumnOrderConnector) Driver() driver.Driver { return oracleExcelColumnOrderDriver{} }

type oracleExcelColumnOrderDriver struct{}

func (oracleExcelColumnOrderDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type oracleExcelColumnOrderSQLConnection struct{}

func (oracleExcelColumnOrderSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (oracleExcelColumnOrderSQLConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unsupported")
}
func (oracleExcelColumnOrderSQLConnection) Close() error                             { return nil }
func (oracleExcelColumnOrderSQLConnection) CheckNamedValue(*driver.NamedValue) error { return nil }
func (oracleExcelColumnOrderSQLConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &oracleExcelColumnOrderRows{}, nil
}

type oracleExcelColumnOrderRows struct{ read bool }

func (*oracleExcelColumnOrderRows) Columns() []string { return []string{"C", "A"} }
func (*oracleExcelColumnOrderRows) Close() error      { return nil }
func (rows *oracleExcelColumnOrderRows) Next(values []driver.Value) error {
	if rows.read {
		return io.EOF
	}
	rows.read = true
	values[0], values[1] = "value-c", "value-a"
	return nil
}
