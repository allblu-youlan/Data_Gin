package data_svc

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

const oracleExcelRowsPerFile = 1_000_000

type oracleExcelRows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}

// A single Oracle cursor is streamed through all workbooks, without OFFSET
// pagination or holding the table in memory. The header does not count as data.
func renderOracleExcelExport(ctx context.Context, rows oracleExcelRows, config OracleExcelExportConfig, schema []reportoracle.ResultColumn, workDir string, rowsPerFile int, progress func(int) error) (string, int, error) {
	if rowsPerFile < 1 || rowsPerFile > oracleExcelRowsPerFile || len(config.Columns) == 0 {
		return "", 0, fmt.Errorf("Oracle 导出分片配置无效")
	}
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return "", 0, err
	}
	types := make(map[string]string, len(schema))
	for _, column := range schema {
		types[column.Name] = oracleExcelValueType(column.DataType)
	}
	var workbook *excelize.File
	var stream *excelize.StreamWriter
	var styles reportExportStyles
	defer func() {
		if workbook != nil {
			_ = workbook.Close()
		}
	}()
	paths := make([]string, 0, 1)
	partRows, total := 0, 0
	start := func() error {
		workbook = excelize.NewFile()
		var err error
		styles, err = newReportExportStyles(workbook, "")
		if err != nil {
			return err
		}
		if err := workbook.SetSheetName("Sheet1", "数据"); err != nil {
			return err
		}
		stream, err = workbook.NewStreamWriter("数据")
		if err != nil {
			return err
		}
		if err := stream.SetColWidth(1, len(config.Columns), reportExcelDefaultWidth); err != nil {
			return err
		}
		header := make([]interface{}, len(config.Columns))
		for index, column := range config.Columns {
			header[index] = excelize.Cell{StyleID: styles.header, Value: column.Title}
		}
		partRows = 0
		return stream.SetRow("A1", header)
	}
	finish := func() error {
		if err := stream.Flush(); err != nil {
			return err
		}
		path := filepath.Join(workDir, fmt.Sprintf("%s_%03d.xlsx", config.TableName, len(paths)+1))
		if err := workbook.SaveAs(path); err != nil {
			return err
		}
		if err := workbook.Close(); err != nil {
			return err
		}
		workbook = nil
		paths = append(paths, path)
		return nil
	}
	if err := start(); err != nil {
		return "", 0, err
	}
	values := make([]interface{}, len(config.Columns))
	targets := make([]interface{}, len(values))
	for index := range values {
		targets[index] = &values[index]
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		if partRows == rowsPerFile {
			if err := finish(); err != nil {
				return "", total, err
			}
			if err := start(); err != nil {
				return "", total, err
			}
		}
		if err := rows.Scan(targets...); err != nil {
			return "", total, fmt.Errorf("读取 Oracle 导出数据失败: %w", err)
		}
		cells := make([]interface{}, len(values))
		for index, value := range values {
			cell, truncated, err := reportExportCell(value, frozenResultColumn{ValueType: types[config.Columns[index].Field]}, styles)
			if err != nil {
				return "", total, err
			}
			if truncated {
				return "", total, fmt.Errorf("第 %d 行字段 %s 超过 Excel 单元格 32767 字符限制", total+1, config.Columns[index].Field)
			}
			cells[index] = cell
		}
		if err := stream.SetRow(fmt.Sprintf("A%d", partRows+2), cells); err != nil {
			return "", total, err
		}
		partRows++
		total++
		if total%1000 == 0 && progress != nil {
			if err := progress(total); err != nil {
				return "", total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", total, fmt.Errorf("读取 Oracle 导出结果失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", total, err
	}
	if err := finish(); err != nil {
		return "", total, err
	}
	if len(paths) == 1 {
		output := filepath.Join(workDir, excelMatchResultFileName)
		return output, total, os.Rename(paths[0], output)
	}
	output := filepath.Join(workDir, "result.zip")
	if err := zipOracleExcelFiles(output, paths); err != nil {
		return "", total, err
	}
	return output, total, nil
}

func zipOracleExcelFiles(output string, paths []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, path := range paths {
		err := func() error {
			source, err := os.Open(path)
			if err != nil {
				return err
			}
			defer source.Close()
			// XLSX is already compressed; storing avoids a second compression pass.
			target, err := writer.CreateHeader(&zip.FileHeader{Name: filepath.Base(path), Method: zip.Store})
			if err != nil {
				return err
			}
			_, err = io.Copy(target, source)
			return err
		}()
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for _, path := range paths {
		_ = os.Remove(path)
	}
	return nil
}
