package data_svc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExcelWorkflowReportsOracleCapacityFailureSafely(t *testing.T) {
	for _, code := range []int{1653, 1654, 1658, 1692} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			cause := fakeReportOracleError{code: code, message: "private_table password=secret-value"}
			message := safeExcelWorkflowFailureMessage(fmt.Errorf("第 1 步失败: %w", cause))
			for _, want := range []string{fmt.Sprintf("ORA-%05d", code), "表空间", "管理员", "后续步骤已停止"} {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q missing %q", message, want)
				}
			}
			for _, private := range []string{"private_table", "secret-value", "password"} {
				if strings.Contains(message, private) {
					t.Fatalf("message leaks private diagnostic: %s", message)
				}
			}
		})
	}
}

func TestExcelWorkflowKeepsUnknownFailurePrivate(t *testing.T) {
	for _, cause := range []error{nil, errors.New("ORA-01658: password=secret-value"), fakeReportOracleError{code: 1017, message: "password=secret-value"}} {
		message := safeExcelWorkflowFailureMessage(cause)
		if message != "组合操作失败，后续步骤已停止，请检查步骤日志" {
			t.Fatalf("unknown failure message = %q", message)
		}
	}
}
