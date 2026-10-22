package data_dao

import (
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestExcelWorkflowClaimNeverReplaysRunningOrFailedWrites(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	dao := &ExcelMatchJobDAO{db: db}
	var statement string
	var arguments []interface{}
	if err := db.Callback().Update().After("gorm:update").Register("capture_workflow_claim", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		arguments = append([]interface{}(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := dao.ClaimWorkflow(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement, "id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)") {
		t.Fatalf("claim query: %s", statement)
	}
	var pending bool
	for _, argument := range arguments {
		if argument == "pending" {
			pending = true
		}
		if argument == "failed" {
			t.Fatal("must not reclaim failed writes")
		}
	}
	if !pending {
		t.Fatalf("pending claim missing: %v", arguments)
	}
}
