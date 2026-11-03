package data_ctrl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeSalesSummaryQueryService struct {
	calls int
	actor uint
	code  string
	start string
	end   string
	err   error
}

func (service *fakeSalesSummaryQueryService) Query(
	_ context.Context,
	actor uint,
	code string,
	start string,
	end string,
) (*data_svc.SalesSummaryResult, error) {
	service.calls++
	service.actor, service.code, service.start, service.end = actor, code, start, end
	return &data_svc.SalesSummaryResult{StartDate: start, EndDate: end}, service.err
}

func TestSalesSummaryControllerForwardsQueryAndActor(t *testing.T) {
	service := &fakeSalesSummaryQueryService{}
	recorder := performSalesSummaryRequest(service, "/?mallCode=ABCN001A003&startDate=2026-10-01&endDate=2026-10-31")
	if recorder.Code != http.StatusOK || service.actor != 17 || service.code != "ABCN001A003" || service.start != "2026-10-01" || service.end != "2026-10-31" {
		t.Fatalf("status=%d query=%+v", recorder.Code, service)
	}
}

func TestSalesSummaryControllerRejectsUnexpectedParameters(t *testing.T) {
	for _, path := range []string{
		"/?mallCode=ABCN001A003&startDate=2026-10-01",
		"/?mallCode=ABCN001A003&startDate=2026-10-01&endDate=2026-10-31&extra=1",
		"/?mallCode=ABCN001A003&startDate=2026-10-01&endDate=2026-10-31&endDate=2026-11-01",
	} {
		service := &fakeSalesSummaryQueryService{}
		recorder := performSalesSummaryRequest(service, path)
		if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 {
			t.Fatalf("invalid query accepted: path=%s status=%d", path, recorder.Code)
		}
	}
}

func TestSalesSummaryControllerMapsServiceErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		code int
	}{
		{err: data_svc.ErrSalesSummaryForbidden, code: http.StatusForbidden},
		{err: data_svc.ErrSalesSummaryUnavailable, code: http.StatusServiceUnavailable},
	} {
		recorder := performSalesSummaryRequest(&fakeSalesSummaryQueryService{err: test.err}, "/?mallCode=ABCN001A003&startDate=2026-10-01&endDate=2026-10-31")
		if recorder.Code != test.code {
			t.Fatalf("error=%v status=%d", test.err, recorder.Code)
		}
	}
}

func performSalesSummaryRequest(service SalesSummaryQueryService, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17") })
	router.GET("/", NewSalesSummaryControllerWithService(service).Query)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}
