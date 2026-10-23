package data_svc

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"gin-biz-web-api/model"
	"github.com/xuri/excelize/v2"
)

type excelWorkflowFilesExecutor func(context.Context, int, ExcelWorkflowStep, []string) ([]string, error)

func runExcelWorkflowFiles(ctx context.Context, config ExcelWorkflowConfig, uploads []string, execute excelWorkflowFilesExecutor, completed func(int) error) ([]string, error) {
	var previous, result []string
	matchedByID := make(map[string][]string)
	for index, step := range config.Steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sources, err := excelWorkflowStepSources(step, uploads, previous, matchedByID)
		if err != nil {
			return result, fmt.Errorf("第 %d 步「%s」来源无效: %w", index+1, step.Name, err)
		}
		next, err := execute(ctx, index, step, sources)
		if err != nil {
			return result, fmt.Errorf("第 %d 步「%s」失败: %w", index+1, step.Name, err)
		}
		if len(next) > 0 {
			result = next
		}
		if step.Operation == excelOperationExportMatch {
			previous = next
			if step.ID != "" {
				matchedByID[step.ID] = append([]string(nil), next...)
			}
		}
		if err := completed(index + 1); err != nil {
			return result, err
		}
	}
	return result, nil
}

func excelWorkflowStepSources(step ExcelWorkflowStep, uploads, previous []string, matchedByID map[string][]string) ([]string, error) {
	if step.Source == "previous" {
		sources := previous
		if step.SourceStepID != "" {
			var exists bool
			sources, exists = matchedByID[step.SourceStepID]
			if !exists {
				return nil, fmt.Errorf("找不到此前匹配步骤 %s 的结果", step.SourceStepID)
			}
		}
		if len(sources) == 0 {
			return nil, errors.New("引用的匹配步骤没有可用的 Excel 结果")
		}
		return append([]string(nil), sources...), nil
	}
	if len(step.SourceFiles) == 0 {
		return append([]string(nil), uploads...), nil
	}
	byName := make(map[string]string, len(uploads))
	ambiguous := make(map[string]bool)
	for _, path := range uploads {
		name := filepath.Base(path)
		if existing, ok := byName[name]; ok && existing != path {
			ambiguous[name] = true
		}
		byName[name] = path
	}
	selected := make([]string, 0, len(step.SourceFiles))
	seen := make(map[string]bool, len(step.SourceFiles))
	for _, name := range step.SourceFiles {
		path, exists := byName[name]
		if !exists {
			return nil, fmt.Errorf("缺少本步骤指定的 Excel 文件: %s", name)
		}
		if ambiguous[name] {
			return nil, fmt.Errorf("本步骤的 Excel 文件名不唯一: %s", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("本步骤重复指定 Excel 文件: %s", name)
		}
		seen[name] = true
		selected = append(selected, path)
	}
	return selected, nil
}

type excelWorkflowWriteProgress func(index, total int, path, sheet string, stats ExcelMatchJobStats, complete bool)

// The existing updater owns the non-overwrite rules. A workflow changes only
// file/sheet selection and confirms the write already authorized at submission.
func processExcelWorkflowWriteFiles(ctx context.Context, paths []string, config ExcelMatchConfig, fromPrevious bool, updater ExcelImportUpdater, onProgress excelWorkflowWriteProgress) (ExcelMatchJobStats, error) {
	var total ExcelMatchJobStats
	if len(paths) == 0 {
		return total, errors.New("请选择本写入步骤要处理的 Excel 文件")
	}
	config.Operation, config.ConfirmWrite, config.DryRun = excelOperationImportUpdate, true, false
	normalized, err := normalizeExcelMatchExecutionConfig(config)
	if err != nil {
		return total, err
	}
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		sheets := []string{normalized.SheetName}
		if fromPrevious {
			file, err := excelize.OpenFile(path)
			if err != nil {
				return total, fmt.Errorf("打开写入文件 %s 失败: %w", filepath.Base(path), err)
			}
			sheets = nil
			for _, sheet := range file.GetSheetList() {
				if strings.HasPrefix(sheet, "Result_") {
					sheets = append(sheets, sheet)
				}
			}
			if err := file.Close(); err != nil {
				return total, fmt.Errorf("关闭写入文件 %s 失败: %w", filepath.Base(path), err)
			}
			if len(sheets) == 0 {
				return total, fmt.Errorf("文件 %s 缺少 Result_* 匹配结果工作表", filepath.Base(path))
			}
		}
		for _, sheet := range sheets {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			current := normalized
			current.SheetName = sheet
			if onProgress != nil {
				onProgress(index, len(paths), path, sheet, ExcelMatchJobStats{}, false)
			}
			stats, err := processExcelImportUpdateFileWithProgress(ctx, path, current, updater, func(stats ExcelMatchJobStats) {
				if onProgress != nil {
					onProgress(index, len(paths), path, sheet, stats, false)
				}
			})
			total.TotalRows += stats.TotalRows
			total.ProcessedRows += stats.ProcessedRows
			total.FilteredRows += stats.FilteredRows
			total.MatchedRows += stats.MatchedRows
			total.UnmatchedRows += stats.UnmatchedRows
			if err != nil {
				return total, fmt.Errorf("写入文件 %s，工作表 %s 失败: %w", filepath.Base(path), sheet, err)
			}
			if onProgress != nil {
				onProgress(index, len(paths), path, sheet, stats, true)
			}
		}
	}
	return total, nil
}

func excelWorkflowSourcePaths(record *model.ExcelMatchJob, config ExcelWorkflowConfig) ([]string, error) {
	if len(config.SourceFiles) == 0 {
		if record.SourceFilePath == "" {
			return nil, nil
		}
		return []string{record.SourceFilePath}, nil
	}
	paths := make([]string, 0, len(config.SourceFiles))
	for _, name := range config.SourceFiles {
		if filepath.Base(name) != name || filepath.Ext(name) != ".xlsx" {
			return nil, errors.New("任务源文件名无效")
		}
		paths = append(paths, filepath.Join(record.WorkDir, name))
	}
	return paths, nil
}

func excelWorkflowResultFile(workDir string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	if len(paths) == 1 {
		return paths[0], nil
	}
	result := filepath.Join(workDir, "matched_results.zip")
	if err := zipOracleExcelFiles(result, paths); err != nil {
		return "", err
	}
	return result, nil
}

// Reuse the same match pipeline state across file boundaries so one order item
// cannot be selected again simply because a large upload was split into files.
func processExcelMatchFiles(ctx context.Context, paths []string, outputDir string, config ExcelMatchConfig, lookup ExcelMatchLookup, onProgress func(int, int, ExcelMatchJobStats)) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("请选择至少一个 Excel 文件")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return nil, err
	}
	reservations := make(excelOrderItemReservations)
	hasOrderItems := false
	for _, step := range config.Steps {
		if step.isEnabled() && step.MatchMode == excelMatchModeOrderItemSKU {
			hasOrderItems = true
			break
		}
	}
	if hasOrderItems {
		// Hashes select which orders need reservations; actual reservation and SKU
		// identities still use full strings. A collision only retains extra orders.
		counts := make(map[uint64]uint8)
		hashOrder := func(order string) uint64 { h := fnv.New64a(); _, _ = h.Write([]byte(order)); return h.Sum64() }
		scan := func(selectOrder func(string) bool, merge bool) error {
			for _, path := range paths {
				file, err := excelize.OpenFile(path)
				if err != nil {
					return err
				}
				next, err := collectExcelOrderItemReservationsFiltered(ctx, file, config, 0, selectOrder)
				closeErr := file.Close()
				if err != nil {
					return fmt.Errorf("文件 %s: %w", filepath.Base(path), err)
				}
				if closeErr != nil {
					return closeErr
				}
				if !merge {
					continue
				}
				for step, byOrder := range next {
					if reservations[step] == nil {
						reservations[step] = make(map[string]map[string][]excelOrderItemReservation)
					}
					for order, bySpec := range byOrder {
						if reservations[step][order] == nil {
							reservations[step][order] = make(map[string][]excelOrderItemReservation)
						}
						for spec, items := range bySpec {
							reservations[step][order][spec] = append(reservations[step][order][spec], items...)
						}
					}
				}
			}
			return nil
		}
		if err := scan(func(order string) bool {
			key := hashOrder(order)
			if counts[key] < 2 {
				counts[key]++
			}
			return false
		}, false); err != nil {
			return nil, err
		}
		if err := scan(func(order string) bool { return counts[hashOrder(order)] > 1 }, true); err != nil {
			return nil, err
		}
	}

	state := newExcelMatchPipelineState(reservations)
	state.orderItems.reservationsComplete = hasOrderItems
	outputs := make([]string, 0, len(paths))
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		output := filepath.Join(outputDir, fmt.Sprintf("matched_%03d.xlsx", index+1))
		stats, err := processExcelMatchFileWithState(ctx, path, output, config, lookup, state, func(stats ExcelMatchJobStats) {
			if onProgress != nil {
				onProgress(index, len(paths), stats)
			}
		})
		if err != nil {
			return nil, fmt.Errorf("文件 %s 匹配失败: %w", filepath.Base(path), err)
		}
		outputs = append(outputs, output)
		if onProgress != nil {
			onProgress(index+1, len(paths), stats)
		}
	}
	return outputs, nil
}
