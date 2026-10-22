package data_dao

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestDeleteOracleOperationSchemeIsScopedToOperation(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	dao := &ExcelMatchJobDAO{db: db}
	var statement string
	var arguments []interface{}
	if err := db.Callback().Delete().After("gorm:delete").Register("capture_oracle_operation_delete", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		arguments = append([]interface{}(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	if err := dao.DeleteOperationScheme(t.Context(), "oracle_procedure", 19); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement, "id = ? AND operation = ?") || len(arguments) != 2 || arguments[0] != uint(19) || arguments[1] != "oracle_procedure" {
		t.Fatalf("unscoped delete: SQL=%s arguments=%v", statement, arguments)
	}
}
