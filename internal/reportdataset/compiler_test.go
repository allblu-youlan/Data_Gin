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
