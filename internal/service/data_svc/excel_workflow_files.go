package data_svc

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"

	"gin-biz-web-api/model"
	"github.com/xuri/excelize/v2"
)

type excelWorkflowFilesExecutor func(context.Context, int, ExcelWorkflowStep, []string) ([]string, error)

func runExcelWorkflowFiles(ctx context.Context, config ExcelWorkflowConfig, uploads []string, execute excelWorkflowFilesExecutor, completed func(int) error) ([]string, error) {
	var previous, result []string
	for index, step := range config.Steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sources := uploads
		if step.Source == "previous" {
			sources = previous
		}
		next, err := execute(ctx, index, step, append([]string(nil), sources...))
		if err != nil {
			return result, fmt.Errorf("第 %d 步「%s」失败: %w", index+1, step.Name, err)
		}
		if len(next) > 0 {
			result = next
		}
		if step.Operation == excelOperationExportMatch {
			previous = next
		}
		if err := completed(index + 1); err != nil {
			return result, err
		}
	}
	return result, nil
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
