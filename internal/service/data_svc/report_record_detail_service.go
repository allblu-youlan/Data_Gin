package data_svc

import (
	"context"
	"encoding/json"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"sort"
	"strconv"
	"time"
)

type ReportRecordDetailField struct {
	ColumnID     string `json:"columnId"`
	Name         string `json:"name"`
	Label        string `json:"label"`
	RawValue     any    `json:"rawValue"`
	DisplayValue any    `json:"displayValue"`
}
type ReportRecordDetail struct {
	Mode         string                    `json:"mode"`
	DetailSource string                    `json:"detailSource"`
	QueriedAt    time.Time                 `json:"queriedAt"`
	RelationName string                    `json:"relationName"`
	Fields       []ReportRecordDetailField `json:"fields"`
}

func (s *ReportDatasetService) RecordDetail(ctx context.Context, actor, runID uint, rowKey, relationID, mode string) (ReportRecordDetail, error) {
	if mode == "" {
		mode = "SNAPSHOT"
	}
	if mode != "SNAPSHOT" && mode != "LIVE" {
		return ReportRecordDetail{}, ErrReportRunQueryInvalid
	}
	contract, lease, e := s.repo.AcquireResultReadLease(ctx, actor, runID, time.Now().UTC())
	if e != nil {
		return ReportRecordDetail{}, classifyReportRunQueryError(e)
	}
	defer func() {
		state, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.repo.ReleaseResultReadLease(state, lease)
	}()
	if contract.Version.ExecutionMode != model.ReportExecutionModeSelectSnapshot {
		return ReportRecordDetail{}, ErrReportRunQueryNotFound
	}
	c, e := decodeSelectContract(contract.Version)
	if e != nil {
		return ReportRecordDetail{}, ErrReportRunQueryConflict
	}
	relation, ok := c.Relations[relationID]
	if !ok {
		return ReportRecordDetail{}, ErrReportRunQueryNotFound
	}
	key, e := selectCursorKey()
	if e != nil {
		return ReportRecordDetail{}, e
	}
	query := &ReportRunQueryService{cursorKey: key}
	rowID, e := query.decodeSelectRow(contract.Run, rowKey)
	if e != nil {
		return ReportRecordDetail{}, ErrReportRunQueryNotFound
	}
	password, e := (reportsecret.EnvironmentKeyring{}).Decrypt(contract.Datasource.CredentialKeyVersion, contract.Datasource.PasswordCiphertext)
	if e != nil {
		return ReportRecordDetail{}, ErrReportRunResultTemporary
	}
	names := c.SnapshotColumns()
	page, e := (oracleReportResultPageReader{}).Read(ctx, *contract, password, names, reportquery.Query{}, &reportoracle.ResultCursor{Key: strconv.FormatInt(rowID-1, 10)}, 1)
	if e != nil || len(page.Rows) != 1 || page.Rows[0].Key != strconv.FormatInt(rowID, 10) {
		return ReportRecordDetail{}, ErrReportRunQueryNotFound
	}
	values := map[string]any{}
	for i, name := range names {
		values[name] = page.Rows[0].Values[i]
	}
	for _, target := range relation.TargetKeys {
		if values[target] == nil {
			return ReportRecordDetail{}, ErrReportRunQueryNotFound
		}
	}
	currentValues := map[string]any{}
	for id, name := range relation.DetailNames {
		currentValues[id] = values[name]
	}
	queriedAt := contract.Run.FinishedAt
	if queriedAt == nil {
		now := time.Now().UTC()
		queriedAt = &now
	}
	if mode == "LIVE" {
		liveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c, hash, e := selectAuthorization(liveCtx, contract.Version.DatasourceID, c, actor)
		if e != nil || hash != contract.Run.DataScopeHash {
			return ReportRecordDetail{}, ErrReportRunQueryNotFound
		}
		var params map[string]json.RawMessage
		_ = json.Unmarshal([]byte(contract.Run.NormalizedParametersJSON), &params)
		keys := map[string]json.RawMessage{}
		for i, binding := range relation.Relation.KeyBindings {
			keys[binding.TargetColumnID], _ = json.Marshal(values[relation.TargetKeys[i]])
		}
		plan, e := c.BuildRelationPlan(relationID, params, c.SnapshotFieldValues(values), keys, "", nil, strconv.FormatUint(uint64(actor), 10), true)
		if e != nil {
			return ReportRecordDetail{}, ErrReportRunQueryNotFound
		}
		a, done, e := acquireSelectOracle(liveCtx, contract.Datasource, password)
		if e != nil {
			return ReportRecordDetail{}, ErrReportDatasourceOracleUnavailable
		}
		defer done()
		for _, t := range c.Tables {
			if t.ID == relation.Relation.TargetTableID {
				if e = a.ValidateBojunMetadata(liveCtx, t); e != nil {
					return ReportRecordDetail{}, ErrReportRunQueryConflict
				}
			}
		}
		_, rows, e := a.QuerySelectSample(liveCtx, plan.Plan, 2)
		if e != nil {
			return ReportRecordDetail{}, ErrReportDatasourceOracleUnavailable
		}
		if len(rows) != 1 {
			return ReportRecordDetail{}, ErrReportRunQueryNotFound
		}
		for id, name := range plan.ColumnNames {
			if _, ok := relation.DetailNames[id]; ok {
				currentValues[id] = rows[0][name]
			}
		}
		now := time.Now().UTC()
		queriedAt = &now
	}
	target := c.Tables[relation.Relation.TargetTableID]
	columns := append([]reportdataset.Column{}, target.Columns...)
	sort.Slice(columns, func(i, j int) bool { return columns[i].Ordinal < columns[j].Ordinal })
	out := ReportRecordDetail{Mode: mode, DetailSource: mode, QueriedAt: *queriedAt, RelationName: relation.Relation.Name, Fields: []ReportRecordDetailField{}}
	for _, col := range columns {
		if value, ok := currentValues[col.ID]; ok {
			out.Fields = append(out.Fields, ReportRecordDetailField{ColumnID: col.ID, Name: col.DBName, Label: col.Description, RawValue: value, DisplayValue: value})
		}
	}
	if out.RelationName == "" {
		out.RelationName = target.Description
	}
	s.audit(ctx, actor, "REPORT_RECORD_DETAIL", "REPORT_RUN", runID, map[string]any{"relationId": relationID, "mode": mode})
	return out, nil
}
