package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

func TestOpenBojunProductInfoQueryDefaultsAndPreservesValues(t *testing.T) {
	productCode, priceList, costRate := "000SKU", "123456789012345678.1234567890", "-0.0000000001"
	productID, productColorID, productAliasID := "9007199254740993", "9007199254740994", "9007199254740995"
	oracle := &fakeOpenBojunOracle{}
	opened := 0
	service := newOpenBojunProductInfoQueryService(newTestBojunProvider(oracle, &opened), &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
	result, err := service.Query(t.Context(), 17, requestbody.OpenBojunProductInfoQueryRequest{})
	if err != nil || result.Items == nil || len(result.Items) != 0 ||
		result.Pagination != (OpenBojunQueryPagination{Page: 1, PageSize: 50}) ||
		!reflect.DeepEqual(oracle.productInfoQuery, reportoracle.BojunProductInfoQuery{PageSize: 50}) {
		t.Fatalf("default query result=%+v query=%+v error=%v", result, oracle.productInfoQuery, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"items":[]`) {
		t.Fatalf("empty items JSON=%s error=%v", encoded, err)
	}

	size := 1
	oracle.productInfos = []reportoracle.BojunProductInfoRow{
		{ProductCode: &productCode, PriceList: &priceList, CostRate: &costRate,
			ProductID: &productID, ProductColorID: &productColorID, ProductAliasID: &productAliasID},
		{},
	}
	result, err = service.Query(t.Context(), 17, requestbody.OpenBojunProductInfoQueryRequest{PageSize: &size})
	if err != nil || len(result.Items) != 1 || !result.Pagination.HasMore {
		t.Fatalf("extra-row pagination result=%+v error=%v", result, err)
	}
	if result.Items[0].ProductCode == nil || *result.Items[0].ProductCode != productCode ||
		result.Items[0].PriceList == nil || *result.Items[0].PriceList != priceList ||
		result.Items[0].ProductID == nil || *result.Items[0].ProductID != productID ||
		result.Items[0].ProductColorID == nil || *result.Items[0].ProductColorID != productColorID ||
		result.Items[0].ProductAliasID == nil || *result.Items[0].ProductAliasID != productAliasID ||
		result.Items[0].CostRate == nil || *result.Items[0].CostRate != costRate || result.Items[0].ColorName != nil {
		t.Fatalf("decimal, identifier or NULL changed: %+v", result.Items[0])
	}
	encoded, err = json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"priceList":"`+priceList+`"`) ||
		!strings.Contains(string(encoded), `"costRate":"`+costRate+`"`) || !strings.Contains(string(encoded), `"colorName":null`) {
		t.Fatalf("public values changed: JSON=%s error=%v", encoded, err)
	}
	for _, field := range []struct{ name, value string }{
		{"productId", productID}, {"productColorId", productColorID}, {"productAliasId", productAliasID},
	} {
		if !strings.Contains(string(encoded), `"`+field.name+`":"`+field.value+`"`) {
			t.Fatalf("ID did not remain a JSON string: %s", encoded)
		}
	}
	oracle.productInfos = oracle.productInfos[:1]
	result, err = service.Query(t.Context(), 17, requestbody.OpenBojunProductInfoQueryRequest{PageSize: &size})
	if err != nil || len(result.Items) != 1 || result.Pagination.HasMore || opened != 1 {
		t.Fatalf("last page result=%+v opened=%d error=%v", result, opened, err)
	}
	oracle.productInfos = []reportoracle.BojunProductInfoRow{{}}
	result, err = service.Query(t.Context(), 17, requestbody.OpenBojunProductInfoQueryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"productId", "productColorId", "productAliasId"} {
		if !strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Fatalf("NULL ID was not preserved in JSON: %s", encoded)
		}
	}
}

func TestOpenBojunProductInfoQueryNormalizesBarcodesWithoutMutatingRequest(t *testing.T) {
	page, size := 1001, 100
	request := requestbody.OpenBojunProductInfoQueryRequest{
		ProductCode: []string{" 001AbC ", "002AbC", "001AbC", "001abc"},
		Page:        &page,
		PageSize:    &size,
	}
	original := append([]string(nil), request.ProductCode...)
	oracle := &fakeOpenBojunOracle{}
	opened := 0
	service := newOpenBojunProductInfoQueryService(newTestBojunProvider(oracle, &opened), &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
	result, err := service.Query(t.Context(), 17, request)
	want := reportoracle.BojunProductInfoQuery{ProductCode: []string{"001AbC", "002AbC", "001abc"}, PageSize: 100, Offset: 100000}
	if err != nil || result.Pagination.Page != 1001 || result.Pagination.PageSize != 100 ||
		!reflect.DeepEqual(oracle.productInfoQuery, want) || !reflect.DeepEqual(request.ProductCode, original) ||
		page != 1001 || size != 100 || oracle.productInfoCalls != 1 {
		t.Fatalf("result=%+v query=%+v request=%+v error=%v", result, oracle.productInfoQuery, request, err)
	}
	request.ProductCode = nil
	result, err = service.Query(t.Context(), 17, request)
	if err != nil || result.Pagination.Page != 1001 ||
		!reflect.DeepEqual(oracle.productInfoQuery, reportoracle.BojunProductInfoQuery{PageSize: 100, Offset: 100000}) {
		t.Fatalf("full pagination beyond page 1000 failed: result=%+v query=%+v error=%v", result, oracle.productInfoQuery, err)
	}
}

func TestOpenBojunProductInfoQueryEnforcesProductListLimits(t *testing.T) {
	for _, filter := range []struct {
		name    string
		limit   int
		request func([]string) requestbody.OpenBojunProductInfoQueryRequest
	}{
		{"barcode", 100, func(values []string) requestbody.OpenBojunProductInfoQueryRequest {
			return requestbody.OpenBojunProductInfoQueryRequest{ProductCode: values}
		}},
		{"color", 20, func(values []string) requestbody.OpenBojunProductInfoQueryRequest {
			return requestbody.OpenBojunProductInfoQueryRequest{ProductColor: values}
		}},
		{"name", 20, func(values []string) requestbody.OpenBojunProductInfoQueryRequest {
			return requestbody.OpenBojunProductInfoQueryRequest{ProductName: values}
		}},
	} {
		for _, batch := range []struct {
			name       string
			count      int
			duplicates bool
		}{
			{"maximum", filter.limit, false},
			{"over maximum", filter.limit + 1, false},
			{"duplicates over maximum", filter.limit + 1, true},
		} {
			t.Run(filter.name+"/"+batch.name, func(t *testing.T) {
				values := make([]string, batch.count)
				for index := range values {
					values[index] = fmt.Sprintf("%03dVALUE", index)
					if batch.duplicates {
						values[index] = " SAME "
					}
				}
				oracle := &fakeOpenBojunOracle{}
				opened := 0
				service := newOpenBojunProductInfoQueryService(newTestBojunProvider(oracle, &opened), &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
				result, err := service.Query(t.Context(), 17, filter.request(values))
				if batch.count > filter.limit {
					if !errors.Is(err, ErrOpenBojunProductInfoInvalidQuery) || result != nil || opened != 0 || oracle.productInfoCalls != 0 {
						t.Fatalf("oversized batch reached Oracle: result=%+v error=%v opened=%d calls=%d", result, err, opened, oracle.productInfoCalls)
					}
					return
				}
				query := oracle.productInfoQuery
				if err != nil || opened != 1 || oracle.productInfoCalls != 1 ||
					len(query.ProductCode)+len(query.ProductColor)+len(query.ProductName) != filter.limit {
					t.Fatalf("valid batch failed: error=%v opened=%d calls=%d query=%+v", err, opened, oracle.productInfoCalls, query)
				}
			})
		}
	}
}

func TestOpenBojunProductInfoQueryRejectsInvalidFiltersBeforeOracle(t *testing.T) {
	zero, largeSize := 0, 101
	for _, test := range []struct {
		name    string
		request requestbody.OpenBojunProductInfoQueryRequest
	}{
		{"barcode and color", requestbody.OpenBojunProductInfoQueryRequest{ProductCode: []string{"SKU"}, ProductColor: []string{"COLOR"}}},
		{"barcode and name", requestbody.OpenBojunProductInfoQueryRequest{ProductCode: []string{"SKU"}, ProductName: []string{"STYLE"}}},
		{"color and name", requestbody.OpenBojunProductInfoQueryRequest{ProductColor: []string{"COLOR"}, ProductName: []string{"STYLE"}}},
		{"blank barcode", requestbody.OpenBojunProductInfoQueryRequest{ProductCode: []string{" "}}},
		{"blank color", requestbody.OpenBojunProductInfoQueryRequest{ProductColor: []string{"\t"}}},
		{"blank name", requestbody.OpenBojunProductInfoQueryRequest{ProductName: []string{"STYLE", ""}}},
		{"long product value", requestbody.OpenBojunProductInfoQueryRequest{ProductName: []string{strings.Repeat("款", 27)}}},
		{"zero page", requestbody.OpenBojunProductInfoQueryRequest{Page: &zero}},
		{"zero page size", requestbody.OpenBojunProductInfoQueryRequest{PageSize: &zero}},
		{"large page size", requestbody.OpenBojunProductInfoQueryRequest{PageSize: &largeSize}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oracle := &fakeOpenBojunOracle{}
			opened := 0
			service := newOpenBojunProductInfoQueryService(newTestBojunProvider(oracle, &opened), &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
			result, err := service.Query(t.Context(), 17, test.request)
			if !errors.Is(err, ErrOpenBojunProductInfoInvalidQuery) || result != nil || opened != 0 || oracle.productInfoCalls != 0 {
				t.Fatalf("invalid request reached Oracle: result=%+v error=%v opened=%d calls=%d", result, err, opened, oracle.productInfoCalls)
			}
		})
	}
}

func TestOpenBojunProductInfoQuerySharesStoreStockConnection(t *testing.T) {
	oracle := &fakeOpenBojunOracle{}
	opened := 0
	provider := newTestBojunProvider(oracle, &opened)
	permission := &fakeOpenBojunProductPermissionReader{allowed: true}
	if _, err := newOpenBojunStoreQueryService(provider, permission, time.Now).Query(t.Context(), 17, requestbody.OpenBojunStoreQueryRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := newOpenBojunStockQueryService(provider, permission, time.Now).Query(t.Context(), 17, requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: []string{"SKU"}}); err != nil {
		t.Fatal(err)
	}
	service := newOpenBojunProductInfoQueryService(provider, permission, time.Now)
	for range 2 {
		if _, err := service.Query(t.Context(), 17, requestbody.OpenBojunProductInfoQueryRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if opened != 1 || oracle.productInfoCalls != 2 || oracle.stockCalls != 1 || permission.permission != model.PermissionBojunOrderRead {
		t.Fatalf("pool reuse failed: opened=%d productInfoCalls=%d stockCalls=%d permission=%s", opened, oracle.productInfoCalls, oracle.stockCalls, permission.permission)
	}
}

func TestOpenBojunProductInfoQueryClassifiesAuthorizationAndOracleErrors(t *testing.T) {
	now := time.Date(2026, time.October, 26, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	for _, test := range []struct {
		name    string
		actor   uint
		allowed bool
		stage   string
		wantErr error
	}{
		{"no actor", 0, true, "", ErrOpenBojunProductInfoForbidden},
		{"not granted", 17, false, "", ErrOpenBojunProductInfoForbidden},
		{"permission failure", 17, true, "permission", ErrOpenBojunProductInfoUnavailable},
		{"configuration failure", 17, true, "configuration", ErrOpenBojunProductInfoUnavailable},
		{"connection failure", 17, true, "connection", ErrOpenBojunProductInfoUnavailable},
		{"query failure", 17, true, "query", ErrOpenBojunProductInfoUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New("dependency unavailable")
			oracle := &fakeOpenBojunOracle{}
			opened := 0
			provider := newTestBojunProvider(oracle, &opened)
			permission := &fakeOpenBojunProductPermissionReader{allowed: test.allowed}
			switch test.stage {
			case "permission":
				permission.err = failure
			case "configuration":
				provider.configErr = failure
			case "connection":
				provider.open = func(context.Context, reportoracle.Config) (openBojunOracle, error) {
					opened++
					return nil, failure
				}
			case "query":
				oracle.err = failure
			}
			service := newOpenBojunProductInfoQueryService(provider, permission, func() time.Time { return now })
			result, err := service.Query(t.Context(), test.actor, requestbody.OpenBojunProductInfoQueryRequest{})
			if result != nil || !errors.Is(err, test.wantErr) || (test.stage != "" && !errors.Is(err, failure)) {
				t.Fatalf("result=%+v error=%v want=%v", result, err, test.wantErr)
			}
			wantOpened, wantQueries := 0, 0
			if test.stage == "connection" || test.stage == "query" {
				wantOpened = 1
			}
			if test.stage == "query" {
				wantQueries = 1
			}
			if opened != wantOpened || oracle.productInfoCalls != wantQueries {
				t.Fatalf("Oracle used before permission or dependency failure: opened=%d calls=%d", opened, oracle.productInfoCalls)
			}
			if test.actor != 0 && (permission.actor != test.actor || permission.permission != model.PermissionBojunOrderRead || !permission.at.Equal(now.UTC())) {
				t.Fatalf("unexpected permission check: %+v", permission)
			}
		})
	}
}

func TestOpenBojunProductInfoQuerySharesDeadlineAcrossConnectionAndQuery(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{"default maximum", 0, 10 * time.Second},
		{"configured shorter", 2 * time.Second, 2 * time.Second},
		{"configured longer", 30 * time.Second, 10 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := openBojunProductOracleConfig()
			config.QueryTimeout = test.configured
			oracle := &fakeOpenBojunOracle{}
			var openCtx context.Context
			provider := newOpenBojunOracleProvider(config, func(ctx context.Context, _ reportoracle.Config) (openBojunOracle, error) {
				openCtx = ctx
				return oracle, nil
			})
			service := newOpenBojunProductInfoQueryService(provider, &fakeOpenBojunProductPermissionReader{allowed: true}, time.Now)
			started := time.Now()
			_, err := service.Query(context.Background(), 17, requestbody.OpenBojunProductInfoQueryRequest{})
			finished := time.Now()
			if err != nil || openCtx != oracle.ctx || openCtx == nil {
				t.Fatalf("connection/query context changed or query failed: error=%v", err)
			}
			deadline, ok := oracle.ctx.Deadline()
			if !ok || deadline.Before(started.Add(test.want)) || deadline.After(finished.Add(test.want)) {
				t.Fatalf("deadline=%v expected timeout=%s", deadline, test.want)
			}
		})
	}
}
