package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"github.com/google/uuid"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
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
	if err := validateSelectInputBindings(compiled, schema); err != nil {
		return compiled, err
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
	payload, _ := json.Marshal(SelectReportContract{Dataset: c, InputSchema: json.RawMessage(draft.Version.InputSchemaJSON), Grants: selectGrantContract(draft.Grants), DatasourceID: draft.Version.DatasourceID})
	return reportcontract.Compiled{SpecJSON: payload, Hashes: reportcontract.Hashes{Contract: reportdataset.Hash(json.RawMessage(payload)), ParameterSchema: reportdataset.Hash(json.RawMessage(draft.Version.InputSchemaJSON)), ProcedureSignature: reportdataset.Hash("SELECT_SNAPSHOT"), ResultSchema: c.SchemaHash, Permission: reportdataset.Hash(selectGrantContract(draft.Grants)), ExportSchema: reportdataset.Hash(c.Outputs)}}
}

type SelectReportContract struct {
	Dataset      reportdataset.Compiled `json:"dataset"`
	InputSchema  json.RawMessage        `json:"inputSchema"`
	Grants       []SelectGrantContract  `json:"grants"`
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
	result, hash, err := reportrepo.New().EffectiveSelectAuthorization(ctx, ds, c, actor)
	if err != nil {
		return result, hash, fmt.Errorf("%w: 字段或数据范围授权已变化，请重新发布或查询", ErrReportRunDenied)
	}
	return result, hash, nil
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

type selectMetadataInspector interface {
	ValidateBojunMetadata(context.Context, reportdataset.Table) error
}

type selectSourceInspector interface {
	selectMetadataInspector
	ValidateJSONSnapshotStore(context.Context) error
}

func inspectSelectSource(ctx context.Context, a selectSourceInspector, c reportdataset.Compiled) error {
	if e := a.ValidateJSONSnapshotStore(ctx); e != nil {
		return classifySelectSourceError("JSON snapshot store", e)
	}
	return inspectSelectMetadata(ctx, a, c)
}

// Draft samples only read the source and do not use the platform snapshot store.
func inspectSelectMetadata(ctx context.Context, a selectMetadataInspector, c reportdataset.Compiled) error {
	for _, t := range c.DependencyTables() {
		if e := a.ValidateBojunMetadata(ctx, t); e != nil {
			return classifySelectSourceError(t.Owner+"."+t.PhysicalTable, e)
		}
	}
	return nil
}

func classifySelectSourceError(target string, err error) error {
	if errors.Is(err, ErrReportSnapshotStoreNotReady) || strings.Contains(err.Error(), "REPORT_METADATA_CHANGED") {
		return fmt.Errorf("inspect SELECT source %s: %w", target, err)
	}
	return fmt.Errorf("%w: inspect SELECT source %s: %w", ErrReportDatasourceOracleUnavailable, target, err)
}
func canonicalSelectInputSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "{}" {
		return json.RawMessage(`{}`), nil
	}
	return canonicalReportInputSchema(raw)
}
func normalizeSelectConditions(schema model.JSONText, input map[string]json.RawMessage) ([]byte, error) {
	return normalizeSelectConditionsAt(schema, input, time.Now())
}
func normalizeSelectConditionsAt(schema model.JSONText, input map[string]json.RawMessage, now time.Time) ([]byte, error) {
	if strings.TrimSpace(string(schema)) == "{}" {
		if len(input) > 0 {
			return nil, ErrReportRunInvalid
		}
		return []byte(`{}`), nil
	}
	normalized, _, e := normalizeRefCursorConditionsAt([]byte(schema), input, now)
	return normalized, e
}
func (service *ReportRunService) createSelectRun(ctx context.Context, actor, definitionID uint, req requestbody.ReportRunCreateRequest, published *reportrepo.PublishedReport) (*ReportRunDTO, error) {
	if !selectReportsEnabled() {
		return nil, fmt.Errorf("%w: 自定义查询功能尚未启用", ErrReportRunInvalid)
	}
	if len(req.Parameters) > 0 && len(req.Conditions) > 0 {
		return nil, ErrReportRunInvalid
	}
	conditions := req.Conditions
	if conditions == nil {
		conditions = req.Parameters
	}
	now := service.now().UTC()
	parameters, e := normalizeSelectConditionsAt(published.Version.InputSchemaJSON, conditions, now)
	if e != nil {
		return nil, e
	}
	decoded, e := decodeSelectContract(published.Version)
	if e != nil {
		return nil, ErrReportRunInvalid
	}
	dataset, scope, e := selectAuthorization(ctx, published.Version.DatasourceID, decoded, actor)
	if e != nil {
		return nil, e
	}
	inputs := reportdataset.QueryInputs{}
	if req.QueryInputs != nil {
		inputs = *req.QueryInputs
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal(parameters, &values)
	if _, e = dataset.Build(values, inputs, strconv.FormatUint(uint64(actor), 10), false); e != nil {
		return nil, fmt.Errorf("%w: %v", ErrReportRunInvalid, e)
	}
	inputJSON, _ := json.Marshal(inputs)
	runUUID := uuid.NewString()
	fingerprint := reportdataset.Hash(struct {
		Parameters json.RawMessage
		Inputs     reportdataset.QueryInputs
		Scope      string
		Policy     reportdataset.Policy
	}{parameters, inputs, scope, dataset.Policy})
	command := &reportrepo.CreateRunCommand{Run: model.ReportRun{RunUUID: runUUID, DefinitionID: definitionID, VersionID: published.Version.ID, RequestedBy: actor, Status: model.ReportRunStatusQueued, ExecutionFingerprint: executionFingerprint(published.Version.ContractHash, fingerprint, req.RefreshNonce), RefreshNonce: req.RefreshNonce, NormalizedParametersJSON: model.JSONText(parameters), QueryInputsJSON: model.JSONText(inputJSON), DataScopeHash: scope, ExecutionPolicyJSON: published.Version.ExecutionPolicyJSON, ContractHash: published.Version.ContractHash, ProcedureSignatureHash: published.Version.ProcedureSignatureHash, ResultSchemaHash: published.Version.ResultSchemaHash}, Outbox: reportrepo.NewReportRunOutbox(runUUID, now)}
	if e = service.store.CreateRun(ctx, actor, definitionID, command); e != nil {
		return nil, classifyReportRunStoreError(e)
	}
	return &ReportRunDTO{ID: command.Run.ID, RunUUID: command.Run.RunUUID, DefinitionID: definitionID, VersionID: command.Run.VersionID, Status: command.Run.Status, CreatedAt: command.Run.CreatedAt}, nil
}

type SelectGrantContract struct {
	SubjectType string   `json:"subjectType"`
	SubjectID   uint     `json:"subjectId"`
	Actions     []string `json:"actions"`
}

func selectGrantContract(grants []model.ReportGrant) []SelectGrantContract {
	out := make([]SelectGrantContract, 0, len(grants))
	for _, grant := range grants {
		actions := []string{}
		_ = json.Unmarshal([]byte(grant.ActionsJSON), &actions)
		sort.Strings(actions)
		out = append(out, SelectGrantContract{SubjectType: grant.SubjectType, SubjectID: grant.SubjectID, Actions: actions})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SubjectType == out[j].SubjectType {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].SubjectType < out[j].SubjectType
	})
	return out
}
func validateSelectInputBindings(c reportdataset.Compiled, schema map[string]reportRunInputFieldSchema) error {
	aliases := map[string]reportdataset.Table{"base": c.Tables[c.Query.Base.TableID]}
	for _, r := range c.Query.Relations {
		aliases[r.ID] = c.Tables[r.TargetTableID]
	}
	for _, field := range schema {
		if field.QueryName != "" {
			return invalidReport("SELECT inputs must use relation options or approved constants")
		}
		if field.RelationID == "" {
			continue
		}
		r, ok := c.Relations[field.RelationID]
		if !ok || field.Control != "SELECT" || (field.Type != "str" && field.Type != "list[str]") {
			return invalidReport("input option relation is invalid")
		}
		validKey := field.RelationKeyColumnID == "" && len(r.Relation.KeyBindings) == 1
		for _, binding := range r.Relation.KeyBindings {
			if binding.TargetColumnID == field.RelationKeyColumnID {
				validKey = true
			}
		}
		if !validKey {
			return invalidReport("input option key is missing or not part of the relation")
		}
	}
	var check func(*reportdataset.Condition) error
	check = func(condition *reportdataset.Condition) error {
		if condition == nil {
			return nil
		}
		if condition.Value != nil && condition.Value.Kind == "PARAMETER" {
			field, ok := schema[condition.Value.Code]
			if !ok {
				return invalidReport("query references an undefined input parameter")
			}
			list := condition.Operator == "IN" || condition.Operator == "NOT_IN" || condition.Operator == "BETWEEN"
			if list != conditionTypeIsList(field.Type, false) {
				return invalidReport("parameter scalar/list shape does not match query operator")
			}
			typ := field.Type
			if list {
				typ = conditionListItemType(typ)
			}
			var col reportdataset.Column
			for _, candidate := range aliases[condition.AliasID].Columns {
				if candidate.ID == condition.ColumnID {
					col = candidate
					break
				}
			}
			if col.LogicalType == "string" && typ != "str" || col.LogicalType == "integer" && typ != "number" && typ != "str" || col.LogicalType == "decimal" && typ != "number" && typ != "str" || (col.LogicalType == "date" || col.LogicalType == "datetime") && typ != "str" {
				return invalidReport("parameter type does not match query column")
			}
		}
		for i := range condition.Items {
			if e := check(&condition.Items[i]); e != nil {
				return e
			}
		}
		return nil
	}
	if e := check(c.Query.Where); e != nil {
		return e
	}
	for _, r := range c.Query.Relations {
		if e := check(r.MatchConditions); e != nil {
			return e
		}
	}
	return nil
}
