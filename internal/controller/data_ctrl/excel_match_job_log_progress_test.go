package data_ctrl

import (
	"encoding/json"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestSafeExcelMatchJobLogsExposeOnlyAllowlistedImportProgress(t *testing.T) {
	logs := safeExcelMatchJobLogs([]model.ExcelMatchJobLog{{
		BaseModel: model.BaseModel{ID: 19}, JobID: 17, Message: "Oracle 导入进度",
		DetailJSON: `{"oracleImportProgress":{"stepIndex":2,"stepName":"导入订单","stage":"importing","processedRows":50,"totalRows":100,"totalKnown":true,"scannedRows":100,"fileIndex":1,"fileCount":2,"fileName":"/private/jobs/source/orders.xlsx","sheetName":"数据","rowNumber":51,"identifier":"42","password":"private-progress"},"sql":"private-sql","token":"private-detail"}`,
	}})
	if len(logs) != 1 || logs[0].ImportProgress == nil {
		t.Fatalf("progress was omitted: %+v", logs)
	}
	progress := logs[0].ImportProgress
	if progress.StepIndex != 2 || progress.StepName != "导入订单" || progress.Stage != "importing" || progress.ProcessedRows != 50 ||
		progress.TotalRows != 100 || !progress.TotalKnown || progress.ScannedRows != 100 || progress.FileIndex != 1 || progress.FileCount != 2 ||
		progress.FileName != "orders.xlsx" || progress.SheetName != "数据" || progress.RowNumber != 51 || progress.Identifier != "42" {
		t.Fatalf("progress values changed: %+v", progress)
	}
	encoded, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	response := string(encoded)
	for _, private := range []string{"private-progress", "private-sql", "private-detail", "/private/jobs", "detail_json", "oracleImportProgress", "password", "token"} {
		if strings.Contains(response, private) {
			t.Fatalf("progress exposed %q: %s", private, response)
		}
	}
	if !strings.Contains(response, `"import_progress":`) || !strings.Contains(response, `"processedRows":50`) {
		t.Fatalf("progress JSON contract changed: %s", response)
	}
}

func TestSafeExcelMatchJobLogsOmitMalformedImportProgress(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"not JSON", `broken`},
		{"no progress", `{"sql":"private-query"}`},
		{"null progress", `{"oracleImportProgress":null}`},
		{"unknown stage", `{"oracleImportProgress":{"stage":"raw_sql"}}`},
		{"negative count", `{"oracleImportProgress":{"stage":"importing","processedRows":-1}}`},
		{"negative file", `{"oracleImportProgress":{"stage":"counting","fileIndex":-1}}`},
		{"wrong type", `{"oracleImportProgress":{"stage":"importing","totalRows":"100"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			logs := safeExcelMatchJobLogs([]model.ExcelMatchJobLog{{Message: "进度已记录", DetailJSON: test.raw}})
			encoded, err := json.Marshal(logs)
			if err != nil || logs[0].ImportProgress != nil || strings.Contains(string(encoded), "import_progress") || logs[0].Message != "进度已记录" {
				t.Fatalf("invalid progress was exposed or hid its message: logs=%+v JSON=%s error=%v", logs, encoded, err)
			}
		})
	}
}

func TestSafeExcelMatchJobImportProgressTimingMetricsAreOptional(t *testing.T) {
	legacy := safeOracleExcelImportProgress(`{"oracleImportProgress":{"stage":"importing","processedRows":50}}`)
	if legacy == nil || legacy.ElapsedMillis != 0 || legacy.CountMillis != 0 || legacy.ReadMillis != 0 || legacy.WriteMillis != 0 || legacy.BatchRows != 0 || legacy.Batches != 0 {
		t.Fatalf("legacy progress was rejected or gained fabricated timings: %+v", legacy)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(encoded), "Millis") || strings.Contains(string(encoded), "batchRows") || strings.Contains(string(encoded), "batches") {
		t.Fatalf("legacy progress exposed absent metrics: %s error=%v", encoded, err)
	}
	progress := safeOracleExcelImportProgress(`{"oracleImportProgress":{"stage":"importing","elapsedMillis":4500,"countMillis":100,"readMillis":3000,"writeMillis":2000,"batchRows":1000,"batches":2,"unknownMillis":99}}`)
	if progress == nil || progress.ElapsedMillis != 4500 || progress.CountMillis != 100 || progress.ReadMillis != 3000 || progress.WriteMillis != 2000 || progress.BatchRows != 1000 || progress.Batches != 2 {
		t.Fatalf("timing metrics changed: %+v", progress)
	}
	encoded, err = json.Marshal(progress)
	if err != nil || strings.Contains(string(encoded), "unknownMillis") || !strings.Contains(string(encoded), `"writeMillis":2000`) || !strings.Contains(string(encoded), `"batches":2`) {
		t.Fatalf("timing metric allowlist changed: %s error=%v", encoded, err)
	}
}

func TestSafeExcelMatchJobImportProgressRejectsInvalidTimingMetrics(t *testing.T) {
	for _, key := range []string{"elapsedMillis", "countMillis", "readMillis", "writeMillis", "batchRows", "batches"} {
		t.Run(key, func(t *testing.T) {
			for _, invalid := range []string{"-1", "0.5", `"100"`} {
				raw := `{"oracleImportProgress":{"stage":"importing","` + key + `":` + invalid + `}}`
				if progress := safeOracleExcelImportProgress(raw); progress != nil {
					t.Fatalf("invalid %s=%s metric was exposed: %+v", key, invalid, progress)
				}
			}
		})
	}
}

func TestSafeExcelMatchJobImportProgressRedactsSensitiveText(t *testing.T) {
	logs := safeExcelMatchJobLogs([]model.ExcelMatchJobLog{{
		Message:    "token=private-message",
		DetailJSON: `{"oracleImportProgress":{"stage":"failed","stepName":"password=private-step","fileName":"C:\\private\\token=private-file.xlsx","sheetName":"api_key=private-sheet","identifier":"secret=private-identifier"}}`,
	}})
	encoded, err := json.Marshal(logs)
	if err != nil || logs[0].ImportProgress == nil {
		t.Fatalf("logs=%+v error=%v", logs, err)
	}
	if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), `C:\\private`) || !strings.Contains(string(encoded), "敏感内容已隐藏") {
		t.Fatalf("progress leaked sensitive values: %s", encoded)
	}
}
