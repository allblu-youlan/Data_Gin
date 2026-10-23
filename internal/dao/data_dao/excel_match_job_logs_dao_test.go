package data_dao

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func TestExcelMatchJobLogsReturnNewestWindowInChronologicalOrder(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int
		want  string
	}{
		{"bounded window", 3, "LIMIT 3"},
		{"default window", 0, "LIMIT 200"},
		{"oversized window", 501, "LIMIT 200"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := dryRunWeatherDAOTestDB(t)
			if err := db.Callback().Query().After("gorm:query").Register("test:latest_excel_logs", func(tx *gorm.DB) {
				statement := tx.Statement.SQL.String()
				if !strings.Contains(statement, "job_id = ?") || !strings.Contains(statement, "ORDER BY id DESC") || !strings.Contains(statement, test.want) ||
					len(tx.Statement.Vars) != 1 || tx.Statement.Vars[0] != uint(17) {
					t.Fatalf("latest log window is not bound or limited: SQL=%s args=%v", statement, tx.Statement.Vars)
				}
				*tx.Statement.Dest.(*[]model.ExcelMatchJobLog) = []model.ExcelMatchJobLog{
					{BaseModel: model.BaseModel{ID: 206}, Message: "completed"},
					{BaseModel: model.BaseModel{ID: 205}, Message: "committing"},
					{BaseModel: model.BaseModel{ID: 204}, Message: "importing"},
				}
			}); err != nil {
				t.Fatal(err)
			}
			logs, err := (&ExcelMatchJobDAO{db: db}).FindLogsByJobID(t.Context(), 17, test.limit)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uint, len(logs))
			messages := make([]string, len(logs))
			for index, log := range logs {
				ids[index], messages[index] = log.ID, log.Message
			}
			if !reflect.DeepEqual(ids, []uint{204, 205, 206}) || !reflect.DeepEqual(messages, []string{"importing", "committing", "completed"}) {
				t.Fatalf("latest logs lost chronological order: ids=%v messages=%v", ids, messages)
			}
		})
	}
}

func TestExcelMatchJobLogsPropagateQueryFailure(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	want := errors.New("log storage unavailable")
	if err := db.Callback().Query().After("gorm:query").Register("test:excel_log_failure", func(tx *gorm.DB) { tx.AddError(want) }); err != nil {
		t.Fatal(err)
	}
	logs, err := (&ExcelMatchJobDAO{db: db}).FindLogsByJobID(t.Context(), 17, 200)
	if !errors.Is(err, want) || logs != nil {
		t.Fatalf("logs=%v error=%v, want nil and %v", logs, err, want)
	}
}
