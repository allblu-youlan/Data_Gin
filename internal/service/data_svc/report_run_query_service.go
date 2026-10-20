package data_svc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
)

var (
	ErrReportSelectResultExpired = errors.New("REPORT_RESULT_EXPIRED")
	ErrReportRunQueryInvalid     = errors.New("report run query service: invalid input")
	ErrReportRunQueryNotFound    = errors.New("report run query service: not found")
	ErrReportRunQueryConflict    = errors.New("report run query service: conflict")
	ErrReportRunResultTemporary  = errors.New("report run query service: result temporarily unavailable")
)

const maxReportResultCursorLength = 1024

type reportRunQueryStore interface {
	FindRunForActor(context.Context, uint, uint) (*model.ReportRun, error)
	RequestRunCancellation(context.Context, uint, uint, time.Time) (*model.ReportRun, error)
	AcquireResultReadLease(context.Context, uint, uint, time.Time) (*reportrepo.RunResultContract, string, error)
	ReleaseResultReadLease(context.Context, string) error
	WriteReportAudit(context.Context, model.ReportAudit) error
}

type reportResultPageReader interface {
	Read(context.Context, reportrepo.RunResultContract, string, []string, reportquery.Query, *reportoracle.ResultCursor, int) (reportoracle.ResultPage, error)
}

type ReportRunViewDTO struct {
	ID              uint       `json:"id"`
	RunUUID         string     `json:"runUuid"`
	DefinitionID    uint       `json:"definitionId"`
	VersionID       uint       `json:"versionId"`
	Status          string     `json:"status"`
	RowCount        int64      `json:"rowCount"`
	CancelRequested bool       `json:"cancelRequested"`
	Attempt         int        `json:"attempt"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	ResultExpiresAt *time.Time `json:"resultExpiresAt,omitempty"`
	ErrorCode       string     `json:"errorCode,omitempty"`
	ErrorMessage    string     `json:"errorMessage,omitempty"`
	CanCancel       bool       `json:"canCancel"`
	ResultAvailable bool       `json:"resultAvailable"`
}

type ReportResultColumnDTO struct {
	RelationID       string                    `json:"relationId,omitempty"`
	ValueMode        string                    `json:"valueMode,omitempty"`
	Capabilities     *ReportResultCapabilities `json:"capabilities,omitempty"`
	FieldID          string                    `json:"fieldId"`
	Code             string                    `json:"code"`
	Header           string                    `json:"header"`
	ValueType        string                    `json:"valueType"`
	Nullable         bool                      `json:"nullable"`
	NullDisplay      string                    `json:"nullDisplay,omitempty"`
	Filterable       bool                      `json:"filterable"`
	Sortable         bool                      `json:"sortable"`
	AllowedOperators []string                  `json:"allowedOperators"`
}

type ReportResultRowDTO struct {
	DisplayValues  map[string]interface{}         `json:"displayValues,omitempty"`
	RelationStates map[string]ReportRelationState `json:"relationStates,omitempty"`
	Key            string                         `json:"key"`
	Values         map[string]interface{}         `json:"values"`
}

type ReportResultPaginationDTO struct {
	PageSize   int    `json:"pageSize"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type ReportResultPageDTO struct {
	Run        ReportRunViewDTO          `json:"run"`
	Columns    []ReportResultColumnDTO   `json:"columns"`
	Rows       []ReportResultRowDTO      `json:"rows"`
	Pagination ReportResultPaginationDTO `json:"pagination"`
}

type ReportRunQueryService struct {
	store      reportRunQueryStore
	credential reportDatasourceCredentialDecryptor
	reader     reportResultPageReader
	cursorKey  []byte
	now        func() time.Time
}

func NewReportRunQueryService() *ReportRunQueryService {
	jwtKey := config.GetString("cfg.jwt.key")
	if strings.TrimSpace(jwtKey) == "" {
		panic("report run query service: JWT signing key is required")
	}
	key := sha256.Sum256([]byte("report-result-cursor\x1f" + jwtKey))
	return NewReportRunQueryServiceWithDependencies(reportrepo.New(), reportsecret.EnvironmentKeyring{}, oracleReportResultPageReader{}, key[:])
}

func NewReportRunQueryServiceWithDependencies(store reportRunQueryStore, credential reportDatasourceCredentialDecryptor, reader reportResultPageReader, cursorKey []byte) *ReportRunQueryService {
	if store == nil || credential == nil || reader == nil || len(cursorKey) < 8 {
		panic("report run query service: dependencies and cursor key are required")
	}
	return &ReportRunQueryService{store: store, credential: credential, reader: reader, cursorKey: append([]byte(nil), cursorKey...), now: func() time.Time { return time.Now().UTC() }}
}

func (service *ReportRunQueryService) Get(ctx context.Context, actor, runID uint) (*ReportRunViewDTO, error) {
	if service == nil || ctx == nil || actor == 0 || runID == 0 {
		return nil, ErrReportRunQueryInvalid
	}
	run, err := service.store.FindRunForActor(ctx, actor, runID)
	if err != nil {
		return nil, classifyReportRunQueryError(err)
	}
	result := reportRunView(*run, service.now())
	return &result, nil
}

func (service *ReportRunQueryService) Cancel(ctx context.Context, actor, runID uint) (*ReportRunViewDTO, error) {
	if service == nil || ctx == nil || actor == 0 || runID == 0 {
		return nil, ErrReportRunQueryInvalid
	}
	run, err := service.store.RequestRunCancellation(ctx, actor, runID, service.now())
	if err != nil {
		return nil, classifyReportRunQueryError(err)
	}
	result := reportRunView(*run, service.now())
	return &result, nil
}

func (service *ReportRunQueryService) ReadResults(ctx context.Context, actor, runID uint, cursor string, limit int) (*ReportResultPageDTO, error) {
	return service.QueryResults(ctx, actor, runID, reportquery.Input{}, cursor, limit)
}

func (service *ReportRunQueryService) QueryResults(ctx context.Context, actor, runID uint, input reportquery.Input, cursor string, limit int) (*ReportResultPageDTO, error) {
	if service == nil || ctx == nil || actor == 0 || runID == 0 || limit < 1 || limit > 1000 || len(cursor) > maxReportResultCursorLength {
		return nil, ErrReportRunQueryInvalid
	}
	contract, leaseToken, err := service.store.AcquireResultReadLease(ctx, actor, runID, service.now())
	if err != nil {
		service.writeRejectedResultAudit(ctx, actor, runID, err)
		return nil, classifyReportRunQueryError(err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = service.store.ReleaseResultReadLease(releaseCtx, leaseToken)
	}()
	if contract.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
		return service.querySelectResults(ctx, *contract, input, cursor, limit)
	}
	allColumns, err := frozenResultColumns(contract.Run.PresentationSnapshotJSON)
	if err != nil {
		return nil, ErrReportRunQueryConflict
	}
	columns := previewColumns(allColumns)
	if len(columns) == 0 {
		return nil, ErrReportRunQueryConflict
	}
	query, err := reportquery.Normalize(input, reportQueryColumns(allColumns))
	if err != nil {
		return nil, ErrReportRunQueryInvalid
	}
	fingerprint, err := reportquery.Fingerprint(query)
	if err != nil {
		return nil, ErrReportRunResultTemporary
	}
	var after *reportoracle.ResultCursor
	if strings.TrimSpace(cursor) != "" {
		decoded, decodeErr := service.decodeCursor(cursor, contract.Run, limit, fingerprint)
		if decodeErr != nil {
			return nil, ErrReportRunQueryInvalid
		}
		after = &reportoracle.ResultCursor{Key: decoded.AfterKey, SortValue: decoded.SortValue}
	}
	password, err := service.credential.Decrypt(contract.Datasource.CredentialKeyVersion, contract.Datasource.PasswordCiphertext)
	if err != nil {
		return nil, ErrReportRunResultTemporary
	}
	databaseColumns := make([]string, len(columns))
	for index := range columns {
		databaseColumns[index] = columns[index].DatabaseColumn
	}
	page, err := service.reader.Read(ctx, *contract, password, databaseColumns, query, after, limit)
	if err != nil {
		return nil, ErrReportRunResultTemporary
	}
	result := &ReportResultPageDTO{
		Run: reportRunView(contract.Run, service.now()), Columns: make([]ReportResultColumnDTO, len(columns)),
		Rows: make([]ReportResultRowDTO, 0, len(page.Rows)), Pagination: ReportResultPaginationDTO{PageSize: limit, HasMore: page.HasNext},
	}
	queryColumnsByID := make(map[string]reportquery.Column, len(allColumns))
	for _, column := range reportQueryColumns(allColumns) {
		queryColumnsByID[column.FieldID] = column
	}
	for index, column := range columns {
		operators, _ := frozenAllowedOperators(column.AllowedOperators)
		capability := queryColumnsByID[column.FieldID]
		result.Columns[index] = ReportResultColumnDTO{FieldID: column.FieldID, Code: column.LogicalCode, Header: column.PreviewHeader, ValueType: column.ValueType, Nullable: column.Nullable, NullDisplay: column.NullDisplay, Filterable: capability.Filterable, Sortable: capability.Sortable, AllowedOperators: operators}
	}
	for _, row := range page.Rows {
		if len(row.Values) != len(columns) {
			return nil, ErrReportRunResultTemporary
		}
		values := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			values[column.LogicalCode] = reportResultValue(row.Values[index], column)
		}
		result.Rows = append(result.Rows, ReportResultRowDTO{Key: row.Key, Values: values})
	}
	if page.HasNext {
		cursor := reportResultCursor{Version: 3, RunUUID: contract.Run.RunUUID, ContractHash: contract.Run.ContractHash, PageSize: limit, QueryFingerprint: fingerprint, AfterKey: page.NextKey}
		if len(query.Sort) == 1 {
			last := page.Rows[len(page.Rows)-1]
			cursor.SortValue = last.SortValue
		}
		result.Pagination.NextCursor, err = service.encodeCursor(cursor)
		if err != nil {
			return nil, ErrReportRunResultTemporary
		}
	}
	detail, err := json.Marshal(map[string]interface{}{
		"pageSize": limit,
		"rowCount": len(result.Rows),
		"hasMore":  result.Pagination.HasMore,
	})
	if err != nil {
		return nil, ErrReportRunResultTemporary
	}
	if err := service.store.WriteReportAudit(ctx, model.ReportAudit{
		ActorUserID: actor,
		Action:      "REPORT_RESULT_QUERY_SUCCESS",
		TargetType:  "REPORT_RUN",
		TargetID:    runID,
		RequestID:   uuid.NewString(),
		DetailJSON:  model.JSONText(detail),
	}); err != nil {
		return nil, ErrReportRunResultTemporary
	}
	return result, nil
}

func (service *ReportRunQueryService) writeRejectedResultAudit(ctx context.Context, actor, runID uint, cause error) {
	detail, err := json.Marshal(map[string]string{"reason": reportRunQueryAuditReason(cause)})
	if err != nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = service.store.WriteReportAudit(auditCtx, model.ReportAudit{
		ActorUserID: actor,
		Action:      "REPORT_RESULT_QUERY_DENIED",
		TargetType:  "REPORT_RUN",
		TargetID:    runID,
		RequestID:   uuid.NewString(),
		DetailJSON:  model.JSONText(detail),
	})
}

func reportRunQueryAuditReason(err error) string {
	switch {
	case errors.Is(err, reportrepo.ErrReportRunAccessNotFound):
		return "NOT_FOUND"
	case errors.Is(err, reportrepo.ErrReportResultUnavailable):
		return "RESULT_UNAVAILABLE"
	default:
		return "ACCESS_FAILED"
	}
}

type frozenResultColumn struct {
	FieldID           string          `json:"fieldId"`
	LogicalCode       string          `json:"logicalCode"`
	DatabaseColumn    string          `json:"databaseColumn"`
	SourceOracleType  string          `json:"sourceOracleType"`
	Precision         *int            `json:"precision,omitempty"`
	Scale             *int            `json:"scale,omitempty"`
	Nullable          bool            `json:"nullable"`
	ValueType         string          `json:"valueType"`
	PreviewHeader     string          `json:"previewHeader"`
	ExcelHeader       string          `json:"excelHeader"`
	DisplayOrder      int             `json:"displayOrder"`
	ExportOrder       int             `json:"exportOrder"`
	PreviewVisible    bool            `json:"previewVisible"`
	ExportVisible     bool            `json:"exportVisible"`
	Filterable        bool            `json:"filterable"`
	Sortable          bool            `json:"sortable"`
	ExportAllowed     bool            `json:"exportAllowed"`
	AllowedOperators  json.RawMessage `json:"allowedOperators,omitempty"`
	Format            json.RawMessage `json:"format,omitempty"`
	DictionaryVersion json.RawMessage `json:"dictionaryVersion,omitempty"`
	MaskingPolicy     json.RawMessage `json:"maskingPolicy,omitempty"`
	ExcelWidth        float64         `json:"excelWidth"`
	NullDisplay       string          `json:"nullDisplay"`
}

func frozenResultColumns(raw model.JSONText) ([]frozenResultColumn, error) {
	var columns []frozenResultColumn
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&columns); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("presentation snapshot contains trailing data")
	}
	for _, column := range columns {
		if column.FieldID == "" || column.LogicalCode == "" || column.DatabaseColumn == "" {
			return nil, fmt.Errorf("presentation snapshot contains invalid column")
		}
		if _, err := frozenAllowedOperators(column.AllowedOperators); err != nil {
			return nil, err
		}
	}
	return columns, nil
}

func frozenPreviewColumns(raw model.JSONText) ([]frozenResultColumn, error) {
	columns, err := frozenResultColumns(raw)
	if err != nil {
		return nil, err
	}
	return previewColumns(columns), nil
}

func previewColumns(columns []frozenResultColumn) []frozenResultColumn {
	visible := make([]frozenResultColumn, 0, len(columns))
	for _, column := range columns {
		if column.PreviewVisible {
			visible = append(visible, column)
		}
	}
	return visible
}

func frozenAllowedOperators(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []string{}, nil
	}
	var operators []string
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operators); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("allowed operators contain trailing data")
	}
	allowed := map[string]struct{}{"EQ": {}, "NE": {}, "GT": {}, "GTE": {}, "LT": {}, "LTE": {}, "IN": {}, "NOT_IN": {}, "IS_NULL": {}, "IS_NOT_NULL": {}, "CONTAINS": {}, "STARTS_WITH": {}, "BETWEEN": {}}
	seen := make(map[string]struct{}, len(operators))
	result := make([]string, 0, len(operators))
	for _, operator := range operators {
		operator = strings.ToUpper(strings.TrimSpace(operator))
		if _, ok := allowed[operator]; !ok {
			return nil, fmt.Errorf("unsupported result operator")
		}
		if _, duplicate := seen[operator]; duplicate {
			continue
		}
		seen[operator] = struct{}{}
		result = append(result, operator)
	}
	return result, nil
}

func reportQueryColumns(columns []frozenResultColumn) []reportquery.Column {
	result := make([]reportquery.Column, 0, len(columns))
	for _, column := range columns {
		operators, _ := frozenAllowedOperators(column.AllowedOperators)
		masked := len(bytes.TrimSpace(column.MaskingPolicy)) > 0 && !bytes.Equal(bytes.TrimSpace(column.MaskingPolicy), []byte("{}")) && !bytes.Equal(bytes.TrimSpace(column.MaskingPolicy), []byte("null"))
		result = append(result, reportquery.Column{FieldID: column.FieldID, LogicalCode: column.LogicalCode, DatabaseColumn: column.DatabaseColumn, ValueType: column.ValueType, SourceOracleType: column.SourceOracleType, Nullable: column.Nullable, Filterable: column.PreviewVisible && column.Filterable && !masked, Sortable: column.PreviewVisible && column.Sortable && !masked, AllowedOperators: operators})
	}
	return result
}

func reportRunView(run model.ReportRun, now time.Time) ReportRunViewDTO {
	available := run.Status == model.ReportRunStatusSucceeded && run.ResultPurgedAt == nil && run.ResultExpiresAt != nil && now.Before(*run.ResultExpiresAt)
	return ReportRunViewDTO{
		ID: run.ID, RunUUID: run.RunUUID, DefinitionID: run.DefinitionID, VersionID: run.VersionID, Status: run.Status,
		RowCount: run.RowCount, CancelRequested: run.CancelRequested, Attempt: run.Attempt, CreatedAt: run.CreatedAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ResultExpiresAt: run.ResultExpiresAt,
		ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessageSafe,
		CanCancel: run.Status == model.ReportRunStatusQueued || (run.Status == model.ReportRunStatusRunning && !run.CancelRequested), ResultAvailable: available,
	}
}

func reportResultValue(value interface{}, column frozenResultColumn) interface{} {
	if value == nil {
		return nil
	}
	if policy := bytes.TrimSpace(column.MaskingPolicy); len(policy) > 0 && !bytes.Equal(policy, []byte("{}")) && !bytes.Equal(policy, []byte("null")) {
		return "***"
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	}
	switch strings.ToLower(column.ValueType) {
	case "integer", "decimal", "number":
		return fmt.Sprint(value)
	default:
		return value
	}
}

type reportResultCursor struct {
	Version          int                `json:"v"`
	RunUUID          string             `json:"runUuid"`
	ContractHash     string             `json:"contractHash"`
	PageSize         int                `json:"pageSize"`
	AfterKey         string             `json:"afterKey"`
	QueryFingerprint string             `json:"queryFingerprint"`
	SortValue        *reportquery.Value `json:"sortValue,omitempty"`
}

func (service *ReportRunQueryService) encodeCursor(cursor reportResultCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, service.cursorKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (service *ReportRunQueryService) decodeCursor(value string, run model.ReportRun, limit int, fingerprint string) (reportResultCursor, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return reportResultCursor{}, ErrReportRunQueryInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return reportResultCursor{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return reportResultCursor{}, err
	}
	mac := hmac.New(sha256.New, service.cursorKey)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return reportResultCursor{}, ErrReportRunQueryInvalid
	}
	var cursor reportResultCursor
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 3 || strings.TrimSpace(cursor.AfterKey) == "" || cursor.RunUUID != run.RunUUID || cursor.ContractHash != run.ContractHash || cursor.PageSize != limit || cursor.QueryFingerprint != fingerprint {
		return reportResultCursor{}, ErrReportRunQueryInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return reportResultCursor{}, ErrReportRunQueryInvalid
	}
	return cursor, nil
}

func classifyReportRunQueryError(err error) error {
	if errors.Is(err, reportrepo.ErrReportSelectResultExpired) {
		return ErrReportSelectResultExpired
	}
	if errors.Is(err, reportrepo.ErrReportActionDenied) {
		return ErrReportRunQueryNotFound
	}
	switch {
	case errors.Is(err, reportrepo.ErrReportRunAccessNotFound):
		return ErrReportRunQueryNotFound
	case errors.Is(err, reportrepo.ErrReportRunStateConflict), errors.Is(err, reportrepo.ErrReportResultUnavailable):
		return ErrReportRunQueryConflict
	default:
		return fmt.Errorf("report run query service: store: %w", err)
	}
}

type oracleReportResultPageReader struct{}

func (oracleReportResultPageReader) Read(ctx context.Context, contract reportrepo.RunResultContract, password string, columns []string, query reportquery.Query, after *reportoracle.ResultCursor, limit int) (reportoracle.ResultPage, error) {
	queryCtx, cancel := reportOracleQueryContext(ctx, contract.Datasource)
	defer cancel()
	adapter, err := reportoracle.Open(queryCtx, oracleConfigFromDatasource(contract.Datasource, password))
	if err != nil {
		return reportoracle.ResultPage{}, err
	}
	defer func() { _ = adapter.Close() }()
	if contract.Version.UsesJSONSnapshot() {
		if len(query.Filters) != 0 || len(query.Sort) != 0 {
			return reportoracle.ResultPage{}, fmt.Errorf("JSON cursor report result filtering is not supported; pass filters in conditions")
		}
		if err := adapter.ValidateJSONSnapshotStore(queryCtx); err != nil {
			return reportoracle.ResultPage{}, err
		}
		afterRowID := int64(0)
		if after != nil {
			afterRowID, err = strconv.ParseInt(after.Key, 10, 64)
			if err != nil || afterRowID < 0 {
				return reportoracle.ResultPage{}, fmt.Errorf("JSON cursor report result cursor is invalid")
			}
		}
		if contract.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
			if _, err := adapter.ValidateSelectSnapshot(queryCtx, contract.Run.RunUUID, contract.Run.ResultSchemaHash); err != nil {
				return reportoracle.ResultPage{}, err
			}
		}
		return adapter.ReadJSONSnapshotPage(queryCtx, contract.Run.RunUUID, columns, afterRowID, limit)
	}
	ref := reportoracle.ResultTableSnapshotRef(
		reportoracle.ResultTableRef{Owner: contract.Version.ResultTableOwner, Name: contract.Version.ResultTableName}, columns,
	)
	resultColumns, err := adapter.InspectResultTable(queryCtx, ref.Table)
	if err != nil {
		return reportoracle.ResultPage{}, err
	}
	if err := reportcontract.VerifyRuntimeResultMetadata(
		[]byte(contract.Version.CompiledSpecJSON), contract.Run.ContractHash, contract.Run.ResultSchemaHash, resultColumns,
	); err != nil {
		return reportoracle.ResultPage{}, err
	}
	snapshot, err := adapter.InspectResultSnapshotContract(queryCtx, ref)
	if err != nil {
		return reportoracle.ResultPage{}, err
	}
	plan, err := reportoracle.BuildResultQueryPlan(snapshot, columns, query)
	if err != nil {
		return reportoracle.ResultPage{}, err
	}
	return adapter.ReadResultPage(queryCtx, plan, after, limit)
}
