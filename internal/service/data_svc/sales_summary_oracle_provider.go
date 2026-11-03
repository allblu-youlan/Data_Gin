package data_svc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

func (provider *OpenBojunOracleProvider) QuerySalesSummary(ctx context.Context, query reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error) {
	zone := strings.TrimSpace(provider.config.Timezone)
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return reportoracle.SalesSummaryTotals{}, fmt.Errorf("sales summary Oracle timezone: %w", err)
	}
	// Dates selected in the UI represent whole calendar days in the source zone.
	query.Start = time.Date(query.Start.Year(), query.Start.Month(), query.Start.Day(), 0, 0, 0, 0, location)
	query.End = time.Date(query.End.Year(), query.End.Month(), query.End.Day(), 0, 0, 0, 0, location)
	queryCtx, cancel := context.WithTimeout(ctx, provider.queryTimeout(30*time.Second))
	defer cancel()
	connection, err := provider.connection(queryCtx)
	if err != nil {
		return reportoracle.SalesSummaryTotals{}, err
	}
	return connection.QuerySalesSummary(queryCtx, query)
}
