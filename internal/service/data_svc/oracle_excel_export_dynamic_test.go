package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"github.com/godror/godror"
	"github.com/xuri/excelize/v2"
)

func oracleExcelDynamicTestSchema() []reportoracle.ResultColumn {
	return []reportoracle.ResultColumn{
		{Name: "CODE", DataType: "VARCHAR2"},
		{Name: "BILLDATE", DataType: "NUMBER"},
		{Name: "AMOUNT", DataType: "NUMBER"},
		{Name: "CREATED_AT", DataType: "DATE"},
		{Name: "PRECISE_AT", DataType: "TIMESTAMP(6)"},
	}
}

func TestOracleExcelDynamicTimeBindingsUseTaskBeijingCalendar(t *testing.T) {
	for _, test := range []struct {
		name, field, op, kind, code, wantSQL string
		now                                  time.Time
		want                                 []string
	}{
		{name: "numeric current month after UTC boundary", field: "BILLDATE", op: "between", kind: "TIME_RANGE", code: "CURRENT_MONTH", now: time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), want: []string{"20261101", "20261130"}, wantSQL: `"BILLDATE" BETWEEN :filter_0_start AND :filter_0_end`},
		{name: "previous month crosses year", field: "BILLDATE", op: "between", kind: "TIME_RANGE", code: "PREVIOUS_MONTH", now: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), want: []string{"20251201", "20251231"}, wantSQL: `"BILLDATE" BETWEEN`},
		{name: "next leap month", field: "BILLDATE", op: "between", kind: "TIME_RANGE", code: "NEXT_MONTH", now: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC), want: []string{"20240201", "20240229"}, wantSQL: `"BILLDATE" BETWEEN`},
		{name: "numeric first day", field: "BILLDATE", op: "gte", kind: "TIME", code: "MONTH_START", want: []string{"20261001"}, wantSQL: `"BILLDATE" >= :filter_0`},
		{name: "numeric final day", field: "BILLDATE", op: "lte", kind: "TIME", code: "MONTH_END", want: []string{"20261031"}, wantSQL: `"BILLDATE" <= :filter_0`},
		{name: "DATE range includes final day", field: "CREATED_AT", op: "between", kind: "TIME_RANGE", code: "CURRENT_MONTH", want: []string{"2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"}, wantSQL: `("CREATED_AT" >= :filter_0_start AND "CREATED_AT" < :filter_0_end)`},
		{name: "timestamp range includes fractional seconds", field: "PRECISE_AT", op: "between", kind: "TIME_RANGE", code: "CURRENT_MONTH", want: []string{"2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"}, wantSQL: `"PRECISE_AT" < :filter_0_end`},
		{name: "DATE final day upper bound", field: "CREATED_AT", op: "lte", kind: "TIME", code: "MONTH_END", want: []string{"2026-11-01T00:00:00+08:00"}, wantSQL: `"CREATED_AT" < :filter_0`},
		{name: "timestamp now keeps seconds", field: "PRECISE_AT", op: "lte", kind: "TIME", code: "NOW", now: time.Date(2026, 10, 22, 12, 34, 56, 0, time.UTC), want: []string{"2026-10-22T20:34:56+08:00"}, wantSQL: `"PRECISE_AT" <= :filter_0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.now.IsZero() {
				test.now = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
			}
			input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{{Field: test.field, Op: test.op, ValueSource: &OracleExcelExportValueSource{Kind: test.kind, Code: test.code}}}}
			config, statement, arguments, err := buildOracleExcelExportQueryAt(input, oracleExcelDynamicTestSchema(), test.now)
			if err != nil || !strings.Contains(statement, test.wantSQL) || len(arguments) != len(test.want) {
				t.Fatalf("SQL=%s args=%v error=%v", statement, arguments, err)
			}
			if !reflect.DeepEqual(config.Filters[0].ValueSource, input.Filters[0].ValueSource) || config.Filters[0].Value != "" {
				t.Fatal("normalization froze or discarded the saved dynamic source")
			}
			for index, argument := range arguments {
				value := argument.(sql.NamedArg).Value
				actual := ""
				switch typed := value.(type) {
				case godror.Number:
					actual = string(typed)
				case time.Time:
					actual = typed.Format(time.RFC3339)
				default:
					t.Fatalf("unexpected dynamic binding type %T", value)
				}
				if actual != test.want[index] {
					t.Fatalf("binding=%s want=%s", actual, test.want[index])
				}
			}
		})
	}
}

func TestOracleExcelMaximumUsesAllOtherFiltersAndKeepsProjection(t *testing.T) {
	input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE"}}, Filters: []OracleExcelExportFilter{
		{Field: "BILLDATE", Op: "between", ValueSource: &OracleExcelExportValueSource{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}},
		{Field: "CODE", Op: "contains", Value: "%' OR 1=1--"},
		{Field: "AMOUNT", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX"}},
	}}
	_, statement, arguments, err := buildOracleExcelExportQueryAt(input, oracleExcelDynamicTestSchema(), time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`SELECT "EXCEL_VALUE_001" AS "CODE" FROM (`, `MAX("AMOUNT") OVER () AS "EXCEL_MAXIMUM"`, `WHERE "BILLDATE" BETWEEN :filter_0_start AND :filter_0_end AND INSTR("CODE", :filter_1) > 0) WHERE "EXCEL_MAX_VALUE" = "EXCEL_MAXIMUM"`} {
		if !strings.Contains(statement, expected) {
			t.Fatalf("missing %s in %s", expected, statement)
		}
	}
	if strings.Contains(statement, "ROW_NUMBER") || strings.Contains(statement, "OR 1=1") || len(arguments) != 3 || arguments[2].(sql.NamedArg).Value != "%' OR 1=1--" {
		t.Fatalf("maximum changed ties or parameter binding: %s %v", statement, arguments)
	}
}

func TestOracleExcelDynamicSourcesSaveReloadAndKeepTaskTimeInternal(t *testing.T) {
	store := &fakeOracleExcelExportSchemeStore{}
	service := &OracleExcelExportSchemeService{store: store, normalize: func(ctx context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
		return normalizeOracleExcelExportWithMetadata(ctx, input, func(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
			return oracleExcelDynamicTestSchema(), nil
		})
	}}
	filters := []OracleExcelExportFilter{{Field: "BILLDATE", Op: "between", ValueSource: &OracleExcelExportValueSource{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}}, {Field: "AMOUNT", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX"}}}
	input := OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{{SheetName: "A", Owner: "DEMO", TableName: "ORDERS", Filters: filters}, {SheetName: "B", Owner: "DEMO", TableName: "ORDERS", Filters: filters}}}
	saved, err := service.Save(t.Context(), "动态导出", input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.ConfigJSON, "queryTime") || !strings.Contains(saved.ConfigJSON, `"valueSource":{"kind":"TIME_RANGE","code":"CURRENT_MONTH"}`) {
		t.Fatalf("saved config lost dynamic sources or leaked execution time: %s", saved.ConfigJSON)
	}
	var restored OracleExcelExportConfig
	if err := json.Unmarshal([]byte(saved.ConfigJSON), &restored); err != nil {
		t.Fatal(err)
	}
	restored.queryTime = time.Date(2026, 10, 31, 15, 59, 59, 0, time.UTC)
	sheets, _, err := oracleExcelExportSheets(restored)
	if err != nil {
		t.Fatal(err)
	}
	for _, sheet := range sheets {
		config := sheet.tableConfig()
		_, _, first, err := buildOracleExcelExportQuery(config, oracleExcelDynamicTestSchema())
		if err != nil {
			t.Fatal(err)
		}
		_, _, retried, err := buildOracleExcelExportQuery(config, oracleExcelDynamicTestSchema())
		if err != nil || !reflect.DeepEqual(first, retried) || first[0].(sql.NamedArg).Value != godror.Number("20261001") || first[1].(sql.NamedArg).Value != godror.Number("20261031") {
			t.Fatalf("Sheet %s period drifted: %v %v %v", sheet.SheetName, first, retried, err)
		}
	}
}

func TestOracleExcelDynamicFiltersRejectInvalidSources(t *testing.T) {
	for _, filter := range []OracleExcelExportFilter{
		{Field: "BILLDATE", Op: "between", Value: "20261001,20261031"},
		{Field: "BILLDATE", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}},
		{Field: "BILLDATE", Op: "between", ValueSource: &OracleExcelExportValueSource{Kind: "TIME_RANGE", Code: "UNKNOWN"}},
		{Field: "BILLDATE", Op: "contains", ValueSource: &OracleExcelExportValueSource{Kind: "TIME", Code: "TODAY"}},
		{Field: "BILLDATE", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "TIME", Code: "SYSDATE;DELETE"}},
		{Field: "CODE", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "TIME", Code: "TODAY"}},
		{Field: "AMOUNT", Op: "gte", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX"}},
		{Field: "AMOUNT", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX", Code: "OTHER_FIELD"}},
	} {
		t.Run(filter.Field+filter.Op+filter.Value, func(t *testing.T) {
			if _, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{filter}}, oracleExcelDynamicTestSchema()); err == nil {
				t.Fatal("accepted invalid dynamic filter")
			}
		})
	}
	maximum := OracleExcelExportFilter{Field: "AMOUNT", Op: "eq", ValueSource: &OracleExcelExportValueSource{Kind: "COLUMN_MAX"}}
	if _, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{maximum, maximum}}, oracleExcelDynamicTestSchema()); err == nil {
		t.Fatal("accepted multiple maximum filters in one Sheet")
	}
}

func TestOracleExcelActualSpoolFormatsIntegralNumbersWithoutDecimalPoint(t *testing.T) {
	dir := t.TempDir()
	sheet := OracleExcelExportSheet{SheetName: "整数", Columns: []OracleExcelExportColumn{{Field: "AMOUNT", Title: "金额"}}}
	snapshot, err := spoolOracleExcelSheet(t.Context(), &oracleExcelSpoolFixtureRows{values: []interface{}{godror.Number("12.0")}}, sheet, []reportoracle.ResultColumn{{Name: "AMOUNT", DataType: "NUMBER"}}, filepath.Join(dir, "spool"), nil)
	if err != nil {
		t.Fatal(err)
	}
	output, _, err := renderOracleExcelSnapshots(t.Context(), []oracleExcelSheetSnapshot{snapshot}, dir, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := excelize.OpenFile(output)
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	value, err := workbook.GetCellValue("整数", "A2")
	if err != nil || value != "12" {
		t.Fatalf("actual Oracle workbook integer=%q err=%v", value, err)
	}
}
