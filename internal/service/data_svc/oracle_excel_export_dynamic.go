package data_svc

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reporting"
	"gin-biz-web-api/internal/reportoracle"
	"github.com/godror/godror"
)

func oracleExcelDynamicFilter(filter OracleExcelExportFilter, column reportoracle.ResultColumn, now time.Time, index int) (string, []interface{}, error) {
	source := filter.ValueSource
	valueType := oracleExcelValueType(column.DataType)
	if valueType != "number" && valueType != "datetime" && filter.DateFormat == "" {
		return "", nil, fmt.Errorf("文本日期使用动态时间时须选择日期格式")
	}
	field := `"` + filter.Field + `"`
	bindName := fmt.Sprintf("filter_%d", index)
	bind := func(raw json.RawMessage, name string) (string, interface{}, error) {
		logicalType := "decimal"
		if valueType == "datetime" {
			logicalType = "datetime"
		} else if valueType == "string" {
			logicalType = "string"
		}
		value, err := reportdataset.TypedValue(raw, reportdataset.Column{LogicalType: logicalType})
		if err != nil {
			return "", nil, err
		}
		if number, ok := value.(json.Number); ok {
			value = godror.Number(number)
		}
		return ":" + name, sql.Named(name, value), nil
	}
	if source.Kind == "TIME_RANGE" {
		if filter.Op != "between" {
			return "", nil, fmt.Errorf("动态月份范围仅支持区间筛选")
		}
		start, end, err := oracleExcelMonthRange(source.Code, now)
		if err != nil {
			return "", nil, err
		}
		if valueType == "number" {
			end = end.AddDate(0, 0, -1)
		}
		lower, first, err := bind(oracleExcelFormattedDateJSON(start, valueType, filter.DateFormat), bindName+"_start")
		if err != nil {
			return "", nil, err
		}
		upper, last, err := bind(oracleExcelFormattedDateJSON(end, valueType, filter.DateFormat), bindName+"_end")
		if err != nil {
			return "", nil, err
		}
		if valueType == "number" {
			return field + " BETWEEN " + lower + " AND " + upper, []interface{}{first, last}, nil
		}
		return "(" + field + " >= " + lower + " AND " + field + " < " + upper + ")", []interface{}{first, last}, nil
	}
	operator := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[filter.Op]
	if source.Kind != "TIME" || operator == "" {
		return "", nil, fmt.Errorf("动态时间仅支持单值比较")
	}
	if filter.DateFormat != "" {
		resolved, err := oracleExcelFormattedTime(source.Code, now)
		if err != nil {
			return "", nil, err
		}
		if valueType != "number" && filter.Op == "lte" && (source.Code == "MONTH_END" || source.Code == "YEAR_END") {
			resolved, operator = resolved.AddDate(0, 0, 1), "<"
		}
		bound, argument, err := bind(oracleExcelFormattedDateJSON(resolved, valueType, filter.DateFormat), bindName)
		if err != nil {
			return "", nil, err
		}
		return field + " " + operator + " " + bound, []interface{}{argument}, nil
	}
	fieldType, control, format := "number", "NUMBER", ""
	if valueType == "datetime" {
		fieldType, control, format = "str", "DATETIME", "YYYY-MM-DD HH:mm:ss"
	}
	raw, err := reporting.TimeDefaultValue(source.Code, fieldType, control, format, now)
	if err != nil {
		return "", nil, fmt.Errorf("动态时间来源无效")
	}
	if valueType == "datetime" {
		switch source.Code {
		case "CURRENT_MONTH", "PREVIOUS_MONTH", "NEXT_MONTH":
			start, _, err := oracleExcelMonthRange(source.Code, now)
			if err != nil {
				return "", nil, err
			}
			raw = oracleExcelFilterDateJSON(start, valueType)
		case "CURRENT_YEAR":
			raw, err = reporting.TimeDefaultValue("YEAR_START", fieldType, control, format, now)
		}
		if err != nil {
			return "", nil, err
		}
		if filter.Op == "lte" && (source.Code == "MONTH_END" || source.Code == "YEAR_END") {
			resolved, err := reportdataset.TypedValue(raw, reportdataset.Column{LogicalType: "datetime"})
			if err != nil {
				return "", nil, err
			}
			raw = oracleExcelFilterDateJSON(resolved.(time.Time).AddDate(0, 0, 1), valueType)
			operator = "<"
		}
	}
	bound, argument, err := bind(raw, bindName)
	if err != nil {
		return "", nil, err
	}
	return field + " " + operator + " " + bound, []interface{}{argument}, nil
}

func oracleExcelMonthRange(code string, now time.Time) (time.Time, time.Time, error) {
	offset := 0
	switch code {
	case "CURRENT_MONTH":
	case "PREVIOUS_MONTH":
		offset = -1
	case "NEXT_MONTH":
		offset = 1
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("动态月份范围来源无效")
	}
	raw, err := reporting.TimeDefaultValue("MONTH_START", "str", "DATE", "YYYY-MM-DD", now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	resolved, err := reportdataset.TypedValue(raw, reportdataset.Column{LogicalType: "date"})
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := resolved.(time.Time).AddDate(0, offset, 0)
	return start, start.AddDate(0, 1, 0), nil
}

func oracleExcelFilterDateJSON(value time.Time, valueType string) json.RawMessage {
	text := value.Format(time.RFC3339)
	if valueType == "number" {
		text = value.Format("20060102")
	}
	raw, _ := json.Marshal(text)
	return raw
}

func oracleExcelCanFilterMaximum(dataType string) bool {
	switch strings.ToUpper(strings.TrimSpace(dataType)) {
	case "CLOB", "NCLOB", "BLOB", "LONG", "LONG RAW", "RAW":
		return false
	default:
		return true
	}
}

func oracleExcelMaximumQuery(quoted []string, field, from string) string {
	inner, outer := make([]string, 0, len(quoted)+2), make([]string, 0, len(quoted))
	for index, column := range quoted {
		alias := fmt.Sprintf(`"EXCEL_VALUE_%03d"`, index+1)
		inner = append(inner, column+" AS "+alias)
		outer = append(outer, alias+" AS "+column)
	}
	inner = append(inner, field+` AS "EXCEL_MAX_VALUE"`, `MAX(`+field+`) OVER () AS "EXCEL_MAXIMUM"`)
	return "SELECT " + strings.Join(outer, ", ") + " FROM (SELECT " + strings.Join(inner, ", ") + from + `) WHERE "EXCEL_MAX_VALUE" = "EXCEL_MAXIMUM"`
}
