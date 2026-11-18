package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/cache"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type fakeSalesSummaryOracle struct {
	queries []reportoracle.SalesSummaryQuery
	totals  reportoracle.SalesSummaryTotals
	err     error
}

func (oracle *fakeSalesSummaryOracle) QuerySalesSummary(_ context.Context, query reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error) {
	oracle.queries = append(oracle.queries, query)
	return oracle.totals, oracle.err
}

func TestSalesSummaryStoreWithoutPushTargetUsesOracleAmounts(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "ABCN001A008")
	store := &fakeSalesSummaryStore{orderErr: errors.New("no local orders")}
	oracle := &fakeSalesSummaryOracle{totals: reportoracle.SalesSummaryTotals{
		SalesAmount: 13613.70, SalesQuantity: 31, OrderCount: 31,
	}}
	service := &SalesSummaryService{
		store: store, oracle: oracle,
		mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001A002"}}, now: time.Now,
		metricsCache: cache.NewMemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics](30*time.Second, 256),
	}
	for range 2 {
		result, err := service.Query(t.Context(), 17, "ABCN001A002", "2026-11-18", "2026-11-18")
		if err != nil || result.Metrics.SalesAmount != 13613.70 || result.Metrics.OrderCount != 31 {
			t.Fatalf("store without push target lost source totals: result=%+v err=%v", result, err)
		}
		if result.Metrics.PushedOrderCount != 0 || result.Metrics.DiscountRate != 0 {
			t.Fatalf("source orders fabricated push count or discount: %+v", result.Metrics)
		}
	}
	if store.readCalls != 0 || len(oracle.queries) != 1 {
		t.Fatalf("wrong data source or cache behavior: local=%d oracle=%d", store.readCalls, len(oracle.queries))
	}
	query := oracle.queries[0]
	if query.StoreCode != "ABCN001A002" || !query.IncludeNonToShop ||
		query.Start.Format(time.RFC3339) != "2026-11-18T00:00:00+08:00" ||
		query.End.Format(time.RFC3339) != "2026-11-19T00:00:00+08:00" {
		t.Fatalf("wrong source date or store filter: %+v", query)
	}
}

func TestSalesSummaryConfiguredTargetNeverFallsBackToOracleAmounts(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "ABCN001A008")
	for _, code := range []string{"ABCN001A003", "ABCN001A008"} {
		for _, queryErr := range []error{nil, errors.New("delivery query unavailable")} {
			t.Run(code+"/"+fmt.Sprint(queryErr), func(t *testing.T) {
				store := &fakeSalesSummaryStore{orderErr: queryErr, emptyOrders: true}
				oracle := &fakeSalesSummaryOracle{totals: reportoracle.SalesSummaryTotals{SalesAmount: 13613.70}}
				service := &SalesSummaryService{store: store, oracle: oracle,
					mallScope: &fakeBusinessOverviewMallScope{allowed: []string{code}}, now: time.Now}
				result, err := service.Query(t.Context(), 17, code, "2026-11-18", "2026-11-18")
				if queryErr != nil {
					if !errors.Is(err, ErrSalesSummaryUnavailable) || result != nil {
						t.Fatalf("delivery query failure was hidden: result=%+v err=%v", result, err)
					}
				} else if err != nil || result.Metrics.SalesAmount != 0 || result.Metrics.PushedOrderCount != 0 {
					t.Fatalf("no successful deliveries must produce zero amount: result=%+v err=%v", result, err)
				}
				if len(oracle.queries) != 0 {
					t.Fatalf("configured target queried unpushed source totals: %d", len(oracle.queries))
				}
			})
		}
	}
}

func TestSalesSummaryStoreWithoutPushTargetPreservesOracleFailure(t *testing.T) {
	t.Setenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE", "ABCN001A008")
	store := &fakeSalesSummaryStore{}
	oracle := &fakeSalesSummaryOracle{err: errors.New("Oracle unavailable")}
	service := &SalesSummaryService{store: store, oracle: oracle,
		mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001A002"}}, now: time.Now}
	result, err := service.Query(t.Context(), 17, "ABCN001A002", "2026-11-18", "2026-11-18")
	if !errors.Is(err, ErrSalesSummaryUnavailable) || result != nil || store.readCalls != 0 {
		t.Fatalf("source failure returned partial local amount: result=%+v err=%v local=%d", result, err, store.readCalls)
	}
}

func TestSalesSummaryMetricsUsePushedAmountAndItemQuantities(t *testing.T) {
	oracleID := uint64(25)
	orders := []model.BojunRetailOrder{
		{DocNo: "SALE-1", OrderTypeCode: "CMR", TotalAmtActual: 300, TotalQty: 3, ItemsJSON: `[{"qty":2},{"qty":1}]`},
		{DocNo: "SALE-2", OracleRetailID: &oracleID, OrderTypeCode: "CMR", PushAmount: 200, PaidAmount: 250, TotalQty: 1, ItemsJSON: `[{"qty":4}]`},
		{DocNo: "RETURN", OracleRetailID: &oracleID, OrderTypeCode: "RET", PushAmount: 50, TotalQty: -1, ItemsJSON: `[{"qty":2}]`},
		{DocNo: "EXCHANGE", OrderTypeCode: "EXP", TotalAmtActual: 30, TotalQty: 1, ItemsJSON: `[{"qty":-1},{"qty":2}]`},
	}
	var metrics SalesSummaryMetrics
	for _, order := range orders {
		if err := addSalesSummaryOrder(&metrics, order); err != nil {
			t.Fatal(err)
		}
	}
	finishSalesSummaryMetrics(&metrics)
	if metrics.SalesAmount != 480 || metrics.SalesQuantity != 9 || metrics.ReturnQuantity != -3 || metrics.TotalQuantity != 6 || metrics.OrderCount != 3 || metrics.PushedOrderCount != 4 {
		t.Fatalf("unexpected totals: %+v", metrics)
	}
	if metrics.AverageOrderValue != 160 || metrics.AverageItemPrice != 80 || metrics.ItemsPerOrder != 3 {
		t.Fatalf("unexpected ratios: %+v", metrics)
	}
	if metrics.DiscountRate != 0 {
		t.Fatalf("unavailable discount rate must not contain fabricated data: %v", metrics.DiscountRate)
	}
}

type fakeSalesSummaryStore struct {
	called       bool
	readCalls    int
	emptyOrders  bool
	orderErr     error
	code         string
	start        int
	end          int
	weatherStart time.Time
	weatherErr   error
}

type concurrentSalesSummaryStore struct {
	salesSummaryStore
	readCalls atomic.Int32
}

func (store *concurrentSalesSummaryStore) ReadPushedOrders(
	ctx context.Context, _ string, _, _ int, visit func(model.BojunRetailOrder) error,
) error {
	store.readCalls.Add(1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Millisecond):
		return visit(model.BojunRetailOrder{TotalAmtActual: 80, TotalQty: 2, OrderTypeCode: "CMR"})
	}
}

func TestSalesSummaryCoalescesConcurrentMetricLoads(t *testing.T) {
	store := &concurrentSalesSummaryStore{}
	service := &SalesSummaryService{
		store: store, metricsCache: cache.NewMemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics](30*time.Second, 256),
	}
	key := salesSummaryCacheKey{MallCode: "ABCN001A003", Start: 20261001, End: 20261031}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			metrics, err := service.readMetrics(t.Context(), key)
			if err != nil || metrics.SalesAmount != 80 || metrics.SalesQuantity != 2 || metrics.OrderCount != 1 {
				t.Errorf("shared aggregation = %+v, error = %v", metrics, err)
			}
		}()
	}
	close(start)
	workers.Wait()
	if calls := store.readCalls.Load(); calls != 1 {
		t.Fatalf("concurrent aggregation read orders %d times, want 1", calls)
	}
}

func (store *fakeSalesSummaryStore) FindMall(_ context.Context, code string) (*model.Mall, error) {
	store.called = true
	return &model.Mall{BaseModel: model.BaseModel{ID: 3}, MallCode: code, NameCN: "东台里", Timezone: "Asia/Shanghai"}, nil
}

func (store *fakeSalesSummaryStore) ReadPushedOrders(
	_ context.Context,
	code string,
	start int,
	end int,
	visit func(model.BojunRetailOrder) error,
) error {
	store.readCalls++
	if store.orderErr != nil {
		return store.orderErr
	}
	store.code, store.start, store.end = code, start, end
	if store.emptyOrders {
		return nil
	}
	return visit(model.BojunRetailOrder{TotalAmtActual: 80, TotalQty: 2, OrderTypeCode: "CMR"})
}

func TestSalesSummaryCachesSuccessfulTotalsAfterAuthorization(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	scope := &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001A003"}}
	service := &SalesSummaryService{
		store: store, mallScope: scope, now: time.Now,
		metricsCache: cache.NewMemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics](30*time.Second, 256),
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
		if err != nil || result.Metrics.SalesAmount != 80 {
			t.Fatalf("query=%+v err=%v", result, err)
		}
		result.Metrics.SalesAmount = 999
	}
	if store.readCalls != 1 {
		t.Fatalf("repeated summary read orders %d times", store.readCalls)
	}
	if _, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-02", "2026-10-31"); err != nil {
		t.Fatal(err)
	}
	if store.readCalls != 2 {
		t.Fatal("different date range reused the wrong summary")
	}
	scope.err = auth_svc.ErrMallScopeForbidden
	if _, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31"); !errors.Is(err, ErrSalesSummaryForbidden) {
		t.Fatalf("cached summary bypassed authorization: %v", err)
	}
}

func TestSalesSummaryDoesNotCacheFailedAggregation(t *testing.T) {
	store := &fakeSalesSummaryStore{orderErr: errors.New("database unavailable")}
	service := &SalesSummaryService{
		store: store, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001A003"}}, now: time.Now,
		metricsCache: cache.NewMemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics](30*time.Second, 256),
	}
	if _, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31"); err == nil {
		t.Fatal("failed aggregation succeeded")
	}
	store.orderErr = nil
	result, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
	if err != nil || result.Metrics.SalesAmount != 80 || store.readCalls != 2 {
		t.Fatalf("recovered summary=%+v calls=%d err=%v", result, store.readCalls, err)
	}
}

func (store *fakeSalesSummaryStore) ReadDailyWeather(
	_ context.Context,
	_ uint,
	start time.Time,
) ([]model.MallWeatherDaily, error) {
	store.weatherStart = start
	low, high := 16.0, 26.0
	return []model.MallWeatherDaily{{TemperatureMinC: &low, TemperatureMaxC: &high, Skycon: "LIGHT_RAIN"}}, store.weatherErr
}

func TestSalesSummaryQueryScopesMallAndUsesTodayWeather(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	service := &SalesSummaryService{
		store: store, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001A003"}},
		now: func() time.Time { return time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC) },
	}
	result, err := service.Query(t.Context(), 17, "abcn001a003", "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if store.code != "ABCN001A003" || store.start != 20261001 || store.end != 20261031 {
		t.Fatalf("wrong order filter: %+v", store)
	}
	if result.Metrics.SalesAmount != 80 || result.Mall.NameCN != "东台里" || result.Weather == nil || result.Weather.Date != "2026-11-02" {
		t.Fatalf("wrong result: %+v", result)
	}
	store.weatherErr = errors.New("weather unavailable")
	result, err = service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
	if err != nil || result.Weather != nil || result.Metrics.SalesAmount != 80 {
		t.Fatalf("weather failure blocked sales: result=%+v err=%v", result, err)
	}
}

func TestSalesSummaryRejectsUnauthorizedMallAndInvalidDate(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	service := &SalesSummaryService{
		store: store, mallScope: &fakeBusinessOverviewMallScope{err: auth_svc.ErrMallScopeForbidden}, now: time.Now,
	}
	_, err := service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
	if !errors.Is(err, ErrSalesSummaryForbidden) || store.called {
		t.Fatalf("unauthorized store was queried: %v", err)
	}
	for _, dates := range [][2]string{{"2026-02-30", "2026-03-01"}, {"2026-10-31", "2026-10-01"}} {
		_, err := service.Query(t.Context(), 17, "ABCN001A003", dates[0], dates[1])
		if !errors.Is(err, ErrSalesSummaryInvalid) || store.called {
			t.Fatalf("invalid dates were queried: dates=%v err=%v", dates, err)
		}
	}
}

func TestSalesSummaryQueryUsesFirstSuccessfulPushDate(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	statement := salesSummaryPushedOrdersQuery(db, "ABCN001A003", 20261001, 20261031).
		Find(&[]model.BojunRetailOrder{}).Statement
	query := statement.SQL.String()
	for _, required := range []string{
		"c_store_code = ?", "sent.sent_at >= ? AND sent.sent_at < ?",
		"sent.success = TRUE", "sent.source_code = 'bojun_order'", "sent.destination_id = 0",
		"COALESCE(sent.response_body, '') <> 'skipped_by_order_push_policy'",
		"NOT EXISTS (", "earlier.clean_record_id = sent.clean_record_id", "earlier.sent_at < ?",
		"earlier.success = TRUE", "COALESCE(earlier.response_body, '') <> 'skipped_by_order_push_policy'",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("missing pushed-order filter %q in %s", required, query)
		}
	}
	for _, excluded := range []string{"billdate", "JSON_EXTRACT", "DATE(sent.sent_at)", "GROUP BY"} {
		if strings.Contains(query, excluded) {
			t.Fatalf("push-date query contains obsolete or unbounded filter %q: %s", excluded, query)
		}
	}
	if len(statement.Vars) != 4 || statement.Vars[0] != "ABCN001A003" ||
		statement.Vars[1] != "2026-10-01 00:00:00" || statement.Vars[2] != "2026-11-01 00:00:00" ||
		statement.Vars[3] != "2026-10-01 00:00:00" {
		t.Fatalf("unexpected bound query parameters: %v", statement.Vars)
	}
}

func TestSalesSummaryEmptyOrdersAndQuantityFallback(t *testing.T) {
	var empty SalesSummaryMetrics
	finishSalesSummaryMetrics(&empty)
	if empty.AverageOrderValue != 0 || empty.AverageItemPrice != 0 || empty.ItemsPerOrder != 0 {
		t.Fatalf("empty ratios: %+v", empty)
	}
	if empty.PushedOrderCount != 0 || empty.DiscountRate != 0 {
		t.Fatalf("empty orders produced push count or discount: %+v", empty)
	}
	var metrics SalesSummaryMetrics
	if err := addSalesSummaryOrder(&metrics, model.BojunRetailOrder{OrderTypeCode: "CMR", TotalQty: 5, TotalAmtActual: 99, ItemsJSON: `[]`}); err != nil {
		t.Fatal(err)
	}
	if metrics.SalesQuantity != 5 || metrics.OrderCount != 1 {
		t.Fatalf("missing header quantity fallback: %+v", metrics)
	}
}

func TestSalesSummaryHeaderQuantityAggregationAllocationBudget(t *testing.T) {
	order := model.BojunRetailOrder{
		DocNo: "HEADER-QUANTITY", BillDate: 20261105, OrderTypeCode: "CMR",
		TotalAmtActual: 99, TotalQty: 5,
	}
	var metrics SalesSummaryMetrics
	var err error
	allocations := testing.AllocsPerRun(100, func() {
		metrics = SalesSummaryMetrics{}
		err = addSalesSummaryOrder(&metrics, order)
	})
	if err != nil || metrics.SalesAmount != 99 || metrics.SalesQuantity != 5 || metrics.OrderCount != 1 {
		t.Fatalf("header quantity aggregation = %+v, error = %v", metrics, err)
	}
	if allocations > 1 {
		t.Fatalf("header quantity aggregation allocated %.0f times per order, want at most 1", allocations)
	}
}

func TestSalesSummaryNegativeAmountRefundAndExchangeQuantities(t *testing.T) {
	var refund SalesSummaryMetrics
	if err := addSalesSummaryOrder(&refund, model.BojunRetailOrder{
		TotalAmtActual: -90, TotalQty: 2, ItemsJSON: `[{"qty":2}]`,
	}); err != nil {
		t.Fatal(err)
	}
	finishSalesSummaryMetrics(&refund)
	if refund.SalesQuantity != 0 || refund.ReturnQuantity != -2 || refund.OrderCount != 0 || refund.PushedOrderCount != 1 || refund.AverageItemPrice != 0 {
		t.Fatalf("negative amount refund counted as sales: %+v", refund)
	}
	var exchange SalesSummaryMetrics
	if err := addSalesSummaryOrder(&exchange, model.BojunRetailOrder{
		OrderTypeCode: "EXP", TotalAmtActual: -20, TotalQty: 0, ItemsJSON: `[{"qty":1},{"qty":-1}]`,
	}); err != nil {
		t.Fatal(err)
	}
	if exchange.SalesQuantity != 1 || exchange.ReturnQuantity != -1 || exchange.OrderCount != 1 {
		t.Fatalf("negative exchange lost item directions: %+v", exchange)
	}
	finishSalesSummaryMetrics(&exchange)
	if exchange.TotalQuantity != 0 || exchange.PushedOrderCount != 1 || exchange.AverageItemPrice != 0 {
		t.Fatalf("zero-net-quantity exchange produced an item price: %+v", exchange)
	}
}
