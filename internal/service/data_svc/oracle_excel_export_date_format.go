package data_svc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reporting"
)

func oracleExcelFilterDateLayout(format string) (string, bool) {
	if layout, ok := officeDateLayout(format); ok {
		return layout, true
	}
	switch format {
	case "yyyy/MM/dd":
		return "2006/01/02", true
	case "yyyy.MM.dd":
		return "2006.01.02", true
	case "yyyy/MM/dd HH:mm:ss":
		return "2006/01/02 15:04:05", true
	default:
		return "", false
	}
}

func validateOracleExcelFilterDateFormat(filter OracleExcelExportFilter, dataType string) error {
	if filter.DateFormat == "" {
		return nil
	}
	if _, ok := oracleExcelFilterDateLayout(filter.DateFormat); !ok {
		return fmt.Errorf("不支持的日期格式: %s", filter.DateFormat)
	}
	if oracleExcelValueType(dataType) == "number" && filter.DateFormat != "yyyyMMdd" {
		return fmt.Errorf("数值日期字段仅支持 yyyyMMdd，带分隔符的格式请用于 DATE 或文本日期字段")
	}
	return nil
}

func oracleExcelFilterParameterValue(filter OracleExcelExportFilter, dataType string) (interface{}, string, error) {
	parameter := OfficeQueryParameter{Code: filter.Field, ValueType: "string"}
	value := filter.Value
	valueType := oracleExcelValueType(dataType)
	if filter.DateFormat != "" {
		layout, ok := oracleExcelFilterDateLayout(filter.DateFormat)
		if !ok {
			return nil, "", fmt.Errorf("不支持的日期格式: %s", filter.DateFormat)
		}
		parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), time.Local)
		if err != nil {
			return nil, "", fmt.Errorf("日期须符合 %s", filter.DateFormat)
		}
		normalized := parsed.Format(layout)
		switch valueType {
		case "datetime":
			parameter.ValueType, parameter.Format = "date", "yyyy-MM-dd HH:mm:ss"
			bound, _, err := officeParameterValue(parameter, parsed.Format("2006-01-02 15:04:05"))
			return bound, normalized, err
		case "number":
			parameter.ValueType = "decimal"
		}
		return officeParameterValue(parameter, normalized)
	}
	// Schemes saved before date format selection retain their original inputs.
	switch valueType {
	case "number":
		parameter.ValueType = "decimal"
	case "datetime":
		value = strings.Replace(strings.TrimSpace(value), "T", " ", 1)
		if len(value) == 16 {
			value += ":00"
		}
		parameter.ValueType, parameter.Format = "date", "yyyy-MM-dd HH:mm:ss"
		if len(value) == 10 {
			parameter.Format = "yyyy-MM-dd"
		}
	}
	return officeParameterValue(parameter, value)
}

func oracleExcelFormattedTime(code string, now time.Time) (time.Time, error) {
	switch code {
	case "CURRENT_MONTH", "PREVIOUS_MONTH", "NEXT_MONTH":
		start, _, err := oracleExcelMonthRange(code, now)
		return start, err
	case "CURRENT_YEAR":
		code = "YEAR_START"
	}
	raw, err := reporting.TimeDefaultValue(code, "str", "DATETIME", "YYYY-MM-DD HH:mm:ss", now)
	if err != nil {
		return time.Time{}, fmt.Errorf("动态时间来源无效")
	}
	value, err := reportdataset.TypedValue(raw, reportdataset.Column{LogicalType: "datetime"})
	if err != nil {
		return time.Time{}, err
	}
	return value.(time.Time), nil
}

func oracleExcelFormattedDateJSON(value time.Time, valueType, format string) json.RawMessage {
	if valueType == "datetime" && format != "" && !strings.Contains(format, "HH") {
		value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
	}
	if valueType != "string" {
		return oracleExcelFilterDateJSON(value, valueType)
	}
	layout, _ := oracleExcelFilterDateLayout(format)
	raw, _ := json.Marshal(value.Format(layout))
	return raw
}
