package data_ctrl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
)

func TestListModelsReturnsSafeServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/excel-match-jobs/models", nil)
	controller := &ExcelMatchJobController{service: &data_svc.ExcelMatchJobService{}}

	controller.ListModels(context)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var body responses.ResponseData
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errcode.ServiceUnavailable.Code() || body.Msg != "模型目录暂时不可用" {
		t.Fatalf("response = %#v", body)
	}
	if strings.Contains(recorder.Body.String(), "database") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
	}
}

func TestSafeExcelMatchJobResponseDoesNotExposePrivateFields(t *testing.T) {
	job := model.ExcelMatchJob{
		BaseModel:       model.BaseModel{ID: 17},
		SourceFileName:  "orders.xlsx",
		SourceFilePath:  "/private/source.xlsx",
		ResultFilePath:  "/private/result.xlsx",
		ResultObjectKey: "excel/private-object",
		ResultURL:       "https://storage.example/private-signed-url",
		WorkDir:         "/private/work",
		ConfigJSON:      `{"operation":"import_update","filters":[{"value":"business-sensitive"}],"headers":{"Authorization":"Bearer secret-value"}}`,
		Status:          "failed",
		ErrorMessage:    "password=private-error",
		CommonTimestampsField: model.CommonTimestampsField{
			CreatedAt: 123,
		},
	}
	encoded, err := json.Marshal(safeExcelMatchJob(job))
	if err != nil {
		t.Fatalf("marshal safe Excel job: %v", err)
	}
	response := string(encoded)
	for _, privateValue := range []string{
		"source_file_path", "result_file_path", "result_object_key", "result_url", "work_dir", "error_message", "config_json",
		"private-signed-url", "business-sensitive", "secret-value", "private-error",
	} {
		if strings.Contains(response, privateValue) {
			t.Fatalf("safe Excel job response leaked %q: %s", privateValue, response)
		}
	}
	if !strings.Contains(response, `"operation":"import_update"`) || !strings.Contains(response, `"created_at":123`) {
		t.Fatalf("safe Excel job omitted required display fields: %s", response)
	}
}

func TestSafeExcelMatchJobLogsExposeOnlySanitizedMessages(t *testing.T) {
	logs := safeExcelMatchJobLogs([]model.ExcelMatchJobLog{{
		BaseModel: model.BaseModel{ID: 19}, JobID: 17, Level: "error", Message: "token=private-log", DetailJSON: `{"secret":"private-detail"}`,
	}})
	encoded, err := json.Marshal(logs)
	if err != nil {
		t.Fatalf("marshal safe Excel logs: %v", err)
	}
	response := string(encoded)
	if strings.Contains(response, "private-log") || strings.Contains(response, "private-detail") || strings.Contains(response, "detail_json") {
		t.Fatalf("safe Excel logs leaked private details: %s", response)
	}
	if !strings.Contains(response, `"message":"任务日志已记录（敏感内容已隐藏）"`) {
		t.Fatalf("safe Excel logs omitted sanitized message: %s", response)
	}
}

func TestExcelMatchJobQueryStatusValidation(t *testing.T) {
	for _, status := range []string{"", "pending", "running", "success", "failed", "expired"} {
		if !validExcelMatchJobStatus(status) {
			t.Fatalf("validExcelMatchJobStatus(%q) = false", status)
		}
	}
	for _, status := range []string{"queued", "deleted", "success OR 1=1"} {
		if validExcelMatchJobStatus(status) {
			t.Fatalf("validExcelMatchJobStatus(%q) = true", status)
		}
	}
}

func TestExcelMatchJobQueryOperationValidation(t *testing.T) {
	for _, operation := range []string{"", "all", "match", "write", "export"} {
		if !validExcelMatchJobOperation(operation) {
			t.Fatalf("validExcelMatchJobOperation(%q) = false", operation)
		}
	}
	for _, operation := range []string{"export_match", "import_update", "deleted", "write OR 1=1"} {
		if validExcelMatchJobOperation(operation) {
			t.Fatalf("validExcelMatchJobOperation(%q) = true", operation)
		}
	}
}
