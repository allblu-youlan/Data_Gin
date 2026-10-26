package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

type OracleExcelImportMapping struct {
	ExcelColumn string `json:"excelColumn"`
	Field       string `json:"field"`
}

type OracleExcelImportFixedIdentifier struct {
	Enabled bool   `json:"enabled"`
	Field   string `json:"field"`
	Value   string `json:"value"`
}

type OracleExcelImportConfig struct {
	Operation       string                            `json:"operation"`
	Owner           string                            `json:"owner"`
	TableName       string                            `json:"tableName"`
	SheetName       string                            `json:"sheetName"`
	HeaderRow       int                               `json:"headerRow"`
	Mappings        []OracleExcelImportMapping        `json:"mappings"`
	FixedIdentifier *OracleExcelImportFixedIdentifier `json:"fixedIdentifier,omitempty"`
	AllSheets       bool                              `json:"allSheets,omitempty"`
	RawCellValues   bool                              `json:"rawCellValues,omitempty"`
}

func NormalizeOracleExcelImportConfig(ctx context.Context, input OracleExcelImportConfig) (OracleExcelImportConfig, error) {
	var normalized OracleExcelImportConfig
	err := withOracleOperationConnection(ctx, 30*time.Second, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		schema, err := connection.InspectResultTable(queryCtx, reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
		if err != nil {
			return err
		}
		normalized, _, _, err = buildOracleExcelImport(input, schema)
		return err
	})
	return normalized, err
}

func buildOracleExcelImport(input OracleExcelImportConfig, schema []reportoracle.ResultColumn) (OracleExcelImportConfig, string, []reportoracle.ResultColumn, error) {
	table, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
	if err != nil {
		return input, "", nil, err
	}
	input.Operation, input.Owner, input.TableName = ExcelOperationOracleImport, table.Owner, table.Name
	input.SheetName = strings.TrimSpace(input.SheetName)
	if input.SheetName == "" {
		input.SheetName = defaultExcelSheetName
	}
	if input.HeaderRow == 0 {
		input.HeaderRow = 1
	}
	if input.HeaderRow < 1 || input.HeaderRow > excelMaxRowsPerSheet || len(input.Mappings) == 0 || len(input.Mappings) > 512 {
		return input, "", nil, errors.New("表头行或字段映射数量无效")
	}
	available := make(map[string]reportoracle.ResultColumn, len(schema))
	for _, column := range schema {
		available[strings.ToUpper(column.Name)] = column
	}
	fields, binds := make([]string, 0, len(input.Mappings)+1), make([]string, 0, len(input.Mappings)+1)
	columns := make([]reportoracle.ResultColumn, 0, len(input.Mappings)+1)
	seen := make(map[string]bool)
	addColumn := func(field string) error {
		column, exists := available[field]
		if !exists || seen[field] {
			return fmt.Errorf("Oracle 字段必须存在且不能重复: %s", field)
		}
		if _, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: table.Owner, Name: field}); err != nil {
			return err
		}
		if _, err := reportoracle.NormalizeOperationDataType(column.DataType); err != nil {
			return err
		}
		seen[field] = true
		fields = append(fields, `"`+field+`"`)
		binds = append(binds, fmt.Sprintf(":%d", len(fields)))
		columns = append(columns, column)
		return nil
	}
	input.Mappings = append([]OracleExcelImportMapping(nil), input.Mappings...)
	for index := range input.Mappings {
		mapping := &input.Mappings[index]
		mapping.ExcelColumn = strings.TrimSpace(mapping.ExcelColumn)
		mapping.Field = strings.ToUpper(strings.TrimSpace(mapping.Field))
		if mapping.ExcelColumn == "" {
			return input, "", nil, errors.New("Excel 表头不能为空")
		}
		if err := addColumn(mapping.Field); err != nil {
			return input, "", nil, err
		}
	}
	if input.FixedIdentifier != nil {
		fixed := *input.FixedIdentifier
		// Saved configurations never retain an execution's batch number.
		fixed.Value = ""
		input.FixedIdentifier = &fixed
		if fixed.Enabled {
			fixed.Field = strings.ToUpper(strings.TrimSpace(fixed.Field))
			if err := addColumn(fixed.Field); err != nil {
				return input, "", nil, err
			}
			if _, err := reportoracle.OperationValue(columns[len(columns)-1].DataType, "1"); err != nil {
				return input, "", nil, fmt.Errorf("导入标号字段须支持字符或数值: %w", err)
			}
		}
	}
	statement := `INSERT INTO "` + table.Owner + `"."` + table.Name + `" (` + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(binds, ", ") + ")"
	return input, statement, columns, nil
}

func ExecuteOracleExcelImport(ctx context.Context, path string, config OracleExcelImportConfig) (int, error) {
	return ExecuteOracleExcelImportFiles(ctx, []string{path}, config)
}

func ExecuteOracleExcelImportFiles(ctx context.Context, paths []string, config OracleExcelImportConfig) (int, error) {
	return ExecuteOracleExcelImportFilesWithProgress(ctx, paths, config, nil)
}

func ExecuteOracleExcelImportFilesWithProgress(ctx context.Context, paths []string, config OracleExcelImportConfig, progress func(OracleExcelImportProgress)) (int, error) {
	count := 0
	err := withOracleOperationConnection(ctx, 2*time.Hour, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		var err error
		count, err = executeOracleExcelImportFilesWithProgressOnConnection(queryCtx, connection, paths, config, nextOracleExcelImportIdentifier, progress)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func executeOracleExcelImportOnConnection(ctx context.Context, connection oracleOperationConnection, path string, config OracleExcelImportConfig) (int, error) {
	return executeOracleExcelImportFilesOnConnection(ctx, connection, []string{path}, config, nextOracleExcelImportIdentifier)
}

func executeOracleExcelImportFilesOnConnection(ctx context.Context, connection oracleOperationConnection, paths []string, config OracleExcelImportConfig, nextIdentifier func(context.Context) (string, error)) (int, error) {
	return executeOracleExcelImportFilesWithProgressOnConnection(ctx, connection, paths, config, nextIdentifier, nil)
}

func executeOracleExcelImportFilesWithProgressOnConnection(ctx context.Context, connection oracleOperationConnection, paths []string, config OracleExcelImportConfig, nextIdentifier func(context.Context) (string, error), progress func(OracleExcelImportProgress)) (imported int, importErr error) {
	if len(paths) == 0 {
		return 0, errors.New("请选择至少一个 Excel 文件")
	}
	reporter := oracleExcelImportProgressReporter{callback: progress, started: time.Now(), state: OracleExcelImportProgress{Stage: "counting", FileCount: len(paths), BatchRows: oracleExcelImportBatchRows}}
	reporter.emit(true)
	defer func() {
		if importErr != nil {
			reporter.state.Stage = "failed"
			reporter.emit(true)
		}
	}()
	schema, err := connection.InspectResultTable(ctx, reportoracle.ResultTableRef{Owner: config.Owner, Name: config.TableName})
	if err != nil {
		return 0, err
	}
	normalized, statement, columns, err := buildOracleExcelImport(config, schema)
	if err != nil {
		return 0, err
	}
	if progress != nil {
		countStarted := time.Now()
		// Counting needs mapped nonempty cells, not display formatting or
		// conversion of every value into an Oracle binding a second time.
		for index, path := range paths {
			reporter.state.FileIndex, reporter.state.FileName = index+1, filepath.Base(path)
			reporter.state.SheetName, reporter.state.RowNumber = "", 0
			reporter.emit(true)
			_, err := countOracleExcelImportLocated(ctx, path, normalized, func(location oracleExcelImportLocation) error {
				reporter.state.ScannedRows++
				reporter.state.SheetName, reporter.state.RowNumber = location.sheet, location.row
				if reporter.state.ScannedRows%1000 == 0 {
					reporter.emit(false)
				}
				return nil
			})
			if err != nil {
				return 0, err
			}
			reporter.emit(true)
		}
		reporter.state.TotalRows, reporter.state.TotalKnown = reporter.state.ScannedRows, true
		reporter.state.CountMillis = time.Since(countStarted).Milliseconds()
	}
	if fixed := normalized.FixedIdentifier; fixed != nil && fixed.Enabled {
		// Allocate once per execution, before any Oracle insert. All files,
		// sheets and rows in this execution receive this same new number.
		fixed.Value, err = nextIdentifier(ctx)
		if err != nil {
			return 0, fmt.Errorf("生成导入标号失败: %w", err)
		}
		reporter.state.Identifier = fixed.Value
	}
	reporter.state.Stage = "importing"
	reporter.state.FileIndex, reporter.state.FileName = 1, filepath.Base(paths[0])
	reporter.state.SheetName, reporter.state.RowNumber = "", 0
	reporter.emit(true)
	tx, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		return 0, fmt.Errorf("prepare Oracle Excel import: %w", err)
	}
	defer prepared.Close()
	batches, stopReading := streamOracleExcelImportBatches(ctx, paths, normalized, columns, oracleExcelImportBatchRows)
	defer stopReading()
	var writeDuration time.Duration
	count := 0
	lastProgressFileIndex := 0
	for batch := range batches {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		reporter.state.FileIndex, reporter.state.FileName = batch.FileIndex, batch.Last.file
		reporter.state.SheetName, reporter.state.RowNumber = batch.Last.sheet, batch.Last.row
		reporter.state.ReadMillis = batch.ReadDuration.Milliseconds()
		var arguments []interface{}
		if len(batch.Values) == 1 {
			arguments = batch.Values[0]
		} else {
			arguments, err = oracleExcelImportArrayBinds(columns, batch.Values)
			if err != nil {
				return 0, err
			}
		}
		writeStarted := time.Now()
		_, err = prepared.ExecContext(ctx, arguments...)
		writeDuration += time.Since(writeStarted)
		reporter.state.WriteMillis = writeDuration.Milliseconds()
		if err != nil {
			return 0, fmt.Errorf("批量写入失败（%s 至 %s）: %w", batch.First, batch.Last, err)
		}
		count += len(batch.Values)
		reporter.state.ProcessedRows, reporter.state.Batches = count, reporter.state.Batches+1
		reporter.emit(batch.FileIndex != lastProgressFileIndex)
		lastProgressFileIndex = batch.FileIndex
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
	if err := stopReading(); err != nil {
		return 0, err
	}
	// The actual reader is authoritative (for example, hidden display formats
	// can make a raw nonempty cell look blank). Never finish above/below 100%.
	reporter.state.TotalRows, reporter.state.TotalKnown = count, true
	reporter.state.Stage = "committing"
	reporter.emit(true)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit Oracle Excel import: %w", err)
	}
	reporter.state.Stage = "completed"
	reporter.emit(true)
	return count, nil
}

func readOracleExcelImport(ctx context.Context, path string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, write func([]interface{}) error) (int, error) {
	return readOracleExcelImportLocated(ctx, path, config, columns, func(values []interface{}, _ oracleExcelImportLocation) error { return write(values) })
}

type oracleExcelImportLocation struct {
	file  string
	sheet string
	row   int
}

func (location oracleExcelImportLocation) String() string {
	return fmt.Sprintf("文件 %s，工作表 %s，第 %d 行", location.file, location.sheet, location.row)
}

func readOracleExcelImportLocated(ctx context.Context, path string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, write func([]interface{}, oracleExcelImportLocation) error) (int, error) {
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
		read, err := readOracleExcelImportSheet(ctx, file, sheet, config, columns, func(values []interface{}, row int) error {
			return write(values, oracleExcelImportLocation{file: filepath.Base(path), sheet: sheet, row: row})
		})
		if err != nil {
			return 0, fmt.Errorf("文件 %s，工作表 %s: %w", filepath.Base(path), sheet, err)
		}
		count += read
	}
	return count, nil
}

func readOracleExcelImportSheet(ctx context.Context, file *excelize.File, sheet string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, write func([]interface{}, int) error) (int, error) {
	date1904 := false
	if config.RawCellValues {
		properties, err := file.GetWorkbookProps()
		if err != nil {
			return 0, fmt.Errorf("读取 Excel 日期系统失败: %w", err)
		}
		date1904 = properties.Date1904 != nil && *properties.Date1904
	}
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
		cells, err := rows.Columns(excelize.Options{RawCellValue: config.RawCellValues && rowNumber != config.HeaderRow})
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
		values := make([]interface{}, len(columns))
		empty := true
		for index, columnIndex := range indexes {
			cell := ""
			if columnIndex < len(cells) {
				cell = cells[columnIndex]
			}
			if strings.TrimSpace(cell) != "" {
				empty = false
			}
			value, err := reportoracle.OperationValue(columns[index].DataType, cell)
			if err != nil && oracleExcelValueType(columns[index].DataType) == "datetime" {
				if serial, parseErr := strconv.ParseFloat(cell, 64); parseErr == nil {
					if config.RawCellValues {
						var date time.Time
						date, err = excelize.ExcelDateToTime(serial, date1904)
						if err == nil {
							// Excel serials describe wall-clock components, not UTC instants.
							value, err = reportoracle.OperationValue(columns[index].DataType, date.Format("2006-01-02 15:04:05.999999999"))
						}
					} else {
						value, err = excelize.ExcelDateToTime(serial, false)
					}
				}
			}
			if err != nil {
				return 0, fmt.Errorf("Excel 第 %d 行，字段 %s: %w", rowNumber, columns[index].Name, err)
			}
			values[index] = value
		}
		if empty {
			continue
		}
		if fixed := config.FixedIdentifier; fixed != nil && fixed.Enabled {
			value, err := reportoracle.OperationValue(columns[len(columns)-1].DataType, fixed.Value)
			if err != nil {
				return 0, fmt.Errorf("导入标号无效: %w", err)
			}
			values[len(values)-1] = value
		}
		if err := write(values, rowNumber); err != nil {
			return 0, fmt.Errorf("写入 Excel 第 %d 行失败: %w", rowNumber, err)
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
