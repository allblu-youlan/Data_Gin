package reportoracle

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInspectResultTableReportsGeneratedValuesWithoutDefaultExpressions(t *testing.T) {
	for _, test := range []struct {
		name, identity, defaultOnNull, trigger             string
		defaultValue                                       driver.Value
		wantDefault, wantIdentity, wantOnNull, wantTrigger bool
	}{
		{name: "required without default", identity: "NO", defaultOnNull: "NO", trigger: "NO"},
		{name: "literal NULL is not a usable default", defaultValue: " NULL\n", identity: "NO", defaultOnNull: "NO", trigger: "NO"},
		{name: "parenthesized NULL", defaultValue: "( ( null ) )", identity: "NO", defaultOnNull: "NO", trigger: "NO"},
		{name: "empty string is Oracle NULL", defaultValue: "''", identity: "NO", defaultOnNull: "NO", trigger: "NO"},
		{name: "parenthesized empty string", defaultValue: "( '' )", identity: "NO", defaultOnNull: "NO", trigger: "NO"},
		{name: "zero default", defaultValue: "0", identity: "NO", defaultOnNull: "NO", trigger: "NO", wantDefault: true},
		{name: "quoted NULL is a value", defaultValue: "'NULL'", identity: "NO", defaultOnNull: "NO", trigger: "NO", wantDefault: true},
		{name: "database time", defaultValue: "SYSDATE", identity: "NO", defaultOnNull: "NO", trigger: "NO", wantDefault: true},
		{name: "identity sequence", defaultValue: `"INTERNAL_OWNER"."IMPORT_ID_SEQ".NEXTVAL`, identity: "YES", defaultOnNull: "YES", trigger: "NO", wantDefault: true, wantIdentity: true, wantOnNull: true},
		{name: "insert trigger without column default", identity: "NO", defaultOnNull: "NO", trigger: "YES", wantTrigger: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := &bojunQueryTestRows{
				columns: []string{"COLUMN_NAME", "COLUMN_ID", "DATA_TYPE", "DATA_LENGTH", "DATA_PRECISION", "DATA_SCALE", "NULLABLE", "COMMENTS", "DATA_DEFAULT", "IDENTITY_COLUMN", "DEFAULT_ON_NULL", "HAS_INSERT_TRIGGER"},
				values:  [][]driver.Value{{"REQUIRED_VALUE", int64(1), "NUMBER", int64(22), nil, nil, "N", nil, test.defaultValue, test.identity, test.defaultOnNull, test.trigger}},
			}
			adapter := newBojunQueryTestAdapter(t, func(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
				if arguments[0].Value != "DEMO" || arguments[1].Value != "ORDERS" {
					t.Fatalf("metadata reference was not bound: %+v", arguments)
				}
				for _, fragment := range []string{"c.data_default", "c.identity_column", "c.default_on_null", "all_triggers", "triggers.table_owner = c.owner", "triggers.table_name = c.table_name", "triggers.status = 'ENABLED'", "INSTR(triggers.triggering_event, 'INSERT') > 0"} {
					if !strings.Contains(statement, fragment) {
						t.Fatalf("metadata query is missing %s", fragment)
					}
				}
				return rows, nil
			})
			columns, err := adapter.InspectResultTable(t.Context(), ResultTableRef{Owner: "demo", Name: "orders"})
			if err != nil || len(columns) != 1 {
				t.Fatalf("columns=%+v error=%v", columns, err)
			}
			if !rows.closed {
				t.Fatal("metadata cursor was not closed")
			}
			encoded, err := json.Marshal(columns[0])
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]bool{"hasDefault": test.wantDefault, "identity": test.wantIdentity, "defaultOnNull": test.wantOnNull, "hasInsertTrigger": test.wantTrigger} {
				var got bool
				if err := json.Unmarshal(fields[key], &got); err != nil || got != want {
					t.Fatalf("metadata %s=%s, want %t, error=%v", key, fields[key], want, err)
				}
			}
			if strings.Contains(string(encoded), "IMPORT_ID_SEQ") || strings.Contains(string(encoded), "INTERNAL_OWNER") || strings.Contains(string(encoded), "SYSDATE") {
				t.Fatalf("metadata exposed an internal default expression: %s", encoded)
			}
		})
	}
}

func TestInspectResultTableDoesNotReturnPartialMetadataAfterReadFailure(t *testing.T) {
	rows := &bojunQueryTestRows{
		columns: []string{"COLUMN_NAME", "COLUMN_ID", "DATA_TYPE", "DATA_LENGTH", "DATA_PRECISION", "DATA_SCALE", "NULLABLE", "COMMENTS", "DATA_DEFAULT", "IDENTITY_COLUMN", "DEFAULT_ON_NULL", "HAS_INSERT_TRIGGER"},
		values:  [][]driver.Value{{"ID", int64(1), "NUMBER", int64(22), nil, nil, "N", nil, nil, "NO", "NO", "NO"}},
		nextErr: context.DeadlineExceeded,
	}
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
		if ctx != t.Context() {
			t.Fatal("metadata query did not preserve the caller context")
		}
		return rows, nil
	})
	columns, err := adapter.InspectResultTable(t.Context(), ResultTableRef{Owner: "DEMO", Name: "ORDERS"})
	if !errors.Is(err, context.DeadlineExceeded) || columns != nil || !rows.closed {
		t.Fatalf("incomplete metadata must fail and close its cursor: columns=%+v error=%v closed=%t", columns, err, rows.closed)
	}
}
