package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"os"
	"strconv"
	"strings"
)

func selectReportsEnabled() bool {
	return strings.EqualFold(os.Getenv("REPORT_SELECT_SNAPSHOT_ENABLED"), "true") || os.Getenv("REPORT_SELECT_SNAPSHOT_ENABLED") == "1"
}
func selectDraftFromRequest(actor uint, req requestbody.ReportDraftSaveRequest) (*reportrepo.Draft, error) {
	if !selectReportsEnabled() {
		return nil, invalidReport("自定义查询功能尚未启用（REPORT_SELECT_SNAPSHOT_ENABLED）")
	}
	if req.Procedure.Name != "" || req.Procedure.Owner != "" || req.Result.TableName != "" || req.CallTemplate != "" || len(req.Parameters) > 0 || len(req.Columns) > 0 {
		return nil, invalidReport("SELECT reports use querySpec as the only query and column definition")
	}
	q, e := reportdataset.Decode[reportdataset.QuerySpec](req.QuerySpec)
	if e != nil {
		return nil, invalidReport(e.Error())
	}
	if q.Version != 1 || len(q.Columns) > 100 || len(q.Relations) > 8 {
		return nil, invalidReport("invalid query version or size")
	}
	input, e := canonicalSelectInputSchema(req.InputSchema)
	if e != nil {
		return nil, e
	}
	policy := reportdataset.DefaultPolicy()
	if len(req.ExecutionPolicy) > 0 {
		if e = json.Unmarshal(req.ExecutionPolicy, &policy); e != nil {
			return nil, invalidReport("invalid execution policy")
		}
	}
	policy.SnapshotFormatVersion = 2
	if policy.ExportPolicy != "MANUAL" || policy.PurgeAfterExport || policy.ResultRetentionHours < 1 || policy.ResultRetentionHours > 720 {
		return nil, invalidReport("invalid execution policy")
	}
	p, _ := json.Marshal(policy)
	canonical, _ := json.Marshal(q)
	grants, e := reportGrantsFromRequest(req.Grants, actor)
	if e != nil {
		return nil, e
	}
	return &reportrepo.Draft{Definition: model.ReportDefinition{Code: req.Code, Name: req.Name, Category: req.Category, Description: req.Description, DatasourceID: req.DatasourceID, OwnerUserID: actor, Status: model.ReportDefinitionStatusDraft, CreatedBy: actor, UpdatedBy: actor}, Version: model.ReportVersion{DatasourceID: req.DatasourceID, Status: model.ReportVersionStatusDraft, ExecutionMode: model.ReportExecutionModeSelectSnapshot, QuerySpecJSON: model.JSONText(canonical), InputSchemaJSON: model.JSONText(input), ExecutionPolicyJSON: model.JSONText(p), CreatedBy: actor}, Columns: []model.ReportColumn{}, Parameters: []model.ReportParameter{}, Grants: grants}, nil
}
func compileSelectDraft(ctx context.Context, draft *reportrepo.Draft) (reportdataset.Compiled, error) {
	q, e := reportdataset.Decode[reportdataset.QuerySpec]([]byte(draft.Version.QuerySpecJSON))
	if e != nil {
		return reportdataset.Compiled{}, invalidReport(e.Error())
	}
	tables, e := NewReportBojunCatalogService().LoadTables(ctx, draft.Version.DatasourceID, reportdataset.TableIDs(q))
	if e != nil {
		return reportdataset.Compiled{}, e
	}
	policy := reportdataset.DefaultPolicy()
	if e = json.Unmarshal([]byte(draft.Version.ExecutionPolicyJSON), &policy); e != nil {
		return reportdataset.Compiled{}, invalidReport("invalid execution policy")
	}
	compiled, e := reportdataset.Compile(q, tables, policy)
	if e != nil {
		return compiled, invalidReport(e.Error())
	}
	var schema map[string]reportRunInputFieldSchema
	if e = json.Unmarshal([]byte(draft.Version.InputSchemaJSON), &schema); e != nil {
		return compiled, invalidReport("invalid input schema")
	}
	known := map[string]bool{}
	for code := range schema {
		known[code] = true
	}
	var check func(*reportdataset.Condition) error
	check = func(cond *reportdataset.Condition) error {
		if cond == nil {
			return nil
		}
		if cond.Value != nil && cond.Value.Kind == "PARAMETER" && !known[cond.Value.Code] {
			return invalidReport("query references an undefined input parameter")
		}
		for i := range cond.Items {
			if e := check(&cond.Items[i]); e != nil {
				return e
			}
		}
		return nil
	}
	if e = check(q.Where); e != nil {
		return compiled, e
	}
	for _, r := range q.Relations {
		if e = check(r.MatchConditions); e != nil {
			return compiled, e
		}
	}
	return compiled, nil
}
func selectReportColumns(c reportdataset.Compiled) []model.ReportColumn {
	out := make([]model.ReportColumn, 0, len(c.Outputs))
	for i, col := range c.Outputs {
		src, _ := json.Marshal(c.Query.Columns[i].Source)
		out = append(out, model.ReportColumn{FieldID: col.FieldID, LogicalCode: col.Code, DatabaseColumn: col.Name, SourceOracleType: col.OracleType, PrecisionValue: col.Precision, ScaleValue: col.Scale, Nullable: col.Nullable, ValueType: col.LogicalType, PreviewHeader: col.Header, ExcelHeader: col.Header, DisplayOrder: i + 1, ExportOrder: i + 1, PreviewVisible: col.PreviewVisible, ExportVisible: col.ExportVisible, ExportAllowed: true, AllowedOperatorsJSON: "[]", SourceSpecJSON: model.JSONText(src), ValueMode: col.ValueMode, RelationID: col.RelationID, FormatJSON: model.JSONText(c.Query.Columns[i].Format), ExcelWidth: c.Query.Columns[i].ExcelWidth})
	}
	return out
}
func selectCompiledContract(draft *reportrepo.Draft, c reportdataset.Compiled) reportcontract.Compiled {
	// Include input and report grants in the immutable hash alongside the dataset.
	payload, _ := json.Marshal(SelectReportContract{Dataset: c, InputSchema: json.RawMessage(draft.Version.InputSchemaJSON), Grants: draft.Grants, DatasourceID: draft.Version.DatasourceID})
	return reportcontract.Compiled{SpecJSON: payload, Hashes: reportcontract.Hashes{Contract: reportdataset.Hash(json.RawMessage(payload)), ParameterSchema: reportdataset.Hash(json.RawMessage(draft.Version.InputSchemaJSON)), ProcedureSignature: reportdataset.Hash("SELECT_SNAPSHOT"), ResultSchema: c.SchemaHash, Permission: reportdataset.Hash(draft.Grants), ExportSchema: reportdataset.Hash(c.Outputs)}}
}

type SelectReportContract struct {
	Dataset      reportdataset.Compiled `json:"dataset"`
	InputSchema  json.RawMessage        `json:"inputSchema"`
	Grants       []model.ReportGrant    `json:"grants"`
	DatasourceID uint                   `json:"datasourceId"`
}

func decodeSelectContract(version model.ReportVersion) (reportdataset.Compiled, error) {
	contract, e := reportdataset.Decode[SelectReportContract]([]byte(version.CompiledSpecJSON))
	if e != nil || reportdataset.Hash(json.RawMessage(version.CompiledSpecJSON)) != version.ContractHash {
		return reportdataset.Compiled{}, fmt.Errorf("REPORT_METADATA_CHANGED: frozen query contract is invalid")
	}
	if contract.Dataset.CompilerVersion != 1 || contract.DatasourceID != version.DatasourceID {
		return reportdataset.Compiled{}, fmt.Errorf("REPORT_METADATA_CHANGED: unsupported frozen query contract")
	}
	return contract.Dataset, nil
}

// selectAuthorization loads only the control database, so existing snapshots remain readable during a source outage.
func selectAuthorization(ctx context.Context, ds uint, c reportdataset.Compiled, actor uint) (reportdataset.Compiled, string, error) {
	catalog := NewReportBojunCatalogService()
	scopes := map[string]any{}
	actorID := strconv.FormatUint(uint64(actor), 10)
	for id, frozen := range c.Tables {
		v, e := strconv.ParseUint(id, 10, 64)
		if e != nil {
			return c, "", e
		}
		record, e := catalog.repo.GetBojunTable(ctx, ds, uint(v))
		if e != nil {
			return c, "", ErrReportRunDenied
		}
		current, e := catalog.dto(ctx, *record)
		if e != nil {
			return c, "", e
		}
		if !current.Enabled {
			return c, "", ErrReportRunDenied
		}
		scope := current.Config.DataScope
		if scope.Mode != "GLOBAL_APPROVED" && scope.Mode != "SCOPE_COLUMN" {
			return c, "", ErrReportRunDenied
		}
		if scope.Mode != frozen.Config.DataScope.Mode || scope.ColumnID != frozen.Config.DataScope.ColumnID {
			return c, "", ErrReportRunDenied
		}
		scopes[id] = struct {
			Enabled                bool
			ScopeMode, ScopeColumn string
			Values                 []string
			Selectable, Detail     *[]string
		}{current.Enabled, scope.Mode, scope.ColumnID, scope.UserValues[actorID], current.Config.SelectableColumns, current.Config.DetailColumns}
		frozen.Config.DataScope.UserValues = map[string][]string{actorID: scope.UserValues[actorID]}
		c.Tables[id] = frozen
	}
	return c, reportdataset.Hash(scopes), nil
}
func validateSelectRunAuthorization(ctx context.Context, run model.ReportRun, version model.ReportVersion) error {
	if version.ExecutionMode != model.ReportExecutionModeSelectSnapshot {
		return nil
	}
	c, e := decodeSelectContract(version)
	if e != nil {
		return e
	}
	_, hash, e := selectAuthorization(ctx, version.DatasourceID, c, run.RequestedBy)
	if e != nil {
		return e
	}
	if hash != run.DataScopeHash {
		return fmt.Errorf("%w: 数据范围已变化，请重新查询", ErrReportRunQueryConflict)
	}
	return nil
}
func inspectSelectSource(ctx context.Context, a *reportoracle.Adapter, c reportdataset.Compiled) error {
	if e := a.ValidateJSONSnapshotStore(ctx); e != nil {
		return e
	}
	for _, t := range c.Tables {
		if e := a.ValidateBojunMetadata(ctx, t); e != nil {
			return e
		}
	}
	return nil
}
func canonicalSelectInputSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "{}" {
		return json.RawMessage(`{}`), nil
	}
	return canonicalReportInputSchema(raw)
}
