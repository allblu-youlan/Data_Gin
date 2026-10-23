package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/godror/godror"
	"github.com/xuri/excelize/v2"
)

type oracleImportSheetFixture struct {
	name string
	rows [][]interface{}
}

func oracleImportWorkbook(t *testing.T, name string, sheets ...oracleImportSheetFixture) string {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()
	for index, sheet := range sheets {
		if index == 0 {
			if err := file.SetSheetName(defaultExcelSheetName, sheet.name); err != nil {
				t.Fatal(err)
			}
		} else if _, err := file.NewSheet(sheet.name); err != nil {
			t.Fatal(err)
		}
		for rowIndex, row := range sheet.rows {
			cell, err := excelize.CoordinatesToCellName(1, rowIndex+1)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.SetSheetRow(sheet.name, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := filepath.Join(t.TempDir(), name)
	if err := file.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func oracleImportFilesConfig() OracleExcelImportConfig {
	config := oracleImportTestConfig()
	config.HeaderRow = 1
	config.AllSheets = true
	config.SheetName = "按工作簿顺序读取"
	config.FixedIdentifier = &OracleExcelImportFixedIdentifier{Enabled: true, Field: "batch_tag", Value: "001"}
	return config
}

func oracleImportFilesColumns() []reportoracle.ResultColumn {
	return append(oracleImportTestColumns(), reportoracle.ResultColumn{Name: "BATCH_TAG", DataType: "VARCHAR2"})
}

func TestOracleExcelImportFilesStreamsAllSheetsWithBoundedBatchesAndAutomaticIdentifier(t *testing.T) {
	firstRows := [][]interface{}{{"订单号", "金额"}, {"001", "1.50"}, {"", ""}, {"002", "2.50"}}
	secondRows := [][]interface{}{{"金额", "订单号"}}
	for index := 3; index <= 203; index++ {
		secondRows = append(secondRows, []interface{}{"3.50", fmt.Sprintf("%03d", index)})
	}
	first := oracleImportWorkbook(t, "part-one.xlsx", oracleImportSheetFixture{name: "先处理", rows: firstRows}, oracleImportSheetFixture{name: "后处理", rows: secondRows})
	second := oracleImportWorkbook(t, "part-two.xlsx", oracleImportSheetFixture{name: "第二文件首表", rows: [][]interface{}{{"订单号", "金额"}, {"204", "4.50"}}}, oracleImportSheetFixture{name: "第二文件末表", rows: [][]interface{}{{"订单号", "金额"}, {"205", "5.50"}}})
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportFilesOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{first, second}, oracleImportFilesConfig(), oracleImportTestIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	if count != 205 || state.commits != 1 || state.rollbacks != 0 || len(state.rows) != 2 {
		t.Fatalf("rows=%d executions=%d commits=%d rollbacks=%d", count, len(state.rows), state.commits, state.rollbacks)
	}
	next := 1
	for batchIndex, arguments := range state.rows {
		if len(arguments) > oracleExcelImportMaxBinds || len(arguments) > oracleExcelImportMaxBatchColumns {
			t.Fatalf("unbounded batch: binds=%d", len(arguments))
		}
		if !strings.HasPrefix(state.statements[batchIndex], "INSERT ALL ") || strings.Contains(state.statements[batchIndex], "'001'") {
			t.Fatalf("batch did not use bound INSERT ALL: %s", state.statements[batchIndex])
		}
		for index := 0; index < len(arguments); index += 3 {
			if arguments[index].Value != fmt.Sprintf("%03d", next) || arguments[index+2].Value != "42" {
				t.Fatalf("file/sheet order or automatic identifier changed: row=%d values=%v", next, arguments[index:index+3])
			}
			if _, ok := arguments[index+1].Value.(godror.Number); !ok {
				t.Fatalf("amount lost numeric binding: %T", arguments[index+1].Value)
			}
			next++
		}
	}
	if next != 206 {
		t.Fatalf("blank Excel rows became fixed-identifier-only rows: next=%d", next)
	}
}

func TestOracleExcelImportFilesRollsBackEarlierBatchesWhenLaterFileFails(t *testing.T) {
	firstRows := [][]interface{}{{"订单号", "金额"}}
	for index := 0; index < oracleExcelImportBatchRows; index++ {
		firstRows = append(firstRows, []interface{}{fmt.Sprintf("ORDER_%d", index), "10"})
	}
	first := oracleImportWorkbook(t, "completed-batch.xlsx", oracleImportSheetFixture{name: "已写入批次", rows: firstRows})
	for _, test := range []struct {
		name, amount string
		writeError   error
		errorAt      int
	}{
		{name: "invalid later workbook row", amount: "invalid number"},
		{name: "database rejects later batch", amount: "20", writeError: errors.New("constraint rejected"), errorAt: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			second := oracleImportWorkbook(t, "failing-workbook.xlsx", oracleImportSheetFixture{name: "失败数据", rows: [][]interface{}{{"订单号", "金额"}, {"LAST", test.amount}}})
			state := &oracleOperationsTestState{writeError: test.writeError, writeErrorAt: test.errorAt}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			count, err := executeOracleExcelImportFilesOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{first, second}, oracleImportFilesConfig(), oracleImportTestIdentifier)
			if err == nil || count != 0 || state.commits != 0 || state.rollbacks != 1 || len(state.rows) == 0 {
				t.Fatalf("count=%d state=%+v error=%v", count, state, err)
			}
			if len(state.rows[0]) != oracleExcelImportBatchRows*3 {
				t.Fatalf("earlier workbook was not flushed before failure: %d binds", len(state.rows[0]))
			}
			if !strings.Contains(err.Error(), "failing-workbook.xlsx") || !strings.Contains(err.Error(), "失败数据") {
				t.Fatalf("error lacks filename/sheet context: %v", err)
			}
		})
	}
}

func TestOracleExcelImportSingleSheetAndDisabledIdentifierKeepSingleRowStatement(t *testing.T) {
	path := oracleImportWorkbook(t, "single-sheet.xlsx", oracleImportSheetFixture{name: defaultExcelSheetName, rows: [][]interface{}{{"订单号", "金额"}, {"A001", "10"}}}, oracleImportSheetFixture{name: "不应导入", rows: [][]interface{}{{"错误表头"}, {"无关数据"}}})
	config := oracleImportTestConfig()
	config.HeaderRow = 1
	config.FixedIdentifier = &OracleExcelImportFixedIdentifier{Enabled: false, Field: "CODE", Value: "无效设置应忽略"}
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	count, err := executeOracleExcelImportOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportTestColumns()}, path, config)
	if err != nil {
		t.Fatal(err)
	}
	want := `INSERT INTO "DEMO"."ORDERS" ("CODE", "AMOUNT") VALUES (:1, :2)`
	if count != 1 || state.commits != 1 || !reflect.DeepEqual(state.statements, []string{want}) || len(state.rows[0]) != 2 {
		t.Fatalf("single-row compatibility lost: count=%d state=%+v", count, state)
	}
}

func TestOracleExcelImportIdentifierValidatesMetadataAndClearsSavedValue(t *testing.T) {
	for _, test := range []struct{ name, field, value string }{
		{name: "field must exist", field: "MISSING", value: "001"},
		{name: "field cannot also be mapped", field: "CODE", value: "001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := oracleImportFilesConfig()
			config.FixedIdentifier.Field, config.FixedIdentifier.Value = test.field, test.value
			if _, _, _, err := buildOracleExcelImport(config, oracleImportFilesColumns()); err == nil {
				t.Fatal("invalid identifier field accepted")
			}
		})
	}
	config := oracleImportFilesConfig()
	normalized, statement, columns, err := buildOracleExcelImport(config, oracleImportFilesColumns())
	if err != nil {
		t.Fatal(err)
	}
	if normalized.FixedIdentifier.Field != "BATCH_TAG" || config.FixedIdentifier.Field != "batch_tag" || normalized.FixedIdentifier.Value != "" || config.FixedIdentifier.Value != "001" || len(columns) != 3 || strings.Contains(statement, "001") {
		t.Fatalf("fixed identifier was mutated or interpolated: config=%+v normalized=%+v SQL=%s", config.FixedIdentifier, normalized.FixedIdentifier, statement)
	}
}

func oracleImportTestIdentifier(context.Context) (string, error) { return "42", nil }

func TestOracleExcelImportAllocatesOneNewNumberOnEveryExecution(t *testing.T) {
	first := oracleImportWorkbook(t, "first.xlsx", oracleImportSheetFixture{name: "first", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}}}, oracleImportSheetFixture{name: "second", rows: [][]interface{}{{"订单号", "金额"}, {"B", "20"}}})
	second := oracleImportWorkbook(t, "second.xlsx", oracleImportSheetFixture{name: "third", rows: [][]interface{}{{"订单号", "金额"}, {"C", "30"}}})
	for _, kind := range []string{"VARCHAR2", "NUMBER"} {
		t.Run(kind, func(t *testing.T) {
			columns := oracleImportFilesColumns()
			columns[len(columns)-1].DataType = kind
			next := 0
			allocate := func(context.Context) (string, error) { next++; return strconv.Itoa(next), nil }
			config := oracleImportFilesConfig()
			for run := 1; run <= 2; run++ {
				state := &oracleOperationsTestState{}
				db := sql.OpenDB(oracleOperationsTestConnector{state: state})
				count, err := executeOracleExcelImportFilesOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: columns}, []string{first, second}, config, allocate)
				_ = db.Close()
				if err != nil || count != 3 || next != run || state.commits != 1 {
					t.Fatalf("run=%d count=%d allocations=%d state=%+v error=%v", run, count, next, state, err)
				}
				for _, batch := range state.rows {
					for offset := 2; offset < len(batch); offset += 3 {
						if kind == "NUMBER" {
							if _, ok := batch[offset].Value.(godror.Number); !ok {
								t.Fatalf("identifier lost numeric binding: %T", batch[offset].Value)
							}
						}
						if fmt.Sprint(batch[offset].Value) != strconv.Itoa(run) {
							t.Fatalf("run=%d reused another import number: %v", run, batch[offset].Value)
						}
					}
				}
				if config.FixedIdentifier.Value != "001" {
					t.Fatalf("execution changed saved draft: %+v", config.FixedIdentifier)
				}
			}
		})
	}
}

func TestOracleExcelImportIdentifierFailureStopsBeforeOracleWrites(t *testing.T) {
	path := oracleImportWorkbook(t, "import.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}}})
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	want := errors.New("sequence storage unavailable")
	count, err := executeOracleExcelImportFilesOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{path}, oracleImportFilesConfig(), func(context.Context) (string, error) { return "", want })
	if !errors.Is(err, want) || count != 0 || len(state.rows) != 0 || state.statement != "" || state.commits != 0 {
		t.Fatalf("count=%d state=%+v error=%v", count, state, err)
	}
}

func TestOracleExcelImportIdentifierAcceptsEmptyLegacyValueAndRejectsDates(t *testing.T) {
	for _, kind := range []string{"VARCHAR2", "NUMBER", "DATE"} {
		t.Run(kind, func(t *testing.T) {
			config := oracleImportFilesConfig()
			config.FixedIdentifier.Value = ""
			columns := oracleImportFilesColumns()
			columns[len(columns)-1].DataType = kind
			normalized, _, _, err := buildOracleExcelImport(config, columns)
			if kind == "DATE" {
				if err == nil {
					t.Fatal("date column accepted for an incrementing identifier")
				}
				return
			}
			if err != nil || normalized.FixedIdentifier.Value != "" {
				t.Fatalf("config=%+v error=%v", normalized, err)
			}
		})
	}
}

func TestOracleExcelImportBatchSizeRespectsColumnAndBindingLimits(t *testing.T) {
	for _, width := range []int{1, 3, 5, 20, 200, 513} {
		t.Run(fmt.Sprintf("%d columns", width), func(t *testing.T) {
			rows := oracleExcelImportBatchSize(width)
			if rows < 1 || rows > oracleExcelImportBatchRows || rows*width > oracleExcelImportMaxBinds || rows*width > oracleExcelImportMaxBatchColumns {
				t.Fatalf("unbounded batch: width=%d rows=%d", width, rows)
			}
		})
	}
}
