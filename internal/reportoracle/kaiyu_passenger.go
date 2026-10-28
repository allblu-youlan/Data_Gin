package reportoracle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/etc"
	"gin-biz-web-api/model"
)

// InitializeKaiyuPassengerStore is an explicit installation step, never run by
// the hourly job. It creates only the new passenger table in YLZ_USER.
func (adapter *Adapter) InitializeKaiyuPassengerStore(ctx context.Context) error {
	var owner string
	if err := adapter.db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&owner); err != nil {
		return err
	}
	if !strings.EqualFold(owner, "YLZ_USER") {
		return fmt.Errorf("开域客流存储必须使用 YLZ_USER 用户，当前为 %s", owner)
	}
	var count int
	if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'KAIYU_PASSENGER_HOURLY'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := adapter.db.ExecContext(ctx, strings.TrimSpace(etc.OracleKaiyuPassengerSQL)); err != nil {
			var coded interface{ Code() int }
			if !errors.As(err, &coded) || coded.Code() != 955 {
				return fmt.Errorf("create Kaiyu passenger table: %w", err)
			}
		}
	}
	rows, err := adapter.db.QueryContext(ctx, "SELECT SHOP_ID,HOUR_START,MALL_ID,SHOP_NAME,PASSBY,TRAFFIC,TRAFFIC_COUNT,TRAFFIC_BATCH,CAPTURE_RATE,AVG_VISIT_DURATION,NEW_VISITORS_RATIO,DEEP_VISIT_RATIO,NET_SALES,SALES_TRANSACTIONS,ATV,SHOPPER_YIELD,FETCHED_AT FROM YLZ_USER.KAIYU_PASSENGER_HOURLY WHERE 1=0")
	if err != nil {
		return fmt.Errorf("validate Kaiyu passenger table: %w", err)
	}
	return rows.Close()
}

func (adapter *Adapter) UpsertKaiyuPassengerHour(ctx context.Context, row model.KaiyuPassengerHourly) error {
	if row.ShopID == "" || row.MallID == 0 || row.HourStart.IsZero() || row.HourStart.Minute() != 0 || row.HourStart.Second() != 0 {
		return fmt.Errorf("invalid Kaiyu passenger hour")
	}
	// Oracle TIMESTAMP has no timezone. Bind the source wall clock explicitly,
	// avoiding dependence on driver or database session timezone conversion.
	hour := row.HourStart.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
	const statement = `MERGE INTO YLZ_USER.KAIYU_PASSENGER_HOURLY t
USING (SELECT :1 SHOP_ID, TO_TIMESTAMP(:2, 'YYYY-MM-DD HH24:MI:SS') HOUR_START,
 :3 MALL_ID, :4 SHOP_NAME, :5 PASSBY, :6 TRAFFIC, :7 TRAFFIC_COUNT, :8 TRAFFIC_BATCH,
 :9 CAPTURE_RATE, :10 AVG_VISIT_DURATION, :11 NEW_VISITORS_RATIO, :12 DEEP_VISIT_RATIO,
 :13 NET_SALES, :14 SALES_TRANSACTIONS, :15 ATV, :16 SHOPPER_YIELD FROM DUAL) s
ON (t.SHOP_ID = s.SHOP_ID AND t.HOUR_START = s.HOUR_START)
WHEN MATCHED THEN UPDATE SET t.MALL_ID=s.MALL_ID,t.SHOP_NAME=s.SHOP_NAME,
 t.PASSBY=s.PASSBY,t.TRAFFIC=s.TRAFFIC,t.TRAFFIC_COUNT=s.TRAFFIC_COUNT,t.TRAFFIC_BATCH=s.TRAFFIC_BATCH,
 t.CAPTURE_RATE=s.CAPTURE_RATE,t.AVG_VISIT_DURATION=s.AVG_VISIT_DURATION,
 t.NEW_VISITORS_RATIO=s.NEW_VISITORS_RATIO,t.DEEP_VISIT_RATIO=s.DEEP_VISIT_RATIO,
 t.NET_SALES=s.NET_SALES,t.SALES_TRANSACTIONS=s.SALES_TRANSACTIONS,t.ATV=s.ATV,
 t.SHOPPER_YIELD=s.SHOPPER_YIELD,t.FETCHED_AT=SYSTIMESTAMP
WHEN NOT MATCHED THEN INSERT (SHOP_ID,HOUR_START,MALL_ID,SHOP_NAME,PASSBY,TRAFFIC,TRAFFIC_COUNT,TRAFFIC_BATCH,
 CAPTURE_RATE,AVG_VISIT_DURATION,NEW_VISITORS_RATIO,DEEP_VISIT_RATIO,NET_SALES,SALES_TRANSACTIONS,ATV,SHOPPER_YIELD,FETCHED_AT)
 VALUES (s.SHOP_ID,s.HOUR_START,s.MALL_ID,s.SHOP_NAME,s.PASSBY,s.TRAFFIC,s.TRAFFIC_COUNT,s.TRAFFIC_BATCH,
 s.CAPTURE_RATE,s.AVG_VISIT_DURATION,s.NEW_VISITORS_RATIO,s.DEEP_VISIT_RATIO,s.NET_SALES,s.SALES_TRANSACTIONS,s.ATV,s.SHOPPER_YIELD,SYSTIMESTAMP)`
	_, err := adapter.db.ExecContext(ctx, statement, row.ShopID, hour, row.MallID, row.ShopName, row.Passby, row.Traffic, row.TrafficCount, row.TrafficBatch, row.CaptureRate, row.AvgVisitDuration, row.NewVisitorsRatio, row.DeepVisitRatio, row.NetSales, row.SalesTransactions, row.ATV, row.ShopperYield)
	if err != nil {
		return fmt.Errorf("save Kaiyu passenger hour: %w", err)
	}
	return nil
}
