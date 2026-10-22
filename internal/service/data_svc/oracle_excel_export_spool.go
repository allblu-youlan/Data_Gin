package data_svc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

type oracleExcelSheetSnapshot struct {
	Sheet  OracleExcelExportSheet
	Schema []reportoracle.ResultColumn
	Parts  []string
	Rows   int
}

// Persist one cursor exactly once. Repeated small sheets are replayed from this
// snapshot, so each output workbook contains the same small-table contents.
func spoolOracleExcelSheet(ctx context.Context, rows oracleExcelRows, sheet OracleExcelExportSheet, schema []reportoracle.ResultColumn, dir string, rowsPerFile int, progress func(int) error) (oracleExcelSheetSnapshot, error) {
	snapshot := oracleExcelSheetSnapshot{Sheet: sheet, Schema: schema}
	if rowsPerFile < 1 || rowsPerFile > oracleExcelRowsPerFile || len(sheet.Columns) == 0 {
		return snapshot, fmt.Errorf("Oracle Sheet 分片配置无效")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return snapshot, err
	}
	var file *os.File
	var writer *bufio.Writer
	var encoder *json.Encoder
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	openPart := func() error {
		part := filepath.Join(dir, fmt.Sprintf("part_%05d.jsonl", len(snapshot.Parts)+1))
		var err error
		file, err = os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		writer = bufio.NewWriterSize(file, 64*1024)
		encoder = json.NewEncoder(writer)
		snapshot.Parts = append(snapshot.Parts, part)
		return nil
	}
	closePart := func() error {
		if err := writer.Flush(); err != nil {
			return err
		}
		err := file.Close()
		file = nil
		return err
	}
	if err := openPart(); err != nil {
		return snapshot, err
	}
	values := make([]interface{}, len(sheet.Columns))
	targets := make([]interface{}, len(values))
	for index := range values {
		targets[index] = &values[index]
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		if snapshot.Rows > 0 && snapshot.Rows%rowsPerFile == 0 {
			if err := closePart(); err != nil {
				return snapshot, err
			}
			if err := openPart(); err != nil {
				return snapshot, err
			}
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
	return snapshot, closePart()
}

func renderOracleExcelSnapshots(ctx context.Context, snapshots []oracleExcelSheetSnapshot, workDir string, rowsPerFile int, progress func(int) error) (string, int, error) {
	if len(snapshots) == 0 || len(snapshots) > oracleExcelMaxSheets || rowsPerFile < 1 || rowsPerFile > oracleExcelRowsPerFile {
		return "", 0, fmt.Errorf("Oracle 多 Sheet 导出配置无效")
	}
	parts, total := 1, 0
	for _, snapshot := range snapshots {
		if len(snapshot.Parts) == 0 {
			return "", 0, fmt.Errorf("Sheet %s 缺少数据分片", snapshot.Sheet.SheetName)
		}
		parts = max(parts, len(snapshot.Parts))
		total += snapshot.Rows
	}
	paths := make([]string, 0, parts)
	for part := 0; part < parts; part++ {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		output := filepath.Join(workDir, fmt.Sprintf("export_%03d.xlsx", part+1))
		if parts == 1 {
			output = filepath.Join(workDir, excelMatchResultFileName)
		}
		if err := renderOracleExcelSnapshotWorkbook(ctx, snapshots, output, part, rowsPerFile); err != nil {
			return "", total, err
		}
		paths = append(paths, output)
		if progress != nil {
			if err := progress(total); err != nil {
				return "", total, err
			}
		}
	}
	if parts == 1 {
		return paths[0], total, nil
	}
	output := filepath.Join(workDir, "result.zip")
	if err := zipOracleExcelFiles(output, paths); err != nil {
		return "", total, err
	}
	return output, total, nil
}

func renderOracleExcelSnapshotWorkbook(ctx context.Context, snapshots []oracleExcelSheetSnapshot, output string, part, rowsPerFile int) error {
	file := excelize.NewFile()
	defer file.Close()
	styles, err := newReportExportStyles(file, "")
	if err != nil {
		return err
	}
	for index, snapshot := range snapshots {
		name := snapshot.Sheet.SheetName
		if index == 0 {
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
		readPart := part
		if snapshot.Rows <= rowsPerFile {
			readPart = 0
		}
		if readPart < len(snapshot.Parts) {
			if err := replayOracleExcelSheet(ctx, stream, styles, snapshot, snapshot.Parts[readPart]); err != nil {
				return err
			}
		}
		if err := stream.Flush(); err != nil {
			return err
		}
	}
	return file.SaveAs(output)
}

func replayOracleExcelSheet(ctx context.Context, stream *excelize.StreamWriter, styles reportExportStyles, snapshot oracleExcelSheetSnapshot, partPath string) error {
	file, err := os.Open(partPath)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 64*1024))
	decoder.UseNumber()
	types := make(map[string]string, len(snapshot.Schema))
	for _, column := range snapshot.Schema {
		types[column.Name] = oracleExcelValueType(column.DataType)
	}
	for rowNumber := 2; ; rowNumber++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var values []interface{}
		if err := decoder.Decode(&values); err != nil {
			if err == io.EOF {
				return nil
			}
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
}
