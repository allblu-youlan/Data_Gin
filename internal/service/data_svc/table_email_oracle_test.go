package data_svc

import (
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestTableEmailOracleQueryBindsWatermarkAndBoundsBatch(t *testing.T) {
	query, err := tableEmailIncrementalSQL(TableEmailRuleInput{Owner: "ERP", TableName: "ORDERS", CursorColumn: "ID"}, []string{"ID", "EMAIL"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"ERP"."ORDERS"`, `"ID" > :after_id`, `ORDER BY "ID" ASC`, `ROWNUM <= :batch_size`} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %q: %s", fragment, query)
		}
	}
	if !reportoracle.ValidateSelect(query) {
		t.Fatalf("not a permitted SELECT: %s", query)
	}
	for _, column := range []string{`ID; DELETE FROM ORDERS`, `ID"`, `ID DESC`} {
		if _, err := tableEmailIncrementalSQL(TableEmailRuleInput{Owner: "ERP", TableName: "ORDERS", CursorColumn: column}, []string{"ID"}); err == nil {
			t.Fatalf("accepted identifier %q", column)
		}
	}
}

func TestTableEmailCursorPreservesLargeIntegerAndRejectsDecimals(t *testing.T) {
	for _, value := range []string{"0", "9007199254740993", "12345678901234567890123456789012345678"} {
		if got, err := normalizeTableEmailCursor(value); err != nil || got != value {
			t.Fatalf("cursor %q: %s, %v", value, got, err)
		}
	}
	for _, value := range []string{"1.5", "NaN", "1 OR 1=1", ""} {
		if _, err := normalizeTableEmailCursor(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
