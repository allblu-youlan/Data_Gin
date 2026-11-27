package data_ctrl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTableBoardPreviewReportDateValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/preview", PreviewTableBoard)
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"invalid_day", `{"reportDate":"2026-02-30"}`, "报表日期需要有效的 YYYY-MM-DD 日期", http.StatusUnprocessableEntity},
		{"invalid_date_format", `{"reportDate":"11/15/2026"}`, "报表日期需要有效的 YYYY-MM-DD 日期", http.StatusUnprocessableEntity},
		{"wrong_type", `{"reportDate":20261115}`, "预览配置无效", http.StatusBadRequest},
		{"unknown_property", `{"reportDate":"2026-11-15","sql":"SELECT 1"}`, "预览配置无效", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), tc.message) {
				t.Fatalf("status=%d body=%s, want status=%d message=%s", recorder.Code, recorder.Body, tc.status, tc.message)
			}
		})
	}
}
