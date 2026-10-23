package reporting

import (
	"encoding/json"
	"time"
)

var reportInputTimezone = time.FixedZone("Asia/Shanghai", 8*60*60)

// Dynamic defaults are resolved once when a run is created, then stored with
// the run so retries and exports keep the original query period.
func TimeDefaultValue(source, valueType, control, format string, now time.Time) (json.RawMessage, error) {
	if (valueType != "str" && valueType != "number") || control == "SELECT" || control == "CHECKBOX" {
		return nil, ErrInvalidParameterInput
	}
	now = now.In(reportInputTimezone)
	value := now
	layout := "20060102"
	switch source {
	case "CURRENT_MONTH", "PREVIOUS_MONTH", "NEXT_MONTH":
		value = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, reportInputTimezone)
		if source == "PREVIOUS_MONTH" {
			value = value.AddDate(0, -1, 0)
		} else if source == "NEXT_MONTH" {
			value = value.AddDate(0, 1, 0)
		}
		layout = "200601"
	case "CURRENT_YEAR":
		layout = "2006"
	case "TODAY", "YESTERDAY", "MONTH_START", "MONTH_END", "YEAR_START", "YEAR_END", "NOW":
		switch source {
		case "YESTERDAY":
			value = now.AddDate(0, 0, -1)
		case "MONTH_START":
			value = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, reportInputTimezone)
		case "MONTH_END":
			value = time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, reportInputTimezone)
		case "YEAR_START":
			value = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, reportInputTimezone)
		case "YEAR_END":
			value = time.Date(now.Year(), 12, 31, 0, 0, 0, 0, reportInputTimezone)
		}
		if control == "DATE" && format == "YYYY-MM-DD" {
			layout = "2006-01-02"
		} else if control == "DATETIME" {
			if source != "NOW" {
				value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, reportInputTimezone)
			}
			switch format {
			case "YYYYMMDDHHmmss":
				layout = "20060102150405"
			case "YYYY-MM-DD HH:mm:ss":
				layout = "2006-01-02 15:04:05"
			default:
				layout = "2006-01-02T15:04:05"
			}
		} else if source == "NOW" {
			layout = "20060102150405"
		}
	default:
		return nil, ErrInvalidParameterInput
	}
	text := value.Format(layout)
	if valueType == "number" {
		return json.RawMessage(text), nil
	}
	return json.Marshal(text)
}
