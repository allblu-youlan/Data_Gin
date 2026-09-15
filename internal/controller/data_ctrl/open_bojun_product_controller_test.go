package data_ctrl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenBojunProductQueryService struct {
	request requestbody.OpenBojunProductQueryRequest
	actor   uint
	calls   int
	err     error
}

func (service *fakeOpenBojunProductQueryService) Query(
	_ context.Context,
	actor uint,
	request requestbody.OpenBojunProductQueryRequest,
) (*data_svc.OpenBojunProductDTO, error) {
	service.actor, service.request = actor, request
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	return &data_svc.OpenBojunProductDTO{
		PriceList: "179", ProductName: "C17F2021", Value: "儿童|半裙",
		ProductCode: request.ProductCode, ProductColor: "C17F2021H063",
		Value1: "甜蜜薰衣草", Value2: "090",
	}, nil
}

func TestOpenBojunProductControllerParsesStrictJSON(t *testing.T) {
	service := &fakeOpenBojunProductQueryService{}
	recorder := performOpenBojunProductControllerRequest(t, service, "/api/open/bojun/products/query", `{"productCode":"C17F2021H063090"}`)
	if recorder.Code != http.StatusOK || service.calls != 1 || service.actor != 17 ||
		service.request.ProductCode != "C17F2021H063090" {
		t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
	}
	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"priceList", "productName", "value", "productCode", "productColor", "value1", "value2"} {
		if _, ok := response.Data[field]; !ok {
			t.Fatalf("response is missing %q: %s", field, recorder.Body.String())
		}
	}
	for _, internalID := range []string{"mProductColorId", "mProductId", "mProductAliasId", "mAttributeSetInstanceId"} {
		if _, ok := response.Data[internalID]; ok {
			t.Fatalf("response exposes internal field %q: %s", internalID, recorder.Body.String())
		}
	}
}

func TestOpenBojunProductControllerRejectsUnknownFieldsAndQueryParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "unknown field", path: "/api/open/bojun/products/query", body: `{"productCode":"C17F2021H063090","unknown":true}`},
		{name: "query parameter", path: "/api/open/bojun/products/query?no=C17F2021H063090", body: `{"productCode":"C17F2021H063090"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeOpenBojunProductQueryService{}
			recorder := performOpenBojunProductControllerRequest(t, service, tt.path, tt.body)
			if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 || strings.Contains(recorder.Body.String(), "unknown") {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenBojunProductControllerMapsNotFound(t *testing.T) {
	service := &fakeOpenBojunProductQueryService{err: data_svc.ErrOpenBojunProductNotFound}
	recorder := performOpenBojunProductControllerRequest(t, service, "/api/open/bojun/products/query", `{"productCode":"C17F2021H063090"}`)
	if recorder.Code != http.StatusNotFound || service.calls != 1 || strings.Contains(recorder.Body.String(), "open bojun") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
	}
}

func performOpenBojunProductControllerRequest(
	t *testing.T,
	service OpenBojunProductQueryService,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	controller := NewOpenBojunProductControllerWithService(service)
	router.POST("/api/open/bojun/products/query", controller.Query)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
