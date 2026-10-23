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

func TestOracleExcelImportBatchesKeepSeparateInsertsForGeneratedIDs(t *testing.T) {
	// 28 mapped columns produce the 35-row batch seen in the failing import.
	// ID is deliberately omitted so Oracle evaluates its default per INSERT.
	const width, rows = 28, 36
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
	if !strings.HasPrefix(query, "BEGIN ") || !strings.HasSuffix(query, "; END;") {
		t.Fatalf("batch must execute independent INSERT statements: %s", query)
	}
	inserts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(query, "BEGIN "), " END;"), ";")
	if len(inserts) != 36 || inserts[35] != "" || len(state.rows[0]) != 35*width {
		t.Fatalf("expected 35 independent inserts and one trailing row: inserts=%d binds=%d", len(inserts)-1, len(state.rows[0]))
	}
	placeholder := regexp.MustCompile(`:(\d+)`)
	for row, insert := range inserts[:35] {
		insert = strings.TrimSpace(insert)
		binds := placeholder.FindAllStringSubmatch(insert, -1)
		if !strings.HasPrefix(insert, `INSERT INTO "DEMO"."ORDERS" (`) || strings.Contains(insert, `"ID"`) || len(binds) != width {
			t.Fatalf("row %d is not an independent insert leaving ID to its default: %s", row+1, insert)
		}
		for column, bind := range binds {
			offset := row*width + column
			if bind[1] != strconv.Itoa(offset+1) || state.rows[0][offset].Value != fmt.Sprintf("row %d column %d", row+1, column+1) {
				t.Fatalf("row %d column %d lost its distinct bind or value: %v", row+1, column+1, bind)
			}
		}
	}
	if state.statements[1] != state.statement || len(state.rows[1]) != width || state.rows[1][0].Value != "row 36 column 1" {
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
