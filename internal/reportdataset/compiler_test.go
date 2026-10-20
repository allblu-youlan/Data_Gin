package reportdataset

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func testDataset() (QuerySpec, map[string]Table) {
	zero := 0
	col := func(id, name, typ string) Column {
		return Column{ID: id, DBName: name, OracleType: typ, LogicalType: LogicalType(typ, &zero), Scale: &zero, IsPhysical: true, IsSelectable: true}
	}
	tables := map[string]Table{
		"1": {ID: "1", Owner: "BOSNDS3", PhysicalTable: "M_RETAILITEM", DBLink: "Link_To_Bojun", ValidationStatus: "VALID", PrimaryKeys: []string{"11"}, DisplayFields: []string{"11"}, UniqueKeys: [][]string{{"11"}}, Columns: []Column{col("11", "ID", "NUMBER"), col("12", "M_RETAIL_ID", "NUMBER"), col("13", "M_PRODUCT_ID", "NUMBER"), col("14", "QTY", "NUMBER")}, Config: TableConfig{Enabled: true, DataScope: DataScope{Mode: "GLOBAL_APPROVED"}}},
		"2": {ID: "2", Owner: "BOSNDS3", PhysicalTable: "M_RETAIL", DBLink: "Link_To_Bojun", ValidationStatus: "VALID", PrimaryKeys: []string{"21"}, DisplayFields: []string{"22"}, UniqueKeys: [][]string{{"21"}}, Columns: []Column{col("21", "ID", "NUMBER"), col("22", "DOCNO", "VARCHAR2"), col("23", "BILLDATE", "NUMBER")}, Config: TableConfig{Enabled: true, DataScope: DataScope{Mode: "GLOBAL_APPROVED"}}},
		"3": {ID: "3", Owner: "BOSNDS3", PhysicalTable: "M_PRODUCT", DBLink: "Link_To_Bojun", ValidationStatus: "VALID", PrimaryKeys: []string{"31"}, DisplayFields: []string{"32"}, UniqueKeys: [][]string{{"31"}}, Columns: []Column{col("31", "ID", "NUMBER"), col("32", "NAME", "VARCHAR2"), col("33", "CODE", "VARCHAR2")}, Config: TableConfig{Enabled: true, DataScope: DataScope{Mode: "GLOBAL_APPROVED"}}}}
	q := QuerySpec{Version: 1, Base: Base{TableID: "1", AliasID: "base"}, Relations: []Relation{{ID: "retail", FromAliasID: "base", TargetTableID: "2", KeyBindings: []KeyBinding{{SourceColumnID: "12", TargetColumnID: "21"}}, JoinType: "LEFT", Cardinality: "MANY_TO_ONE"}, {ID: "product", FromAliasID: "base", TargetTableID: "3", KeyBindings: []KeyBinding{{SourceColumnID: "13", TargetColumnID: "31"}}, JoinType: "LEFT", Cardinality: "MANY_TO_ONE"}}, Columns: []QueryColumn{{FieldID: "00000000-0000-4000-8000-000000000001", Source: Source{Kind: "RELATION_DISPLAY", RelationID: "retail"}}, {FieldID: "00000000-0000-4000-8000-000000000002", Source: Source{Kind: "RELATION_DISPLAY", RelationID: "product"}}, {FieldID: "00000000-0000-4000-8000-000000000003", Source: Source{Kind: "RELATION_COLUMN", RelationID: "product", ColumnID: "33"}}}}
	return q, tables
}
func TestCompileMultipleRelationsAndAdditionalColumns(t *testing.T) {
	q, tables := testDataset()
	q.Columns[0].Header = "自定义单号"
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.Build(nil, QueryInputs{}, "1", false)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(p.SQL, "LEFT JOIN") != 2 || strings.Count(p.SQL, "M_PRODUCT@") != 1 || c.Outputs[0].Header != "自定义单号" || c.Outputs[1].Header != "M_PRODUCT_ID" {
		t.Fatalf("unexpected plan %s %#v", p.SQL, c.Outputs)
	}
	if c.Outputs[0].RawName == "" || len(c.Relations["product"].DetailNames) != 2 {
		t.Fatal("relation raw or detail projection missing")
	}
}
func TestCompileKeepsMatchPredicatesInJoinAndFiltersInWhere(t *testing.T) {
	q, tables := testDataset()
	q.Relations[1].MatchConditions = &Condition{AliasID: "product", ColumnID: "33", Operator: "PREFIX", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"a_%' OR 1=1--"`)}}
	q.Where = &Condition{AliasID: "retail", ColumnID: "23", Operator: "GTE", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"20261001"`)}}
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.Build(nil, QueryInputs{}, "1", false)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(p.SQL, "OR 1=1") || !strings.Contains(p.SQL, "R2.CODE LIKE :b1") || !strings.Contains(p.SQL, "WHERE (1=1) AND (R1.BILLDATE >= :b2)") {
		t.Fatal(p.SQL)
	}
	if p.Args[0].(sql.NamedArg).Value != "a\\_\\%' OR 1=1--%" {
		t.Fatalf("unescaped LIKE: %#v", p.Args)
	}
}
func TestCompileRejectsNonUniqueAndIncompleteCompositeKeys(t *testing.T) {
	for _, name := range []string{"non-unique", "incomplete-composite"} {
		t.Run(name, func(t *testing.T) {
			q, tables := testDataset()
			target := tables["3"]
			if name == "non-unique" {
				target.Columns[2].LogicalType = "integer"
				target.Columns[2].OracleType = "NUMBER"
				tables["3"] = target
				q.Relations[1].KeyBindings[0].TargetColumnID = "33"
			} else {
				target.UniqueKeys = [][]string{{"31", "33"}}
				target.PrimaryKeys = []string{"31", "33"}
				tables["3"] = target
			}
			if _, e := Compile(q, tables, DefaultPolicy()); e == nil || !strings.Contains(e.Error(), "REPORT_RELATION_NOT_UNIQUE") {
				t.Fatalf("expected uniqueness error: %v", e)
			}
		})
	}
}
func TestCompileValidatesRelationGraphAndVirtualFields(t *testing.T) {
	for _, name := range []string{"cycle", "virtual", "wrong-table", "unapproved"} {
		t.Run(name, func(t *testing.T) {
			q, tables := testDataset()
			switch name {
			case "cycle":
				q.Relations[0].FromAliasID = "product"
				q.Relations[1].FromAliasID = "retail"
			case "virtual":
				v := tables["3"]
				v.Columns[2].IsPhysical = false
				tables["3"] = v
			case "wrong-table":
				q.Columns[2].Source.ColumnID = "23"
			case "unapproved":
				v := tables["3"]
				v.Config.Enabled = false
				tables["3"] = v
			}
			if _, e := Compile(q, tables, DefaultPolicy()); e == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
}
func TestScopeRemainsOutsideUserOr(t *testing.T) {
	q, tables := testDataset()
	base := tables["1"]
	base.Config.DataScope = DataScope{Mode: "SCOPE_COLUMN", ColumnID: "11", UserValues: map[string][]string{"7": {"9007199254740993"}}}
	tables["1"] = base
	q.AllowedFilters = []AllowedFilter{{AliasID: "base", ColumnID: "14", Operators: []string{"EQ"}}}
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	input := QueryInputs{Where: &Condition{Logic: "OR", Items: []Condition{{AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"1"`)}}, {AliasID: "base", ColumnID: "14", Operator: "EQ", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"2"`)}}}}}
	p, e := c.Build(nil, input, "7", false)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(p.SQL, "WHERE (B0.ID IN (:b1)) AND ((B0.QTY = :b2 OR B0.QTY = :b3))") {
		t.Fatal(p.SQL)
	}
	if p.Args[0].(sql.NamedArg).Value != json.Number("9007199254740993") {
		t.Fatal("integer lost precision")
	}
	input.Where.Items[0].ColumnID = "11"
	if _, e := c.Build(nil, input, "7", false); e == nil {
		t.Fatal("undeclared filter accepted")
	}
}
func TestConditionsHandleEmptySetsAndNulls(t *testing.T) {
	for _, tc := range []struct{ name, op, raw, want string }{{"empty in", "IN", "[]", "1=0"}, {"empty not in", "NOT_IN", "[]", "1=1"}, {"null in", "IN", "[null]", "error"}} {
		t.Run(tc.name, func(t *testing.T) {
			q, tables := testDataset()
			q.Where = &Condition{AliasID: "base", ColumnID: "14", Operator: tc.op, Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(tc.raw)}}
			c, e := Compile(q, tables, DefaultPolicy())
			if tc.want == "error" {
				if e == nil {
					t.Fatal("NULL set accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			p, e := c.Build(nil, QueryInputs{}, "1", false)
			if e != nil || !strings.Contains(p.SQL, "AND ("+tc.want+")") {
				t.Fatalf("%s %v", p.SQL, e)
			}
		})
	}
}
func TestDuplicateHeadersAndSameSourceHaveIndependentFieldIdentity(t *testing.T) {
	q, tables := testDataset()
	q.Columns[0].Header = "相同"
	q.Columns[1].Header = "相同"
	q.Columns[2].Source = q.Columns[1].Source
	q.Columns[2].Header = "相同"
	if _, e := Compile(q, tables, DefaultPolicy()); e != nil {
		t.Fatal(e)
	}
}
func TestSnapshotDisplayFallsBackToMatchedKeyAndPreservesRaw(t *testing.T) {
	q, tables := testDataset()
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	r := c.Relations["product"]
	values := map[string]any{r.SourceKeys[0]: "002000", r.TargetKeys[0]: "002000", r.DisplayNames[0]: nil}
	c.Materialize(values)
	if values[c.Outputs[1].Name] != "002000" || c.RawValue(r, values) != "002000" {
		t.Fatal(values)
	}
	values[r.TargetKeys[0]] = nil
	c.Materialize(values)
	if values[c.Outputs[1].Name] != nil || c.RawValue(r, values) != "002000" {
		t.Fatal("unmatched target lost source key")
	}
}
func TestLiveRelationPlanReplaysSourceFieldsAndScope(t *testing.T) {
	q, tables := testDataset()
	q.Relations[1].MatchConditions = &Condition{AliasID: "product", ColumnID: "31", Operator: "EQ", Value: &Value{Kind: "FIELD", AliasID: "base", ColumnID: "13"}}
	target := tables["3"]
	target.Config.DataScope = DataScope{Mode: "SCOPE_COLUMN", ColumnID: "31", UserValues: map[string][]string{"7": {"88"}}}
	tables["3"] = target
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	plan, e := c.BuildRelationPlan("product", nil, map[string]map[string]json.RawMessage{"base": {"13": json.RawMessage(`"88"`)}}, map[string]json.RawMessage{"31": json.RawMessage(`"88"`)}, "", nil, "7", true)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(plan.Plan.SQL, "M_RETAILITEM") || !strings.Contains(plan.Plan.SQL, "T0.ID IN (:b1)") || !strings.Contains(plan.Plan.SQL, "T0.ID = :b2") || !strings.Contains(plan.Plan.SQL, "T0.ID = :b3") {
		t.Fatal(plan.Plan.SQL)
	}
}
func TestCompositeRawOutputPreservesAllOrderedBindings(t *testing.T) {
	q, tables := testDataset()
	target := tables["3"]
	target.PrimaryKeys = []string{"31", "33"}
	target.UniqueKeys = [][]string{{"31", "33"}}
	target.Columns[2].OracleType = "NUMBER"
	target.Columns[2].LogicalType = "integer"
	tables["3"] = target
	q.Relations[1].KeyBindings = append(q.Relations[1].KeyBindings, KeyBinding{SourceColumnID: "14", TargetColumnID: "33"})
	q.Columns[1].ValueMode = "RAW"
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	r := c.Relations["product"]
	values := map[string]any{r.SourceKeys[0]: "2001", r.SourceKeys[1]: "003"}
	c.Materialize(values)
	raw, ok := values[c.Outputs[1].Name].(string)
	if !ok || !strings.Contains(raw, `"31":"2001"`) || !strings.Contains(raw, `"33":"003"`) {
		t.Fatalf("composite raw lost a component: %#v", values)
	}
	if c.Outputs[1].LogicalType != "string" {
		t.Fatal("composite RAW must export as explicit key JSON")
	}
}
func TestExplicitOutputStringTypeIsAppliedWithoutFloatConversion(t *testing.T) {
	q, tables := testDataset()
	q.Columns[2].Source = Source{Kind: "BASE_COLUMN", ColumnID: "11"}
	q.Columns[2].ValueType = "string"
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	out := c.Outputs[2]
	values := map[string]any{out.ValueName: json.Number("9007199254740993")}
	c.Materialize(values)
	if values[out.Name] != "9007199254740993" || out.LogicalType != "string" {
		t.Fatalf("override lost: %#v %#v", out, values)
	}
}
func TestNestedDetailUsesConfiguredDisplayAndSingleScopedSelect(t *testing.T) {
	q, tables := testDataset()
	q.Relations[0].DetailColumnIDs = []string{"21"}
	q.Relations = append(q.Relations, Relation{ID: "store", Name: "店仓", FromAliasID: "retail", TargetTableID: "3", JoinType: "LEFT", Cardinality: "MANY_TO_ONE", KeyBindings: []KeyBinding{{SourceColumnID: "21", TargetColumnID: "31"}}})
	c, e := Compile(q, tables, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	r := c.Relations["store"]
	values := map[string]any{r.TargetKeys[0]: "7", r.DisplayNames[0]: "店仓显示"}
	display, ok := c.RelatedFieldDisplay("retail", "21", values)
	if !ok || display != "店仓显示" {
		t.Fatalf("%#v %t", display, ok)
	}
	plan, e := c.BuildRelationPlan("retail", nil, nil, map[string]json.RawMessage{"21": json.RawMessage(`"7"`)}, "", nil, "7", true)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(plan.Plan.SQL, "LEFT JOIN") != 1 || strings.Contains(plan.Plan.SQL, "M_RETAILITEM") || len(plan.SnapshotNames) == 0 {
		t.Fatalf("nested detail is not a single bounded relation query: %s", plan.Plan.SQL)
	}
}
func TestDetailOnlyColumnCannotBecomeFilterWithoutSelectApproval(t *testing.T) {
	q, tables := testDataset()
	table := tables["3"]
	allowed := []string{"31", "32"}
	details := []string{"33"}
	table.Config.SelectableColumns = &allowed
	table.Config.DetailColumns = &details
	tables["3"] = table
	q.Columns = q.Columns[:2]
	q.Relations[1].DetailColumnIDs = []string{"33"}
	if _, e := Compile(q, tables, DefaultPolicy()); e != nil {
		t.Fatal(e)
	}
	q.Where = &Condition{AliasID: "product", ColumnID: "33", Operator: "EQ", Value: &Value{Kind: "CONSTANT", Value: json.RawMessage(`"secret"`)}}
	if _, e := Compile(q, tables, DefaultPolicy()); e == nil {
		t.Fatal("detail-only field was accepted as a filter")
	}
}
