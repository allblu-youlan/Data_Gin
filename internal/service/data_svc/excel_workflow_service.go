package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"github.com/hibiken/asynq"
)

func (s *ExcelMatchJobService) CreateWorkflowJob(ctx context.Context, input ExcelWorkflowConfig, uploadID string) (*model.ExcelMatchJob, error) {
	if global.QueueJobClient == nil {
		return nil, errors.New("异步任务客户端未初始化")
	}
	config, err := s.resolveWorkflow(ctx, input)
	if err != nil {
		return nil, err
	}
	var source excelJobSource
	for _, step := range config.Steps {
		if step.Source == "upload" {
			var sourcePath string
			var meta excelUploadMeta
			meta, sourcePath, err = s.requireCompletedUpload(uploadID)
			source = excelJobSource{fileName: meta.FileName, save: func(dst string) error { return copyFile(sourcePath, dst) }}
			if err != nil {
				return nil, err
			}
			break
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	name := source.fileName
	if name == "" {
		name = "组合操作"
	}
	expires := time.Now().Add(excelMatchRetention)
	record := &model.ExcelMatchJob{SourceFileName: name, ConfigJSON: string(encoded), Status: excelMatchStatusPending, TotalRows: len(config.Steps), ExpiresAt: &model.TimeNormal{Time: expires}}
	if _, err = s.jobDAO.Create(ctx, record); err != nil {
		return nil, err
	}
	workDir := excelMatchJobDir(record.ID)
	if err = os.MkdirAll(workDir, 0700); err == nil && source.save != nil {
		err = source.save(filepath.Join(workDir, excelMatchSourceFileName))
	}
	if err == nil {
		err = s.jobDAO.UpdatePaths(ctx, record.ID, workDir, filepath.Join(workDir, excelMatchSourceFileName), "")
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
	s.logJob(ctx, record.ID, "info", "组合操作已创建，按顺序执行，任一步失败即停止", nil)
	return s.jobDAO.FindByID(ctx, record.ID)
}
