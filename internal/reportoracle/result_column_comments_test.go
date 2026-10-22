package reportoracle

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestInspectResultTableIncludesCommentsAndKeepsUncommentedColumns(t *testing.T) {
	adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(statement, "LEFT JOIN all_col_comments") || !strings.Contains(statement, "cc.owner = c.owner AND cc.table_name = c.table_name AND cc.column_name = c.column_name") {
			t.Fatalf("column comments must be joined by owner, table and column: %s", statement)
		}
		if arguments[0].Value != "DEMO" || arguments[1].Value != "ORDERS" {
			t.Fatalf("metadata reference was not bound: %+v", arguments)
		}
		return &bojunQueryTestRows{
			columns: []string{"COLUMN_NAME", "COLUMN_ID", "DATA_TYPE", "DATA_LENGTH", "DATA_PRECISION", "DATA_SCALE", "NULLABLE", "COMMENTS"},
			values: [][]driver.Value{
				{"CODE", int64(1), "VARCHAR2", int64(128), nil, nil, "N", "订单编号"},
				{"AMOUNT", int64(2), "NUMBER", int64(22), int64(18), int64(2), "Y", nil},
			},
		}, nil
	})
	columns, err := adapter.InspectResultTable(t.Context(), ResultTableRef{Owner: "demo", Name: "orders"})
	if err != nil || len(columns) != 2 {
		t.Fatalf("columns=%+v error=%v", columns, err)
	}
	if columns[0].Comment != "订单编号" || columns[1].Comment != "" || columns[1].Name != "AMOUNT" || *columns[1].DataPrecision != 18 {
		t.Fatalf("comments or column metadata changed: %+v", columns)
	}
}
