package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"github.com/godror/godror"
)

func TestOracleExcelDateFormatManualBindings(t *testing.T) {
	for _, test := range []struct {
		name, field, format, input, want string
	}{
		{name: "compact DATE", field: "CREATED_AT", format: "yyyyMMdd", input: "20261026", want: "2026-10-26 00:00:00"},
		{name: "hyphen DATE", field: "CREATED_AT", format: "yyyy-MM-dd", input: "2026-10-26", want: "2026-10-26 00:00:00"},
		{name: "slash timestamp", field: "PRECISE_AT", format: "yyyy/MM/dd", input: "2026/10/26", want: "2026-10-26 00:00:00"},
		{name: "dot DATE", field: "CREATED_AT", format: "yyyy.MM.dd", input: "2026.10.26", want: "2026-10-26 00:00:00"},
		{name: "hyphen seconds", field: "PRECISE_AT", format: "yyyy-MM-dd HH:mm:ss", input: "2026-10-26 12:34:56", want: "2026-10-26 12:34:56"},
		{name: "slash seconds", field: "PRECISE_AT", format: "yyyy/MM/dd HH:mm:ss", input: "2026/10/26 12:34:56", want: "2026-10-26 12:34:56"},
		{name: "numeric date", field: "BILLDATE", format: "yyyyMMdd", input: "20261026", want: "20261026"},
		{name: "text date", field: "CODE", format: "yyyy/MM/dd", input: "2026/10/26", want: "2026/10/26"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{{Field: test.field, Op: "gte", Value: test.input, DateFormat: test.format}}}
			config, statement, arguments, err := buildOracleExcelExportQuery(input, oracleExcelDynamicTestSchema())
			if err != nil || len(arguments) != 1 {
				t.Fatalf("query=%s args=%v error=%v", statement, arguments, err)
			}
			if !strings.Contains(statement, `"`+test.field+`" >= :filter_0`) || strings.Contains(statement, "TO_CHAR") || strings.Contains(statement, test.input) {
				t.Fatalf("date should use a bound value without converting the column: %s", statement)
			}
			bound := arguments[0].(sql.NamedArg).Value
			actual := ""
			switch value := bound.(type) {
			case time.Time:
				actual = value.Format("2006-01-02 15:04:05")
			case godror.Number:
				actual = string(value)
			case string:
				actual = value
			default:
				t.Fatalf("unexpected date binding %T", bound)
			}
			if actual != test.want || config.Filters[0].DateFormat != test.format || config.Filters[0].Value != test.input {
				t.Fatalf("binding=%s config=%+v", actual, config.Filters[0])
			}
		})
	}
}

func TestOracleExcelDateFormatDynamicBindings(t *testing.T) {
	for _, test := range []struct {
		name, field, format, kind, code, op string
		want                                []string
		wantSQL                             string
	}{
		{name: "text slash month", field: "CODE", format: "yyyy/MM/dd", kind: "TIME_RANGE", code: "CURRENT_MONTH", op: "between", want: []string{"2026/10/01", "2026/11/01"}, wantSQL: `"CODE" >= :filter_0_start AND "CODE" < :filter_0_end`},
		{name: "text seconds includes final day", field: "CODE", format: "yyyy-MM-dd HH:mm:ss", kind: "TIME_RANGE", code: "CURRENT_MONTH", op: "between", want: []string{"2026-10-01 00:00:00", "2026-11-01 00:00:00"}, wantSQL: `"CODE" < :filter_0_end`},
		{name: "text final day", field: "CODE", format: "yyyy.MM.dd", kind: "TIME", code: "MONTH_END", op: "lte", want: []string{"2026.11.01"}, wantSQL: `"CODE" < :filter_0`},
		{name: "text today", field: "CODE", format: "yyyy-MM-dd", kind: "TIME", code: "TODAY", op: "eq", want: []string{"2026-10-26"}, wantSQL: `"CODE" = :filter_0`},
		{name: "numeric full date", field: "BILLDATE", format: "yyyyMMdd", kind: "TIME", code: "CURRENT_MONTH", op: "eq", want: []string{"20261001"}, wantSQL: `"BILLDATE" = :filter_0`},
		{name: "old numeric month", field: "BILLDATE", kind: "TIME", code: "CURRENT_MONTH", op: "eq", want: []string{"202610"}, wantSQL: `"BILLDATE" = :filter_0`},
		{name: "DATE slash stays native", field: "CREATED_AT", format: "yyyy/MM/dd", kind: "TIME_RANGE", code: "CURRENT_MONTH", op: "between", want: []string{"2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"}, wantSQL: `"CREATED_AT" < :filter_0_end`},
		{name: "DATE now date only uses midnight", field: "CREATED_AT", format: "yyyyMMdd", kind: "TIME", code: "NOW", op: "eq", want: []string{"2026-10-26T00:00:00+08:00"}, wantSQL: `"CREATED_AT" = :filter_0`},
		{name: "timestamp now date only uses midnight", field: "PRECISE_AT", format: "yyyy/MM/dd", kind: "TIME", code: "NOW", op: "eq", want: []string{"2026-10-26T00:00:00+08:00"}, wantSQL: `"PRECISE_AT" = :filter_0`},
		{name: "timestamp explicit seconds stays precise", field: "PRECISE_AT", format: "yyyy/MM/dd HH:mm:ss", kind: "TIME", code: "NOW", op: "eq", want: []string{"2026-10-26T16:00:00+08:00"}, wantSQL: `"PRECISE_AT" = :filter_0`},
		{name: "old DATE now stays precise", field: "CREATED_AT", kind: "TIME", code: "NOW", op: "eq", want: []string{"2026-10-26T16:00:00+08:00"}, wantSQL: `"CREATED_AT" = :filter_0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter := OracleExcelExportFilter{Field: test.field, Op: test.op, DateFormat: test.format, ValueSource: &OracleExcelExportValueSource{Kind: test.kind, Code: test.code}}
			_, statement, arguments, err := buildOracleExcelExportQueryAt(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{filter}}, oracleExcelDynamicTestSchema(), time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
			if err != nil || !strings.Contains(statement, test.wantSQL) || len(arguments) != len(test.want) {
				t.Fatalf("SQL=%s arguments=%v error=%v", statement, arguments, err)
			}
			for index, argument := range arguments {
				actual := ""
				switch value := argument.(sql.NamedArg).Value.(type) {
				case time.Time:
					actual = value.Format(time.RFC3339)
				case godror.Number:
					actual = string(value)
				case string:
					actual = value
				}
				if actual != test.want[index] {
					t.Fatalf("binding=%s want=%s", actual, test.want[index])
				}
			}
		})
	}
}

func TestOracleExcelDateFormatRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name, field, format, value, want string
	}{
		{name: "number separators", field: "BILLDATE", format: "yyyy-MM-dd", value: "2026-10-26", want: "数值日期字段仅支持 yyyyMMdd"},
		{name: "unknown format", field: "CREATED_AT", format: "dd/MM/yyyy", value: "26/10/2026", want: "不支持的日期格式"},
		{name: "mismatched separators", field: "CREATED_AT", format: "yyyy/MM/dd", value: "2026-10-26", want: "日期须符合 yyyy/MM/dd"},
		{name: "invalid calendar day", field: "BILLDATE", format: "yyyyMMdd", value: "20260230", want: "日期须符合 yyyyMMdd"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := buildOracleExcelExportQuery(OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{{Field: test.field, Op: "eq", Value: test.value, DateFormat: test.format}}}, oracleExcelDynamicTestSchema())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want=%s", err, test.want)
			}
		})
	}
}

func TestOracleExcelDateFormatSavedSchemeRoundTrip(t *testing.T) {
	service := &OracleExcelExportSchemeService{store: &fakeOracleExcelExportSchemeStore{}, normalize: func(ctx context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
		return normalizeOracleExcelExportWithMetadata(ctx, input, func(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
			return oracleExcelDynamicTestSchema(), nil
		})
	}}
	input := OracleExcelExportConfig{Owner: "DEMO", TableName: "ORDERS", Filters: []OracleExcelExportFilter{{Field: "CODE", Op: "between", DateFormat: "yyyy/MM/dd", ValueSource: &OracleExcelExportValueSource{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}}}}
	saved, err := service.Save(t.Context(), "文本日期导出", input)
	if err != nil {
		t.Fatal(err)
	}
	var restored OracleExcelExportConfig
	if err := json.Unmarshal([]byte(saved.ConfigJSON), &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Filters) != 1 || restored.Filters[0].DateFormat != "yyyy/MM/dd" || restored.Filters[0].ValueSource.Code != "CURRENT_MONTH" || restored.Filters[0].Value != "" {
		t.Fatalf("scheme lost date format or froze the dynamic date: %s", saved.ConfigJSON)
	}
}
