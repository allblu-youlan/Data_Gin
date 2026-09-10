package data_svc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reporting"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

const (
	defaultReportDraftPageSize = 50
	maxReportDraftPageSize     = 100
	maxReportParameters        = 128
	maxReportColumns           = 512
	maxReportGrants            = 256
)

var (
	ErrReportInvalid        = errors.New("report draft service: invalid input")
	ErrReportNotFound       = errors.New("report draft service: not found")
	ErrReportConflict       = errors.New("report draft service: conflict")
	ErrReportDeleteConflict = errors.New("report draft service: report cannot be deleted")

	reportCodePattern        = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)
	reportLogicalCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	reportControlTypes       = map[string]struct{}{"TEXT": {}, "TEXTAREA": {}, "NUMBER": {}, "CHECKBOX": {}, "DATE": {}, "DATETIME": {}, "SELECT": {}, "MULTI_SELECT": {}}
	reportValueTypes         = map[string]struct{}{"string": {}, "integer": {}, "decimal": {}, "boolean": {}, "date": {}, "datetime": {}, "enum": {}, "multi_enum": {}, "json": {}}
	reportGrantActions       = map[string]struct{}{"QUERY": {}, "EXPORT": {}}
)

type reportDraftStore interface {
	CreateDraft(context.Context, uint, *reportrepo.Draft) error
	FindDraftByID(context.Context, uint, uint) (*reportrepo.Draft, error)
	ListDrafts(context.Context, uint, reportrepo.DraftListQuery) (reportrepo.DraftPage, error)
	UpdateDraft(context.Context, uint, uint, uint64, *reportrepo.Draft) error
	DeleteDraft(context.Context, uint, uint, uint64) error
}

type ReportDraftService struct {
	store reportDraftStore
}

func NewReportDraftService() *ReportDraftService {
	return NewReportDraftServiceWithStore(reportrepo.New())
}

func NewReportDraftServiceWithStore(store reportDraftStore) *ReportDraftService {
	if store == nil {
		panic("report draft service: nil store")
	}
	return &ReportDraftService{store: store}
}

type ReportDraftDTO struct {
	ID            uint                 `json:"id"`
	Code          string               `json:"code"`
	Name          string               `json:"name"`
	Category      string               `json:"category"`
	Description   string               `json:"description"`
	DatasourceID  uint                 `json:"datasourceId"`
	Status        string               `json:"status"`
	LockVersion   uint64               `json:"lockVersion"`
	Procedure     ReportProcedureDTO   `json:"procedure"`
	ExecutionMode string               `json:"executionMode"`
	InputSchema   json.RawMessage      `json:"inputSchema"`
	Result        ReportResultDTO      `json:"result"`
	CallTemplate  string               `json:"callTemplate"`
	Parameters    []ReportParameterDTO `json:"parameters"`
	Columns       []ReportColumnDTO    `json:"columns"`
	Grants        []ReportGrantDTO     `json:"grants"`
	CreatedAt     time.Time            `json:"createdAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
}

type ReportDraftSummaryDTO struct {
	ID                        uint      `json:"id"`
	Code                      string    `json:"code"`
	Name                      string    `json:"name"`
	Category                  string    `json:"category"`
	Description               string    `json:"description"`
	DatasourceID              uint      `json:"datasourceId"`
	Status                    string    `json:"status"`
	LockVersion               uint64    `json:"lockVersion"`
	CurrentDraftVersionID     uint      `json:"currentDraftVersionId,omitempty"`
	CurrentPublishedVersionID uint      `json:"currentPublishedVersionId,omitempty"`
	IsOwner                   bool      `json:"isOwner"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

type ReportProcedureDTO struct {
	Owner               string `json:"owner"`
	Package             string `json:"package,omitempty"`
	Name                string `json:"name"`
	Overload            string `json:"overload,omitempty"`
	JSONInputArgName    string `json:"jsonInputArgName,omitempty"`
	ResultCursorArgName string `json:"resultCursorArgName,omitempty"`
}

type ReportResultDTO struct {
	TableOwner string `json:"tableOwner"`
	TableName  string `json:"tableName"`
}

type ReportParameterDTO struct {
	Code               string          `json:"code"`
	Label              string          `json:"label"`
	DisplayOrder       int             `json:"displayOrder"`
	ControlType        string          `json:"controlType"`
	LogicalType        string          `json:"logicalType"`
	Cardinality        string          `json:"cardinality"`
	ProcedureArgName   string          `json:"procedureArgName"`
	Position           int             `json:"position"`
	OracleType         string          `json:"oracleType"`
	Precision          *int            `json:"precision,omitempty"`
	Scale              *int            `json:"scale,omitempty"`
	MaxLength          *int            `json:"maxLength,omitempty"`
	Required           bool            `json:"required"`
	Nullable           bool            `json:"nullable"`
	SystemInjected     bool            `json:"systemInjected"`
	Sensitive          bool            `json:"sensitive"`
	DefaultValue       json.RawMessage `json:"defaultValue,omitempty"`
	AllowedValues      json.RawMessage `json:"allowedValues,omitempty"`
	Validation         json.RawMessage `json:"validation,omitempty"`
	Normalizer         json.RawMessage `json:"normalizer,omitempty"`
	ValueSource        json.RawMessage `json:"valueSource,omitempty"`
	Timezone           string          `json:"timezone,omitempty"`
	NullPolicy         string          `json:"nullPolicy"`
	CollectionEncoding string          `json:"collectionEncoding,omitempty"`
	ErrorMessage       string          `json:"errorMessage,omitempty"`
}

type ReportColumnDTO struct {
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
	NullDisplay       string          `json:"nullDisplay,omitempty"`
}

type ReportGrantDTO struct {
	SubjectType string          `json:"subjectType"`
	SubjectID   uint            `json:"subjectId"`
	Actions     json.RawMessage `json:"actions"`
}

type ReportDraftListDTO struct {
	Items       []ReportDraftSummaryDTO `json:"items"`
	HasMore     bool                    `json:"hasMore"`
	NextAfterID uint                    `json:"nextAfterId,omitempty"`
}

type ReportDraftDeleteDTO struct {
	ID uint `json:"id"`
}

func (service *ReportDraftService) Create(ctx context.Context, actor uint, request requestbody.ReportDraftSaveRequest) (*ReportDraftDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 {
		return nil, invalidReport("service, context and actor are required")
	}
	if request.ExpectedLockVersion != nil {
		return nil, invalidReport("expectedLockVersion is only valid on update")
	}
	draft, err := reportDraftFromRequest(actor, request)
	if err != nil {
		return nil, err
	}
	if err := service.store.CreateDraft(ctx, actor, draft); err != nil {
		return nil, classifyReportStoreError(err)
	}
	persisted, err := service.store.FindDraftByID(ctx, actor, draft.Definition.ID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	return reportDraftDTO(persisted), nil
}

func (service *ReportDraftService) Get(ctx context.Context, actor, definitionID uint) (*ReportDraftDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 {
		return nil, invalidReport("service, context, actor and report id are required")
	}
	draft, err := service.store.FindDraftByID(ctx, actor, definitionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	return reportDraftDTO(draft), nil
}

func (service *ReportDraftService) List(ctx context.Context, actor, afterID uint, limit int, category, search string) (*ReportDraftListDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 {
		return nil, invalidReport("service, context and actor are required")
	}
	if limit == 0 {
		limit = defaultReportDraftPageSize
	}
	category = strings.TrimSpace(category)
	search = strings.TrimSpace(search)
	if limit < 1 || limit > maxReportDraftPageSize || utf8.RuneCountInString(category) > 64 || utf8.RuneCountInString(search) > 128 {
		return nil, invalidReport("invalid list filters")
	}
	page, err := service.store.ListDrafts(ctx, actor, reportrepo.DraftListQuery{
		AfterID: afterID, Limit: limit, Category: category, Search: search,
	})
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	result := &ReportDraftListDTO{Items: make([]ReportDraftSummaryDTO, 0, len(page.Items)), HasMore: page.HasMore, NextAfterID: page.NextAfterID}
	for _, item := range page.Items {
		result.Items = append(result.Items, reportDraftSummaryDTO(item))
	}
	return result, nil
}

func (service *ReportDraftService) Update(ctx context.Context, actor, definitionID uint, request requestbody.ReportDraftSaveRequest) (*ReportDraftDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 {
		return nil, invalidReport("service, context, actor and report id are required")
	}
	if request.ExpectedLockVersion == nil || *request.ExpectedLockVersion == 0 {
		return nil, invalidReport("expectedLockVersion is required")
	}
	// Preserve 404 for a missing scoped report. A deletion or concurrent change
	// after this read is still correctly reported as a 409 by UpdateDraft.
	existing, err := service.store.FindDraftByID(ctx, actor, definitionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	if existing.Definition.Status == model.ReportDefinitionStatusActive && strings.TrimSpace(existing.Definition.Category) != "" && strings.TrimSpace(request.Category) != existing.Definition.Category {
		return nil, invalidReport("published report category cannot be changed")
	}
	draft, err := reportDraftFromRequest(actor, request)
	if err != nil {
		return nil, err
	}
	draft.Definition.ID = definitionID
	if err := service.store.UpdateDraft(ctx, actor, definitionID, *request.ExpectedLockVersion, draft); err != nil {
		return nil, classifyReportStoreError(err)
	}
	persisted, err := service.store.FindDraftByID(ctx, actor, definitionID)
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	return reportDraftDTO(persisted), nil
}

func (service *ReportDraftService) Delete(ctx context.Context, actor, definitionID uint, expectedLockVersion uint64) (*ReportDraftDeleteDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 || definitionID == 0 || expectedLockVersion == 0 {
		return nil, invalidReport("service, context, actor, report id and lock version are required")
	}
	if err := service.store.DeleteDraft(ctx, actor, definitionID, expectedLockVersion); err != nil {
		return nil, classifyReportStoreError(err)
	}
	return &ReportDraftDeleteDTO{ID: definitionID}, nil
}

func reportDraftFromRequest(actor uint, request requestbody.ReportDraftSaveRequest) (*reportrepo.Draft, error) {
	request.Code = strings.ToLower(strings.TrimSpace(request.Code))
	request.Name = strings.TrimSpace(request.Name)
	request.Category = strings.TrimSpace(request.Category)
	request.Description = strings.TrimSpace(request.Description)
	if !reportCodePattern.MatchString(request.Code) || request.Name == "" || utf8.RuneCountInString(request.Name) > 128 || request.Category == "" ||
		utf8.RuneCountInString(request.Category) > 64 || utf8.RuneCountInString(request.Description) > 500 || request.DatasourceID == 0 {
		return nil, invalidReport("invalid report identity")
	}
	procedure, err := reportoracle.NormalizeProcedureRef(reportoracle.ProcedureRef{
		Owner: request.Procedure.Owner, Package: request.Procedure.Package,
		Name: request.Procedure.Name, Overload: request.Procedure.Overload,
	})
	if err != nil {
		return nil, invalidReport("invalid Oracle procedure")
	}
	mode := strings.ToUpper(strings.TrimSpace(request.ExecutionMode))
	if mode == "" {
		mode = model.ReportExecutionModeTableSnapshot
	}
	resultRef := reportoracle.ResultTableRef{}
	runIDColumn, rowIDColumn := "", ""
	jsonInputArgName, resultCursorArgName := "", ""
	inputSchema := json.RawMessage(nil)
	if mode == model.ReportExecutionModeRefCursor {
		jsonInputArgName, err = normalizeReportIdentifier(request.Procedure.JSONInputArgName)
		if err != nil {
			return nil, invalidReport("invalid JSON input argument")
		}
		resultCursorArgName, err = normalizeReportIdentifier(request.Procedure.ResultCursorArgName)
		if err != nil || jsonInputArgName == resultCursorArgName {
			return nil, invalidReport("invalid result cursor argument")
		}
		inputSchema, err = canonicalReportInputSchema(request.InputSchema)
		if err != nil {
			return nil, err
		}
	} else if mode == model.ReportExecutionModeTableSnapshot {
		resultRef, err = reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: request.Result.TableOwner, Name: request.Result.TableName})
		if err != nil {
			return nil, invalidReport("invalid Oracle result table")
		}
		if strings.TrimSpace(request.Procedure.JSONInputArgName) != "" {
			jsonInputArgName, err = normalizeReportIdentifier(request.Procedure.JSONInputArgName)
			if err != nil || strings.TrimSpace(request.Procedure.ResultCursorArgName) != "" {
				return nil, invalidReport("invalid JSON result-table arguments")
			}
			inputSchema, err = canonicalReportInputSchema(request.InputSchema)
			if err != nil {
				return nil, err
			}
			if len(request.Parameters) != 0 {
				return nil, invalidReport("JSON result-table reports must not configure procedure parameters")
			}
		}
	} else {
		return nil, invalidReport("invalid report execution mode")
	}
	if len(procedure.Owner) > 128 || len(procedure.Package) > 128 || len(procedure.Name) > 128 || len(procedure.Overload) > 32 ||
		len(resultRef.Owner) > 128 || len(resultRef.Name) > 128 {
		return nil, invalidReport("Oracle object name is too long")
	}
	parameters := []model.ReportParameter{}
	definitions := []reporting.ParameterDefinition{}
	if mode == model.ReportExecutionModeTableSnapshot && jsonInputArgName == "" {
		parameters, definitions, err = reportParametersFromRequest(request.Parameters)
		if err != nil {
			return nil, err
		}
	}
	callTemplate := strings.TrimSpace(request.CallTemplate)
	if len(callTemplate) > 64*1024 {
		return nil, invalidReport("call template is too large")
	}
	if mode == model.ReportExecutionModeTableSnapshot && jsonInputArgName == "" {
		if _, err := reporting.CompileCallTemplate(callTemplate, definitions); err != nil {
			return nil, invalidReport("call template and parameters do not match")
		}
	} else if mode == model.ReportExecutionModeRefCursor {
		target := procedure.Owner + "."
		if procedure.Package != "" {
			target += procedure.Package + "."
		}
		target += procedure.Name
		callTemplate = fmt.Sprintf("BEGIN %s(%s => :payload, %s => :resultCursor); END;", target, jsonInputArgName, resultCursorArgName)
	} else if callTemplate == "" {
		return nil, invalidReport("JSON result-table call template is required")
	}
	columns := []model.ReportColumn{}
	if len(request.Columns) > 0 {
		columns, err = reportColumnsFromRequest(request.Columns, mode)
		if err != nil {
			return nil, err
		}
	} else if mode == model.ReportExecutionModeTableSnapshot {
		return nil, invalidReport("result columns are required")
	}
	grants, err := reportGrantsFromRequest(request.Grants, actor)
	if err != nil {
		return nil, err
	}
	return &reportrepo.Draft{
		Definition: model.ReportDefinition{
			Code: request.Code, Name: request.Name, Category: request.Category, Description: request.Description,
			DatasourceID: request.DatasourceID, OwnerUserID: actor, Status: model.ReportDefinitionStatusDraft,
			CreatedBy: actor, UpdatedBy: actor,
		},
		Version: model.ReportVersion{
			Status: model.ReportVersionStatusDraft, DatasourceID: request.DatasourceID, ProcedureOwner: procedure.Owner, PackageName: procedure.Package,
			ProcedureName: procedure.Name, ProcedureOverload: procedure.Overload,
			ExecutionMode: mode, JSONInputArgName: jsonInputArgName, ResultCursorArgName: resultCursorArgName, InputSchemaJSON: model.JSONText(inputSchema),
			ResultTableOwner: resultRef.Owner, ResultTableName: resultRef.Name,
			ResultRunIDColumn: runIDColumn, ResultRowIDColumn: rowIDColumn,
			CallTemplate: callTemplate, CreatedBy: actor,
		},
		Parameters: parameters, Columns: columns, Grants: grants,
	}, nil
}

func reportParametersFromRequest(requests []requestbody.ReportParameterRequest) ([]model.ReportParameter, []reporting.ParameterDefinition, error) {
	if len(requests) == 0 || len(requests) > maxReportParameters {
		return nil, nil, invalidReport("parameters must contain between 1 and 128 items")
	}
	parameters := make([]model.ReportParameter, 0, len(requests))
	definitions := make([]reporting.ParameterDefinition, 0, len(requests))
	displayOrders := make(map[int]struct{}, len(requests))
	for _, request := range requests {
		request.Code = strings.TrimSpace(request.Code)
		request.Label = strings.TrimSpace(request.Label)
		request.ControlType = strings.ToUpper(strings.TrimSpace(request.ControlType))
		request.LogicalType = strings.ToLower(strings.TrimSpace(request.LogicalType))
		request.Cardinality = strings.ToUpper(strings.TrimSpace(request.Cardinality))
		request.ProcedureArgName = strings.TrimSpace(request.ProcedureArgName)
		request.OracleType = strings.ToUpper(strings.Join(strings.Fields(request.OracleType), " "))
		request.Timezone = strings.TrimSpace(request.Timezone)
		request.NullPolicy = strings.ToUpper(strings.TrimSpace(request.NullPolicy))
		request.CollectionEncoding = strings.ToUpper(strings.TrimSpace(request.CollectionEncoding))
		request.ErrorMessage = strings.TrimSpace(request.ErrorMessage)
		_, knownControlType := reportControlTypes[request.ControlType]
		if !reportLogicalCodePattern.MatchString(request.Code) || request.Label == "" || utf8.RuneCountInString(request.Label) > 128 ||
			!knownControlType || request.Position <= 0 || request.DisplayOrder < 0 ||
			request.OracleType == "" || utf8.RuneCountInString(request.OracleType) > 64 || utf8.RuneCountInString(request.ErrorMessage) > 500 {
			return nil, nil, invalidReport("invalid parameter configuration")
		}
		argName, err := normalizeReportIdentifier(request.ProcedureArgName)
		if err != nil {
			return nil, nil, invalidReport("invalid procedure argument name")
		}
		if request.Cardinality == "" {
			request.Cardinality = reporting.CardinalitySingle
		}
		if request.NullPolicy == "" {
			request.NullPolicy = "TYPED_NULL"
		}
		if request.Required && request.Nullable {
			return nil, nil, invalidReport("required parameter cannot be nullable")
		}
		if request.SystemInjected && request.Sensitive {
			return nil, nil, invalidReport("system-injected parameter cannot be sensitive")
		}
		if _, exists := displayOrders[request.DisplayOrder]; exists {
			return nil, nil, invalidReport("duplicated parameter display order")
		}
		displayOrders[request.DisplayOrder] = struct{}{}
		if request.Cardinality != reporting.CardinalitySingle && request.Cardinality != reporting.CardinalityMultiple {
			return nil, nil, invalidReport("invalid parameter cardinality")
		}
		if request.NullPolicy != "TYPED_NULL" {
			return nil, nil, invalidReport("invalid parameter null policy")
		}
		if request.Precision != nil && (*request.Precision < 1 || *request.Precision > 38) ||
			request.Scale != nil && (*request.Scale < -84 || *request.Scale > 127) ||
			request.MaxLength != nil && (*request.MaxLength < 1 || *request.MaxLength > 1_000_000) {
			return nil, nil, invalidReport("invalid parameter size constraint")
		}
		if err := validateOptionalJSON(request.DefaultValue, jsonAny); err != nil ||
			validateOptionalJSON(request.AllowedValues, jsonArray) != nil || validateOptionalJSON(request.Validation, jsonObject) != nil ||
			validateOptionalJSON(request.Normalizer, jsonObject) != nil || validateOptionalJSON(request.ValueSource, jsonObject) != nil {
			return nil, nil, invalidReport("invalid parameter JSON configuration")
		}
		definition := reporting.ParameterDefinition{
			Code: request.Code, ProcedureArgName: argName, Position: request.Position, Direction: "IN",
			LogicalType: request.LogicalType, OracleType: request.OracleType, Cardinality: request.Cardinality,
			Required: request.Required, Nullable: request.Nullable, SystemInjected: request.SystemInjected,
			Sensitive: request.Sensitive, DefaultValue: cloneJSON(request.DefaultValue), AllowedValues: cloneJSON(request.AllowedValues),
			Validation: cloneJSON(request.Validation), Normalizer: cloneJSON(request.Normalizer),
			ValueSource: cloneJSON(request.ValueSource), Timezone: request.Timezone, NullPolicy: request.NullPolicy,
			CollectionEncoding: request.CollectionEncoding,
		}
		if err := reporting.ValidateParameterPresentation(request.ControlType, definition); err != nil {
			return nil, nil, invalidReport("parameter control configuration is invalid")
		}
		parameters = append(parameters, model.ReportParameter{
			ParameterCode: request.Code, Label: request.Label, DisplayOrder: request.DisplayOrder,
			ControlType: request.ControlType, LogicalType: request.LogicalType, Cardinality: request.Cardinality,
			ProcedureArgName: argName, Position: request.Position, Direction: "IN", OracleType: request.OracleType,
			PrecisionValue: request.Precision, ScaleValue: request.Scale, MaxLength: request.MaxLength,
			Required: request.Required, Nullable: request.Nullable, SystemInjected: request.SystemInjected, Sensitive: request.Sensitive,
			DefaultValueJSON: jsonText(request.DefaultValue), AllowedValuesJSON: jsonText(request.AllowedValues),
			ValidationJSON: jsonText(request.Validation), NormalizerJSON: jsonText(request.Normalizer),
			ValueSourceJSON: jsonText(request.ValueSource), Timezone: request.Timezone, NullPolicy: request.NullPolicy,
			CollectionEncoding: request.CollectionEncoding, ErrorMessage: request.ErrorMessage,
		})
		definitions = append(definitions, definition)
	}
	if _, err := reportoracle.BuildCallPlan(reportoracle.ProcedureRef{Owner: "VALIDATION", Name: "VALIDATION"}, definitions); err != nil {
		return nil, nil, invalidReport("parameter binding configuration is invalid")
	}
	if err := reporting.ValidateParameterDefinitions(definitions); err != nil {
		return nil, nil, invalidReport("parameter validation configuration is invalid")
	}
	return parameters, definitions, nil
}

func reportColumnsFromRequest(requests []requestbody.ReportColumnRequest, executionMode string) ([]model.ReportColumn, error) {
	if len(requests) == 0 || len(requests) > maxReportColumns {
		return nil, invalidReport("columns must contain between 1 and 512 items")
	}
	columns := make([]model.ReportColumn, 0, len(requests))
	logicalCodes := make(map[string]struct{}, len(requests))
	fieldIDs := make(map[string]struct{}, len(requests))
	databaseColumns := make(map[string]struct{}, len(requests))
	excelHeaders := make(map[string]struct{}, len(requests))
	displayOrders := make(map[int]struct{}, len(requests))
	exportOrders := make(map[int]struct{}, len(requests))
	exportableColumns := 0
	for _, request := range requests {
		request.FieldID = strings.TrimSpace(request.FieldID)
		request.LogicalCode = strings.TrimSpace(request.LogicalCode)
		request.DatabaseColumn = strings.TrimSpace(request.DatabaseColumn)
		request.SourceOracleType = strings.ToUpper(strings.Join(strings.Fields(request.SourceOracleType), " "))
		request.ValueType = strings.ToLower(strings.TrimSpace(request.ValueType))
		if executionMode == model.ReportExecutionModeRefCursor {
			if request.SourceOracleType == "" {
				request.SourceOracleType = "VARCHAR2"
			}
			if request.ValueType == "" {
				request.ValueType = "string"
			}
			request.Filterable = false
			request.Sortable = false
			request.AllowedOperators = nil
		}
		request.PreviewHeader = strings.TrimSpace(request.PreviewHeader)
		request.ExcelHeader = strings.TrimSpace(request.ExcelHeader)
		request.NullDisplay = strings.TrimSpace(request.NullDisplay)
		fieldID, fieldIDError := uuid.Parse(request.FieldID)
		_, knownValueType := reportValueTypes[request.ValueType]
		if fieldIDError != nil || len(request.FieldID) != 36 || !reportLogicalCodePattern.MatchString(request.LogicalCode) ||
			request.SourceOracleType == "" || len(request.SourceOracleType) > 64 || !knownValueType || request.DisplayOrder < 0 || request.ExportOrder < 0 ||
			request.ExcelWidth < 0 || request.ExcelWidth > 255 || utf8.RuneCountInString(request.PreviewHeader) > 255 ||
			utf8.RuneCountInString(request.ExcelHeader) > 255 || utf8.RuneCountInString(request.NullDisplay) > 64 {
			return nil, invalidReport("invalid result column configuration")
		}
		if request.Precision != nil && (*request.Precision < 1 || *request.Precision > 38) ||
			request.Scale != nil && (*request.Scale < -84 || *request.Scale > 127) {
			return nil, invalidReport("invalid result column size constraint")
		}
		request.FieldID = fieldID.String()
		databaseColumn, err := normalizeReportIdentifier(request.DatabaseColumn)
		if err != nil {
			return nil, invalidReport("invalid Oracle result column")
		}
		logicalKey := strings.ToUpper(request.LogicalCode)
		if _, exists := logicalCodes[logicalKey]; exists {
			return nil, invalidReport("duplicated logical column")
		}
		if _, exists := fieldIDs[request.FieldID]; exists {
			return nil, invalidReport("duplicated stable field id")
		}
		if _, exists := databaseColumns[databaseColumn]; exists {
			return nil, invalidReport("duplicated Oracle result column")
		}
		if _, exists := displayOrders[request.DisplayOrder]; exists {
			return nil, invalidReport("duplicated column display order")
		}
		exportable := request.ExportVisible && request.ExportAllowed
		if exportable {
			if _, exists := exportOrders[request.ExportOrder]; exists {
				return nil, invalidReport("duplicated column export order")
			}
			exportOrders[request.ExportOrder] = struct{}{}
		}
		if exportable {
			exportableColumns++
		}
		if request.PreviewVisible && request.PreviewHeader == "" {
			return nil, invalidReport("visible preview column requires a header")
		}
		if exportable {
			if request.ExcelHeader == "" {
				return nil, invalidReport("visible export column requires an Excel header")
			}
			if _, exists := excelHeaders[request.ExcelHeader]; exists {
				return nil, invalidReport("duplicated Excel header")
			}
			excelHeaders[request.ExcelHeader] = struct{}{}
		}
		if validateOptionalJSON(request.AllowedOperators, jsonArray) != nil || validateOptionalJSON(request.Format, jsonObject) != nil ||
			validateOptionalJSON(request.DictionaryVersion, jsonObject) != nil || validateOptionalJSON(request.MaskingPolicy, jsonObject) != nil {
			return nil, invalidReport("invalid result column JSON configuration")
		}
		logicalCodes[logicalKey] = struct{}{}
		fieldIDs[request.FieldID] = struct{}{}
		databaseColumns[databaseColumn] = struct{}{}
		displayOrders[request.DisplayOrder] = struct{}{}
		columns = append(columns, model.ReportColumn{
			FieldID: request.FieldID, LogicalCode: request.LogicalCode, DatabaseColumn: databaseColumn,
			SourceOracleType: request.SourceOracleType, PrecisionValue: request.Precision, ScaleValue: request.Scale,
			Nullable: request.Nullable, ValueType: request.ValueType, PreviewHeader: request.PreviewHeader,
			ExcelHeader: request.ExcelHeader, DisplayOrder: request.DisplayOrder, ExportOrder: request.ExportOrder,
			PreviewVisible: request.PreviewVisible, ExportVisible: request.ExportVisible, Filterable: request.Filterable,
			Sortable: request.Sortable, ExportAllowed: request.ExportAllowed,
			AllowedOperatorsJSON: jsonText(request.AllowedOperators), FormatJSON: jsonText(request.Format),
			DictionaryVersionJSON: jsonText(request.DictionaryVersion), MaskingPolicyJSON: jsonText(request.MaskingPolicy),
			ExcelWidth: request.ExcelWidth, NullDisplay: request.NullDisplay,
		})
	}
	if exportableColumns == 0 {
		return nil, invalidReport("at least one exportable result column is required")
	}
	sort.SliceStable(columns, func(i, j int) bool { return columns[i].DisplayOrder < columns[j].DisplayOrder })
	return columns, nil
}

func reportGrantsFromRequest(requests []requestbody.ReportGrantRequest, actor uint) ([]model.ReportGrant, error) {
	if len(requests) > maxReportGrants {
		return nil, invalidReport("too many grants")
	}
	grants := make([]model.ReportGrant, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		request.SubjectType = strings.ToUpper(strings.TrimSpace(request.SubjectType))
		if (request.SubjectType != "USER" && request.SubjectType != "ROLE") || request.SubjectID == 0 {
			return nil, invalidReport("invalid grant subject")
		}
		actions, err := normalizeReportActions(request.Actions)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", request.SubjectType, request.SubjectID)
		if _, exists := seen[key]; exists {
			return nil, invalidReport("duplicated grant subject")
		}
		seen[key] = struct{}{}
		grants = append(grants, model.ReportGrant{SubjectType: request.SubjectType, SubjectID: request.SubjectID, ActionsJSON: model.JSONText(actions), CreatedBy: actor, UpdatedBy: actor})
	}
	return grants, nil
}

func normalizeReportActions(raw json.RawMessage) ([]byte, error) {
	var actions []string
	if err := decodeStrictJSON(raw, &actions); err != nil || len(actions) == 0 || len(actions) > 8 {
		return nil, invalidReport("grant actions must be a non-empty string array")
	}
	seen := make(map[string]struct{}, len(actions))
	for index, action := range actions {
		action = strings.ToUpper(strings.TrimSpace(action))
		if _, allowed := reportGrantActions[action]; !allowed {
			return nil, invalidReport("invalid grant action")
		}
		if _, exists := seen[action]; exists {
			return nil, invalidReport("duplicated grant action")
		}
		seen[action] = struct{}{}
		actions[index] = action
	}
	return json.Marshal(actions)
}

type jsonKind uint8

const (
	jsonAny jsonKind = iota
	jsonArray
	jsonObject
)

func validateOptionalJSON(raw json.RawMessage, kind jsonKind) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var value interface{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return err
	}
	switch kind {
	case jsonArray:
		if _, ok := value.([]interface{}); !ok {
			return errors.New("not array")
		}
	case jsonObject:
		if _, ok := value.(map[string]interface{}); !ok {
			return errors.New("not object")
		}
	}
	return nil
}

func decodeStrictJSON(raw []byte, destination interface{}) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func normalizeReportIdentifier(value string) (string, error) {
	ref, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: "VALIDATION", Name: value})
	if err != nil {
		return "", err
	}
	return ref.Name, nil
}

func classifyReportStoreError(err error) error {
	switch {
	case errors.Is(err, reportrepo.ErrDraftNotFound):
		return ErrReportNotFound
	case errors.Is(err, reportrepo.ErrDraftVersionConflict), isReportDuplicateError(err):
		return ErrReportConflict
	case errors.Is(err, reportrepo.ErrDraftDeleteConflict):
		return ErrReportDeleteConflict
	case errors.Is(err, reportrepo.ErrInvalidDraft):
		return invalidReport("draft repository rejected the contract")
	default:
		return fmt.Errorf("report draft service: store: %w", err)
	}
}

func isReportDuplicateError(err error) bool {
	var mysqlError *mysqlDriver.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

func invalidReport(message string) error { return fmt.Errorf("%w: %s", ErrReportInvalid, message) }

func jsonText(raw json.RawMessage) model.JSONText { return model.JSONText(cloneJSON(raw)) }

func cloneJSON(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

type reportInputFieldSchema struct {
	Type          string          `json:"type"`
	DisplayName   string          `json:"displayName"`
	DisplayOrder  *int            `json:"displayOrder,omitempty"`
	Control       string          `json:"control,omitempty"`
	Format        string          `json:"format,omitempty"`
	Required      bool            `json:"required,omitempty"`
	Multiple      bool            `json:"multiple,omitempty"`
	Example       json.RawMessage `json:"example,omitempty"`
	DefaultValue  json.RawMessage `json:"default,omitempty"`
	AllowedValues json.RawMessage `json:"allowedValues,omitempty"`
	QueryName     string          `json:"queryName,omitempty"`
}

func canonicalReportInputSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || len(raw) > 64*1024 {
		return nil, invalidReport("input schema is required and must not exceed 64 KiB")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil || len(fields) == 0 || len(fields) > maxReportParameters {
		return nil, invalidReport("input schema must be a non-empty JSON object with at most 128 fields")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, invalidReport("input schema contains trailing JSON")
	}
	canonical := make(map[string]reportInputFieldSchema, len(fields))
	displayOrders := make(map[int]string, len(fields))
	for code, encoded := range fields {
		if !reportLogicalCodePattern.MatchString(code) {
			return nil, invalidReport("input schema contains an invalid condition code")
		}
		var field reportInputFieldSchema
		fieldDecoder := json.NewDecoder(bytes.NewReader(encoded))
		fieldDecoder.DisallowUnknownFields()
		if err := fieldDecoder.Decode(&field); err != nil {
			return nil, invalidReport("input schema field configuration is invalid")
		}
		if err := fieldDecoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, invalidReport("input schema field contains trailing JSON")
		}
		field.Type = normalizeJSONConditionType(field.Type, field.Multiple)
		field.DisplayName = strings.TrimSpace(field.DisplayName)
		field.Control = strings.ToUpper(strings.TrimSpace(field.Control))
		field.Format = strings.TrimSpace(field.Format)
		field.QueryName = strings.TrimSpace(field.QueryName)
		field.Multiple = false
		if field.Type == "" || field.DisplayName == "" || utf8.RuneCountInString(field.DisplayName) > 128 {
			return nil, invalidReport("input schema field type or displayName is invalid")
		}
		if field.DisplayOrder != nil {
			order := *field.DisplayOrder
			if order < 0 || order >= len(fields) {
				return nil, invalidReport("input schema field displayOrder is invalid")
			}
			if _, exists := displayOrders[order]; exists {
				return nil, invalidReport("input schema field displayOrder is duplicated")
			}
			displayOrders[order] = code
		}
		if field.Control != "" {
			if _, ok := reportControlTypes[field.Control]; !ok {
				return nil, invalidReport("input schema field control is invalid")
			}
		}
		if !validJSONConditionFormat(field.Type, field.Control, field.Format) {
			return nil, invalidReport("input schema field format is invalid")
		}
		if field.QueryName != "" && (field.Control != "SELECT" || !appConfig.ValidateReportInputQueryName(field.QueryName) || !reportInputQueryTypeSupported(field.Type)) {
			return nil, invalidReport("input schema field queryName is invalid")
		}
		if field.QueryName != "" && len(bytes.TrimSpace(field.AllowedValues)) > 0 {
			return nil, invalidReport("input schema field queryName and allowedValues are mutually exclusive")
		}
		if len(bytes.TrimSpace(field.AllowedValues)) > 0 {
			var allowed []json.RawMessage
			if err := decodeStrictReportJSON(field.AllowedValues, &allowed); err != nil || len(allowed) == 0 {
				return nil, invalidReport("input schema allowedValues must be a non-empty JSON array")
			}
		}
		if err := validateReportInputFieldValues(field); err != nil {
			return nil, err
		}
		canonical[code] = field
	}
	if len(displayOrders) != 0 && len(displayOrders) != len(fields) {
		return nil, invalidReport("input schema field displayOrder must be configured for every field")
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, invalidReport("input schema cannot be canonicalized")
	}
	return encoded, nil
}

func reportInputQueryTypeSupported(valueType string) bool {
	switch valueType {
	case "str", "number", "list[str]", "list[number]":
		return true
	default:
		return false
	}
}

func validateReportInputFieldValues(field reportInputFieldSchema) error {
	runtimeField := reportRunInputFieldSchema{
		Type: field.Type, DisplayName: field.DisplayName, Control: field.Control,
		Format: field.Format, Required: field.Required,
	}
	var allowed []json.RawMessage
	if len(bytes.TrimSpace(field.AllowedValues)) > 0 {
		if err := decodeStrictReportJSON(field.AllowedValues, &allowed); err != nil || len(allowed) == 0 {
			return invalidReport("input schema allowedValues must be a non-empty JSON array")
		}
		allowedField := runtimeField
		if conditionTypeIsList(field.Type, false) {
			allowedField.Type = conditionListItemType(field.Type)
			allowedField.Required = false
		}
		for _, value := range allowed {
			_, decoded, err := canonicalConditionValue(value)
			if err != nil || !conditionValueMatchesField(decoded, allowedField) {
				return invalidReport("input schema allowedValues do not match the field type")
			}
		}
	}
	for _, candidate := range []struct {
		name  string
		value json.RawMessage
	}{
		{name: "example", value: field.Example},
		{name: "default", value: field.DefaultValue},
	} {
		if len(bytes.TrimSpace(candidate.value)) == 0 {
			continue
		}
		canonical, decoded, err := canonicalConditionValue(candidate.value)
		if err != nil || !conditionValueMatchesField(decoded, runtimeField) {
			return invalidReport("input schema " + candidate.name + " does not match the field type or format")
		}
		if !conditionValueAllowed(canonical, allowed, conditionTypeIsList(field.Type, false)) {
			return invalidReport("input schema " + candidate.name + " is outside allowedValues")
		}
	}
	return nil
}

func normalizeJSONConditionType(value string, multiple bool) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(value), ""))
	base := ""
	switch normalized {
	case "str", "string", "varchar2", "nvarchar2", "char", "nchar", "clob", "nclob", "date", "timestamp":
		base = "str"
	case "number":
		base = "number"
	case "bool", "boolean":
		base = "bool"
	case "json":
		base = "json"
	case "list[str]", "list[string]", "string[]":
		return "list[str]"
	case "list[number]", "number[]":
		return "list[number]"
	case "list[bool]", "list[boolean]", "boolean[]":
		return "list[bool]"
	default:
		return ""
	}
	if multiple && base != "json" {
		return "list[" + base + "]"
	}
	return base
}

func validJSONConditionFormat(valueType, control, format string) bool {
	if control == "DATE" {
		return valueType == "str" && (format == "YYYYMMDD" || format == "YYYY-MM-DD")
	}
	if control == "DATETIME" {
		return valueType == "str" && (format == "YYYYMMDDHHmmss" || format == "YYYY-MM-DD HH:mm:ss" || format == "ISO8601")
	}
	return format == ""
}

func decodeStrictReportJSON(raw []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func reportDraftDTO(draft *reportrepo.Draft) *ReportDraftDTO {
	if draft == nil {
		return nil
	}
	result := &ReportDraftDTO{
		ID: draft.Definition.ID, Code: draft.Definition.Code, Name: draft.Definition.Name,
		Category: draft.Definition.Category, Description: draft.Definition.Description,
		DatasourceID: draft.Definition.DatasourceID, Status: draft.Definition.Status, LockVersion: draft.LockVersion,
		Procedure:     ReportProcedureDTO{Owner: draft.Version.ProcedureOwner, Package: draft.Version.PackageName, Name: draft.Version.ProcedureName, Overload: draft.Version.ProcedureOverload, JSONInputArgName: draft.Version.JSONInputArgName, ResultCursorArgName: draft.Version.ResultCursorArgName},
		ExecutionMode: draft.Version.ExecutionMode, InputSchema: cloneJSON([]byte(draft.Version.InputSchemaJSON)),
		Result:       ReportResultDTO{TableOwner: draft.Version.ResultTableOwner, TableName: draft.Version.ResultTableName},
		CallTemplate: draft.Version.CallTemplate, Parameters: make([]ReportParameterDTO, 0, len(draft.Parameters)),
		Columns: make([]ReportColumnDTO, 0, len(draft.Columns)), Grants: make([]ReportGrantDTO, 0, len(draft.Grants)),
		CreatedAt: draft.Definition.CreatedAt, UpdatedAt: draft.Definition.UpdatedAt,
	}
	for _, parameter := range draft.Parameters {
		item := ReportParameterDTO{
			Code: parameter.ParameterCode, Label: parameter.Label, DisplayOrder: parameter.DisplayOrder,
			ControlType: parameter.ControlType, LogicalType: parameter.LogicalType, Cardinality: parameter.Cardinality,
			ProcedureArgName: parameter.ProcedureArgName, Position: parameter.Position, OracleType: parameter.OracleType,
			Precision: parameter.PrecisionValue, Scale: parameter.ScaleValue, MaxLength: parameter.MaxLength,
			Required: parameter.Required, Nullable: parameter.Nullable, SystemInjected: parameter.SystemInjected,
			Sensitive: parameter.Sensitive, AllowedValues: cloneJSON([]byte(parameter.AllowedValuesJSON)),
			Validation: cloneJSON([]byte(parameter.ValidationJSON)), Normalizer: cloneJSON([]byte(parameter.NormalizerJSON)),
			ValueSource: cloneJSON([]byte(parameter.ValueSourceJSON)), Timezone: parameter.Timezone,
			NullPolicy: parameter.NullPolicy, CollectionEncoding: parameter.CollectionEncoding, ErrorMessage: parameter.ErrorMessage,
		}
		if !parameter.Sensitive {
			item.DefaultValue = cloneJSON([]byte(parameter.DefaultValueJSON))
		}
		result.Parameters = append(result.Parameters, item)
	}
	for _, column := range draft.Columns {
		result.Columns = append(result.Columns, ReportColumnDTO{
			FieldID: column.FieldID, LogicalCode: column.LogicalCode, DatabaseColumn: column.DatabaseColumn,
			SourceOracleType: column.SourceOracleType, Precision: column.PrecisionValue, Scale: column.ScaleValue,
			Nullable: column.Nullable, ValueType: column.ValueType, PreviewHeader: column.PreviewHeader, ExcelHeader: column.ExcelHeader,
			DisplayOrder: column.DisplayOrder, ExportOrder: column.ExportOrder, PreviewVisible: column.PreviewVisible,
			ExportVisible: column.ExportVisible, Filterable: column.Filterable, Sortable: column.Sortable,
			ExportAllowed: column.ExportAllowed, AllowedOperators: cloneJSON([]byte(column.AllowedOperatorsJSON)),
			Format: cloneJSON([]byte(column.FormatJSON)), DictionaryVersion: cloneJSON([]byte(column.DictionaryVersionJSON)),
			MaskingPolicy: cloneJSON([]byte(column.MaskingPolicyJSON)), ExcelWidth: column.ExcelWidth, NullDisplay: column.NullDisplay,
		})
	}
	for _, grant := range draft.Grants {
		result.Grants = append(result.Grants, ReportGrantDTO{SubjectType: grant.SubjectType, SubjectID: grant.SubjectID, Actions: cloneJSON([]byte(grant.ActionsJSON))})
	}
	return result
}

func reportDraftSummaryDTO(summary reportrepo.DraftSummary) ReportDraftSummaryDTO {
	datasourceID := summary.Definition.DatasourceID
	lockVersion := summary.LockVersion
	currentDraftVersionID := summary.Definition.CurrentDraftVersionID
	currentPublishedVersionID := summary.Definition.CurrentPublishedVersionID
	if !summary.IsOwner {
		// Shared entries expose published catalog metadata only. Draft locks and
		// datasource bindings remain inside the owner-only configuration boundary.
		datasourceID = 0
		lockVersion = 0
		currentDraftVersionID = 0
		currentPublishedVersionID = 0
	}
	return ReportDraftSummaryDTO{
		ID: summary.Definition.ID, Code: summary.Definition.Code, Name: summary.Definition.Name,
		Category: summary.Definition.Category, Description: summary.Definition.Description,
		DatasourceID: datasourceID, Status: summary.Definition.Status,
		LockVersion: lockVersion, CurrentDraftVersionID: currentDraftVersionID, CurrentPublishedVersionID: currentPublishedVersionID, IsOwner: summary.IsOwner,
		CreatedAt: summary.Definition.CreatedAt, UpdatedAt: summary.Definition.UpdatedAt,
	}
}
