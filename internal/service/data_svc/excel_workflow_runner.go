package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

func workflowTaskID(id uint) string { return fmt.Sprintf("excel-workflow:%d", id) }

type excelWorkflowExecutor func(context.Context, int, ExcelWorkflowStep, string) (string, error)

// One synchronous loop is the execution boundary. An error prevents every
// subsequent step, including when a procedure's configured flag does not match.
func runExcelWorkflow(ctx context.Context, config ExcelWorkflowConfig, uploadPath string, execute excelWorkflowExecutor, completed func(int) error) (string, error) {
	previous, result := "", ""
	for i, step := range config.Steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		source := uploadPath
		if step.Source == "previous" {
			source = previous
		}
		next, err := execute(ctx, i, step, source)
		if err != nil {
			return result, fmt.Errorf("第 %d 步「%s」失败: %w", i+1, step.Name, err)
		}
		if next != "" {
			result = next
		}
		if step.Operation == excelOperationExportMatch {
			previous = next
		}
		if err := completed(i + 1); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *ExcelMatchJobService) processExcelWorkflowJob(ctx context.Context, record *model.ExcelMatchJob) (processErr error) {
	claimed, err := s.jobDAO.ClaimWorkflow(ctx, record.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("%w: 组合操作已领取或已结束，不重复执行写入", asynq.SkipRetry)
	}
	defer func() {
		if processErr != nil {
			zap.L().Error("Excel workflow failed", zap.Uint("job_id", record.ID), zap.Error(processErr))
			stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = s.jobDAO.MarkFailed(stateCtx, record.ID, "组合操作失败，后续步骤已停止，请检查步骤日志", time.Now().Add(excelMatchRetention))
		}
	}()
	var config ExcelWorkflowConfig
	if err := json.Unmarshal([]byte(record.ConfigJSON), &config); err != nil {
		return err
	}
	result, err := runExcelWorkflow(ctx, config, record.SourceFilePath, func(ctx context.Context, index int, step ExcelWorkflowStep, source string) (string, error) {
		s.logJob(ctx, record.ID, "info", fmt.Sprintf("第 %d/%d 步：%s，开始执行", index+1, len(config.Steps), step.Name), nil)
		output, err := s.executeExcelWorkflowStep(ctx, record, index, step, source)
		if err != nil {
			s.logJob(context.WithoutCancel(ctx), record.ID, "error", fmt.Sprintf("第 %d 步：%s 失败，后续步骤已停止；请检查方法配置、源文件及 Oracle 返回标志", index+1, step.Name), nil)
			return "", err
		}
		s.logJob(ctx, record.ID, "info", fmt.Sprintf("第 %d 步：%s，执行成功", index+1, step.Name), nil)
		return output, nil
	}, func(completed int) error {
		return s.jobDAO.UpdateProgress(ctx, record.ID, ExcelMatchJobStats{TotalRows: len(config.Steps), ProcessedRows: completed})
	})
	if err != nil {
		return err
	}
	if result != "" {
		record.ResultFilePath = result
		if err := s.jobDAO.UpdateOracleExportResultPath(ctx, record.ID, record.WorkDir, result); err != nil {
			return err
		}
		if storage.OSSStorageEnabled() {
			key := filepath.ToSlash(filepath.Join(filepath.Dir(excelMatchResultObjectKey(record.ID, time.Now())), filepath.Base(result)))
			if _, err := s.uploadExcelResultToOSS(ctx, record, key, excelMatchOSSUploadTimeout(fileSizeOrZero(result))); err != nil {
				return err
			}
		}
	}
	if err := s.jobDAO.MarkSuccess(ctx, record.ID, ExcelMatchJobStats{TotalRows: len(config.Steps), ProcessedRows: len(config.Steps)}, time.Now().Add(excelMatchRetention)); err != nil {
		return err
	}
	s.logJob(ctx, record.ID, "info", "组合操作全部完成，结果保留 24 小时", nil)
	return nil
}

func (s *ExcelMatchJobService) executeExcelWorkflowStep(ctx context.Context, record *model.ExcelMatchJob, index int, step ExcelWorkflowStep, source string) (string, error) {
	switch step.Operation {
	case excelOperationExportMatch:
		var config ExcelMatchConfig
		if err := json.Unmarshal(step.Config, &config); err != nil {
			return "", err
		}
		if step.Source == "previous" {
			config.SheetName = defaultExcelResultSheet
		}
		output := filepath.Join(record.WorkDir, fmt.Sprintf("match_%d.xlsx", index+1))
		err := withExcelizeTempDir(filepath.Join(record.WorkDir, excelizeTempDirName), func() error {
			_, err := processExcelMatchFile(ctx, source, output, config, databaseExcelMatchLookup{dao: s.jobDAO})
			return err
		})
		return output, err
	case ExcelOperationOracleImport:
		config, err := excelWorkflowImportConfig(step)
		if err != nil {
			return "", err
		}
		count, err := ExecuteOracleExcelImport(ctx, source, config)
		if err == nil {
			s.logJob(ctx, record.ID, "info", fmt.Sprintf("Oracle 已导入 %d 行", count), nil)
		}
		return "", err
	case ExcelOperationOracleProcedure:
		var config OracleProcedureConfig
		if err := json.Unmarshal(step.Config, &config); err != nil {
			return "", err
		}
		_, err := ExecuteOracleProcedure(ctx, config)
		return "", err
	case excelOperationOracleExport:
		var config OracleExcelExportConfig
		if err := json.Unmarshal(step.Config, &config); err != nil {
			return "", err
		}
		dir := filepath.Join(record.WorkDir, fmt.Sprintf("export_%d", index+1))
		var output string
		err := withOracleOperationConnection(ctx, 2*time.Hour, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
			return withExcelizeTempDir(filepath.Join(record.WorkDir, excelizeTempDirName), func() error {
				var total int
				var renderErr error
				output, total, renderErr = executeOracleExcelExport(queryCtx, connection, config, dir, oracleExcelRowsPerFile, nil)
				if renderErr == nil {
					s.logJob(ctx, record.ID, "info", fmt.Sprintf("Oracle 已导出 %d 行", total), nil)
				}
				return renderErr
			})
		})
		return output, err
	default:
		return "", fmt.Errorf("未知组合操作类型: %s", step.Operation)
	}
}

// Matching always writes a new workbook with a first-row header in Result_1.
// The original upload's sheet/header settings do not apply to that artifact.
func excelWorkflowImportConfig(step ExcelWorkflowStep) (OracleExcelImportConfig, error) {
	var config OracleExcelImportConfig
	if err := json.Unmarshal(step.Config, &config); err != nil {
		return config, err
	}
	if step.Source == "previous" {
		config.SheetName = defaultExcelResultSheet
		config.HeaderRow = 1
	}
	return config, nil
}
