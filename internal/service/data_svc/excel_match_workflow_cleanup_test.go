package data_svc

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/model"
	pkgconfig "gin-biz-web-api/pkg/config"
)

func TestExcelMatchJobCleanerDeletesGeneratedWorkflowResults(t *testing.T) {
	for _, test := range []struct {
		name         string
		prefix       string
		fileCount    int
		wantFilename string
	}{
		{name: "single workbook", fileCount: 1, wantFilename: "matched_001.xlsx"},
		{name: "multiple workbooks", fileCount: 2, wantFilename: "matched_results.zip"},
		{name: "single workbook with prefix", prefix: "warehouse/test", fileCount: 1, wantFilename: "matched_001.xlsx"},
		{name: "multiple workbooks with prefix", prefix: "warehouse/test", fileCount: 2, wantFilename: "matched_results.zip"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			configYAML := fmt.Sprintf("Storage:\n  OSS:\n    Prefix: %q\n", "/"+test.prefix)
			if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			pkgconfig.NewConfig("", configDir+string(os.PathSeparator))

			matching, err := normalizeExcelMatchConfig(ExcelMatchConfig{
				SheetName: "Sheet1",
				Steps: []ExcelMatchStep{{
					TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号",
					DBMatchField: "docno", DBValueField: "c_store_name", OutputColumnName: "匹配门店",
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var sources []string
			for range test.fileCount {
				sources = append(sources, oracleImportWorkbook(t, "source.xlsx", oracleImportSheetFixture{
					name: "Sheet1", rows: [][]interface{}{{"订单号"}, {"ORDER-1"}},
				}))
			}
			workDir := t.TempDir()
			lookup := &fakeExcelMatchLookup{value: map[string]string{"ORDER-1": "测试门店"}}
			outputs, err := processExcelMatchFiles(t.Context(), sources, filepath.Join(workDir, "match_1"), matching, lookup, nil)
			if err != nil {
				t.Fatal(err)
			}
			resultPath, err := excelWorkflowResultFile(workDir, outputs)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(resultPath) != test.wantFilename {
				t.Fatalf("generated result=%q, want filename %q", resultPath, test.wantFilename)
			}
			now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
			objectKey := filepath.ToSlash(filepath.Join(filepath.Dir(excelMatchResultObjectKey(17, now)), filepath.Base(resultPath)))
			wantKey := path.Join(test.prefix, "excel-match-results/2026/07/26/17", test.wantFilename)
			if objectKey != wantKey {
				t.Fatalf("generated object key=%q, want %q", objectKey, wantKey)
			}
			store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
				BaseModel: model.BaseModel{ID: 17}, Status: excelMatchStatusSuccess, ResultObjectKey: objectKey,
			}}}
			objects := &fakeExcelMatchCleanupObjectStore{}
			cleaner := newTestExcelMatchJobCleaner(store, objects)
			cleaner.objectKeyPrefix = path.Join(test.prefix, "excel-match-results")

			result, err := cleaner.Cleanup(t.Context())
			if err != nil {
				t.Fatalf("Cleanup() rejected generated result %q: %v", objectKey, err)
			}
			if result != (ExcelMatchCleanupResult{Scanned: 1, Claimed: 1, Deleted: 1, Expired: 1}) ||
				!reflect.DeepEqual(objects.deleted, []string{wantKey}) ||
				!reflect.DeepEqual(store.finished, []finishedExcelMatchCleanup{{jobID: 17, objectKey: wantKey}}) || len(store.released) != 0 {
				t.Fatalf("cleanup result=%+v deleted=%v finished=%v released=%v", result, objects.deleted, store.finished, store.released)
			}
		})
	}
}
