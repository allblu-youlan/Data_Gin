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

type fakeOpenMonitoringQueryService struct {
	hourlyRequest requestbody.OpenMonitoringHourlyQueryRequest
	areaRequest   requestbody.OpenMonitoringAreaQueryRequest
	actor         uint
	operation     string
	calls         int
	err           error
}

func (service *fakeOpenMonitoringQueryService) QueryHourly(_ context.Context, actor uint, request requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringHourlyResult, error) {
	service.actor, service.hourlyRequest, service.operation = actor, request, "hourly"
	service.calls++
	return &data_svc.OpenMonitoringHourlyResult{}, service.err
}

func (service *fakeOpenMonitoringQueryService) QueryDaily(_ context.Context, actor uint, request requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringDailyResult, error) {
	service.actor, service.hourlyRequest, service.operation = actor, request, "daily"
	service.calls++
	return &data_svc.OpenMonitoringDailyResult{}, service.err
}

func (service *fakeOpenMonitoringQueryService) QueryAreas(_ context.Context, actor uint, request requestbody.OpenMonitoringAreaQueryRequest) (*data_svc.OpenMonitoringAreaResult, error) {
	service.actor, service.areaRequest, service.operation = actor, request, "areas"
	service.calls++
	return &data_svc.OpenMonitoringAreaResult{}, service.err
}

func newOpenMonitoringControllerRouter(service *fakeOpenMonitoringQueryService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	controller := NewOpenMonitoringControllerWithService(service)
	router.POST("/hourly", controller.QueryHourly)
	router.POST("/daily", controller.QueryDaily)
	router.POST("/areas", controller.QueryAreas)
	return router
}

func performOpenMonitoringQuery(router *gin.Engine, path, body, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestOpenMonitoringControllerPassesTypedRequestsAndActor(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		hourly           requestbody.OpenMonitoringHourlyQueryRequest
		area             requestbody.OpenMonitoringAreaQueryRequest
	}{
		{
			name: "hourly date list", path: "hourly", body: `{"shopCode":"001","dates":["2026-11-10","2026-11-12"]}`,
			hourly: requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: "001", Dates: []string{"2026-11-10", "2026-11-12"}},
		},
		{
			name: "daily range", path: "daily", body: `{"shopCode":"001","startDate":"2026-11-10","endDate":"2026-11-12"}`,
			hourly: requestbody.OpenMonitoringHourlyQueryRequest{ShopCode: "001", StartDate: "2026-11-10", EndDate: "2026-11-12"},
		},
		{
			name: "areas filter", path: "areas", body: `{"shopCode":"001","date":"2026-11-12","regionName":"入口"}`,
			area: requestbody.OpenMonitoringAreaQueryRequest{ShopCode: "001", Date: "2026-11-12", RegionName: "入口"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenMonitoringQueryService{}
			response := performOpenMonitoringQuery(newOpenMonitoringControllerRouter(service), "/"+test.path, test.body, "application/json")
			if response.Code != http.StatusOK || service.calls != 1 || service.actor != 17 || service.operation != test.path {
				t.Fatalf("status=%d calls=%d actor=%d operation=%s body=%s", response.Code, service.calls, service.actor, service.operation, response.Body)
			}
			if !reflect.DeepEqual(service.hourlyRequest, test.hourly) || !reflect.DeepEqual(service.areaRequest, test.area) {
				t.Fatalf("hourly=%+v area=%+v", service.hourlyRequest, service.areaRequest)
			}
			var envelope struct {
				Code int            `json:"code"`
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Code != 0 || envelope.Data == nil {
				t.Fatalf("unexpected public response: %s", response.Body)
			}
		})
	}
}

func TestOpenMonitoringControllerRejectsInvalidTransportInput(t *testing.T) {
	for _, path := range []string{"/hourly", "/daily", "/areas"} {
		for _, test := range []struct {
			name, query, body, contentType string
		}{
			{"unknown field", "", `{"unknown":true}`, "application/json"},
			{"query parameters", "?shopCode=001", `{}`, "application/json"},
			{"empty query", "?", `{}`, "application/json"},
			{"multiple objects", "", `{} {}`, "application/json"},
			{"numeric code", "", `{"shopCode":1}`, "application/json"},
			{"array body", "", `[]`, "application/json"},
			{"null body", "", `null`, "application/json"},
			{"empty body", "", ``, "application/json"},
			{"wrong content type", "", `{}`, "text/plain"},
		} {
			t.Run(path+"/"+test.name, func(t *testing.T) {
				service := &fakeOpenMonitoringQueryService{}
				response := performOpenMonitoringQuery(newOpenMonitoringControllerRouter(service), path+test.query, test.body, test.contentType)
				if response.Code != http.StatusUnprocessableEntity || service.calls != 0 || !strings.Contains(response.Body.String(), `"code":100422`) {
					t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body)
				}
			})
		}
	}
}

func TestOpenMonitoringControllerMapsSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"forbidden", data_svc.ErrOpenMonitoringForbidden, http.StatusForbidden},
		{"invalid", data_svc.ErrOpenMonitoringInvalidQuery, http.StatusUnprocessableEntity},
		{"unavailable", data_svc.ErrOpenMonitoringUnavailable, http.StatusServiceUnavailable},
		{"not found", data_svc.ErrOpenMonitoringNotFound, http.StatusNotFound},
		{"unexpected", errors.New("private SQL password"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenMonitoringQueryService{err: fmt.Errorf("private SQL password: %w", test.err)}
			router := newOpenMonitoringControllerRouter(service)
			for _, path := range []string{"/hourly", "/daily", "/areas"} {
				response := performOpenMonitoringQuery(router, path, `{}`, "application/json")
				if response.Code != test.status || strings.Contains(response.Body.String(), "private") || !strings.Contains(response.Body.String(), `"data":null`) {
					t.Fatalf("path=%s status=%d body=%s", path, response.Code, response.Body)
				}
			}
		})
	}
}
