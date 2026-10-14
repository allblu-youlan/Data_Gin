package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenBojunStoreQueryService struct {
	request requestbody.OpenBojunStoreQueryRequest
	actor   uint
	calls   int
	err     error
}

func (f *fakeOpenBojunStoreQueryService) Query(_ context.Context, actor uint, request requestbody.OpenBojunStoreQueryRequest) (*data_svc.OpenBojunStoreQueryDTO, error) {
	f.request, f.actor = request, actor
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &data_svc.OpenBojunStoreQueryDTO{Items: []data_svc.OpenBojunStoreDTO{{StoreID: 1, StoreCode: request.StoreCode, StoreName: "仓", IsActive: "Y"}}, Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50}}, nil
}

type fakeOpenBojunStockQueryService struct {
	request requestbody.OpenBojunStockQueryRequest
	actor   uint
	calls   int
	err     error
}

func (f *fakeOpenBojunStockQueryService) Query(_ context.Context, actor uint, request requestbody.OpenBojunStockQueryRequest) (*data_svc.OpenBojunStockQueryDTO, error) {
	f.request, f.actor = request, actor
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	qty := "-123456789012345678.1234567890"
	return &data_svc.OpenBojunStockQueryDTO{Items: []data_svc.OpenBojunStockDTO{{StoreCode: request.StoreCode, ProductCode: request.ProductCode, Qty: &qty}}, Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50}}, nil
}

func newBojunStoreStockControllerRouter(store *fakeOpenBojunStoreQueryService, stock *fakeOpenBojunStockQueryService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17"); c.Next() })
	router.POST("/stores", NewOpenBojunStoreControllerWithService(store).Query)
	router.POST("/stocks", NewOpenBojunStockControllerWithService(stock).Query)
	return router
}
func performBojunQuery(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}
func TestOpenBojunStoreStockControllersEnforceStrictJSON(t *testing.T) {
	for _, path := range []string{"/stores", "/stocks"} {
		for _, tt := range []struct{ name, query, body string }{
			{"unknown field", "", `{"unknown":true}`},
			{"URL query", "?storeCode=001", `{}`},
			{"malformed URL query", "?bad=%zz", `{}`},
			{"multiple objects", "", `{} {}`},
			{"numeric code", "", `{"storeCode":1}`},
			{"array code", "", `{"storeCode":["001"]}`},
			{"fractional page", "", `{"page":1.5}`},
			{"string page", "", `{"page":"1"}`},
			{"top level array", "", `[]`},
			{"null body", "", `null`},
			{"empty body", "", ``},
			{"oversized", "", `{"storeCode":"` + strings.Repeat("x", 1024*1024) + `"}`},
		} {
			t.Run(path+"/"+tt.name, func(t *testing.T) {
				store, stock := &fakeOpenBojunStoreQueryService{}, &fakeOpenBojunStockQueryService{}
				w := performBojunQuery(newBojunStoreStockControllerRouter(store, stock), path+tt.query, tt.body)
				if w.Code != 422 || store.calls+stock.calls != 0 || !strings.Contains(w.Body.String(), `"code":100422`) {
					t.Fatalf("status=%d calls=%d body=%s", w.Code, store.calls+stock.calls, w.Body.String())
				}
			})
		}
	}
}

func TestOpenBojunStoreStockControllersReturnExactPublicFields(t *testing.T) {
	store, stock := &fakeOpenBojunStoreQueryService{}, &fakeOpenBojunStockQueryService{}
	router := newBojunStoreStockControllerRouter(store, stock)
	for _, tt := range []struct {
		path, body string
		fields     []string
	}{
		{"/stores", `{"storeCode":"001","isActive":null,"isStop":null,"page":null,"pageSize":null}`, []string{"storeId", "storeCode", "storeName", "isActive", "isStop"}},
		{"/stocks", `{"storeCode":"001","productCode":"AbC","page":2,"pageSize":100}`, []string{"storeCode", "storeName", "productCode", "productColor", "productName", "value", "value1", "sizeCode", "value2", "priceList", "qty", "amtPriceQty", "qtyBas", "qtyPreOut", "qtyBasPreOut", "amtPriceQtyPreOut", "qtyPreIn", "amtPriceQtyPreIn", "qtyFreeze", "qtyOms", "qtyValid", "amtPriceQtyValid", "qtyConsign", "jstPreOut", "jstDiffQty", "qtyCan", "attributeSetInstanceId", "isActive"}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := performBojunQuery(router, tt.path, tt.body)
			if w.Code != 200 || w.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body struct {
				Code int `json:"code"`
				Data struct {
					Items      []map[string]any `json:"items"`
					Pagination map[string]any   `json:"pagination"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != 0 || len(body.Data.Items) != 1 || len(body.Data.Items[0]) != len(tt.fields) || len(body.Data.Pagination) != 3 {
				t.Fatalf("unexpected envelope: %s", w.Body.String())
			}
			for _, field := range tt.fields {
				if _, ok := body.Data.Items[0][field]; !ok {
					t.Fatalf("missing %s", field)
				}
			}
			if body.Data.Items[0]["storeCode"] != "001" {
				t.Fatal("lost leading zero")
			}
			if tt.path == "/stocks" && (body.Data.Items[0]["qty"] != "-123456789012345678.1234567890" || body.Data.Items[0]["qtyOms"] != nil) {
				t.Fatal("lost decimal precision or null")
			}
		})
	}
	if store.actor != 17 || stock.actor != 17 || store.request.Page != nil || stock.request.Page == nil || *stock.request.Page != 2 || *stock.request.PageSize != 100 {
		t.Fatalf("store=%+v stock=%+v", store, stock)
	}
}

func TestOpenBojunStoreStockControllersMapSafeErrors(t *testing.T) {
	for _, tt := range []struct {
		name               string
		storeErr, stockErr error
		status             int
	}{
		{"forbidden", data_svc.ErrOpenBojunStoreForbidden, data_svc.ErrOpenBojunStockForbidden, 403},
		{"invalid", data_svc.ErrOpenBojunStoreInvalidQuery, data_svc.ErrOpenBojunStockInvalidQuery, 422},
		{"unavailable", errors.New("Oracle password and raw SQL must stay private"), errors.New("Oracle password and raw SQL must stay private"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := newBojunStoreStockControllerRouter(&fakeOpenBojunStoreQueryService{err: tt.storeErr}, &fakeOpenBojunStockQueryService{err: tt.stockErr})
			for _, path := range []string{"/stores", "/stocks"} {
				w := performBojunQuery(router, path, `{}`)
				if w.Code != tt.status || strings.Contains(w.Body.String(), "Oracle") || !strings.Contains(w.Body.String(), `"data":null`) {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestOpenBojunStockControllerParsesProductAlternatives(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		want       requestbody.OpenBojunStockQueryRequest
	}{
		{"code", `{"storeCode":"001","productCode":"SKU","productColor":null,"productName":null}`, requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductCode: "SKU"}},
		{"color", `{"storeCode":"001","productCode":null,"productColor":"C44G3063CE146","productName":null}`, requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductColor: "C44G3063CE146"}},
		{"name", `{"storeCode":"001","productName":"C44G3063C"}`, requestbody.OpenBojunStockQueryRequest{StoreCode: "001", ProductName: "C44G3063C"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stock := &fakeOpenBojunStockQueryService{}
			response := performBojunQuery(newBojunStoreStockControllerRouter(&fakeOpenBojunStoreQueryService{}, stock), "/stocks", tt.body)
			if response.Code != http.StatusOK || stock.calls != 1 || stock.request != tt.want {
				t.Fatalf("status=%d calls=%d request=%+v want=%+v", response.Code, stock.calls, stock.request, tt.want)
			}
		})
	}
	for _, body := range []string{`{"storeCode":"001","productColor":123}`, `{"storeCode":"001","productName":["C44G3063C"]}`} {
		stock := &fakeOpenBojunStockQueryService{}
		response := performBojunQuery(newBojunStoreStockControllerRouter(&fakeOpenBojunStoreQueryService{}, stock), "/stocks", body)
		if response.Code != http.StatusUnprocessableEntity || stock.calls != 0 {
			t.Fatalf("invalid product alternative status=%d calls=%d", response.Code, stock.calls)
		}
	}
}
