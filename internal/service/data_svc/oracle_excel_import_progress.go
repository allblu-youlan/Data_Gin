package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"go.uber.org/zap"
)

// OracleExcelImportProgress separates row progress from workflow step counts.
// ProcessedRows are written inside the transaction until Stage is completed.
type OracleExcelImportProgress struct {
	StepIndex     int    `json:"stepIndex"`
	StepName      string `json:"stepName"`
	Stage         string `json:"stage"`
	ProcessedRows int    `json:"processedRows"`
	TotalRows     int    `json:"totalRows"`
	TotalKnown    bool   `json:"totalKnown"`
	ScannedRows   int    `json:"scannedRows"`
	FileIndex     int    `json:"fileIndex"`
	FileCount     int    `json:"fileCount"`
	FileName      string `json:"fileName"`
	SheetName     string `json:"sheetName"`
	RowNumber     int    `json:"rowNumber"`
	Identifier    string `json:"identifier"`
	ElapsedMillis int64  `json:"elapsedMillis,omitempty"`
	CountMillis   int64  `json:"countMillis,omitempty"`
	ReadMillis    int64  `json:"readMillis,omitempty"`
	WriteMillis   int64  `json:"writeMillis,omitempty"`
	BatchRows     int    `json:"batchRows,omitempty"`
	Batches       int    `json:"batches,omitempty"`
}

type oracleExcelImportProgressReporter struct {
	state    OracleExcelImportProgress
	callback func(OracleExcelImportProgress)
	last     time.Time
	written  bool
	started  time.Time
}

func (reporter *oracleExcelImportProgressReporter) emit(force bool) {
	if reporter.callback == nil {
		return
	}
	firstWrite := reporter.state.ProcessedRows > 0 && !reporter.written
	if !force && !firstWrite && time.Since(reporter.last) < 2*time.Second {
		return
	}
	if !reporter.started.IsZero() {
		reporter.state.ElapsedMillis = time.Since(reporter.started).Milliseconds()
	}
	reporter.callback(reporter.state)
	reporter.last = time.Now()
	reporter.written = reporter.written || reporter.state.ProcessedRows > 0
}

func (service *ExcelMatchJobService) oracleExcelImportProgressLogger(ctx context.Context, jobID uint, stepIndex int, stepName string, cancelRun context.CancelFunc) func(OracleExcelImportProgress) {
	var checkedAt time.Time
	return func(progress OracleExcelImportProgress) {
		terminal := progress.Stage == "failed" || progress.Stage == "completed"
		if !terminal {
			if ctx.Err() != nil {
				return
			}
			if progress.Stage == "committing" || time.Since(checkedAt) >= 2*time.Second {
				if err := service.requireExcelWorkflowRunning(ctx, jobID); err != nil {
					cancelRun()
					logCtx, cancelLog := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancelLog()
					message := "任务已停止运行，已取消 Oracle 导入，等待事务回滚"
					if !errors.Is(err, data_dao.ErrExcelMatchJobNotRunning) {
						message = "任务状态检查失败，已停止 Oracle 导入，等待事务回滚"
						zap.L().Error("Check Oracle import job state failed", zap.Uint("job_id", jobID), zap.Error(err))
					}
					service.logJob(logCtx, jobID, "error", message, nil)
					return
				}
				checkedAt = time.Now()
			}
		}
		progress.StepIndex, progress.StepName = stepIndex+1, stepName
		logCtx := ctx
		if terminal {
			logCtx = context.WithoutCancel(ctx)
		}
		logCtx, cancel := context.WithTimeout(logCtx, 5*time.Second)
		defer cancel()
		level := "info"
		if progress.Stage == "failed" {
			level = "error"
		}
		service.logJob(logCtx, jobID, level, oracleExcelImportProgressMessage(progress), map[string]interface{}{"oracleImportProgress": progress})
	}
}

func oracleExcelImportProgressMessage(progress OracleExcelImportProgress) string {
	prefix := fmt.Sprintf("第 %d 步 Oracle 导入", progress.StepIndex)
	if progress.BatchRows > 0 {
		prefix += fmt.Sprintf("（数组批量 %d 行，累计读取 %.1f 秒，Oracle 写入 %.1f 秒）", progress.BatchRows, float64(progress.ReadMillis)/1000, float64(progress.WriteMillis)/1000)
	}
	switch progress.Stage {
	case "counting":
		return fmt.Sprintf("%s：正在统计待导入行数，已扫描 %d 行（文件 %d/%d）", prefix, progress.ScannedRows, progress.FileIndex, progress.FileCount)
	case "importing":
		return fmt.Sprintf("%s：已写入 %d/%d 行，待事务提交（文件 %d/%d）", prefix, progress.ProcessedRows, progress.TotalRows, progress.FileIndex, progress.FileCount)
	case "committing":
		return fmt.Sprintf("%s：已写入 %d/%d 行，正在提交事务", prefix, progress.ProcessedRows, progress.TotalRows)
	case "completed":
		return fmt.Sprintf("%s：已成功导入 %d 行，事务已提交", prefix, progress.ProcessedRows)
	default:
		return fmt.Sprintf("%s失败，本次事务未成功完成", prefix)
	}
}
