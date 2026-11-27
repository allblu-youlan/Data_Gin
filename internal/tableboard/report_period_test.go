package tableboard

import (
	"testing"
	"time"
)

func TestGMVReportPeriods(t *testing.T) {
	const sh = "+08:00"
	asOf := time.Date(2026, 11, 27, 4, 5, 0, 0, time.UTC)
	tests := []struct {
		period, date string
		offset       int
		start, end   string
	}{
		{"TODAY", "", 0, "2026-11-27T00:00:00", "2026-11-27T12:05:00"},
		{"YEAR_TO_DATE", "2026-11-15", 0, "2026-01-01T00:00:00", "2026-11-16T00:00:00"},
		{"REPORT_WEEK", "2026-11-15", -1, "2025-11-10T00:00:00", "2025-11-17T00:00:00"},
		{"YEAR_TO_WEEK", "2026-11-15", -1, "2025-01-01T00:00:00", "2025-11-17T00:00:00"},
		{"WEEK_TO_DATE", "2026-11-12", 0, "2026-11-09T00:00:00", "2026-11-13T00:00:00"},
		{"QUARTER_TO_DATE", "2026-11-15", 0, "2026-10-01T00:00:00", "2026-11-16T00:00:00"},
		{"TODAY", "2024-02-29", -1, "2023-02-28T00:00:00", "2023-03-01T00:00:00"},
		{"REPORT_WEEK", "2026-01-01", 0, "2026-01-01T00:00:00", "2026-01-05T00:00:00"},
		{"REPORT_WEEK", "2026-12-31", 0, "2026-12-28T00:00:00", "2027-01-01T00:00:00"},
		{"WEEK_TO_DATE", "2026-01-02", 0, "2026-01-01T00:00:00", "2026-01-03T00:00:00"},
	}
	for _, tc := range tests {
		start, end, e := (Metric{}).ReportBounds(tc.period, asOf, tc.date, tc.offset)
		if e != nil || start.Format(time.RFC3339) != tc.start+sh || end.Format(time.RFC3339) != tc.end+sh {
			t.Errorf("%+v => %v %v %v", tc, start, end, e)
		}
	}
	if ValidateReportDate("2026-02-29") == nil {
		t.Fatal("invalid calendar date accepted")
	}
}
