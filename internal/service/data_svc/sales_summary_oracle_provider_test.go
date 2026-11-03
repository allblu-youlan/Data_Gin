package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

type salesSummaryProviderOracle struct {
	fakeOpenBojunOracle
	query reportoracle.SalesSummaryQuery
}

func (oracle *salesSummaryProviderOracle) QuerySalesSummary(ctx context.Context, query reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error) {
	oracle.query, oracle.ctx = query, ctx
	return reportoracle.SalesSummaryTotals{SalesAmount: 125, SalesQuantity: 2, OrderCount: 1}, oracle.err
}

func TestSalesSummaryProviderReusesPoolAndPreservesLocalDateRange(t *testing.T) {
	config := openBojunProductOracleConfig()
	config.Timezone = "Asia/Shanghai"
	config.QueryTimeout = 2 * time.Second
	oracle := &salesSummaryProviderOracle{}
	opened := 0
	var openContext context.Context
	provider := newOpenBojunOracleProvider(config, func(ctx context.Context, _ reportoracle.Config) (openBojunOracle, error) {
		opened++
		openContext = ctx
		return oracle, nil
	})
	query := reportoracle.SalesSummaryQuery{
		Start:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		StoreCode: "ABCN001P014", ConfirmedDocNos: []string{"SENT"}, SkippedDocNos: []string{"SKIP"},
	}
	started := time.Now()
	totals, err := provider.QuerySalesSummary(t.Context(), query)
	if err != nil || totals.SalesAmount != 125 || opened != 1 {
		t.Fatalf("totals=%+v opened=%d error=%v", totals, opened, err)
	}
	if oracle.ctx != openContext {
		t.Fatal("connection and query must share the same bounded context")
	}
	deadline, ok := oracle.ctx.Deadline()
	if !ok || deadline.Before(started.Add(2*time.Second)) || deadline.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("wrong query deadline: %v", deadline)
	}
	if oracle.query.Start.Format(time.RFC3339) != "2026-10-01T00:00:00+08:00" || oracle.query.End.Format(time.RFC3339) != "2026-11-01T00:00:00+08:00" {
		t.Fatalf("source date range shifted: %+v", oracle.query)
	}
	query.StoreCode = "ABCN001A003"
	if _, err := provider.QuerySalesSummary(t.Context(), query); err != nil || opened != 1 || oracle.query.StoreCode != query.StoreCode {
		t.Fatalf("pool not reused or store filter stale: opened=%d query=%+v err=%v", opened, oracle.query, err)
	}
}

func TestSalesSummaryProviderPropagatesSourceFailures(t *testing.T) {
	failure := errors.New("source unavailable")
	oracle := &salesSummaryProviderOracle{fakeOpenBojunOracle: fakeOpenBojunOracle{err: failure}}
	provider := newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) {
		return oracle, nil
	})
	if _, err := provider.QuerySalesSummary(t.Context(), reportoracle.SalesSummaryQuery{}); !errors.Is(err, failure) {
		t.Fatalf("source failure was hidden: %v", err)
	}
	provider.configErr = failure
	if _, err := provider.QuerySalesSummary(t.Context(), reportoracle.SalesSummaryQuery{}); !errors.Is(err, failure) {
		t.Fatalf("configuration failure was hidden: %v", err)
	}
}
