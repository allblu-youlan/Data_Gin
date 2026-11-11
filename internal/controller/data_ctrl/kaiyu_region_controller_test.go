package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/internal/service/data_svc"
	"github.com/gin-gonic/gin"
)

type kaiyuRegionControllerStub struct {
	actor  uint
	query  data_svc.KaiyuRegionQuery
	cursor string
	force  bool
	calls  int
	err    error
}

func (s *kaiyuRegionControllerStub) Query(_ context.Context, actor uint, input data_svc.KaiyuRegionQuery) (*data_svc.KaiyuRegionResult, error) {
	s.actor, s.query = actor, input
	s.calls++
	return &data_svc.KaiyuRegionResult{}, s.err
}
func (s *kaiyuRegionControllerStub) Missing(_ context.Context, actor uint, input data_svc.KaiyuRegionQuery) (*data_svc.KaiyuRegionMissingResult, error) {
	s.actor, s.query = actor, input
	s.calls++
	return &data_svc.KaiyuRegionMissingResult{}, s.err
}
func (s *kaiyuRegionControllerStub) Backfill(_ context.Context, actor uint, input data_svc.KaiyuRegionBackfillRequest) (*data_svc.KaiyuRegionBackfillResult, error) {
	s.actor, s.query, s.cursor, s.force = actor, input.KaiyuRegionQuery, input.Cursor, input.Force
	s.calls++
	return &data_svc.KaiyuRegionBackfillResult{Done: true}, s.err
}
func (s *kaiyuRegionControllerStub) Export(_ context.Context, actor uint, input data_svc.KaiyuRegionQuery) (*data_svc.KaiyuPassengerExport, error) {
	s.actor, s.query = actor, input
	s.calls++
	return &data_svc.KaiyuPassengerExport{FileName: "region.xlsx", Content: []byte("PK-workbook")}, s.err
}
func kaiyuRegionControllerRequest(service *kaiyuRegionControllerStub, method, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17"); c.Next() })
	controller := NewKaiyuRegionControllerWithService(service)
	router.GET("/region", controller.Query)
	router.GET("/region/missing", controller.Missing)
	router.POST("/region/backfill", controller.Backfill)
	router.POST("/region/export", controller.Export)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}
func TestKaiyuRegionControllerPassesScopeAndCursor(t *testing.T) {
	service := &kaiyuRegionControllerStub{}
	response := kaiyuRegionControllerRequest(service, "POST", "/region/backfill", `{"startDate":"2026-11-01","endDate":"2026-11-02","mallId":7,"regionName":"入口","cursor":"2026-11-01:7","force":true}`)
	if response.Code != 200 || service.actor != 17 || service.query.MallID != 7 || service.query.RegionName != "入口" || !service.force || service.cursor != "2026-11-01:7" {
		t.Fatalf("response=%d service=%+v", response.Code, service)
	}
}
func TestKaiyuRegionControllerRejectsUnknownAndDuplicateInputs(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{{"GET", "/region?startDate=2026-11-01&endDate=2026-11-02&mallId=1&mallId=2", ""}, {"POST", "/region/backfill", `{"startDate":"2026-11-01","endDate":"2026-11-02","shopId":"unscoped"}`}, {"GET", "/region?startDate=2026-11-01&endDate=2026-11-02&shopId=unscoped", ""}} {
		service := &kaiyuRegionControllerStub{}
		response := kaiyuRegionControllerRequest(service, tc.method, tc.path, tc.body)
		if response.Code != http.StatusUnprocessableEntity || service.calls != 0 {
			t.Fatalf("response=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
		}
	}
}
func TestKaiyuRegionControllerSanitizesErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{errors.New("password=secret ORA-internal-table"), 503}, {auth_svc.ErrMallScopeForbidden, 403}, {data_svc.ErrKaiyuRegionBusy, 409}, {data_svc.ErrKaiyuRegionBindingConflict, 409}} {
		response := kaiyuRegionControllerRequest(&kaiyuRegionControllerStub{err: tc.err}, "GET", "/region?startDate=2026-11-01&endDate=2026-11-02", "")
		if response.Code != tc.status || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "ORA-") {
			t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
		}
	}
}
func TestKaiyuRegionControllerExportsBinary(t *testing.T) {
	response := kaiyuRegionControllerRequest(&kaiyuRegionControllerStub{}, "POST", "/region/export", `{"startDate":"2026-11-01","endDate":"2026-11-02"}`)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Disposition"), "region.xlsx") || !strings.HasPrefix(response.Body.String(), "PK") {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
}
