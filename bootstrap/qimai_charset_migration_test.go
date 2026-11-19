package bootstrap

import (
	"database/sql"
	"strings"
	"testing"
)

func TestQimaiCharsetSQLPreservesColumnTypesAndNullability(t *testing.T) {
	statement, err := buildQimaiCharsetSQL("utf8_general_ci", []qimaiCharsetColumn{
		{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8", Collation: "utf8_general_ci"},
		{Name: "order_no", ColumnType: "varchar(100)", Nullable: "NO", Charset: "utf8", Collation: "utf8_general_ci"},
		{Name: "buyer_remarks", ColumnType: "text", Nullable: "YES", Charset: "utf8", Collation: "utf8_general_ci"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ALTER TABLE `qimai_order_data` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci",
		"MODIFY COLUMN `contact_name` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NULL DEFAULT NULL",
		"MODIFY COLUMN `order_no` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL",
		"MODIFY COLUMN `buyer_remarks` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NULL DEFAULT NULL",
	} {
		if !strings.Contains(statement, want) {
			t.Fatalf("migration lost column definition %q: %s", want, statement)
		}
	}
	if strings.Contains(statement, "CONVERT TO") || strings.Contains(statement, "mediumtext") {
		t.Fatalf("migration widens TEXT implicitly: %s", statement)
	}
}

func TestQimaiCharsetSQLIsIdempotent(t *testing.T) {
	statement, err := buildQimaiCharsetSQL("utf8mb4_general_ci", []qimaiCharsetColumn{
		{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
	})
	if err != nil || statement != "" {
		t.Fatalf("already migrated table: sql=%q error=%v", statement, err)
	}
}

func TestQimaiCharsetSQLPreservesExistingUTF8MB4Collations(t *testing.T) {
	for _, collation := range []string{"utf8mb4_bin", "utf8mb4_unicode_ci"} {
		t.Run(collation, func(t *testing.T) {
			statement, err := buildQimaiCharsetSQL(collation, []qimaiCharsetColumn{
				{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8mb4", Collation: collation},
			})
			if err != nil || statement != "" {
				t.Fatalf("existing UTF8MB4 comparison semantics changed: sql=%q error=%v", statement, err)
			}
		})
	}
	statement, err := buildQimaiCharsetSQL("utf8mb4_bin", []qimaiCharsetColumn{
		{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8mb4", Collation: "utf8mb4_bin"},
		{Name: "buyer_remarks", ColumnType: "text", Nullable: "YES", Charset: "utf8mb3", Collation: "utf8mb3_general_ci"},
	})
	if err != nil || strings.Contains(statement, "contact_name") || strings.Contains(statement, "DEFAULT CHARACTER SET") || !strings.Contains(statement, "MODIFY COLUMN `buyer_remarks`") {
		t.Fatalf("mixed table migration changed more than its legacy column: sql=%q error=%v", statement, err)
	}
}

func TestQimaiCharsetSQLRejectsUnknownLegacyCollations(t *testing.T) {
	column := qimaiCharsetColumn{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8", Collation: "utf8_general_ci"}
	if _, err := buildQimaiCharsetSQL("utf8_unicode_ci", []qimaiCharsetColumn{column}); err == nil {
		t.Fatal("unexpected legacy table collation was silently changed")
	}
	column.Collation = "utf8_bin"
	if _, err := buildQimaiCharsetSQL("utf8_general_ci", []qimaiCharsetColumn{column}); err == nil {
		t.Fatal("unexpected legacy column collation was silently changed")
	}
}

func TestQimaiCharsetSQLRejectsUnexpectedDefinitionBeforeDDL(t *testing.T) {
	column := qimaiCharsetColumn{Name: "contact_name", ColumnType: "varchar(50)", Nullable: "YES", Charset: "utf8", Collation: "utf8_general_ci"}
	for _, change := range []func(*qimaiCharsetColumn){
		func(c *qimaiCharsetColumn) { c.Default = sql.NullString{String: "existing default", Valid: true} },
		func(c *qimaiCharsetColumn) { c.Extra = "VIRTUAL GENERATED" },
		func(c *qimaiCharsetColumn) { c.ColumnType = "varchar(50); DROP TABLE users" },
	} {
		current := column
		change(&current)
		if _, err := buildQimaiCharsetSQL("utf8_general_ci", []qimaiCharsetColumn{current}); err == nil {
			t.Fatal("unexpected definition was accepted instead of preserving it")
		}
	}
}
