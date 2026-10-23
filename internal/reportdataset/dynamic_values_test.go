package reportdataset

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDynamicTimeFiltersUseBeijingCalendarAndKeepBindings(t *testing.T) {
	for _, test := range []struct {
		name, logicalType, operator, kind, code, wantSQL string
		now                                              time.Time
		want                                             []string
	}{
		{name: "numeric current month crosses UTC day", logicalType: "integer", operator: "BETWEEN", kind: "TIME_RANGE", code: "CURRENT_MONTH", now: time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), want: []string{"20261101", "20261130"}, wantSQL: "R1.BILLDATE BETWEEN :b1 AND :b2"},
		{name: "numeric previous month crosses year", logicalType: "integer", operator: "BETWEEN", kind: "TIME_RANGE", code: "PREVIOUS_MONTH", now: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), want: []string{"20251201", "20251231"}, wantSQL: "R1.BILLDATE BETWEEN :b1 AND :b2"},
		{name: "leap year next month", logicalType: "integer", operator: "BETWEEN", kind: "TIME_RANGE", code: "NEXT_MONTH", now: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC), want: []string{"20240201", "20240229"}, wantSQL: "R1.BILLDATE BETWEEN :b1 AND :b2"},
		{name: "DATE month includes final day time", logicalType: "date", operator: "BETWEEN", kind: "TIME_RANGE", code: "CURRENT_MONTH", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"}, wantSQL: "(R1.BILLDATE >= :b1 AND R1.BILLDATE < :b2)"},
		{name: "DATETIME month includes fractional seconds", logicalType: "datetime", operator: "BETWEEN", kind: "TIME_RANGE", code: "CURRENT_MONTH", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"}, wantSQL: "(R1.BILLDATE >= :b1 AND R1.BILLDATE < :b2)"},
		{name: "numeric month start", logicalType: "integer", operator: "GTE", kind: "TIME", code: "MONTH_START", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"20261001"}, wantSQL: "R1.BILLDATE >= :b1"},
		{name: "numeric month end", logicalType: "integer", operator: "LTE", kind: "TIME", code: "MONTH_END", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"20261031"}, wantSQL: "R1.BILLDATE <= :b1"},
		{name: "DATE month end includes whole day", logicalType: "date", operator: "LTE", kind: "TIME", code: "MONTH_END", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"2026-11-01T00:00:00+08:00"}, wantSQL: "R1.BILLDATE < :b1"},
		{name: "timestamp year end includes whole day", logicalType: "datetime", operator: "LTE", kind: "TIME", code: "YEAR_END", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"2027-01-01T00:00:00+08:00"}, wantSQL: "R1.BILLDATE < :b1"},
		{name: "timestamp now retains seconds", logicalType: "datetime", operator: "LTE", kind: "TIME", code: "NOW", now: time.Date(2026, 10, 22, 12, 34, 56, 0, time.UTC), want: []string{"2026-10-22T20:34:56+08:00"}, wantSQL: "R1.BILLDATE <= :b1"},
		{name: "DATE current month is first day", logicalType: "date", operator: "GTE", kind: "TIME", code: "CURRENT_MONTH", now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), want: []string{"2026-10-01T00:00:00+08:00"}, wantSQL: "R1.BILLDATE >= :b1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query, tables := testDataset()
			table := tables["2"]
			table.Columns[2].LogicalType = test.logicalType
			if test.logicalType == "date" || test.logicalType == "datetime" {
				table.Columns[2].OracleType = "DATE"
			}
			tables["2"] = table
			query.Where = &Condition{AliasID: "retail", ColumnID: "23", Operator: test.operator, Value: &Value{Kind: test.kind, Code: test.code}}
			compiled, err := Compile(query, tables, DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := compiled.BuildAt(nil, QueryInputs{}, "7", false, test.now)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plan.SQL, test.wantSQL) || len(plan.Args) != len(test.want) {
				t.Fatalf("SQL=%s args=%v", plan.SQL, plan.Args)
			}
			for index, argument := range plan.Args {
				value := argument.(sql.NamedArg).Value
				actual := ""
				switch typed := value.(type) {
				case time.Time:
					actual = typed.Format(time.RFC3339)
				case json.Number:
					actual = typed.String()
				default:
					t.Fatalf("unexpected binding type %T", value)
				}
				if actual != test.want[index] {
					t.Fatalf("binding %d=%s want=%s", index, actual, test.want[index])
				}
			}
		})
	}
}

func TestColumnMaximumRunsAfterScopesFixedRelationsAndRuntimeFilters(t *testing.T) {
	query, tables := testDataset()
	base := tables["1"]
	base.Config.DataScope = DataScope{Mode: "SCOPE_COLUMN", ColumnID: "11", UserValues: map[string][]string{"7": {"7"}}}
	base.FixedCondition = &Condition{ColumnID: "14", Operator: "GT", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`0`)}}
	tables["1"] = base
	query.Relations[1].MatchConditions = &Condition{AliasID: "product", ColumnID: "33", Operator: "PREFIX", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"SKU"`)}}
	query.Where = &Condition{Logic: "AND", Items: []Condition{
		{AliasID: "retail", ColumnID: "23", Operator: "BETWEEN", Value: &Value{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}},
		{AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "COLUMN_MAX"}},
	}}
	query.OrderBy = []Order{{AliasID: "retail", ColumnID: "23", Direction: "DESC"}}
	query.AllowedFilters = []AllowedFilter{{AliasID: "product", ColumnID: "33", Operators: []string{"EQ"}}}
	compiled, err := Compile(query, tables, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	inputs := QueryInputs{Where: &Condition{AliasID: "product", ColumnID: "33", Operator: "EQ", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"SKU-1"`)}}}
	plan, err := compiled.BuildAt(nil, inputs, "7", false, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"MAX(B0.QTY) OVER () AS REPORT_MAXIMUM",
		"R2.CODE LIKE :b1", "B0.ID IN (:b2)", "B0.QTY > :b3",
		"R1.BILLDATE BETWEEN :b4 AND :b5", "R2.CODE = :b6",
		") REPORT_FILTERED WHERE REPORT_MAX_VALUE = REPORT_MAXIMUM ORDER BY REPORT_SORT_1 DESC NULLS LAST, REPORT_SORT_2 ASC NULLS LAST",
	} {
		if !strings.Contains(plan.SQL, expected) {
			t.Fatalf("missing %s in %s", expected, plan.SQL)
		}
	}
	if strings.Count(plan.SQL, "LEFT JOIN") != 2 || strings.Count(plan.SQL, "MAX(") != 1 || strings.Contains(plan.SQL, "ROW_NUMBER") {
		t.Fatalf("maximum must preserve ties in one scoped source query: %s", plan.SQL)
	}
	if len(plan.Columns) != len(compiled.Projections) || strings.Contains(strings.Join(plan.Columns, ","), "REPORT_") {
		t.Fatalf("internal maximum or ordering projection leaked: %v", plan.Columns)
	}
}

func TestDynamicFiltersRejectUnsupportedSemantics(t *testing.T) {
	maximum := Condition{AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "COLUMN_MAX"}}
	for _, test := range []struct {
		name string
		edit func(*QuerySpec)
	}{
		{name: "maximum in OR", edit: func(query *QuerySpec) { query.Where = &Condition{Logic: "OR", Items: []Condition{maximum}} }},
		{name: "maximum in relation ON", edit: func(query *QuerySpec) { query.Relations[0].MatchConditions = &maximum }},
		{name: "maximum only equals", edit: func(query *QuerySpec) { copy := maximum; copy.Operator = "GTE"; query.Where = &copy }},
		{name: "multiple maxima", edit: func(query *QuerySpec) { query.Where = &Condition{Logic: "AND", Items: []Condition{maximum, maximum}} }},
		{name: "time collection", edit: func(query *QuerySpec) {
			query.Where = &Condition{AliasID: "base", ColumnID: "14", Operator: "IN", Value: &Value{Kind: "TIME", Code: "TODAY"}}
		}},
		{name: "month range only BETWEEN", edit: func(query *QuerySpec) {
			query.Where = &Condition{AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}}
		}},
		{name: "unknown time", edit: func(query *QuerySpec) {
			query.Where = &Condition{AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "TIME", Code: "SYSDATE;DELETE"}}
		}},
		{name: "string is not a numeric date", edit: func(query *QuerySpec) {
			query.Where = &Condition{AliasID: "product", ColumnID: "33", Operator: "EQ", Value: &Value{Kind: "TIME", Code: "TODAY"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query, tables := testDataset()
			test.edit(&query)
			if _, err := Compile(query, tables, DefaultPolicy()); err == nil {
				t.Fatal("accepted unsupported dynamic condition")
			}
		})
	}
}

func TestRuntimeDynamicFiltersStillRequireApproval(t *testing.T) {
	query, tables := testDataset()
	query.AllowedFilters = []AllowedFilter{{AliasID: "retail", ColumnID: "23", Operators: []string{"BETWEEN", "EQ"}}}
	compiled, err := Compile(query, tables, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []Value{{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}, {Kind: "COLUMN_MAX"}} {
		operator := "EQ"
		if value.Kind == "TIME_RANGE" {
			operator = "BETWEEN"
		}
		inputs := QueryInputs{Where: &Condition{AliasID: "retail", ColumnID: "23", Operator: operator, Value: &value}}
		if _, err := compiled.Build(nil, inputs, "7", false); err != nil {
			t.Fatal(err)
		}
		inputs.Where.AliasID, inputs.Where.ColumnID = "base", "14"
		if _, err := compiled.Build(nil, inputs, "7", false); err == nil {
			t.Fatal("unapproved runtime dynamic filter accepted")
		}
	}
}

func TestLiveRelationTimePredicatesUseRunSnapshot(t *testing.T) {
	query, tables := testDataset()
	query.Relations[0].MatchConditions = &Condition{AliasID: "retail", ColumnID: "23", Operator: "BETWEEN", Value: &Value{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}}
	compiled, err := Compile(query, tables, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	first, err := compiled.BuildRelationPlanAt("retail", nil, nil, nil, "", nil, "7", false, frozen)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compiled.BuildRelationPlanAt("retail", nil, nil, nil, "", nil, "7", false, frozen)
	if err != nil || !reflect.DeepEqual(first.Plan, second.Plan) || first.Plan.Args[0].(sql.NamedArg).Value != json.Number("20261001") || first.Plan.Args[1].(sql.NamedArg).Value != json.Number("20261031") {
		t.Fatalf("LIVE relation period drifted: %v %v %v", first.Plan.Args, second.Plan.Args, err)
	}
}
