package data_svc

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"github.com/xuri/excelize/v2"
)

func TestReportExportRendererWritesFrozenHeadersTypesAndSafeText(t *testing.T) {
	pager := &fakeReportExportPager{pages: []reportoracle.ResultPage{{
		Columns: []string{"CODE", "AMOUNT", "CREATED_AT", "SECRET"},
		Rows:    []reportoracle.ResultRow{{Key: "ROW-A", Values: []interface{}{"=2+3", "9007199254740993", time.Date(2026, 8, 12, 12, 30, 0, 0, time.UTC), "private"}}},
		NextKey: "ROW-A",
	}}}
	output := filepath.Join(t.TempDir(), "report.xlsx")
	renderer := NewReportExportRenderer(pager)
	result, err := renderer.Render(t.Context(), ReportExportRenderRequest{
		OutputPath: output,
		Columns: []frozenResultColumn{
			{FieldID: "1", LogicalCode: "code", DatabaseColumn: "CODE", ExcelHeader: "编码", ValueType: "string", ExportVisible: true, ExportAllowed: true},
			{FieldID: "2", LogicalCode: "amount", DatabaseColumn: "AMOUNT", ExcelHeader: "金额", ValueType: "integer", ExportVisible: true, ExportAllowed: true},
			{FieldID: "3", LogicalCode: "createdAt", DatabaseColumn: "CREATED_AT", ExcelHeader: "时间", ValueType: "datetime", ExportVisible: true, ExportAllowed: true},
			{FieldID: "4", LogicalCode: "secret", DatabaseColumn: "SECRET", ExcelHeader: "敏感", ValueType: "string", MaskingPolicy: []byte(`{"type":"full"}`), ExportVisible: true, ExportAllowed: true},
		},
	}, nil)
	if err != nil || result.ProcessedRows != 1 || result.SheetCount != 1 {
		t.Fatalf("Render() result=%#v error=%v", result, err)
	}
	workbook, err := excelize.OpenFile(output)
	if err != nil {
		t.Fatalf("OpenFile() error=%v", err)
	}
	defer workbook.Close()
	rows, err := workbook.GetRows("数据")
	if err != nil || len(rows) != 2 || strings.Join(rows[0], ",") != "编码,金额,时间,敏感" {
		t.Fatalf("rows=%#v error=%v", rows, err)
	}
	if rows[1][0] != "=2+3" || rows[1][1] != "9007199254740993" || rows[1][3] != "***" {
		t.Fatalf("data row=%#v", rows[1])
	}
	formula, err := workbook.GetCellFormula("数据", "A2")
	if err != nil || formula != "" {
		t.Fatalf("formula=%q error=%v", formula, err)
	}
}

func TestReportExportRendererDecimalNumberFormat(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		numberFormat  string
		wantFormatted string
	}{
		{name: "default keeps available decimal places", value: "12.3456", wantFormatted: "12.3456"},
		{name: "requested format displays two decimal places", value: "12.5", numberFormat: "0.00", wantFormatted: "12.50"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pager := &fakeReportExportPager{pages: []reportoracle.ResultPage{{
				Columns: []string{"AMOUNT"},
				Rows:    []reportoracle.ResultRow{{Key: "ROW-A", Values: []interface{}{test.value}}},
				NextKey: "ROW-A",
			}}}
			output := filepath.Join(t.TempDir(), "decimal.xlsx")
			renderer := NewReportExportRenderer(pager)
			_, err := renderer.Render(t.Context(), ReportExportRenderRequest{
				OutputPath: output, decimalNumberFormat: test.numberFormat,
				Columns: []frozenResultColumn{{
					FieldID: "1", LogicalCode: "amount", DatabaseColumn: "AMOUNT", ExcelHeader: "金额",
					ValueType: "decimal", ExportVisible: true, ExportAllowed: true,
				}},
			}, nil)
			if err != nil {
				t.Fatalf("Render() error=%v", err)
			}
			workbook, err := excelize.OpenFile(output)
			if err != nil {
				t.Fatalf("OpenFile() error=%v", err)
			}
			defer workbook.Close()
			formatted, err := workbook.GetCellValue("数据", "A2")
			if err != nil || formatted != test.wantFormatted {
				t.Fatalf("GetCellValue()=%q error=%v, want %q", formatted, err, test.wantFormatted)
			}
		})
	}
}

func TestReportExportRendererRejectsNonAdvancingCursor(t *testing.T) {
	pager := &fakeReportExportPager{pages: []reportoracle.ResultPage{{
		Columns: []string{"CODE"}, Rows: []reportoracle.ResultRow{{Key: "ROW-A", Values: []interface{}{"a"}}}, NextKey: "ROW-A", HasNext: true,
	}, {
		Columns: []string{"CODE"}, Rows: []reportoracle.ResultRow{{Key: "ROW-A", Values: []interface{}{"b"}}}, NextKey: "ROW-A",
	}}}
	renderer := NewReportExportRenderer(pager)
	_, err := renderer.Render(t.Context(), ReportExportRenderRequest{
		OutputPath: filepath.Join(t.TempDir(), "report.xlsx"),
		Columns:    []frozenResultColumn{{FieldID: "1", LogicalCode: "code", DatabaseColumn: "CODE", ExcelHeader: "编码", ExportVisible: true, ExportAllowed: true}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "non-advancing") {
		t.Fatalf("Render() error=%v", err)
	}
}

func TestFrozenExportColumnsUsesOnlyAllowedVisibleMapping(t *testing.T) {
	columns, err := frozenExportColumns(model.JSONText(`[
		{"fieldId":"1","logicalCode":"a","databaseColumn":"A","excelHeader":"甲","exportVisible":true,"exportAllowed":true},
		{"fieldId":"2","logicalCode":"b","databaseColumn":"B","excelHeader":"乙","exportVisible":true,"exportAllowed":false},
		{"fieldId":"3","logicalCode":"c","databaseColumn":"C","excelHeader":"丙","exportVisible":false,"exportAllowed":true}
	]`))
	if err != nil || len(columns) != 1 || columns[0].DatabaseColumn != "A" || columns[0].ExcelHeader != "甲" {
		t.Fatalf("frozenExportColumns()=%#v error=%v", columns, err)
	}
}

type fakeReportExportPager struct {
	pages []reportoracle.ResultPage
	calls int
}

func (pager *fakeReportExportPager) Read(_ context.Context, _ []string, _ *reportoracle.ResultCursor, _ int) (reportoracle.ResultPage, error) {
	page := pager.pages[pager.calls]
	pager.calls++
	return page, nil
}
