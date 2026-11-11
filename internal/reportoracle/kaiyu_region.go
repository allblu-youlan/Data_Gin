package reportoracle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/etc"
	"gin-biz-web-api/model"
)

var kaiyuRegionLocation = time.FixedZone("Asia/Shanghai", 8*3600)

// InitializeKaiyuRegionStore explicitly installs the region stores. Scheduled
// requests never perform DDL; rerunning this command validates existing tables.
func (adapter *Adapter) InitializeKaiyuRegionStore(ctx context.Context) error {
	var owner string
	if err := adapter.db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&owner); err != nil {
		return fmt.Errorf("check Kaiyu region storage owner: %w", err)
	}
	if !strings.EqualFold(owner, "YLZ_USER") {
		return fmt.Errorf("开域区域存储必须使用 YLZ_USER 用户，当前为 %s", owner)
	}
	for _, table := range []struct{ name, create, columns string }{
		{"KAIYU_REGION_STAY_DAILY", etc.OracleKaiyuRegionStaySQL, "SHOP_ID,STAT_DATE,REGION_NAME,MALL_ID,SHOP_NAME,EXPOSURE_COUNT,STAY_COUNT,STAY_RATE,AVG_STAY_TIME,FETCHED_AT"},
		{"KAIYU_REGION_SYNC_DAY", etc.OracleKaiyuRegionSyncSQL, "SHOP_ID,STAT_DATE,MALL_ID,SHOP_NAME,SYNC_STATE,RECORD_COUNT,FETCHED_AT,LAST_SUCCESS_AT,ERROR_MESSAGE"},
	} {
		var count int
		if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = :1", table.name).Scan(&count); err != nil {
			return fmt.Errorf("check Kaiyu region table: %w", err)
		}
		if count == 0 {
			if _, err := adapter.db.ExecContext(ctx, strings.TrimSpace(table.create)); err != nil {
				var coded interface{ Code() int }
				if !errors.As(err, &coded) || coded.Code() != 955 {
					return fmt.Errorf("create Kaiyu region table %s: %w", table.name, err)
				}
			}
		}
		rows, err := adapter.db.QueryContext(ctx, "SELECT "+table.columns+" FROM YLZ_USER."+table.name+" WHERE 1=0")
		if err != nil {
			return fmt.Errorf("validate Kaiyu region table %s: %w", table.name, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close Kaiyu region table validation: %w", err)
		}
	}
	for _, index := range []struct{ name, table string }{
		{"IX_KAIYU_REGION_DATE_MALL", "KAIYU_REGION_STAY_DAILY"},
		{"IX_KAIYU_REGION_SYNC_DATE_MALL", "KAIYU_REGION_SYNC_DAY"},
	} {
		var count int
		if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_INDEXES WHERE INDEX_NAME = :1", index.name).Scan(&count); err != nil {
			return fmt.Errorf("check Kaiyu region date index: %w", err)
		}
		if count == 0 {
			query := "CREATE INDEX YLZ_USER." + index.name + " ON YLZ_USER." + index.table + " (STAT_DATE,MALL_ID)"
			if _, err := adapter.db.ExecContext(ctx, query); err != nil {
				var coded interface{ Code() int }
				if !errors.As(err, &coded) || coded.Code() != 955 {
					return fmt.Errorf("create Kaiyu region date index: %w", err)
				}
			}
		}
	}
	return nil
}

const kaiyuRegionStateMerge = `MERGE INTO YLZ_USER.KAIYU_REGION_SYNC_DAY t
USING (SELECT :1 SHOP_ID, TO_DATE(:2,'YYYY-MM-DD') STAT_DATE, :3 MALL_ID, :4 SHOP_NAME,
 :5 SYNC_STATE, :6 RECORD_COUNT, TO_TIMESTAMP(:7,'YYYY-MM-DD HH24:MI:SS') FETCHED_AT, :8 ERROR_MESSAGE FROM DUAL) s
ON (t.SHOP_ID=s.SHOP_ID AND t.STAT_DATE=s.STAT_DATE)
WHEN MATCHED THEN UPDATE SET t.MALL_ID=s.MALL_ID,t.SHOP_NAME=s.SHOP_NAME,t.SYNC_STATE=s.SYNC_STATE,
 t.RECORD_COUNT=s.RECORD_COUNT,t.FETCHED_AT=s.FETCHED_AT,t.ERROR_MESSAGE=s.ERROR_MESSAGE,
 t.LAST_SUCCESS_AT=CASE WHEN s.SYNC_STATE='success' THEN s.FETCHED_AT ELSE t.LAST_SUCCESS_AT END
WHEN NOT MATCHED THEN INSERT (SHOP_ID,STAT_DATE,MALL_ID,SHOP_NAME,SYNC_STATE,RECORD_COUNT,FETCHED_AT,LAST_SUCCESS_AT,ERROR_MESSAGE)
 VALUES(s.SHOP_ID,s.STAT_DATE,s.MALL_ID,s.SHOP_NAME,s.SYNC_STATE,s.RECORD_COUNT,s.FETCHED_AT,
 CASE WHEN s.SYNC_STATE='success' THEN s.FETCHED_AT ELSE NULL END,s.ERROR_MESSAGE)`

const kaiyuRegionFactMerge = `MERGE INTO YLZ_USER.KAIYU_REGION_STAY_DAILY t
USING (SELECT :1 SHOP_ID, TO_DATE(:2,'YYYY-MM-DD') STAT_DATE, :3 REGION_NAME, :4 MALL_ID, :5 SHOP_NAME,
 :6 EXPOSURE_COUNT, :7 STAY_COUNT, :8 STAY_RATE, :9 AVG_STAY_TIME, TO_TIMESTAMP(:10,'YYYY-MM-DD HH24:MI:SS') FETCHED_AT FROM DUAL) s
ON (t.SHOP_ID=s.SHOP_ID AND t.STAT_DATE=s.STAT_DATE AND t.REGION_NAME=s.REGION_NAME)
WHEN MATCHED THEN UPDATE SET t.MALL_ID=s.MALL_ID,t.SHOP_NAME=s.SHOP_NAME,t.EXPOSURE_COUNT=s.EXPOSURE_COUNT,
 t.STAY_COUNT=s.STAY_COUNT,t.STAY_RATE=s.STAY_RATE,t.AVG_STAY_TIME=s.AVG_STAY_TIME,t.FETCHED_AT=s.FETCHED_AT
WHEN NOT MATCHED THEN INSERT (SHOP_ID,STAT_DATE,REGION_NAME,MALL_ID,SHOP_NAME,EXPOSURE_COUNT,STAY_COUNT,STAY_RATE,AVG_STAY_TIME,FETCHED_AT)
 VALUES(s.SHOP_ID,s.STAT_DATE,s.REGION_NAME,s.MALL_ID,s.SHOP_NAME,s.EXPOSURE_COUNT,s.STAY_COUNT,s.STAY_RATE,s.AVG_STAY_TIME,s.FETCHED_AT)`

// SaveKaiyuRegionDay atomically replaces a successful, nonempty daily snapshot
// and records its attempt. Empty/failed attempts never delete retained facts.
func (adapter *Adapter) SaveKaiyuRegionDay(ctx context.Context, state model.KaiyuRegionSyncDay, rows []model.KaiyuRegionStayDaily) error {
	if err := validateKaiyuRegionSnapshot(state, rows); err != nil {
		return err
	}
	day := state.StatDate.In(kaiyuRegionLocation).Format(time.DateOnly)
	fetched := state.FetchedAt
	if fetched.IsZero() {
		fetched = time.Now()
	}
	fetchedText := fetched.In(kaiyuRegionLocation).Format(time.DateTime)
	errorMessage := []rune(state.ErrorMessage)
	if len(errorMessage) > 1000 {
		errorMessage = errorMessage[:1000]
	}
	if state.State == model.KaiyuRegionSyncSuccess {
		errorMessage = nil
	}
	tx, err := adapter.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Updating the status first serializes replacements of an existing shop-day.
	if _, err := tx.ExecContext(ctx, kaiyuRegionStateMerge, state.ShopID, day, state.MallID, state.ShopName, state.State, len(rows), fetchedText, string(errorMessage)); err != nil {
		return fmt.Errorf("save Kaiyu region day status: %w", err)
	}
	if state.State == model.KaiyuRegionSyncSuccess {
		if _, err := tx.ExecContext(ctx, "DELETE FROM YLZ_USER.KAIYU_REGION_STAY_DAILY WHERE SHOP_ID=:1 AND STAT_DATE=TO_DATE(:2,'YYYY-MM-DD')", state.ShopID, day); err != nil {
			return fmt.Errorf("replace Kaiyu region day: %w", err)
		}
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, kaiyuRegionFactMerge, row.ShopID, day, row.RegionName, row.MallID, row.ShopName, row.ExposureCount, row.StayCount, row.StayRate, row.AvgStayTime, fetchedText); err != nil {
				return fmt.Errorf("save Kaiyu region metrics: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Kaiyu region day: %w", err)
	}
	return nil
}

func validateKaiyuRegionSnapshot(state model.KaiyuRegionSyncDay, rows []model.KaiyuRegionStayDaily) error {
	if state.MallID == 0 || strings.TrimSpace(state.ShopID) == "" || utf8.RuneCountInString(state.ShopID) > 64 || strings.TrimSpace(state.ShopName) == "" || utf8.RuneCountInString(state.ShopName) > 255 || state.StatDate.IsZero() {
		return fmt.Errorf("invalid Kaiyu region shop or date")
	}
	switch state.State {
	case model.KaiyuRegionSyncSuccess:
		if len(rows) == 0 {
			return fmt.Errorf("successful Kaiyu region snapshot requires records")
		}
	case model.KaiyuRegionSyncEmpty, model.KaiyuRegionSyncFailed:
		if len(rows) != 0 {
			return fmt.Errorf("empty or failed Kaiyu region attempt cannot contain records")
		}
	default:
		return fmt.Errorf("invalid Kaiyu region sync state")
	}
	day := state.StatDate.In(kaiyuRegionLocation).Format(time.DateOnly)
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.MallID != state.MallID || row.ShopID != state.ShopID || row.ShopName != state.ShopName || row.StatDate.IsZero() || row.StatDate.In(kaiyuRegionLocation).Format(time.DateOnly) != day {
			return fmt.Errorf("Kaiyu region records do not match the shop-day snapshot")
		}
		if strings.TrimSpace(row.RegionName) == "" || utf8.RuneCountInString(row.RegionName) > 255 || seen[row.RegionName] {
			return fmt.Errorf("invalid or duplicate Kaiyu region name")
		}
		seen[row.RegionName] = true
		if row.ExposureCount < 0 || row.StayCount < 0 || row.StayRate < 0 || row.StayRate > 1 || row.AvgStayTime < 0 || math.IsNaN(row.StayRate) || math.IsInf(row.StayRate, 0) || math.IsNaN(row.AvgStayTime) || math.IsInf(row.AvgStayTime, 0) {
			return fmt.Errorf("invalid Kaiyu region metrics")
		}
	}
	return nil
}
