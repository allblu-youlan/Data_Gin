package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
)

type fakeSalesSummaryStore struct {
	called       bool
	code         string
	mallErr      error
	weatherStart time.Time
	weatherErr   error
}

func (store *fakeSalesSummaryStore) FindMall(_ context.Context, code string) (*model.Mall, error) {
	store.called = true
	store.code = code
	if store.mallErr != nil {
		return nil, store.mallErr
	}
	return &model.Mall{BaseModel: model.BaseModel{ID: 3}, MallCode: code, NameCN: "上海浦东世博天地店", Timezone: "Asia/Shanghai"}, nil
}
func (store *fakeSalesSummaryStore) ReadDailyWeather(_ context.Context, _ uint, start time.Time) ([]model.MallWeatherDaily, error) {
	store.weatherStart = start
	low, high := 16.0, 26.0
	return []model.MallWeatherDaily{{TemperatureMinC: &low, TemperatureMaxC: &high, Skycon: "LIGHT_RAIN"}}, store.weatherErr
}

type fakeSalesSummaryOracle struct {
	queries []reportoracle.SalesSummaryQuery
	totals  map[string]reportoracle.SalesSummaryTotals
	err     error
}

func (oracle *fakeSalesSummaryOracle) QuerySalesSummary(_ context.Context, query reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error) {
	oracle.queries = append(oracle.queries, query)
	return oracle.totals[query.StoreCode], oracle.err
}

func TestSalesSummaryUsesOracleHistoryWithoutLocalOrders(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	oracle := &fakeSalesSummaryOracle{totals: map[string]reportoracle.SalesSummaryTotals{
		"ABCN001P014": {SalesAmount: 380998, SalesQuantity: 2503, ReturnQuantity: -66, OrderCount: 636},
		"ABCN001A003": {SalesAmount: 480, SalesQuantity: 9, ReturnQuantity: -3, OrderCount: 3},
	}}
	scope := &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: scope,
		now: func() time.Time { return time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC) },
	}
	result, err := service.Query(t.Context(), 17, "abcn001p014", "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.SalesAmount != 380998 || result.Metrics.OrderCount != 636 || result.Metrics.TotalQuantity != 2437 {
		t.Fatalf("Oracle historical totals were lost without local orders: %+v", result.Metrics)
	}
	if result.Metrics.AverageOrderValue != 599.05 || result.Metrics.AverageItemPrice != 599.05 || result.Metrics.ItemsPerOrder != 3.94 {
		t.Fatalf("incorrect derived metrics: %+v", result.Metrics)
	}
	if result.Metrics.DiscountRate < 80 || result.Metrics.DiscountRate > 83 {
		t.Fatalf("discount rate: %v", result.Metrics.DiscountRate)
	}
	if store.code != "ABCN001P014" {
		t.Fatalf("wrong mall code: %+v", store)
	}
	query := oracle.queries[0]
	if query.StoreCode != "ABCN001P014" || query.Start.Format(time.RFC3339) != "2026-10-01T00:00:00+08:00" || query.End.Format(time.RFC3339) != "2026-11-01T00:00:00+08:00" {
		t.Fatalf("wrong source filter: %+v", query)
	}
	if result.Weather == nil || result.Weather.Date != "2026-11-02" {
		t.Fatalf("weather uses sales date: %+v", result.Weather)
	}
	scope.allowed = []string{"ABCN001A003"}
	result, err = service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
	if err != nil || result.Metrics.SalesAmount != 480 || oracle.queries[1].StoreCode != "ABCN001A003" {
		t.Fatalf("switching malls reused another mall's totals: result=%+v err=%v", result, err)
	}
}

func TestSalesSummaryToleratesWeatherFailure(t *testing.T) {
	store := &fakeSalesSummaryStore{weatherErr: errors.New("weather unavailable")}
	oracle := &fakeSalesSummaryOracle{totals: map[string]reportoracle.SalesSummaryTotals{"ABCN001P014": {SalesAmount: 80, SalesQuantity: 2, OrderCount: 1}}}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}, now: time.Now}
	result, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
	if err != nil || result.Weather != nil || result.Metrics.SalesAmount != 80 {
		t.Fatalf("weather failure blocked sales: result=%+v err=%v", result, err)
	}

}

func TestSalesSummaryDependencyFailuresDoNotReturnZeroTotals(t *testing.T) {
	for _, test := range []struct {
		name               string
		oracleErr, mallErr error
	}{
		{name: "source unavailable", oracleErr: errors.New("Oracle unavailable")},
		{name: "mall unavailable", mallErr: errors.New("MySQL unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeSalesSummaryStore{mallErr: test.mallErr}
			oracle := &fakeSalesSummaryOracle{err: test.oracleErr}
			service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}, now: time.Now}
			result, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
			if !errors.Is(err, ErrSalesSummaryUnavailable) || result != nil {
				t.Fatalf("failure returned a successful zero summary: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestSalesSummaryRejectsUnauthorizedMallAndInvalidDate(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	oracle := &fakeSalesSummaryOracle{}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{err: auth_svc.ErrMallScopeForbidden}, now: time.Now}
	_, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
	if !errors.Is(err, ErrSalesSummaryForbidden) || store.called || len(oracle.queries) > 0 {
		t.Fatalf("unauthorized mall queried: %v", err)
	}
	for _, test := range []struct{ name, start, end string }{{"invalid day", "2026-02-30", "2026-03-01"}, {"reversed range", "2026-10-31", "2026-10-01"}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Query(t.Context(), 17, "ABCN001P014", test.start, test.end)
			if !errors.Is(err, ErrSalesSummaryInvalid) || store.called || len(oracle.queries) > 0 {
				t.Fatalf("invalid dates queried: %v", err)
			}
		})
	}
}

func TestSalesSummaryEmptySourceTotalsKeepZeroRatios(t *testing.T) {
	metrics := SalesSummaryMetrics{}
	finishSalesSummaryMetrics(&metrics)
	if metrics.AverageOrderValue != 0 || metrics.AverageItemPrice != 0 || metrics.ItemsPerOrder != 0 {
		t.Fatalf("empty ratios: %+v", metrics)
	}
}
