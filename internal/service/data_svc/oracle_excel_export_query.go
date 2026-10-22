package data_svc

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportoracle"
)

const excelOperationOracleExport = "oracle_export"

type OracleExcelExportColumn struct {
	Field string `json:"field"`
	Title string `json:"title"`
}

type OracleExcelExportFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type OracleExcelExportConfig struct {
	Operation string                    `json:"operation"`
	Owner     string                    `json:"owner"`
	TableName string                    `json:"tableName"`
	Columns   []OracleExcelExportColumn `json:"columns"`
	Filters   []OracleExcelExportFilter `json:"filters"`
}

func buildOracleExcelExportQuery(input OracleExcelExportConfig, schema []reportoracle.ResultColumn) (OracleExcelExportConfig, string, []interface{}, error) {
	table, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
	if err != nil {
		return input, "", nil, fmt.Errorf("Oracle 表名无效: %w", err)
	}
	input.Operation, input.Owner, input.TableName = excelOperationOracleExport, table.Owner, table.Name
	if input.Filters == nil {
		input.Filters = make([]OracleExcelExportFilter, 0)
	}
	if len(schema) == 0 || len(schema) > 512 || len(input.Columns) > 512 || len(input.Filters) > 50 {
		return input, "", nil, fmt.Errorf("导出字段须为 1–512 个，筛选条件不得超过 50 个")
	}
	available := make(map[string]reportoracle.ResultColumn, len(schema))
	for _, column := range schema {
		available[column.Name] = column
	}
	if len(input.Columns) == 0 {
		for _, column := range schema {
			input.Columns = append(input.Columns, OracleExcelExportColumn{Field: column.Name, Title: oracleExcelDefaultColumnTitle(column)})
		}
	}
	fields := make([]string, len(input.Columns))
	for index := range input.Columns {
		column := &input.Columns[index]
		column.Field = strings.ToUpper(strings.TrimSpace(column.Field))
		if _, ok := available[column.Field]; !ok {
			return input, "", nil, fmt.Errorf("导出字段不存在: %s", column.Field)
		}
		column.Title = strings.TrimSpace(column.Title)
		if column.Title == "" {
			column.Title = oracleExcelDefaultColumnTitle(available[column.Field])
		}
		if utf8.RuneCountInString(column.Title) > 255 {
			return input, "", nil, fmt.Errorf("导出列名不得超过 255 字符")
		}
		fields[index] = column.Field
	}
	// Reuse the Oracle contract to validate identifiers, duplicates and metadata.
	if _, err := reportoracle.CompileResultSnapshotContract(reportoracle.ResultTableSnapshotRef(table, fields), schema); err != nil {
		return input, "", nil, fmt.Errorf("导出字段无效: %w", err)
	}
	quoted := make([]string, len(fields))
	for index, field := range fields {
		quoted[index] = `"` + field + `"`
	}
	statement := `SELECT ` + strings.Join(quoted, ", ") + ` FROM "` + table.Owner + `"."` + table.Name + `"`
	conditions := make([]string, 0, len(input.Filters))
	arguments := make([]interface{}, 0, len(input.Filters))
	for index := range input.Filters {
		filter := &input.Filters[index]
		filter.Field = strings.ToUpper(strings.TrimSpace(filter.Field))
		column, exists := available[filter.Field]
		if !exists {
			return input, "", nil, fmt.Errorf("筛选字段不存在: %s", filter.Field)
		}
		if _, err := reportoracle.CompileResultSnapshotContract(reportoracle.ResultTableSnapshotRef(table, []string{filter.Field}), schema); err != nil {
			return input, "", nil, fmt.Errorf("筛选字段无效: %w", err)
		}
		field := `"` + filter.Field + `"`
		filter.Op = strings.TrimSpace(filter.Op)
		if filter.Op == "empty" || filter.Op == "not_empty" {
			filter.Value = ""
			condition := field + " IS NULL"
			if filter.Op == "not_empty" {
				condition = field + " IS NOT NULL"
			}
			conditions = append(conditions, condition)
			continue
		}
		operators := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		operator, valid := operators[filter.Op]
		if !valid && filter.Op != "contains" {
			return input, "", nil, fmt.Errorf("不支持的筛选操作: %s", filter.Op)
		}
		if strings.TrimSpace(filter.Value) == "" {
			return input, "", nil, fmt.Errorf("筛选值不能为空，请使用为空/不为空操作")
		}
		parameter := OfficeQueryParameter{Code: filter.Field, ValueType: "string"}
		switch oracleExcelValueType(column.DataType) {
		case "number":
			parameter.ValueType = "decimal"
		case "datetime":
			filter.Value = strings.Replace(strings.TrimSpace(filter.Value), "T", " ", 1)
			if len(filter.Value) == 16 {
				filter.Value += ":00"
			}
			parameter.ValueType, parameter.Format = "date", "yyyy-MM-dd HH:mm:ss"
			if len(filter.Value) == 10 {
				parameter.Format = "yyyy-MM-dd"
			}
		}
		if filter.Op == "contains" && parameter.ValueType != "string" {
			return input, "", nil, fmt.Errorf("包含筛选只支持文本字段")
		}
		value, normalizedValue, err := officeParameterValue(parameter, filter.Value)
		if err != nil {
			return input, "", nil, fmt.Errorf("字段 %s 的筛选值无效: %w", filter.Field, err)
		}
		filter.Value = normalizedValue
		bindName := fmt.Sprintf("filter_%d", index)
		if filter.Op == "contains" {
			conditions = append(conditions, "INSTR("+field+", :"+bindName+") > 0")
		} else {
			conditions = append(conditions, field+" "+operator+" :"+bindName)
		}
		arguments = append(arguments, sql.Named(bindName, value))
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	return input, statement, arguments, nil
}

func oracleExcelDefaultColumnTitle(column reportoracle.ResultColumn) string {
	if comment := strings.TrimSpace(column.Comment); comment != "" {
		return comment
	}
	return column.Name
}

func oracleExcelValueType(dataType string) string {
	dataType = strings.ToUpper(strings.TrimSpace(dataType))
	switch {
	case dataType == "DATE", strings.HasPrefix(dataType, "TIMESTAMP"):
		return "datetime"
	case dataType == "NUMBER", dataType == "FLOAT", dataType == "BINARY_FLOAT", dataType == "BINARY_DOUBLE", dataType == "INTEGER":
		return "number"
	default:
		return "string"
	}
}
