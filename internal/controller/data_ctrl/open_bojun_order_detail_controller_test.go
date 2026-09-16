package data_ctrl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenBojunOrderDetailQueryService struct {
	request requestbody.OpenBojunOrderDetailQueryRequest
	actor   uint
	result  *data_svc.OpenBojunOrderDetailQueryResult
	err     error
	calls   int
}

func (service *fakeOpenBojunOrderDetailQueryService) Query(
	_ context.Context,
	actor uint,
	request requestbody.OpenBojunOrderDetailQueryRequest,
) (*data_svc.OpenBojunOrderDetailQueryResult, error) {
	service.actor = actor
	service.request = request
	service.calls++
	if service.result == nil {
		service.result = &data_svc.OpenBojunOrderDetailQueryResult{
			OrderNo: "ORDER-1",
			Items:   []data_svc.OpenBojunOrderLineDTO{}, Payments: []data_svc.OpenBojunOrderPaymentDTO{},
		}
	}
	return service.result, service.err
}

func TestOpenBojunOrderDetailControllerParsesStrictJSON(t *testing.T) {
	service := &fakeOpenBojunOrderDetailQueryService{}
	recorder := performOpenBojunOrderDetailRequest(
		t, service, "/api/open/bojun/orders/details/query",
		`{"orderNo":"ORDER-1","pageSize":200,"cursor":"next"}`,
	)
	if recorder.Code != http.StatusOK || service.calls != 1 || service.actor != 17 ||
		service.request.OrderNo != "ORDER-1" || service.request.PageSize != 200 || service.request.Cursor != "next" {
		t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
	}
}

func TestOpenBojunOrderDetailControllerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "query parameters", path: "/api/open/bojun/orders/details/query?page=2", body: `{"orderNo":"ORDER-1"}`},
		{name: "unknown json field", path: "/api/open/bojun/orders/details/query", body: `{"orderNo":"ORDER-1","page":2}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenBojunOrderDetailQueryService{}
			recorder := performOpenBojunOrderDetailRequest(t, service, test.path, test.body)
			if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 ||
				strings.Contains(recorder.Body.String(), "page") {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenBojunOrderDetailControllerMapsNotFound(t *testing.T) {
	service := &fakeOpenBojunOrderDetailQueryService{err: data_svc.ErrOpenBojunOrderNotFound}
	recorder := performOpenBojunOrderDetailRequest(
		t, service, "/api/open/bojun/orders/details/query", `{"orderNo":"ORDER-404"}`,
	)
	if recorder.Code != http.StatusNotFound || service.calls != 1 ||
		strings.Contains(recorder.Body.String(), "open bojun") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
	}
}

func performOpenBojunOrderDetailRequest(
	t *testing.T,
	service OpenBojunOrderDetailQueryService,
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
	controller := NewOpenBojunOrderDetailControllerWithService(service)
	router.POST("/api/open/bojun/orders/details/query", controller.Query)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
