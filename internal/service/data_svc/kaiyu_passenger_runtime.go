package data_svc

import (
	"context"
	"errors"
	"fmt"
	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
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
