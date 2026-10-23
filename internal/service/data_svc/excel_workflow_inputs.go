package data_svc

import (
	"fmt"
	"strings"
)

func excelWorkflowNeedsExcel(operation string) bool {
	return operation == excelOperationExportMatch || operation == excelOperationImportUpdate || operation == ExcelOperationOracleImport
}

func normalizeExcelWorkflowStepIDs(steps []ExcelWorkflowStep) error {
	used := make(map[string]bool, len(steps))
	for index := range steps {
		id := strings.TrimSpace(steps[index].ID)
		steps[index].ID = id
		if id == "" {
			continue
		}
		if len(id) > 80 || used[id] {
			return fmt.Errorf("第 %d 步标识无效或重复", index+1)
		}
		used[id] = true
	}
	for index := range steps {
		if steps[index].ID != "" {
			continue
		}
		id := fmt.Sprintf("step_%d", index+1)
		for used[id] {
			id += "_"
		}
		steps[index].ID, used[id] = id, true
	}
	return nil
}

// Upload IDs are request data, while filenames are generated for the immutable
// job snapshot. Reusing a file in another step never changes either binding.
func prepareExcelWorkflowUploads(config ExcelWorkflowConfig, fallback []string, stepUploads map[string][]string, resolve func(string) (excelJobSource, error)) (ExcelWorkflowConfig, []excelJobSource, error) {
	config.Steps = append([]ExcelWorkflowStep(nil), config.Steps...)
	config.SourceFiles = nil
	eligible := make(map[string]bool)
	for index := range config.Steps {
		config.Steps[index].SourceFiles = nil
		step := config.Steps[index]
		if step.Source == "upload" && excelWorkflowNeedsExcel(step.Operation) {
			eligible[step.ID] = true
		}
	}
	for id := range stepUploads {
		if !eligible[id] {
			return config, nil, fmt.Errorf("Excel 文件关联的步骤 %q 不存在或无需上传文件", id)
		}
	}
	var sources []excelJobSource
	names := make(map[string]string)
	for index := range config.Steps {
		step := &config.Steps[index]
		if step.Source != "upload" || !excelWorkflowNeedsExcel(step.Operation) {
			continue
		}
		ids := fallback
		if stepUploads != nil {
			ids = stepUploads[step.ID]
		}
		if len(ids) == 0 {
			return config, nil, fmt.Errorf("请为第 %d 步「%s」选择对应的 Excel 文件", index+1, step.Name)
		}
		seen := make(map[string]bool, len(ids))
		for fileIndex, id := range ids {
			if id == "" || seen[id] {
				return config, nil, fmt.Errorf("第 %d 步的 Excel 文件标识为空或重复", index+1)
			}
			seen[id] = true
			name, exists := names[id]
			if !exists {
				source, err := resolve(id)
				if err != nil {
					return config, nil, fmt.Errorf("第 %d 步的第 %d 个文件无效: %w", index+1, fileIndex+1, err)
				}
				sources = append(sources, source)
				name = fmt.Sprintf("source_%03d.xlsx", len(sources))
				names[id] = name
				config.SourceFiles = append(config.SourceFiles, name)
			}
			step.SourceFiles = append(step.SourceFiles, name)
		}
	}
	return config, sources, nil
}
