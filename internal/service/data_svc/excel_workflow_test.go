package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

func workflowTestConfig() ExcelWorkflowConfig {
	return ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{
		{Operation: excelOperationExportMatch, SchemeID: 1, Source: "upload", Name: "匹配"},
		{Operation: ExcelOperationOracleImport, SchemeID: 2, Source: "previous", Name: "导入"},
		{Operation: ExcelOperationOracleProcedure, SchemeID: 3, Name: "处理"},
		{Operation: excelOperationOracleExport, SchemeID: 4, Name: "导出"},
	}}
}

func TestExcelWorkflowSequentialFileHandoff(t *testing.T) {
	config := workflowTestConfig()
	var events []string
	result, err := runExcelWorkflow(context.Background(), config, "upload.xlsx", func(ctx context.Context, index int, step ExcelWorkflowStep, source string) (string, error) {
		events = append(events, step.Name)
		if index == 0 {
			if source != "upload.xlsx" {
				t.Fatal(source)
			}
			return "matched.xlsx", nil
		}
		if index == 1 && source != "matched.xlsx" {
			t.Fatalf("import source = %q", source)
		}
		if index == 3 {
			return "export.xlsx", nil
		}
		return "", nil
	}, func(index int) error { events = append(events, "完成"); return nil })
	if err != nil || result != "export.xlsx" {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if !reflect.DeepEqual(events, []string{"匹配", "完成", "导入", "完成", "处理", "完成", "导出", "完成"}) {
		t.Fatal(events)
	}
}

func TestExcelWorkflowStopsAfterProcedureFlagFailure(t *testing.T) {
	failure := errors.New("success flag mismatch")
	var executed, completed []int
	_, err := runExcelWorkflow(context.Background(), workflowTestConfig(), "upload.xlsx", func(ctx context.Context, index int, step ExcelWorkflowStep, source string) (string, error) {
		executed = append(executed, index)
		if index == 2 {
			return "", failure
		}
		return "matched.xlsx", nil
	}, func(index int) error { completed = append(completed, index); return nil })
	if !errors.Is(err, failure) || !reflect.DeepEqual(executed, []int{0, 1, 2}) || !reflect.DeepEqual(completed, []int{1, 2}) {
		t.Fatalf("executed=%v completed=%v err=%v", executed, completed, err)
	}
}

func TestExcelWorkflowStopsOnCancellationOrProgressFailure(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		_, err := runExcelWorkflow(ctx, workflowTestConfig(), "source", func(ctx context.Context, index int, step ExcelWorkflowStep, source string) (string, error) {
			calls++
			if cancelled {
				cancel()
			}
			return "output", nil
		}, func(index int) error {
			if !cancelled {
				return errors.New("cannot persist progress")
			}
			return nil
		})
		cancel()
		if err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestExcelWorkflowValidatesSourcesAndReferences(t *testing.T) {
	config := workflowTestConfig()
	if _, err := validateExcelWorkflow(config); err != nil {
		t.Fatal(err)
	}
	cases := []ExcelWorkflowConfig{
		{},
		{Steps: []ExcelWorkflowStep{{Operation: ExcelOperationOracleImport, SchemeID: 2, Source: "previous"}}},
		{Steps: []ExcelWorkflowStep{{Operation: excelOperationExportMatch, SchemeID: 0}}},
		{Steps: []ExcelWorkflowStep{{Operation: "invalid", SchemeID: 1}}},
	}
	for _, input := range cases {
		if _, err := validateExcelWorkflow(input); err == nil {
			t.Fatalf("accepted %#v", input)
		}
	}
}

func TestExcelWorkflowImportsCompletedMatchWorkbook(t *testing.T) {
	for _, test := range []struct {
		name               string
		missingMatchColumn bool
	}{
		{name: "matched values are imported"},
		{name: "match error prevents import", missingMatchColumn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			uploadPath, matchedPath := filepath.Join(dir, "upload.xlsx"), filepath.Join(dir, "matched.xlsx")
			upload := excelize.NewFile()
			defer upload.Close()
			orderHeader := "订单号"
			if test.missingMatchColumn {
				orderHeader = "错误订单表头"
			}
			for index, row := range [][]interface{}{{orderHeader, "原始门店"}, {"001", "原文件门店A"}, {"002", "原文件门店B"}} {
				cell, err := excelize.CoordinatesToCellName(1, index+1)
				if err != nil {
					t.Fatal(err)
				}
				if err := upload.SetSheetRow(defaultExcelSheetName, cell, &row); err != nil {
					t.Fatal(err)
				}
			}
			if err := upload.SaveAs(uploadPath); err != nil {
				t.Fatal(err)
			}
			matchConfig, err := normalizeExcelMatchConfig(ExcelMatchConfig{
				SheetName: defaultExcelSheetName,
				Steps:     []ExcelMatchStep{{TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号", DBMatchField: "docno", DBValueField: "c_store_name", OutputColumnName: "匹配门店"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			// The saved method targets an uploaded workbook. Workflow handoff must
			// use the completed matching workbook's own sheet and header instead.
			importJSON, err := json.Marshal(OracleExcelImportConfig{Owner: "DEMO", TableName: "IMPORT_ORDERS", SheetName: defaultExcelSheetName, HeaderRow: 8, Mappings: []OracleExcelImportMapping{{ExcelColumn: "订单号", Field: "ORDER_CODE"}, {ExcelColumn: "匹配门店", Field: "MATCHED_STORE"}}})
			if err != nil {
				t.Fatal(err)
			}
			workflow := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{
				{Operation: excelOperationExportMatch, SchemeID: 1, Source: "upload", Name: "匹配门店"},
				{Operation: ExcelOperationOracleImport, SchemeID: 2, Source: "previous", Name: "导入匹配结果", Config: importJSON},
			}}
			lookup := &fakeExcelMatchLookup{value: map[string]string{"001": "匹配后的门店A", "002": "匹配后的门店B"}}
			var executed []string
			var completed []int
			var written [][]interface{}
			result, err := runExcelWorkflow(t.Context(), workflow, uploadPath, func(ctx context.Context, index int, step ExcelWorkflowStep, source string) (string, error) {
				executed = append(executed, step.Operation)
				if index == 0 {
					if source != uploadPath {
						t.Fatalf("match source=%q", source)
					}
					_, err := processExcelMatchFile(ctx, source, matchedPath, matchConfig, lookup)
					return matchedPath, err
				}
				if source != matchedPath || !reflect.DeepEqual(completed, []int{1}) {
					t.Fatalf("import started before match completion or used the uploaded file: source=%q completed=%v", source, completed)
				}
				config, err := excelWorkflowImportConfig(step)
				if err != nil {
					return "", err
				}
				if config.SheetName != defaultExcelResultSheet || config.HeaderRow != 1 {
					t.Fatalf("import handoff kept the uploaded workbook layout: %+v", config)
				}
				config, _, columns, err := buildOracleExcelImport(config, []reportoracle.ResultColumn{{Name: "ORDER_CODE", DataType: "VARCHAR2"}, {Name: "MATCHED_STORE", DataType: "VARCHAR2"}})
				if err != nil {
					return "", err
				}
				count, err := readOracleExcelImport(ctx, source, config, columns, func(values []interface{}) error {
					written = append(written, append([]interface{}(nil), values...))
					return nil
				})
				if err == nil && count != 2 {
					t.Fatalf("imported rows=%d", count)
				}
				return "", err
			}, func(index int) error { completed = append(completed, index); return nil })
			if test.missingMatchColumn {
				if err == nil || len(written) != 0 || len(completed) != 0 || !reflect.DeepEqual(executed, []string{excelOperationExportMatch}) {
					t.Fatalf("failed matching continued to import: executed=%v completed=%v written=%v err=%v", executed, completed, written, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := [][]interface{}{{"001", "匹配后的门店A"}, {"002", "匹配后的门店B"}}
			if result != matchedPath || !reflect.DeepEqual(written, want) || !reflect.DeepEqual(completed, []int{1, 2}) {
				t.Fatalf("result=%q completed=%v imported=%v", result, completed, written)
			}
			if !reflect.DeepEqual(lookup.keys, []string{"001", "002"}) {
				t.Fatalf("matching was not actually executed: keys=%v", lookup.keys)
			}
			original, err := excelize.OpenFile(uploadPath)
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			originalRows, err := original.GetRows(defaultExcelSheetName)
			if err != nil || !reflect.DeepEqual(originalRows, [][]string{{"订单号", "原始门店"}, {"001", "原文件门店A"}, {"002", "原文件门店B"}}) {
				t.Fatalf("uploaded file changed instead of handing off the matched workbook: rows=%v err=%v", originalRows, err)
			}
		})
	}
}
