package reportoracle

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/godror/godror"
)

func TestJSONSnapshotStoreMissingIsNotAReportMetadataMismatch(t *testing.T) {
	ctx := t.Context()
	rows := &bojunQueryTestRows{columns: []string{"COUNT(*)"}, values: [][]driver.Value{{int64(0)}}}
	adapter := newBojunQueryTestAdapter(t, func(received context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
		if received != ctx || len(args) != 0 || !strings.Contains(query, "user_tab_columns") {
			t.Fatalf("snapshot store query context = %v, SQL = %s, arguments = %#v", received, query, args)
		}
		return rows, nil
	})
	err := adapter.ValidateJSONSnapshotStore(ctx)
	if !errors.Is(err, ErrJSONSnapshotStoreNotReady) || errors.Is(err, ErrMetadataMismatch) {
		t.Fatalf("missing deployment store must not be a report metadata mismatch: %v", err)
	}
	if !rows.closed {
		t.Fatal("snapshot store inspection did not close rows")
	}
}

func TestJSONSnapshotStoreInspectionSeparatesMissingColumnsAndDatabaseErrors(t *testing.T) {
	queryError := errors.New("connection reset")
	for _, test := range []struct {
		name     string
		count    int64
		queryErr error
		want     error
	}{
		{name: "one column missing", count: 8, want: ErrJSONSnapshotStoreNotReady},
		{name: "ready", count: 9},
		{name: "query failed", queryErr: queryError, want: queryError},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := &bojunQueryTestRows{columns: []string{"COUNT(*)"}, values: [][]driver.Value{{test.count}}}
			adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				return rows, test.queryErr
			})
			err := adapter.ValidateJSONSnapshotStore(t.Context())
			if !errors.Is(err, test.want) || test.queryErr != nil && errors.Is(err, ErrJSONSnapshotStoreNotReady) {
				t.Fatalf("snapshot store error = %v, want %v", err, test.want)
			}
			if test.queryErr == nil && !rows.closed {
				t.Fatal("snapshot store inspection did not close rows")
			}
		})
	}
}

func TestBuildJSONCursorCallPlanUsesDiscoveredArgumentNames(t *testing.T) {
	plan, err := BuildJSONCursorCallPlan(
		ProcedureRef{Owner: "report_owner", Package: "pkg_report", Name: "query_report"},
		[]ProcedureArgument{
			{Name: "P_QUERY_JSON", Position: 1, Direction: "IN", DataType: "CLOB"},
			{Name: "P_DATA", Position: 2, Direction: "OUT", DataType: "REF CURSOR"},
		},
		"p_query_json", "p_data",
	)
	if err != nil {
		t.Fatalf("BuildJSONCursorCallPlan() error = %v", err)
	}
	if got, want := plan.Statement(), "BEGIN REPORT_OWNER.PKG_REPORT.QUERY_REPORT(P_QUERY_JSON => :payload, P_DATA => :resultCursor); END;"; got != want {
		t.Fatalf("Statement() = %q, want %q", got, want)
	}
}

func TestBuildJSONCursorCallPlanRejectsExtraOrMismatchedArguments(t *testing.T) {
	tests := []struct {
		name      string
		arguments []ProcedureArgument
	}{
		{name: "extra", arguments: []ProcedureArgument{{Name: "P_JSON", Direction: "IN", DataType: "CLOB"}, {Name: "P_RESULT", Direction: "OUT", DataType: "REF CURSOR"}, {Name: "P_OTHER", Direction: "IN", DataType: "VARCHAR2"}}},
		{name: "wrong input", arguments: []ProcedureArgument{{Name: "P_JSON", Direction: "IN", DataType: "NUMBER"}, {Name: "P_RESULT", Direction: "OUT", DataType: "REF CURSOR"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildJSONCursorCallPlan(ProcedureRef{Owner: "OWNER", Name: "PROC"}, test.arguments, "P_JSON", "P_RESULT"); err == nil {
				t.Fatal("BuildJSONCursorCallPlan() error = nil")
			}
		})
	}
}

func TestCursorSnapshotValueRoundTripPreservesNumbersAndDates(t *testing.T) {
	instant := time.Date(2026, 5, 4, 8, 30, 0, 123, time.FixedZone("CST", 8*60*60))
	columns := []CursorColumn{{Name: "AMOUNT", OracleType: "NUMBER"}, {Name: "CREATED_AT", OracleType: "TIMESTAMP"}, {Name: "NAME", OracleType: "VARCHAR2"}}
	encoded, err := encodeCursorValues([]interface{}{godror.Number("12345678901234567890.12"), instant, []byte("测试")}, columns)
	if err != nil {
		t.Fatalf("encodeCursorValues() error = %v", err)
	}
	decoded, err := decodeCursorValues(encoded, len(columns))
	if err != nil {
		t.Fatalf("decodeCursorValues() error = %v", err)
	}
	if number, ok := decoded[0].(json.Number); !ok || number.String() != "12345678901234567890.12" {
		t.Fatalf("number = %#v", decoded[0])
	}
	if decoded[1] != instant.UTC().Format(time.RFC3339Nano) || decoded[2] != "测试" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestSnapshotColumnIndexesUsesOracleNamesAndRequestedOrder(t *testing.T) {
	indexes, columns, err := snapshotColumnIndexes(
		[]CursorColumn{{Name: "A"}, {Name: "B"}},
		[]string{"b", "a"},
	)
	if err != nil {
		t.Fatalf("snapshotColumnIndexes() error = %v", err)
	}
	if indexes[0] != 1 || indexes[1] != 0 || columns[0] != "B" || columns[1] != "A" {
		t.Fatalf("indexes=%v columns=%v", indexes, columns)
	}
}
