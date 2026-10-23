package data_svc

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestExcelWorkflowAllMatchFilesImportBeforeProcedure(t *testing.T) {
	for _, test := range []struct {
		name          string
		badSecondFile bool
	}{{name: "all matched workbooks reach one import"}, {name: "second matching error stops import", badSecondFile: true}} {
		t.Run(test.name, func(t *testing.T) {
			first := oracleImportWorkbook(t, "first.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}}})
			secondHeader := "订单号"
			if test.badSecondFile {
				secondHeader = "错误表头"
			}
			second := oracleImportWorkbook(t, "second.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"金额", secondHeader}, {"20", "B"}}})
			matching, err := normalizeExcelMatchConfig(ExcelMatchConfig{SheetName: "Sheet1", Steps: []ExcelMatchStep{{TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号", DBMatchField: "docno", DBValueField: "c_store_name", OutputColumnName: "匹配门店"}}})
			if err != nil {
				t.Fatal(err)
			}
			saved := OracleExcelImportConfig{Owner: "DEMO", TableName: "ORDERS", SheetName: "原文件工作表", HeaderRow: 8, Mappings: []OracleExcelImportMapping{{ExcelColumn: "订单号", Field: "ORDER_CODE"}, {ExcelColumn: "匹配门店", Field: "STORE_NAME"}}, FixedIdentifier: &OracleExcelImportFixedIdentifier{Enabled: true, Field: "BATCH_TAG", Value: "001"}}
			raw, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{{Operation: excelOperationExportMatch, Source: "upload", Name: "匹配"}, {Operation: ExcelOperationOracleImport, Source: "previous", Name: "导入", Config: raw}, {Operation: ExcelOperationOracleProcedure, Name: "过程"}}}
			state := &oracleOperationsTestState{}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			connection := oracleOperationsFakeConnection{db: db, columns: []reportoracle.ResultColumn{{Name: "ORDER_CODE", DataType: "VARCHAR2"}, {Name: "STORE_NAME", DataType: "VARCHAR2"}, {Name: "BATCH_TAG", DataType: "VARCHAR2"}}}
			lookup := &fakeExcelMatchLookup{value: map[string]string{"A": "匹配店A", "B": "匹配店B"}}
			var completed []int
			procedureCalls := 0
			result, err := runExcelWorkflowFiles(t.Context(), config, []string{first, second}, func(ctx context.Context, index int, step ExcelWorkflowStep, sources []string) ([]string, error) {
				switch index {
				case 0:
					return processExcelMatchFiles(ctx, sources, filepath.Join(t.TempDir(), "matched"), matching, lookup, nil)
				case 1:
					if !reflect.DeepEqual(completed, []int{1}) || len(sources) != 2 {
						t.Fatalf("import started too early or dropped files: completed=%v sources=%v", completed, sources)
					}
					for _, source := range sources {
						if _, err := os.Stat(source); err != nil {
							t.Fatal(err)
						}
					}
					input, err := excelWorkflowImportConfig(step)
					if err != nil {
						return nil, err
					}
					if !input.AllSheets || input.HeaderRow != 1 {
						t.Fatalf("handoff=%+v", input)
					}
					count, err := executeOracleExcelImportFilesOnConnection(ctx, connection, sources, input, oracleImportTestIdentifier)
					if err == nil && count != 2 {
						t.Fatalf("imported rows=%d", count)
					}
					return nil, err
				default:
					if !reflect.DeepEqual(completed, []int{1, 2}) || state.commits != 1 {
						t.Fatalf("procedure ran before import committed: completed=%v state=%+v", completed, state)
					}
					procedureCalls++
					return nil, nil
				}
			}, func(done int) error { completed = append(completed, done); return nil })
			if test.badSecondFile {
				if err == nil || state.commits != 0 || len(state.rows) != 0 || procedureCalls != 0 || len(completed) != 0 {
					t.Fatalf("continued after match failure: err=%v state=%+v completed=%v", err, state, completed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 2 || procedureCalls != 1 || len(state.rows) != 1 || len(state.rows[0]) != 6 || len(state.rawRows[0]) != 3 {
				t.Fatalf("result=%v state=%+v calls=%d", result, state, procedureCalls)
			}
			if !reflect.DeepEqual(state.rawRows[0][0].Value, []string{"A", "B"}) || state.statements[0] != state.statement {
				t.Fatalf("workflow did not bind all workbooks to one prepared insert: raw=%v SQL=%s", state.rawRows[0], state.statements[0])
			}
			want := []interface{}{"A", "匹配店A", "42", "B", "匹配店B", "42"}
			for index, value := range state.rows[0] {
				if value.Value != want[index] {
					t.Fatalf("bound[%d]=%v, want %v", index, value.Value, want[index])
				}
			}
			artifact, err := excelWorkflowResultFile(t.TempDir(), result)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.OpenReader(artifact)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			if len(archive.File) != 2 || archive.File[0].Name != "matched_001.xlsx" || archive.File[1].Name != "matched_002.xlsx" {
				t.Fatalf("archive dropped workbooks: %+v", archive.File)
			}
		})
	}
}

func TestExcelMatchFilesNeverReuseOrderItemAcrossFiles(t *testing.T) {
	paths := make([]string, 2)
	for index := range paths {
		paths[index] = oracleImportWorkbook(t, "source.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "规格", "单价", "数量"}, {"ORDER-1", "C09H1073", "319.20", "1"}}})
	}
	config, err := normalizeExcelMatchConfig(ExcelMatchConfig{SheetName: "Sheet1", Steps: []ExcelMatchStep{{MatchMode: excelMatchModeOrderItemSKU, TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号", SpecExcelColumn: "规格", PriceExcelColumn: "单价", QtyExcelColumn: "数量", DBMatchField: "docno", DBValueField: "items_json", OutputColumnName: "完整SKU"}}})
	if err != nil {
		t.Fatal(err)
	}
	lookup := &fakeExcelMatchLookup{value: map[string]string{"ORDER-1": `[{"no":"SKU-001","qty":1,"totAmtActual":319.2,"mProductName":"C09H1073A"}]`}}
	output, err := processExcelMatchFiles(t.Context(), paths, t.TempDir(), config, lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, path := range output {
		config := OracleExcelImportConfig{SheetName: defaultExcelResultSheet, HeaderRow: 1, Mappings: []OracleExcelImportMapping{{ExcelColumn: "完整SKU", Field: "SKU"}}}
		var sku string
		_, err := readOracleExcelImport(t.Context(), path, config, []reportoracle.ResultColumn{{Name: "SKU", DataType: "VARCHAR2"}}, func(values []interface{}) error { sku = values[0].(string); return nil })
		if err != nil {
			t.Fatal(err)
		}
		want := "SKU-001"
		if index == 1 {
			want = ""
		}
		if sku != want {
			t.Fatalf("file %d reused SKU: got %q want %q", index, sku, want)
		}
	}
}
