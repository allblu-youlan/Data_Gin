export type ReportDefinitionStatus = 'DRAFT' | 'ACTIVE' | 'DISABLED'

export type ReportSummary = {
  id: number
  code: string
  name: string
  category: string
  description: string
  datasourceId: number
  status: ReportDefinitionStatus
  currentDraftVersionId: number
  currentPublishedVersionId: number
  lockVersion: number
  isOwner: boolean
  updatedAt: string | null
}

export type ReportCatalogPage = {
  items: ReportSummary[]
  hasMore: boolean
  nextAfterId: number
}

export type ReportCatalogQuery = {
  search?: string
  category?: string
  afterId?: number
  limit?: number
}

export type ReportDatasourceTestStatus = 'SUCCESS' | 'FAILED' | 'NOT_TESTED'

export type ReportDatasource = {
  id: number
  code: string
  name: string
  driver: 'ORACLE'
  host: string
  port: number
  serviceName: string
  sid: string
  username: string
  hasPassword: boolean
  sessionTimezone: string
  connectTimeoutSeconds: number
  queryTimeoutSeconds: number
  maxOpenConnections: number
  maxIdleConnections: number
  prefetchRows: number
  arraySize: number
  enabled: boolean
  lastTestStatus: ReportDatasourceTestStatus
  lastTestError: string
  lastTestedAt: string | null
}

export type ReportDatasourceInput = Omit<ReportDatasource,
  'id' | 'driver' | 'hasPassword' | 'lastTestStatus' | 'lastTestError' | 'lastTestedAt'
> & {
  password: string
}

export type ReportDatasourceTest = {
  status: Exclude<ReportDatasourceTestStatus, 'NOT_TESTED'>
  testedAt: string
  latencyMs: number
  errorCode: string
  message: string
}

export type ReportProcedureRef = {
  owner: string
  package: string
  name: string
  overload: string
}

export type ReportProcedureSummary = ReportProcedureRef & {
  argumentCount: number
  qualifiedName: string
}

export type ReportProcedurePage = {
  items: ReportProcedureSummary[]
  hasMore: boolean
  nextAfter: string
}

export type ReportProcedureArgument = {
  name: string
  position: number
  sequence: number
  direction: string
  oracleType: string
  dataLength: number | null
  precision: number | null
  scale: number | null
  typeOwner: string
  typeName: string
  defaulted: boolean
  supported: boolean
  unsupportedReason: string
  suggestedCode: string
  suggestedLogicalType: string
  suggestedControlType: string
  suggestedSystemValue: string
  role: string
}

export type ReportProcedureSignature = {
  procedure: ReportProcedureSummary
  arguments: ReportProcedureArgument[]
  allSupported: boolean
  protocolReady: boolean
  inputArgName: string
  outputArgName: string
  callTemplate: string
  blockingReasons: string[]
}

export type ReportResultTableRef = {
  owner: string
  name: string
}

export type ReportResultTableSummary = ReportResultTableRef & {
  columnCount: number
  qualifiedName: string
}

export type ReportResultTablePage = {
  items: ReportResultTableSummary[]
  hasMore: boolean
  nextAfter: string
}

export type ReportResultTableColumn = {
  name: string
  position: number
  oracleType: string
  dataLength: number | null
  precision: number | null
  scale: number | null
  nullable: boolean
}

export type ReportResultTableSchema = {
  table: ReportResultTableSummary
  columns: ReportResultTableColumn[]
}

export type ReportExecutionMode = 'TABLE_SNAPSHOT' | 'REF_CURSOR' | 'SELECT_SNAPSHOT'

export type ReportInputType = 'str' | 'number' | 'bool' | 'list[str]' | 'list[number]' | 'list[bool]' | 'json'

export type ReportInputControl = 'TEXT' | 'TEXTAREA' | 'NUMBER' | 'CHECKBOX' | 'DATE' | 'DATETIME' | 'SELECT'

export type ReportInputFormat = 'YYYYMMDD' | 'YYYY-MM-DD' | 'YYYYMMDDHHmmss' | 'YYYY-MM-DD HH:mm:ss' | 'ISO8601'

export type ReportInputField = {
  type: ReportInputType
  displayName: string
	displayOrder?: number
  control: ReportInputControl | ''
  required: boolean
  format?: ReportInputFormat
  example?: unknown
  default?: unknown
  allowedValues?: unknown[]
	queryName?: string
}

export type ReportInputOption = {
	id: string
	name: string
}

export type ReportInputQueryDefinition = {
	id: number
	name: string
	selectSql: string
	enabled: boolean
	lockVersion: number
	lastTestStatus: 'NOT_TESTED' | 'SUCCESS' | 'FAILED'
	lastTestError: string
	lastTestedAt: string | null
	createdAt: string | null
	updatedAt: string | null
}

export type ReportInputQueryDefinitionInput = {
	name: string
	selectSql: string
	enabled: boolean
	expectedLockVersion?: number
}

export type ReportInputQueryTestResult = {
	status: 'SUCCESS' | 'FAILED'
	testedAt: string | null
	latencyMs: number
	rowCount: number
	items: ReportInputOption[]
	errorCode: string
	message: string
}

export type ReportInputSchema = Record<string, ReportInputField>

export type ReportCenterSection = 'catalog' | 'configuration' | 'query' | 'exports'

export type ReportParameter = {
  code: string
  label: string
  displayOrder: number
  controlType: 'TEXT' | 'TEXTAREA' | 'NUMBER' | 'CHECKBOX' | 'DATE' | 'DATETIME' | 'SELECT' | 'MULTI_SELECT'
  logicalType: 'string' | 'integer' | 'decimal' | 'boolean' | 'date' | 'datetime' | 'enum' | 'multi_enum' | 'json'
  cardinality: 'SINGLE' | 'MULTIPLE'
  procedureArgName: string
  position: number
  oracleType: string
  precision: number | null
  scale: number | null
  maxLength: number | null
  required: boolean
  nullable: boolean
  systemInjected: boolean
  sensitive: boolean
  defaultValue: unknown
  allowedValues: string[]
  validation: Record<string, unknown>
  normalizer: Record<string, unknown>
  valueSource: Record<string, unknown>
  timezone: string
  nullPolicy: string
  errorMessage: string
  collectionEncoding?: string
}

export type ReportColumn = {
  source?: ReportColumnSource
  valueMode?: 'DISPLAY' | 'RAW'
  fieldId: string
  logicalCode: string
  databaseColumn: string
  sourceOracleType: string
  precision: number | null
  scale: number | null
  nullable: boolean
  valueType: string
  previewHeader: string
  excelHeader: string
  displayOrder: number
  exportOrder: number
  previewVisible: boolean
  exportVisible: boolean
  filterable: boolean
  sortable: boolean
  exportAllowed: boolean
  allowedOperators: unknown
  format: unknown
  dictionaryVersion: unknown
  maskingPolicy: unknown
  excelWidth: number
  nullDisplay: string
}

export type ReportGrant = { subjectType: 'USER' | 'ROLE'; subjectId: number; actions: string[] }

export type ReportDraft = {
  id: number
  code: string
  name: string
  category: string
  description: string
  datasourceId: number
  status: ReportDefinitionStatus
  lockVersion: number
  executionMode: ReportExecutionMode
  querySpec?: ReportQuerySpec
  executionPolicy?: ReportExecutionPolicy
  procedure: ReportProcedureRef & { jsonInputArgName: string; resultCursorArgName: string }
  inputSchema: ReportInputSchema
  result: { tableOwner: string; tableName: string }
  callTemplate: string
  parameters: ReportParameter[]
  columns: ReportColumn[]
  grants: ReportGrant[]
  createdAt: string | null
  updatedAt: string | null
}

export type ReportValidationSummary = {
  validatedAt: string
  procedure: {
    owner: string
    package: string
    name: string
    overload: string
    argumentCount: number
    signatureHash: string
  }
  result: {
    tableOwner: string
    tableName: string
    columnCount: number
    schemaHash: string
  }
  snapshot: {
    resultTableValidated: boolean
  }
  export: {
    exportableColumnCount: number
    schemaHash: string
  }
}

export type ReportPublication = {
  definitionId: number
  versionId: number
  version: number
  status: string
  contractHash: string
  publishedAt: string | null
  validation: ReportValidationSummary | null
}

export type ReportVersionSummary = { id: number; version: number; status: 'PUBLISHED'; publishedAt: string | null; contractFingerprint: string; parameterCount: number; columnCount: number; grantCount: number }
export type ReportVersionPage = { items: ReportVersionSummary[]; hasMore: boolean; nextAfterId: number }
export type ReportVersionChange = { kind: 'CHANGED'; key: string; label: string; before: string | number; after: string | number }
export type ReportVersionDiffSection = { key: string; label: string; changes: ReportVersionChange[] }
export type ReportVersionDiff = { base: ReportVersionSummary; target: ReportVersionSummary; sections: ReportVersionDiffSection[] }
export type ReportVersionConfiguration = Pick<ReportDraft, 'datasourceId' | 'procedure' | 'executionMode' | 'querySpec' | 'executionPolicy' | 'inputSchema' | 'result' | 'callTemplate' | 'parameters' | 'columns' | 'grants'>
export type ReportVersionDetail = { summary: ReportVersionSummary; configuration: ReportVersionConfiguration }

export type ReportRunContract = {
  executionPolicy?: ReportExecutionPolicy
  allowedFilters?: ReportAllowedFilter[]
  allowedSortFields?: ReportQueryField[]
  relations?: ReportQueryRelation[]
  definitionId: number
  versionId: number
  code: string
  name: string
  description: string
  executionMode: ReportExecutionMode
  jsonInput: boolean
  inputSchema: ReportInputSchema
  parameters: ReportParameter[]
}

export type ReportRunStatus = 'QUEUED' | 'RUNNING' | 'CANCEL_REQUESTED' | 'SUCCEEDED' | 'FAILED' | 'CANCELLED' | 'UNKNOWN' | 'RECONCILING' | 'EXPORTING' | 'EXPORTED' | 'RESULT_PURGING' | 'RESULT_PURGED' | 'SUPERSEDED'

export type ReportRun = {
  id: number
  runUuid: string
  definitionId: number
  versionId: number
  status: ReportRunStatus
  rowCount: number
  cancelRequested: boolean
  createdAt: string | null
  startedAt: string | null
  finishedAt: string | null
  resultExpiresAt: string | null
  errorCode: string
  errorMessage: string
  canCancel: boolean
  resultAvailable: boolean
}

export type ReportResultColumn = {
  relationId?: string
  valueMode?: 'DISPLAY' | 'RAW'
  capabilities?: { canReadRaw: boolean; canViewDetail: boolean }
  fieldId: string
  code: string
  header: string
  valueType: string
  nullable: boolean
  nullDisplay: string
	filterable: boolean
	sortable: boolean
	allowedOperators: ReportFilterOperator[]
}

export type ReportFilterOperator = 'EQ' | 'NE' | 'GT' | 'GTE' | 'LT' | 'LTE' | 'IN' | 'NOT_IN' | 'IS_NULL' | 'IS_NOT_NULL' | 'CONTAINS' | 'STARTS_WITH' | 'BETWEEN'
export type ReportResultFilter = { field: string; operator: ReportFilterOperator; value?: unknown }
export type ReportResultSort = { field: string; direction: 'ASC' | 'DESC' }
export type ReportResultQuery = { filters: ReportResultFilter[]; sort: ReportResultSort[] }

export type ReportResultRow = { key: string; values: Record<string, unknown>; displayValues?: Record<string, unknown>; relationStates?: Record<string, { status: string; canViewDetail: boolean }> }

export type ReportResultPage = {
  run: ReportRun
  columns: ReportResultColumn[]
  rows: ReportResultRow[]
  pagination: { pageSize: number; hasMore: boolean; nextCursor: string }
}

export type ReportExportStatus = 'PENDING' | 'RUNNING' | 'READY' | 'FAILED' | 'CANCELLED' | 'EXPIRED'

export type ReportExport = {
  id: number
  exportUuid: string
  runId: number
  status: ReportExportStatus
  processedRows: number
  exportedRows: number
  currentSheet: string
  sheetCount: number
  truncatedCellCount: number
  fileSizeBytes: number
  createdAt: string | null
  startedAt: string | null
  readyAt: string | null
  expiresAt: string | null
  purgedAt: string | null
  errorCode: string
  errorMessage: string
  canDownload: boolean
	reportName: string
	purgedRows: number
	purgeStartedAt: string | null
}

export type ReportExportPage = { items: ReportExport[]; hasMore: boolean; nextAfterId: number }

export type ReportAudit = {
  id: number
  actorType: 'USER' | 'SYSTEM'
  actorUserId: number
  action: string
  targetType: string
  targetId: number
  requestId: string
  detail: Record<string, unknown>
  createdAt: string
}

export type ReportAuditQuery = {
  action?: string
  targetType?: string
  targetId?: number
  afterId?: number
  limit?: number
}

export type ReportAuditPage = {
  items: ReportAudit[]
  hasMore: boolean
  nextAfterId: number
}

export type ReportColumnSource =
  | { kind: 'BASE_COLUMN'; columnId: string }
  | { kind: 'RELATION_DISPLAY'; relationId: string }
  | { kind: 'RELATION_COLUMN'; relationId: string; columnId: string }

export type ReportQueryOperator = 'EQ' | 'NE' | 'GT' | 'GTE' | 'LT' | 'LTE' | 'BETWEEN' | 'IN' | 'NOT_IN' | 'PREFIX' | 'CONTAINS' | 'IS_NULL' | 'IS_NOT_NULL'
export type ReportQueryField = { aliasId: string; columnId: string }
export type ReportQueryValue = { kind: 'CONSTANT'; value: unknown } | { kind: 'PARAMETER'; code: string } | { kind: 'FIELD'; aliasId: string; columnId: string }
export type ReportQueryCondition = ReportQueryField & { operator: ReportQueryOperator; value?: ReportQueryValue }
export type ReportQueryConditionGroup = { logic: 'AND' | 'OR'; items: Array<ReportQueryCondition | ReportQueryConditionGroup> }
export type ReportQueryOrder = ReportQueryField & { direction: 'ASC' | 'DESC' }
export type ReportAllowedFilter = ReportQueryField & { operators: ReportQueryOperator[]; label?: string; logicalType?: string }
export type ReportQueryRelation = {
  id: string
  name: string
  fromAliasId: string
  targetTableId: string
  keyBindings: Array<{ sourceColumnId: string; targetColumnId: string }>
  cardinality: 'MANY_TO_ONE' | 'ONE_TO_ONE'
  joinType: 'LEFT'
  displayColumnIds: string[]
  detailColumnIds: string[]
  matchConditions: ReportQueryConditionGroup
}
export type ReportQueryColumn = {
  fieldId: string
  code?: string
  source: ReportColumnSource
  header: string
  valueMode?: 'DISPLAY' | 'RAW'
  valueType?: string
  previewVisible?: boolean
  exportVisible?: boolean
}
export type ReportQuerySpec = {
  version: 1
  base: { tableId: string; aliasId: string }
  relations: ReportQueryRelation[]
  columns: ReportQueryColumn[]
  where: ReportQueryConditionGroup
  orderBy: ReportQueryOrder[]
  allowedFilters?: ReportAllowedFilter[]
  allowedSortFields?: ReportQueryField[]
}
export type ReportExecutionPolicy = { exportPolicy: 'MANUAL' | 'AUTO'; resultRetentionHours: number; purgeAfterExport: boolean }
export type ReportQueryInputs = { where?: ReportQueryConditionGroup; orderBy?: ReportQueryOrder[] }
export type BojunDataScope = { mode: 'GLOBAL_APPROVED' | 'SCOPE_COLUMN'; columnId?: string; values?: string[] }
export type BojunTable = {
  id: number
  bojunTableId: string
  logicalName: string
  description: string
  resolvedTable: string
  primaryKeys: string[]
  displayFields: string[]
  searchFields: string[]
  enabled: boolean
  validationStatus: string
  validationReason: string
  lastSyncedAt: string | null
  lockVersion: number
  dataScope?: BojunDataScope
}
export type BojunColumn = {
  id: number
  bojunColumnId: string
  name: string
  dbName: string
  description: string
  oracleType: string
  logicalType: string
  nullable: boolean
  isPhysical: boolean
  isSelectable: boolean
  refTableId: string
  refColumnId: string
  supportedOperators: ReportQueryOperator[]
  ordinal: number
}
export type BojunPage<T> = { items: T[]; hasMore: boolean; nextAfter: string; generation: string }
export type BojunSyncRun = { id: number; syncRunId: number; status: string; tableCount: number; columnCount: number; error: string; startedAt: string | null; finishedAt: string | null }
export type ReportQueryIssue = { fieldPath: string; message: string; code: string }
export type ReportQueryValidation = { valid: boolean; errors: ReportQueryIssue[]; warnings: ReportQueryIssue[] }
export type ReportQueryPreview = { sampleOnly: boolean; elapsedMs: number; columns: ReportResultColumn[]; rows: ReportResultRow[] }
export type ReportRecordDetail = {
  mode: 'SNAPSHOT' | 'LIVE'
  detailSource: string
  queriedAt: string | null
  relationName: string
  fields: Array<{ columnId: string; name: string; label: string; rawValue?: unknown; displayValue?: unknown }>
}
export type ReportRelationOption = { keyValues: Record<string, string>; displayValue: string }
export type ReportRelationOptions = { items: ReportRelationOption[]; hasMore: boolean; nextCursor: string }
