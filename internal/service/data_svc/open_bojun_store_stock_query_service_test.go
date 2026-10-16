package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

type fakeOpenBojunOracle struct {
	stores     []reportoracle.BojunStoreRow
	stocks     []reportoracle.BojunStockRow
	storeQuery reportoracle.BojunStoreQuery
	stockQuery reportoracle.BojunStockQuery
	stockCalls int
	ctx        context.Context
	err        error
}

func (f *fakeOpenBojunOracle) QueryBojunStores(ctx context.Context, q reportoracle.BojunStoreQuery) ([]reportoracle.BojunStoreRow, error) {
	f.storeQuery, f.ctx = q, ctx
	return f.stores, f.err
}
func (f *fakeOpenBojunOracle) QueryBojunStocks(ctx context.Context, q reportoracle.BojunStockQuery) ([]reportoracle.BojunStockRow, error) {
	f.stockCalls++
	f.stockQuery, f.ctx = q, ctx
	return f.stocks, f.err
}
func newTestBojunProvider(oracle *fakeOpenBojunOracle, opened *int) *OpenBojunOracleProvider {
	return newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) {
		*opened++
		return oracle, nil
	})
}
func TestOpenBojunQueriesSharePoolAndPreservePaginationAndValues(t *testing.T) {
	value := "-123456789012345678.1234567890"
	oracle := &fakeOpenBojunOracle{
		stores: []reportoracle.BojunStoreRow{{StoreID: 1, StoreCode: "001", StoreName: "仓", IsActive: "Y"}, {StoreID: 2}},
		stocks: []reportoracle.BojunStockRow{{StoreCode: "001", ProductCode: "AbC", Qty: &value}, {StoreCode: "002"}},
	}
	opened := 0
	provider := newTestBojunProvider(oracle, &opened)
	permission := &fakeOpenBojunProductPermissionReader{allowed: true}
	storeService := newOpenBojunStoreQueryService(provider, permission, time.Now)
	stockService := newOpenBojunStockQueryService(provider, permission, time.Now)
	page, size := 2, 1
	stores, err := storeService.Query(context.Background(), 17, requestbody.OpenBojunStoreQueryRequest{StoreCode: " 001 ", NamePrefix: " 仓 ", IsStop: " N ", Page: &page, PageSize: &size})
	if err != nil || len(stores.Items) != 1 || !stores.Pagination.HasMore || stores.Items[0].StoreCode != "001" || stores.Items[0].IsStop != nil {
		t.Fatalf("stores=%+v error=%v", stores, err)
	}
	if permission.permission != model.PermissionBojunStoreRead || oracle.storeQuery != (reportoracle.BojunStoreQuery{StoreCode: "001", NamePrefix: "仓", IsActive: "Y", IsStop: "N", Offset: 1, PageSize: 1}) {
		t.Fatalf("permission=%s query=%+v", permission.permission, oracle.storeQuery)
	}
	stocks, err := stockService.Query(context.Background(), 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{" AbC "}, StoreCode: " 001 ", Page: &page, PageSize: &size})
	if err != nil || len(stocks.Items) != 1 || !stocks.Pagination.HasMore || stocks.Items[0].Qty == nil || *stocks.Items[0].Qty != value || stocks.Items[0].QtyOms != nil {
		t.Fatalf("stocks=%+v error=%v", stocks, err)
	}
	if opened != 1 || permission.permission != model.PermissionBojunStockRead || !reflect.DeepEqual(oracle.stockQuery, reportoracle.BojunStockQuery{ProductCode: []string{"AbC"}, StoreCode: "001", Offset: 1, PageSize: 1}) {
		t.Fatalf("opened=%d permission=%s query=%+v", opened, permission.permission, oracle.stockQuery)
	}
	for _, result := range []any{stores, stocks} {
		encoded, err := json.Marshal(result)
		if err != nil || strings.Contains(string(encoded), "totalItems") || strings.Contains(string(encoded), "totalPages") {
			t.Fatalf("JSON=%s error=%v", encoded, err)
		}
	}
	oracle.stores, oracle.stocks = nil, nil
	stores, err = storeService.Query(context.Background(), 17, requestbody.OpenBojunStoreQueryRequest{})
	if err != nil || stores.Items == nil || stores.Pagination.HasMore || stores.Pagination.Page != 1 || stores.Pagination.PageSize != 50 {
		t.Fatalf("empty stores=%+v error=%v", stores, err)
	}
	stocks, err = stockService.Query(context.Background(), 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}, StoreCode: "001"})
	if err != nil || stocks.Items == nil || stocks.Pagination.HasMore {
		t.Fatalf("empty stocks=%+v error=%v", stocks, err)
	}
	oracle.stocks = []reportoracle.BojunStockRow{{StoreCode: "001"}}
	stocks, err = stockService.Query(context.Background(), 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}, StoreCode: "001", PageSize: &size})
	if err != nil || stocks.Pagination.HasMore || len(stocks.Items) != 1 {
		t.Fatalf("last page=%+v error=%v", stocks, err)
	}
}

func TestOpenBojunQueriesRejectInvalidInputBeforeOpeningOracle(t *testing.T) {
	opened := 0
	provider := newTestBojunProvider(&fakeOpenBojunOracle{}, &opened)
	permission := &fakeOpenBojunProductPermissionReader{allowed: true}
	store := newOpenBojunStoreQueryService(provider, permission, time.Now)
	stock := newOpenBojunStockQueryService(provider, permission, time.Now)
	zero, large := 0, 1001
	for _, tt := range []struct {
		name    string
		request requestbody.OpenBojunStoreQueryRequest
	}{
		{"code too long", requestbody.OpenBojunStoreQueryRequest{StoreCode: strings.Repeat("中", 27)}},
		{"name too long", requestbody.OpenBojunStoreQueryRequest{NamePrefix: strings.Repeat("中", 86)}},
		{"lowercase active", requestbody.OpenBojunStoreQueryRequest{IsActive: "y"}},
		{"invalid stop", requestbody.OpenBojunStoreQueryRequest{IsStop: "ALL"}},
		{"page zero", requestbody.OpenBojunStoreQueryRequest{Page: &zero}},
		{"large page", requestbody.OpenBojunStoreQueryRequest{Page: &large}},
	} {
		t.Run("store/"+tt.name, func(t *testing.T) {
			if _, err := store.Query(context.Background(), 17, tt.request); !errors.Is(err, ErrOpenBojunStoreInvalidQuery) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, tt := range []struct {
		name    string
		request requestbody.OpenBojunStockQueryRequest
	}{
		{"missing product", requestbody.OpenBojunStockQueryRequest{StoreCode: "001"}},
		{"blank product identifiers", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{" "}, ProductColor: []string{" "}, ProductName: []string{" "}}},
		{"code and color", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{"SKU"}, ProductColor: []string{"COLOR"}}},
		{"code and name", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{"SKU"}, ProductName: []string{"STYLE"}}},
		{"color and name", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductColor: []string{"COLOR"}, ProductName: []string{"STYLE"}}},
		{"all product identifiers", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{"SKU"}, ProductColor: []string{"COLOR"}, ProductName: []string{"STYLE"}}},
		{"long color", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductColor: []string{strings.Repeat("中", 27)}}},
		{"long name", requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductName: []string{strings.Repeat("x", 81)}}},
		{"color missing store", requestbody.OpenBojunStockQueryRequest{ProductColor: []string{"COLOR"}}},
		{"name missing store", requestbody.OpenBojunStockQueryRequest{ProductName: []string{"STYLE"}}},
		{"missing store", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}}},
		{"blank product", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{" "}, StoreCode: "001"}},
		{"blank store", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}, StoreCode: " "}},
		{"long product", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{strings.Repeat("中", 27)}, StoreCode: "001"}},
		{"long store", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}, StoreCode: strings.Repeat("x", 81)}},
		{"size zero", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"AbC"}, StoreCode: "001", PageSize: &zero}},
	} {
		t.Run("stock/"+tt.name, func(t *testing.T) {
			if _, err := stock.Query(context.Background(), 17, tt.request); !errors.Is(err, ErrOpenBojunStockInvalidQuery) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if opened != 0 {
		t.Fatalf("invalid inputs opened Oracle %d times", opened)
	}
}

func TestOpenBojunQueriesEnforcePermissionsBeforeOracle(t *testing.T) {
	for _, tt := range []struct {
		name    string
		actor   uint
		allowed bool
		err     error
	}{
		{"no actor", 0, true, nil}, {"not granted", 17, false, nil}, {"permission unavailable", 17, false, errors.New("permission database down")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opened := 0
			provider := newTestBojunProvider(&fakeOpenBojunOracle{}, &opened)
			permission := &fakeOpenBojunProductPermissionReader{allowed: tt.allowed, err: tt.err}
			_, storeErr := newOpenBojunStoreQueryService(provider, permission, time.Now).Query(context.Background(), tt.actor, requestbody.OpenBojunStoreQueryRequest{})
			_, stockErr := newOpenBojunStockQueryService(provider, permission, time.Now).Query(context.Background(), tt.actor, requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{"ABC"}})
			storeWant, stockWant := ErrOpenBojunStoreForbidden, ErrOpenBojunStockForbidden
			if tt.err != nil {
				storeWant, stockWant = ErrOpenBojunStoreUnavailable, ErrOpenBojunStockUnavailable
			}
			if !errors.Is(storeErr, storeWant) || !errors.Is(stockErr, stockWant) || opened != 0 {
				t.Fatalf("store=%v stock=%v opened=%d", storeErr, stockErr, opened)
			}
		})
	}
}

func TestOpenBojunOracleProviderRetriesInitializationAndHonorsWaiterCancellation(t *testing.T) {
	calls := 0
	oracle := &fakeOpenBojunOracle{}
	provider := newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary connection failure")
		}
		return oracle, nil
	})
	if _, err := provider.connection(context.Background()); err == nil {
		t.Fatal("first failure must be returned")
	}
	for i := 0; i < 2; i++ {
		if got, err := provider.connection(context.Background()); err != nil || got != oracle {
			t.Fatalf("connection=%v error=%v", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("open calls=%d", calls)
	}

	started, release := make(chan struct{}), make(chan struct{})
	provider = newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) {
		close(started)
		<-release
		return oracle, nil
	})
	done := make(chan error, 1)
	go func() { _, err := provider.connection(context.Background()); done <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.connection(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error=%v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOpenBojunOracleProviderSharesOnePoolAcrossConcurrentCallers(t *testing.T) {
	var opened atomic.Int32
	oracle := &fakeOpenBojunOracle{}
	provider := newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) { opened.Add(1); return oracle, nil })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := provider.connection(context.Background())
			if err != nil || got != oracle {
				t.Errorf("connection=%v error=%v", got, err)
			}
		}()
	}
	wg.Wait()
	if opened.Load() != 1 {
		t.Fatalf("opened=%d", opened.Load())
	}
}

func TestOpenBojunQueriesShareDeadlineAcrossConnectionAndQuery(t *testing.T) {
	for _, kind := range []string{"store", "stock"} {
		t.Run(kind, func(t *testing.T) {
			for _, configured := range []time.Duration{0, 2 * time.Second, 30 * time.Second} {
				config := openBojunProductOracleConfig()
				config.QueryTimeout = configured
				oracle := &fakeOpenBojunOracle{}
				var openCtx context.Context
				provider := newOpenBojunOracleProvider(config, func(ctx context.Context, _ reportoracle.Config) (openBojunOracle, error) {
					openCtx = ctx
					return oracle, nil
				})
				permission := &fakeOpenBojunProductPermissionReader{allowed: true}
				started := time.Now()
				var err error
				cap := 5 * time.Second
				if kind == "store" {
					_, err = newOpenBojunStoreQueryService(provider, permission, time.Now).Query(context.Background(), 17, requestbody.OpenBojunStoreQueryRequest{})
				} else {
					cap = 10 * time.Second
					_, err = newOpenBojunStockQueryService(provider, permission, time.Now).Query(context.Background(), 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"ABC"}, StoreCode: "001"})
				}
				if configured > 0 && configured < cap {
					cap = configured
				}
				if err != nil || openCtx != oracle.ctx {
					t.Fatalf("contexts differ or error=%v", err)
				}
				deadline, ok := openCtx.Deadline()
				if !ok || deadline.Before(started.Add(cap)) || deadline.After(time.Now().Add(cap)) {
					t.Fatalf("deadline=%v expected cap=%s", deadline, cap)
				}
			}
		})
	}
	opened := 0
	provider := newTestBojunProvider(&fakeOpenBojunOracle{}, &opened)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newOpenBojunStockQueryService(provider, &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now).Query(ctx, 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"ABC"}, StoreCode: "001"})
	if !errors.Is(err, context.Canceled) || opened != 0 {
		t.Fatalf("canceled request error=%v opened=%d", err, opened)
	}
}

func TestOpenBojunQueriesDoNotReturnEmptySuccessForOracleFailures(t *testing.T) {
	for _, stage := range []string{"configuration", "connection", "query"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("database unavailable")
			opened := 0
			oracle := &fakeOpenBojunOracle{}
			provider := newTestBojunProvider(oracle, &opened)
			switch stage {
			case "configuration":
				provider.configErr = failure
			case "connection":
				provider.open = func(context.Context, reportoracle.Config) (openBojunOracle, error) { return nil, failure }
			case "query":
				oracle.err = failure
			}
			permissions := &fakeOpenBojunProductPermissionReader{allowed: true}
			stores, err := newOpenBojunStoreQueryService(provider, permissions, time.Now).Query(context.Background(), 17, requestbody.OpenBojunStoreQueryRequest{})
			if stores != nil || !errors.Is(err, ErrOpenBojunStoreUnavailable) || !errors.Is(err, failure) {
				t.Fatalf("stores=%+v error=%v", stores, err)
			}
			stocks, err := newOpenBojunStockQueryService(provider, permissions, time.Now).Query(context.Background(), 17, requestbody.OpenBojunStockQueryRequest{ProductCode: []string{"ABC"}, StoreCode: "001"})
			if stocks != nil || !errors.Is(err, ErrOpenBojunStockUnavailable) || !errors.Is(err, failure) {
				t.Fatalf("stocks=%+v error=%v", stocks, err)
			}
		})
	}
}

func TestOpenBojunStockQueriesByAnyProductIdentifier(t *testing.T) {
	page, size := 3, 2
	for _, tt := range []struct {
		name    string
		request requestbody.OpenBojunStockQueryRequest
		want    reportoracle.BojunStockQuery
	}{
		{"code", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{" 001AbC ", "002AbC", "001AbC"}, ProductColor: []string{}, ProductName: []string{}}, reportoracle.BojunStockQuery{ProductCode: []string{"001AbC", "002AbC"}}},
		{"color", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{}, ProductColor: []string{" C44G3063CE146 ", "C44G3063CE147", "C44G3063CE146"}, ProductName: []string{}}, reportoracle.BojunStockQuery{ProductColor: []string{"C44G3063CE146", "C44G3063CE147"}}},
		{"name", requestbody.OpenBojunStockQueryRequest{ProductCode: []string{}, ProductColor: []string{}, ProductName: []string{" C44G3063C ", "C44G3064C", "C44G3063C"}}, reportoracle.BojunStockQuery{ProductName: []string{"C44G3063C", "C44G3064C"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opened := 0
			oracle := &fakeOpenBojunOracle{stocks: []reportoracle.BojunStockRow{
				{StoreCode: "001", ProductCode: "SKU090"},
				{StoreCode: "001", ProductCode: "SKU100"},
				{StoreCode: "001", ProductCode: "SKU110"},
			}}
			provider := newTestBojunProvider(oracle, &opened)
			permission := &fakeOpenBojunProductPermissionReader{allowed: true}
			service := newOpenBojunStockQueryService(provider, permission, time.Now)
			tt.request.StoreCode, tt.request.Page, tt.request.PageSize = " 001 ", &page, &size
			tt.want.StoreCode, tt.want.PageSize, tt.want.Offset = "001", size, 4
			result, err := service.Query(context.Background(), 17, tt.request)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(oracle.stockQuery, tt.want) || opened != 1 || oracle.stockCalls != 1 || permission.permission != model.PermissionBojunStockRead {
				t.Fatalf("query=%+v want=%+v opened=%d permission=%s", oracle.stockQuery, tt.want, opened, permission.permission)
			}
			if len(result.Items) != 2 || !result.Pagination.HasMore || result.Pagination.Page != page || result.Pagination.PageSize != size || result.Items[0].ProductCode != "SKU090" || result.Items[1].ProductCode != "SKU100" {
				t.Fatalf("multi-SKU pagination=%+v", result)
			}
		})
	}
}

func TestOpenBojunStockBatchLimitsBeforeOpeningOracle(t *testing.T) {
	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = fmt.Sprintf("SKU%03d", i)
	}
	tooMany := append(append([]string(nil), hundred...), hundred[0])
	for selector := 0; selector < 3; selector++ {
		for _, tt := range []struct {
			name    string
			values  []string
			wantErr bool
		}{
			{"100 products", hundred, false},
			{"101 products including duplicate", tooMany, true},
			{"empty list", []string{}, true},
			{"empty item", []string{"SKU", ""}, true},
			{"blank item", []string{"SKU", " \t"}, true},
			{"80 UTF8 bytes", []string{strings.Repeat("款", 26) + "ab"}, false},
			{"81 UTF8 bytes", []string{strings.Repeat("款", 27)}, true},
		} {
			t.Run(fmt.Sprintf("%s/%s", []string{"code", "color", "name"}[selector], tt.name), func(t *testing.T) {
				request := requestbody.OpenBojunStockQueryRequest{StoreCode: "001"}
				fields := []*[]string{&request.ProductCode, &request.ProductColor, &request.ProductName}
				*fields[selector] = tt.values
				opened := 0
				oracle := &fakeOpenBojunOracle{}
				service := newOpenBojunStockQueryService(newTestBojunProvider(oracle, &opened), &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
				result, err := service.Query(t.Context(), 17, request)
				if tt.wantErr {
					if !errors.Is(err, ErrOpenBojunStockInvalidQuery) || result != nil || opened != 0 || oracle.stockCalls != 0 {
						t.Fatalf("invalid batch: result=%+v error=%v opened=%d calls=%d", result, err, opened, oracle.stockCalls)
					}
					return
				}
				if err != nil || opened != 1 || oracle.stockCalls != 1 {
					t.Fatalf("valid batch: error=%v opened=%d calls=%d", err, opened, oracle.stockCalls)
				}
				got := [][]string{oracle.stockQuery.ProductCode, oracle.stockQuery.ProductColor, oracle.stockQuery.ProductName}[selector]
				if !reflect.DeepEqual(got, tt.values) || result.Items == nil || len(result.Items) != 0 || result.Pagination.HasMore {
					t.Fatalf("query=%+v result=%+v", oracle.stockQuery, result)
				}
			})
		}
	}
}
