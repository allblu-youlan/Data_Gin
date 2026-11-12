package reportoracle

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/godror/godror"
)

func validFeishuAttendanceQuery() FeishuAttendanceQuery {
	return FeishuAttendanceQuery{
		StartDate: "2026-10-01", EndDate: "2026-10-31", PageSize: 50,
		Filters: map[string]string{"USER_NAME": "张三", "DEPARTMENT_NAME": "运营中心/杭州一店"},
	}
}

func TestNormalizeFeishuAttendanceRejectsInvalidScopeBeforeOracle(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*FeishuAttendanceQuery)
	}{
		{"missing name", func(q *FeishuAttendanceQuery) { delete(q.Filters, "USER_NAME") }},
		{"missing department", func(q *FeishuAttendanceQuery) { delete(q.Filters, "DEPARTMENT_NAME") }},
		{"blank name", func(q *FeishuAttendanceQuery) { q.Filters["USER_NAME"] = " \t " }},
		{"blank department", func(q *FeishuAttendanceQuery) { q.Filters["DEPARTMENT_NAME"] = "\u3000" }},
		{"long name", func(q *FeishuAttendanceQuery) { q.Filters["USER_NAME"] = strings.Repeat("张", 201) }},
		{"long department", func(q *FeishuAttendanceQuery) { q.Filters["DEPARTMENT_NAME"] = strings.Repeat("部", 1001) }},
		{"unknown filter", func(q *FeishuAttendanceQuery) { q.Filters["USER_NAME OR 1=1"] = "张三" }},
		{"unapproved metric filter", func(q *FeishuAttendanceQuery) { q.Filters["ACTUAL_MINUTES"] = "480" }},
		{"invalid date", func(q *FeishuAttendanceQuery) { q.StartDate = "2026-02-30" }},
		{"noncanonical date", func(q *FeishuAttendanceQuery) { q.StartDate = "2026-1-01" }},
		{"year zero", func(q *FeishuAttendanceQuery) { q.StartDate, q.EndDate = "0000-01-01", "0000-01-01" }},
		{"reversed dates", func(q *FeishuAttendanceQuery) { q.EndDate = "2026-09-30" }},
		{"32 inclusive days", func(q *FeishuAttendanceQuery) { q.EndDate = "2026-11-01" }},
		{"zero page size", func(q *FeishuAttendanceQuery) { q.PageSize = 0 }},
		{"oversized page", func(q *FeishuAttendanceQuery) { q.PageSize = 101 }},
		{"negative offset", func(q *FeishuAttendanceQuery) { q.Offset = -1 }},
		{"excessive offset", func(q *FeishuAttendanceQuery) { q.Offset = 99901 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := validFeishuAttendanceQuery()
			test.change(&query)
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				t.Fatal("invalid query reached Oracle")
				return nil, nil
			})
			if _, err := adapter.QueryFeishuAttendance(t.Context(), FeishuAttendanceDaily, query); err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
}

func TestNormalizeFeishuAttendanceTrimsAndCountsUnicodeWithoutMutatingCaller(t *testing.T) {
	query := validFeishuAttendanceQuery()
	query.StartDate, query.EndDate = " 2026-10-01 ", " 2026-10-31 "
	query.Filters["USER_NAME"] = " " + strings.Repeat("张", 200) + " "
	query.Filters["DEPARTMENT_NAME"] = " " + strings.Repeat("部", 1000) + " "
	query.Offset, query.PageSize = 99900, 100
	got, err := NormalizeFeishuAttendanceQuery(FeishuAttendanceDaily, query)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartDate != "2026-10-01" || got.EndDate != "2026-10-31" || got.Filters["USER_NAME"] != strings.Repeat("张", 200) || got.Filters["DEPARTMENT_NAME"] != strings.Repeat("部", 1000) {
		t.Fatalf("query was not normalized: %+v", got)
	}
	if query.Filters["USER_NAME"] != " "+strings.Repeat("张", 200)+" " {
		t.Fatal("normalization mutated caller filters")
	}
	got.Filters["USER_NAME"] = "李四"
	if query.Filters["USER_NAME"] == "李四" {
		t.Fatal("normalized filters share caller storage")
	}
}

func TestFeishuAttendanceRejectsUnknownDatasetAndCrossDatasetFilters(t *testing.T) {
	for _, test := range []struct {
		dataset FeishuAttendanceDataset
		filter  string
	}{
		{FeishuAttendanceDataset("ODS_OTHER_TABLE"), ""},
		{FeishuAttendanceDaily, "METRIC_CODE"},
		{FeishuAttendancePunch, "METRIC_CODE"},
		{FeishuAttendanceItem, "PUNCH_TYPE"},
	} {
		query := validFeishuAttendanceQuery()
		if test.filter != "" {
			query.Filters[test.filter] = "1"
		}
		if _, err := NormalizeFeishuAttendanceQuery(test.dataset, query); err == nil {
			t.Fatalf("accepted dataset %q filter %q", test.dataset, test.filter)
		}
	}
}

func TestFeishuAttendanceQueriesBindDateWindowIdentityAndStablePagination(t *testing.T) {
	const employeeName = "张' OR 1=1 --"
	for _, test := range []struct {
		dataset FeishuAttendanceDataset
		table   string
		order   string
	}{
		{FeishuAttendanceDaily, "ODS_FEISHU_ATTENDANCE_DAILY", "a.WORK_DATE, a.USER_ID"},
		{FeishuAttendancePunch, "ODS_FEISHU_ATTENDANCE_PUNCH", "a.WORK_DATE, a.USER_ID, a.SOURCE_ROW_NO, a.PUNCH_INDEX, a.PUNCH_TYPE"},
		{FeishuAttendanceItem, "ODS_FEISHU_ATTENDANCE_ITEM", "a.WORK_DATE, a.USER_ID, a.SOURCE_ROW_NO, a.ITEM_SEQ"},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				identityAlias := "a"
				if test.dataset != FeishuAttendanceDaily {
					identityAlias = "d"
					if !strings.Contains(statement, "JOIN YL_DBS.ODS_FEISHU_ATTENDANCE_DAILY d ON d.USER_ID = a.USER_ID AND d.WORK_DATE = a.WORK_DATE") || strings.Count(statement, "JOIN ") != 1 {
						t.Fatalf("detail must join only its daily identity: %s", statement)
					}
				}
				for _, fragment := range []string{
					"FROM YL_DBS." + test.table + " a",
					"a.WORK_DATE >= TO_DATE(:1, 'YYYY-MM-DD') AND a.WORK_DATE < TO_DATE(:2, 'YYYY-MM-DD') + 1",
					identityAlias + ".DEPARTMENT_NAME = :3", identityAlias + ".USER_NAME = :4",
					"ORDER BY " + test.order, "OFFSET :5 ROWS FETCH NEXT :6 ROWS ONLY",
					"TO_CHAR(a.WORK_DATE, 'YYYY-MM-DD')",
				} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("missing %q in %s", fragment, statement)
					}
				}
				if ctx != t.Context() || strings.Contains(statement, employeeName) || strings.Contains(statement, "SELECT *") || strings.Contains(statement, "COUNT(") || strings.Contains(statement, "TRUNC(a.WORK_DATE)") || strings.Contains(statement, " LIKE ") {
					t.Fatalf("unsafe or unbounded query: %s", statement)
				}
				want := []any{"2026-12-31", "2026-12-31", "运营中心/杭州一店", employeeName, 50, 51}
				got := make([]any, len(args)-1)
				for i := range got {
					got[i] = args[i].Value
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("binds=%v want=%v", got, want)
				}
				if _, ok := args[len(args)-1].Value.(godror.Option); !ok {
					t.Fatal("Oracle numbers must be fetched as strings")
				}
				return &bojunQueryTestRows{columns: []string{"unused"}}, nil
			})
			query := validFeishuAttendanceQuery()
			query.StartDate, query.EndDate, query.Offset = "2026-12-31", "2026-12-31", 50
			query.Filters["USER_NAME"] = employeeName
			rows, err := adapter.QueryFeishuAttendance(t.Context(), test.dataset, query)
			if err != nil || rows == nil || len(rows) != 0 {
				t.Fatalf("rows=%v error=%v", rows, err)
			}
		})
	}
}

func TestQueryFeishuAttendancePropagatesReadErrorsAndClosesRows(t *testing.T) {
	failure := errors.New("connection interrupted")
	rows := &bojunQueryTestRows{columns: []string{"unused"}, nextErr: failure}
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return rows, nil })
	_, err := adapter.QueryFeishuAttendance(t.Context(), FeishuAttendanceDaily, validFeishuAttendanceQuery())
	if !errors.Is(err, failure) || !rows.closed {
		t.Fatalf("error=%v closed=%v", err, rows.closed)
	}
}

func TestFeishuAttendanceColumnContracts(t *testing.T) {
	common := "ID,USER_ID,USER_NAME,EMPLOYEE_NO,DEPARTMENT_NAME,WORK_DATE,MODIFIED_TIME"
	for _, test := range []struct {
		dataset FeishuAttendanceDataset
		fields  string
	}{
		{FeishuAttendanceDaily, common + ",ATTENDANCE_GROUP_NAME,SHIFT_NAME,TIME_ZONE,RESIGN_DATE,REQUIRED_MINUTES,ACTUAL_MINUTES,OVERTIME_MINUTES,LEAVE_MINUTES"},
		{FeishuAttendancePunch, common + ",SOURCE_ROW_NO,PUNCH_INDEX,PUNCH_TYPE,TIME_CODE,RESULT_CODE,PUNCH_TIME_TEXT,RESULT_TEXT,FEATURE_DATE,GROUP_NAME,GROUP_TYPE,DAY_TYPE,SOURCE_PUNCH_NO,PUNCH_TITLE,FEATURE_PUNCH_TIME_TEXT,SHIFT_TIME_TEXT,PUNCH_SHIFT_TIME_TEXT,PUNCH_STATUS,PUNCH_SUB_STATUS,STATUS_MESSAGE,PUNCH_LOCATION_NAME,REMARK"},
		{FeishuAttendanceItem, common + ",SOURCE_ROW_NO,ITEM_SEQ,METRIC_CODE,METRIC_TITLE,VALUE_TEXT,DURATION_DAY,DURATION_HALF_DAY,DURATION_HOUR,DURATION_HALF_HOUR,DURATION_MINUTE"},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			columns := FeishuAttendanceColumns(test.dataset)
			var names []string
			for _, column := range columns {
				names = append(names, column.Name)
				if column.Filterable != (column.Name == "USER_NAME" || column.Name == "DEPARTMENT_NAME") {
					t.Fatalf("unexpected filter capability: %+v", column)
				}
				if column.Comment == "" || column.DataType == "" {
					t.Fatalf("missing column metadata: %+v", column)
				}
			}
			if strings.Join(names, ",") != test.fields {
				t.Fatalf("columns=%v", names)
			}
			columns[0].Name = "MUTATED"
			if FeishuAttendanceColumns(test.dataset)[0].Name != "ID" {
				t.Fatal("column metadata exposed mutable catalog storage")
			}
		})
	}
	if columns := FeishuAttendanceColumns("unknown"); len(columns) != 0 {
		t.Fatalf("unknown dataset columns=%v", columns)
	}
}

func TestQueryFeishuAttendancePreservesExactValuesNullsAndLookahead(t *testing.T) {
	for _, test := range []struct {
		dataset FeishuAttendanceDataset
		values  map[string]string
	}{
		{FeishuAttendanceDaily, map[string]string{"ACTUAL_MINUTES": "123456789012.123456", "OVERTIME_MINUTES": "0", "RESIGN_DATE": "2026-10-31"}},
		{FeishuAttendancePunch, map[string]string{"PUNCH_INDEX": "1", "PUNCH_TIME_TEXT": "--", "REMARK": "跨天班次\n补卡备注", "FEATURE_DATE": "2026-10-01"}},
		{FeishuAttendanceItem, map[string]string{"DURATION_HOUR": "7.500001", "DURATION_MINUTE": "0", "VALUE_TEXT": "--"}},
	} {
		t.Run(string(test.dataset), func(t *testing.T) {
			columns := FeishuAttendanceColumns(test.dataset)
			values := make([]driver.Value, len(columns))
			test.values["ID"] = "9007199254740993123"
			test.values["EMPLOYEE_NO"] = "000123"
			test.values["MODIFIED_TIME"] = "2026-11-12 08:09:10"
			for i, column := range columns {
				if value, ok := test.values[column.Name]; ok {
					values[i] = value
				}
			}
			dbRows := &bojunQueryTestRows{columns: make([]string, len(columns)), values: [][]driver.Value{values, values}}
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
				identityAlias := "a"
				if test.dataset != FeishuAttendanceDaily {
					identityAlias = "d"
				}
				for _, expression := range []string{
					"a.ID, a.USER_ID, " + identityAlias + ".USER_NAME, " + identityAlias + ".EMPLOYEE_NO, " + identityAlias + ".DEPARTMENT_NAME",
					"TO_CHAR(a.MODIFIED_TIME, 'YYYY-MM-DD HH24:MI:SS')",
				} {
					if !strings.Contains(statement, expression) {
						t.Fatalf("missing projection %q: %s", expression, statement)
					}
				}
				for _, date := range []string{"RESIGN_DATE", "FEATURE_DATE"} {
					if _, ok := test.values[date]; ok && !strings.Contains(statement, "TO_CHAR(a."+date+", 'YYYY-MM-DD')") {
						t.Fatalf("incorrect date projection: %s", statement)
					}
				}
				return dbRows, nil
			})
			query := validFeishuAttendanceQuery()
			query.PageSize = 1
			got, err := adapter.QueryFeishuAttendance(t.Context(), test.dataset, query)
			if err != nil || len(got) != 2 || !dbRows.closed {
				t.Fatalf("rows=%v error=%v closed=%v", got, err, dbRows.closed)
			}
			for _, row := range got {
				if len(row) != len(columns) {
					t.Fatalf("row fields=%d want=%d", len(row), len(columns))
				}
				for _, column := range columns {
					want, present := test.values[column.Name]
					actual := row[column.Name]
					if (present && (actual == nil || *actual != want)) || (!present && actual != nil) {
						t.Fatalf("field=%s actual=%v want=%q present=%v", column.Name, actual, want, present)
					}
				}
			}
		})
	}
}

func TestQueryFeishuAttendanceMapsOracleCancellation(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, oracleTestError{code: 1013}
	})
	if _, err := adapter.QueryFeishuAttendance(t.Context(), FeishuAttendanceDaily, validFeishuAttendanceQuery()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Oracle cancellation not preserved: %v", err)
	}
}
