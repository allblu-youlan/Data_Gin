package reportdataset

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCompiledDependenciesRetainCompletePrimaryAndRelationUniqueKeys(t *testing.T) {
	for _, test := range []struct {
		name       string
		configure  func(*QuerySpec, map[string]Table)
		baseKey    []string
		targetKeys [][]string
	}{
		{name: "ordinary relations", baseKey: []string{"11"}, targetKeys: [][]string{{"31"}}},
		{name: "unselected composite base primary key", baseKey: []string{"11", "14"}, targetKeys: [][]string{{"31"}}, configure: func(_ *QuerySpec, tables map[string]Table) {
			base := tables["1"]
			base.PrimaryKeys = []string{"11", "14"}
			base.UniqueKeys = [][]string{{"11", "14"}}
			tables["1"] = base
		}},
		{name: "complete composite relation key", baseKey: []string{"11"}, targetKeys: [][]string{{"31", "33"}}, configure: func(query *QuerySpec, tables map[string]Table) {
			target := tables["3"]
			target.Columns[2].OracleType, target.Columns[2].LogicalType = "NUMBER", "integer"
			target.PrimaryKeys = []string{"31", "33"}
			target.UniqueKeys = [][]string{{"31", "33"}}
			tables["3"] = target
			query.Relations[1].KeyBindings = append(query.Relations[1].KeyBindings, KeyBinding{SourceColumnID: "14", TargetColumnID: "33"})
		}},
		{name: "relation uses alternate unique key", baseKey: []string{"11"}, targetKeys: [][]string{{"33"}}, configure: func(query *QuerySpec, tables map[string]Table) {
			dependencyAlternateKey(tables)
			query.Relations[1].KeyBindings[0].TargetColumnID = "33"
		}},
		{name: "same table aliases use different unique keys", baseKey: []string{"11"}, targetKeys: [][]string{{"31"}, {"33"}}, configure: func(query *QuerySpec, tables map[string]Table) {
			dependencyAlternateKey(tables)
			query.Relations = append(query.Relations, Relation{ID: "product_by_code", FromAliasID: "base", TargetTableID: "3", JoinType: "LEFT", Cardinality: "MANY_TO_ONE", KeyBindings: []KeyBinding{{SourceColumnID: "14", TargetColumnID: "33"}}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query, tables := testDataset()
			if test.configure != nil {
				test.configure(&query, tables)
			}
			compiled, err := Compile(query, tables, DefaultPolicy())
			if err != nil {
				t.Fatalf("compile actual query: %v", err)
			}
			before, err := json.Marshal(compiled.Tables)
			if err != nil {
				t.Fatal(err)
			}
			dependencies := compiled.DependencyTables()
			if !reflect.DeepEqual(dependencies["1"].UniqueKeys, [][]string{test.baseKey}) || !reflect.DeepEqual(dependencies["3"].UniqueKeys, test.targetKeys) {
				t.Fatalf("dependency keys: base=%v target=%v", dependencies["1"].UniqueKeys, dependencies["3"].UniqueKeys)
			}
			for tableID, probe := range dependencies {
				columns := map[string]Column{}
				for _, column := range probe.Columns {
					columns[column.ID] = column
				}
				for _, required := range probe.UniqueKeys {
					for _, id := range required {
						if columns[id].DBName == "" {
							t.Fatalf("table %s lost required unique-key column %s from %v", tableID, id, required)
						}
					}
					// Oracle compares these complete physical names against live keys.
					if !dependencyMatchesVerifiedKey(probe, required, tables[tableID]) {
						t.Fatalf("table %s dependency key no longer matches the verified Oracle key: %v", tableID, required)
					}
				}
			}
			for _, projection := range compiled.Projections {
				tableID := query.Base.TableID
				if projection.AliasID != query.Base.AliasID {
					for _, relation := range query.Relations {
						if relation.ID == projection.AliasID {
							tableID = relation.TargetTableID
						}
					}
				}
				found := false
				for _, column := range dependencies[tableID].Columns {
					found = found || column.ID == projection.ColumnID
				}
				if !found {
					t.Fatalf("dependency probe lost query projection: %#v", projection)
				}
			}
			after, err := json.Marshal(compiled.Tables)
			if err != nil || string(before) != string(after) {
				t.Fatalf("dependency pruning changed the frozen table contract: %v", err)
			}
		})
	}
}

func TestCompiledDependencyProbeOmitsUnrelatedPrimaryAndUniqueKeys(t *testing.T) {
	query, tables := testDataset()
	dependencyAlternateKey(tables)
	query.Relations[1].KeyBindings[0].TargetColumnID = "33"
	compiled, err := Compile(query, tables, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	target := compiled.DependencyTables()["3"]
	for _, column := range target.Columns {
		if column.ID == "31" {
			t.Fatal("unused target primary key unexpectedly became a query dependency")
		}
	}
	if !reflect.DeepEqual(target.UniqueKeys, [][]string{{"33"}}) || !reflect.DeepEqual(target.PrimaryKeys, []string{"31"}) {
		t.Fatalf("alternate unique-key dependency = %#v", target)
	}
}

func dependencyAlternateKey(tables map[string]Table) {
	target := tables["3"]
	target.Columns[2].OracleType, target.Columns[2].LogicalType = "NUMBER", "integer"
	target.UniqueKeys = [][]string{{"31"}, {"33"}}
	tables["3"] = target
}

func dependencyMatchesVerifiedKey(probe Table, required []string, original Table) bool {
	physicalNames := func(table Table, ids []string) string {
		names := []string{}
		for _, id := range ids {
			for _, column := range table.Columns {
				if column.ID == id {
					names = append(names, column.DBName)
				}
			}
		}
		sort.Strings(names)
		return strings.Join(names, "\x00")
	}
	for _, key := range original.UniqueKeys {
		if len(key) == len(required) && physicalNames(original, key) == physicalNames(probe, required) {
			return true
		}
	}
	return false
}
