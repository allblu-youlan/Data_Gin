package data_svc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

var (
	ErrReportDatasourceInvalid               = errors.New("report datasource service: invalid input")
	ErrReportDatasourceNotFound              = errors.New("report datasource service: not found")
	ErrReportDatasourceConflict              = errors.New("report datasource service: conflict")
	ErrReportDatasourceCredentialUnavailable = errors.New("report datasource service: credential configuration unavailable")
	ErrReportDatasourceOracleUnavailable     = errors.New("report datasource service: oracle metadata unavailable")
)

const (
	reportDatasourceTestSuccess = "SUCCESS"
	reportDatasourceTestFailed  = "FAILED"
)

var reportDatasourceCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

type reportDatasourceStore interface {
	ManagedResultTableRefs(context.Context, uint, []reportoracle.ResultTableRef) (map[reportoracle.ResultTableRef]bool, error)
	ListReportDatasources(context.Context) ([]model.ReportDatasource, error)
	GetReportDatasource(context.Context, uint) (*model.ReportDatasource, error)
	CreateReportDatasource(context.Context, uint, *model.ReportDatasource) error
	UpdateReportDatasource(context.Context, uint, *model.ReportDatasource) error
	RecordReportDatasourceTest(context.Context, uint, uint, string, string, time.Time) error
}

type reportDatasourceCipher interface {
	Encrypt(string) (string, string, error)
	Decrypt(string, string) (string, error)
}

type reportDatasourceConnection interface {
	Close() error
	ListProcedures(context.Context, reportoracle.ProcedureCatalogQuery) ([]reportoracle.ProcedureSummary, error)
	InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error)
	ListResultTables(context.Context, reportoracle.ResultTableCatalogQuery) ([]reportoracle.ResultTableSummary, error)
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
}
type reportDatasourceOpener func(context.Context, reportoracle.Config) (reportDatasourceConnection, error)

type ReportDatasourceService struct {
	store  reportDatasourceStore
	cipher reportDatasourceCipher
	open   reportDatasourceOpener
	now    func() time.Time
}

type ReportDatasourceDTO struct {
	ID                    uint       `json:"id"`
	Code                  string     `json:"code"`
	Name                  string     `json:"name"`
	Driver                string     `json:"driver"`
	Host                  string     `json:"host"`
	Port                  int        `json:"port"`
	ServiceName           string     `json:"serviceName"`
	SID                   string     `json:"sid"`
	Username              string     `json:"username"`
	HasPassword           bool       `json:"hasPassword"`
	SessionTimezone       string     `json:"sessionTimezone"`
	ConnectTimeoutSeconds int        `json:"connectTimeoutSeconds"`
	QueryTimeoutSeconds   int        `json:"queryTimeoutSeconds"`
	MaxOpenConnections    int        `json:"maxOpenConnections"`
	MaxIdleConnections    int        `json:"maxIdleConnections"`
	PrefetchRows          int        `json:"prefetchRows"`
	ArraySize             int        `json:"arraySize"`
	Enabled               bool       `json:"enabled"`
	LastTestStatus        string     `json:"lastTestStatus"`
	LastTestError         string     `json:"lastTestError"`
	LastTestedAt          *time.Time `json:"lastTestedAt"`
}

type ReportDatasourceTestDTO struct {
	Status    string    `json:"status"`
	TestedAt  time.Time `json:"testedAt"`
	LatencyMS int64     `json:"latencyMs"`
	ErrorCode string    `json:"errorCode,omitempty"`
	Message   string    `json:"message"`
}

type ReportProcedureCatalogQuery struct {
	Owner  string
	Search string
	After  string
	Limit  int
}

type ReportProcedureSummaryDTO struct {
	Owner         string `json:"owner"`
	Package       string `json:"package"`
	Name          string `json:"name"`
	Overload      string `json:"overload"`
	ArgumentCount int    `json:"argumentCount"`
	QualifiedName string `json:"qualifiedName"`
}

type ReportProcedurePageDTO struct {
	Items     []ReportProcedureSummaryDTO `json:"items"`
	HasMore   bool                        `json:"hasMore"`
	NextAfter string                      `json:"nextAfter"`
}

type ReportResultTableCatalogQuery struct {
	Owner  string
	Search string
	After  string
	Limit  int
}

type ReportResultTableSummaryDTO struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName"`
	ColumnCount   int    `json:"columnCount"`
}

type ReportResultTablePageDTO struct {
	Items     []ReportResultTableSummaryDTO `json:"items"`
	HasMore   bool                          `json:"hasMore"`
	NextAfter string                        `json:"nextAfter"`
}

type ReportResultTableColumnDTO struct {
	Description string `json:"description,omitempty"`
	Name        string `json:"name"`
	Position    int    `json:"position"`
	OracleType  string `json:"oracleType"`
	DataLength  int64  `json:"dataLength"`
	Precision   *int64 `json:"precision"`
	Scale       *int64 `json:"scale"`
	Nullable    bool   `json:"nullable"`
}

type ReportResultTableSchemaDTO struct {
	Table   ReportResultTableSummaryDTO  `json:"table"`
	Columns []ReportResultTableColumnDTO `json:"columns"`
}

type ReportProcedureArgumentDTO struct {
	Name                 string `json:"name"`
	Position             int    `json:"position"`
	Sequence             int    `json:"sequence"`
	Direction            string `json:"direction"`
	OracleType           string `json:"oracleType"`
	DataLength           *int64 `json:"dataLength"`
	Precision            *int64 `json:"precision"`
	Scale                *int64 `json:"scale"`
	TypeOwner            string `json:"typeOwner"`
	TypeName             string `json:"typeName"`
	Defaulted            bool   `json:"defaulted"`
	Supported            bool   `json:"supported"`
	UnsupportedReason    string `json:"unsupportedReason,omitempty"`
	SuggestedCode        string `json:"suggestedCode"`
	SuggestedLogicalType string `json:"suggestedLogicalType"`
	SuggestedControlType string `json:"suggestedControlType"`
	SuggestedSystemValue string `json:"suggestedSystemValue,omitempty"`
	Role                 string `json:"role"`
}

type ReportProcedureSignatureDTO struct {
	Procedure       ReportProcedureSummaryDTO    `json:"procedure"`
	Arguments       []ReportProcedureArgumentDTO `json:"arguments"`
	AllSupported    bool                         `json:"allSupported"`
	ProtocolReady   bool                         `json:"protocolReady"`
	InputArgName    string                       `json:"inputArgName"`
	OutputArgName   string                       `json:"outputArgName"`
	CallTemplate    string                       `json:"callTemplate"`
	BlockingReasons []string                     `json:"blockingReasons"`
}

func NewReportDatasourceService() *ReportDatasourceService {
	return NewReportDatasourceServiceWithDependencies(reportrepo.New(), reportsecret.EnvironmentKeyring{}, func(ctx context.Context, config reportoracle.Config) (reportDatasourceConnection, error) {
		return reportoracle.Open(ctx, config)
	})
}

func NewReportDatasourceServiceWithDependencies(store reportDatasourceStore, cipher reportDatasourceCipher, open reportDatasourceOpener) *ReportDatasourceService {
	if store == nil || cipher == nil || open == nil {
		panic("report datasource service: nil dependency")
	}
	return &ReportDatasourceService{store: store, cipher: cipher, open: open, now: func() time.Time { return time.Now().UTC() }}
}

func (service *ReportDatasourceService) List(ctx context.Context, actor uint) ([]ReportDatasourceDTO, error) {
	if service == nil || ctx == nil || actor == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	items, err := service.store.ListReportDatasources(ctx)
	if err != nil {
		return nil, fmt.Errorf("report datasource service: list: %w", err)
	}
	result := make([]ReportDatasourceDTO, 0, len(items))
	for _, item := range items {
		result = append(result, reportDatasourceDTO(item))
	}
	return result, nil
}

func (service *ReportDatasourceService) Get(ctx context.Context, actor, datasourceID uint) (*ReportDatasourceDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	datasource, err := service.store.GetReportDatasource(ctx, datasourceID)
	if err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	result := reportDatasourceDTO(*datasource)
	return &result, nil
}

func (service *ReportDatasourceService) Create(ctx context.Context, actor uint, request requestbody.ReportDatasourceSaveRequest) (*ReportDatasourceDTO, error) {
	datasource, err := reportDatasourceFromRequest(request, true)
	if service == nil || ctx == nil || actor == 0 || err != nil {
		return nil, ErrReportDatasourceInvalid
	}
	version, ciphertext, err := service.cipher.Encrypt(request.Password)
	if err != nil {
		if errors.Is(err, reportsecret.ErrInvalidCredential) {
			return nil, fmt.Errorf("%w: %v", ErrReportDatasourceCredentialUnavailable, err)
		}
		return nil, fmt.Errorf("report datasource service: encrypt credential: %w", err)
	}
	datasource.CredentialKeyVersion = version
	datasource.PasswordCiphertext = ciphertext
	if err := service.store.CreateReportDatasource(ctx, actor, datasource); err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	result := reportDatasourceDTO(*datasource)
	return &result, nil
}

func (service *ReportDatasourceService) Update(ctx context.Context, actor, datasourceID uint, request requestbody.ReportDatasourceSaveRequest) (*ReportDatasourceDTO, error) {
	datasource, err := reportDatasourceFromRequest(request, false)
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 || err != nil {
		return nil, ErrReportDatasourceInvalid
	}
	datasource.ID = datasourceID
	if request.Password != "" {
		version, ciphertext, encryptErr := service.cipher.Encrypt(request.Password)
		if encryptErr != nil {
			if errors.Is(encryptErr, reportsecret.ErrInvalidCredential) {
				return nil, fmt.Errorf("%w: %v", ErrReportDatasourceCredentialUnavailable, encryptErr)
			}
			return nil, fmt.Errorf("report datasource service: encrypt credential: %w", encryptErr)
		}
		datasource.CredentialKeyVersion = version
		datasource.PasswordCiphertext = ciphertext
	}
	if err := service.store.UpdateReportDatasource(ctx, actor, datasource); err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	persisted, err := service.store.GetReportDatasource(ctx, datasourceID)
	if err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	result := reportDatasourceDTO(*persisted)
	return &result, nil
}

func (service *ReportDatasourceService) Test(ctx context.Context, actor, datasourceID uint) (*ReportDatasourceTestDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	datasource, err := service.store.GetReportDatasource(ctx, datasourceID)
	if err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	password, err := service.cipher.Decrypt(datasource.CredentialKeyVersion, datasource.PasswordCiphertext)
	if err != nil {
		return nil, fmt.Errorf("report datasource service: decrypt credential: %w", err)
	}
	result := service.testConnection(ctx, *datasource, password)
	safeError := ""
	if result.Status == reportDatasourceTestFailed {
		safeError = result.ErrorCode + ": " + result.Message
	}
	if err := service.store.RecordReportDatasourceTest(ctx, actor, datasourceID, result.Status, safeError, result.TestedAt); err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	return result, nil
}

// TestConnection performs a real Oracle PingContext against an unsaved draft.
// When editing, an omitted password reuses the encrypted credential from the
// referenced datasource while every other field comes from the draft.
func (service *ReportDatasourceService) TestConnection(ctx context.Context, actor uint, request requestbody.ReportDatasourceConnectionTestRequest) (*ReportDatasourceTestDTO, error) {
	if service == nil || ctx == nil || actor == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	datasource, err := reportDatasourceFromConnectionTest(request)
	if err != nil {
		return nil, ErrReportDatasourceInvalid
	}
	password := request.Password
	if password == "" {
		if request.DatasourceID == 0 {
			return nil, ErrReportDatasourceInvalid
		}
		persisted, loadErr := service.store.GetReportDatasource(ctx, request.DatasourceID)
		if loadErr != nil {
			return nil, classifyReportDatasourceStoreError(loadErr)
		}
		password, err = service.cipher.Decrypt(persisted.CredentialKeyVersion, persisted.PasswordCiphertext)
		if err != nil {
			return nil, fmt.Errorf("report datasource service: decrypt credential for connection test: %w", err)
		}
	}
	return service.testConnection(ctx, *datasource, password), nil
}

func (service *ReportDatasourceService) ListProcedures(ctx context.Context, actor, datasourceID uint, query ReportProcedureCatalogQuery) (*ReportProcedurePageDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	query.Owner = strings.TrimSpace(query.Owner)
	query.Search = strings.TrimSpace(query.Search)
	if utf8.RuneCountInString(query.Search) > 128 || query.Limit < 0 || query.Limit > 100 {
		return nil, ErrReportDatasourceInvalid
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	afterKey := ""
	if query.After != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(query.After)
		if err != nil || len(decoded) > 520 {
			return nil, ErrReportDatasourceInvalid
		}
		afterKey = string(decoded)
		if _, err := reportoracle.ParseProcedureCursorKey(afterKey); err != nil {
			return nil, ErrReportDatasourceInvalid
		}
	}

	connection, queryCtx, cancel, err := service.openMetadataConnection(ctx, datasourceID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer connection.Close()
	procedures, err := connection.ListProcedures(queryCtx, reportoracle.ProcedureCatalogQuery{
		Owner: query.Owner, Search: query.Search, AfterKey: afterKey, Limit: query.Limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list procedures: %v", ErrReportDatasourceOracleUnavailable, err)
	}
	hasMore := len(procedures) > query.Limit
	if hasMore {
		procedures = procedures[:query.Limit]
	}
	items := make([]ReportProcedureSummaryDTO, 0, len(procedures))
	for _, procedure := range procedures {
		items = append(items, procedureSummaryDTO(procedure.ProcedureRef, procedure.ArgumentCount))
	}
	nextAfter := ""
	if hasMore && len(procedures) > 0 {
		nextAfter = base64.RawURLEncoding.EncodeToString([]byte(procedures[len(procedures)-1].CursorKey))
	}
	return &ReportProcedurePageDTO{Items: items, HasMore: hasMore, NextAfter: nextAfter}, nil
}

func (service *ReportDatasourceService) GetProcedureSignature(ctx context.Context, actor, datasourceID uint, ref reportoracle.ProcedureRef) (*ReportProcedureSignatureDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	normalized, err := reportoracle.NormalizeProcedureRef(ref)
	if err != nil {
		return nil, ErrReportDatasourceInvalid
	}
	connection, queryCtx, cancel, err := service.openMetadataConnection(ctx, datasourceID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer connection.Close()
	arguments, err := connection.InspectProcedure(queryCtx, normalized)
	if err != nil {
		if errors.Is(err, reportoracle.ErrMetadataMismatch) {
			return nil, ErrReportDatasourceNotFound
		}
		return nil, fmt.Errorf("%w: inspect procedure: %v", ErrReportDatasourceOracleUnavailable, err)
	}

	usedCodes := make(map[string]int, len(arguments))
	items := make([]ReportProcedureArgumentDTO, 0, len(arguments))
	inputArgName := ""
	blockingReasons := make([]string, 0, 3)
	for _, argument := range arguments {
		item := procedureArgumentDTO(argument, usedCodes)
		items = append(items, item)
		switch item.Role {
		case "JSON_INPUT":
			if inputArgName == "" {
				inputArgName = item.Name
			} else {
				blockingReasons = append(blockingReasons, "存储过程只能有一个 JSON 输入参数")
			}
		case "ERROR_OUTPUT":
			// R_ERROR is checked by the generated PL/SQL block before commit.
		default:
			blockingReasons = append(blockingReasons, fmt.Sprintf("参数 %s 不是可支持的 JSON 输入或普通输出参数", item.Name))
		}
	}
	if inputArgName == "" {
		blockingReasons = append(blockingReasons, "缺少 IN CLOB/字符 JSON 输入参数")
	}
	callTemplate := ""
	if len(blockingReasons) == 0 {
		plan, planErr := reportoracle.BuildJSONTableCallPlan(normalized, arguments, inputArgName)
		if planErr != nil {
			blockingReasons = append(blockingReasons, "存储过程参数无法生成安全调用")
		} else {
			callTemplate = plan.Statement()
		}
	}
	protocolReady := len(blockingReasons) == 0
	return &ReportProcedureSignatureDTO{
		Procedure: procedureSummaryDTO(normalized, len(arguments)), Arguments: items,
		AllSupported: protocolReady, ProtocolReady: protocolReady, InputArgName: inputArgName,
		OutputArgName: "", CallTemplate: callTemplate, BlockingReasons: blockingReasons,
	}, nil
}

func (service *ReportDatasourceService) ListResultTables(ctx context.Context, actor, datasourceID uint, query ReportResultTableCatalogQuery) (*ReportResultTablePageDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	query.Owner = strings.TrimSpace(query.Owner)
	query.Search = strings.TrimSpace(query.Search)
	if utf8.RuneCountInString(query.Search) > 128 || query.Limit < 0 || query.Limit > 100 {
		return nil, ErrReportDatasourceInvalid
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	afterKey := ""
	if query.After != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(query.After)
		if err != nil || len(decoded) > 260 {
			return nil, ErrReportDatasourceInvalid
		}
		afterKey = string(decoded)
		if _, err := reportoracle.ParseResultTableCursorKey(afterKey); err != nil {
			return nil, ErrReportDatasourceInvalid
		}
	}

	connection, queryCtx, cancel, err := service.openMetadataConnection(ctx, datasourceID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer connection.Close()
	tables, err := connection.ListResultTables(queryCtx, reportoracle.ResultTableCatalogQuery{
		Owner: query.Owner, Search: query.Search, AfterKey: afterKey, Limit: query.Limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list result tables: %v", ErrReportDatasourceOracleUnavailable, err)
	}
	hasMore := len(tables) > query.Limit
	if hasMore {
		tables = tables[:query.Limit]
	}
	refs := make([]reportoracle.ResultTableRef, 0, len(tables))
	for _, table := range tables {
		refs = append(refs, table.ResultTableRef)
	}
	managed, err := service.store.ManagedResultTableRefs(ctx, datasourceID, refs)
	if err != nil {
		return nil, err
	}
	items := make([]ReportResultTableSummaryDTO, 0, len(tables))
	for _, table := range tables {
		if managed[table.ResultTableRef] {
			continue
		}
		items = append(items, resultTableSummaryDTO(table.ResultTableRef, table.ColumnCount))
	}
	nextAfter := ""
	if hasMore && len(tables) > 0 {
		nextAfter = base64.RawURLEncoding.EncodeToString([]byte(tables[len(tables)-1].CursorKey))
	}
	return &ReportResultTablePageDTO{Items: items, HasMore: hasMore, NextAfter: nextAfter}, nil
}

func (service *ReportDatasourceService) GetResultTableSchema(ctx context.Context, actor, datasourceID uint, ref reportoracle.ResultTableRef) (*ReportResultTableSchemaDTO, error) {
	if service == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return nil, ErrReportDatasourceInvalid
	}
	normalized, err := reportoracle.NormalizeResultTableRef(ref)
	if err != nil {
		return nil, ErrReportDatasourceInvalid
	}
	connection, queryCtx, cancel, err := service.openMetadataConnection(ctx, datasourceID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer connection.Close()
	managed, err := service.store.ManagedResultTableRefs(ctx, datasourceID, []reportoracle.ResultTableRef{normalized})
	if err != nil {
		return nil, err
	}
	if managed[normalized] {
		return nil, fmt.Errorf("%w: 请通过伯俊目录选择受管表", ErrReportDatasourceInvalid)
	}
	columns, err := connection.InspectResultTable(queryCtx, normalized)
	if err != nil {
		if errors.Is(err, reportoracle.ErrMetadataMismatch) {
			return nil, ErrReportDatasourceNotFound
		}
		return nil, fmt.Errorf("%w: inspect result table: %v", ErrReportDatasourceOracleUnavailable, err)
	}
	items := make([]ReportResultTableColumnDTO, 0, len(columns))
	for _, column := range columns {
		items = append(items, ReportResultTableColumnDTO{
			Name: column.Name, Description: column.Comment, Position: column.Position, OracleType: column.DataType,
			DataLength: column.DataLength, Precision: column.DataPrecision, Scale: column.DataScale,
			Nullable: column.Nullable,
		})
	}
	return &ReportResultTableSchemaDTO{
		Table: resultTableSummaryDTO(normalized, len(items)), Columns: items,
	}, nil
}

func (service *ReportDatasourceService) openMetadataConnection(ctx context.Context, datasourceID uint) (reportDatasourceConnection, context.Context, context.CancelFunc, error) {
	datasource, err := service.store.GetReportDatasource(ctx, datasourceID)
	if err != nil {
		return nil, nil, nil, classifyReportDatasourceStoreError(err)
	}
	if !datasource.Enabled {
		return nil, nil, nil, ErrReportDatasourceOracleUnavailable
	}
	password, err := service.cipher.Decrypt(datasource.CredentialKeyVersion, datasource.PasswordCiphertext)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: decrypt credential", ErrReportDatasourceCredentialUnavailable)
	}
	connection, err := service.open(ctx, oracleConfigFromDatasource(*datasource, password))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: connect", ErrReportDatasourceOracleUnavailable)
	}
	timeout := time.Duration(datasource.QueryTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	return connection, queryCtx, cancel, nil
}

func procedureSummaryDTO(ref reportoracle.ProcedureRef, argumentCount int) ReportProcedureSummaryDTO {
	return ReportProcedureSummaryDTO{
		Owner: ref.Owner, Package: ref.Package, Name: ref.Name, Overload: ref.Overload,
		ArgumentCount: argumentCount, QualifiedName: displayProcedureName(ref),
	}
}

func resultTableSummaryDTO(ref reportoracle.ResultTableRef, columnCount int) ReportResultTableSummaryDTO {
	return ReportResultTableSummaryDTO{
		Owner: ref.Owner, Name: ref.Name, QualifiedName: ref.Owner + "." + ref.Name, ColumnCount: columnCount,
	}
}

func qualifiedProcedureName(ref reportoracle.ProcedureRef) string {
	parts := []string{ref.Owner}
	if ref.Package != "" {
		parts = append(parts, ref.Package)
	}
	parts = append(parts, ref.Name)
	return strings.Join(parts, ".")
}

func displayProcedureName(ref reportoracle.ProcedureRef) string {
	qualified := qualifiedProcedureName(ref)
	if ref.Overload != "" {
		qualified += " #" + ref.Overload
	}
	return qualified
}

func procedureArgumentDTO(argument reportoracle.ProcedureArgument, usedCodes map[string]int) ReportProcedureArgumentDTO {
	oracleType := strings.ToUpper(strings.Join(strings.Fields(argument.DataType), " "))
	logicalType, controlType, role, supported, reason := recommendedProcedureArgumentType(argument.Name, argument.Direction, oracleType, argument.TypeOwner, argument.TypeName, argument.DataScale)
	code := suggestedProcedureParameterCode(argument.Name)
	usedCodes[code]++
	if usedCodes[code] > 1 {
		code = fmt.Sprintf("%s%d", code, argument.Position)
	}
	systemValue := ""
	if code == "runId" && logicalType == "string" && supported {
		systemValue = "RUN_ID"
	}
	return ReportProcedureArgumentDTO{
		Name: argument.Name, Position: argument.Position, Sequence: argument.Sequence,
		Direction: strings.ToUpper(strings.TrimSpace(argument.Direction)), OracleType: oracleType,
		DataLength: argument.DataLength, Precision: argument.DataPrecision, Scale: argument.DataScale,
		TypeOwner: argument.TypeOwner, TypeName: argument.TypeName, Defaulted: argument.Defaulted,
		Supported: supported, UnsupportedReason: reason, SuggestedCode: code,
		SuggestedLogicalType: logicalType, SuggestedControlType: controlType, SuggestedSystemValue: systemValue,
		Role: role,
	}
}

func recommendedProcedureArgumentType(name, direction, oracleType, typeOwner, typeName string, scale *int64) (string, string, string, bool, string) {
	direction = strings.ToUpper(strings.TrimSpace(direction))
	if direction == "OUT" {
		argument := reportoracle.ProcedureArgument{Name: name, Direction: direction, DataType: oracleType, TypeOwner: typeOwner, TypeName: typeName, DataScale: scale}
		if reportoracle.SupportsJSONTableErrorOutput(argument) {
			return "", "", "ERROR_OUTPUT", true, ""
		}
		return "", "", "UNSUPPORTED", false, "暂不支持该 Oracle OUT 参数类型"
	}
	if direction != "IN" {
		return "", "", "UNSUPPORTED", false, "暂不支持 IN OUT 参数"
	}
	if strings.TrimSpace(typeOwner) != "" || strings.TrimSpace(typeName) != "" {
		return "", "", "UNSUPPORTED", false, "暂不支持 Oracle 对象、集合或复合类型"
	}
	switch {
	case oracleType == "VARCHAR2" || oracleType == "NVARCHAR2" || oracleType == "CHAR" || oracleType == "NCHAR" || oracleType == "CLOB" || oracleType == "NCLOB" || oracleType == "JSON":
		return "json", "TEXTAREA", "JSON_INPUT", true, ""
	default:
		return "", "", "UNSUPPORTED", false, "JSON 输入参数必须是 CLOB、字符或 Oracle JSON 类型"
	}
}

func suggestedProcedureParameterCode(name string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(name)), func(r rune) bool { return r == '_' })
	if len(parts) > 1 && parts[0] == "p" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "param"
	}
	result := parts[0]
	for _, part := range parts[1:] {
		if part != "" {
			result += strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return result
}

func (service *ReportDatasourceService) testConnection(ctx context.Context, datasource model.ReportDatasource, password string) *ReportDatasourceTestDTO {
	startedAt := service.now()
	connection, openErr := service.open(ctx, oracleConfigFromDatasource(datasource, password))
	testedAt := service.now()
	result := &ReportDatasourceTestDTO{Status: reportDatasourceTestSuccess, TestedAt: testedAt, LatencyMS: maxInt64(0, testedAt.Sub(startedAt).Milliseconds()), Message: "Oracle 连接测试成功"}
	if openErr != nil {
		result.Status = reportDatasourceTestFailed
		result.ErrorCode, result.Message = safeDatasourceConnectionFailure(openErr)
	}
	if connection != nil {
		_ = connection.Close()
	}
	return result
}

func reportDatasourceFromConnectionTest(request requestbody.ReportDatasourceConnectionTestRequest) (*model.ReportDatasource, error) {
	host := strings.TrimSpace(request.Host)
	serviceName := strings.TrimSpace(request.ServiceName)
	sid := strings.TrimSpace(request.SID)
	username := strings.TrimSpace(request.Username)
	timezone := strings.TrimSpace(request.SessionTimezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	if host == "" || utf8.RuneCountInString(host) > 255 || username == "" || utf8.RuneCountInString(username) > 128 ||
		request.Port < 1 || request.Port > 65535 || (serviceName == "") == (sid == "") || len(request.Password) > 1024 ||
		request.ConnectTimeoutSeconds < 1 || request.ConnectTimeoutSeconds > 60 || request.QueryTimeoutSeconds < 1 || request.QueryTimeoutSeconds > 86400 ||
		request.MaxOpenConnections < 1 || request.MaxOpenConnections > 100 || request.MaxIdleConnections < 0 || request.MaxIdleConnections > request.MaxOpenConnections ||
		request.PrefetchRows < 1 || request.PrefetchRows > 10000 || request.ArraySize < 1 || request.ArraySize > 10000 {
		return nil, ErrReportDatasourceInvalid
	}
	return &model.ReportDatasource{Driver: model.ReportDatasourceDriverOracle, Host: host, Port: request.Port, ServiceName: serviceName, SID: sid, Username: username, SessionTimezone: timezone, ConnectTimeoutSeconds: request.ConnectTimeoutSeconds, QueryTimeoutSeconds: request.QueryTimeoutSeconds, MaxOpenConnections: request.MaxOpenConnections, MaxIdleConnections: request.MaxIdleConnections, PrefetchRows: request.PrefetchRows, ArraySize: request.ArraySize, Enabled: true}, nil
}

func reportDatasourceFromRequest(request requestbody.ReportDatasourceSaveRequest, passwordRequired bool) (*model.ReportDatasource, error) {
	request.Code = strings.ToLower(strings.TrimSpace(request.Code))
	request.Name = strings.TrimSpace(request.Name)
	request.Host = strings.TrimSpace(request.Host)
	request.ServiceName = strings.TrimSpace(request.ServiceName)
	request.SID = strings.TrimSpace(request.SID)
	request.Username = strings.TrimSpace(request.Username)
	request.SessionTimezone = strings.TrimSpace(request.SessionTimezone)
	if request.SessionTimezone == "" {
		request.SessionTimezone = "Asia/Shanghai"
	}
	if !reportDatasourceCodePattern.MatchString(request.Code) || request.Name == "" || utf8.RuneCountInString(request.Name) > 128 || request.Host == "" || utf8.RuneCountInString(request.Host) > 255 || request.Username == "" || utf8.RuneCountInString(request.Username) > 128 || request.Port < 1 || request.Port > 65535 || (request.ServiceName == "") == (request.SID == "") || (passwordRequired && request.Password == "") || len(request.Password) > 1024 || request.ConnectTimeoutSeconds < 1 || request.ConnectTimeoutSeconds > 60 || request.QueryTimeoutSeconds < 1 || request.QueryTimeoutSeconds > 86400 || request.MaxOpenConnections < 1 || request.MaxOpenConnections > 100 || request.MaxIdleConnections < 0 || request.MaxIdleConnections > request.MaxOpenConnections || request.PrefetchRows < 1 || request.PrefetchRows > 10000 || request.ArraySize < 1 || request.ArraySize > 10000 {
		return nil, ErrReportDatasourceInvalid
	}
	return &model.ReportDatasource{Code: request.Code, Name: request.Name, Driver: model.ReportDatasourceDriverOracle, Host: request.Host, Port: request.Port, ServiceName: request.ServiceName, SID: request.SID, Username: request.Username, SessionTimezone: request.SessionTimezone, ConnectTimeoutSeconds: request.ConnectTimeoutSeconds, QueryTimeoutSeconds: request.QueryTimeoutSeconds, MaxOpenConnections: request.MaxOpenConnections, MaxIdleConnections: request.MaxIdleConnections, PrefetchRows: request.PrefetchRows, ArraySize: request.ArraySize, Enabled: request.Enabled}, nil
}

func reportDatasourceDTO(datasource model.ReportDatasource) ReportDatasourceDTO {
	return ReportDatasourceDTO{ID: datasource.ID, Code: datasource.Code, Name: datasource.Name, Driver: datasource.Driver, Host: datasource.Host, Port: datasource.Port, ServiceName: datasource.ServiceName, SID: datasource.SID, Username: datasource.Username, HasPassword: datasource.PasswordCiphertext != "", SessionTimezone: datasource.SessionTimezone, ConnectTimeoutSeconds: datasource.ConnectTimeoutSeconds, QueryTimeoutSeconds: datasource.QueryTimeoutSeconds, MaxOpenConnections: datasource.MaxOpenConnections, MaxIdleConnections: datasource.MaxIdleConnections, PrefetchRows: datasource.PrefetchRows, ArraySize: datasource.ArraySize, Enabled: datasource.Enabled, LastTestStatus: datasource.LastTestStatus, LastTestError: datasource.LastTestErrorSafe, LastTestedAt: datasource.LastTestedAt}
}

func safeDatasourceConnectionFailure(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "CONNECT_TIMEOUT", "Oracle 连接超时"
	}
	if errors.Is(err, reportoracle.ErrInvalidConfiguration) {
		return "INVALID_CONFIGURATION", "Oracle 连接配置无效"
	}
	upper := strings.ToUpper(err.Error())
	switch {
	case strings.Contains(upper, "DPI-1047"), strings.Contains(upper, "DPI-1072"):
		return "ORACLE_CLIENT_UNAVAILABLE", "服务端 Oracle 客户端不可用，请联系管理员"
	case strings.Contains(upper, "ORA-01017"):
		return "AUTHENTICATION_FAILED", "Oracle 用户名或密码无效"
	case strings.Contains(upper, "ORA-28000"):
		return "ACCOUNT_LOCKED", "Oracle 账号已锁定"
	case strings.Contains(upper, "ORA-28001"):
		return "PASSWORD_EXPIRED", "Oracle 密码已过期"
	case strings.Contains(upper, "ORA-12154"), strings.Contains(upper, "ORA-12505"), strings.Contains(upper, "ORA-12514"):
		return "SERVICE_NOT_FOUND", "Oracle 服务名或 SID 不可用"
	case strings.Contains(upper, "ORA-12170"), strings.Contains(upper, "ORA-12535"):
		return "CONNECT_TIMEOUT", "Oracle 连接超时"
	case strings.Contains(upper, "ORA-12541"), strings.Contains(upper, "NO SUCH HOST"), strings.Contains(upper, "CONNECTION REFUSED"), strings.Contains(upper, "NO ROUTE TO HOST"), strings.Contains(upper, "NETWORK IS UNREACHABLE"):
		return "NETWORK_UNREACHABLE", "无法连接 Oracle 网络地址"
	default:
		return "UNKNOWN", "Oracle 连接测试失败"
	}
}

func classifyReportDatasourceStoreError(err error) error {
	var mysqlError *mysqlDriver.MySQLError
	switch {
	case errors.Is(err, reportrepo.ErrDatasourceNotFound):
		return ErrReportDatasourceNotFound
	case errors.Is(err, reportrepo.ErrDatasourceInUse):
		return ErrReportDatasourceConflict
	case errors.As(err, &mysqlError) && mysqlError.Number == 1062:
		return ErrReportDatasourceConflict
	default:
		return fmt.Errorf("report datasource service: store: %w", err)
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
