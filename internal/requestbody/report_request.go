package requestbody

import (
	"encoding/json"

	"gin-biz-web-api/internal/reportquery"
)

// ReportDraftSaveRequest is the complete mutable report contract. Updates
// replace the draft atomically and require ExpectedLockVersion.
type ReportDraftSaveRequest struct {
	Code                string                   `json:"code"`
	Name                string                   `json:"name"`
	Category            string                   `json:"category,omitempty"`
	Description         string                   `json:"description,omitempty"`
	DatasourceID        uint                     `json:"datasourceId"`
	ExpectedLockVersion *uint64                  `json:"expectedLockVersion,omitempty"`
	Procedure           ReportProcedureRequest   `json:"procedure"`
	ExecutionMode       string                   `json:"executionMode"`
	InputSchema         json.RawMessage          `json:"inputSchema,omitempty"`
	Result              ReportResultRequest      `json:"result"`
	CallTemplate        string                   `json:"callTemplate"`
	Parameters          []ReportParameterRequest `json:"parameters"`
	Columns             []ReportColumnRequest    `json:"columns"`
	Grants              []ReportGrantRequest     `json:"grants"`
}

type ReportProcedureRequest struct {
	Owner               string `json:"owner"`
	Package             string `json:"package,omitempty"`
	Name                string `json:"name"`
	Overload            string `json:"overload,omitempty"`
	JSONInputArgName    string `json:"jsonInputArgName,omitempty"`
	ResultCursorArgName string `json:"resultCursorArgName,omitempty"`
}

type ReportResultRequest struct {
	TableOwner string `json:"tableOwner"`
	TableName  string `json:"tableName"`
}

type ReportParameterRequest struct {
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

type ReportColumnRequest struct {
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

type ReportGrantRequest struct {
	SubjectType string          `json:"subjectType"`
	SubjectID   uint            `json:"subjectId"`
	Actions     json.RawMessage `json:"actions"`
}

type ReportCategoryAccessSaveRequest struct {
	Category            string               `json:"category"`
	ExpectedLockVersion uint64               `json:"expectedLockVersion"`
	Grants              []ReportGrantRequest `json:"grants"`
}

type ReportPublishRequest struct {
	ExpectedLockVersion uint64 `json:"expectedLockVersion"`
}

type ReportVersionActivateRequest struct {
	VersionID           uint   `json:"versionId"`
	ExpectedLockVersion uint64 `json:"expectedLockVersion"`
}

type ReportRunCreateRequest struct {
	Parameters   map[string]json.RawMessage `json:"parameters"`
	Conditions   map[string]json.RawMessage `json:"conditions"`
	RefreshNonce string                     `json:"refreshNonce,omitempty"`
}

type ReportResultQueryRequest struct {
	Filters []reportquery.FilterInput `json:"filters"`
	Sort    []reportquery.SortInput   `json:"sort"`
	Cursor  string                    `json:"cursor,omitempty"`
	Limit   int                       `json:"limit,omitempty"`
}

type ReportExportCreateRequest struct {
	Filters []reportquery.FilterInput `json:"filters"`
	Sort    []reportquery.SortInput   `json:"sort"`
}

type ReportDatasourceSaveRequest struct {
	Code                  string `json:"code"`
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	ServiceName           string `json:"serviceName,omitempty"`
	SID                   string `json:"sid,omitempty"`
	Username              string `json:"username"`
	Password              string `json:"password,omitempty"`
	SessionTimezone       string `json:"sessionTimezone,omitempty"`
	ConnectTimeoutSeconds int    `json:"connectTimeoutSeconds"`
	QueryTimeoutSeconds   int    `json:"queryTimeoutSeconds"`
	MaxOpenConnections    int    `json:"maxOpenConnections"`
	MaxIdleConnections    int    `json:"maxIdleConnections"`
	PrefetchRows          int    `json:"prefetchRows"`
	ArraySize             int    `json:"arraySize"`
	Enabled               bool   `json:"enabled"`
}

// ReportDatasourceConnectionTestRequest probes Oracle with the supplied
// connection draft without persisting the draft or its password.
type ReportDatasourceConnectionTestRequest struct {
	DatasourceID          uint   `json:"datasourceId,omitempty"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	ServiceName           string `json:"serviceName,omitempty"`
	SID                   string `json:"sid,omitempty"`
	Username              string `json:"username"`
	Password              string `json:"password,omitempty"`
	SessionTimezone       string `json:"sessionTimezone,omitempty"`
	ConnectTimeoutSeconds int    `json:"connectTimeoutSeconds"`
	QueryTimeoutSeconds   int    `json:"queryTimeoutSeconds"`
	MaxOpenConnections    int    `json:"maxOpenConnections"`
	MaxIdleConnections    int    `json:"maxIdleConnections"`
	PrefetchRows          int    `json:"prefetchRows"`
	ArraySize             int    `json:"arraySize"`
}

type ReportInputQueryDefinitionSaveRequest struct {
	Name                string `json:"name"`
	SelectSQL           string `json:"selectSql"`
	Enabled             bool   `json:"enabled"`
	ExpectedLockVersion uint64 `json:"expectedLockVersion,omitempty"`
}

type ReportInputQueryDefinitionTestRequest struct {
	SelectSQL string `json:"selectSql,omitempty"`
	Name      string `json:"name,omitempty"`
}
