package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

const openBojunProductInfoTestPath = "/api/open/bojun/product-infos/query"

type fakeOpenBojunProductInfoQueryService struct {
	request requestbody.OpenBojunProductInfoQueryRequest
	actor   uint
	calls   int
	err     error
}

func (service *fakeOpenBojunProductInfoQueryService) Query(_ context.Context, actor uint, request requestbody.OpenBojunProductInfoQueryRequest) (*data_svc.OpenBojunProductInfoQueryDTO, error) {
	service.request, service.actor = request, actor
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	return &data_svc.OpenBojunProductInfoQueryDTO{
		Items:      []data_svc.OpenBojunProductInfoDTO{},
		Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50},
	}, nil
}

func TestOpenBojunProductInfoControllerParsesProductFiltersAndFullQuery(t *testing.T) {
	page, pageSize := 2, 100
	for _, test := range []struct {
		name, body string
		want       requestbody.OpenBojunProductInfoQueryRequest
	}{
		{"full query", `{}`, requestbody.OpenBojunProductInfoQueryRequest{}},
		{"null filters", `{"productCode":null,"productColor":null,"productName":null,"page":null,"pageSize":null}`, requestbody.OpenBojunProductInfoQueryRequest{}},
		{"barcode", `{"productCode":["001SKU","002SKU"],"page":2,"pageSize":100}`, requestbody.OpenBojunProductInfoQueryRequest{ProductCode: []string{"001SKU", "002SKU"}, Page: &page, PageSize: &pageSize}},
		{"color", `{"productColor":["001COLOR","002COLOR"]}`, requestbody.OpenBojunProductInfoQueryRequest{ProductColor: []string{"001COLOR", "002COLOR"}}},
		{"name", `{"productName":["001STYLE","002STYLE"]}`, requestbody.OpenBojunProductInfoQueryRequest{ProductName: []string{"001STYLE", "002STYLE"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenBojunProductInfoQueryService{}
			recorder := performOpenBojunProductInfoControllerRequest(t, service, openBojunProductInfoTestPath, test.body)
			if recorder.Code != http.StatusOK || service.calls != 1 || service.actor != 17 || !reflect.DeepEqual(service.request, test.want) {
				t.Fatalf("status=%d service=%+v want=%+v body=%s", recorder.Code, service, test.want, recorder.Body.String())
			}
			if recorder.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
			var response struct {
				Code int `json:"code"`
				Data struct {
					Items      []json.RawMessage                 `json:"items"`
					Pagination data_svc.OpenBojunQueryPagination `json:"pagination"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != 0 || response.Data.Items == nil || len(response.Data.Items) != 0 ||
				response.Data.Pagination != (data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50}) {
				t.Fatalf("unexpected response envelope: %s", recorder.Body.String())
			}
		})
	}
}

func TestOpenBojunProductInfoControllerEnforcesStrictJSON(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"unknown field", "", `{"unknown":true}`},
		{"store filter", "", `{"storeCode":"001"}`},
		{"URL query", "?page=1", `{}`},
		{"empty URL query", "?", `{}`},
		{"multiple objects", "", `{} {}`},
		{"string barcode", "", `{"productCode":"SKU"}`},
		{"numeric color", "", `{"productColor":123}`},
		{"invalid array value", "", `{"productName":["STYLE",123]}`},
		{"fractional page", "", `{"page":1.5}`},
		{"top level array", "", `[]`},
		{"null body", "", `null`},
		{"empty body", "", ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenBojunProductInfoQueryService{}
			recorder := performOpenBojunProductInfoControllerRequest(t, service, openBojunProductInfoTestPath+test.query, test.body)
			if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 || !strings.Contains(recorder.Body.String(), `"code":100422`) {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenBojunProductInfoControllerMapsSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name, message string
		err           error
		status        int
	}{
		{"forbidden", "无权查询基础商品资料", fmt.Errorf("private detail: %w", data_svc.ErrOpenBojunProductInfoForbidden), http.StatusForbidden},
		{"invalid", "基础商品资料查询参数校验失败", fmt.Errorf("private detail: %w", data_svc.ErrOpenBojunProductInfoInvalidQuery), http.StatusUnprocessableEntity},
		{"unavailable", "基础商品资料查询服务暂时不可用", fmt.Errorf("private detail: %w", data_svc.ErrOpenBojunProductInfoUnavailable), http.StatusInternalServerError},
		{"unexpected", "基础商品资料查询服务暂时不可用", errors.New("private detail: Oracle password and SQL"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenBojunProductInfoQueryService{err: test.err}
			recorder := performOpenBojunProductInfoControllerRequest(t, service, openBojunProductInfoTestPath, `{}`)
			var response struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status || service.calls != 1 || response.Code != 100000+test.status ||
				response.Msg != test.message || string(response.Data) != "null" || strings.Contains(recorder.Body.String(), "private detail") {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func performOpenBojunProductInfoControllerRequest(t *testing.T, service OpenBojunProductInfoQueryService, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	router.POST(openBojunProductInfoTestPath, NewOpenBojunProductInfoControllerWithService(service).Query)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
