package reportdataset

import (
	"encoding/json"
	"time"

	"gin-biz-web-api/internal/reporting"
)

func containsColumnMax(condition *Condition) bool {
	if condition == nil {
		return false
	}
	if condition.Value != nil && condition.Value.Kind == "COLUMN_MAX" {
		return true
	}
	for index := range condition.Items {
		if containsColumnMax(&condition.Items[index]) {
			return true
		}
	}
	return false
}

func (b *sqlBuilder) timeCondition(expression string, column Column, operator string, value Value) (string, error) {
	if !numeric(column.LogicalType) && column.LogicalType != "date" && column.LogicalType != "datetime" {
		return "", queryError("动态时间仅支持数值日期、DATE 和 DATETIME 字段")
	}
	if len(value.Value) != 0 || value.AliasID != "" || value.ColumnID != "" {
		return "", queryError("动态时间来源不能包含常量或字段引用")
	}
	if value.Kind == "TIME_RANGE" {
		if operator != "BETWEEN" {
			return "", queryError("动态月份范围仅支持 BETWEEN 比较")
		}
		start, end, err := reportMonthRange(value.Code, b.now)
		if err != nil {
			return "", err
		}
		if numeric(column.LogicalType) {
			lower, err := b.bind(reportFilterTimeJSON(start, true), column)
			if err != nil {
				return "", err
			}
			upper, err := b.bind(reportFilterTimeJSON(end.AddDate(0, 0, -1), true), column)
			if err != nil {
				return "", err
			}
			return expression + " BETWEEN " + lower + " AND " + upper, nil
		}
		lower, err := b.bind(reportFilterTimeJSON(start, false), column)
		if err != nil {
			return "", err
		}
		upper, err := b.bind(reportFilterTimeJSON(end, false), column)
		if err != nil {
			return "", err
		}
		// A half-open range includes the entire final day, including timestamps.
		return "(" + expression + " >= " + lower + " AND " + expression + " < " + upper + ")", nil
	}
	if !contains([]string{"EQ", "NE", "GT", "GTE", "LT", "LTE"}, operator) {
		return "", queryError("动态时间单值仅支持单值比较")
	}
	valueType, control, format := "number", "NUMBER", ""
	if column.LogicalType == "date" {
		valueType, control, format = "str", "DATE", "YYYY-MM-DD"
	} else if column.LogicalType == "datetime" {
		valueType, control, format = "str", "DATETIME", "YYYY-MM-DD HH:mm:ss"
	}
	raw, err := reporting.TimeDefaultValue(value.Code, valueType, control, format, b.now)
	if err != nil {
		return "", queryError("动态时间来源无效")
	}
	if !numeric(column.LogicalType) {
		switch value.Code {
		case "CURRENT_MONTH", "PREVIOUS_MONTH", "NEXT_MONTH":
			start, _, err := reportMonthRange(value.Code, b.now)
			if err != nil {
				return "", err
			}
			raw = reportFilterTimeJSON(start, false)
		case "CURRENT_YEAR":
			raw, err = reporting.TimeDefaultValue("YEAR_START", valueType, control, format, b.now)
		}
		if err != nil {
			return "", queryError("动态时间来源无效")
		}
		if operator == "LTE" && (value.Code == "MONTH_END" || value.Code == "YEAR_END") {
			resolved, err := TypedValue(raw, column)
			if err != nil {
				return "", err
			}
			raw = reportFilterTimeJSON(resolved.(time.Time).AddDate(0, 0, 1), false)
			operator = "LT"
		}
	}
	bound, err := b.bind(raw, column)
	if err != nil {
		return "", err
	}
	return expression + " " + binaryOperator(operator) + " " + bound, nil
}

func reportMonthRange(code string, now time.Time) (time.Time, time.Time, error) {
	offset := 0
	switch code {
	case "CURRENT_MONTH":
	case "PREVIOUS_MONTH":
		offset = -1
	case "NEXT_MONTH":
		offset = 1
	default:
		return time.Time{}, time.Time{}, queryError("动态月份范围来源无效")
	}
	raw, err := reporting.TimeDefaultValue("MONTH_START", "str", "DATE", "YYYY-MM-DD", now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	resolved, err := TypedValue(raw, Column{LogicalType: "date"})
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := resolved.(time.Time).AddDate(0, offset, 0)
	return start, start.AddDate(0, 1, 0), nil
}

func reportFilterTimeJSON(value time.Time, numeric bool) json.RawMessage {
	text := value.Format(time.RFC3339Nano)
	if numeric {
		text = value.Format("20060102")
	}
	raw, _ := json.Marshal(text)
	return raw
}
