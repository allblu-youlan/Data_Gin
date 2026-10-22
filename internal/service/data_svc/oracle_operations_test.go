package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/godror/godror"
	"github.com/xuri/excelize/v2"
)

type oracleOperationsTestState struct {
	commits, rollbacks int
	statement          string
	statements         []string
	rows               [][]driver.NamedValue
	writeError         error
	writeErrorAt       int
}

type oracleOperationsTestConnector struct{ state *oracleOperationsTestState }

func (connector oracleOperationsTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &oracleOperationsTestConn{state: connector.state}, nil
}
func (connector oracleOperationsTestConnector) Driver() driver.Driver {
	return oracleOperationsTestDriver{state: connector.state}
}

type oracleOperationsTestDriver struct{ state *oracleOperationsTestState }

func (driver oracleOperationsTestDriver) Open(string) (driver.Conn, error) {
	return &oracleOperationsTestConn{state: driver.state}, nil
}

type oracleOperationsTestConn struct{ state *oracleOperationsTestState }

func (connection *oracleOperationsTestConn) Prepare(statement string) (driver.Stmt, error) {
	connection.state.statement = statement
	return &oracleOperationsTestStmt{state: connection.state, query: statement}, nil
}
func (*oracleOperationsTestConn) Close() error { return nil }
func (connection *oracleOperationsTestConn) Begin() (driver.Tx, error) {
	return &oracleOperationsTestTx{state: connection.state}, nil
}
func (*oracleOperationsTestConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (connection *oracleOperationsTestConn) ExecContext(_ context.Context, query string, values []driver.NamedValue) (driver.Result, error) {
	return connection.state.execute(query, values)
}

type oracleOperationsTestTx struct{ state *oracleOperationsTestState }

func (transaction *oracleOperationsTestTx) Commit() error { transaction.state.commits++; return nil }
func (transaction *oracleOperationsTestTx) Rollback() error {
	transaction.state.rollbacks++
	return nil
}

type oracleOperationsTestStmt struct {
	state *oracleOperationsTestState
	query string
}

func (*oracleOperationsTestStmt) Close() error  { return nil }
func (*oracleOperationsTestStmt) NumInput() int { return -1 }
func (*oracleOperationsTestStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, driver.ErrSkip
}
func (*oracleOperationsTestStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, driver.ErrSkip
}
func (statement *oracleOperationsTestStmt) ExecContext(_ context.Context, values []driver.NamedValue) (driver.Result, error) {
	return statement.state.execute(statement.query, values)
}

func (state *oracleOperationsTestState) execute(query string, values []driver.NamedValue) (driver.Result, error) {
	state.statements = append(state.statements, query)
	state.rows = append(state.rows, append([]driver.NamedValue(nil), values...))
	if state.writeErrorAt == 0 || len(state.rows) == state.writeErrorAt {
		return driver.RowsAffected(1), state.writeError
	}
	return driver.RowsAffected(1), nil
}

type oracleOperationsFakeConnection struct {
	db             *sql.DB
	columns        []reportoracle.ResultColumn
	arguments      []reportoracle.ProcedureArgument
	outputs        map[string]string
	procedureError error
}

func (connection oracleOperationsFakeConnection) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	return connection.columns, nil
}
func (connection oracleOperationsFakeConnection) InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error) {
	return connection.arguments, nil
}
func (connection oracleOperationsFakeConnection) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	return connection.db.BeginTx(ctx, options)
}
func (connection oracleOperationsFakeConnection) ExecuteOperationProcedure(context.Context, *sql.Tx, reportoracle.ProcedureRef, []reportoracle.ProcedureValue) (map[string]string, error) {
	return connection.outputs, connection.procedureError
}

func oracleImportTestConfig() OracleExcelImportConfig {
	return OracleExcelImportConfig{Owner: " demo ", TableName: "orders", HeaderRow: 2, Mappings: []OracleExcelImportMapping{{ExcelColumn: "订单号", Field: "code"}, {ExcelColumn: "金额", Field: "amount"}}}
}

func oracleImportTestColumns() []reportoracle.ResultColumn {
	return []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, {Name: "AMOUNT", DataType: "NUMBER"}}
}

func oracleImportTestFile(t *testing.T, rows [][]interface{}) string {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()
	for index, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, index+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "import.xlsx")
	if err := file.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOracleExcelImportUsesHeaderMappingPreservesValuesAndCommits(t *testing.T) {
	path := oracleImportTestFile(t, [][]interface{}{{"导入说明"}, {"金额", "订单号", "其他列"}, {"12345678901234567890.125", "000123", "忽略"}, {"", "", "跳过"}, {"42.50", "000124"}})
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	connection := oracleOperationsFakeConnection{db: db, columns: oracleImportTestColumns()}
	count, err := executeOracleExcelImportOnConnection(t.Context(), connection, path, oracleImportTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || state.commits != 1 || state.rollbacks != 0 || len(state.rows) != 1 {
		t.Fatalf("count=%d state=%+v", count, state)
	}
	if state.statement != `INSERT INTO "DEMO"."ORDERS" ("CODE", "AMOUNT") VALUES (:1, :2)` {
		t.Fatalf("SQL=%s", state.statement)
	}
	if state.rows[0][0].Value != "000123" || state.rows[0][1].Value != godror.Number("12345678901234567890.125") {
		t.Fatalf("mapped values lost precision or order: %+v", state.rows[0])
	}
	if !strings.HasPrefix(state.statements[0], "INSERT ALL ") || state.rows[0][2].Value != "000124" {
		t.Fatalf("rows were not batched in order: SQL=%s values=%v", state.statements[0], state.rows[0])
	}
}

func TestOracleExcelImportRollsBackInvalidRowsAndWriteFailures(t *testing.T) {
	for _, test := range []struct {
		name, amount string
		writeError   error
	}{{name: "invalid amount", amount: "not a number"}, {name: "database error", amount: "20", writeError: errors.New("constraint rejected")}} {
		t.Run(test.name, func(t *testing.T) {
			path := oracleImportTestFile(t, [][]interface{}{{"导入说明"}, {"金额", "订单号"}, {"10", "A"}, {test.amount, "B"}})
			state := &oracleOperationsTestState{writeError: test.writeError}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportTestColumns()}, path, oracleImportTestConfig())
			if err == nil || count != 0 || state.commits != 0 || state.rollbacks != 1 {
				t.Fatalf("count=%d error=%v state=%+v", count, err, state)
			}
		})
	}
}

func TestOracleExcelImportRejectsMissingHeadersAndUnknownFields(t *testing.T) {
	config, _, columns, err := buildOracleExcelImport(oracleImportTestConfig(), oracleImportTestColumns())
	if err != nil {
		t.Fatal(err)
	}
	path := oracleImportTestFile(t, [][]interface{}{{"说明"}, {"金额", "错误表头"}, {"10", "A"}})
	written := false
	_, err = readOracleExcelImport(t.Context(), path, config, columns, func([]interface{}) error { written = true; return nil })
	if err == nil || written || !strings.Contains(err.Error(), "缺少表头") {
		t.Fatalf("written=%v error=%v", written, err)
	}
	config.Mappings[0].Field = "MISSING"
	if _, _, _, err := buildOracleExcelImport(config, oracleImportTestColumns()); err == nil {
		t.Fatal("missing Oracle column accepted")
	}
}

func oracleProcedureTestConfig() OracleProcedureConfig {
	return OracleProcedureConfig{Owner: "demo", PackageName: "order_pkg", ProcedureName: "sync_order", Parameters: []OracleProcedureParameter{{Name: "p_code", Direction: "in", DataType: "varchar2", Value: "000123"}, {Name: "p_count", Direction: "inout", DataType: "number", Value: "7"}, {Name: "r_flag", Direction: "out", DataType: "varchar2"}}, Success: OracleProcedureSuccess{Parameter: "r_flag", Operator: "eq", Value: "成功"}}
}

func oracleProcedureTestArguments() []reportoracle.ProcedureArgument {
	return []reportoracle.ProcedureArgument{{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2"}, {Name: "P_COUNT", Direction: "IN/OUT", DataType: "NUMBER"}, {Name: "R_FLAG", Direction: "OUT", DataType: "VARCHAR2"}}
}

func TestOracleProcedureCommitsOnlyWhenSuccessFlagMatches(t *testing.T) {
	for _, test := range []struct {
		name, operator, output string
		wantError              bool
	}{{"equal", "eq", "成功", false}, {"contains", "contains", "订单处理成功，共 1 条", false}, {"failure", "eq", "订单失败", true}} {
		t.Run(test.name, func(t *testing.T) {
			state := &oracleOperationsTestState{}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			config := oracleProcedureTestConfig()
			config.Success.Operator = test.operator
			connection := oracleOperationsFakeConnection{db: db, arguments: oracleProcedureTestArguments(), outputs: map[string]string{"R_FLAG": test.output, "P_COUNT": "8"}}
			outputs, err := executeOracleProcedureOnConnection(t.Context(), connection, config)
			if (err != nil) != test.wantError || outputs["P_COUNT"] != "8" {
				t.Fatalf("outputs=%v error=%v", outputs, err)
			}
			if test.wantError && (state.commits != 0 || state.rollbacks != 1) || !test.wantError && (state.commits != 1 || state.rollbacks != 0) {
				t.Fatalf("transaction=%+v", state)
			}
		})
	}
}

func TestOracleProcedureValidatesSignatureAndSuccessOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*OracleProcedureConfig)
	}{
		{"missing parameter", func(config *OracleProcedureConfig) { config.Parameters = config.Parameters[:2] }},
		{"wrong type", func(config *OracleProcedureConfig) { config.Parameters[0].DataType = "NUMBER" }},
		{"input success flag", func(config *OracleProcedureConfig) { config.Success.Parameter = "P_CODE" }},
		{"unsupported operator", func(config *OracleProcedureConfig) { config.Success.Operator = "sql" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := oracleProcedureTestConfig()
			test.mutate(&config)
			if _, err := normalizeOracleProcedureWithMetadata(config, oracleProcedureTestArguments()); err == nil {
				t.Fatal("invalid procedure configuration accepted")
			}
		})
	}
	normalized, err := normalizeOracleProcedureWithMetadata(oracleProcedureTestConfig(), oracleProcedureTestArguments())
	if err != nil || normalized.Parameters[1].Direction != "IN OUT" || normalized.Success.Parameter != "R_FLAG" {
		t.Fatalf("normalized=%+v error=%v", normalized, err)
	}
}

func TestOracleProcedureDefaultNoFlagCanBeSavedAndExecuted(t *testing.T) {
	for _, test := range []struct {
		name       string
		parameters []OracleProcedureParameter
		arguments  []reportoracle.ProcedureArgument
	}{
		{name: "no argument procedure"},
		{name: "input without success flag", parameters: []OracleProcedureParameter{{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2", Value: "ORDER_001"}}, arguments: []reportoracle.ProcedureArgument{{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := OracleProcedureConfig{Owner: "DEMO", ProcedureName: "SYNC_ORDERS", Parameters: test.parameters, Success: OracleProcedureSuccess{Operator: "eq"}}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			store := &operationSchemeMemoryStore{}
			service := &ExcelOperationSchemeService{store: store, normalize: func(_ context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
				var input OracleProcedureConfig
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, err
				}
				normalized, err := normalizeOracleProcedureWithMetadata(input, test.arguments)
				if err != nil {
					return nil, err
				}
				return json.Marshal(normalized)
			}}
			saved, err := service.Save(t.Context(), "无标志过程", ExcelOperationOracleProcedure, raw)
			if err != nil {
				t.Fatal(err)
			}
			var persisted OracleProcedureConfig
			if err := json.Unmarshal(saved.Config, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Success != (OracleProcedureSuccess{}) {
				t.Fatalf("default success setting was not cleared: %+v", persisted.Success)
			}
			state := &oracleOperationsTestState{}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			connection := oracleOperationsFakeConnection{db: db, arguments: test.arguments, outputs: map[string]string{}}
			if _, err := executeOracleProcedureOnConnection(t.Context(), connection, persisted); err != nil {
				t.Fatal(err)
			}
			if state.commits != 1 || state.rollbacks != 0 {
				t.Fatalf("no-flag procedure did not commit: %+v", state)
			}
		})
	}
}

func TestOracleProcedureNoFlagStillRejectsResidualSuccessConfig(t *testing.T) {
	for _, test := range []struct {
		name    string
		success OracleProcedureSuccess
	}{
		{name: "value without parameter", success: OracleProcedureSuccess{Operator: "eq", Value: "执行成功"}},
		{name: "contains without parameter", success: OracleProcedureSuccess{Operator: "contains"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := OracleProcedureConfig{Owner: "DEMO", ProcedureName: "SYNC_ORDERS", Success: test.success}
			if _, err := normalizeOracleProcedureWithMetadata(config, nil); err == nil {
				t.Fatal("residual success setting accepted")
			}
		})
	}
}
