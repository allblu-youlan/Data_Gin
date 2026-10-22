package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
