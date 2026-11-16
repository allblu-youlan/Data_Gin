package data_ctrl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

func TestMallWeatherControllerOpenRealtimeReturnsCurrentSnapshot(t *testing.T) {
	var gotActor, gotMall uint
	var gotTimeZone string
	service := fakeMallWeatherControllerService{currentRealtime: func(
		_ context.Context,
		actor, mallID uint,
		timeZone string,
	) (*data_svc.MallWeatherCurrentRealtimeResult, error) {
		gotActor, gotMall, gotTimeZone = actor, mallID, timeZone
		snapshotAt := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
		return &data_svc.MallWeatherCurrentRealtimeResult{
			Realtime: &data_svc.MallWeatherRealtimeDTO{
				SnapshotAtUTC:           snapshotAt,
				SnapshotAtLocal:         "2026-07-29T12:00:00+08:00",
				ProviderServerTimeUTC:   snapshotAt,
				ProviderServerTimeLocal: "2026-07-29T12:00:00+08:00",
				FetchedAtUTC:            snapshotAt,
				FetchedAtLocal:          "2026-07-29T12:00:00+08:00",
				QualityStatus:           "VALID",
				QualityWarnings:         []data_svc.MallWeatherWarningDTO{},
			},
			Meta: data_svc.MallWeatherQueryMeta{TimeZone: "Asia/Shanghai", FreshnessStatus: "FRESH"},
		}, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t, service, "/api/open/weather/realtime", `{"mallId":7,"timeZone":"Asia/Shanghai"}`,
	)
	response := recorder.Body.String()
	if recorder.Code != http.StatusOK || gotActor != 17 || gotMall != 7 || gotTimeZone != "Asia/Shanghai" ||
		!strings.Contains(response, `"snapshotAtUtc":"2026-07-29 04:00:00"`) ||
		!strings.Contains(response, `"snapshotAtLocal":"2026-07-29 12:00:00"`) ||
		strings.Contains(response, `"items"`) || strings.Contains(response, `"pagination"`) {
		t.Fatalf("status=%d actor=%d mall=%d timeZone=%q body=%s", recorder.Code, gotActor, gotMall, gotTimeZone, response)
	}
}

func TestMallWeatherControllerOpenRealtimeRejectsHistoricalQueryFields(t *testing.T) {
	calls := 0
	service := fakeMallWeatherControllerService{currentRealtime: func(
		context.Context,
		uint,
		uint,
		string,
	) (*data_svc.MallWeatherCurrentRealtimeResult, error) {
		calls++
		return nil, nil
	}}
	tests := []string{
		`{"mallId":7,"start":"2026-07-29 00:00:00"}`,
		`{"mallId":7,"end":"2026-07-30 00:00:00"}`,
		`{"mallId":7,"latest":true}`,
		`{"mallId":7,"asOf":"2026-07-29 00:00:00"}`,
		`{"mallId":7,"qualityStatus":"VALID"}`,
		`{"mallId":7,"cursor":"abc"}`,
		`{"mallId":7,"pageSize":50}`,
	}
	for _, body := range tests {
		recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/realtime", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d body=%s request=%s", recorder.Code, recorder.Body.String(), body)
		}
	}
	if calls != 0 {
		t.Fatalf("service calls=%d", calls)
	}
}

func TestMallWeatherControllerOpenRealtimeReturnsEmptyObjectWhenUnavailable(t *testing.T) {
	service := fakeMallWeatherControllerService{currentRealtime: func(
		context.Context,
		uint,
		uint,
		string,
	) (*data_svc.MallWeatherCurrentRealtimeResult, error) {
		return &data_svc.MallWeatherCurrentRealtimeResult{
			Meta: data_svc.MallWeatherQueryMeta{TimeZone: "Asia/Shanghai", FreshnessStatus: "UNAVAILABLE"},
		}, nil
	}}
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/realtime", `{"mallId":7}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"realtime":{}`) ||
		!strings.Contains(recorder.Body.String(), `"freshnessStatus":"UNAVAILABLE"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHistoryDayUsesDatePrecision(t *testing.T) {
	var gotActor, gotMall uint
	var gotRequest requestbody.OpenWeatherHistoryDayQueryRequest
	service := fakeMallWeatherControllerService{historyDay: func(
		_ context.Context,
		actor, mallID uint,
		request requestbody.OpenWeatherHistoryDayQueryRequest,
	) (*data_svc.MallWeatherRealtimeResult, error) {
		gotActor, gotMall, gotRequest = actor, mallID, request
		return &data_svc.MallWeatherRealtimeResult{
			Items:      []data_svc.MallWeatherRealtimeDTO{},
			Pagination: data_svc.MallWeatherPagination{Page: 1, PageSize: 50},
		}, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t,
		service,
		"/api/open/weather/history/day",
		`{"mallId":7,"date":"2026-07-28","timeZone":"Asia/Shanghai","qualityStatus":"VALID","pageSize":50}`,
	)
	if recorder.Code != http.StatusOK || gotActor != 17 || gotMall != 7 || gotRequest.Date != "2026-07-28" ||
		gotRequest.TimeZone != "Asia/Shanghai" || gotRequest.QualityStatus != "VALID" || gotRequest.PageSize != 50 {
		t.Fatalf("status=%d actor=%d mall=%d request=%+v body=%s", recorder.Code, gotActor, gotMall, gotRequest, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHistoryRangeUsesSecondPrecision(t *testing.T) {
	var gotRequest requestbody.OpenWeatherHistoryRangeQueryRequest
	service := fakeMallWeatherControllerService{historyRange: func(
		_ context.Context,
		_, _ uint,
		request requestbody.OpenWeatherHistoryRangeQueryRequest,
	) (*data_svc.MallWeatherRealtimeResult, error) {
		gotRequest = request
		return &data_svc.MallWeatherRealtimeResult{Items: []data_svc.MallWeatherRealtimeDTO{}}, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t,
		service,
		"/api/open/weather/history/range",
		`{"mallId":7,"startTime":"2026-07-28 01:02:03","endTime":"2026-07-28 04:05:06","cursor":"next"}`,
	)
	if recorder.Code != http.StatusOK || gotRequest.StartTime != "2026-07-28 01:02:03" ||
		gotRequest.EndTime != "2026-07-28 04:05:06" || gotRequest.Cursor != "next" {
		t.Fatalf("status=%d request=%+v body=%s", recorder.Code, gotRequest, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHistoryDaySummaryUsesBodyAndFormatsTimes(t *testing.T) {
	var gotActor, gotMall uint
	var gotRequest requestbody.OpenWeatherHistoryDaySummaryRequest
	service := fakeMallWeatherControllerService{historyDaySummary: func(
		_ context.Context,
		actor, mallID uint,
		request requestbody.OpenWeatherHistoryDaySummaryRequest,
	) (*data_svc.OpenWeatherHistoryDaySummaryResult, error) {
		gotActor, gotMall, gotRequest = actor, mallID, request
		return &data_svc.OpenWeatherHistoryDaySummaryResult{
			ObservationStatus: "AVAILABLE",
			Summary: &data_svc.OpenWeatherHistoryDaySummaryDTO{
				Date: "2026-07-28", SampleCount: 144,
				ObservedStartUTC:   time.Date(2026, 7, 27, 16, 0, 0, 0, time.UTC),
				ObservedEndUTC:     time.Date(2026, 7, 28, 15, 50, 0, 0, time.UTC),
				ObservedStartLocal: "2026-07-28T00:00:00+08:00",
				ObservedEndLocal:   "2026-07-28T23:50:00+08:00",
			},
			Meta: data_svc.MallWeatherQueryMeta{TimeZone: "Asia/Shanghai"},
		}, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t, service, "/api/open/weather/history/day/summary",
		`{"mallId":7,"date":"2026-07-28","timeZone":"Asia/Shanghai","qualityStatus":"VALID"}`,
	)
	response := recorder.Body.String()
	if recorder.Code != http.StatusOK || gotActor != 17 || gotMall != 7 || gotRequest.Date != "2026-07-28" ||
		gotRequest.TimeZone != "Asia/Shanghai" || gotRequest.QualityStatus != "VALID" ||
		!strings.Contains(response, `"observedStartUtc":"2026-07-27 16:00:00"`) ||
		!strings.Contains(response, `"observedEndLocal":"2026-07-28 23:50:00"`) ||
		!strings.Contains(response, `"dominantSkycon":""`) ||
		!strings.Contains(response, `"temperatureMinC":0`) ||
		!strings.Contains(response, `"aqiChnMax":0`) || strings.Contains(response, ":null") ||
		strings.Contains(response, `"items"`) || strings.Contains(response, `"pagination"`) {
		t.Fatalf("status=%d actor=%d mall=%d request=%+v body=%s", recorder.Code, gotActor, gotMall, gotRequest, response)
	}
}

func TestMallWeatherControllerOpenHistoryDaySummaryReturnsEmptyObjectWhenUnavailable(t *testing.T) {
	service := fakeMallWeatherControllerService{historyDaySummary: func(
		context.Context,
		uint,
		uint,
		requestbody.OpenWeatherHistoryDaySummaryRequest,
	) (*data_svc.OpenWeatherHistoryDaySummaryResult, error) {
		return &data_svc.OpenWeatherHistoryDaySummaryResult{
			ObservationStatus: "UNAVAILABLE",
			Meta: data_svc.MallWeatherQueryMeta{
				TimeZone: "Asia/Shanghai", FreshnessStatus: "UNAVAILABLE",
			},
		}, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t, service, "/api/open/weather/history/day/summary", `{"mallId":7,"date":"2026-07-28"}`,
	)
	response := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(response, `"summary":{}`) ||
		!strings.Contains(response, `"observationStatus":"UNAVAILABLE"`) || strings.Contains(response, ":null") {
		t.Fatalf("status=%d body=%s", recorder.Code, response)
	}
}

func TestMallWeatherControllerOpenHistoryDaySummaryRejectsNonBodyParameters(t *testing.T) {
	calls := 0
	service := fakeMallWeatherControllerService{historyDaySummary: func(
		context.Context,
		uint,
		uint,
		requestbody.OpenWeatherHistoryDaySummaryRequest,
	) (*data_svc.OpenWeatherHistoryDaySummaryResult, error) {
		calls++
		return nil, nil
	}}
	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api/open/weather/history/day/summary?mallId=7", body: `{"mallId":7,"date":"2026-07-28"}`},
		{path: "/api/open/weather/history/day/summary", body: `{"mallId":7,"date":"2026-07-28","pageSize":50}`},
		{path: "/api/open/weather/history/day/summary", body: `{"mallId":7,"date":"2026-07-28"}{}`},
		{path: "/api/open/weather/history/day/summary", body: `{}`},
	} {
		recorder := performOpenMallWeatherRequest(t, service, test.path, test.body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("path=%s status=%d body=%s", test.path, recorder.Code, recorder.Body.String())
		}
	}
	if calls != 0 {
		t.Fatalf("service calls=%d", calls)
	}
}

func TestMallWeatherControllerOpenHistoryRejectsWrongFieldLevel(t *testing.T) {
	service := fakeMallWeatherControllerService{
		historyDay: func(context.Context, uint, uint, requestbody.OpenWeatherHistoryDayQueryRequest) (*data_svc.MallWeatherRealtimeResult, error) {
			t.Fatal("HistoryDay must not be called")
			return nil, nil
		},
		historyRange: func(context.Context, uint, uint, requestbody.OpenWeatherHistoryRangeQueryRequest) (*data_svc.MallWeatherRealtimeResult, error) {
			t.Fatal("HistoryRange must not be called")
			return nil, nil
		},
	}
	tests := []struct {
		path string
		body string
	}{
		{path: "/api/open/weather/history/day", body: `{"mallId":7,"date":"2026-07-28","startTime":"2026-07-28 00:00:00"}`},
		{path: "/api/open/weather/history/day", body: `{"mallId":7,"date":"2026-07-28","pageSize":0}`},
		{path: "/api/open/weather/history/range", body: `{"mallId":7,"start":"2026-07-28 00:00:00","end":"2026-07-29 00:00:00"}`},
	}
	for _, test := range tests {
		recorder := performOpenMallWeatherRequest(t, service, test.path, test.body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("path=%s status=%d body=%s", test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestMallWeatherControllerOpenRealtimeRejectsConflictingPathAndBodyMallID(t *testing.T) {
	calls := 0
	service := fakeMallWeatherControllerService{currentRealtime: func(
		context.Context,
		uint,
		uint,
		string,
	) (*data_svc.MallWeatherCurrentRealtimeResult, error) {
		calls++
		return nil, nil
	}}
	recorder := performOpenMallWeatherRequest(
		t, service, "/api/open/weather/malls/7/realtime", `{"mallId":8}`,
	)
	if recorder.Code != http.StatusUnprocessableEntity || calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHourlyParsesJSON(t *testing.T) {
	var gotActor, gotMall uint
	var gotRequest requestbody.MallWeatherHourlyQueryRequest
	service := fakeMallWeatherControllerService{hourly: func(
		_ context.Context,
		actor, mallID uint,
		request requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		gotActor, gotMall, gotRequest = actor, mallID, request
		return &data_svc.MallWeatherHourlyResult{
			Items:      []data_svc.MallWeatherHourlyDTO{},
			Pagination: data_svc.MallWeatherPagination{PageSize: 25},
		}, nil
	}}
	body := `{
		"mallId":7,
		"start":"2026-07-22 08:00:00",
		"end":"2026-07-23 08:00:00",
		"timeZone":"Asia/Shanghai",
		"latest":false,
		"asOf":"2026-07-22 09:00:00",
		"qualityStatus":"valid",
		"cursor":"abc",
		"pageSize":25
	}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/hourly", body)
	if recorder.Code != http.StatusOK || gotActor != 17 || gotMall != 7 {
		t.Fatalf("status=%d actor=%d mall=%d body=%s", recorder.Code, gotActor, gotMall, recorder.Body.String())
	}
	if gotRequest.Latest || gotRequest.PageSize != 25 || gotRequest.TimeZone != "Asia/Shanghai" ||
		gotRequest.QualityStatus != "valid" || gotRequest.Cursor != "abc" || gotRequest.AsOfUTC == nil ||
		!gotRequest.IncludeTotals || !gotRequest.StartUTC.Equal(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("request=%+v", gotRequest)
	}
}

func TestMallWeatherControllerOpenHourlyDefaultsLatest(t *testing.T) {
	var gotRequest requestbody.MallWeatherHourlyQueryRequest
	service := fakeMallWeatherControllerService{hourly: func(
		_ context.Context,
		_, _ uint,
		request requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		gotRequest = request
		return &data_svc.MallWeatherHourlyResult{Items: []data_svc.MallWeatherHourlyDTO{}}, nil
	}}
	body := `{"mallId":7,"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00"}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/hourly", body)
	if recorder.Code != http.StatusOK || !gotRequest.Latest {
		t.Fatalf("status=%d request=%+v body=%s", recorder.Code, gotRequest, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHourlyAcceptsTimePrecisionFields(t *testing.T) {
	var gotRequest requestbody.MallWeatherHourlyQueryRequest
	service := fakeMallWeatherControllerService{hourly: func(
		_ context.Context,
		_, _ uint,
		request requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		gotRequest = request
		return &data_svc.MallWeatherHourlyResult{Items: []data_svc.MallWeatherHourlyDTO{}}, nil
	}}
	body := `{"mallId":7,"startTime":"2026-07-22 08:01:02","endTime":"2026-07-22 09:02:03","timeZone":"Asia/Shanghai"}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/hourly", body)
	if recorder.Code != http.StatusOK ||
		!gotRequest.StartUTC.Equal(time.Date(2026, 7, 22, 0, 1, 2, 0, time.UTC)) ||
		!gotRequest.EndUTC.Equal(time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("status=%d request=%+v body=%s", recorder.Code, gotRequest, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenDailyAcceptsDatePrecisionFields(t *testing.T) {
	var gotRequest requestbody.MallWeatherDailyQueryRequest
	service := fakeMallWeatherControllerService{daily: func(
		_ context.Context,
		_, _ uint,
		request requestbody.MallWeatherDailyQueryRequest,
	) (*data_svc.MallWeatherDailyResult, error) {
		gotRequest = request
		return &data_svc.MallWeatherDailyResult{Items: []data_svc.MallWeatherDailyDTO{}}, nil
	}}
	body := `{"mallId":7,"startDate":"2026-07-22","endDate":"2026-07-24","timeZone":"Asia/Shanghai"}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/daily", body)
	if recorder.Code != http.StatusOK ||
		!gotRequest.StartUTC.Equal(time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC)) ||
		!gotRequest.EndUTC.Equal(time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("status=%d request=%+v body=%s", recorder.Code, gotRequest, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenWeatherRejectsMixedRangeLevels(t *testing.T) {
	service := fakeMallWeatherControllerService{
		hourly: func(context.Context, uint, uint, requestbody.MallWeatherHourlyQueryRequest) (*data_svc.MallWeatherHourlyResult, error) {
			t.Fatal("Hourly must not be called")
			return nil, nil
		},
		daily: func(context.Context, uint, uint, requestbody.MallWeatherDailyQueryRequest) (*data_svc.MallWeatherDailyResult, error) {
			t.Fatal("Daily must not be called")
			return nil, nil
		},
	}
	tests := []struct {
		path string
		body string
	}{
		{path: "/api/open/weather/hourly", body: `{"mallId":7,"startTime":"2026-07-22 00:00:00","endTime":"2026-07-23 00:00:00","start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00"}`},
		{path: "/api/open/weather/hourly", body: `{"mallId":7,"startDate":"2026-07-22","endDate":"2026-07-23"}`},
		{path: "/api/open/weather/daily", body: `{"mallId":7,"startTime":"2026-07-22 00:00:00","endTime":"2026-07-23 00:00:00"}`},
	}
	for _, test := range tests {
		recorder := performOpenMallWeatherRequest(t, service, test.path, test.body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("path=%s status=%d body=%s", test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestMallWeatherControllerOpenHourlyRejectsConflictingPathAndBodyMallID(t *testing.T) {
	calls := 0
	service := fakeMallWeatherControllerService{hourly: func(
		context.Context,
		uint,
		uint,
		requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		calls++
		return nil, nil
	}}
	body := `{"mallId":8,"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00"}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/malls/7/hourly", body)
	if recorder.Code != http.StatusUnprocessableEntity || calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHourlyFormatsResponseDateTimes(t *testing.T) {
	service := fakeMallWeatherControllerService{hourly: func(
		context.Context,
		uint,
		uint,
		requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		return &data_svc.MallWeatherHourlyResult{Items: []data_svc.MallWeatherHourlyDTO{{
			ForecastTimeUTC:   time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC),
			ForecastTimeLocal: "2026-07-22T09:02:03+08:00",
		}}}, nil
	}}
	body := `{"mallId":7,"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00"}`
	recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/hourly", body)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"forecastTimeUtc":"2026-07-22 01:02:03"`) ||
		!strings.Contains(recorder.Body.String(), `"forecastTimeLocal":"2026-07-22 09:02:03"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMallWeatherControllerOpenHourlyRejectsInvalidJSON(t *testing.T) {
	calls := 0
	service := fakeMallWeatherControllerService{hourly: func(
		context.Context,
		uint,
		uint,
		requestbody.MallWeatherHourlyQueryRequest,
	) (*data_svc.MallWeatherHourlyResult, error) {
		calls++
		return nil, nil
	}}
	tests := []string{
		`{"mallId":7,"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00","unknown":true}`,
		`{"mallId":7,"start":"bad","end":"2026-07-23 00:00:00"}`,
		`{"mallId":7,"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00","pageSize":0}`,
		`{"start":"2026-07-22 00:00:00","end":"2026-07-23 00:00:00"}`,
	}
	for _, body := range tests {
		recorder := performOpenMallWeatherRequest(t, service, "/api/open/weather/hourly", body)
		if recorder.Code != http.StatusUnprocessableEntity || strings.Contains(recorder.Body.String(), "unknown") {
			t.Fatalf("status=%d response=%s request=%s", recorder.Code, recorder.Body.String(), body)
		}
	}
	if calls != 0 {
		t.Fatalf("service calls=%d", calls)
	}
}

func performOpenMallWeatherRequest(
	t *testing.T,
	service MallWeatherQueryService,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, strconv.FormatUint(17, 10))
		c.Next()
	})
	controller := NewMallWeatherControllerWithService(service)
	router.POST("/api/open/weather/realtime", controller.OpenRealtime)
	router.POST("/api/open/weather/malls/:id/realtime", controller.OpenRealtime)
	router.POST("/api/open/weather/history/day", controller.OpenHistoryDay)
	router.POST("/api/open/weather/history/day/summary", controller.OpenHistoryDaySummary)
	router.POST("/api/open/weather/history/range", controller.OpenHistoryRange)
	router.POST("/api/open/weather/hourly", controller.OpenHourly)
	router.POST("/api/open/weather/malls/:id/hourly", controller.OpenHourly)
	router.POST("/api/open/weather/daily", controller.OpenDaily)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}
