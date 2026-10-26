package data_svc

import (
	"context"
	"fmt"
	"sync"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
)

type openBojunOracle interface {
	QueryBojunStores(context.Context, reportoracle.BojunStoreQuery) ([]reportoracle.BojunStoreRow, error)
	QueryBojunStocks(context.Context, reportoracle.BojunStockQuery) ([]reportoracle.BojunStockRow, error)
	QueryBojunProductInfos(context.Context, reportoracle.BojunProductInfoQuery) ([]reportoracle.BojunProductInfoRow, error)
}

type openBojunOracleOpener func(context.Context, reportoracle.Config) (openBojunOracle, error)

type openBojunQueryPermissionReader interface {
	HasPermission(context.Context, uint, string, time.Time) (bool, error)
}

// OpenBojunOracleProvider owns the lazily opened pool shared by store, stock and product info queries.
type OpenBojunOracleProvider struct {
	config    appConfig.ReportInputOracleConfig
	configErr error
	open      openBojunOracleOpener
	mu        sync.Mutex
	oracle    openBojunOracle
	opening   chan struct{}
}

func NewOpenBojunOracleProvider() *OpenBojunOracleProvider {
	configured, err := appConfig.LoadReportInputQueryConfig()
	provider := newOpenBojunOracleProvider(configured.Oracle, func(ctx context.Context, config reportoracle.Config) (openBojunOracle, error) {
		return reportoracle.Open(ctx, config)
	})
	if err != nil {
		provider.configErr = fmt.Errorf("load default Oracle configuration: %w", err)
	} else if !validDefaultOracleConfig(configured.Oracle) {
		provider.configErr = fmt.Errorf("default Oracle configuration is incomplete")
	}
	return provider
}

func newOpenBojunOracleProvider(config appConfig.ReportInputOracleConfig, opener openBojunOracleOpener) *OpenBojunOracleProvider {
	if opener == nil {
		panic("open bojun Oracle provider: nil opener")
	}
	return &OpenBojunOracleProvider{config: config, open: opener}
}

func (provider *OpenBojunOracleProvider) connection(ctx context.Context) (openBojunOracle, error) {
	if provider.configErr != nil {
		return nil, provider.configErr
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		provider.mu.Lock()
		if provider.oracle != nil {
			connection := provider.oracle
			provider.mu.Unlock()
			return connection, nil
		}
		if ready := provider.opening; ready != nil {
			provider.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ready:
				continue
			}
		}
		provider.opening = make(chan struct{})
		provider.mu.Unlock()

		connection, err := provider.open(ctx, defaultOracleAdapterConfig(provider.config))
		if err == nil && connection == nil {
			err = fmt.Errorf("open default Oracle returned no connection")
		}
		provider.mu.Lock()
		if err == nil {
			provider.oracle = connection
		}
		close(provider.opening)
		provider.opening = nil
		provider.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("open default Oracle: %w", err)
		}
		return connection, nil
	}
}

func (provider *OpenBojunOracleProvider) queryTimeout(maximum time.Duration) time.Duration {
	if configured := provider.config.QueryTimeout; configured > 0 && configured < maximum {
		return configured
	}
	return maximum
}
