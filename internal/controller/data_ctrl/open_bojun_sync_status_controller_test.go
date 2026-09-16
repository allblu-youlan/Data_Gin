package data_ctrl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenBojunSyncStatusQueryService struct {
	actor uint
	calls int
	err   error
}

func (service *fakeOpenBojunSyncStatusQueryService) Query(_ context.Context, actor uint) (*data_svc.OpenBojunSyncStatusResult, error) {
	service.actor = actor
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	return &data_svc.OpenBojunSyncStatusResult{
		Source: "ORACLE", WatermarkField: "oracle_retail_id", Initialized: true,
		WatermarkOracleRetailID: 45077, CheckedAt: "2026-09-16 10:30:00",
	}, nil
}

func TestOpenBojunSyncStatusControllerParsesEmptyJSON(t *testing.T) {
	service := &fakeOpenBojunSyncStatusQueryService{}
	recorder := performOpenBojunSyncStatusRequest(t, service, "/api/open/bojun/sync-status/query", `{}`)
	if recorder.Code != http.StatusOK || service.calls != 1 || service.actor != 17 ||
		!strings.Contains(recorder.Body.String(), `"watermarkOracleRetailId":45077`) {
		t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
	}
}

func TestOpenBojunSyncStatusControllerRejectsInputs(t *testing.T) {
	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api/open/bojun/sync-status/query?source=oracle", body: `{}`},
		{path: "/api/open/bojun/sync-status/query", body: `{"source":"oracle"}`},
	} {
		service := &fakeOpenBojunSyncStatusQueryService{}
		recorder := performOpenBojunSyncStatusRequest(t, service, test.path, test.body)
		if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
		}
	}
}

func TestOpenBojunSyncStatusControllerMapsUnavailableTo503(t *testing.T) {
	service := &fakeOpenBojunSyncStatusQueryService{err: data_svc.ErrOpenBojunSyncStatusUnavailable}
	recorder := performOpenBojunSyncStatusRequest(t, service, "/api/open/bojun/sync-status/query", `{}`)
	if recorder.Code != http.StatusServiceUnavailable || service.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
	}
}

func performOpenBojunSyncStatusRequest(
	t *testing.T,
	service OpenBojunSyncStatusQueryService,
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
	controller := NewOpenBojunSyncStatusControllerWithService(service)
	router.POST("/api/open/bojun/sync-status/query", controller.Query)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
