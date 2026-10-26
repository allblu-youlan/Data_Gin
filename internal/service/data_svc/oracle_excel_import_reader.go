package data_svc

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Counting only needs mapped cells' presence, not display formatting or Oracle binds.
func countOracleExcelImportLocated(ctx context.Context, path string, config OracleExcelImportConfig, visit func(oracleExcelImportLocation) error) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		return 0, fmt.Errorf("打开 Excel 文件 %s 失败: %w", filepath.Base(path), err)
	}
	defer file.Close()
	sheets := []string{config.SheetName}
	if config.AllSheets {
		sheets = file.GetSheetList()
	}
	count := 0
	for _, sheet := range sheets {
		read, err := countOracleExcelImportSheet(ctx, file, sheet, config, func(row int) error {
			if visit == nil {
				return nil
			}
			return visit(oracleExcelImportLocation{file: filepath.Base(path), sheet: sheet, row: row})
		})
		if err != nil {
			return 0, fmt.Errorf("文件 %s，工作表 %s: %w", filepath.Base(path), sheet, err)
		}
		count += read
	}
	return count, nil
}

func countOracleExcelImportSheet(ctx context.Context, file *excelize.File, sheet string, config OracleExcelImportConfig, visit func(int) error) (int, error) {
	rows, err := file.Rows(sheet)
	if err != nil {
		return 0, fmt.Errorf("read import sheet: %w", err)
	}
	defer rows.Close()
	indexes := make([]int, len(config.Mappings))
	headerFound, count, rowNumber := false, 0, 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		rowNumber++
		if rowNumber < config.HeaderRow {
			continue
		}
		// Headers retain the same displayed values as the import reader.
		cells, err := rows.Columns(excelize.Options{RawCellValue: rowNumber != config.HeaderRow})
		if err != nil {
			return 0, err
		}
		if rowNumber == config.HeaderRow {
			byHeader := make(map[string]int, len(cells))
			for index, header := range normalizeHeaders(cells) {
				if header == "" {
					continue
				}
				if _, duplicate := byHeader[header]; duplicate {
					return 0, fmt.Errorf("Excel 表头重复: %s", header)
				}
				byHeader[header] = index
			}
			for index, mapping := range config.Mappings {
				columnIndex, exists := byHeader[mapping.ExcelColumn]
				if !exists {
					return 0, fmt.Errorf("Excel 缺少表头: %s", mapping.ExcelColumn)
				}
				indexes[index] = columnIndex
			}
			headerFound = true
			continue
		}
		empty := true
		for _, columnIndex := range indexes {
			if columnIndex < len(cells) && strings.TrimSpace(cells[columnIndex]) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		if visit != nil {
			if err := visit(rowNumber); err != nil {
				return 0, fmt.Errorf("扫描 Excel 第 %d 行失败: %w", rowNumber, err)
			}
		}
		count++
	}
	if err := rows.Error(); err != nil {
		return 0, err
	}
	if !headerFound {
		return 0, errors.New("Excel 缺少配置的表头行")
	}
	return count, nil
}
