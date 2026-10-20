package data_ctrl

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"github.com/gin-gonic/gin"
)

func TestDatasetErrorsKeepHTTPStatusAndStableCodeAcrossReportHandlers(t *testing.T) {
	writers := map[string]func(*gin.Context, error){
		"definition": writeReportError,
		"dataset":    writeDatasetError,
		"results":    writeReportRunQueryError,
		"export":     writeReportExportError,
	}
	cases := []struct {
		code   string
		err    error
		status int
	}{
		{"REPORT_RESULT_EXPIRED", fmt.Errorf("result lookup: %w", data_svc.ErrReportSelectResultExpired), http.StatusGone},
		{"REPORT_METADATA_CHANGED", errors.New("REPORT_METADATA_CHANGED: password=secret"), http.StatusConflict},
		{"REPORT_QUERY_LIMIT_EXCEEDED", errors.New("REPORT_QUERY_LIMIT_EXCEEDED: password=secret"), http.StatusUnprocessableEntity},
		{"REPORT_QUERY_INVALID", errors.New("REPORT_QUERY_INVALID: password=secret"), http.StatusBadRequest},
	}
	for name, write := range writers {
		for _, tc := range cases {
			t.Run(name+"/"+tc.code, func(t *testing.T) {
				router := reportControllerRouter()
				router.GET("/error", func(c *gin.Context) { write(c, tc.err) })
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/error", nil))
				var body struct {
					Data struct {
						ErrorCode string `json:"errorCode"`
					} `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != tc.status || body.Data.ErrorCode != tc.code || strings.Contains(recorder.Body.String(), "secret") {
					t.Fatalf("response status=%d body=%s", recorder.Code, recorder.Body)
				}
			})
		}
	}
}
