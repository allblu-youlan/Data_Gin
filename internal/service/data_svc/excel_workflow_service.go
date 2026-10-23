package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"github.com/hibiken/asynq"
)

func (s *ExcelMatchJobService) CreateWorkflowJob(ctx context.Context, input ExcelWorkflowConfig, uploadID string) (*model.ExcelMatchJob, error) {
	var ids []string
	if uploadID != "" {
		ids = []string{uploadID}
	}
	return s.CreateWorkflowJobFromUploads(ctx, input, ids)
}

func (s *ExcelMatchJobService) CreateWorkflowJobFromUploads(ctx context.Context, input ExcelWorkflowConfig, uploadIDs []string) (*model.ExcelMatchJob, error) {
	config, err := s.resolveWorkflow(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.createResolvedWorkflowJob(ctx, config, uploadIDs)
}

func (s *ExcelMatchJobService) CreateWorkflowJobFromStepUploads(ctx context.Context, input ExcelWorkflowConfig, uploads map[string][]string) (*model.ExcelMatchJob, error) {
	config, err := s.resolveWorkflow(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.createResolvedWorkflowJobWithStepUploads(ctx, config, nil, uploads)
}

// The standalone match page uses the same multi-file worker as the workbench.
// Its configuration has already been normalized and does not need a saved ID.
func (s *ExcelMatchJobService) CreateMatchJobFromUploads(ctx context.Context, uploadIDs []string, rawConfig string) (*model.ExcelMatchJob, error) {
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, err
	}
	normalized, err := normalizeExcelMatchExecutionConfig(config)
	if err != nil {
		return nil, err
	}
	if normalized.Operation != excelOperationExportMatch {
		return nil, errors.New("批量文件仅支持 Excel 匹配操作")
	}
	if err := validateExcelExportSteps(ctx, normalized, s.jobDAO); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	workflow := ExcelWorkflowConfig{Operation: excelOperationWorkflow, Steps: []ExcelWorkflowStep{{Operation: excelOperationExportMatch, Source: "upload", Name: "批量 Excel 匹配", Config: encoded}}}
	return s.createResolvedWorkflowJob(ctx, workflow, uploadIDs)
}

func (s *ExcelMatchJobService) createResolvedWorkflowJob(ctx context.Context, config ExcelWorkflowConfig, uploadIDs []string) (*model.ExcelMatchJob, error) {
	return s.createResolvedWorkflowJobWithStepUploads(ctx, config, uploadIDs, nil)
}

func (s *ExcelMatchJobService) createResolvedWorkflowJobWithStepUploads(ctx context.Context, config ExcelWorkflowConfig, uploadIDs []string, uploads map[string][]string) (*model.ExcelMatchJob, error) {
	if global.QueueJobClient == nil {
		return nil, errors.New("异步任务客户端未初始化")
	}
	config, sources, err := prepareExcelWorkflowUploads(config, uploadIDs, uploads, func(id string) (excelJobSource, error) {
		meta, path, err := s.requireCompletedUpload(id)
		if err != nil {
			return excelJobSource{}, err
		}
		return excelJobSource{fileName: meta.FileName, save: func(dst string) error { return copyFile(path, dst) }}, nil
	})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	name := "组合操作"
	if len(sources) == 1 {
		name = sources[0].fileName
	} else if len(sources) > 1 {
		name = fmt.Sprintf("批量 Excel（%d 个文件）", len(sources))
	}
	expires := time.Now().Add(excelMatchRetention)
	record := &model.ExcelMatchJob{SourceFileName: name, ConfigJSON: string(encoded), Status: excelMatchStatusPending, TotalRows: len(config.Steps), ExpiresAt: &model.TimeNormal{Time: expires}}
	if _, err = s.jobDAO.Create(ctx, record); err != nil {
		return nil, err
	}
	workDir := excelMatchJobDir(record.ID)
	if err = os.MkdirAll(workDir, 0700); err == nil {
		for index, source := range sources {
			if err = source.save(filepath.Join(workDir, config.SourceFiles[index])); err != nil {
				break
			}
		}
	}
	sourcePath := ""
	if len(config.SourceFiles) > 0 {
		sourcePath = filepath.Join(workDir, config.SourceFiles[0])
	}
	if err == nil {
		err = s.jobDAO.UpdatePaths(ctx, record.ID, workDir, sourcePath, "")
	}
	if err == nil {
		var task *asynq.Task
		task, err = job.NewExcelMatchExportTask(record.ID)
		if err == nil {
			_, err = global.QueueJobClient.Enqueue(task, asynq.TaskID(workflowTaskID(record.ID)), asynq.MaxRetry(0), asynq.Timeout(2*time.Hour))
		}
	}
	if err != nil {
		_ = s.jobDAO.MarkFailed(ctx, record.ID, "组合操作任务创建失败", expires)
		return nil, err
	}
	s.logJob(ctx, record.ID, "info", fmt.Sprintf("组合操作已创建，共 %d 个 Excel 文件；文件和步骤均按顺序执行，任一步失败即停止", len(sources)), nil)
	stepDescriptions := make(map[string]string, len(config.Steps))
	fileDescriptions := make(map[string]string, len(sources))
	for index, step := range config.Steps {
		stepDescriptions[step.ID] = fmt.Sprintf("第 %d 步「%s」", index+1, step.Name)
	}
	for index, source := range sources {
		fileDescriptions[config.SourceFiles[index]] = source.fileName
	}
	for index, step := range config.Steps {
		if !excelWorkflowNeedsExcel(step.Operation) {
			continue
		}
		message := fmt.Sprintf("第 %d 步「%s」使用其单独选择的 %d 个 Excel 文件", index+1, step.Name, len(step.SourceFiles))
		if step.Source == "previous" {
			message = fmt.Sprintf("第 %d 步「%s」使用 %s 的全部匹配结果文件", index+1, step.Name, stepDescriptions[step.SourceStepID])
		} else {
			names := make([]string, 0, len(step.SourceFiles))
			for _, name := range step.SourceFiles {
				names = append(names, fileDescriptions[name])
			}
			message += "：" + strings.Join(names, "、")
		}
		s.logJob(ctx, record.ID, "info", message, nil)
	}
	return s.jobDAO.FindByID(ctx, record.ID)
}
