package data_svc

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestOracleExcelImportArrayBatchesLeaveGeneratedIDsToOracle(t *testing.T) {
	// ID is deliberately omitted so Oracle evaluates its default per row.
	const width, rows = 28, 1001
	config := OracleExcelImportConfig{Owner: "DEMO", TableName: "ORDERS", SheetName: "Sheet1", HeaderRow: 1}
	columns := []reportoracle.ResultColumn{{Name: "ID", DataType: "NUMBER"}}
	header := make([]interface{}, width)
	for column := range header {
		field := fmt.Sprintf("FIELD_%d", column+1)
		header[column] = field
		config.Mappings = append(config.Mappings, OracleExcelImportMapping{ExcelColumn: field, Field: field})
		columns = append(columns, reportoracle.ResultColumn{Name: field, DataType: "VARCHAR2"})
	}
	data := [][]interface{}{header}
	for row := 1; row <= rows; row++ {
		values := make([]interface{}, width)
		for column := range values {
			values[column] = fmt.Sprintf("row %d column %d", row, column+1)
		}
		data = append(data, values)
	}
	path := oracleImportTestFile(t, data)
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: columns}, path, config)
	if err != nil || count != rows || state.commits != 1 || state.rollbacks != 0 || len(state.rows) != 2 {
		t.Fatalf("count=%d state=%+v error=%v", count, state, err)
	}
	query := state.statements[0]
	if query != state.statement || !strings.HasPrefix(query, `INSERT INTO "DEMO"."ORDERS" (`) || strings.Contains(query, `"ID"`) || strings.Contains(query, "BEGIN") || strings.Contains(query, "INSERT ALL") {
		t.Fatalf("array batch must leave ID to its default in one prepared INSERT: %s", query)
	}
	if len(state.rawRows[0]) != width || len(state.rows[0]) != 1000*width {
		t.Fatalf("expected 1000 rows in 28 column arrays and one trailing row: column binds=%d expanded values=%d", len(state.rawRows[0]), len(state.rows[0]))
	}
	placeholder := regexp.MustCompile(`:(\d+)`)
	binds := placeholder.FindAllStringSubmatch(query, -1)
	if len(binds) != width {
		t.Fatalf("expected one bind per column: %s", query)
	}
	for column, bind := range binds {
		values, ok := state.rawRows[0][column].Value.([]string)
		if bind[1] != strconv.Itoa(column+1) || !ok || len(values) != 1000 {
			t.Fatalf("column %d lost its full array bind: placeholder=%v values=%v", column+1, bind, state.rawRows[0][column])
		}
		for row := range values {
			offset := row*width + column
			want := fmt.Sprintf("row %d column %d", row+1, column+1)
			if values[row] != want || state.rows[0][offset].Value != want {
				t.Fatalf("row %d column %d lost its distinct value: array=%v expanded=%v", row+1, column+1, values[row], state.rows[0][offset].Value)
			}
		}
	}
	if state.statements[1] != state.statement || len(state.rawRows[1]) != width || len(state.rows[1]) != width || state.rawRows[1][0].Value != "row 1001 column 1" {
		t.Fatalf("trailing single row changed: SQL=%s values=%v", state.statements[1], state.rows[1])
	}
}

func TestOracleExcelImportDoesNotIgnoreUniqueConstraintErrors(t *testing.T) {
	path := oracleImportTestFile(t, [][]interface{}{{"说明"}, {"订单号", "金额"}, {"A", "10"}, {"A", "20"}})
	want := errors.New("ORA-00001: unique constraint (YL_DBS.SYS_C008086) violated")
	state := &oracleOperationsTestState{writeError: want}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportTestColumns()}, path, oracleImportTestConfig())
	if count != 0 || !errors.Is(err, want) || state.commits != 0 || state.rollbacks != 1 {
		t.Fatalf("unique constraint error must abort the import: count=%d state=%+v error=%v", count, state, err)
	}
	if !strings.Contains(err.Error(), "第 3 行") || !strings.Contains(err.Error(), "第 4 行") {
		t.Fatalf("unique constraint error lost source row context: %v", err)
	}
}
