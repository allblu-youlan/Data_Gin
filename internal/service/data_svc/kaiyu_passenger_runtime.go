package data_svc

import (
	"context"
	"errors"
	"fmt"
	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
	"strings"
	"time"
)

// OpenKaiyuOracle reuses the existing system Oracle configuration, but requires
// the account explicitly requested for this model instead of silently changing schemas.
func OpenKaiyuOracle(ctx context.Context) (*reportoracle.Adapter, error) {
	cfg, err := appConfig.LoadReportInputOracleConfig()
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(cfg.Username, "YLZ_USER") {
		return nil, fmt.Errorf("开域客流需要 REPORT_INPUT_ORACLE_USERNAME=YLZ_USER")
	}
	adapterConfig := defaultOracleAdapterConfig(cfg)
	adapterConfig.MaxOpenConnections = 2
	adapterConfig.MaxIdleConnections = 1
	return reportoracle.Open(ctx, adapterConfig)
}

// RunKaiyuPassengerGapCheck is for trusted server-side CLI operation. HTTP callers
// must use the actor-scoped Missing and Backfill methods instead.
func RunKaiyuPassengerGapCheck(ctx context.Context, start, end time.Time, backfill bool) (missing *KaiyuPassengerMissingResult, result *KaiyuPassengerBackfillResult, resultErr error) {
	if start.IsZero() || !end.After(start) || end.Sub(start) > 31*24*time.Hour || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 || end.Minute() != 0 || end.Second() != 0 || end.Nanosecond() != 0 {
		return nil, nil, ErrKaiyuPassengerInvalid
	}
	if backfill {
		if !kaiyuBackfillMu.TryLock() {
			return nil, nil, ErrKaiyuPassengerBackfillBusy
		}
		defer kaiyuBackfillMu.Unlock()
	}
	start, end = start.In(kaiyuLocation), minTime(end.In(kaiyuLocation), time.Now().In(kaiyuLocation).Truncate(time.Hour))
	store, err := OpenKaiyuOracle(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	malls := make([]KaiyuPassengerMallOption, 0)
	var after uint
	dao := data_dao.NewMallDAO()
	for {
		rows, err := dao.List(ctx, data_dao.MallListFilter{AfterID: after, Limit: 200})
		if err != nil {
			return nil, nil, err
		}
		for _, mall := range rows {
			malls = append(malls, KaiyuPassengerMallOption{MallID: mall.ID, MallName: mall.NameCN, ShopID: mall.KaiyuShopID})
		}
		if len(rows) < 200 {
			break
		}
		after = rows[len(rows)-1].ID
	}
	var rows []model.KaiyuPassengerHourly
	if end.After(start) {
		rows, err = store.QueryKaiyuPassengerHours(ctx, start, end, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	missing, days := planKaiyuMissingHours(start, end, malls, rows)
	missing.StartDate, missing.EndDate = start.Format(time.DateOnly), end.Add(-time.Nanosecond).Format(time.DateOnly)
	if !backfill || len(days) == 0 {
		return missing, &KaiyuPassengerBackfillResult{}, nil
	}
	client, err := newKaiyuBackfillClient()
	if err != nil {
		return missing, nil, err
	}
	result, err = fillKaiyuMissingDays(ctx, days, missing.MissingHours, "", len(days), client, store)
	if err != nil {
		return missing, result, err
	}
	if result.FailedHours > 0 || result.NextCursor != "" {
		return missing, result, fmt.Errorf("客流补拉未全部完成：失败 %d 小时，仍缺 %d 小时", result.FailedHours, result.RemainingHours)
	}
	return missing, result, nil
}

func RunKaiyuPassengerSync(ctx context.Context, hour time.Time) (result KaiyuSyncResult, resultErr error) {
	cfg, err := appConfig.LoadKaiyuConfig()
	if err != nil {
		return result, err
	}
	client, err := kaiyu.NewClient(cfg.Client)
	if err != nil {
		return result, err
	}
	store, err := OpenKaiyuOracle(ctx)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	return NewKaiyuPassengerService(data_dao.NewMallDAO(), client, store).SyncHour(ctx, hour)
}
