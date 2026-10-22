package reportrepo

import (
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestAvailableBojunTablesAreFilteredBeforePagination(t *testing.T) {
	statement := buildBojunTableListQuery(newDryRunDB(t), 3, 100, 50, "店仓", "", true).Find(&[]model.ReportBojunTable{}).Statement
	sqlText := statement.SQL.String()
	for _, required := range []string{"source_active = ?", "id > ?", "validation_status IN", "NOT EXISTS", "blocked.table_id = report_bojun_tables.id", "JSON_EXTRACT(blocked.config_json, '$.enabled') = false", "ORDER BY id LIMIT 51"} {
		if !strings.Contains(sqlText, required) {
			t.Fatalf("available catalog SQL lacks %q: %s", required, sqlText)
		}
	}
	if !containsSQLVariable(statement.Vars, "VALID") || !containsSQLVariable(statement.Vars, "PENDING") || !containsSQLVariable(statement.Vars, "%店仓%") {
		t.Fatalf("query lost status or search: %#v", statement.Vars)
	}
}

func TestBojunAdministrationKeepsUnapprovedTablesVisible(t *testing.T) {
	statement := buildBojunTableListQuery(newDryRunDB(t), 3, 0, 50, "", "", false).Find(&[]model.ReportBojunTable{}).Statement
	if strings.Contains(statement.SQL.String(), "approved") || strings.Contains(statement.SQL.String(), "validation_status") {
		t.Fatalf("administration cannot approve pending or disabled tables: %s", statement.SQL.String())
	}
}

func TestBojunReferenceUsesExactDictionaryIDAndDatasource(t *testing.T) {
	statement := bojunTableSourceScope(newDryRunDB(t), 7, "00012345678901234567890").First(&model.ReportBojunTable{}).Statement
	for _, required := range []string{"datasource_id = ?", "bojun_table_id = ?", "source_active = ?"} {
		if !strings.Contains(statement.SQL.String(), required) {
			t.Fatalf("reference catalog SQL lacks %q: %s", required, statement.SQL.String())
		}
	}
	if len(statement.Vars) < 3 || statement.Vars[0] != uint(7) || statement.Vars[1] != "00012345678901234567890" || statement.Vars[2] != true {
		t.Fatalf("reference scope variables = %#v", statement.Vars)
	}
}
