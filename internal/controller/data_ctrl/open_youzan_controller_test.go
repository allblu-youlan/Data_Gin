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
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenYouzanQueryService struct {
	actor   uint
	dataset reportoracle.YouzanDataset
	calls   int
	err     error
	request requestbody.OpenYouzanQueryRequest
}

func (service *fakeOpenYouzanQueryService) Query(_ context.Context, actor uint, dataset reportoracle.YouzanDataset, request requestbody.OpenYouzanQueryRequest) (*data_svc.OpenYouzanQueryDTO, error) {
	service.actor, service.dataset = actor, dataset
	service.request = request
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	return &data_svc.OpenYouzanQueryDTO{
		Items:      []reportoracle.YouzanRow{},
		Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50},
	}, nil
}

func TestOpenYouzanControllerSelectsDatasetAndReturnsEnvelope(t *testing.T) {
	page, pageSize := 2, 25
	wantRequest := requestbody.OpenYouzanQueryRequest{
		StartTime: "2026-10-01 00:00:00", EndTime: "2026-10-02 00:00:00", TimeField: "created",
		Filters: map[string]string{"TID": "E001"}, Page: &page, PageSize: &pageSize,
	}
	for _, test := range []struct {
		path    string
		dataset reportoracle.YouzanDataset
	}{
		{"orders", reportoracle.YouzanOrders},
		{"orders/items", reportoracle.YouzanOrderItems},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := &fakeOpenYouzanQueryService{}
			recorder := performOpenYouzanControllerRequest(service, "/api/open/youzan/"+test.path+"/query", `{"startTime":"2026-10-01 00:00:00","endTime":"2026-10-02 00:00:00","timeField":"created","filters":{"TID":"E001"},"page":2,"pageSize":25}`)
			if recorder.Code != http.StatusOK || service.calls != 1 || service.actor != 17 || service.dataset != test.dataset || !reflect.DeepEqual(service.request, wantRequest) {
				t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
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
			if response.Code != 0 || response.Data.Items == nil || strings.Contains(recorder.Body.String(), `"columns"`) ||
				response.Data.Pagination != (data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50}) {
				t.Fatalf("unexpected response envelope: %s", recorder.Body.String())
			}
		})
	}
}

func TestOpenYouzanControllerRejectsInvalidJSONBeforeService(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"unknown field", "", `{"unknown":true}`},
		{"query parameters", "?page=1", `{}`},
		{"empty query parameters", "?", `{}`},
		{"multiple objects", "", `{} {}`},
		{"numeric start date", "", `{"startTime":20261001}`},
		{"numeric filter value", "", `{"filters":{"TID":123}}`},
		{"array filters", "", `{"filters":["TID"]}`},
		{"fractional page", "", `{"page":1.5}`},
		{"array body", "", `[]`},
		{"null body", "", `null`},
		{"empty body", "", ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenYouzanQueryService{}
			recorder := performOpenYouzanControllerRequest(service, "/api/open/youzan/orders/query"+test.query, test.body)
			if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 || !strings.Contains(recorder.Body.String(), `"code":100422`) {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenYouzanControllerReturnsSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", fmt.Errorf("private detail: %w", data_svc.ErrOpenYouzanForbidden), http.StatusForbidden},
		{"invalid", fmt.Errorf("private detail: %w", data_svc.ErrOpenYouzanInvalidQuery), http.StatusUnprocessableEntity},
		{"unavailable", fmt.Errorf("private detail: %w", data_svc.ErrOpenYouzanUnavailable), http.StatusServiceUnavailable},
		{"unexpected", errors.New("private detail: Oracle password and SQL"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenYouzanQueryService{err: test.err}
			recorder := performOpenYouzanControllerRequest(service, "/api/open/youzan/orders/query", `{}`)
			var response struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status || response.Code != 100000+test.status || response.Msg == "" ||
				string(response.Data) != "null" || strings.Contains(recorder.Body.String(), "private detail") {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func performOpenYouzanControllerRequest(service OpenYouzanQueryService, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	controller := NewOpenYouzanControllerWithService(service)
	router.POST("/api/open/youzan/orders/query", controller.QueryOrders)
	router.POST("/api/open/youzan/orders/items/query", controller.QueryOrderItems)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
