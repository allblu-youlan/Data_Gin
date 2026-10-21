package reportrepo

import (
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestAvailableBojunTablesAreFilteredBeforePagination(t *testing.T) {
	statement := buildBojunTableListQuery(newDryRunDB(t), 3, 100, 50, "店仓", "", true).Find(&[]model.ReportBojunTable{}).Statement
	sqlText := statement.SQL.String()
	for _, required := range []string{"source_active = ?", "id > ?", "validation_status = ?", "EXISTS", "approved.table_id = report_bojun_tables.id", "JSON_EXTRACT(approved.config_json, '$.enabled') = true", "ORDER BY id LIMIT 51"} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("available catalog SQL lacks %q: %s", required, sqlText)
		}
	}
	if !containsSQLVariable(statement.Vars, "VALID") || !containsSQLVariable(statement.Vars, "%店仓%") {
		t.Fatalf("query lost status or search: %#v", statement.Vars)
	}
}

func TestBojunAdministrationKeepsUnapprovedTablesVisible(t *testing.T) {
	statement := buildBojunTableListQuery(newDryRunDB(t), 3, 0, 50, "", "", false).Find(&[]model.ReportBojunTable{}).Statement
	if strings.Contains(statement.SQL.String(), "approved") || strings.Contains(statement.SQL.String(), "validation_status") {
		t.Fatalf("administration cannot approve pending or disabled tables: %s", statement.SQL.String())
	}
}
