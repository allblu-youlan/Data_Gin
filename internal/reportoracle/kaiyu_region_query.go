package reportoracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"
)

const KaiyuRegionMaxRows = 100_000

var ErrKaiyuRegionTooManyRows = errors.New("kaiyu region: too many rows")

// QueryKaiyuRegionDays reads [start,end) Shanghai calendar dates. mallIDs must
// already be authorized; an empty list explicitly represents all malls.
func (adapter *Adapter) QueryKaiyuRegionDays(ctx context.Context, start, end time.Time, mallIDs []uint) ([]model.KaiyuRegionStayDaily, error) {
	where, args, err := kaiyuRegionDateScope(start, end, mallIDs)
	if err != nil {
		return nil, err
	}
	query := `SELECT SHOP_ID,TO_CHAR(STAT_DATE,'YYYY-MM-DD'),MALL_ID,SHOP_NAME,REGION_NAME,
 EXPOSURE_COUNT,STAY_COUNT,STAY_RATE,AVG_STAY_TIME,TO_CHAR(FETCHED_AT,'YYYY-MM-DD HH24:MI:SS')
FROM YLZ_USER.KAIYU_REGION_STAY_DAILY` + where + " ORDER BY STAT_DATE,MALL_ID,SHOP_ID,REGION_NAME FETCH FIRST 100001 ROWS ONLY"
	rows, err := adapter.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query Kaiyu region days: %w", err)
	}
	defer rows.Close()
	result := make([]model.KaiyuRegionStayDaily, 0)
	for rows.Next() {
		if len(result) >= KaiyuRegionMaxRows {
			return nil, ErrKaiyuRegionTooManyRows
		}
		var row model.KaiyuRegionStayDaily
		var day, fetched string
		if err := rows.Scan(&row.ShopID, &day, &row.MallID, &row.ShopName, &row.RegionName, &row.ExposureCount, &row.StayCount, &row.StayRate, &row.AvgStayTime, &fetched); err != nil {
			return nil, fmt.Errorf("scan Kaiyu region day: %w", err)
		}
		row.StatDate, err = time.ParseInLocation(time.DateOnly, day, kaiyuRegionLocation)
		if err != nil {
			return nil, fmt.Errorf("parse Kaiyu region date: %w", err)
		}
		row.FetchedAt, err = time.ParseInLocation(time.DateTime, fetched, kaiyuRegionLocation)
		if err != nil {
			return nil, fmt.Errorf("parse Kaiyu region fetch time: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Kaiyu region days: %w", err)
	}
	return result, nil
}

// QueryKaiyuRegionSyncDays includes empty and failed attempts independently of
// retained metrics, so missing-source days are never presented as zero traffic.
func (adapter *Adapter) QueryKaiyuRegionSyncDays(ctx context.Context, start, end time.Time, mallIDs []uint) ([]model.KaiyuRegionSyncDay, error) {
	where, args, err := kaiyuRegionDateScope(start, end, mallIDs)
	if err != nil {
		return nil, err
	}
	query := `SELECT SHOP_ID,TO_CHAR(STAT_DATE,'YYYY-MM-DD'),MALL_ID,SHOP_NAME,SYNC_STATE,RECORD_COUNT,
 TO_CHAR(FETCHED_AT,'YYYY-MM-DD HH24:MI:SS'),TO_CHAR(LAST_SUCCESS_AT,'YYYY-MM-DD HH24:MI:SS'),ERROR_MESSAGE
FROM YLZ_USER.KAIYU_REGION_SYNC_DAY` + where + " ORDER BY STAT_DATE,MALL_ID,SHOP_ID FETCH FIRST 100001 ROWS ONLY"
	rows, err := adapter.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query Kaiyu region sync days: %w", err)
	}
	defer rows.Close()
	result := make([]model.KaiyuRegionSyncDay, 0)
	for rows.Next() {
		if len(result) >= KaiyuRegionMaxRows {
			return nil, ErrKaiyuRegionTooManyRows
		}
		var row model.KaiyuRegionSyncDay
		var day, fetched string
		var lastSuccess, message sql.NullString
		if err := rows.Scan(&row.ShopID, &day, &row.MallID, &row.ShopName, &row.State, &row.RecordCount, &fetched, &lastSuccess, &message); err != nil {
			return nil, fmt.Errorf("scan Kaiyu region sync day: %w", err)
		}
		row.StatDate, err = time.ParseInLocation(time.DateOnly, day, kaiyuRegionLocation)
		if err != nil {
			return nil, fmt.Errorf("parse Kaiyu region sync date: %w", err)
		}
		row.FetchedAt, err = time.ParseInLocation(time.DateTime, fetched, kaiyuRegionLocation)
		if err != nil {
			return nil, fmt.Errorf("parse Kaiyu region sync time: %w", err)
		}
		if lastSuccess.Valid {
			parsed, err := time.ParseInLocation(time.DateTime, lastSuccess.String, kaiyuRegionLocation)
			if err != nil {
				return nil, fmt.Errorf("parse Kaiyu region last success: %w", err)
			}
			row.LastSuccessAt = &parsed
		}
		row.ErrorMessage = message.String
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Kaiyu region sync days: %w", err)
	}
	return result, nil
}

func kaiyuRegionDateScope(start, end time.Time, mallIDs []uint) (string, []any, error) {
	if start.IsZero() || end.IsZero() {
		return "", nil, fmt.Errorf("invalid Kaiyu region date range")
	}
	startText, endText := start.In(kaiyuRegionLocation).Format(time.DateOnly), end.In(kaiyuRegionLocation).Format(time.DateOnly)
	if endText <= startText {
		return "", nil, fmt.Errorf("invalid Kaiyu region date range")
	}
	args := []any{startText, endText}
	where := " WHERE STAT_DATE>=TO_DATE(:1,'YYYY-MM-DD') AND STAT_DATE<TO_DATE(:2,'YYYY-MM-DD')"
	if len(mallIDs) > 0 {
		groups := make([]string, 0, (len(mallIDs)+999)/1000)
		for offset := 0; offset < len(mallIDs); offset += 1000 {
			placeholders := make([]string, 0, min(1000, len(mallIDs)-offset))
			for _, id := range mallIDs[offset:min(offset+1000, len(mallIDs))] {
				if id == 0 {
					return "", nil, fmt.Errorf("invalid Kaiyu region mall id")
				}
				args = append(args, id)
				placeholders = append(placeholders, fmt.Sprintf(":%d", len(args)))
			}
			groups = append(groups, "MALL_ID IN ("+strings.Join(placeholders, ",")+")")
		}
		where += " AND (" + strings.Join(groups, " OR ") + ")"
	}
	return where, args, nil
}
