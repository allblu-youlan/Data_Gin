package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExcelWorkflowIndependentUploadsAndExplicitMatchingStepSources(t *testing.T) {
	uploads := []string{"/uploads/source_a.xlsx", "/uploads/source_b.xlsx", "/uploads/unselected.xlsx"}
	aResults := []string{"/matched/a_001.xlsx", "/matched/a_002.xlsx"}
	bResults := []string{"/matched/b_001.xlsx"}
	config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{
		{ID: "match_a", Operation: excelOperationExportMatch, Source: "upload", SourceFiles: []string{"source_a.xlsx"}},
		{ID: "match_b", Operation: excelOperationExportMatch, Source: "upload", SourceFiles: []string{"source_b.xlsx"}},
		{ID: "write_a", Operation: excelOperationImportUpdate, Source: "previous", SourceStepID: "match_a"},
		{ID: "import_b", Operation: ExcelOperationOracleImport, Source: "previous", SourceStepID: "match_b"},
		{ID: "legacy_write", Operation: excelOperationImportUpdate, Source: "previous"},
	}}
	wantSources := [][]string{{uploads[0]}, {uploads[1]}, aResults, bResults, bResults}
	var completed []int
	result, err := runExcelWorkflowFiles(t.Context(), config, uploads, func(_ context.Context, index int, _ ExcelWorkflowStep, sources []string) ([]string, error) {
		if !reflect.DeepEqual(sources, wantSources[index]) {
			t.Fatalf("step %d mixed its source files: got=%v want=%v", index+1, sources, wantSources[index])
		}
		switch index {
		case 0:
			return aResults, nil
		case 1:
			return bResults, nil
		default:
			return nil, nil
		}
	}, func(index int) error { completed = append(completed, index); return nil })
	if err != nil || !reflect.DeepEqual(result, bResults) || !reflect.DeepEqual(completed, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("result=%v completed=%v error=%v", result, completed, err)
	}
	if !reflect.DeepEqual(uploads, []string{"/uploads/source_a.xlsx", "/uploads/source_b.xlsx", "/uploads/unselected.xlsx"}) {
		t.Fatal("step selection mutated the complete upload inventory")
	}
}

func TestExcelWorkflowMissingStepSourceStopsWithoutFallback(t *testing.T) {
	for _, test := range []struct {
		name    string
		step    ExcelWorkflowStep
		message string
	}{
		{name: "missing selected upload", step: ExcelWorkflowStep{Operation: excelOperationImportUpdate, Source: "upload", SourceFiles: []string{"missing.xlsx"}}, message: "missing.xlsx"},
		{name: "missing explicit matching step", step: ExcelWorkflowStep{Operation: excelOperationImportUpdate, Source: "previous", SourceStepID: "missing_step"}, message: "missing_step"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{{ID: "first_match", Operation: excelOperationExportMatch, Source: "upload"}, test.step, {Operation: ExcelOperationOracleProcedure}}}
			calls, completed := 0, 0
			_, err := runExcelWorkflowFiles(t.Context(), config, []string{"/uploads/available.xlsx"}, func(context.Context, int, ExcelWorkflowStep, []string) ([]string, error) {
				calls++
				return []string{"/matched/available.xlsx"}, nil
			}, func(int) error { completed++; return nil })
			if err == nil || !strings.Contains(err.Error(), test.message) || calls != 1 || completed != 1 {
				t.Fatalf("source error fell back to another file or continued: calls=%d completed=%d error=%v", calls, completed, err)
			}
		})
	}
}

func TestExcelWorkflowLegacySnapshotStillUsesAllUploads(t *testing.T) {
	uploads := []string{"/uploads/a.xlsx", "/uploads/b.xlsx"}
	config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{{Operation: excelOperationExportMatch, Source: "upload"}, {Operation: excelOperationImportUpdate, Source: "previous"}}}
	_, err := runExcelWorkflowFiles(t.Context(), config, uploads, func(_ context.Context, index int, _ ExcelWorkflowStep, sources []string) ([]string, error) {
		if !reflect.DeepEqual(sources, uploads) {
			t.Fatalf("legacy step %d lost a file: %v", index+1, sources)
		}
		if index == 0 {
			return sources, nil
		}
		return nil, nil
	}, func(int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}

func TestExcelWorkflowIndependentMatchingResultsWriteTheirOwnRows(t *testing.T) {
	first := oracleImportWorkbook(t, "source_a.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "原始值"}, {"A", "不能写入的原始A"}}})
	second := oracleImportWorkbook(t, "source_b.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "原始值"}, {"B", "不能写入的原始B"}}})
	matching, err := normalizeExcelMatchConfig(ExcelMatchConfig{SheetName: "Sheet1", Steps: []ExcelMatchStep{{TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号", DBMatchField: "docno", DBValueField: "c_store_name", OutputColumnName: "匹配结果"}}})
	if err != nil {
		t.Fatal(err)
	}
	matchRaw, err := json.Marshal(matching)
	if err != nil {
		t.Fatal(err)
	}
	writeRaw, err := json.Marshal(workflowWriteTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	workflow := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{
		{ID: "match_a", Operation: excelOperationExportMatch, Source: "upload", SourceFiles: []string{filepath.Base(first)}, Config: matchRaw},
		{ID: "match_b", Operation: excelOperationExportMatch, Source: "upload", SourceFiles: []string{filepath.Base(second)}, Config: matchRaw},
		{ID: "write_a", Operation: excelOperationImportUpdate, Source: "previous", SourceStepID: "match_a", Config: writeRaw},
		{ID: "write_b", Operation: excelOperationImportUpdate, Source: "previous", SourceStepID: "match_b", Config: writeRaw},
	}}
	lookup := &fakeExcelMatchLookup{value: map[string]string{"A": "匹配A", "B": "匹配B"}}
	updater := &workflowWriteTestUpdater{rows: map[string]map[string]string{"A": {}, "B": {}}}
	matched := make(map[string][]string)
	_, err = runExcelWorkflowFiles(t.Context(), workflow, []string{first, second}, func(ctx context.Context, _ int, step ExcelWorkflowStep, sources []string) ([]string, error) {
		var config ExcelMatchConfig
		if err := json.Unmarshal(step.Config, &config); err != nil {
			return nil, err
		}
		if step.Operation == excelOperationExportMatch {
			outputs, err := processExcelMatchFiles(ctx, sources, filepath.Join(t.TempDir(), step.ID), config, lookup, nil)
			matched[step.ID] = outputs
			return outputs, err
		}
		if !reflect.DeepEqual(sources, matched[step.SourceStepID]) {
			t.Fatalf("write %s consumed another match's files: %v", step.ID, sources)
		}
		_, err := processExcelWorkflowWriteFiles(ctx, sources, config, true, updater, nil)
		return nil, err
	}, func(int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if updater.rows["A"]["matched_docno"] != "匹配A" || updater.rows["B"]["matched_docno"] != "匹配B" || updater.writeCalls != 2 {
		t.Fatalf("independent matches wrote the wrong source data: %v", updater.rows)
	}
}

type workflowWriteTestUpdater struct {
	rows       map[string]map[string]string
	onlyEmpty  []bool
	writeCalls int
	failKey    string
}

func (updater *workflowWriteTestUpdater) FindWritableKeys(_ context.Context, _, field string, onlyEmpty, allowPendingToShop bool, keys []string) (map[string]struct{}, error) {
	updater.onlyEmpty = append(updater.onlyEmpty, onlyEmpty)
	result := make(map[string]struct{})
	for _, key := range keys {
		row, exists := updater.rows[key]
		if exists && (!onlyEmpty || row[field] == "") {
			result[key] = struct{}{}
		}
	}
	return result, nil
}

func (updater *workflowWriteTestUpdater) BatchUpdateFieldsByKeys(_ context.Context, _ string, values map[string]map[string]string, clearMatchedDocNo bool) (int64, error) {
	updater.writeCalls++
	if clearMatchedDocNo {
		return 0, errors.New("workflow write unexpectedly cleared existing data")
	}
	for _, byKey := range values {
		if _, exists := byKey[updater.failKey]; updater.failKey != "" && exists {
			return 0, errors.New("database rejected this file")
		}
	}
	var count int64
	for field, byKey := range values {
		for key, value := range byKey {
			updater.rows[key][field] = value
			count++
		}
	}
	return count, nil
}

func workflowWriteTestConfig() ExcelMatchConfig {
	return ExcelMatchConfig{Operation: excelOperationImportUpdate, SheetName: "原始配置工作表", MatchExcelColumn: "订单号", DBMatchField: "docno", WriteMappings: []ExcelImportWriteMapping{{DBWriteField: "matched_docno", WriteExcelColumn: "匹配结果"}}, DryRun: true, ConfirmWrite: false}
}

func TestExcelWorkflowWritePreviousFilesProcessesEveryResultSheetAndKeepsNonOverwriteRules(t *testing.T) {
	first := oracleImportWorkbook(t, "first_match.xlsx",
		oracleImportSheetFixture{name: "Result_1", rows: [][]interface{}{{"订单号", "匹配结果"}, {"EXISTING", "不应覆盖"}, {"A", "A-first"}}},
		oracleImportSheetFixture{name: "其他表", rows: [][]interface{}{{"错误表头"}, {"不应读取"}}},
		oracleImportSheetFixture{name: "Result_2", rows: [][]interface{}{{"订单号", "匹配结果"}, {"B", "B-value"}}},
	)
	second := oracleImportWorkbook(t, "second_match.xlsx",
		oracleImportSheetFixture{name: "Result_1", rows: [][]interface{}{{"订单号", "匹配结果"}, {"A", "A-second"}, {"C", "C-value"}}},
		oracleImportSheetFixture{name: "Result_2", rows: [][]interface{}{{"订单号", "匹配结果"}, {"D", "D-value"}}},
	)
	updater := &workflowWriteTestUpdater{rows: map[string]map[string]string{"EXISTING": {"matched_docno": "original"}, "A": {}, "B": {}, "C": {}, "D": {}}}
	var completed []string
	config := workflowWriteTestConfig()
	stats, err := processExcelWorkflowWriteFiles(t.Context(), []string{first, second}, config, true, updater, func(_, _ int, path, sheet string, _ ExcelMatchJobStats, complete bool) {
		if complete {
			completed = append(completed, filepath.Base(path)+":"+sheet)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"EXISTING": {"matched_docno": "original"}, "A": {"matched_docno": "A-first"}, "B": {"matched_docno": "B-value"}, "C": {"matched_docno": "C-value"}, "D": {"matched_docno": "D-value"}}
	if !reflect.DeepEqual(updater.rows, want) || stats.ProcessedRows != 6 || stats.MatchedRows != 4 || stats.UnmatchedRows != 2 {
		t.Fatalf("write/non-overwrite behavior changed: rows=%v stats=%+v", updater.rows, stats)
	}
	if !reflect.DeepEqual(completed, []string{"first_match.xlsx:Result_1", "first_match.xlsx:Result_2", "second_match.xlsx:Result_1", "second_match.xlsx:Result_2"}) {
		t.Fatalf("files/sheets were skipped or reordered: %v", completed)
	}
	for _, onlyEmpty := range updater.onlyEmpty {
		if !onlyEmpty {
			t.Fatal("write bypassed the existing empty-field policy")
		}
	}
	if !config.DryRun || config.ConfirmWrite {
		t.Fatal("runtime confirmation mutated the saved method")
	}
}

func TestExcelWorkflowWriteUploadUsesConfiguredSheetOnly(t *testing.T) {
	path := oracleImportWorkbook(t, "upload.xlsx", oracleImportSheetFixture{name: "订单数据", rows: [][]interface{}{{"订单号", "匹配结果"}, {"A", "from-selected-sheet"}}}, oracleImportSheetFixture{name: "Result_1", rows: [][]interface{}{{"订单号", "匹配结果"}, {"B", "must-not-write"}}})
	config := workflowWriteTestConfig()
	config.SheetName = "订单数据"
	updater := &workflowWriteTestUpdater{rows: map[string]map[string]string{"A": {}, "B": {}}}
	stats, err := processExcelWorkflowWriteFiles(t.Context(), []string{path}, config, false, updater, nil)
	if err != nil || stats.ProcessedRows != 1 || updater.rows["A"]["matched_docno"] != "from-selected-sheet" || updater.rows["B"]["matched_docno"] != "" {
		t.Fatalf("upload source mixed sheets: rows=%v stats=%+v error=%v", updater.rows, stats, err)
	}
}

func TestExcelWorkflowWriteFailureStopsLaterFilesAndSteps(t *testing.T) {
	var paths []string
	for _, key := range []string{"A", "B", "C"} {
		paths = append(paths, oracleImportWorkbook(t, key+".xlsx", oracleImportSheetFixture{name: "Result_1", rows: [][]interface{}{{"订单号", "匹配结果"}, {key, key + "-value"}}}))
	}
	updater := &workflowWriteTestUpdater{rows: map[string]map[string]string{"A": {}, "B": {}, "C": {}}, failKey: "B"}
	config := ExcelWorkflowConfig{Steps: []ExcelWorkflowStep{{Operation: excelOperationExportMatch, Source: "upload", ID: "match"}, {Operation: excelOperationImportUpdate, Source: "previous", SourceStepID: "match"}, {Operation: ExcelOperationOracleProcedure}}}
	var executed []string
	var completed []int
	_, err := runExcelWorkflowFiles(t.Context(), config, paths, func(ctx context.Context, _ int, step ExcelWorkflowStep, sources []string) ([]string, error) {
		executed = append(executed, step.Operation)
		if step.Operation == excelOperationExportMatch {
			return sources, nil
		}
		_, err := processExcelWorkflowWriteFiles(ctx, sources, workflowWriteTestConfig(), true, updater, nil)
		return nil, err
	}, func(index int) error { completed = append(completed, index); return nil })
	if err == nil || !strings.Contains(err.Error(), "B.xlsx") || !strings.Contains(err.Error(), "Result_1") || !reflect.DeepEqual(executed, []string{excelOperationExportMatch, excelOperationImportUpdate}) || !reflect.DeepEqual(completed, []int{1}) {
		t.Fatalf("write failure did not stop later execution: steps=%v completed=%v error=%v", executed, completed, err)
	}
	if updater.rows["A"]["matched_docno"] != "A-value" || updater.rows["B"]["matched_docno"] != "" || updater.rows["C"]["matched_docno"] != "" || updater.writeCalls != 2 {
		t.Fatalf("failed write continued into later files: rows=%v calls=%d", updater.rows, updater.writeCalls)
	}
}
