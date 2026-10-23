package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/internal/reporting"
	"time"
)

func reportTimeDefaultValue(source, valueType, control, format string, now time.Time) (json.RawMessage, error) {
	return reporting.TimeDefaultValue(source, valueType, control, format, now)
}
