package data_dao

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestDeleteOracleExportSchemeKeepsOtherSchemeTypes(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	dao := &ExcelMatchJobDAO{db: db}
	var statement string
	var arguments []interface{}
	if err := db.Callback().Delete().After("gorm:delete").Register("capture_oracle_scheme_delete", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		arguments = append([]interface{}(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	if err := dao.DeleteOracleExportScheme(t.Context(), 17); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement, "id = ? AND operation = ?") || len(arguments) != 2 || arguments[0] != uint(17) || arguments[1] != "oracle_export" {
		t.Fatalf("delete is not scoped to Oracle configurations: SQL=%s arguments=%v", statement, arguments)
	}
}
