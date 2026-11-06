package data_ctrl

import (
	"net/http"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestTmallRealtimeControllerMapsTimeRangeAndRejectsOtherModes(t *testing.T) {
	service := &fakeOpenTmallQueryService{}
	recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/orders/realtime/query", `{"startTime":"2026-11-06 00:00:00","endTime":"2026-11-07 00:00:00","timeField":"modified","filters":{"TID":"000123"},"page":2,"pageSize":25}`)
	if strings.Contains(recorder.Body.String(), `"columns"`) || recorder.Code != http.StatusOK || service.calls != 1 || service.dataset != reportoracle.TmallOrders || service.request.StartTime != "2026-11-06 00:00:00" || service.request.EndTime != "2026-11-07 00:00:00" || service.request.TimeField != "modified" || service.request.Filters["TID"] != "000123" || *service.request.Page != 2 || *service.request.PageSize != 25 {
		t.Fatalf("status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
	}
	for _, body := range []string{`{"startDate":"2026-11-06"}`, `{"batchNo":"202611060000"}`, `{"startTime":123}`, `{"filters":{"NUM_IID":123}}`, `{} {}`, `null`} {
		service := &fakeOpenTmallQueryService{}
		recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/orders/realtime/query", body)
		if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 {
			t.Fatalf("body=%s status=%d calls=%d", body, recorder.Code, service.calls)
		}
	}
	service = &fakeOpenTmallQueryService{}
	recorder = performOpenTmallControllerRequest(service, "/api/open/tmall/orders/realtime/query?page=1", `{}`)
	if recorder.Code != http.StatusUnprocessableEntity || service.calls != 0 {
		t.Fatal("URL parameters must be rejected")
	}
}
