package data_dao

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestExcelMatchJobStateWritesPreserveStoppedJobs(t *testing.T) {
	for _, test := range []struct {
		name      string
		condition string
		wantTail  []interface{}
		update    func(*ExcelMatchJobDAO) error
	}{
		{
			name: "success requires running", condition: "id = ? AND status = ?", wantTail: []interface{}{uint(17), "running"},
			update: func(dao *ExcelMatchJobDAO) error {
				return dao.MarkSuccess(t.Context(), 17, ExcelMatchJobStats{ProcessedRows: 10}, time.Now().Add(time.Hour))
			},
		},
		{
			name: "failure preserves existing terminal status and reason", condition: "id = ? AND status IN (?,?)", wantTail: []interface{}{uint(17), "pending", "running"},
			update: func(dao *ExcelMatchJobDAO) error {
				return dao.MarkFailed(t.Context(), 17, "worker failed", time.Now().Add(time.Hour))
			},
		},
		{
			name: "progress requires running", condition: "id = ? AND status = ?", wantTail: []interface{}{uint(17), "running"},
			update: func(dao *ExcelMatchJobDAO) error {
				return dao.UpdateProgress(t.Context(), 17, ExcelMatchJobStats{ProcessedRows: 5})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := dryRunWeatherDAOTestDB(t)
			if err := db.Callback().Update().After("gorm:update").Register("test:excel_state_guard", func(tx *gorm.DB) {
				if !strings.Contains(tx.Statement.SQL.String(), test.condition) {
					t.Fatalf("missing state guard: %s", tx.Statement.SQL.String())
				}
				vars := tx.Statement.Vars
				if len(vars) < len(test.wantTail) || !reflect.DeepEqual(vars[len(vars)-len(test.wantTail):], test.wantTail) {
					t.Fatalf("unexpected state guard arguments: %v", vars)
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			if err := test.update(&ExcelMatchJobDAO{db: db}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExcelMatchJobSuccessReportsStoppedJobAndDatabaseFailure(t *testing.T) {
	for _, databaseErr := range []error{nil, errors.New("database unavailable")} {
		db := dryRunWeatherDAOTestDB(t)
		if err := db.Callback().Update().After("gorm:update").Register("test:excel_success_conflict", func(tx *gorm.DB) {
			tx.RowsAffected = 0
			if databaseErr != nil {
				tx.AddError(databaseErr)
			}
		}); err != nil {
			t.Fatal(err)
		}
		err := (&ExcelMatchJobDAO{db: db}).MarkSuccess(t.Context(), 17, ExcelMatchJobStats{}, time.Now())
		want := databaseErr
		if want == nil {
			want = ErrExcelMatchJobNotRunning
		}
		if !errors.Is(err, want) {
			t.Fatalf("error=%v want=%v", err, want)
		}
	}
}
