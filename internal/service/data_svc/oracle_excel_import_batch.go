package data_svc

import (
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/godror/godror"
)

const oracleExcelImportBatchRows = 1000

// Each slice binds one column of a single INSERT. godror executes the INSERT
// once per row with native array DML, preserving per-row identity defaults.
func oracleExcelImportArrayBinds(columns []reportoracle.ResultColumn, rows [][]interface{}) ([]interface{}, error) {
	arguments := make([]interface{}, len(columns))
	for index, column := range columns {
		kind, err := reportoracle.NormalizeOperationDataType(column.DataType)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "NUMBER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE", "INTEGER":
			arguments[index], err = oracleExcelImportColumnValues[godror.Number](rows, index)
		case "DATE", "TIMESTAMP":
			arguments[index], err = oracleExcelImportColumnValues[time.Time](rows, index)
		case "CLOB", "NCLOB":
			var values []godror.Lob
			values, err = oracleExcelImportColumnValues[godror.Lob](rows, index)
			for row := range values {
				// The driver infers the LOB type from the first element, even NULL.
				values[row].IsClob = true
			}
			arguments[index] = values
		default:
			arguments[index], err = oracleExcelImportColumnValues[string](rows, index)
		}
		if err != nil {
			return nil, fmt.Errorf("字段 %s 的批量绑定失败: %w", column.Name, err)
		}
	}
	return arguments, nil
}

func oracleExcelImportColumnValues[T any](rows [][]interface{}, column int) ([]T, error) {
	values := make([]T, len(rows))
	for index, row := range rows {
		if column >= len(row) {
			return nil, fmt.Errorf("第 %d 条记录缺少字段", index+1)
		}
		if row[column] == nil {
			continue // The driver maps zero Number, Time and empty strings to NULL.
		}
		value, ok := row[column].(T)
		if !ok {
			return nil, fmt.Errorf("第 %d 条记录的类型 %T 无法绑定", index+1, row[column])
		}
		values[index] = value
	}
	return values, nil
}
