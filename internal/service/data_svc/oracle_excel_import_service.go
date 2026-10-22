package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

type OracleExcelImportConfig struct {
	Operation string                     `json:"operation"`
	Owner     string                     `json:"owner"`
	TableName string                     `json:"tableName"`
	SheetName string                     `json:"sheetName"`
	HeaderRow int                        `json:"headerRow"`
	Mappings  []OracleExcelImportMapping `json:"mappings"`
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
	fields, binds := make([]string, 0, len(input.Mappings)), make([]string, 0, len(input.Mappings))
	columns := make([]reportoracle.ResultColumn, 0, len(input.Mappings))
	seen := make(map[string]bool)
	input.Mappings = append([]OracleExcelImportMapping(nil), input.Mappings...)
	for index := range input.Mappings {
		mapping := &input.Mappings[index]
		mapping.ExcelColumn = strings.TrimSpace(mapping.ExcelColumn)
		mapping.Field = strings.ToUpper(strings.TrimSpace(mapping.Field))
		column, exists := available[mapping.Field]
		if mapping.ExcelColumn == "" || !exists || seen[mapping.Field] {
			return input, "", nil, fmt.Errorf("Excel 表头不能为空，Oracle 字段必须存在且不能重复: %s", mapping.Field)
		}
		// Reuse identifier validation for column names before quoting them.
		if _, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: table.Owner, Name: mapping.Field}); err != nil {
			return input, "", nil, err
		}
		if _, err := reportoracle.NormalizeOperationDataType(column.DataType); err != nil {
			return input, "", nil, err
		}
		seen[mapping.Field] = true
		fields = append(fields, `"`+mapping.Field+`"`)
		binds = append(binds, fmt.Sprintf(":%d", index+1))
		columns = append(columns, column)
	}
	statement := `INSERT INTO "` + table.Owner + `"."` + table.Name + `" (` + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(binds, ", ") + ")"
	return input, statement, columns, nil
}

func ExecuteOracleExcelImport(ctx context.Context, path string, config OracleExcelImportConfig) (int, error) {
	count := 0
	err := withOracleOperationConnection(ctx, 2*time.Hour, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		var err error
		count, err = executeOracleExcelImportOnConnection(queryCtx, connection, path, config)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func executeOracleExcelImportOnConnection(ctx context.Context, connection oracleOperationConnection, path string, config OracleExcelImportConfig) (int, error) {
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
	count, err := readOracleExcelImport(ctx, path, normalized, columns, func(values []interface{}) error {
		_, err := prepared.ExecContext(ctx, values...)
		return err
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit Oracle Excel import: %w", err)
	}
	return count, nil
}

func readOracleExcelImport(ctx context.Context, path string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, write func([]interface{}) error) (int, error) {
	file, err := excelize.OpenFile(path)
	if err != nil {
		return 0, fmt.Errorf("open import Excel: %w", err)
	}
	defer file.Close()
	rows, err := file.Rows(config.SheetName)
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
		values := make([]interface{}, len(indexes))
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
		if err := write(values); err != nil {
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
