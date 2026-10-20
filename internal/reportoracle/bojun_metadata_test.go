package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestBojunCatalogAlwaysFiltersActiveBeforePagination(t *testing.T) {
	db := sql.OpenDB(bojunQueryTestConnector{query: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(query, "WHERE ISACTIVE = 'Y' AND ID > :1 ORDER BY ID) WHERE ROWNUM <= :2") || strings.Contains(query, "SELECT *") {
			t.Fatalf("unsafe catalog query: %s", query)
		}
		if len(args) != 2 || args[0].Value != "100" || args[1].Value != 200 {
			t.Fatalf("unexpected binds: %#v", args)
		}
		return &bojunQueryTestRows{columns: []string{"ID", "NAME", "DESCRIPTION", "REALTABLE_ID", "FILTER", "PK", "AK", "DK"}, values: [][]driver.Value{{"12888", "C_STORE", "店仓", nil, nil, "1", "2", "3"}}}, nil
	}})
	defer db.Close()
	a := &Adapter{db: db}
	items, e := a.ListBojunMetadata(t.Context(), BojunNamespace{Owner: "BOSNDS3", DBLink: "Link_To_Bojun"}, "100", 200)
	if e != nil || len(items) != 1 || items[0].DK != "3" {
		t.Fatalf("items=%#v err=%v", items, e)
	}
}
func TestBojunNamespaceRejectsSQLFragments(t *testing.T) {
	for _, n := range []BojunNamespace{{Owner: "BOSNDS3; DROP TABLE X"}, {Owner: "BOSNDS3", DBLink: "LINK@OTHER"}} {
		if _, e := n.object("AD_TABLE"); e == nil {
			t.Fatal("accepted arbitrary SQL")
		}
	}
}
func TestBojunLogicalFilterIsNeverSilentlyDiscarded(t *testing.T) {
	a := &Adapter{}
	source := BojunTableMetadata{ID: "1", Name: "C_V_RESTORE", RealTableID: "2", Filter: "ISACTIVE = 'Y'"}
	meta, e := a.InspectBojunTable(t.Context(), BojunNamespace{Owner: "BOSNDS3"}, source, nil)
	if e != nil || meta.ValidationReason == "" || meta.PhysicalTable != "" {
		t.Fatalf("unsafe logical resolution: %#v %v", meta, e)
	}
}
