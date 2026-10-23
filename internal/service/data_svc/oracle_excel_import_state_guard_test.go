package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestOracleExcelImportLoggerCancelsStoppedJobsBeforeCommit(t *testing.T) {
	for _, status := range []string{"failed", "expired", "success"} {
		t.Run(status, func(t *testing.T) {
			service, state := oracleImportStateGuardService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			log := service.oracleExcelImportProgressLogger(ctx, 17, 2, "导入订单", cancel)
			log(OracleExcelImportProgress{Stage: "importing"})
			if ctx.Err() != nil || state.reads != 1 {
				t.Fatalf("running job was stopped: error=%v reads=%d", ctx.Err(), state.reads)
			}
			state.status = status
			log(OracleExcelImportProgress{Stage: "importing", ProcessedRows: 1000})
			if state.reads != 1 {
				t.Fatal("progress callback bypassed the two-second state-check throttle")
			}
			log(OracleExcelImportProgress{Stage: "committing", ProcessedRows: 1000})
			if !errors.Is(ctx.Err(), context.Canceled) || state.reads != 2 {
				t.Fatalf("stopped job reached commit: error=%v reads=%d", ctx.Err(), state.reads)
			}
			log(OracleExcelImportProgress{Stage: "failed", ProcessedRows: 1000})
			last := state.logs[len(state.logs)-1]
			var detail struct {
				Progress OracleExcelImportProgress `json:"oracleImportProgress"`
			}
			if err := json.Unmarshal([]byte(last.DetailJSON), &detail); err != nil {
				t.Fatal(err)
			}
			if detail.Progress.Stage != "failed" || detail.Progress.StepIndex != 3 || detail.Progress.StepName != "导入订单" || state.logContextErr != nil {
				t.Fatalf("rollback progress lost after cancellation: detail=%+v context=%v", detail, state.logContextErr)
			}
		})
	}
}

func TestOracleExcelImportLoggerStopsWhenTaskStateCannotBeRead(t *testing.T) {
	service, state := oracleImportStateGuardService(t)
	state.queryErr = errors.New("database unavailable")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service.oracleExcelImportProgressLogger(ctx, 17, 0, "导入订单", cancel)(OracleExcelImportProgress{Stage: "counting"})
	if !errors.Is(ctx.Err(), context.Canceled) || len(state.logs) != 1 || !strings.Contains(state.logs[0].Message, "任务状态检查失败") || state.logContextErr != nil {
		t.Fatalf("unreadable state did not stop safely: error=%v state=%+v", ctx.Err(), state)
	}
}

type oracleImportStateGuardState struct {
	status        string
	queryErr      error
	reads         int
	logs          []model.ExcelMatchJobLog
	logContextErr error
}

func oracleImportStateGuardService(t *testing.T) (*ExcelMatchJobService, *oracleImportStateGuardState) {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	state := &oracleImportStateGuardState{status: "running"}
	if err := db.Callback().Query().After("gorm:query").Register("test:oracle_import_state", func(tx *gorm.DB) {
		state.reads++
		if state.queryErr != nil {
			tx.AddError(state.queryErr)
			return
		}
		*tx.Statement.Dest.(*model.ExcelMatchJob) = model.ExcelMatchJob{BaseModel: model.BaseModel{ID: 17}, Status: state.status}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register("test:oracle_import_guard_log", func(tx *gorm.DB) {
		state.logContextErr = tx.Statement.Context.Err()
		state.logs = append(state.logs, *tx.Statement.Dest.(*model.ExcelMatchJobLog))
	}); err != nil {
		t.Fatal(err)
	}
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	return &ExcelMatchJobService{jobDAO: data_dao.NewExcelMatchJobDAO()}, state
}
