package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"github.com/godror/godror"
	"github.com/xuri/excelize/v2"
)

func oracleExcelTestSchema() []reportoracle.ResultColumn {
	return []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, {Name: "AMOUNT", DataType: "NUMBER"}, {Name: "CREATED_AT", DataType: "DATE"}}
}

func TestOracleExcelExportUsesColumnCommentsAndPreservesCustomTitles(t *testing.T) {
	schema := []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2", Comment: " 订单编号 "}, {Name: "AMOUNT", DataType: "NUMBER", Comment: " "}}
	config, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS"}, schema)
	if err != nil || config.Columns[0].Title != "订单编号" || config.Columns[1].Title != "AMOUNT" {
		t.Fatalf("default titles=%+v error=%v", config.Columns, err)
	}
	output, _, err := renderOracleExcelTestRows(t.Context(), &fakeOracleExcelRows{}, config, schema, t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenFile(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := file.GetRows("数据")
	if err != nil || len(rows) != 1 || strings.Join(rows[0], ",") != "订单编号,AMOUNT" {
		t.Fatalf("workbook headers=%+v error=%v", rows, err)
	}
	for _, test := range []struct{ title, want string }{{"", "订单编号"}, {" ", "订单编号"}, {"自定义编号", "自定义编号"}, {"CODE", "CODE"}} {
		t.Run("title_"+test.title, func(t *testing.T) {
			config, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: test.title}}}, schema)
			if err != nil || config.Columns[0].Title != test.want {
				t.Fatalf("title=%s columns=%+v error=%v", test.title, config.Columns, err)
			}
		})
	}
}

func TestOracleExcelExportDefaultsAndBoundFilters(t *testing.T) {
	config, statement, args, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "demo", TableName: "orders"}, oracleExcelTestSchema())
	if err != nil || len(config.Columns) != 3 || config.Columns[0].Title != "CODE" || len(args) != 0 || strings.Contains(statement, "WHERE") {
		t.Fatalf("defaults: config=%#v SQL=%s args=%v err=%v", config, statement, args, err)
	}
	config, statement, args, err = buildOracleExcelExportQuery(OracleExcelExportConfig{
		Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "code", Title: "编码"}},
		Filters: []OracleExcelExportFilter{{Field: "CODE", Op: "contains", Value: "%' OR 1=1 --"}, {Field: "AMOUNT", Op: "gte", Value: "123456789012345678.12"}, {Field: "CREATED_AT", Op: "lt", Value: "2026-10-21"}},
	}, oracleExcelTestSchema())
	if err != nil || config.Columns[0].Title != "编码" || len(args) != 3 || strings.Contains(statement, "OR 1=1") {
		t.Fatalf("filters: config=%#v SQL=%s args=%v err=%v", config, statement, args, err)
	}
	if !strings.Contains(statement, `INSTR("CODE", :filter_0) > 0 AND "AMOUNT" >= :filter_1 AND "CREATED_AT" < :filter_2`) {
		t.Fatalf("unexpected SQL: %s", statement)
	}
	if args[1].(sql.NamedArg).Value != godror.Number("123456789012345678.12") {
		t.Fatalf("numeric bind lost precision: %#v", args[1])
	}
	if _, ok := args[2].(sql.NamedArg).Value.(time.Time); !ok {
		t.Fatalf("date was not bound as time: %#v", args[2])
	}
}

func TestOracleExcelExportRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		edit func(*OracleExcelExportConfig)
	}{
		{"SQL table", func(c *OracleExcelExportConfig) { c.TableName = "ORDERS;DROP TABLE X" }},
		{"unknown column", func(c *OracleExcelExportConfig) { c.Columns = []OracleExcelExportColumn{{Field: "MISSING"}} }},
		{"duplicate column", func(c *OracleExcelExportConfig) {
			c.Columns = []OracleExcelExportColumn{{Field: "CODE"}, {Field: "code"}}
		}},
		{"unknown filter", func(c *OracleExcelExportConfig) {
			c.Filters = []OracleExcelExportFilter{{Field: "MISSING", Op: "empty"}}
		}},
		{"operator", func(c *OracleExcelExportConfig) {
			c.Filters = []OracleExcelExportFilter{{Field: "CODE", Op: "eq OR 1=1", Value: "a"}}
		}},
		{"bad number", func(c *OracleExcelExportConfig) {
			c.Filters = []OracleExcelExportFilter{{Field: "AMOUNT", Op: "gte", Value: "bad"}}
		}},
		{"bad date", func(c *OracleExcelExportConfig) {
			c.Filters = []OracleExcelExportFilter{{Field: "CREATED_AT", Op: "gte", Value: "not-a-date"}}
		}},
		{"contains number", func(c *OracleExcelExportConfig) {
			c.Filters = []OracleExcelExportFilter{{Field: "AMOUNT", Op: "contains", Value: "1"}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS"}
			test.edit(&input)
			if _, _, _, err := buildOracleExcelExportQuery(input, oracleExcelTestSchema()); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

func TestOracleExcelExportWorkbookAndBalancedSheets(t *testing.T) {
	if oracleExcelRowsPerSheet != 1_000_000 {
		t.Fatal("sheet limit must be one million data rows")
	}
	for _, test := range []struct {
		name  string
		count int
		sizes []int
	}{{"empty", 0, []int{0}}, {"boundary", 3, []int{3}}, {"even split", 4, []int{2, 2}}, {"remainder", 5, []int{3, 2}}, {"three sheets", 8, []int{3, 3, 2}}} {
		t.Run(test.name, func(t *testing.T) {
			config, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: "编码"}, {Field: "AMOUNT"}}}, oracleExcelTestSchema())
			if err != nil {
				t.Fatal(err)
			}
			path, total, err := renderOracleExcelTestRows(t.Context(), &fakeOracleExcelRows{remaining: test.count}, config, oracleExcelTestSchema(), t.TempDir(), 3, nil)
			if err != nil || total != test.count || filepath.Ext(path) != ".xlsx" {
				t.Fatalf("render path=%s total=%d err=%v", path, total, err)
			}
			file, err := excelize.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if len(file.GetSheetList()) != len(test.sizes) {
				t.Fatalf("sheets=%v sizes=%v", file.GetSheetList(), test.sizes)
			}
			for part, count := range test.sizes {
				name := "数据"
				if len(test.sizes) > 1 {
					name = fmt.Sprintf("数据_%d", part+1)
				}
				data, err := file.GetRows(name)
				if err != nil || len(data) != count+1 || strings.Join(data[0], ",") != "编码,AMOUNT" {
					t.Fatalf("sheet=%s rows=%v err=%v", name, data, err)
				}
				for index, row := range data[1:] {
					if row[0] != "=2+3" || row[1] != "123456789012345678.12" {
						t.Fatalf("data changed: %v", row)
					}
					formula, _ := file.GetCellFormula(name, fmt.Sprintf("A%d", index+2))
					if formula != "" {
						t.Fatalf("text interpreted as formula: %s", formula)
					}
				}
			}
		})
	}
}

func renderOracleExcelTestRows(ctx context.Context, rows oracleExcelRows, config OracleExcelExportConfig, schema []reportoracle.ResultColumn, dir string, limit int, progress func(int) error) (string, int, error) {
	spool := filepath.Join(dir, "sheet-data")
	defer os.RemoveAll(spool)
	sheet := OracleExcelExportSheet{SheetName: "数据", Owner: config.Owner, TableName: config.TableName, Columns: config.Columns, Filters: config.Filters}
	snapshot, err := spoolOracleExcelSheet(ctx, rows, sheet, schema, spool, progress)
	if err != nil {
		return "", snapshot.Rows, err
	}
	return renderOracleExcelSnapshots(ctx, []oracleExcelSheetSnapshot{snapshot}, dir, limit, progress)
}

func TestOracleExcelExportPropagatesReadFailure(t *testing.T) {
	want := errors.New("Oracle read failed")
	rows := &fakeOracleExcelRows{err: want}
	_, _, err := renderOracleExcelTestRows(t.Context(), rows, OracleExcelExportConfig{TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: "CODE"}}}, oracleExcelTestSchema(), t.TempDir(), 2, nil)
	if !errors.Is(err, want) {
		t.Fatalf("render error=%v", err)
	}
}

func TestOracleExcelExportDownloadFilenames(t *testing.T) {
	for _, extension := range []string{".xlsx", ".zip"} {
		t.Run(extension, func(t *testing.T) {
			export := &model.ExcelMatchJob{BaseModel: model.BaseModel{ID: 17}, ConfigJSON: `{"operation":"oracle_export"}`, ResultObjectKey: "excel-match-results/2026/10/21/17/result" + extension}
			if name := excelJobResultFilename(export); name != "oracle_export_17"+extension {
				t.Fatalf("filename=%s", name)
			}
			service := &ExcelMatchJobService{}
			export.Status, export.ResultURL = "success", "https://example.com/result"+extension
			service.refreshDownloadState(t.Context(), export)
			if !export.CanDownload {
				t.Fatalf("Oracle export is not downloadable: %+v", export)
			}
		})
	}
}

func TestOracleExcelExportQueueRecoveryClaimsRunningImmediately(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if before := oracleExcelExportClaimBefore("oracle-export:17", 17, now); !before.Equal(now) {
		t.Fatal("re-delivered queue task cannot recover its running job")
	}
	for _, taskID := range []string{"", "unrelated-task", "oracle-export:18"} {
		if before := oracleExcelExportClaimBefore(taskID, 17, now); !before.Equal(now.Add(-excelMatchRunningStaleAfter)) {
			t.Fatalf("task %q bypassed the normal running-job guard", taskID)
		}
	}
}

func TestOracleExcelExportDateTimeRangeBindsAndNormalizes(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		value    string
		want     string
	}{
		{"date seconds", "DATE", "2026-10-21 10:30:15", "2026-10-21 10:30:15"},
		{"date input minutes", "DATE", "2026-10-21T10:30", "2026-10-21 10:30:00"},
		{"timestamp input seconds", "TIMESTAMP(6)", "2026-10-21T10:30:15", "2026-10-21 10:30:15"},
		{"date only", "DATE", "2026-10-21", "2026-10-21"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := []reportoracle.ResultColumn{{Name: "CREATED_AT", DataType: test.dataType}}
			config, statement, args, err := buildOracleExcelExportQuery(OracleExcelExportConfig{
				Owner: "DEMO", TableName: "ORDERS",
				Filters: []OracleExcelExportFilter{{Field: "CREATED_AT", Op: "gte", Value: test.value}, {Field: "CREATED_AT", Op: "lt", Value: "2026-10-22T00:00"}},
			}, schema)
			if err != nil {
				t.Fatal(err)
			}
			if config.Filters[0].Value != test.want || config.Filters[1].Value != "2026-10-22 00:00:00" || !strings.Contains(statement, `"CREATED_AT" >= :filter_0 AND "CREATED_AT" < :filter_1`) {
				t.Fatalf("config=%+v SQL=%s", config, statement)
			}
			for _, arg := range args {
				if _, ok := arg.(sql.NamedArg).Value.(time.Time); !ok {
					t.Fatalf("date filter was not bound as time: %+v", arg)
				}
			}
		})
	}
}

type fakeOracleExcelRows struct {
	remaining int
	err       error
}

func (rows *fakeOracleExcelRows) Next() bool {
	if rows.remaining == 0 {
		return false
	}
	rows.remaining--
	return true
}

func (rows *fakeOracleExcelRows) Scan(targets ...interface{}) error {
	*targets[0].(*interface{}) = "=2+3"
	*targets[1].(*interface{}) = "123456789012345678.12"
	return nil
}

func (rows *fakeOracleExcelRows) Err() error { return rows.err }
