package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"strconv"
	"strings"
	"time"
)

type ReportRelationOption struct {
	KeyValues    map[string]string `json:"keyValues"`
	DisplayValue string            `json:"displayValue"`
}
type ReportRelationOptions struct {
	Items      []ReportRelationOption `json:"items"`
	HasMore    bool                   `json:"hasMore"`
	NextCursor string                 `json:"nextCursor"`
}
type ReportRelationOptionsRequest struct {
	VersionID        uint                       `json:"versionId"`
	RelationID       string                     `json:"relationId"`
	Search           string                     `json:"search"`
	Cursor           string                     `json:"cursor,omitempty"`
	Limit            int                        `json:"limit"`
	DependencyValues map[string]json.RawMessage `json:"dependencyValues,omitempty"`
}
type relationOptionsCursor struct {
	Identity string                     `json:"identity"`
	After    map[string]json.RawMessage `json:"after"`
}

func (s *ReportDatasetService) RelationOptions(ctx context.Context, actor, id uint, req ReportRelationOptionsRequest) (ReportRelationOptions, error) {
	if !selectReportsEnabled() {
		return ReportRelationOptions{}, ErrReportRunInvalid
	}
	if req.Limit < 1 {
		req.Limit = 20
	}
	if req.Limit > 100 || len(req.Search) > 255 {
		return ReportRelationOptions{}, ErrReportRunInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	published, e := s.repo.FindPublishedReport(ctx, actor, id, reportrepo.ReportActionQuery)
	if e != nil {
		return ReportRelationOptions{}, classifyReportRunStoreError(e)
	}
	if published.Version.ID != req.VersionID || published.Version.ExecutionMode != model.ReportExecutionModeSelectSnapshot {
		return ReportRelationOptions{}, ErrReportRunInvalid
	}
	c, e := decodeSelectContract(published.Version)
	if e != nil {
		return ReportRelationOptions{}, ErrReportRunInvalid
	}
	c, scope, e := selectAuthorization(ctx, published.Version.DatasourceID, c, actor)
	if e != nil {
		return ReportRelationOptions{}, e
	}
	r, ok := c.Relations[req.RelationID]
	if !ok {
		return ReportRelationOptions{}, ErrReportRunInvalid
	}
	key, e := selectCursorKey()
	if e != nil {
		return ReportRelationOptions{}, e
	}
	identity := reportdataset.Hash(struct {
		Actor, Version          uint
		Relation, Search, Scope string
		Dependencies            map[string]json.RawMessage
		Limit                   int
	}{actor, req.VersionID, req.RelationID, req.Search, scope, req.DependencyValues, req.Limit})
	var after map[string]json.RawMessage
	if req.Cursor != "" {
		var cursor relationOptionsCursor
		if e = decodeSelectToken(key, req.Cursor, &cursor); e != nil || cursor.Identity != identity {
			return ReportRelationOptions{}, ErrReportRunInvalid
		}
		after = cursor.After
	}
	source := map[string]map[string]json.RawMessage{}
	params := map[string]json.RawMessage{}
	for name, value := range req.DependencyValues {
		parts := strings.SplitN(name, ".", 2)
		if len(parts) == 2 {
			if source[parts[0]] == nil {
				source[parts[0]] = map[string]json.RawMessage{}
			}
			source[parts[0]][parts[1]] = value
		} else {
			params[name] = value
		}
	}
	plan, e := c.BuildRelationPlan(req.RelationID, params, source, nil, req.Search, after, strconv.FormatUint(uint64(actor), 10), false)
	if e != nil {
		return ReportRelationOptions{}, invalidReport(e.Error())
	}
	ds, e := s.repo.GetReportDatasource(ctx, published.Version.DatasourceID)
	if e != nil {
		return ReportRelationOptions{}, e
	}
	password, e := (reportsecret.EnvironmentKeyring{}).Decrypt(ds.CredentialKeyVersion, ds.PasswordCiphertext)
	if e != nil {
		return ReportRelationOptions{}, ErrReportDatasourceCredentialUnavailable
	}
	a, done, e := acquireSelectOracle(ctx, *ds, password)
	if e != nil {
		return ReportRelationOptions{}, ErrReportDatasourceOracleUnavailable
	}
	defer done()
	_, rows, e := a.QuerySelectSample(ctx, plan.Plan, req.Limit+1)
	if e != nil {
		return ReportRelationOptions{}, ErrReportDatasourceOracleUnavailable
	}
	out := ReportRelationOptions{Items: []ReportRelationOption{}, HasMore: len(rows) > req.Limit}
	if out.HasMore {
		rows = rows[:req.Limit]
	}
	for _, row := range rows {
		keys := map[string]string{}
		for _, binding := range r.Relation.KeyBindings {
			value := row[plan.ColumnNames[binding.TargetColumnID]]
			keys[binding.TargetColumnID] = fmt.Sprint(value)
		}
		display := []string{}
		for _, id := range plan.DisplayColumns {
			if value := row[plan.ColumnNames[id]]; value != nil && fmt.Sprint(value) != "" {
				display = append(display, fmt.Sprint(value))
			}
		}
		if len(display) == 0 {
			for _, id := range plan.KeyColumns {
				display = append(display, fmt.Sprint(row[plan.ColumnNames[id]]))
			}
		}
		out.Items = append(out.Items, ReportRelationOption{KeyValues: keys, DisplayValue: strings.Join(display, " · ")})
	}
	if out.HasMore {
		last := rows[len(rows)-1]
		after := map[string]json.RawMessage{}
		for _, id := range plan.KeyColumns {
			after[id], _ = json.Marshal(last[plan.ColumnNames[id]])
		}
		out.NextCursor, e = signSelectToken(key, relationOptionsCursor{Identity: identity, After: after})
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
