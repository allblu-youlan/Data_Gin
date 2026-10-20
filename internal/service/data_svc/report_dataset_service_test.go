package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"testing"
)

func TestSelectDraftDoesNotRequireProcedureAndAllowsIncompleteQuery(t *testing.T) {
	t.Setenv("REPORT_SELECT_SNAPSHOT_ENABLED", "true")
	draft, e := reportDraftFromRequest(9, requestbody.ReportDraftSaveRequest{Code: "custom_report", Name: "自定义", Category: "销售", DatasourceID: 3, ExecutionMode: model.ReportExecutionModeSelectSnapshot, InputSchema: json.RawMessage(`{}`), QuerySpec: json.RawMessage(`{"version":1,"base":{"tableId":"","aliasId":"base"},"relations":[],"columns":[]}`)})
	if e != nil {
		t.Fatal(e)
	}
	if draft.Version.ProcedureName != "" || draft.Version.ResultTableName != "" || len(draft.Columns) != 0 || draft.Version.ExecutionPolicyJSON == "" {
		t.Fatalf("invalid draft: %#v", draft)
	}
	dto := reportDraftDTO(draft)
	if len(dto.QuerySpec) == 0 || len(dto.ExecutionPolicy) == 0 {
		t.Fatal("query contract lost on read")
	}
}
func TestSelectDraftRejectsConflictingLegacyColumns(t *testing.T) {
	t.Setenv("REPORT_SELECT_SNAPSHOT_ENABLED", "true")
	_, e := reportDraftFromRequest(9, requestbody.ReportDraftSaveRequest{Code: "custom_report", Name: "自定义", Category: "销售", DatasourceID: 3, ExecutionMode: model.ReportExecutionModeSelectSnapshot, QuerySpec: json.RawMessage(`{"version":1}`), Columns: []requestbody.ReportColumnRequest{{}}})
	if e == nil {
		t.Fatal("conflicting column sources accepted")
	}
}
func TestSelectDraftIsDisabledByDefault(t *testing.T) {
	t.Setenv("REPORT_SELECT_SNAPSHOT_ENABLED", "")
	_, e := reportDraftFromRequest(9, requestbody.ReportDraftSaveRequest{Code: "custom_report", Name: "自定义", Category: "销售", DatasourceID: 3, ExecutionMode: model.ReportExecutionModeSelectSnapshot})
	if e == nil {
		t.Fatal("disabled feature accepted")
	}
}
