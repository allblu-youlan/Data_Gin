package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gin-biz-web-api/model"
)

const excelOperationWorkflow = "excel_workflow"

type ExcelWorkflowStep struct {
	Operation string          `json:"operation"`
	SchemeID  uint            `json:"schemeId"`
	Source    string          `json:"source,omitempty"`
	Name      string          `json:"name,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}

type ExcelWorkflowConfig struct {
	Operation   string              `json:"operation"`
	Steps       []ExcelWorkflowStep `json:"steps"`
	SourceFiles []string            `json:"sourceFiles,omitempty"`
}

type ExcelWorkflowScheme struct {
	ID     uint                `json:"id"`
	Name   string              `json:"name"`
	Config ExcelWorkflowConfig `json:"config"`
}

func validateExcelWorkflow(input ExcelWorkflowConfig) (ExcelWorkflowConfig, error) {
	input.Steps = append([]ExcelWorkflowStep(nil), input.Steps...)
	input.SourceFiles = nil
	input.Operation = excelOperationWorkflow
	if len(input.Steps) == 0 || len(input.Steps) > 20 {
		return input, errors.New("组合操作需配置 1 至 20 个步骤")
	}
	hasPrevious := false
	for i := range input.Steps {
		step := &input.Steps[i]
		switch step.Operation {
		case excelOperationExportMatch, "oracle_import", "oracle_procedure", excelOperationOracleExport:
		default:
			return input, fmt.Errorf("第 %d 步操作类型无效", i+1)
		}
		if step.SchemeID == 0 {
			return input, fmt.Errorf("第 %d 步请选择已保存的方法", i+1)
		}
		if step.Operation == excelOperationExportMatch || step.Operation == "oracle_import" {
			if step.Source == "" {
				step.Source = "upload"
			}
			if step.Source != "upload" && step.Source != "previous" {
				return input, errors.New("Excel 来源无效")
			}
			if step.Source == "previous" && !hasPrevious {
				return input, fmt.Errorf("第 %d 步之前没有可用的匹配结果", i+1)
			}
		} else {
			step.Source = ""
		}
		if step.Operation == excelOperationExportMatch {
			hasPrevious = true
		}
	}
	return input, nil
}

func (s *ExcelMatchJobService) ListWorkflows(ctx context.Context) ([]ExcelWorkflowScheme, error) {
	rows, err := s.jobDAO.ListSchemes(ctx, excelOperationWorkflow)
	if err != nil {
		return nil, err
	}
	result := make([]ExcelWorkflowScheme, 0, len(rows))
	for _, row := range rows {
		var config ExcelWorkflowConfig
		if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
			return nil, err
		}
		result = append(result, ExcelWorkflowScheme{ID: row.ID, Name: row.Name, Config: config})
	}
	return result, nil
}

func (s *ExcelMatchJobService) SaveWorkflow(ctx context.Context, name string, input ExcelWorkflowConfig) (*ExcelWorkflowScheme, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, errors.New("操作台名称应为 1 至 100 个字符")
	}
	config, err := validateExcelWorkflow(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.resolveWorkflow(ctx, config); err != nil {
		return nil, err
	}
	// Saved workflows reference methods; each run stores its own immutable snapshot.
	for i := range config.Steps {
		config.Steps[i].Config = nil
		config.Steps[i].Name = ""
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	row := &model.ExcelMatchScheme{Name: name, Operation: excelOperationWorkflow, ConfigJSON: string(encoded)}
	if err := s.jobDAO.SaveScheme(ctx, row); err != nil {
		return nil, err
	}
	return &ExcelWorkflowScheme{ID: row.ID, Name: row.Name, Config: config}, nil
}

func (s *ExcelMatchJobService) resolveWorkflow(ctx context.Context, input ExcelWorkflowConfig) (ExcelWorkflowConfig, error) {
	config, err := validateExcelWorkflow(input)
	if err != nil {
		return config, err
	}
	for i := range config.Steps {
		step := &config.Steps[i]
		rows, err := s.jobDAO.ListSchemes(ctx, step.Operation)
		if err != nil {
			return config, err
		}
		var found *model.ExcelMatchScheme
		for j := range rows {
			if rows[j].ID == step.SchemeID {
				found = &rows[j]
				break
			}
		}
		if found == nil {
			return config, fmt.Errorf("第 %d 步方法不存在或已删除，请重新选择", i+1)
		}
		step.Name, step.Config = found.Name, json.RawMessage(found.ConfigJSON)
		switch step.Operation {
		case excelOperationExportMatch:
			var c ExcelMatchConfig
			if err := json.Unmarshal(step.Config, &c); err != nil {
				return config, err
			}
			c.Operation = excelOperationExportMatch
			c, err = normalizeExcelMatchExecutionConfig(c)
			if err == nil {
				err = validateExcelExportSteps(ctx, c, s.jobDAO)
			}
			if err != nil {
				return config, err
			}
			step.Config, err = json.Marshal(c)
		case "oracle_import":
			var c OracleExcelImportConfig
			if err := json.Unmarshal(step.Config, &c); err != nil {
				return config, err
			}
			c, err = NormalizeOracleExcelImportConfig(ctx, c)
			if err == nil {
				step.Config, err = json.Marshal(c)
			}
		case "oracle_procedure":
			var c OracleProcedureConfig
			if err := json.Unmarshal(step.Config, &c); err != nil {
				return config, err
			}
			c, err = NormalizeOracleProcedureConfig(ctx, c)
			if err == nil {
				step.Config, err = json.Marshal(c)
			}
		case excelOperationOracleExport:
			var c OracleExcelExportConfig
			if err := json.Unmarshal(step.Config, &c); err != nil {
				return config, err
			}
			c, err = normalizeOracleExcelExportConfig(ctx, c)
			if err == nil {
				step.Config, err = json.Marshal(c)
			}
		}
		if err != nil {
			return config, fmt.Errorf("第 %d 步方法无效: %w", i+1, err)
		}
	}
	return config, nil
}

func (s *ExcelMatchJobService) DeleteWorkflow(ctx context.Context, id uint) error {
	if id == 0 {
		return errors.New("组合操作 ID 不能为空")
	}
	return s.jobDAO.DeleteWorkflow(ctx, id)
}
