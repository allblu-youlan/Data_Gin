package data_svc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
	"strconv"
	"strings"
)

type ReportResultCapabilities struct {
	CanReadRaw    bool `json:"canReadRaw"`
	CanViewDetail bool `json:"canViewDetail"`
}
type ReportRelationState struct {
	Status        string `json:"status"`
	CanViewDetail bool   `json:"canViewDetail"`
}

func selectResultColumns(c reportdataset.Compiled) []ReportResultColumnDTO {
	out := []ReportResultColumnDTO{}
	for _, column := range c.Outputs {
		if !column.PreviewVisible {
			continue
		}
		dto := ReportResultColumnDTO{FieldID: column.FieldID, Code: column.Code, Header: column.Header, ValueType: column.LogicalType, Nullable: column.Nullable, AllowedOperators: []string{}, RelationID: column.RelationID, ValueMode: column.ValueMode}
		if column.RelationID != "" {
			dto.Capabilities = &ReportResultCapabilities{CanReadRaw: column.RawName != "", CanViewDetail: true}
		}
		out = append(out, dto)
	}
	return out
}
func selectResultRow(c reportdataset.Compiled, values map[string]any, key string) ReportResultRowDTO {
	row := ReportResultRowDTO{Key: key, Values: map[string]any{}, DisplayValues: map[string]any{}, RelationStates: map[string]ReportRelationState{}}
	for id, relation := range c.Relations {
		state := ReportRelationState{Status: "MATCHED", CanViewDetail: true}
		for _, name := range relation.SourceKeys {
			if values[name] == nil {
				state = ReportRelationState{Status: "EMPTY_KEY"}
				break
			}
		}
		if state.Status == "MATCHED" {
			for _, name := range relation.TargetKeys {
				if values[name] == nil {
					state = ReportRelationState{Status: "UNMATCHED"}
					break
				}
			}
		}
		row.RelationStates[id] = state
	}
	for _, column := range c.Outputs {
		if !column.PreviewVisible {
			continue
		}
		if column.RawName != "" {
			row.Values[column.Code] = c.RawValue(c.Relations[column.RelationID], values)
			row.DisplayValues[column.Code] = values[column.Name]
		} else {
			row.Values[column.Code] = values[column.Name]
		}
	}
	return row
}
func (s *ReportRunQueryService) querySelectResults(ctx context.Context, contract reportrepo.RunResultContract, input reportquery.Input, cursor string, limit int) (*ReportResultPageDTO, error) {
	if len(input.Filters) > 0 || len(input.Sort) > 0 {
		return nil, fmt.Errorf("%w: 修改条件或排序后请重新生成报表", ErrReportRunQueryInvalid)
	}
	c, e := decodeSelectContract(contract.Version)
	if e != nil {
		return nil, ErrReportRunQueryConflict
	}
	fingerprint := reportdataset.Hash("SELECT_SNAPSHOT_PAGINATION")
	var after *reportoracle.ResultCursor
	if cursor != "" {
		v, e := s.decodeCursor(cursor, contract.Run, limit, fingerprint)
		if e != nil {
			return nil, ErrReportRunQueryInvalid
		}
		after = &reportoracle.ResultCursor{Key: v.AfterKey}
	}
	password, e := s.credential.Decrypt(contract.Datasource.CredentialKeyVersion, contract.Datasource.PasswordCiphertext)
	if e != nil {
		return nil, ErrReportRunResultTemporary
	}
	names := c.SnapshotColumns()
	page, e := s.reader.Read(ctx, contract, password, names, reportquery.Query{}, after, limit)
	if e != nil {
		return nil, ErrReportRunResultTemporary
	}
	result := &ReportResultPageDTO{Run: reportRunView(contract.Run, s.now()), Columns: selectResultColumns(c), Rows: []ReportResultRowDTO{}, Pagination: ReportResultPaginationDTO{PageSize: limit, HasMore: page.HasNext}}
	for _, row := range page.Rows {
		if len(row.Values) != len(names) {
			return nil, ErrReportRunResultTemporary
		}
		values := map[string]any{}
		for i, name := range names {
			values[name] = row.Values[i]
		}
		key, e := s.signSelectRow(contract.Run, row.Key)
		if e != nil {
			return nil, ErrReportRunResultTemporary
		}
		result.Rows = append(result.Rows, selectResultRow(c, values, key))
	}
	if page.HasNext {
		result.Pagination.NextCursor, e = s.encodeCursor(reportResultCursor{Version: 3, RunUUID: contract.Run.RunUUID, ContractHash: contract.Run.ContractHash, PageSize: limit, QueryFingerprint: fingerprint, AfterKey: page.NextKey})
		if e != nil {
			return nil, e
		}
	}
	return result, nil
}

type selectRowKey struct {
	RunUUID      string `json:"run"`
	ContractHash string `json:"contract"`
	Row          string `json:"row"`
}

func signSelectToken(key []byte, payload any) (string, error) {
	raw, e := json.Marshal(payload)
	if e != nil {
		return "", e
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func decodeSelectToken(key []byte, token string, out any) error {
	if len(token) > 4096 {
		return ErrReportRunQueryInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return ErrReportRunQueryInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return ErrReportRunQueryInvalid
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return ErrReportRunQueryInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return ErrReportRunQueryInvalid
	}
	return json.Unmarshal(raw, out)
}
func (s *ReportRunQueryService) signSelectRow(run model.ReportRun, row string) (string, error) {
	return signSelectToken(s.cursorKey, selectRowKey{RunUUID: run.RunUUID, ContractHash: run.ContractHash, Row: row})
}
func (s *ReportRunQueryService) decodeSelectRow(run model.ReportRun, token string) (int64, error) {
	var key selectRowKey
	if e := decodeSelectToken(s.cursorKey, token, &key); e != nil || key.RunUUID != run.RunUUID || key.ContractHash != run.ContractHash {
		return 0, ErrReportRunQueryInvalid
	}
	row, e := strconv.ParseInt(key.Row, 10, 64)
	if e != nil || row < 1 {
		return 0, ErrReportRunQueryInvalid
	}
	return row, nil
}
