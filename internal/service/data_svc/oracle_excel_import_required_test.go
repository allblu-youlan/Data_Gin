package data_svc

import (
	"database/sql"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestOracleExcelImportReportsAllMissingRequiredColumnsBeforeTransaction(t *testing.T) {
	config := OracleExcelImportConfig{
		Owner: "YL_DBS", TableName: "ADS_IMPORT_SALES_PRODUCT_REPORT_QUERY", HeaderRow: 1,
		Mappings: []OracleExcelImportMapping{{ExcelColumn: "日期", Field: "YMD_DATE"}},
	}
	columns := []reportoracle.ResultColumn{
		{Name: "ID", DataType: "NUMBER"},
		{Name: "YMD_DATE", DataType: "DATE"},
		{Name: "INSERT_DATE", DataType: "DATE"},
		{Name: "NOTE", DataType: "VARCHAR2", Nullable: true},
	}
	_, _, _, err := buildOracleExcelImport(config, columns)
	if err == nil || !strings.Contains(err.Error(), "ID, INSERT_DATE") || strings.Contains(err.Error(), "YMD_DATE") || strings.Contains(err.Error(), "NOTE") {
		t.Errorf("missing fields must be reported together: %v", err)
	}
	path := oracleImportWorkbook(t, "required.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"日期"}, {"2026-11-27"}}})
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: columns}, path, config)
	if err == nil || !strings.Contains(err.Error(), "ID, INSERT_DATE") || count != 0 || state.statement != "" || state.commits != 0 || state.rollbacks != 0 {
		t.Fatalf("required-field preflight must precede Oracle transaction: count=%d error=%v state=%+v", count, err, state)
	}
}

func TestOracleExcelImportRequiredNullIncludesSourceLocation(t *testing.T) {
	path := oracleImportWorkbook(t, "missing-amount.xlsx", oracleImportSheetFixture{name: "销售数据", rows: [][]interface{}{
		{"订单号", "金额"}, {"", ""}, {"A", ""},
	}})
	config := oracleImportTestConfig()
	config.SheetName, config.HeaderRow = "销售数据", 1
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportTestColumns()}, path, config)
	if err == nil || count != 0 || state.commits != 0 || state.rollbacks != 1 || len(state.rows) != 0 {
		t.Fatalf("required NULL must abort before writing: count=%d error=%v state=%+v", count, err, state)
	}
	for _, want := range []string{"missing-amount.xlsx", "销售数据", "第 3 行", "AMOUNT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestOracleExcelImportAllowsOmittedDatabaseGeneratedColumns(t *testing.T) {
	for _, test := range []struct {
		name   string
		column reportoracle.ResultColumn
	}{
		{name: "nullable", column: reportoracle.ResultColumn{Nullable: true}},
		{name: "default", column: reportoracle.ResultColumn{HasDefault: true}},
		{name: "identity", column: reportoracle.ResultColumn{Identity: true}},
		{name: "insert trigger", column: reportoracle.ResultColumn{HasInsertTrigger: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.column.Name, test.column.DataType = "ID", "NUMBER"
			columns := append(oracleImportTestColumns(), test.column)
			_, statement, _, err := buildOracleExcelImport(oracleImportTestConfig(), columns)
			if err != nil || strings.Contains(statement, `"ID"`) {
				t.Fatalf("database-generated field must stay omitted: SQL=%s error=%v", statement, err)
			}
		})
	}
}

func TestOracleExcelImportRequiredNullRespectsDatabaseGenerationRules(t *testing.T) {
	path := oracleImportWorkbook(t, "date.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{
		{"订单号", "写入时间"}, {"A", ""},
	}})
	config := OracleExcelImportConfig{Owner: "DEMO", TableName: "ORDERS", SheetName: "Sheet1", HeaderRow: 1, Mappings: []OracleExcelImportMapping{
		{ExcelColumn: "订单号", Field: "CODE"}, {ExcelColumn: "写入时间", Field: "INSERT_DATE"},
	}}
	for _, test := range []struct {
		name    string
		column  reportoracle.ResultColumn
		allowed bool
	}{
		{name: "required"},
		{name: "ordinary default does not replace NULL", column: reportoracle.ResultColumn{HasDefault: true}},
		{name: "identity alone does not replace NULL", column: reportoracle.ResultColumn{Identity: true}},
		{name: "default on NULL", column: reportoracle.ResultColumn{HasDefault: true, DefaultOnNull: true}, allowed: true},
		{name: "insert trigger", column: reportoracle.ResultColumn{HasInsertTrigger: true}, allowed: true},
		{name: "nullable", column: reportoracle.ResultColumn{Nullable: true}, allowed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.column.Name, test.column.DataType = "INSERT_DATE", "DATE"
			columns := []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, test.column}
			written := 0
			count, err := readOracleExcelImportLocated(t.Context(), path, config, columns, func(values []interface{}, _ oracleExcelImportLocation) error {
				written++
				if values[1] != nil {
					t.Fatalf("reader invented a date instead of leaving generation to Oracle: %v", values[1])
				}
				return nil
			})
			if test.allowed {
				if err != nil || count != 1 || written != 1 {
					t.Fatalf("generated/nullable NULL rejected: count=%d written=%d error=%v", count, written, err)
				}
			} else if err == nil || count != 0 || written != 0 || !strings.Contains(err.Error(), "INSERT_DATE") {
				t.Fatalf("required NULL accepted: count=%d written=%d error=%v", count, written, err)
			}
		})
	}
}
