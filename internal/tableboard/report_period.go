package tableboard

import (
	"errors"
	"time"
)

func ValidateReportDate(date string) error {
	if date == "" {
		return nil
	}
	d, e := time.Parse(time.DateOnly, date)
	if e != nil || d.Year() < 2 {
		return errors.New("报表日期需要有效的 YYYY-MM-DD 日期")
	}
	return nil
}
func validPeriod(period string) bool {
	switch period {
	case "TODAY", "WEEK_TO_DATE", "MONTH_TO_DATE", "QUARTER_TO_DATE", "YEAR_TO_DATE", "LAST_MONTH", "REPORT_WEEK", "YEAR_TO_WEEK":
		return true
	}
	return false
}

// Excel WEEKNUM(date-1,1) moves the Sunday-based index to Monday.
// Jan 1 belongs to the reporting year's first (possibly partial) week.
func reportWeekOne(yearStart time.Time) time.Time {
	return yearStart.AddDate(0, 0, 1-int(yearStart.Weekday()))
}
func reportWeekNumber(day time.Time) int {
	first := time.Date(day.Year(), 1, 1, 0, 0, 0, 0, day.Location())
	days := day.YearDay() - 1 + int(first.Weekday()) - 1
	return 1 + max(0, days)/7
}

func (m Metric) ReportBounds(period string, asOf time.Time, reportDate string, yearOffset int) (time.Time, time.Time, error) {
	if !validPeriod(period) || yearOffset != 0 && yearOffset != -1 {
		return time.Time{}, time.Time{}, errors.New("统计期间或年度偏移无效")
	}
	if err := ValidateReportDate(reportDate); err != nil {
		return time.Time{}, time.Time{}, err
	}
	zone := m.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	clock := asOf.In(loc)
	if reportDate != "" {
		clock, err = time.ParseInLocation(time.DateOnly, reportDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	year, month, day := clock.Date()
	year += yearOffset
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	date := time.Date(year, month, min(day, lastDay), 0, 0, 0, 0, loc)
	start := date
	end := time.Date(year, month, date.Day(), clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(), loc)
	if reportDate != "" {
		end = date.AddDate(0, 0, 1)
	}
	jan := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	switch period {
	case "YEAR_TO_DATE":
		start = jan
	case "MONTH_TO_DATE", "LAST_MONTH":
		start = time.Date(year, month, 1, 0, 0, 0, 0, loc)
		if period == "LAST_MONTH" {
			end = start
			start = start.AddDate(0, -1, 0)
		}
	case "QUARTER_TO_DATE":
		start = time.Date(year, 1+3*((month-1)/3), 1, 0, 0, 0, 0, loc)
	case "WEEK_TO_DATE":
		start = date.AddDate(0, 0, -(int(date.Weekday())+6)%7)
		if start.Before(jan) {
			start = jan
		}
	case "REPORT_WEEK", "YEAR_TO_WEEK":
		week := reportWeekNumber(clock)
		start = reportWeekOne(jan).AddDate(0, 0, 7*(week-1))
		end = start.AddDate(0, 0, 7)
		next := jan.AddDate(1, 0, 0)
		if week == 1 || period == "YEAR_TO_WEEK" {
			start = jan
		}
		if start.After(next) {
			start = next
		}
		if end.After(next) {
			end = next
		}
	}
	return start, end, nil
}
