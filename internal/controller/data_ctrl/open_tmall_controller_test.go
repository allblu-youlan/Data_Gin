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

type fakeOpenTmallQueryService struct {
	actor   uint
	dataset reportoracle.TmallDataset
	calls   int
	err     error
	request requestbody.OpenTmallQueryRequest
}

func (service *fakeOpenTmallQueryService) Query(_ context.Context, actor uint, dataset reportoracle.TmallDataset, request requestbody.OpenTmallQueryRequest) (*data_svc.OpenTmallQueryDTO, error) {
	service.actor, service.dataset = actor, dataset
	service.request = request
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	return &data_svc.OpenTmallQueryDTO{
		Items:      []reportoracle.TmallRow{},
		Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50},
	}, nil
}

func TestOpenTmallControllerSelectsDatasetAndReturnsEnvelope(t *testing.T) {
	page, pageSize := 2, 25
	wantRequest := requestbody.OpenTmallQueryRequest{
		StartDate: "2026-10-01", EndDate: "2026-10-31",
		Filters: map[string]string{"BATCH_ID": "batch-20261001"}, Page: &page, PageSize: &pageSize,
	}
	for _, test := range []struct {
		path    string
		dataset reportoracle.TmallDataset
	}{
		{"sycm", reportoracle.TmallSycm},
		{"qztg", reportoracle.TmallQztg},
		{"tot", reportoracle.TmallTot},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/"+test.path+"/query", `{"startDate":"2026-10-01","endDate":"2026-10-31","filters":{"BATCH_ID":"batch-20261001"},"page":2,"pageSize":25}`)
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

func TestOpenTmallControllerRejectsInvalidJSONBeforeService(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"unknown field", "", `{"unknown":true}`},
		{"query parameters", "?page=1", `{}`},
		{"empty query parameters", "?", `{}`},
		{"multiple objects", "", `{} {}`},
		{"numeric start date", "", `{"startDate":20261001}`},
		{"numeric filter value", "", `{"filters":{"BATCH_ID":123}}`},
		{"array filters", "", `{"filters":["BATCH_ID"]}`},
		{"fractional page", "", `{"page":1.5}`},
		{"array body", "", `[]`},
		{"null body", "", `null`},
		{"empty body", "", ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/query"+test.query, test.body)
			if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 || !strings.Contains(recorder.Body.String(), `"code":100422`) {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenTmallControllerReturnsSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", fmt.Errorf("private detail: %w", data_svc.ErrOpenTmallForbidden), http.StatusForbidden},
		{"invalid", fmt.Errorf("private detail: %w", data_svc.ErrOpenTmallInvalidQuery), http.StatusUnprocessableEntity},
		{"unavailable", fmt.Errorf("private detail: %w", data_svc.ErrOpenTmallUnavailable), http.StatusServiceUnavailable},
		{"unexpected", errors.New("private detail: Oracle password and SQL"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{err: test.err}
			recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/query", `{}`)
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

func performOpenTmallControllerRequest(service OpenTmallQueryService, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	controller := NewOpenTmallControllerWithService(service)
	router.POST("/api/open/tmall/orders/realtime/query", controller.QueryOrdersRealtime)
	router.POST("/api/open/tmall/sycm/query", controller.QuerySycm)
	router.POST("/api/open/tmall/qztg/query", controller.QueryQztg)
	router.POST("/api/open/tmall/tot/query", controller.QueryTot)
	router.POST("/api/open/tmall/sycm/hourly/query", controller.QuerySycmHourly)
	router.POST("/api/open/tmall/qztg/hourly/query", controller.QueryQztgHourly)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
