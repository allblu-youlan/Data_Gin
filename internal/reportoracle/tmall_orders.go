package reportoracle

import (
	"fmt"
	"strings"
	"time"
)

// normalizeTmallOrdersTime uses database wall-clock times and a half-open range
// so callers can query adjacent realtime windows without duplicating boundaries.
func normalizeTmallOrdersTime(query TmallQuery) (TmallQuery, error) {
	if query.StartDate != "" || query.EndDate != "" || query.BatchNo != "" {
		return TmallQuery{}, fmt.Errorf("realtime orders only support startTime and endTime")
	}
	query.TimeField = strings.TrimSpace(query.TimeField)
	if query.TimeField == "" {
		query.TimeField = "created"
	}
	if query.TimeField != "created" && query.TimeField != "modified" {
		return TmallQuery{}, fmt.Errorf("timeField must be created or modified")
	}
	query.StartTime, query.EndTime = strings.TrimSpace(query.StartTime), strings.TrimSpace(query.EndTime)
	const layout = "2006-01-02 15:04:05"
	start, startErr := time.Parse(layout, query.StartTime)
	end, endErr := time.Parse(layout, query.EndTime)
	if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() < 1 || start.Format(layout) != query.StartTime || end.Format(layout) != query.EndTime || !end.After(start) || end.Sub(start) > 31*24*time.Hour {
		return TmallQuery{}, fmt.Errorf("startTime and endTime must define a positive range of at most 31 days in YYYY-MM-DD HH:mm:ss format")
	}
	return query, nil
}
