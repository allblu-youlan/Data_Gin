package reportoracle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"
)

const KaiyuPassengerMaxRows = 100_000

var ErrKaiyuPassengerTooManyRows = errors.New("kaiyu passenger: too many rows")

// QueryKaiyuPassengerHours reads the half-open Shanghai wall-clock interval.
// mallIDs must already be constrained by the account's mall scope. Empty means ALL.
func (adapter *Adapter) QueryKaiyuPassengerHours(ctx context.Context, start, end time.Time, mallIDs []uint) ([]model.KaiyuPassengerHourly, error) {
	if ctx == nil || start.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("invalid Kaiyu passenger query")
	}
	location := time.FixedZone("Asia/Shanghai", 8*3600)
	args := []any{start.In(location).Format(time.DateTime), end.In(location).Format(time.DateTime)}
	statement := `SELECT SHOP_ID, TO_CHAR(HOUR_START, 'YYYY-MM-DD HH24:MI:SS'), MALL_ID, SHOP_NAME,
 PASSBY, TRAFFIC, TRAFFIC_COUNT, TRAFFIC_BATCH, NVL(CAPTURE_RATE,0), NVL(AVG_VISIT_DURATION,0),
 NVL(NEW_VISITORS_RATIO,0), NVL(DEEP_VISIT_RATIO,0), NVL(NET_SALES,0), NVL(SALES_TRANSACTIONS,0),
 NVL(ATV,0), NVL(SHOPPER_YIELD,0)
FROM YLZ_USER.KAIYU_PASSENGER_HOURLY
WHERE HOUR_START >= TO_TIMESTAMP(:1, 'YYYY-MM-DD HH24:MI:SS')
 AND HOUR_START < TO_TIMESTAMP(:2, 'YYYY-MM-DD HH24:MI:SS')`
	if len(mallIDs) > 0 {
		groups := make([]string, 0, (len(mallIDs)+999)/1000)
		for offset := 0; offset < len(mallIDs); offset += 1000 {
			placeholders := make([]string, 0, min(1000, len(mallIDs)-offset))
			for _, id := range mallIDs[offset:min(offset+1000, len(mallIDs))] {
				if id == 0 {
					return nil, fmt.Errorf("invalid Kaiyu passenger mall id")
				}
				args = append(args, id)
				placeholders = append(placeholders, fmt.Sprintf(":%d", len(args)))
			}
			groups = append(groups, "MALL_ID IN ("+strings.Join(placeholders, ",")+")")
		}
		statement += " AND (" + strings.Join(groups, " OR ") + ")"
	}
	statement += " ORDER BY HOUR_START, MALL_ID, SHOP_ID"
	rows, err := adapter.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query Kaiyu passenger hours: %w", err)
	}
	defer rows.Close()
	result := make([]model.KaiyuPassengerHourly, 0)
	for rows.Next() {
		if len(result) >= KaiyuPassengerMaxRows {
			return nil, ErrKaiyuPassengerTooManyRows
		}
		var row model.KaiyuPassengerHourly
		var hour string
		if err := rows.Scan(&row.ShopID, &hour, &row.MallID, &row.ShopName, &row.Passby, &row.Traffic, &row.TrafficCount, &row.TrafficBatch, &row.CaptureRate, &row.AvgVisitDuration, &row.NewVisitorsRatio, &row.DeepVisitRatio, &row.NetSales, &row.SalesTransactions, &row.ATV, &row.ShopperYield); err != nil {
			return nil, fmt.Errorf("scan Kaiyu passenger hour: %w", err)
		}
		row.HourStart, err = time.ParseInLocation(time.DateTime, hour, location)
		if err != nil {
			return nil, fmt.Errorf("parse Kaiyu passenger hour: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Kaiyu passenger hours: %w", err)
	}
	return result, nil
}
