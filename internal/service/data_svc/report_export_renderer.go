package data_svc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"github.com/xuri/excelize/v2"
)

const (
	reportExportPageSize              = 1000
	reportExportProgressRows          = int64(1000)
	reportExcelMaxRows                = 1_048_576
	reportExcelMaxDataRows            = reportExcelMaxRows - 1
	reportExcelMaxSheets              = 256
	reportExcelMaxCellRunes           = 32_767
	reportExcelDefaultWidth           = 18
	reportExcelMaximumWidth           = 80
	reportExcelDecimalNumberFormat    = "0.################"
	reportExcelTwoDecimalNumberFormat = "0.00"
)

type reportExportPageReader interface {
	Read(context.Context, []string, *reportoracle.ResultCursor, int) (reportoracle.ResultPage, error)
}

type ReportExportRenderRequest struct {
	Columns             []frozenResultColumn
	OutputPath          string
	decimalNumberFormat string
}

type ReportExportRenderProgress struct {
	ProcessedRows int64
	CurrentSheet  string
	SheetCount    int
	AfterKey      string
}

type ReportExportRenderResult struct {
	ProcessedRows      int64
	SheetCount         int
	TruncatedCellCount int64
}

type ReportExportRenderer struct {
	pager     reportExportPageReader
	pageSize  int
	maxSheets int
}

func NewReportExportRenderer(pager reportExportPageReader) *ReportExportRenderer {
	if pager == nil {
		panic("report export renderer: pager is required")
	}
	return &ReportExportRenderer{pager: pager, pageSize: reportExportPageSize, maxSheets: reportExcelMaxSheets}
}

func frozenExportColumns(raw model.JSONText) ([]frozenResultColumn, error) {
	var columns []frozenResultColumn
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&columns); err != nil {
		return nil, fmt.Errorf("report export renderer: decode frozen columns: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("report export renderer: frozen columns contain trailing data")
	}
	visible := make([]frozenResultColumn, 0, len(columns))
	seenFields := make(map[string]struct{}, len(columns))
	seenDatabaseColumns := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		if !column.ExportVisible || !column.ExportAllowed {
			continue
		}
		if column.FieldID == "" || column.LogicalCode == "" || column.DatabaseColumn == "" || strings.TrimSpace(column.ExcelHeader) == "" {
			return nil, fmt.Errorf("report export renderer: invalid frozen column")
		}
		fieldKey := strings.ToLower(column.LogicalCode)
		databaseKey := strings.ToUpper(column.DatabaseColumn)
		if _, duplicate := seenFields[fieldKey]; duplicate {
			return nil, fmt.Errorf("report export renderer: duplicate logical column")
		}
		if _, duplicate := seenDatabaseColumns[databaseKey]; duplicate {
			return nil, fmt.Errorf("report export renderer: duplicate database column")
		}
		seenFields[fieldKey] = struct{}{}
		seenDatabaseColumns[databaseKey] = struct{}{}
		visible = append(visible, column)
	}
	if len(visible) == 0 || len(visible) > 512 {
		return nil, fmt.Errorf("report export renderer: export columns are empty or excessive")
	}
	sort.SliceStable(visible, func(left, right int) bool {
		if visible[left].ExportOrder == visible[right].ExportOrder {
			return visible[left].LogicalCode < visible[right].LogicalCode
		}
		return visible[left].ExportOrder < visible[right].ExportOrder
	})
	return visible, nil
}

func (renderer *ReportExportRenderer) Render(
	ctx context.Context,
	request ReportExportRenderRequest,
	onProgress func(ReportExportRenderProgress) error,
) (result ReportExportRenderResult, returnErr error) {
	if renderer == nil || renderer.pager == nil || ctx == nil || request.OutputPath == "" || len(request.Columns) == 0 || len(request.Columns) > 512 ||
		renderer.pageSize < 1 || renderer.pageSize > 1000 || renderer.maxSheets < 1 || renderer.maxSheets > reportExcelMaxSheets {
		return result, fmt.Errorf("report export renderer: invalid request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return result, fmt.Errorf("report export renderer: output path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("report export renderer: inspect output path: %w", err)
	}

	file := excelize.NewFile()
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("report export renderer: close workbook: %w", err))
		}
		if returnErr != nil {
			if err := os.Remove(request.OutputPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, fmt.Errorf("report export renderer: remove partial workbook: %w", err))
			}
		}
	}()

	styles, err := newReportExportStyles(file, request.decimalNumberFormat)
	if err != nil {
		return result, err
	}
	databaseColumns := make([]string, len(request.Columns))
	for index := range request.Columns {
		databaseColumns[index] = request.Columns[index].DatabaseColumn
	}

	var stream *excelize.StreamWriter
	var currentSheet string
	rowsInSheet := int64(0)
	openSheet := func() error {
		if stream != nil {
			if err := stream.Flush(); err != nil {
				return fmt.Errorf("report export renderer: flush sheet: %w", err)
			}
		}
		if result.SheetCount >= renderer.maxSheets {
			return fmt.Errorf("report export renderer: sheet limit exceeded")
		}
		result.SheetCount++
		currentSheet = "数据"
		if result.SheetCount > 1 {
			currentSheet = fmt.Sprintf("数据_%d", result.SheetCount)
		}
		if result.SheetCount == 1 {
			if err := file.SetSheetName("Sheet1", currentSheet); err != nil {
				return fmt.Errorf("report export renderer: rename first sheet: %w", err)
			}
		} else if _, err := file.NewSheet(currentSheet); err != nil {
			return fmt.Errorf("report export renderer: create sheet: %w", err)
		}
		stream, err = file.NewStreamWriter(currentSheet)
		if err != nil {
			return fmt.Errorf("report export renderer: create stream writer: %w", err)
		}
		if err := stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
			return fmt.Errorf("report export renderer: freeze header: %w", err)
		}
		header := make([]interface{}, len(request.Columns))
		for index, column := range request.Columns {
			width := column.ExcelWidth
			if width <= 0 {
				width = reportExcelDefaultWidth
			}
			if width > reportExcelMaximumWidth {
				width = reportExcelMaximumWidth
			}
			if err := stream.SetColWidth(index+1, index+1, width); err != nil {
				return fmt.Errorf("report export renderer: set column width: %w", err)
			}
			header[index] = excelize.Cell{StyleID: styles.header, Value: column.ExcelHeader}
		}
		if err := stream.SetRow("A1", header); err != nil {
			return fmt.Errorf("report export renderer: write header: %w", err)
		}
		rowsInSheet = 0
		return nil
	}
	if err := openSheet(); err != nil {
		return result, err
	}

	var after *reportoracle.ResultCursor
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		page, err := renderer.pager.Read(ctx, databaseColumns, after, renderer.pageSize)
		if err != nil {
			return result, fmt.Errorf("report export renderer: read result page: %w", err)
		}
		if len(page.Columns) != len(databaseColumns) {
			return result, fmt.Errorf("report export renderer: page column count changed")
		}
		for index := range page.Columns {
			if !strings.EqualFold(page.Columns[index], databaseColumns[index]) {
				return result, fmt.Errorf("report export renderer: page column order changed")
			}
		}
		for _, row := range page.Rows {
			if len(row.Values) != len(request.Columns) {
				return result, fmt.Errorf("report export renderer: invalid result row")
			}
			if rowsInSheet == reportExcelMaxDataRows {
				if err := openSheet(); err != nil {
					return result, err
				}
			}
			cells := make([]interface{}, len(request.Columns))
			for index, column := range request.Columns {
				cell, truncated, err := reportExportCell(row.Values[index], column, styles)
				if err != nil {
					return result, err
				}
				if truncated {
					result.TruncatedCellCount++
				}
				cells[index] = cell
			}
			coordinate, err := excelize.CoordinatesToCellName(1, int(rowsInSheet)+2)
			if err != nil {
				return result, fmt.Errorf("report export renderer: row coordinate: %w", err)
			}
			if err := stream.SetRow(coordinate, cells); err != nil {
				return result, fmt.Errorf("report export renderer: write row: %w", err)
			}
			rowsInSheet++
			result.ProcessedRows++
			if onProgress != nil && result.ProcessedRows%reportExportProgressRows == 0 {
				if err := onProgress(ReportExportRenderProgress{ProcessedRows: result.ProcessedRows, CurrentSheet: currentSheet, SheetCount: result.SheetCount, AfterKey: row.Key}); err != nil {
					return result, fmt.Errorf("report export renderer: update progress: %w", err)
				}
			}
		}
		if len(page.Rows) > 0 {
			last := page.Rows[len(page.Rows)-1]
			if page.NextKey != last.Key {
				return result, fmt.Errorf("report export renderer: page cursor mismatch")
			}
			next := reportoracle.ResultCursor{Key: page.NextKey}
			next.SortValue = last.SortValue
			if after != nil && after.Key == next.Key && reflect.DeepEqual(after.SortValue, next.SortValue) {
				return result, fmt.Errorf("report export renderer: non-advancing result cursor")
			}
			after = &next
		}
		if page.HasNext && len(page.Rows) == 0 {
			return result, fmt.Errorf("report export renderer: empty page has continuation")
		}
		if !page.HasNext {
			break
		}
	}
	if stream != nil {
		if err := stream.Flush(); err != nil {
			return result, fmt.Errorf("report export renderer: flush final sheet: %w", err)
		}
	}
	if err := file.SaveAs(request.OutputPath); err != nil {
		return result, fmt.Errorf("report export renderer: save workbook: %w", err)
	}
	return result, nil
}

type reportExportStyles struct {
	header   int
	text     int
	integer  int
	decimal  int
	date     int
	datetime int
}

func newReportExportStyles(file *excelize.File, decimalNumberFormat string) (reportExportStyles, error) {
	if decimalNumberFormat == "" {
		decimalNumberFormat = reportExcelDecimalNumberFormat
	}
	styles := reportExportStyles{}
	definitions := []struct {
		destination *int
		style       *excelize.Style
	}{
		{&styles.header, &excelize.Style{Font: &excelize.Font{Bold: true, Color: "#FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#FF9D0A"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}}},
		{&styles.text, &excelize.Style{NumFmt: 49}},
		{&styles.integer, &excelize.Style{CustomNumFmt: stringPointer("0")}},
		{&styles.decimal, &excelize.Style{CustomNumFmt: stringPointer(decimalNumberFormat)}},
		{&styles.date, &excelize.Style{CustomNumFmt: stringPointer("yyyy-mm-dd")}},
		{&styles.datetime, &excelize.Style{CustomNumFmt: stringPointer("yyyy-mm-dd hh:mm:ss")}},
	}
	for _, definition := range definitions {
		styleID, err := file.NewStyle(definition.style)
		if err != nil {
			return styles, fmt.Errorf("report export renderer: create style: %w", err)
		}
		*definition.destination = styleID
	}
	return styles, nil
}

func reportExportCell(value interface{}, column frozenResultColumn, styles reportExportStyles) (excelize.Cell, bool, error) {
	if value == nil {
		return excelize.Cell{StyleID: styles.text, Value: column.NullDisplay}, false, nil
	}
	if policy := bytes.TrimSpace(column.MaskingPolicy); len(policy) > 0 && !bytes.Equal(policy, []byte("{}")) && !bytes.Equal(policy, []byte("null")) {
		return excelize.Cell{StyleID: styles.text, Value: "***"}, false, nil
	}
	if typed, ok := value.([]byte); ok {
		value = string(typed)
	}
	valueType := strings.ToLower(strings.TrimSpace(column.ValueType))
	switch valueType {
	case "boolean", "bool":
		switch typed := value.(type) {
		case bool:
			return excelize.Cell{Value: typed}, false, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err != nil {
				return excelize.Cell{}, false, fmt.Errorf("report export renderer: invalid boolean value")
			}
			return excelize.Cell{Value: parsed}, false, nil
		}
	case "date", "datetime", "timestamp":
		parsed, err := reportExportTime(value)
		if err != nil {
			return excelize.Cell{}, false, err
		}
		style := styles.datetime
		if valueType == "date" {
			style = styles.date
		}
		return excelize.Cell{StyleID: style, Value: parsed}, false, nil
	case "integer":
		text := strings.TrimSpace(fmt.Sprint(value))
		if integer, err := strconv.ParseInt(text, 10, 64); err == nil && len(strings.TrimLeft(text, "-+0")) <= 15 {
			return excelize.Cell{StyleID: styles.integer, Value: integer}, false, nil
		}
		return reportExportTextCell(text, styles.text)
	case "decimal", "number":
		text := strings.TrimSpace(fmt.Sprint(value))
		if reportExportSignificantDigits(text) <= 15 {
			if number, err := strconv.ParseFloat(text, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				return excelize.Cell{StyleID: styles.decimal, Value: number}, false, nil
			}
		}
		return reportExportTextCell(text, styles.text)
	}
	return reportExportTextCell(fmt.Sprint(value), styles.text)
}

func reportExportTextCell(value string, style int) (excelize.Cell, bool, error) {
	truncated := false
	if utf8.RuneCountInString(value) > reportExcelMaxCellRunes {
		runes := []rune(value)
		value = string(runes[:reportExcelMaxCellRunes])
		truncated = true
	}
	return excelize.Cell{StyleID: style, Value: value}, truncated, nil
}

func reportExportSignificantDigits(value string) int {
	count := 0
	for _, character := range value {
		if character >= '0' && character <= '9' {
			count++
		}
	}
	return count
}

func reportExportTime(value interface{}) (time.Time, error) {
	if typed, ok := value.(time.Time); ok {
		return typed, nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", time.DateOnly} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("report export renderer: invalid date value")
}
