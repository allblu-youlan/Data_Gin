package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	limiterLib "github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

func TestOpenAPIUserRouteAllows60RequestsAndRejects61st(t *testing.T) {
	rate, err := limiterLib.NewRateFromFormatted("60-M")
	if err != nil {
		t.Fatal(err)
	}
	counter := limiterLib.New(memory.NewStore(), rate)
	checker := func(c *gin.Context, key, _ string) (limiterLib.Context, error) {
		return counter.Get(c, key)
	}
	handler := limitOpenAPIUserRoute("bojun", "60-M", checker)
	for request := 1; request <= 61; request++ {
		recorder, calls := performOpenRateLimitRequest(t, handler)
		wantStatus, wantCalls := http.StatusNoContent, 1
		if request == 61 {
			wantStatus, wantCalls = http.StatusTooManyRequests, 0
			assertOpenRateLimitCode(t, recorder, errcode.TooManyRequests.Code())
			if recorder.Header().Get("Retry-After") == "" {
				t.Fatal("missing retry delay")
			}
		}
		if recorder.Code != wantStatus || calls != wantCalls || recorder.Header().Get("X-RateLimit-Limit") != "60" {
			t.Fatalf("request=%d status=%d calls=%d headers=%v", request, recorder.Code, calls, recorder.Header())
		}
	}
}

func TestLimitOpenAPIUserRouteUsesScopedPrincipalKey(t *testing.T) {
	var gotKey, gotLimit string
	checker := func(_ *gin.Context, key, limit string) (limiterLib.Context, error) {
		gotKey, gotLimit = key, limit
		return limiterLib.Context{Limit: 120, Remaining: 119, Reset: time.Now().Add(time.Minute).Unix()}, nil
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(
		"/api/open/weather/malls/:id/hourly",
		func(c *gin.Context) {
			c.Set(constant.CurrentUserID, "17")
			c.Next()
		},
		limitOpenAPIUserRoute("weather", "120-M", checker),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/open/weather/malls/7/hourly", nil))

	if recorder.Code != http.StatusNoContent || gotLimit != "120-M" ||
		gotKey != "open-weather|user|17|route|-api-open-weather-malls--id-hourly" {
		t.Fatalf("status=%d key=%q limit=%q", recorder.Code, gotKey, gotLimit)
	}
	if recorder.Header().Get("X-RateLimit-Remaining") != "119" {
		t.Fatalf("headers=%v", recorder.Header())
	}
}

func TestLimitOpenAPIUserRouteReturnsReal429(t *testing.T) {
	checker := func(_ *gin.Context, _, _ string) (limiterLib.Context, error) {
		return limiterLib.Context{
			Limit: 30, Remaining: 0, Reset: time.Now().Add(30 * time.Second).Unix(), Reached: true,
		}, nil
	}
	recorder, calls := performOpenRateLimitRequest(t, limitOpenAPIUserRoute("bojun", "30-M", checker))
	if recorder.Code != http.StatusTooManyRequests || calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("headers=%v", recorder.Header())
	}
	if !strings.Contains(recorder.Body.String(), "开放接口请求过于频繁") ||
		strings.Contains(recorder.Body.String(), "天气") {
		t.Fatalf("body=%s", recorder.Body.String())
	}
	assertOpenRateLimitCode(t, recorder, errcode.TooManyRequests.Code())
}

func TestLimitOpenAPIUserRouteFailsClosedWhenRedisUnavailable(t *testing.T) {
	checker := func(_ *gin.Context, _, _ string) (limiterLib.Context, error) {
		return limiterLib.Context{}, errors.New("redis unavailable")
	}
	recorder, calls := performOpenRateLimitRequest(t, limitOpenAPIUserRoute("bojun", "30-M", checker))
	if recorder.Code != http.StatusServiceUnavailable || calls != 0 || strings.Contains(recorder.Body.String(), "redis") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
	assertOpenRateLimitCode(t, recorder, errcode.ServiceUnavailable.Code())
}

func performOpenRateLimitRequest(t *testing.T, limiter gin.HandlerFunc) (*httptest.ResponseRecorder, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	calls := 0
	router.POST("/open", func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	}, limiter, func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/open", nil))
	return recorder, calls
}

func assertOpenRateLimitCode(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	var body responses.ResponseData
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != want {
		t.Fatalf("response=%+v", body)
	}
}
