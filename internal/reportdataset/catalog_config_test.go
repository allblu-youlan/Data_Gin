package reportdataset

import "testing"

func TestCatalogDefaultsInheritWithoutPerTableConfiguration(t *testing.T) {
	for _, raw := range []string{"", "{}", "null"} {
		config, err := CatalogConfig([]byte(raw))
		if err != nil || !config.Enabled || config.DataScope.Mode != "GLOBAL_APPROVED" || config.PrimaryKeys != nil || config.DisplayFields != nil || config.SelectableColumns != nil {
			t.Fatalf("dictionary defaults require redundant configuration: %s", raw)
		}
	}
}

func TestCatalogDefaultsKeepExplicitExistingRestrictions(t *testing.T) {
	config, err := CatalogConfig([]byte(`{"enabled":false,"selectableColumns":[],"detailColumns":["11"],"dataScope":{"mode":"SCOPE_COLUMN","columnId":"10","userValues":{"17":["001"]}}}`))
	if err != nil || config.Enabled || config.SelectableColumns == nil || len(*config.SelectableColumns) != 0 || config.DetailColumns == nil || (*config.DetailColumns)[0] != "11" || config.DataScope.Mode != "SCOPE_COLUMN" || config.DataScope.UserValues["17"][0] != "001" {
		t.Fatal("dictionary inheritance widened an explicit restriction")
	}
	if _, err := CatalogConfig([]byte(`{"enabled":`)); err == nil {
		t.Fatal("invalid override must not fall back to inherited permissions")
	}
}

func TestInheritedDictionaryBuildsRelationDisplayWithoutManualConfiguration(t *testing.T) {
	query, tables := testDataset()
	for id, table := range tables {
		var err error
		table.Config, err = CatalogConfig(nil)
		if err != nil {
			t.Fatal(err)
		}
		tables[id] = table
	}
	compiled, err := Compile(query, tables, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Build(nil, QueryInputs{}, "7", false); err != nil {
		t.Fatal(err)
	}
	if compiled.Relations["retail"].Relation.DisplayColumnIDs[0] != "22" || compiled.Relations["product"].Relation.DisplayColumnIDs[0] != "32" {
		t.Fatal("relation display fields did not inherit the source dictionary")
	}
}
