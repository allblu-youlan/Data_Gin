package data_svc

import (
	"context"
	"encoding/json"
	"time"

	"gin-biz-web-api/internal/reportrepo"
)

type reportVersionStore interface {
	ListPublishedVersions(context.Context, uint, uint, reportrepo.VersionListQuery) (reportrepo.VersionPage, error)
	FindPublishedVersionSummary(context.Context, uint, uint, uint) (reportrepo.VersionSummary, error)
	FindPublishedVersion(context.Context, uint, uint, uint) (*reportrepo.Draft, error)
}

type ReportVersionService struct{ store reportVersionStore }

func NewReportVersionService(store reportVersionStore) *ReportVersionService {
	if store == nil {
		panic("report version: nil store")
	}
	return &ReportVersionService{store: store}
}

type ReportVersionSummaryDTO struct {
	ID                  uint       `json:"id"`
	Version             uint64     `json:"version"`
	Status              string     `json:"status"`
	PublishedAt         *time.Time `json:"publishedAt"`
	ContractFingerprint string     `json:"contractFingerprint"`
	ParameterCount      int        `json:"parameterCount"`
	ColumnCount         int        `json:"columnCount"`
	GrantCount          int        `json:"grantCount"`
}
type ReportVersionPageDTO struct {
	Items       []ReportVersionSummaryDTO `json:"items"`
	HasMore     bool                      `json:"hasMore"`
	NextAfterID uint                      `json:"nextAfterId"`
}
type ReportVersionChangeDTO struct {
	Kind   string      `json:"kind"`
	Key    string      `json:"key"`
	Label  string      `json:"label"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
}
type ReportVersionDiffSectionDTO struct {
	Key     string                   `json:"key"`
	Label   string                   `json:"label"`
	Changes []ReportVersionChangeDTO `json:"changes"`
}
type ReportVersionDiffDTO struct {
	Base     ReportVersionSummaryDTO       `json:"base"`
	Target   ReportVersionSummaryDTO       `json:"target"`
	Sections []ReportVersionDiffSectionDTO `json:"sections"`
}

type ReportVersionConfigurationDTO struct {
	DatasourceID  uint                 `json:"datasourceId"`
	Procedure     ReportProcedureDTO   `json:"procedure"`
	ExecutionMode string               `json:"executionMode"`
	InputSchema   json.RawMessage      `json:"inputSchema"`
	Result        ReportResultDTO      `json:"result"`
	CallTemplate  string               `json:"callTemplate"`
	Parameters    []ReportParameterDTO `json:"parameters"`
	Columns       []ReportColumnDTO    `json:"columns"`
	Grants        []ReportGrantDTO     `json:"grants"`
}

type ReportVersionDetailDTO struct {
	Summary       ReportVersionSummaryDTO       `json:"summary"`
	Configuration ReportVersionConfigurationDTO `json:"configuration"`
}

func (service *ReportVersionService) List(ctx context.Context, actor, definitionID, afterID uint, limit int) (*ReportVersionPageDTO, error) {
	if limit == 0 {
		limit = defaultReportDraftPageSize
	}
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 || limit < 1 || limit > reportrepo.MaxVersionPageSize {
		return nil, invalidReport("invalid version list request")
	}
	page, err := service.store.ListPublishedVersions(ctx, actor, definitionID, reportrepo.VersionListQuery{AfterID: afterID, Limit: limit})
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	result := &ReportVersionPageDTO{Items: make([]ReportVersionSummaryDTO, 0, len(page.Items)), HasMore: page.HasMore, NextAfterID: page.NextAfterID}
	for _, item := range page.Items {
		result.Items = append(result.Items, versionSummaryDTO(item))
	}
	return result, nil
}

func (service *ReportVersionService) Diff(ctx context.Context, actor, definitionID, baseVersionID, targetVersionID uint) (*ReportVersionDiffDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 || baseVersionID == 0 || targetVersionID == 0 || baseVersionID == targetVersionID {
		return nil, invalidReport("invalid version diff request")
	}
	base, err := service.store.FindPublishedVersionSummary(ctx, actor, definitionID, baseVersionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	target, err := service.store.FindPublishedVersionSummary(ctx, actor, definitionID, targetVersionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	baseDTO, targetDTO := versionSummaryDTO(base), versionSummaryDTO(target)
	sections := []ReportVersionDiffSectionDTO{
		diffSection("procedure", "存储过程", hashChange("procedureSignatureHash", "过程签名", base.Version.ProcedureSignatureHash, target.Version.ProcedureSignatureHash)),
		diffSection("parameters", "{{形参}}", countAndHashChanges(base.ParameterCount, target.ParameterCount, base.Version.ParameterSchemaHash, target.Version.ParameterSchemaHash)),
		diffSection("results", "结果字段与 Excel", append(countChange("columnCount", "字段数量", base.ColumnCount, target.ColumnCount), hashChange("resultSchemaHash", "结果 Schema", base.Version.ResultSchemaHash, target.Version.ResultSchemaHash)...)),
		diffSection("excel", "Excel 契约", hashChange("exportSchemaHash", "Excel Schema", base.Version.ExportSchemaHash, target.Version.ExportSchemaHash)),
		diffSection("permissions", "权限", append(countChange("grantCount", "授权数量", base.GrantCount, target.GrantCount), hashChange("permissionHash", "权限契约", base.Version.PermissionHash, target.Version.PermissionHash)...)),
	}
	return &ReportVersionDiffDTO{Base: baseDTO, Target: targetDTO, Sections: sections}, nil
}

func (service *ReportVersionService) Get(ctx context.Context, actor, definitionID, versionID uint) (*ReportVersionDetailDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 || versionID == 0 {
		return nil, invalidReport("invalid version detail request")
	}
	draft, err := service.store.FindPublishedVersion(ctx, actor, definitionID, versionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	detail := reportDraftDTO(draft)
	return &ReportVersionDetailDTO{
		Summary: versionSummaryDTO(reportrepo.VersionSummary{Version: draft.Version, ParameterCount: len(draft.Parameters), ColumnCount: len(draft.Columns), GrantCount: len(draft.Grants)}),
		Configuration: ReportVersionConfigurationDTO{
			DatasourceID: draft.Version.DatasourceID, Procedure: detail.Procedure, ExecutionMode: detail.ExecutionMode,
			InputSchema: detail.InputSchema, Result: detail.Result, CallTemplate: detail.CallTemplate,
			Parameters: detail.Parameters, Columns: detail.Columns, Grants: detail.Grants,
		},
	}, nil
}

func versionSummaryDTO(item reportrepo.VersionSummary) ReportVersionSummaryDTO {
	v := item.Version
	return ReportVersionSummaryDTO{ID: v.ID, Version: v.VersionNumber, Status: v.Status, PublishedAt: v.PublishedAt, ContractFingerprint: shortVersionHash(v.ContractHash), ParameterCount: item.ParameterCount, ColumnCount: item.ColumnCount, GrantCount: item.GrantCount}
}
func diffSection(key, label string, changes []ReportVersionChangeDTO) ReportVersionDiffSectionDTO {
	return ReportVersionDiffSectionDTO{Key: key, Label: label, Changes: changes}
}
func countAndHashChanges(beforeCount, afterCount int, beforeHash, afterHash string) []ReportVersionChangeDTO {
	return append(countChange("parameterCount", "参数数量", beforeCount, afterCount), hashChange("parameterSchemaHash", "参数 Schema", beforeHash, afterHash)...)
}
func countChange(key, label string, before, after int) []ReportVersionChangeDTO {
	if before == after {
		return []ReportVersionChangeDTO{}
	}
	return []ReportVersionChangeDTO{{Kind: "CHANGED", Key: key, Label: label, Before: before, After: after}}
}
func hashChange(key, label, before, after string) []ReportVersionChangeDTO {
	if before == after {
		return []ReportVersionChangeDTO{}
	}
	return []ReportVersionChangeDTO{{Kind: "CHANGED", Key: key, Label: label, Before: shortVersionHash(before), After: shortVersionHash(after)}}
}
func shortVersionHash(value string) string {
	if len(value) != 64 {
		return "-"
	}
	return value[:12]
}
