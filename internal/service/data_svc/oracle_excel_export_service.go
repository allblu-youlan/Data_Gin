package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"

	"github.com/hibiken/asynq"
)

func (s *ExcelMatchJobService) CreateOracleExportJob(ctx context.Context, input OracleExcelExportConfig) (*model.ExcelMatchJob, error) {
	if global.QueueJobClient == nil {
		return nil, errors.New("异步任务客户端未初始化")
	}
	normalized, err := normalizeOracleExcelExportConfig(ctx, input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(excelMatchRetention)
	export := &model.ExcelMatchJob{
		SourceFileName: oracleExcelExportSourceName(normalized),
		ConfigJSON:     string(encoded), Status: excelMatchStatusPending,
		ExpiresAt: &model.TimeNormal{Time: expiresAt},
	}
	if _, err := s.jobDAO.Create(ctx, export); err != nil {
		return nil, err
	}
	// No uploaded source is required, so any worker can recreate this export.
	task, err := job.NewExcelMatchExportTask(export.ID)
	if err == nil {
		_, err = global.QueueJobClient.Enqueue(task, asynq.TaskID(oracleExcelExportTaskID(export.ID)), asynq.Timeout(2*time.Hour), asynq.MaxRetry(3))
	}
	if err != nil {
		_ = s.jobDAO.MarkFailed(ctx, export.ID, "投递 Oracle 导出任务失败", expiresAt)
		return nil, err
	}
	s.logJob(ctx, export.ID, "info", "Oracle Excel 导出任务已创建，超过 100 万行的表将在同一 Excel 内均分为多个 Sheet", nil)
	return export, nil
}

func normalizeOracleExcelExportConfig(ctx context.Context, input OracleExcelExportConfig) (OracleExcelExportConfig, error) {
	var normalized OracleExcelExportConfig
	err := withOracleExcelConnection(ctx, 30*time.Second, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		var err error
		normalized, err = normalizeOracleExcelExportWithMetadata(queryCtx, input, connection.InspectResultTable)
		return err
	})
	return normalized, err
}

func withOracleExcelConnection(ctx context.Context, timeout time.Duration, action func(context.Context, *reportoracle.Adapter) error) error {
	config, _, err := officeOracleConfigFromEnvironment()
	if err != nil {
		return err
	}
	openCtx, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
	connection, err := reportoracle.Open(openCtx, config)
	cancel()
	if err != nil {
		return err
	}
	defer connection.Close()
	queryCtx, queryCancel := context.WithTimeout(ctx, timeout)
	defer queryCancel()
	return action(queryCtx, connection)
}

func (s *ExcelMatchJobService) processOracleExportJob(ctx context.Context, export *model.ExcelMatchJob) (processErr error) {
	taskID, _ := asynq.GetTaskID(ctx)
	claimed, err := s.jobDAO.MarkRunning(ctx, export.ID, oracleExcelExportClaimBefore(taskID, export.ID, time.Now()))
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("%w: Oracle 导出任务已完成、过期或正在处理", asynq.SkipRetry)
	}
	defer func() {
		if processErr != nil {
			stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = s.jobDAO.MarkFailed(stateCtx, export.ID, "Oracle 导出失败，请查看任务日志或联系管理员", time.Now().Add(excelMatchRetention))
			s.logJob(stateCtx, export.ID, "error", "Oracle Excel 导出失败，请检查表字段、筛选值或服务端日志", nil)
		}
	}()
	var config OracleExcelExportConfig
	if err := json.Unmarshal([]byte(export.ConfigJSON), &config); err != nil {
		return err
	}
	export.WorkDir = excelMatchJobDir(export.ID)
	if err := os.MkdirAll(export.WorkDir, 0700); err != nil {
		return err
	}
	if err := s.jobDAO.UpdatePaths(ctx, export.ID, export.WorkDir, "", filepath.Join(export.WorkDir, excelMatchResultFileName)); err != nil {
		return err
	}
	s.logJob(ctx, export.ID, "info", "开始流式导出 Oracle 表数据", nil)
	var total int
	err = withOracleExcelConnection(ctx, 2*time.Hour, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		return withExcelizeTempDir(filepath.Join(export.WorkDir, excelizeTempDirName), func() error {
			var err error
			export.ResultFilePath, total, err = executeOracleExcelExport(queryCtx, connection, config, export.WorkDir, oracleExcelRowsPerSheet, func(count int) error {
				return s.jobDAO.UpdateProgress(queryCtx, export.ID, ExcelMatchJobStats{ProcessedRows: count})
			})
			return err
		})
	})
	if err != nil {
		return err
	}
	if err := s.jobDAO.UpdateOracleExportResultPath(ctx, export.ID, export.WorkDir, export.ResultFilePath); err != nil {
		return err
	}
	if storage.OSSStorageEnabled() {
		objectKey := strings.TrimSpace(export.ResultObjectKey)
		if objectKey == "" || path.Ext(objectKey) != filepath.Ext(export.ResultFilePath) {
			objectKey = path.Join(path.Dir(excelMatchResultObjectKey(export.ID, time.Now())), filepath.Base(export.ResultFilePath))
		}
		if !validExcelMatchResultObjectKey(objectKey, export.ID, storage.BuildObjectKey("excel-match-results")) {
			return errors.New("Oracle 导出结果的 OSS 文件 Key 无效")
		}
		if err := s.jobDAO.UpdateResultStorage(ctx, export.ID, objectKey, ""); err != nil {
			return err
		}
		if _, err := s.uploadExcelResultToOSS(ctx, export, objectKey, excelMatchOSSUploadTimeout(fileSizeOrZero(export.ResultFilePath))); err != nil {
			return err
		}
		_ = os.Remove(export.ResultFilePath)
	}
	if err := s.jobDAO.MarkSuccess(ctx, export.ID, ExcelMatchJobStats{TotalRows: total, ProcessedRows: total}, time.Now().Add(excelMatchRetention)); err != nil {
		return err
	}
	s.logJob(ctx, export.ID, "info", fmt.Sprintf("Oracle Excel 导出完成，共 %d 条数据，结果保留 24 小时", total), nil)
	return nil
}

func oracleExcelExportTaskID(jobID uint) string {
	return fmt.Sprintf("oracle-export:%d", jobID)
}

func oracleExcelExportClaimBefore(taskID string, jobID uint, now time.Time) time.Time {
	// Asynq serializes this deterministic task ID. A re-delivery after either
	// shutdown or lease recovery must reclaim running immediately, rather than
	// being archived while waiting for the usual stale-running threshold.
	if taskID == oracleExcelExportTaskID(jobID) {
		return now
	}
	return now.Add(-excelMatchRunningStaleAfter)
}

func excelJobResultFilename(export *model.ExcelMatchJob) string {
	if operation := excelJobOperationFromConfigJSON(export.ConfigJSON); operation == excelOperationOracleExport || operation == excelOperationWorkflow {
		extension := filepath.Ext(export.ResultFilePath)
		if strings.HasSuffix(export.ResultObjectKey, ".zip") {
			extension = ".zip"
		}
		if extension != ".zip" {
			extension = ".xlsx"
		}
		return fmt.Sprintf("oracle_export_%d%s", export.ID, extension)
	}
	return fmt.Sprintf("excel_match_job_%d.xlsx", export.ID)
}
