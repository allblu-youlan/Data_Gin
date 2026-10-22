package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"gin-biz-web-api/internal/reportdataset"
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
func TestLogicalTableInheritsVerifiedPhysicalDisplayKey(t *testing.T) {
	db := sql.OpenDB(bojunQueryTestConnector{query: func(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "ALL_TAB_COLUMNS"):
			return &bojunQueryTestRows{columns: []string{"COLUMN_NAME", "DATA_TYPE", "DATA_PRECISION", "DATA_SCALE", "NULLABLE"}, values: [][]driver.Value{{"ID", "NUMBER", int64(10), int64(0), "N"}, {"SCUSTOM01", "VARCHAR2", nil, nil, "Y"}}}, nil
		case strings.Contains(query, "AD_COLUMN"):
			if len(args) != 2 || args[0].Value != "16940" || args[1].Value != "12888" {
				t.Fatalf("logical/physical identity lost: %#v", args)
			}
			return &bojunQueryTestRows{columns: []string{"ID", "NAME", "DBNAME", "DESCRIPTION", "ORDERNO", "REF_TABLE_ID", "REF_COLUMN_ID"}, values: [][]driver.Value{{"11", "ID", "ID", "主键", int64(1), nil, nil}, {"99", "SCUSTOM01", "SCUSTOM01", "店仓默认显示", int64(2), nil, nil}}}, nil
		default:
			return &bojunQueryTestRows{columns: []string{"CONSTRAINT_NAME", "CONSTRAINT_TYPE", "COLUMN_NAME"}, values: [][]driver.Value{{"PK_STORE", "P", "ID"}}}, nil
		}
	}})
	defer db.Close()
	a := &Adapter{db: db}
	source := BojunTableMetadata{ID: "16940", Name: "C_V_RESTORE", RealTableID: "12888", PK: "11"}
	actual := BojunTableMetadata{ID: "12888", Name: "C_STORE", PK: "11", DK: "99"}
	table, e := a.InspectBojunTable(t.Context(), BojunNamespace{Owner: "BOSNDS3", DBLink: "Link_To_Bojun"}, source, map[string]BojunTableMetadata{"16940": source, "12888": actual})
	if e != nil || table.LogicalName != "C_V_RESTORE" || table.PhysicalTable != "C_STORE" || len(table.DisplayFields) != 1 || table.DisplayFields[0] != "SCUSTOM01" {
		t.Fatalf("%#v %v", table, e)
	}
}

func TestValidateBojunMetadataDistinguishesSchemaAndUniqueKeyChanges(t *testing.T) {
	precision, scale := 10, 0
	table := reportdataset.Table{
		Owner: "BOS", DBLink: "LINK_BOJUN", PhysicalTable: "C_STORE",
		Columns:    []reportdataset.Column{{ID: "11", DBName: "ID", OracleType: "NUMBER", Precision: &precision, Scale: &scale, IsPhysical: true, IsSelectable: true}},
		UniqueKeys: [][]string{{"11"}},
	}
	for _, tc := range []struct {
		name, nullable, liveKey, wantDetail string
	}{
		{"unchanged", "N", "ID", ""},
		{"nullable changed", "Y", "ID", "column ID differs"},
		{"unique key changed", "N", "OTHER_ID", "columns=[ID]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
				if len(args) != 2 || args[0].Value != "BOS" || args[1].Value != "C_STORE" {
					t.Fatalf("unexpected table bindings: %#v", args)
				}
				if strings.Contains(query, "ALL_TAB_COLUMNS@LINK_BOJUN") {
					return &bojunQueryTestRows{columns: []string{"COLUMN_NAME", "DATA_TYPE", "DATA_PRECISION", "DATA_SCALE", "NULLABLE"}, values: [][]driver.Value{{"ID", "NUMBER", int64(10), int64(0), tc.nullable}}}, nil
				}
				if !strings.Contains(query, "ALL_CONSTRAINTS@LINK_BOJUN") || !strings.Contains(query, "c.STATUS = 'ENABLED' AND c.VALIDATED = 'VALIDATED'") {
					t.Fatalf("unexpected constraint check: %s", query)
				}
				return &bojunQueryTestRows{columns: []string{"CONSTRAINT_NAME", "COLUMN_NAME"}, values: [][]driver.Value{{"PK_STORE", tc.liveKey}}}, nil
			})
			err := adapter.ValidateBojunMetadata(t.Context(), table)
			if tc.wantDetail == "" {
				if err != nil {
					t.Fatalf("unchanged source rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "REPORT_METADATA_CHANGED") || !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("metadata diagnostic = %v, want %q", err, tc.wantDetail)
			}
		})
	}
}

func TestValidateBojunMetadataPreservesOracleFailure(t *testing.T) {
	cause := errors.New("ORA-03113: connection lost")
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, cause
	})
	err := adapter.ValidateBojunMetadata(t.Context(), reportdataset.Table{Owner: "BOS", PhysicalTable: "C_STORE"})
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "REPORT_METADATA_CHANGED") {
		t.Fatalf("Oracle failure misclassified: %v", err)
	}
}
