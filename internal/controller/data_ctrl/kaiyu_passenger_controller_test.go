package data_ctrl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type kaiyuControllerService struct {
	actor  uint
	query  data_svc.KaiyuPassengerQuery
	calls  int
	err    error
	cursor string
}

func (s *kaiyuControllerService) Missing(_ context.Context, actor uint, query data_svc.KaiyuPassengerQuery) (*data_svc.KaiyuPassengerMissingResult, error) {
	s.actor, s.query = actor, query
	s.calls++
	return &data_svc.KaiyuPassengerMissingResult{}, s.err
}
func (s *kaiyuControllerService) Backfill(_ context.Context, actor uint, input data_svc.KaiyuPassengerBackfillRequest) (*data_svc.KaiyuPassengerBackfillResult, error) {
	s.actor, s.query, s.cursor = actor, input.KaiyuPassengerQuery, input.Cursor
	s.calls++
	return &data_svc.KaiyuPassengerBackfillResult{}, s.err
}

func (s *kaiyuControllerService) Query(_ context.Context, actor uint, query data_svc.KaiyuPassengerQuery) (*data_svc.KaiyuPassengerResult, error) {
	s.actor, s.query = actor, query
	s.calls++
	return &data_svc.KaiyuPassengerResult{}, s.err
}
func (s *kaiyuControllerService) ListMalls(_ context.Context, actor uint) (*data_svc.KaiyuPassengerMallListResult, error) {
	s.actor = actor
	s.calls++
	return &data_svc.KaiyuPassengerMallListResult{Items: []data_svc.KaiyuPassengerMallOption{}}, s.err
}
func (s *kaiyuControllerService) Export(_ context.Context, actor uint, query data_svc.KaiyuPassengerQuery) (*data_svc.KaiyuPassengerExport, error) {
	s.actor, s.query = actor, query
	s.calls++
	return &data_svc.KaiyuPassengerExport{FileName: "kaiyu_passenger_2026-10-28_2026-10-28.xlsx", Content: []byte("PK\x03\x04workbook")}, s.err
}

func kaiyuControllerRequest(t *testing.T, service *kaiyuControllerService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17"); c.Next() })
	controller := NewKaiyuPassengerControllerWithService(service)
	router.GET("/passenger", controller.Query)
	router.GET("/passenger/malls", controller.ListMalls)
	router.POST("/passenger/export", controller.Export)
	router.GET("/passenger/missing", controller.Missing)
	router.POST("/passenger/backfill", controller.Backfill)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestKaiyuPassengerControllerPassesAuthenticatedActorAndFilters(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/passenger?startDate=2026-10-28&endDate=2026-10-28&mallId=7", ""},
		{http.MethodPost, "/passenger/export", `{"startDate":"2026-10-28","endDate":"2026-10-28","mallId":7}`},
		{http.MethodGet, "/passenger/missing?startDate=2026-10-28&endDate=2026-10-28&mallId=7", ""},
		{http.MethodPost, "/passenger/backfill", `{"startDate":"2026-10-28","endDate":"2026-10-28","mallId":7,"cursor":"2026-10-28:7"}`},
	} {
		service := &kaiyuControllerService{}
		recorder := kaiyuControllerRequest(t, service, tc.method, tc.path, tc.body)
		if recorder.Code != 200 || service.calls != 1 || service.actor != 17 || service.query.MallID != 7 || service.query.StartDate != "2026-10-28" || service.query.EndDate != "2026-10-28" {
			t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
		}
	}
}

func TestKaiyuPassengerControllerReturnsBinaryDownload(t *testing.T) {
	recorder := kaiyuControllerRequest(t, &kaiyuControllerService{}, http.MethodPost, "/passenger/export", `{"startDate":"2026-10-28","endDate":"2026-10-28"}`)
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || !strings.HasPrefix(recorder.Body.String(), "PK\x03\x04") || !strings.Contains(recorder.Header().Get("Content-Disposition"), ".xlsx") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestKaiyuPassengerControllerRejectsAmbiguousOrInvalidInput(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/passenger?startDate=2026-10-28", ""},
		{http.MethodGet, "/passenger?startDate=2026-10-28&endDate=2026-10-28&mallId=-1", ""},
		{http.MethodGet, "/passenger?startDate=2026-10-28&startDate=2026-10-29&endDate=2026-10-28", ""},
		{http.MethodGet, "/passenger?startDate=2026-10-28&endDate=2026-10-28&actor=1", ""},
		{http.MethodPost, "/passenger/export", `{"startDate":"2026-10-28","endDate":"2026-10-28","actor":1}`},
		{http.MethodPost, "/passenger/export?mallId=8", `{"startDate":"2026-10-28","endDate":"2026-10-28","mallId":7}`},
		{http.MethodPost, "/passenger/backfill", `{"startDate":"2026-10-28","endDate":"2026-10-28","actor":1}`},
		{http.MethodGet, "/passenger/missing?startDate=2026-10-28&endDate=2026-10-28&mallId=7&mallId=9", ""},
	} {
		service := &kaiyuControllerService{}
		recorder := kaiyuControllerRequest(t, service, tc.method, tc.path, tc.body)
		if recorder.Code != 422 || service.calls != 0 {
			t.Fatalf("path=%s status=%d calls=%d", tc.path, recorder.Code, service.calls)
		}
	}
}

func TestKaiyuPassengerControllerMapsErrorsBeforeBinaryHeaders(t *testing.T) {
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{auth_svc.ErrMallScopeForbidden, 403, "无权"},
		{data_svc.ErrKaiyuPassengerInvalid, 422, "31"},
		{data_svc.ErrKaiyuPassengerBackfillBusy, 409, "正在"},
		{reportoracle.ErrKaiyuPassengerTooManyRows, 422, "100000"},
		{fmt.Errorf("%w: password=hidden", data_svc.ErrKaiyuPassengerUnavailable), 503, "暂时"},
	} {
		service := &kaiyuControllerService{err: tc.err}
		recorder := kaiyuControllerRequest(t, service, http.MethodPost, "/passenger/export", `{"startDate":"2026-10-28","endDate":"2026-10-28"}`)
		if recorder.Code != tc.status || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") || recorder.Header().Get("Content-Disposition") != "" || !strings.Contains(recorder.Body.String(), tc.message) || strings.Contains(recorder.Body.String(), "password") {
			t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
		}
	}
}
