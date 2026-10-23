package data_svc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

type oracleExcelSheetSnapshot struct {
	Sheet  OracleExcelExportSheet
	Schema []reportoracle.ResultColumn
	Path   string
	Rows   int
}

// Count and persist a single cursor before choosing balanced sheet sizes.
// Only one row is held in memory, and each table is queried exactly once.
func spoolOracleExcelSheet(ctx context.Context, rows oracleExcelRows, sheet OracleExcelExportSheet, schema []reportoracle.ResultColumn, dir string, progress func(int) error) (oracleExcelSheetSnapshot, error) {
	snapshot := oracleExcelSheetSnapshot{Sheet: sheet, Schema: schema, Path: filepath.Join(dir, "rows.jsonl")}
	if len(sheet.Columns) == 0 {
		return snapshot, fmt.Errorf("Oracle Sheet 导出字段不能为空")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return snapshot, err
	}
	file, err := os.OpenFile(snapshot.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return snapshot, err
	}
	defer file.Close()
	writer := bufio.NewWriterSize(file, 64*1024)
	encoder := json.NewEncoder(writer)
	values := make([]interface{}, len(sheet.Columns))
	targets := make([]interface{}, len(values))
	for index := range values {
		targets[index] = &values[index]
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		if err := rows.Scan(targets...); err != nil {
			return snapshot, fmt.Errorf("读取 Sheet %s 失败: %w", sheet.SheetName, err)
		}
		for index, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[index] = string(bytes)
			}
		}
		if err := encoder.Encode(values); err != nil {
			return snapshot, fmt.Errorf("缓存 Sheet %s 失败: %w", sheet.SheetName, err)
		}
		snapshot.Rows++
		if snapshot.Rows%1000 == 0 && progress != nil {
			if err := progress(snapshot.Rows); err != nil {
				return snapshot, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("读取 Sheet %s 失败: %w", sheet.SheetName, err)
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if err := writer.Flush(); err != nil {
		return snapshot, err
	}
	return snapshot, file.Close()
}

func oracleExcelBalancedSheetRows(total, rowsPerSheet int) []int {
	count := max(1, (total+rowsPerSheet-1)/rowsPerSheet)
	sizes := make([]int, count)
	for index := range sizes {
		sizes[index] = total / count
		if index < total%count {
			sizes[index]++
		}
	}
	return sizes
}

func oracleExcelSplitSheetName(base string, part int, reserved map[string]bool) string {
	suffix := fmt.Sprintf("_%d", part+1)
	name := truncateMallWeatherExportRunes(base, 31-len(suffix)) + suffix
	for collision := 2; reserved[strings.ToLower(name)]; collision++ {
		suffix = fmt.Sprintf("_%d_%d", part+1, collision)
		name = truncateMallWeatherExportRunes(base, 31-len(suffix)) + suffix
	}
	reserved[strings.ToLower(name)] = true
	return name
}

func renderOracleExcelSnapshots(ctx context.Context, snapshots []oracleExcelSheetSnapshot, workDir string, rowsPerSheet int, progress func(int) error) (string, int, error) {
	if len(snapshots) == 0 || len(snapshots) > oracleExcelMaxSheets || rowsPerSheet < 1 || rowsPerSheet > oracleExcelRowsPerSheet {
		return "", 0, fmt.Errorf("Oracle 多 Sheet 导出配置无效")
	}
	total := 0
	reserved := make(map[string]bool, len(snapshots))
	for _, snapshot := range snapshots {
		total += snapshot.Rows
		// Reserve original names first, so a generated suffix cannot overwrite
		// another table's configured sheet (for example A and A_1).
		reserved[strings.ToLower(snapshot.Sheet.SheetName)] = true
	}
	file := excelize.NewFile()
	defer file.Close()
	styles, err := newReportExportStyles(file, "")
	if err != nil {
		return "", total, err
	}
	sheetCount := 0
	for _, snapshot := range snapshots {
		sizes := oracleExcelBalancedSheetRows(snapshot.Rows, rowsPerSheet)
		err := func() error {
			source, err := os.Open(snapshot.Path)
			if err != nil {
				return err
			}
			defer source.Close()
			decoder := json.NewDecoder(bufio.NewReaderSize(source, 64*1024))
			decoder.UseNumber()
			for part, count := range sizes {
				if err := ctx.Err(); err != nil {
					return err
				}
				name := snapshot.Sheet.SheetName
				if len(sizes) > 1 {
					name = oracleExcelSplitSheetName(name, part, reserved)
				}
				if sheetCount == 0 {
					err = file.SetSheetName("Sheet1", name)
				} else {
					_, err = file.NewSheet(name)
				}
				if err != nil {
					return err
				}
				stream, err := file.NewStreamWriter(name)
				if err != nil {
					return err
				}
				if err := stream.SetColWidth(1, len(snapshot.Sheet.Columns), reportExcelDefaultWidth); err != nil {
					return err
				}
				header := make([]interface{}, len(snapshot.Sheet.Columns))
				for index, column := range snapshot.Sheet.Columns {
					header[index] = excelize.Cell{StyleID: styles.header, Value: column.Title}
				}
				if err := stream.SetRow("A1", header); err != nil {
					return err
				}
				if err := replayOracleExcelSheet(ctx, stream, styles, snapshot, decoder, count); err != nil {
					return err
				}
				if err := stream.Flush(); err != nil {
					return err
				}
				sheetCount++
			}
			return nil
		}()
		if err != nil {
			return "", total, err
		}
	}
	output := filepath.Join(workDir, excelMatchResultFileName)
	if err := file.SaveAs(output); err != nil {
		return "", total, err
	}
	if progress != nil {
		if err := progress(total); err != nil {
			return "", total, err
		}
	}
	return output, total, nil
}

func replayOracleExcelSheet(ctx context.Context, stream *excelize.StreamWriter, styles reportExportStyles, snapshot oracleExcelSheetSnapshot, decoder *json.Decoder, count int) error {
	types := make(map[string]string, len(snapshot.Schema))
	for _, column := range snapshot.Schema {
		types[column.Name] = oracleExcelValueType(column.DataType)
	}
	for rowNumber := 2; rowNumber < count+2; rowNumber++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var values []interface{}
		if err := decoder.Decode(&values); err != nil {
			return fmt.Errorf("读取 Sheet %s 缓存失败: %w", snapshot.Sheet.SheetName, err)
		}
		if len(values) != len(snapshot.Sheet.Columns) {
			return fmt.Errorf("Sheet %s 缓存字段数量不匹配", snapshot.Sheet.SheetName)
		}
		cells := make([]interface{}, len(values))
		for index, value := range values {
			column := snapshot.Sheet.Columns[index]
			cell, truncated, err := reportExportCell(value, frozenResultColumn{ValueType: types[column.Field]}, styles)
			if err != nil {
				return err
			}
			if truncated {
				return fmt.Errorf("Sheet %s 字段 %s 超过 Excel 单元格字符限制", snapshot.Sheet.SheetName, column.Field)
			}
			cells[index] = cell
		}
		if err := stream.SetRow(fmt.Sprintf("A%d", rowNumber), cells); err != nil {
			return err
		}
	}
	return nil
}
