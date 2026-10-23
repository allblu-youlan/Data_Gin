package data_ctrl

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"github.com/gin-gonic/gin"
)

type workflowWritePermission struct {
	allowed bool
	err     error
	calls   int
}

func (p *workflowWritePermission) HasPermission(_ context.Context, _ model.User, code string) (bool, error) {
	p.calls++
	if code != model.PermissionExcelManage {
		return false, errors.New("wrong permission")
	}
	return p.allowed, p.err
}

func TestExcelWorkflowWriteRequiresConfirmationAndManagePermission(t *testing.T) {
	for _, test := range []struct {
		name               string
		operation          string
		confirmed, allowed bool
		err                error
		status, calls      int
	}{
		{name: "ordinary workflow", operation: "oracle_import", status: 200},
		{name: "write not confirmed", operation: "import_update", allowed: true, status: 400},
		{name: "no management permission", operation: "import_update", confirmed: true, status: 403, calls: 1},
		{name: "permission unavailable", operation: "import_update", confirmed: true, err: errors.New("offline"), status: 503, calls: 1},
		{name: "confirmed authorized write", operation: "import_update", confirmed: true, allowed: true, status: 200, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/workflow-runs", nil)
			c.Set(constant.CurrentUserInfo, model.User{})
			permission := &workflowWritePermission{allowed: test.allowed, err: test.err}
			config := data_svc.ExcelWorkflowConfig{Steps: []data_svc.ExcelWorkflowStep{{Operation: test.operation}}}
			allowed := authorizeExcelWorkflowWrite(c, config, test.confirmed, permission)
			if allowed != (test.status == 200) || recorder.Code != test.status || permission.calls != test.calls {
				t.Fatalf("allowed=%v status=%d calls=%d", allowed, recorder.Code, permission.calls)
			}
		})
	}
}
