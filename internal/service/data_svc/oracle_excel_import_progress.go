package data_svc

import (
	"context"
	"fmt"
	"time"
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
}

type oracleExcelImportProgressReporter struct {
	state    OracleExcelImportProgress
	callback func(OracleExcelImportProgress)
	last     time.Time
	written  bool
}

func (reporter *oracleExcelImportProgressReporter) emit(force bool) {
	if reporter.callback == nil {
		return
	}
	firstWrite := reporter.state.ProcessedRows > 0 && !reporter.written
	if !force && !firstWrite && time.Since(reporter.last) < 2*time.Second {
		return
	}
	reporter.callback(reporter.state)
	reporter.last = time.Now()
	reporter.written = reporter.written || reporter.state.ProcessedRows > 0
}

func (service *ExcelMatchJobService) oracleExcelImportProgressLogger(ctx context.Context, jobID uint, stepIndex int, stepName string) func(OracleExcelImportProgress) {
	return func(progress OracleExcelImportProgress) {
		progress.StepIndex, progress.StepName = stepIndex+1, stepName
		logCtx := ctx
		if progress.Stage == "failed" || progress.Stage == "completed" {
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
