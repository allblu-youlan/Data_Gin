package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOpenTmallControllerLogsPrivateCauseWithSafeResponse(t *testing.T) {
	for _, test := range []struct {
		name      string
		path      string
		err       error
		status    int
		wantCause string
	}{
		{
			name: "realtime query failure",
			path: "/api/open/tmall/orders/realtime/query",
			err: fmt.Errorf("%w: query: %w", data_svc.ErrOpenTmallUnavailable,
				errors.New("ORA-00942: table or view does not exist; SQL=SELECT private_column FROM private_table; password=oracle-secret; access_token=private-token")),
			status:    http.StatusServiceUnavailable,
			wantCause: "query: ORA-00942: table or view does not exist",
		},
		{
			name:      "hourly authorization storage timeout",
			path:      "/api/open/tmall/qztg/hourly/query",
			err:       fmt.Errorf("%w: authorize: %w", data_svc.ErrOpenTmallUnavailable, context.DeadlineExceeded),
			status:    http.StatusServiceUnavailable,
			wantCause: "authorize: context deadline exceeded",
		},
		{
			name:      "unexpected daily query failure",
			path:      "/api/open/tmall/sycm/query",
			err:       errors.New("unexpected query failure; password=oracle-secret"),
			status:    http.StatusInternalServerError,
			wantCause: "unexpected query failure",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			previous := logger.Logger
			logger.Logger = zap.New(core)
			t.Cleanup(func() { logger.Logger = previous })

			service := &fakeOpenTmallQueryService{err: test.err}
			recorder := performOpenTmallControllerRequest(service, test.path, `{}`)
			var response struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status || response.Code != 100000+test.status ||
				response.Msg != "天猫查询服务暂时不可用" || string(response.Data) != "{}" || service.calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
			}
			for _, private := range []string{"ORA-", "query failure", "deadline", "private_column", "private_table", "oracle-secret", "private-token", "password", "access_token"} {
				if strings.Contains(recorder.Body.String(), private) {
					t.Errorf("public response contains private detail %q", private)
				}
			}

			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != zap.ErrorLevel || entries[0].Message != "HTTP request failed" {
				t.Fatalf("expected one common error log, got %+v", entries)
			}
			fields := entries[0].ContextMap()
			cause, _ := fields["error_cause"].(string)
			if !strings.Contains(cause, test.wantCause) {
				t.Errorf("private cause=%q, want %q", cause, test.wantCause)
			}
			if fields["request_route"] != test.path || fields["response_status"] != int64(test.status) || fields["user_id"] != "17" {
				t.Errorf("missing request context: %+v", fields)
			}
			for _, secret := range []string{"oracle-secret", "private-token"} {
				if strings.Contains(fmt.Sprint(fields), secret) {
					t.Errorf("private log contains credential %q", secret)
				}
			}
		})
	}
}
