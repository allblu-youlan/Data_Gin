package responses

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/errcode"

	"github.com/gin-gonic/gin"
)

func TestToResponseWithStatusUsesRealHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	New(context).ToResponseWithStatus(http.StatusCreated, gin.H{"id": 7})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	var body ResponseData
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errcode.Success.Code() {
		t.Fatalf("application code = %d, want %d", body.Code, errcode.Success.Code())
	}
}

func TestToSafeErrorResponseMapsStatusWithoutLeakingDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	New(context).ToSafeErrorResponse(errcode.Conflict, "资源版本冲突")

	if recorder.Code != http.StatusConflict {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "database") {
		t.Fatalf("unsafe response body = %q", body)
	}
}

func TestForbiddenUsesForbiddenHTTPStatus(t *testing.T) {
	if got := errcode.Forbidden.HttpStatusCode(); got != http.StatusForbidden {
		t.Fatalf("Forbidden HTTP status = %d, want %d", got, http.StatusForbidden)
	}
}

func TestForOpenAPIFormatsNestedDateTimes(t *testing.T) {
	value := ForOpenAPI(gin.H{
		"fetchedAtUtc": time.Date(2026, 7, 29, 4, 5, 6, 123, time.UTC),
		"items": []interface{}{gin.H{
			"observedAtLocal":   "2026-07-29T12:05:06+08:00",
			"issuedAtLocal":     "2026-07-29T12:05:06+08:00",
			"forecastDateLocal": "2026-07-30",
			"description":       "2026-07-29T12:05:06+08:00",
		}},
	})
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(raw)
	for _, expected := range []string{
		`"fetchedAtUtc":"2026-07-29 04:05:06"`,
		`"observedAtLocal":"2026-07-29T12:05:06+08:00"`,
		`"issuedAtLocal":"2026-07-29 12:05:06"`,
		`"forecastDateLocal":"2026-07-30 00:00:00"`,
		`"description":"2026-07-29T12:05:06+08:00"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response = %s, want %s", body, expected)
		}
	}
}

func TestResponseNormalizesEmptyDataOnlyForOpenAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		render func(*Response)
		status int
	}{
		{"success", func(r *Response) { r.ToResponse(nil) }, http.StatusOK},
		{"status success", func(r *Response) { r.ToResponseWithStatus(http.StatusCreated, nil) }, http.StatusCreated},
		{"safe error", func(r *Response) { r.ToSafeErrorResponse(errcode.Forbidden, "") }, http.StatusForbidden},
		{"legacy error", func(r *Response) { r.ToErrorResponse(errcode.Forbidden) }, http.StatusOK},
		{"validation error", func(r *Response) { r.ToErrorValidateResponse(errcode.UnprocessableEntity, nil) }, http.StatusOK},
	} {
		for _, path := range []string{"/api/open/tmall/qztg/hourly/query", "/api/v1/reports"} {
			t.Run(test.name+path, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
				test.render(New(ctx))
				wantData := `"data":{}`
				if path == "/api/v1/reports" && strings.Contains(test.name, "error") {
					wantData = `"data":null`
				}
				if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), wantData) {
					t.Fatalf("status=%d body=%s, want status=%d %s", recorder.Code, recorder.Body.String(), test.status, wantData)
				}
			})
		}
	}
}

func TestResponsePreservesNonOpenAPIFieldsAndExistingDateFormats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/api/open/tmall/qztg/hourly/query", "/api/v1/reports"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
			New(ctx).ToResponseWithStatus(http.StatusOK, gin.H{
				"text":         (*string)(nil),
				"fetchedAtUtc": "2026-11-04T14:00:00Z",
			})
			wantText := `"text":null`
			if strings.HasPrefix(path, "/api/open/") {
				wantText = `"text":""`
			}
			body := recorder.Body.String()
			if !strings.Contains(body, wantText) || !strings.Contains(body, `"fetchedAtUtc":"2026-11-04T14:00:00Z"`) {
				t.Fatalf("unexpected response: %s", body)
			}
		})
	}
}
