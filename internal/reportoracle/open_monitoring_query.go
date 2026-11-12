package reportoracle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	monitoringHourlyRowLimit = 31 * 24
	monitoringAreaRowLimit   = 1000

	monitoringHourlySQL = `SELECT TO_CHAR(HOUR_START, 'YYYY-MM-DD HH24:MI:SS'),
       PASSBY, TRAFFIC, TRAFFIC_COUNT, TRAFFIC_BATCH,
       NVL(CAPTURE_RATE, 0), NVL(AVG_VISIT_DURATION, 0),
       NVL(NEW_VISITORS_RATIO, 0), NVL(DEEP_VISIT_RATIO, 0),
       NVL(NET_SALES, 0), NVL(SALES_TRANSACTIONS, 0),
       NVL(ATV, 0), NVL(SHOPPER_YIELD, 0),
       TO_CHAR(FETCHED_AT, 'YYYY-MM-DD HH24:MI:SS')
FROM YLZ_USER.KAIYU_PASSENGER_HOURLY
WHERE SHOP_ID = :1 AND MALL_ID = :2`

	monitoringAreaSQL = `SELECT REGION_NAME, EXPOSURE_COUNT, STAY_COUNT, STAY_RATE, AVG_STAY_TIME,
       TO_CHAR(FETCHED_AT, 'YYYY-MM-DD HH24:MI:SS')
FROM YLZ_USER.KAIYU_REGION_STAY_DAILY
WHERE SHOP_ID = :1 AND MALL_ID = :2
  AND STAT_DATE >= TO_DATE(:3, 'YYYY-MM-DD')
  AND STAT_DATE < TO_DATE(:4, 'YYYY-MM-DD')`
)

// Oracle stores these timestamps as Shanghai wall-clock values without a timezone.
var monitoringLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// ErrMonitoringTooManyRows indicates that a monitoring result exceeds its bounded response size.
var ErrMonitoringTooManyRows = errors.New("monitoring query returned too many rows")

// MonitoringHourlyRow contains one hour of monitoring metrics in Shanghai time.
type MonitoringHourlyRow struct {
	HourStart         time.Time `json:"hourStart"`
	Passby            int64     `json:"passby"`
	Traffic           int64     `json:"traffic"`
	TrafficCount      int64     `json:"trafficCount"`
	TrafficBatch      int64     `json:"trafficBatch"`
	CaptureRate       float64   `json:"captureRate"`
	AvgVisitDuration  float64   `json:"avgVisitDuration"`
	NewVisitorsRatio  float64   `json:"newVisitorsRatio"`
	DeepVisitRatio    float64   `json:"deepVisitRatio"`
	NetSales          float64   `json:"netSales"`
	SalesTransactions float64   `json:"salesTransactions"`
	ATV               float64   `json:"atv"`
	ShopperYield      float64   `json:"shopperYield"`
	FetchedAt         time.Time `json:"fetchedAt"`
}

// MonitoringAreaRow contains one region's daily monitoring metrics.
// StayRate is a ratio (0..1), and AvgStayTime is measured in seconds.
type MonitoringAreaRow struct {
	RegionName    string    `json:"regionName"`
	ExposureCount int64     `json:"exposureCount"`
	StayCount     int64     `json:"stayCount"`
	StayRate      float64   `json:"stayRate"`
	AvgStayTime   float64   `json:"avgStayTime"`
	FetchedAt     time.Time `json:"fetchedAt"`
}

// QueryMonitoringHours reads the requested Shanghai calendar dates in hour order.
// Missing hours are omitted; the query does not synthesize zero-valued metrics.
func (adapter *Adapter) QueryMonitoringHours(ctx context.Context, shopID string, mallID uint, dates []time.Time) ([]MonitoringHourlyRow, error) {
	shopID = strings.TrimSpace(shopID)
	if err := adapter.validateMonitoringQuery(ctx, shopID, mallID); err != nil {
		return nil, err
	}
	if len(dates) < 1 || len(dates) > 31 {
		return nil, fmt.Errorf("query monitoring hours: expected 1 to 31 dates")
	}
	days := make([]time.Time, 0, len(dates))
	for _, date := range dates {
		if date.IsZero() {
			return nil, fmt.Errorf("query monitoring hours: date is required")
		}
		days = append(days, monitoringDay(date))
	}
	slices.SortFunc(days, time.Time.Compare)
	days = slices.CompactFunc(days, time.Time.Equal)

	var statement strings.Builder
	statement.WriteString(monitoringHourlySQL)
	statement.WriteString("\n  AND (")
	arguments := make([]any, 0, 2+len(days)*2)
	arguments = append(arguments, shopID, int64(mallID))
	for i, day := range days {
		if i > 0 {
			statement.WriteString(" OR ")
		}
		fmt.Fprintf(&statement,
			"(HOUR_START >= TO_TIMESTAMP(:%d, 'YYYY-MM-DD HH24:MI:SS') AND HOUR_START < TO_TIMESTAMP(:%d, 'YYYY-MM-DD HH24:MI:SS'))",
			len(arguments)+1, len(arguments)+2)
		arguments = append(arguments, day.Format(time.DateTime), day.AddDate(0, 0, 1).Format(time.DateTime))
	}
	fmt.Fprintf(&statement, ")\n  AND ROWNUM <= %d\nORDER BY HOUR_START", monitoringHourlyRowLimit+1)

	rows, err := adapter.db.QueryContext(ctx, statement.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("query monitoring hours: %w", mapExecutionError(ctx, err))
	}
	defer rows.Close()

	result := make([]MonitoringHourlyRow, 0, len(days)*24)
	for rows.Next() {
		if len(result) >= monitoringHourlyRowLimit {
			return nil, ErrMonitoringTooManyRows
		}
		var row MonitoringHourlyRow
		var hourStart, fetchedAt string
		if err := rows.Scan(&hourStart, &row.Passby, &row.Traffic, &row.TrafficCount, &row.TrafficBatch,
			&row.CaptureRate, &row.AvgVisitDuration, &row.NewVisitorsRatio, &row.DeepVisitRatio,
			&row.NetSales, &row.SalesTransactions, &row.ATV, &row.ShopperYield, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan monitoring hour: %w", err)
		}
		row.HourStart, err = time.ParseInLocation(time.DateTime, hourStart, monitoringLocation)
		if err != nil {
			return nil, fmt.Errorf("parse monitoring hour: %w", err)
		}
		row.FetchedAt, err = time.ParseInLocation(time.DateTime, fetchedAt, monitoringLocation)
		if err != nil {
			return nil, fmt.Errorf("parse monitoring hourly fetch time: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring hours: %w", mapExecutionError(ctx, err))
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close monitoring hours: %w", err)
	}
	return result, nil
}

// QueryMonitoringAreas reads one Shanghai calendar date, optionally filtering by exact region name.
func (adapter *Adapter) QueryMonitoringAreas(ctx context.Context, shopID string, mallID uint, date time.Time, regionName string) ([]MonitoringAreaRow, error) {
	shopID = strings.TrimSpace(shopID)
	regionName = strings.TrimSpace(regionName)
	if err := adapter.validateMonitoringQuery(ctx, shopID, mallID); err != nil {
		return nil, err
	}
	if date.IsZero() || utf8.RuneCountInString(regionName) > 255 {
		return nil, fmt.Errorf("query monitoring areas: invalid date or region name")
	}
	day := monitoringDay(date)
	arguments := []any{shopID, int64(mallID), day.Format(time.DateOnly), day.AddDate(0, 0, 1).Format(time.DateOnly)}
	statement := monitoringAreaSQL
	if regionName != "" {
		statement += "\n  AND REGION_NAME = :5"
		arguments = append(arguments, regionName)
	}
	statement += fmt.Sprintf("\n  AND ROWNUM <= %d\nORDER BY REGION_NAME", monitoringAreaRowLimit+1)

	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query monitoring areas: %w", mapExecutionError(ctx, err))
	}
	defer rows.Close()

	result := make([]MonitoringAreaRow, 0)
	for rows.Next() {
		if len(result) >= monitoringAreaRowLimit {
			return nil, ErrMonitoringTooManyRows
		}
		var row MonitoringAreaRow
		var fetchedAt string
		if err := rows.Scan(&row.RegionName, &row.ExposureCount, &row.StayCount,
			&row.StayRate, &row.AvgStayTime, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan monitoring area: %w", err)
		}
		row.FetchedAt, err = time.ParseInLocation(time.DateTime, fetchedAt, monitoringLocation)
		if err != nil {
			return nil, fmt.Errorf("parse monitoring area fetch time: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring areas: %w", mapExecutionError(ctx, err))
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close monitoring areas: %w", err)
	}
	return result, nil
}

func (adapter *Adapter) validateMonitoringQuery(ctx context.Context, shopID string, mallID uint) error {
	if adapter == nil || adapter.db == nil {
		return fmt.Errorf("query monitoring: adapter is closed")
	}
	if ctx == nil || shopID == "" || utf8.RuneCountInString(shopID) > 64 || mallID == 0 {
		return fmt.Errorf("query monitoring: invalid context or store binding")
	}
	return ctx.Err()
}

func monitoringDay(date time.Time) time.Time {
	date = date.In(monitoringLocation)
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, monitoringLocation)
}
