package reportdataset

import (
	"strings"
	"testing"
)

func TestBoardManagedScope(t *testing.T) {
	table := Table{Owner: "OPS", PhysicalTable: "ORDERS", ValidationStatus: "VALID", Config: TableConfig{Enabled: true, RequiredFilterColumns: []string{"7"}, DataScope: DataScope{Mode: "SCOPE_COLUMN", ColumnID: "7", UserValues: map[string][]string{"3": {"0009"}}}}, Columns: []Column{{ID: "7", DBName: "STORE", IsPhysical: true, IsSelectable: true, LogicalType: "string", OracleType: "VARCHAR2"}}}
	fields := []string{"STORE"}
	ids := map[string]uint{"STORE": 7}
	p, e := BoardRelation(table, 3, fields, ids, fields)
	if e != nil || !strings.Contains(p.SQL, "B0.STORE IN (:b1)") || strings.Contains(p.SQL, "0009") || len(p.Args) != 1 {
		t.Fatal(p, e)
	}
	if _, e = BoardRelation(table, 4, fields, ids, fields); e == nil {
		t.Fatal("missing scope granted")
	}
	if _, e = BoardRelation(table, 3, fields, ids, nil); e == nil {
		t.Fatal("required filter omitted")
	}
	table.Config.SelectableColumns = &[]string{}
	if _, e = BoardRelation(table, 3, fields, ids, fields); e == nil {
		t.Fatal("revoked field available")
	}
}
