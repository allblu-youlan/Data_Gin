package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type excelMatchPipelineRow struct {
	rowNumber    int
	values       []string
	participated bool
	stepResults  []ExcelMatchPreviewStepResult
}

type excelMatchPipelineLayout struct {
	headers          []string
	originalWidth    int
	columnIndexes    map[string]int
	stepInputIndexes []int
	emptyCellFills   []excelEmptyCellFillIndexes
	columnFormats    []string
}

type excelEmptyCellFillIndexes struct {
	targetIndex int
	sourceIndex int
}

type excelMatchPipelineState struct {
	orderItems *excelOrderItemMatchState
}

func newExcelMatchPipelineState(reservations excelOrderItemReservations) *excelMatchPipelineState {
	return &excelMatchPipelineState{orderItems: newExcelOrderItemMatchState(reservations)}
}

func processExcelMatchFile(ctx context.Context, inputPath, outputPath string, config ExcelMatchConfig, lookup ExcelMatchLookup) (ExcelMatchJobStats, error) {
	return processExcelMatchFileWithProgress(ctx, inputPath, outputPath, config, lookup, nil)
}

func prepareExcelMatchPipeline(headers []string, config ExcelMatchConfig) (excelMatchPipelineLayout, error) {
	layout := excelMatchPipelineLayout{
		originalWidth: len(headers),
		columnIndexes: make(map[string]int, len(headers)+len(config.Steps)),
	}
	layout.headers = append(layout.headers, headers...)
	originalColumns := make(map[string]struct{}, len(headers))
	for index, header := range headers {
		layout.columnIndexes[header] = index
		originalColumns[header] = struct{}{}
	}
	for index, step := range config.Steps {
		for _, filter := range step.Filters {
			if _, ok := layout.columnIndexes[filter.Column]; !ok {
				return layout, fmt.Errorf("第 %d 个匹配步骤缺少筛选列: %s", index+1, filter.Column)
			}
		}
		inputIndex, ok := layout.columnIndexes[step.MatchExcelColumn]
		if !ok {
			return layout, fmt.Errorf("第 %d 个匹配步骤缺少输入列: %s", index+1, step.MatchExcelColumn)
		}
		if step.MatchMode == excelMatchModeOrderItemSKU {
			for _, column := range []string{step.MatchExcelColumn, step.SpecExcelColumn, step.PriceExcelColumn, step.QtyExcelColumn} {
				if _, ok := originalColumns[column]; !ok {
					return layout, fmt.Errorf("第 %d 个订单商品SKU匹配步骤必须使用未被前序条件写入修改的原始Excel列: %s", index+1, column)
				}
			}
			for _, filter := range step.Filters {
				if _, ok := originalColumns[filter.Column]; !ok {
					return layout, fmt.Errorf("第 %d 个订单商品SKU匹配步骤筛选只能使用未被前序条件写入修改的原始Excel列: %s", index+1, filter.Column)
				}
			}
		}
		_, outputExists := layout.columnIndexes[step.OutputColumnName]
		if outputExists && step.MatchMode != excelMatchModeConditionalWrite {
			return layout, fmt.Errorf("第 %d 个匹配步骤输出列已存在: %s", index+1, step.OutputColumnName)
		}
		layout.stepInputIndexes = append(layout.stepInputIndexes, inputIndex)
		if !outputExists {
			layout.columnIndexes[step.OutputColumnName] = len(layout.headers)
			layout.headers = append(layout.headers, step.OutputColumnName)
		}
		if step.MatchMode == excelMatchModeConditionalWrite {
			delete(originalColumns, step.OutputColumnName)
		}
	}
	for _, fill := range config.EmptyCellFills {
		targetIndex, targetExists := layout.columnIndexes[fill.TargetColumn]
		if !targetExists {
			return layout, fmt.Errorf("空值填充目标列不存在: %s", fill.TargetColumn)
		}
		sourceIndex, sourceExists := layout.columnIndexes[fill.SourceColumn]
		if !sourceExists {
			return layout, fmt.Errorf("空值填充来源列不存在: %s", fill.SourceColumn)
		}
		layout.emptyCellFills = append(layout.emptyCellFills, excelEmptyCellFillIndexes{
			targetIndex: targetIndex,
			sourceIndex: sourceIndex,
		})
	}
	formatByColumn := excelExportColumnFormatMap(config.ExportColumnFormats)
	for column := range formatByColumn {
		if _, ok := layout.columnIndexes[column]; !ok {
			return layout, fmt.Errorf("导出格式配置列不存在: %s", column)
		}
	}
	layout.columnFormats = excelExportColumnFormatsForHeaders(layout.headers, formatByColumn)
	return layout, nil
}

func runExcelMatchSteps(ctx context.Context, config ExcelMatchConfig, lookup ExcelMatchLookup, layout excelMatchPipelineLayout, state *excelMatchPipelineState, rows []*excelMatchPipelineRow) error {
	for stepIndex, step := range config.Steps {
		if step.MatchMode == excelMatchModeConditionalWrite {
			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return err
				}
				result := applyExcelConditionalWrite(stepIndex, step, layout, row)
				row.stepResults = append(row.stepResults, result)
			}
			continue
		}
		keys := make([]string, 0, len(rows))
		seen := make(map[string]struct{}, len(rows))
		eligibleRows := make([]bool, len(rows))
		inputIndex := layout.stepInputIndexes[stepIndex]
		for rowIndex, row := range rows {
			eligibleRows[rowIndex] = excelRowMatchesFilters(row.values, layout.columnIndexes, step.Filters)
			if !eligibleRows[rowIndex] || inputIndex >= len(row.values) {
				continue
			}
			if step.MatchMode == excelMatchModeOrderItemSKU {
				specCode := excelMatchRowValue(row.values, layout.columnIndexes[step.SpecExcelColumn])
				if skipExcelOrderItemSpecCode(specCode) {
					row.participated = true
					continue
				}
				row.participated = true
				if normalizeExcelSpecCode(specCode) == "" {
					continue
				}
				if _, ok := parseExcelMatchPrice(excelMatchRowValue(row.values, layout.columnIndexes[step.PriceExcelColumn])); !ok {
					continue
				}
				if _, ok := parseExcelMatchNumber(excelMatchRowValue(row.values, layout.columnIndexes[step.QtyExcelColumn])); !ok {
					continue
				}
			} else {
				row.participated = true
			}
			key := strings.TrimSpace(row.values[inputIndex])
			if key != "" {
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					keys = append(keys, key)
				}
			}
		}
		if step.MatchMode == excelMatchModeOrderItemSKU {
			if err := runExcelOrderItemMatchStep(ctx, stepIndex, step, lookup, layout, state.orderItems, rows, eligibleRows, keys); err != nil {
				return err
			}
			continue
		}
		matches := map[string]string{}
		if len(keys) > 0 {
			var err error
			matches, err = lookup.Lookup(ctx, step, keys)
			if err != nil {
				return fmt.Errorf("执行第 %d 个匹配步骤失败: %w", stepIndex+1, err)
			}
		}
		for rowIndex, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			result := ExcelMatchPreviewStepResult{StepIndex: stepIndex + 1, StepName: step.Name, Status: "skipped", Reason: "未命中本步骤筛选"}
			value := ""
			if eligibleRows[rowIndex] {
				if inputIndex < len(row.values) {
					result.MatchKey = strings.TrimSpace(row.values[inputIndex])
				}
				if result.MatchKey == "" {
					result.Status = "unmatched"
					result.Reason = "匹配键为空"
				} else if matched, ok := matches[result.MatchKey]; ok {
					value = matched
					result.MatchedValue = matched
					result.Status = "matched"
					result.Reason = "已匹配"
				} else {
					result.Status = "unmatched"
					result.Reason = "数据库无匹配记录"
				}
			}
			row.values = append(row.values, value)
			row.stepResults = append(row.stepResults, result)
		}
	}
	fillExcelEmptyCells(layout.emptyCellFills, rows)
	return nil
}

func applyExcelConditionalWrite(
	stepIndex int,
	step ExcelMatchStep,
	layout excelMatchPipelineLayout,
	row *excelMatchPipelineRow,
) ExcelMatchPreviewStepResult {
	outputIndex := layout.columnIndexes[step.OutputColumnName]
	if outputIndex >= len(row.values) {
		row.values = append(row.values, make([]string, outputIndex+1-len(row.values))...)
	}
	result := ExcelMatchPreviewStepResult{
		StepIndex: stepIndex + 1,
		StepName:  step.Name,
		Status:    "skipped",
		Reason:    "未命中本步骤筛选",
	}
	if !excelRowMatchesFilters(row.values, layout.columnIndexes, step.Filters) {
		return result
	}
	result.MatchKey = excelMatchRowValue(row.values, layout.stepInputIndexes[stepIndex])
	if !strings.Contains(result.MatchKey, step.ContainsValue) {
		result.Reason = "判断列不包含指定文本，保留原值"
		return result
	}
	row.participated = true
	row.values[outputIndex] = step.WriteValue
	result.MatchedValue = step.WriteValue
	result.Status = "matched"
	result.Reason = "条件成立，已写入指定值"
	return result
}

func fillExcelEmptyCells(fills []excelEmptyCellFillIndexes, rows []*excelMatchPipelineRow) {
	for _, row := range rows {
		sourceValues := append([]string(nil), row.values...)
		for _, fill := range fills {
			if fill.targetIndex >= len(row.values) || fill.sourceIndex >= len(row.values) {
				continue
			}
			if strings.TrimSpace(row.values[fill.targetIndex]) == "" {
				row.values[fill.targetIndex] = sourceValues[fill.sourceIndex]
			}
		}
	}
}

func updateExcelMatchFinalStats(stats *ExcelMatchJobStats, rows []*excelMatchPipelineRow) {
	for _, row := range rows {
		if row.participated {
			stats.FilteredRows++
			if result, ok := lastApplicableExcelMatchStepResult(row.stepResults); ok && result.Status == "matched" {
				stats.MatchedRows++
			} else {
				stats.UnmatchedRows++
			}
		}
		stats.ProcessedRows++
	}
}

func lastApplicableExcelMatchStepResult(results []ExcelMatchPreviewStepResult) (ExcelMatchPreviewStepResult, bool) {
	for index := len(results) - 1; index >= 0; index-- {
		if results[index].Status != "skipped" {
			return results[index], true
		}
	}
	return ExcelMatchPreviewStepResult{}, false
}

func processExcelMatchPreview(ctx context.Context, input *excelize.File, config ExcelMatchConfig, lookup ExcelMatchLookup, scanLimit, sampleLimit int) (*ExcelMatchPreviewResult, error) {
	if scanLimit <= 0 || scanLimit > defaultExcelPreviewRows {
		scanLimit = defaultExcelPreviewRows
	}
	if sampleLimit <= 0 || sampleLimit > defaultExcelPreviewItems {
		sampleLimit = defaultExcelPreviewItems
	}
	if len(config.Steps) == 0 {
		return nil, errors.New("至少需要一个匹配步骤")
	}
	if !sheetExists(input.GetSheetList(), config.SheetName) {
		return nil, fmt.Errorf("Excel 不存在 sheet: %s", config.SheetName)
	}
	reservations, err := collectExcelOrderItemReservations(ctx, input, config, 0)
	if err != nil {
		return nil, err
	}
	rows, err := input.Rows(config.SheetName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := &ExcelMatchPreviewResult{Config: config, ScanLimit: scanLimit, SampleLimit: sampleLimit}
	state := newExcelMatchPipelineState(reservations)
	var layout excelMatchPipelineLayout
	var buffered []*excelMatchPipelineRow
	headerRead := false
	flush := func() error {
		if len(buffered) == 0 {
			return nil
		}
		if err := runExcelMatchSteps(ctx, config, lookup, layout, state, buffered); err != nil {
			return err
		}
		updateExcelMatchFinalStats(&result.Stats, buffered)
		for _, row := range buffered {
			if len(result.Samples) >= sampleLimit {
				break
			}
			sample := ExcelMatchPreviewSample{RowNumber: row.rowNumber, Values: make(map[string]string, len(layout.headers)), StepResults: row.stepResults}
			for index, header := range layout.headers {
				if index < len(row.values) {
					sample.Values[header] = row.values[index]
				}
			}
			if last, ok := lastApplicableExcelMatchStepResult(row.stepResults); ok {
				sample.MatchKey, sample.MatchedValue, sample.Status, sample.Reason = last.MatchKey, last.MatchedValue, last.Status, last.Reason
			} else {
				sample.Status, sample.Reason = "skipped", "未命中任一步骤条件"
			}
			result.Samples = append(result.Samples, sample)
		}
		buffered = buffered[:0]
		return nil
	}
	for rows.Next() {
		columns, err := rows.Columns()
		if err != nil {
			return result, err
		}
		if !headerRead {
			headers := normalizeHeaders(columns)
			if len(headers) == 0 {
				return result, errors.New("Excel 表头不能为空")
			}
			layout, err = prepareExcelMatchPipeline(headers, config)
			if err != nil {
				return result, err
			}
			headerRead = true
			continue
		}
		result.Stats.TotalRows++
		values := normalizeExcelRow(columns, layout.originalWidth)
		buffered = append(buffered, &excelMatchPipelineRow{rowNumber: result.Stats.TotalRows + 1, values: values})
		if len(buffered) >= config.BatchSize {
			if err := flush(); err != nil {
				return result, err
			}
		}
		if result.Stats.TotalRows >= scanLimit {
			result.Truncated = true
			break
		}
	}
	if err := rows.Error(); err != nil {
		return result, err
	}
	if !headerRead {
		return result, errors.New("Excel 表头不能为空")
	}
	if err := flush(); err != nil {
		return result, err
	}
	return result, nil
}

func processExcelMatchFileWithProgress(ctx context.Context, inputPath, outputPath string, config ExcelMatchConfig, lookup ExcelMatchLookup, onProgress func(ExcelMatchJobStats)) (ExcelMatchJobStats, error) {
	input, err := excelize.OpenFile(inputPath)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}
	defer func() { _ = input.Close() }()
	if len(config.Steps) == 0 {
		return ExcelMatchJobStats{}, errors.New("至少需要一个匹配步骤")
	}
	if !sheetExists(input.GetSheetList(), config.SheetName) {
		return ExcelMatchJobStats{}, fmt.Errorf("Excel 不存在 sheet: %s", config.SheetName)
	}
	reservations, err := collectExcelOrderItemReservations(ctx, input, config, 0)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}
	rows, err := input.Rows(config.SheetName)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}
	defer func() { _ = rows.Close() }()
	output := excelize.NewFile()
	defer func() { _ = output.Close() }()
	writer, currentSheet, err := initExcelMatchWriter(output)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}

	stats := ExcelMatchJobStats{}
	state := newExcelMatchPipelineState(reservations)
	var layout excelMatchPipelineLayout
	var buffered []*excelMatchPipelineRow
	rowInSheet, sheetIndex := 1, 1
	headerRead := false
	writeHeader := func() error {
		row := make([]interface{}, len(layout.headers))
		for index, header := range layout.headers {
			row[index] = header
		}
		if err := writer.SetRow("A1", row); err != nil {
			return err
		}
		rowInSheet = 2
		return nil
	}
	rotateSheet := func() error {
		if rowInSheet <= excelMaxRowsPerSheet {
			return nil
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		sheetIndex++
		currentSheet = "Result_" + strconv.Itoa(sheetIndex)
		if _, err := output.NewSheet(currentSheet); err != nil {
			return err
		}
		writer, err = output.NewStreamWriter(currentSheet)
		if err != nil {
			return err
		}
		return writeHeader()
	}
	flush := func() error {
		if len(buffered) == 0 {
			return nil
		}
		if err := runExcelMatchSteps(ctx, config, lookup, layout, state, buffered); err != nil {
			return err
		}
		for _, bufferedRow := range buffered {
			if err := rotateSheet(); err != nil {
				return err
			}
			row := make([]interface{}, len(layout.headers))
			for index, value := range normalizeExcelRow(bufferedRow.values, len(layout.headers)) {
				row[index] = excelExportValueForFormat(value, layout.columnFormats[index])
			}
			cell, err := excelize.CoordinatesToCellName(1, rowInSheet)
			if err != nil {
				return err
			}
			if err := writer.SetRow(cell, row); err != nil {
				return err
			}
			rowInSheet++
		}
		updateExcelMatchFinalStats(&stats, buffered)
		if onProgress != nil {
			onProgress(stats)
		}
		buffered = buffered[:0]
		return nil
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		columns, err := rows.Columns()
		if err != nil {
			return stats, err
		}
		if !headerRead {
			headers := normalizeHeaders(columns)
			if len(headers) == 0 {
				return stats, errors.New("Excel 表头不能为空")
			}
			layout, err = prepareExcelMatchPipeline(headers, config)
			if err != nil {
				return stats, err
			}
			if err := writeHeader(); err != nil {
				return stats, err
			}
			headerRead = true
			continue
		}
		stats.TotalRows++
		values := normalizeExcelRow(columns, layout.originalWidth)
		buffered = append(buffered, &excelMatchPipelineRow{values: values})
		if len(buffered) >= config.BatchSize || len(buffered) >= maxBufferedExcelRows {
			if err := flush(); err != nil {
				return stats, err
			}
		}
	}
	if err := rows.Error(); err != nil {
		return stats, err
	}
	if !headerRead {
		return stats, errors.New("Excel 表头不能为空")
	}
	if err := flush(); err != nil {
		return stats, err
	}
	if err := writer.Flush(); err != nil {
		return stats, err
	}
	output.SetActiveSheet(0)
	if err := output.SaveAs(outputPath); err != nil {
		return stats, err
	}
	return stats, nil
}
