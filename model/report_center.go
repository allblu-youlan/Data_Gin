package model

import "time"

const (
	ReportAuditActorUser   = "USER"
	ReportAuditActorSystem = "SYSTEM"

	ReportDatasourceDriverOracle = "ORACLE"

	ReportDefinitionStatusDraft    = "DRAFT"
	ReportDefinitionStatusActive   = "ACTIVE"
	ReportDefinitionStatusDisabled = "DISABLED"

	ReportVersionStatusDraft     = "DRAFT"
	ReportVersionStatusPublished = "PUBLISHED"

	ReportExecutionModeTableSnapshot  = "TABLE_SNAPSHOT"
	ReportExecutionModeRefCursor      = "REF_CURSOR"
	ReportExecutionModeSelectSnapshot = "SELECT_SNAPSHOT"

	ReportRunStatusQueued          = "QUEUED"
	ReportRunStatusRunning         = "RUNNING"
	ReportRunStatusSucceeded       = "SUCCEEDED"
	ReportRunStatusFailed          = "FAILED"
	ReportRunStatusCancelRequested = "CANCEL_REQUESTED"
	ReportRunStatusCancelled       = "CANCELLED"
	ReportRunStatusUnknown         = "UNKNOWN"
	ReportRunStatusReconciling     = "RECONCILING"
	ReportRunStatusExporting       = "EXPORTING"
	ReportRunStatusExported        = "EXPORTED"
	ReportRunStatusResultPurging   = "RESULT_PURGING"
	ReportRunStatusResultPurged    = "RESULT_PURGED"
	ReportRunStatusSuperseded      = "SUPERSEDED"

	ReportExportStatusPending   = "PENDING"
	ReportExportStatusRunning   = "RUNNING"
	ReportExportStatusReady     = "READY"
	ReportExportStatusFailed    = "FAILED"
	ReportExportStatusCancelled = "CANCELLED"
	ReportExportStatusExpired   = "EXPIRED"
)

// ReportDatasource stores Oracle connection configuration in the MySQL
// control database. PasswordCiphertext must only contain encrypted material.
type ReportDatasource struct {
	BaseModel
	Code                  string     `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	Name                  string     `gorm:"column:name;size:128;not null" json:"name"`
	Driver                string     `gorm:"column:driver;size:16;not null;default:'ORACLE';index" json:"driver"`
	Host                  string     `gorm:"column:host;size:255;not null" json:"host"`
	Port                  int        `gorm:"column:port;not null;default:1521" json:"port"`
	ServiceName           string     `gorm:"column:service_name;size:128" json:"serviceName"`
	SID                   string     `gorm:"column:sid;size:128" json:"sid"`
	Username              string     `gorm:"column:username;size:128;not null" json:"username"`
	PasswordCiphertext    string     `gorm:"column:password_ciphertext;type:text;not null" json:"-"`
	CredentialKeyVersion  string     `gorm:"column:credential_key_version;size:64;not null" json:"-"`
	SessionTimezone       string     `gorm:"column:session_timezone;size:64;not null;default:'Asia/Shanghai'" json:"sessionTimezone"`
	SessionInitJSON       JSONText   `gorm:"column:session_init_json;type:json" json:"-"`
	ConnectTimeoutSeconds int        `gorm:"column:connect_timeout_seconds;not null;default:10" json:"connectTimeoutSeconds"`
	QueryTimeoutSeconds   int        `gorm:"column:query_timeout_seconds;not null;default:300" json:"queryTimeoutSeconds"`
	MaxOpenConnections    int        `gorm:"column:max_open_connections;not null;default:10" json:"maxOpenConnections"`
	MaxIdleConnections    int        `gorm:"column:max_idle_connections;not null;default:2" json:"maxIdleConnections"`
	PrefetchRows          int        `gorm:"column:prefetch_rows;not null;default:1000" json:"prefetchRows"`
	ArraySize             int        `gorm:"column:array_size;not null;default:1000" json:"arraySize"`
	Enabled               bool       `gorm:"column:enabled;not null;default:true;index" json:"enabled"`
	LastTestStatus        string     `gorm:"column:last_test_status;size:32" json:"lastTestStatus"`
	LastTestErrorSafe     string     `gorm:"column:last_test_error_safe;size:500" json:"lastTestErrorSafe"`
	LastTestedAt          *time.Time `gorm:"column:last_tested_at;type:datetime(3)" json:"lastTestedAt"`
	CreatedBy             uint       `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	UpdatedBy             uint       `gorm:"column:updated_by;not null;default:0" json:"updatedBy"`
	WeatherTimestamps
}

func (ReportDatasource) TableName() string { return "report_datasources" }

// ReportInputQueryDefinition stores a named SELECT used by report input
// controls. Oracle connection details remain process environment configuration.
type ReportInputQueryDefinition struct {
	BaseModel
	Name              string     `gorm:"column:name;size:64;not null;uniqueIndex" json:"name"`
	SelectSQL         string     `gorm:"column:select_sql;type:longtext;not null" json:"selectSql"`
	Enabled           bool       `gorm:"column:enabled;not null;index" json:"enabled"`
	LockVersion       uint64     `gorm:"column:lock_version;not null;default:1" json:"lockVersion"`
	LastTestStatus    string     `gorm:"column:last_test_status;size:16" json:"lastTestStatus"`
	LastTestErrorSafe string     `gorm:"column:last_test_error_safe;size:500" json:"lastTestErrorSafe"`
	LastTestedAt      *time.Time `gorm:"column:last_tested_at;type:datetime(3)" json:"lastTestedAt"`
	CreatedBy         uint       `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	UpdatedBy         uint       `gorm:"column:updated_by;not null;default:0" json:"updatedBy"`
	WeatherTimestamps
}

func (ReportInputQueryDefinition) TableName() string { return "report_input_query_definitions" }

// ReportResultTableBinding makes a physical Oracle result table exclusive to
// one report definition. ConnectionFingerprint identifies the Oracle database
// and CDB/PDB container without persisting credentials in the registry.
type ReportResultTableBinding struct {
	BaseModel
	ConnectionFingerprint string `gorm:"column:connection_fingerprint;type:char(64);not null;uniqueIndex:uk_report_result_table_binding,priority:1" json:"-"`
	IdentitySource        string `gorm:"column:identity_source;size:32;not null;default:LEGACY_DATASOURCE_V1;index" json:"-"`
	TableOwner            string `gorm:"column:table_owner;size:128;not null;uniqueIndex:uk_report_result_table_binding,priority:2" json:"tableOwner"`
	ResultTableName       string `gorm:"column:table_name;size:128;not null;uniqueIndex:uk_report_result_table_binding,priority:3" json:"tableName"`
	DefinitionID          uint   `gorm:"column:definition_id;not null;uniqueIndex" json:"definitionId"`
	VersionID             uint   `gorm:"column:version_id;not null;index" json:"versionId"`
	WeatherTimestamps
}

func (ReportResultTableBinding) TableName() string { return "report_result_table_bindings" }

// ReportDefinition is the mutable catalog entry. Published behavior is always
// read from CurrentPublishedVersionID and the immutable ReportVersion snapshot.
type ReportDefinition struct {
	BaseModel
	Code                      string `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	Name                      string `gorm:"column:name;size:128;not null" json:"name"`
	Category                  string `gorm:"column:category;size:64;index" json:"category"`
	Description               string `gorm:"column:description;size:500" json:"description"`
	DatasourceID              uint   `gorm:"column:datasource_id;not null;index" json:"datasourceId"`
	OwnerUserID               uint   `gorm:"column:owner_user_id;not null;index" json:"ownerUserId"`
	Status                    string `gorm:"column:status;size:16;not null;default:'DRAFT';index" json:"status"`
	CurrentDraftVersionID     uint   `gorm:"column:current_draft_version_id;not null;default:0" json:"currentDraftVersionId"`
	CurrentPublishedVersionID uint   `gorm:"column:current_published_version_id;not null;default:0" json:"currentPublishedVersionId"`
	CreatedBy                 uint   `gorm:"column:created_by;not null;default:0" json:"createdBy"`
	UpdatedBy                 uint   `gorm:"column:updated_by;not null;default:0" json:"updatedBy"`
	WeatherTimestamps
}

func (ReportDefinition) TableName() string { return "report_definitions" }

// ReportVersion contains the complete MySQL-side contract for an Oracle
// procedure and result table. Rows with status PUBLISHED are immutable.
type ReportVersion struct {
	QuerySpecJSON        JSONText `gorm:"type:json" json:"querySpec,omitempty"`
	MetadataSnapshotJSON JSONText `gorm:"type:json" json:"metadataSnapshot,omitempty"`
	ExecutionPolicyJSON  JSONText `gorm:"type:json" json:"executionPolicy,omitempty"`

	BaseModel
	DefinitionID uint `gorm:"column:definition_id;not null;uniqueIndex:uk_report_version_number,priority:1;index" json:"definitionId"`
	// DatasourceID stays nullable at the database layer during the rolling
	// expand phase. Application writes and publication validation require it.
	DatasourceID           uint       `gorm:"column:datasource_id;index" json:"datasourceId"`
	VersionNumber          uint64     `gorm:"column:version_number;not null;uniqueIndex:uk_report_version_number,priority:2" json:"versionNumber"`
	Status                 string     `gorm:"column:status;size:16;not null;default:'DRAFT';index" json:"status"`
	ProcedureOwner         string     `gorm:"column:procedure_owner;size:128;not null" json:"procedureOwner"`
	PackageName            string     `gorm:"column:package_name;size:128" json:"packageName"`
	ProcedureName          string     `gorm:"column:procedure_name;size:128;not null" json:"procedureName"`
	ProcedureOverload      string     `gorm:"column:procedure_overload;size:32" json:"procedureOverload"`
	ExecutionMode          string     `gorm:"column:execution_mode;size:32;not null;default:'TABLE_SNAPSHOT';index" json:"executionMode"`
	JSONInputArgName       string     `gorm:"column:json_input_arg_name;size:128" json:"jsonInputArgName"`
	ResultCursorArgName    string     `gorm:"column:result_cursor_arg_name;size:128" json:"resultCursorArgName"`
	InputSchemaJSON        JSONText   `gorm:"column:input_schema_json;type:json" json:"inputSchema"`
	ResultTableOwner       string     `gorm:"column:result_table_owner;size:128;not null" json:"resultTableOwner"`
	ResultTableName        string     `gorm:"column:result_table_name;size:128;not null" json:"resultTableName"`
	ResultRunIDColumn      string     `gorm:"column:result_run_id_column;size:128;not null;default:'RUN_ID'" json:"-"`
	ResultRowIDColumn      string     `gorm:"column:result_row_id_column;size:128;not null;default:'ID'" json:"-"`
	CallTemplate           string     `gorm:"column:call_template;type:longtext;not null" json:"callTemplate"`
	CompiledSpecJSON       JSONText   `gorm:"column:compiled_spec_json;type:json" json:"compiledSpec"`
	ContractHash           string     `gorm:"column:contract_hash;type:char(64);index" json:"contractHash"`
	ParameterSchemaHash    string     `gorm:"column:parameter_schema_hash;type:char(64)" json:"parameterSchemaHash"`
	ProcedureSignatureHash string     `gorm:"column:procedure_signature_hash;type:char(64)" json:"procedureSignatureHash"`
	ResultSchemaHash       string     `gorm:"column:result_schema_hash;type:char(64)" json:"resultSchemaHash"`
	PermissionHash         string     `gorm:"column:permission_hash;type:char(64)" json:"permissionHash"`
	ExportSchemaHash       string     `gorm:"column:export_schema_hash;type:char(64)" json:"exportSchemaHash"`
	SchemaProbeToken       string     `gorm:"column:schema_probe_token;type:char(36)" json:"schemaProbeToken"`
	SchemaValidatedAt      *time.Time `gorm:"column:schema_validated_at;type:datetime(3)" json:"schemaValidatedAt"`
	CreatedBy              uint       `gorm:"column:created_by;not null" json:"createdBy"`
	PublishedBy            uint       `gorm:"column:published_by;not null;default:0" json:"publishedBy"`
	PublishedAt            *time.Time `gorm:"column:published_at;type:datetime(3);index" json:"publishedAt"`
	WeatherTimestamps
}

func (ReportVersion) TableName() string { return "report_versions" }

// ReportParameter persists the UI, logical and Oracle binding layers for one
// {{parameterCode}} placeholder.
type ReportParameter struct {
	BaseModel
	VersionID          uint     `gorm:"column:version_id;not null;uniqueIndex:uk_report_parameter_code,priority:1;uniqueIndex:uk_report_parameter_position,priority:1;index" json:"versionId"`
	ParameterCode      string   `gorm:"column:parameter_code;size:64;not null;uniqueIndex:uk_report_parameter_code,priority:2" json:"parameterCode"`
	Label              string   `gorm:"column:label;size:128;not null" json:"label"`
	DisplayOrder       int      `gorm:"column:display_order;not null;default:0" json:"displayOrder"`
	ControlType        string   `gorm:"column:control_type;size:32;not null" json:"controlType"`
	LogicalType        string   `gorm:"column:logical_type;size:32;not null" json:"logicalType"`
	Cardinality        string   `gorm:"column:cardinality;size:16;not null;default:'SINGLE'" json:"cardinality"`
	ProcedureArgName   string   `gorm:"column:procedure_arg_name;size:128;not null" json:"procedureArgName"`
	Position           int      `gorm:"column:position;not null;uniqueIndex:uk_report_parameter_position,priority:2" json:"position"`
	Direction          string   `gorm:"column:direction;size:8;not null;default:'IN'" json:"direction"`
	OracleType         string   `gorm:"column:oracle_type;size:64;not null" json:"oracleType"`
	PrecisionValue     *int     `gorm:"column:precision_value" json:"precision"`
	ScaleValue         *int     `gorm:"column:scale_value" json:"scale"`
	MaxLength          *int     `gorm:"column:max_length" json:"maxLength"`
	Required           bool     `gorm:"column:required;not null;default:false" json:"required"`
	Nullable           bool     `gorm:"column:nullable;not null;default:true" json:"nullable"`
	SystemInjected     bool     `gorm:"column:system_injected;not null;default:false" json:"systemInjected"`
	Sensitive          bool     `gorm:"column:sensitive;not null;default:false" json:"sensitive"`
	DefaultValueJSON   JSONText `gorm:"column:default_value_json;type:json" json:"defaultValue"`
	AllowedValuesJSON  JSONText `gorm:"column:allowed_values_json;type:json" json:"allowedValues"`
	ValidationJSON     JSONText `gorm:"column:validation_json;type:json" json:"validation"`
	NormalizerJSON     JSONText `gorm:"column:normalizer_json;type:json" json:"normalizer"`
	ValueSourceJSON    JSONText `gorm:"column:value_source_json;type:json" json:"valueSource"`
	Timezone           string   `gorm:"column:timezone;size:64" json:"timezone"`
	NullPolicy         string   `gorm:"column:null_policy;size:32;not null;default:'TYPED_NULL'" json:"nullPolicy"`
	CollectionEncoding string   `gorm:"column:collection_encoding;size:32" json:"collectionEncoding"`
	ErrorMessage       string   `gorm:"column:error_message;size:500" json:"errorMessage"`
	WeatherTimestamps
}

func (ReportParameter) TableName() string { return "report_parameters" }

// ReportColumn maps a stable logical column to an Oracle result-table column
// and to the shared preview/Excel presentation contract.
type ReportColumn struct {
	SourceSpecJSON JSONText `gorm:"type:json" json:"source,omitempty"`
	ValueMode      string   `gorm:"size:16" json:"valueMode,omitempty"`
	RelationID     string   `gorm:"size:64" json:"relationId,omitempty"`

	BaseModel
	VersionID             uint     `gorm:"column:version_id;not null;uniqueIndex:uk_report_column_code,priority:1;uniqueIndex:uk_report_column_field,priority:1;index" json:"versionId"`
	FieldID               string   `gorm:"column:field_id;type:char(36);not null;uniqueIndex:uk_report_column_field,priority:2" json:"fieldId"`
	LogicalCode           string   `gorm:"column:logical_code;size:64;not null;uniqueIndex:uk_report_column_code,priority:2" json:"logicalCode"`
	DatabaseColumn        string   `gorm:"column:database_column;size:128;not null" json:"databaseColumn"`
	SourceOracleType      string   `gorm:"column:source_oracle_type;size:64;not null" json:"sourceOracleType"`
	PrecisionValue        *int     `gorm:"column:precision_value" json:"precision"`
	ScaleValue            *int     `gorm:"column:scale_value" json:"scale"`
	Nullable              bool     `gorm:"column:nullable;not null;default:true" json:"nullable"`
	ValueType             string   `gorm:"column:value_type;size:32;not null" json:"valueType"`
	PreviewHeader         string   `gorm:"column:preview_header;size:255;not null" json:"previewHeader"`
	ExcelHeader           string   `gorm:"column:excel_header;size:255;not null" json:"excelHeader"`
	DisplayOrder          int      `gorm:"column:display_order;not null;default:0" json:"displayOrder"`
	ExportOrder           int      `gorm:"column:export_order;not null;default:0" json:"exportOrder"`
	PreviewVisible        bool     `gorm:"column:preview_visible;not null;default:true" json:"previewVisible"`
	ExportVisible         bool     `gorm:"column:export_visible;not null;default:true" json:"exportVisible"`
	Filterable            bool     `gorm:"column:filterable;not null;default:false" json:"filterable"`
	Sortable              bool     `gorm:"column:sortable;not null;default:false" json:"sortable"`
	ExportAllowed         bool     `gorm:"column:export_allowed;not null;default:true" json:"exportAllowed"`
	AllowedOperatorsJSON  JSONText `gorm:"column:allowed_operators_json;type:json" json:"allowedOperators"`
	FormatJSON            JSONText `gorm:"column:format_json;type:json" json:"format"`
	DictionaryVersionJSON JSONText `gorm:"column:dictionary_version_json;type:json" json:"dictionaryVersion"`
	MaskingPolicyJSON     JSONText `gorm:"column:masking_policy_json;type:json" json:"maskingPolicy"`
	ExcelWidth            float64  `gorm:"column:excel_width;type:decimal(6,2);not null;default:0" json:"excelWidth"`
	NullDisplay           string   `gorm:"column:null_display;size:64" json:"nullDisplay"`
	WeatherTimestamps
}

func (ReportColumn) TableName() string { return "report_columns" }

// ReportGrant assigns version-scoped actions to a user or role. Published
// permissions remain immutable while a later draft changes its own grants.
// The permission hash and each run snapshot are derived from the selected
// version's rows plus the global RBAC decision.
type ReportGrant struct {
	BaseModel
	DefinitionID uint      `gorm:"column:definition_id;not null;index" json:"definitionId"`
	VersionID    uint      `gorm:"column:version_id;not null;uniqueIndex:uk_report_grant_subject,priority:1;index" json:"versionId"`
	SubjectType  string    `gorm:"column:subject_type;size:16;not null;uniqueIndex:uk_report_grant_subject,priority:2" json:"subjectType"`
	SubjectID    uint      `gorm:"column:subject_id;not null;uniqueIndex:uk_report_grant_subject,priority:3;index" json:"subjectId"`
	ActionsJSON  JSONText  `gorm:"column:actions_json;type:json;not null" json:"actions"`
	CreatedBy    uint      `gorm:"column:created_by;not null" json:"createdBy"`
	UpdatedBy    uint      `gorm:"column:updated_by;not null" json:"updatedBy"`
	CreatedAt    time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updatedAt"`
}

func (ReportGrant) TableName() string { return "report_grants" }

// ReportCategoryAccess marks a report category as using category-level
// authorization. Once configured, its grants replace legacy report-level
// grants, including when the category intentionally has no grants.
type ReportCategoryAccess struct {
	BaseModel
	Category    string `gorm:"column:category;size:64;not null;uniqueIndex" json:"category"`
	LockVersion uint64 `gorm:"column:lock_version;not null;default:1" json:"lockVersion"`
	UpdatedBy   uint   `gorm:"column:updated_by;not null" json:"updatedBy"`
	WeatherTimestamps
}

func (ReportCategoryAccess) TableName() string { return "report_category_access" }

// ReportCategoryGrant assigns query and export actions to a user or role for
// every published report in one configured category.
type ReportCategoryGrant struct {
	BaseModel
	CategoryAccessID uint     `gorm:"column:category_access_id;not null;uniqueIndex:uk_report_category_grant_subject,priority:1;index" json:"categoryAccessId"`
	SubjectType      string   `gorm:"column:subject_type;size:16;not null;uniqueIndex:uk_report_category_grant_subject,priority:2" json:"subjectType"`
	SubjectID        uint     `gorm:"column:subject_id;not null;uniqueIndex:uk_report_category_grant_subject,priority:3;index" json:"subjectId"`
	ActionsJSON      JSONText `gorm:"column:actions_json;type:json;not null" json:"actions"`
	CreatedBy        uint     `gorm:"column:created_by;not null" json:"createdBy"`
	UpdatedBy        uint     `gorm:"column:updated_by;not null" json:"updatedBy"`
	WeatherTimestamps
}

func (ReportCategoryGrant) TableName() string { return "report_category_grants" }

// ReportRun is the durable MySQL control record for one Oracle execution and
// its immutable parameter, permission and presentation snapshots.
type ReportRun struct {
	QueryInputsJSON     JSONText `gorm:"type:json" json:"-"`
	DataScopeHash       string   `gorm:"size:64" json:"-"`
	ExecutionPolicyJSON JSONText `gorm:"type:json" json:"executionPolicy,omitempty"`

	BaseModel
	RunUUID                       string     `gorm:"column:run_uuid;type:char(36);not null;uniqueIndex" json:"runUuid"`
	DefinitionID                  uint       `gorm:"column:definition_id;not null;index" json:"definitionId"`
	VersionID                     uint       `gorm:"column:version_id;not null;index" json:"versionId"`
	RequestedBy                   uint       `gorm:"column:requested_by;not null;index" json:"requestedBy"`
	Status                        string     `gorm:"column:status;size:32;not null;default:'QUEUED';index" json:"status"`
	ExecutionFingerprint          string     `gorm:"column:execution_fingerprint;type:char(64);not null;index" json:"executionFingerprint"`
	RefreshNonce                  string     `gorm:"column:refresh_nonce;size:64" json:"-"`
	NormalizedParametersJSON      JSONText   `gorm:"column:normalized_parameters_json;type:json;not null" json:"-"`
	SensitiveParametersCipher     string     `gorm:"column:sensitive_parameters_cipher;type:longtext" json:"-"`
	SensitiveParametersKeyVersion string     `gorm:"column:sensitive_parameters_key_version;size:64" json:"-"`
	PermissionSnapshotJSON        JSONText   `gorm:"column:permission_snapshot_json;type:json;not null" json:"permissionSnapshot"`
	PresentationSnapshotJSON      JSONText   `gorm:"column:presentation_snapshot_json;type:json;not null" json:"presentationSnapshot"`
	ContractHash                  string     `gorm:"column:contract_hash;type:char(64);not null" json:"contractHash"`
	ProcedureSignatureHash        string     `gorm:"column:procedure_signature_hash;type:char(64);not null" json:"procedureSignatureHash"`
	ResultSchemaHash              string     `gorm:"column:result_schema_hash;type:char(64);not null" json:"resultSchemaHash"`
	RowCount                      int64      `gorm:"column:row_count;not null;default:0" json:"rowCount"`
	CancelRequested               bool       `gorm:"column:cancel_requested;not null;default:false;index" json:"cancelRequested"`
	Attempt                       int        `gorm:"column:attempt;not null;default:0" json:"attempt"`
	WorkerID                      string     `gorm:"column:worker_id;size:128" json:"-"`
	LeaseToken                    string     `gorm:"column:lease_token;type:char(36);index" json:"-"`
	LeaseExpiresAt                *time.Time `gorm:"column:lease_expires_at;type:datetime(3);index" json:"-"`
	HeartbeatAt                   *time.Time `gorm:"column:heartbeat_at;type:datetime(3)" json:"heartbeatAt"`
	StartedAt                     *time.Time `gorm:"column:started_at;type:datetime(3)" json:"startedAt"`
	OracleStartedAt               *time.Time `gorm:"column:oracle_started_at;type:datetime(3);index" json:"-"`
	FinishedAt                    *time.Time `gorm:"column:finished_at;type:datetime(3)" json:"finishedAt"`
	ResultExpiresAt               *time.Time `gorm:"column:result_expires_at;type:datetime(3);index" json:"resultExpiresAt"`
	ResultPurgedAt                *time.Time `gorm:"column:result_purged_at;type:datetime(3)" json:"resultPurgedAt"`
	UnknownAt                     *time.Time `gorm:"column:unknown_at;type:datetime(3);index" json:"unknownAt"`
	UnknownReasonCode             string     `gorm:"column:unknown_reason_code;size:64" json:"unknownReasonCode"`
	ReconcileAttempts             int        `gorm:"column:reconcile_attempts;not null;default:0" json:"reconcileAttempts"`
	NextReconcileAt               *time.Time `gorm:"column:next_reconcile_at;type:datetime(3);index" json:"nextReconcileAt"`
	LastReconciledAt              *time.Time `gorm:"column:last_reconciled_at;type:datetime(3)" json:"lastReconciledAt"`
	ErrorCode                     string     `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessageSafe              string     `gorm:"column:error_message_safe;type:text" json:"errorMessage"`
	WeatherTimestamps
}

func (ReportRun) TableName() string { return "report_runs" }

// ReportExport is unique per run. Once READY, all downloads reuse the stored
// object and the Oracle run result can be purged.
type ReportExport struct {
	BaseModel
	ExportUUID         string     `gorm:"column:export_uuid;type:char(36);not null;uniqueIndex" json:"exportUuid"`
	RunID              uint       `gorm:"column:run_id;not null;uniqueIndex" json:"runId"`
	Status             string     `gorm:"column:status;size:32;not null;default:'PENDING';index" json:"status"`
	FrozenFiltersJSON  JSONText   `gorm:"column:frozen_filters_json;type:json;not null" json:"filters"`
	FrozenSortJSON     JSONText   `gorm:"column:frozen_sort_json;type:json;not null" json:"sort"`
	FrozenColumnsJSON  JSONText   `gorm:"column:frozen_columns_json;type:json;not null" json:"columns"`
	ResultObjectKey    string     `gorm:"column:result_object_key;size:1024" json:"-"`
	ResultChecksum     string     `gorm:"column:result_checksum;type:char(64)" json:"checksum"`
	FileSizeBytes      int64      `gorm:"column:file_size_bytes;not null;default:0" json:"fileSizeBytes"`
	ExportedRows       int64      `gorm:"column:exported_rows;not null;default:0" json:"exportedRows"`
	SheetCount         int        `gorm:"column:sheet_count;not null;default:0" json:"sheetCount"`
	TruncatedCellCount int64      `gorm:"column:truncated_cell_count;not null;default:0" json:"truncatedCellCount"`
	Attempt            int        `gorm:"column:attempt;not null;default:0" json:"attempt"`
	CancelRequested    bool       `gorm:"column:cancel_requested;not null;default:false" json:"cancelRequested"`
	WorkerID           string     `gorm:"column:worker_id;size:128" json:"-"`
	LeaseToken         string     `gorm:"column:lease_token;type:char(36);index" json:"-"`
	LeaseExpiresAt     *time.Time `gorm:"column:lease_expires_at;type:datetime(3);index" json:"-"`
	HeartbeatAt        *time.Time `gorm:"column:heartbeat_at;type:datetime(3)" json:"heartbeatAt"`
	ProcessedRows      int64      `gorm:"column:processed_rows;not null;default:0" json:"processedRows"`
	CurrentSheet       string     `gorm:"column:current_sheet;size:255" json:"currentSheet"`
	CheckpointJSON     JSONText   `gorm:"column:checkpoint_json;type:json" json:"-"`
	StartedAt          *time.Time `gorm:"column:started_at;type:datetime(3)" json:"startedAt"`
	ReadyAt            *time.Time `gorm:"column:ready_at;type:datetime(3)" json:"readyAt"`
	ExpiresAt          *time.Time `gorm:"column:expires_at;type:datetime(3);index" json:"expiresAt"`
	PurgeStartedAt     *time.Time `gorm:"column:purge_started_at;type:datetime(3)" json:"purgeStartedAt"`
	PurgedAt           *time.Time `gorm:"column:purged_at;type:datetime(3)" json:"purgedAt"`
	PurgedRows         int64      `gorm:"column:purged_rows;not null;default:0" json:"purgedRows"`
	PurgeCursor        int64      `gorm:"column:purge_cursor;not null;default:0" json:"-"`
	ErrorCode          string     `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessageSafe   string     `gorm:"column:error_message_safe;type:text" json:"errorMessage"`
	CreatedBy          uint       `gorm:"column:created_by;not null" json:"createdBy"`
	WeatherTimestamps
}

func (ReportExport) TableName() string { return "report_exports" }

type ReportResultReadLease struct {
	BaseModel
	RunID      uint      `gorm:"column:run_id;not null;index:idx_report_result_read_lease,priority:1" json:"-"`
	LeaseToken string    `gorm:"column:lease_token;type:char(36);not null;uniqueIndex" json:"-"`
	ExpiresAt  time.Time `gorm:"column:expires_at;type:datetime(3);not null;index:idx_report_result_read_lease,priority:2" json:"-"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"-"`
}

func (ReportResultReadLease) TableName() string { return "report_result_read_leases" }

type ReportAudit struct {
	BaseModel
	ActorType   string    `gorm:"column:actor_type;size:16;not null;default:'USER';index" json:"actorType"`
	ActorUserID uint      `gorm:"column:actor_user_id;not null;index:idx_report_audit_actor_created,priority:1" json:"actorUserId"`
	Action      string    `gorm:"column:action;size:64;not null;index" json:"action"`
	TargetType  string    `gorm:"column:target_type;size:32;not null;index:idx_report_audit_target_created,priority:1" json:"targetType"`
	TargetID    uint      `gorm:"column:target_id;not null;index:idx_report_audit_target_created,priority:2" json:"targetId"`
	RequestID   string    `gorm:"column:request_id;size:128;not null;index" json:"requestId"`
	DetailJSON  JSONText  `gorm:"column:detail_json;type:json" json:"detail"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime;index:idx_report_audit_actor_created,priority:2;index:idx_report_audit_target_created,priority:3" json:"createdAt"`
}

func (ReportAudit) TableName() string { return "report_audits" }
