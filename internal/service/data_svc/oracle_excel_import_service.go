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
		input.FixedIdentifier = &fixed
		if fixed.Enabled {
			fixed.Field = strings.ToUpper(strings.TrimSpace(fixed.Field))
			if strings.TrimSpace(fixed.Value) == "" {
				return input, "", nil, errors.New("启用固定标识时，标识值不能为空")
			}
			if err := addColumn(fixed.Field); err != nil {
				return input, "", nil, err
			}
			if _, err := reportoracle.OperationValue(columns[len(columns)-1].DataType, fixed.Value); err != nil {
				return input, "", nil, fmt.Errorf("固定标识值无效: %w", err)
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
	count := 0
	err := withOracleOperationConnection(ctx, 2*time.Hour, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		var err error
		count, err = executeOracleExcelImportFilesOnConnection(queryCtx, connection, paths, config)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func executeOracleExcelImportOnConnection(ctx context.Context, connection oracleOperationConnection, path string, config OracleExcelImportConfig) (int, error) {
	return executeOracleExcelImportFilesOnConnection(ctx, connection, []string{path}, config)
}

const (
	oracleExcelImportBatchRows = 200
	oracleExcelImportMaxBinds  = 5000
	// Keep batches within the existing conservative 999-column budget.
	oracleExcelImportMaxBatchColumns = 999
)

func oracleExcelImportBatchSize(width int) int {
	return min(oracleExcelImportBatchRows, oracleExcelImportMaxBinds/width, oracleExcelImportMaxBatchColumns/width)
}

func oracleExcelImportBatchStatement(single string, width, rows int) string {
	insert := single[:strings.LastIndex(single, " VALUES (")]
	var statement strings.Builder
	statement.Grow(32 + rows*(len(insert)+width*8+12))
	// Separate INSERT statements evaluate identity/sequence defaults per row.
	// INSERT ALL ... SELECT 1 FROM DUAL can reuse one sequence value.
	statement.WriteString("BEGIN")
	for row := 0; row < rows; row++ {
		statement.WriteByte(' ')
		statement.WriteString(insert)
		statement.WriteString(" VALUES (")
		for column := 0; column < width; column++ {
			if column > 0 {
				statement.WriteString(", ")
			}
			fmt.Fprintf(&statement, ":%d", row*width+column+1)
		}
		statement.WriteString(");")
	}
	statement.WriteString(" END;")
	return statement.String()
}

func executeOracleExcelImportFilesOnConnection(ctx context.Context, connection oracleOperationConnection, paths []string, config OracleExcelImportConfig) (int, error) {
	if len(paths) == 0 {
		return 0, errors.New("请选择至少一个 Excel 文件")
	}
	schema, err := connection.InspectResultTable(ctx, reportoracle.ResultTableRef{Owner: config.Owner, Name: config.TableName})
	if err != nil {
		return 0, err
	}
	normalized, statement, columns, err := buildOracleExcelImport(config, schema)
	if err != nil {
		return 0, err
	}
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
	width := len(columns)
	batchSize := oracleExcelImportBatchSize(width)
	batch := make([][]interface{}, 0, batchSize)
	statements := make(map[int]string)
	var first, last oracleExcelImportLocation
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var err error
		if len(batch) == 1 {
			_, err = prepared.ExecContext(ctx, batch[0]...)
		} else {
			query := statements[len(batch)]
			if query == "" {
				query = oracleExcelImportBatchStatement(statement, width, len(batch))
				statements[len(batch)] = query
			}
			arguments := make([]interface{}, 0, len(batch)*width)
			for _, values := range batch {
				arguments = append(arguments, values...)
			}
			_, err = tx.ExecContext(ctx, query, arguments...)
		}
		if err != nil {
			return fmt.Errorf("批量写入失败（%s 至 %s）: %w", first, last, err)
		}
		clear(batch)
		batch = batch[:0]
		return nil
	}
	count := 0
	for _, path := range paths {
		read, err := readOracleExcelImportLocated(ctx, path, normalized, columns, func(values []interface{}, location oracleExcelImportLocation) error {
			if len(batch) == 0 {
				first = location
			}
			last = location
			batch = append(batch, values)
			if len(batch) == batchSize {
				return flush()
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
		count += read
	}
	if err := flush(); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit Oracle Excel import: %w", err)
	}
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
		cells, err := rows.Columns()
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
					value, err = excelize.ExcelDateToTime(serial, false)
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
				return 0, fmt.Errorf("固定标识值无效: %w", err)
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
