package data_ctrl

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"github.com/gin-gonic/gin"
)

func TestTableEmailPreviewBindingKeepsLargeRecordIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/preview", strings.NewReader(`{"name":"提醒","sample":{"ID":9007199254740993}}`))
	var input struct {
		data_svc.TableEmailRuleInput
		Sample map[string]any `json:"sample"`
	}
	if err := bindTableEmailJSON(c, &input); err != nil {
		t.Fatal(err)
	}
	if input.Sample["ID"] != json.Number("9007199254740993") {
		t.Fatalf("ID lost precision: %#v", input.Sample)
	}
}
func TestTableEmailBindingRejectsUnknownConfiguration(t *testing.T) {
	for _, body := range []string{`{"name":"提醒","cursorValue":"999"}`, `{"name":"提醒"} {}`} {
		t.Run(body, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
			var input data_svc.TableEmailRuleInput
			if err := bindTableEmailJSON(c, &input); err == nil {
				t.Fatal("accepted unauthorized cursor or extra JSON")
			}
		})
	}
}
