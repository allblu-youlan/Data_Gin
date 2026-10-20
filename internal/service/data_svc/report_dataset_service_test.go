package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"strings"
	"testing"
	"time"
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
func TestSelectContractHashIgnoresGrantStorageIdentity(t *testing.T) {
	c := reportdataset.Compiled{CompilerVersion: 1, SchemaHash: strings.Repeat("a", 64)}
	first := &reportrepo.Draft{Version: model.ReportVersion{DatasourceID: 3, InputSchemaJSON: "{}"}, Grants: []model.ReportGrant{{BaseModel: model.BaseModel{ID: 17}, VersionID: 9, DefinitionID: 4, SubjectType: "USER", SubjectID: 7, ActionsJSON: `["QUERY","EXPORT"]`}}}
	before := selectCompiledContract(first, c)
	first.Grants[0].ID = 29
	first.Grants[0].VersionID = 10
	first.Grants[0].CreatedAt = time.Now()
	first.Grants[0].UpdatedAt = time.Now()
	first.Grants[0].ActionsJSON = `["EXPORT","QUERY"]`
	after := selectCompiledContract(first, c)
	if before.Hashes.Contract != after.Hashes.Contract || before.Hashes.Permission != after.Hashes.Permission {
		t.Fatal("storage-only grant change invalidated immutable contract")
	}
}
func TestSelectPublishRejectsParameterShapeAndOptionKeyMismatch(t *testing.T) {
	c := reportdataset.Compiled{Query: reportdataset.QuerySpec{Base: reportdataset.Base{TableID: "1", AliasID: "base"}, Where: &reportdataset.Condition{AliasID: "base", ColumnID: "11", Operator: "GTE", Value: &reportdataset.Value{Kind: "PARAMETER", Code: "from"}}}, Tables: map[string]reportdataset.Table{"1": {Columns: []reportdataset.Column{{ID: "11", LogicalType: "integer"}}}}}
	if e := validateSelectInputBindings(c, map[string]reportRunInputFieldSchema{"from": {Type: "list[str]"}}); e == nil {
		t.Fatal("list parameter accepted for scalar comparison")
	}
	if e := validateSelectInputBindings(c, map[string]reportRunInputFieldSchema{"from": {Type: "str"}}); e != nil {
		t.Fatal(e)
	}
	c.Query.Where.Operator = "IN"
	if e := validateSelectInputBindings(c, map[string]reportRunInputFieldSchema{"from": {Type: "str"}}); e == nil {
		t.Fatal("scalar accepted for IN")
	}
}
