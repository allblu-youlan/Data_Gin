package data_dao

import (
	"context"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestIsSafeExcelSQLIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "table", value: "bojun_retail_orders", want: true},
		{name: "column", value: "matched_docno", want: true},
		{name: "starts with number", value: "1_orders", want: false},
		{name: "contains dot", value: "warehouse.orders", want: false},
		{name: "contains sql", value: "orders` WHERE 1=1 --", want: false},
		{name: "empty", value: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeExcelSQLIdentifier(tt.value); got != tt.want {
				t.Fatalf("isSafeExcelSQLIdentifier(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestExcelMatchJobPageQueryUsesBoundFilters(t *testing.T) {
	dao := &ExcelMatchJobDAO{db: dryRunWeatherDAOTestDB(t)}
	params := ExcelMatchJobListQuery{Page: 2, PageSize: 20, Keyword: "orders", Status: "failed", Operation: "write"}
	query := dao.applyListFilters(dao.db.WithContext(context.Background()).Model(&model.ExcelMatchJob{}), params).
		Order("id DESC").Offset((params.Page - 1) * params.PageSize).Limit(params.PageSize).
		Find(&[]model.ExcelMatchJob{})
	if query.Error != nil {
		t.Fatalf("build Excel job page query: %v", query.Error)
	}
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{
		"source_file_name LIKE ? OR error_message LIKE ?",
		"status = ?",
		"JSON_UNQUOTE(JSON_EXTRACT(config_json, '$.operation')) IN (?,?)",
		"LIMIT 20",
		"OFFSET 20",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("Excel job page query missing %q: %s", fragment, statement)
		}
	}
	for _, boundValue := range []string{"orders", "failed", "import_update", "clear_matched_docno"} {
		if strings.Contains(statement, boundValue) {
			t.Fatalf("Excel job page query interpolated %q: %s", boundValue, statement)
		}
	}
	wantVars := []any{"%orders%", "%orders%", "failed", "import_update", "clear_matched_docno"}
	if len(query.Statement.Vars) != len(wantVars) {
		t.Fatalf("Excel job page query vars = %#v, want %#v", query.Statement.Vars, wantVars)
	}
	for index, want := range wantVars {
		if query.Statement.Vars[index] != want {
			t.Fatalf("Excel job page query vars[%d] = %#v, want %#v", index, query.Statement.Vars[index], want)
		}
	}
}

func TestExcelMatchJobPageQueryMatchesPositiveTaskID(t *testing.T) {
	dao := &ExcelMatchJobDAO{db: dryRunWeatherDAOTestDB(t)}
	params := ExcelMatchJobListQuery{Page: 1, PageSize: 20, Keyword: "42", Operation: "match"}
	query := dao.applyListFilters(dao.db.WithContext(context.Background()).Model(&model.ExcelMatchJob{}), params).
		Find(&[]model.ExcelMatchJob{})
	if query.Error != nil {
		t.Fatalf("build Excel job ID query: %v", query.Error)
	}
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{
		"id = ? OR source_file_name LIKE ? OR error_message LIKE ?",
		"COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(config_json, '$.operation')), ''), ?) = ?",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("Excel job ID query missing %q: %s", fragment, statement)
		}
	}
	if strings.Contains(statement, "42") || strings.Contains(statement, "export_match") {
		t.Fatalf("Excel job page query interpolated filters: %s", statement)
	}
	wantVars := []any{uint64(42), "%42%", "%42%", "export_match", "export_match"}
	if len(query.Statement.Vars) != len(wantVars) {
		t.Fatalf("Excel job ID query vars = %#v, want %#v", query.Statement.Vars, wantVars)
	}
	for index, want := range wantVars {
		if query.Statement.Vars[index] != want {
			t.Fatalf("Excel job ID query vars[%d] = %#v, want %#v", index, query.Statement.Vars[index], want)
		}
	}
}

func TestExcelMatchJobPageQueryDoesNotInterpolateKeyword(t *testing.T) {
	dao := &ExcelMatchJobDAO{db: dryRunWeatherDAOTestDB(t)}
	keyword := "orders%' OR 1=1 --"
	query := dao.applyListFilters(
		dao.db.WithContext(context.Background()).Model(&model.ExcelMatchJob{}),
		ExcelMatchJobListQuery{Page: 1, PageSize: 20, Keyword: keyword},
	).Find(&[]model.ExcelMatchJob{})
	if query.Error != nil {
		t.Fatalf("build Excel job keyword query: %v", query.Error)
	}
	statement := query.Statement.SQL.String()
	if strings.Contains(statement, keyword) {
		t.Fatalf("Excel job keyword query interpolated user input: %s", statement)
	}
	wantPattern := "%" + keyword + "%"
	if len(query.Statement.Vars) != 2 || query.Statement.Vars[0] != wantPattern || query.Statement.Vars[1] != wantPattern {
		t.Fatalf("Excel job keyword query vars = %#v", query.Statement.Vars)
	}
}

func TestExcelMatchJobPageQueryFiltersOracleExports(t *testing.T) {
	dao := &ExcelMatchJobDAO{db: dryRunWeatherDAOTestDB(t)}
	query := dao.applyListFilters(dao.db.WithContext(t.Context()).Model(&model.ExcelMatchJob{}), ExcelMatchJobListQuery{Operation: "export"}).Find(&[]model.ExcelMatchJob{})
	if query.Error != nil || !strings.Contains(query.Statement.SQL.String(), "JSON_UNQUOTE(JSON_EXTRACT(config_json, '$.operation')) = ?") || len(query.Statement.Vars) != 1 || query.Statement.Vars[0] != "oracle_export" {
		t.Fatalf("Oracle export filter SQL=%s vars=%v err=%v", query.Statement.SQL.String(), query.Statement.Vars, query.Error)
	}
}
