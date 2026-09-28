export type ExcelMatchFilterConfig = {
  column: string
  op: string
  value: string
}

export type ExcelEmptyCellFillConfig = {
  targetColumn: string
  sourceColumn: string
}

export type ExcelImportWriteMappingConfig = {
  dbWriteField: string
  writeExcelColumn: string
}

export type ExcelMatchMode = 'field' | 'order_item_sku' | 'conditional_write'

export type ExcelMatchStepConfig = {
  name: string
  filters: ExcelMatchFilterConfig[]
  matchMode: ExcelMatchMode
  tableName: string
  matchExcelColumn: string
  dbMatchField: string
  dbValueField: string
  outputColumnName: string
  containsValue: string
  writeValue: string
  specExcelColumn: string
  priceExcelColumn: string
  qtyExcelColumn: string
}

export type ExcelMatchModelField = {
  name: string
  modelField: string
  columnName: string
  dataType: string
  description: string
  mapping: string
  nullable: boolean
}

export type ExcelMatchModel = {
  name: string
  modelName: string
  tableName: string
  description: string
  mapping: string
  fields: ExcelMatchModelField[]
}

export type ExcelCatalogSelectOption = {
  value: string
  label: string
  currentOnly?: boolean
}

export const bojunImportWriteFieldOptions = [
  { value: 'matched_docno', label: '匹配单号 matched_docno' },
  { value: 'completed_at', label: '订单完成时间 completed_at' },
  { value: 'order_phone', label: '订单手机号 order_phone' },
  { value: 'paid_amount', label: '实付金额 paid_amount' },
  { value: 'push_amount', label: '推送金额 push_amount' },
  { value: 'is_to_shop', label: '是否到店 is_to_shop' },
  { value: 'oracle_retail_id', label: 'Oracle 零售单 ID oracle_retail_id' },
] as const

export function bojunImportWriteConfirmation(writeFields: string | string[]) {
  const descriptions: Record<string, string> = {
    matched_docno: '空的 matched_docno，不会覆盖已有匹配单号',
    completed_at: '为空的 completed_at，值必须使用 yyyy-mm-dd hh:mm:ss 格式',
    oracle_retail_id: '为空且到店状态有效的 oracle_retail_id，值必须为正整数',
    order_phone: '为空的 order_phone',
    paid_amount: '当前为 0 的 paid_amount，值必须为数字',
    push_amount: '当前为 0 的 push_amount，值必须为数字',
    is_to_shop: '为空的 is_to_shop，值必须为 Y 或 N',
  }
  const fields = Array.isArray(writeFields) ? writeFields : [writeFields]
  const details = fields.map((field) => descriptions[field] ?? `${field || '目标'}的空字段`)
  return `确认写入数据库？本次会按列填充：${details.join('；')}。不会覆盖已有值。`
}

type ExcelMatchSchemeSource = {
  filters?: Array<Partial<ExcelMatchFilterConfig>>
  matchExcelColumn?: string
  dbMatchField?: string
  dbValueField?: string
  tableName?: string
  outputColumnName?: string
  steps?: Array<Partial<ExcelMatchStepConfig>>
  emptyCellFills?: Array<Partial<ExcelEmptyCellFillConfig>>
}

type ExcelImportSchemeSource = {
  dbWriteField?: string
  writeExcelColumn?: string
  writeMappings?: Array<Partial<ExcelImportWriteMappingConfig>>
}

function normalizeMatchMode(value: unknown): ExcelMatchMode {
  return value === 'order_item_sku' || value === 'conditional_write' ? value : 'field'
}

export function isExcelMatchStepComplete(step: ExcelMatchStepConfig) {
  if (!step.name.trim() || !step.matchExcelColumn.trim() || !step.outputColumnName.trim()) return false
  if (step.matchMode === 'conditional_write') return Boolean(step.containsValue.trim())
  if (!step.tableName.trim() || !step.dbMatchField.trim() || !step.dbValueField.trim()) return false
  return step.matchMode !== 'order_item_sku' || Boolean(step.specExcelColumn.trim() && step.priceExcelColumn.trim() && step.qtyExcelColumn.trim())
}

type ExcelExportConfigInput = {
  sheetName: string
  steps: ExcelMatchStepConfig[]
  emptyCellFills: ExcelEmptyCellFillConfig[]
  exportColumnFormats: Array<{ column: string; format: string }>
  batchSize: number
}

function cloneFilters(filters: Array<Partial<ExcelMatchFilterConfig>> | undefined) {
  if (!Array.isArray(filters)) return []
  return filters.map((filter) => ({
    column: filter.column ?? '',
    op: filter.op ?? 'eq',
    value: filter.value ?? '',
  }))
}

export function cloneExcelMatchSteps(steps: ExcelMatchStepConfig[]) {
  return steps.map((step) => ({
    ...step,
    filters: cloneFilters(step.filters),
  }))
}

export function cloneExcelEmptyCellFills(fills: Array<Partial<ExcelEmptyCellFillConfig>> | undefined): ExcelEmptyCellFillConfig[] {
  if (!Array.isArray(fills)) return []
  return fills.map((fill) => ({
    targetColumn: fill.targetColumn ?? '',
    sourceColumn: fill.sourceColumn ?? '',
  }))
}

export function cloneExcelImportWriteMappings(mappings: Array<Partial<ExcelImportWriteMappingConfig>> | undefined): ExcelImportWriteMappingConfig[] {
  if (!Array.isArray(mappings)) return []
  return mappings.map((mapping) => ({
    dbWriteField: mapping.dbWriteField ?? '',
    writeExcelColumn: mapping.writeExcelColumn ?? '',
  }))
}

export function migrateExcelImportWriteMappings(config: ExcelImportSchemeSource, fallback: ExcelImportWriteMappingConfig): ExcelImportWriteMappingConfig[] {
  if (Array.isArray(config.writeMappings) && config.writeMappings.length > 0) {
    return cloneExcelImportWriteMappings(config.writeMappings)
  }
  return [{
    dbWriteField: config.dbWriteField || fallback.dbWriteField,
    writeExcelColumn: config.writeExcelColumn || fallback.writeExcelColumn,
  }]
}

export function excelMatchSchemePath(schemeID: number) {
  if (!Number.isSafeInteger(schemeID) || schemeID <= 0) {
    throw new RangeError('Excel 匹配方案 ID 必须是正整数')
  }
  return `/v1/excel-match-jobs/schemes/${schemeID}`
}

export function excelModelSelectOptions(models: ExcelMatchModel[], currentValue: string): ExcelCatalogSelectOption[] {
  const options: ExcelCatalogSelectOption[] = models.map((model) => ({
    value: model.tableName,
    label: `${model.name}（${model.modelName} → ${model.tableName}）`,
  }))
  const current = currentValue.trim()
  if (current && !models.some((model) => model.tableName === current)) {
    options.unshift({
      value: current,
      label: `当前配置：${current}（目录中不存在）`,
      currentOnly: true,
    })
  }
  return options
}

export function excelFieldSelectOptions(fields: ExcelMatchModelField[], currentValue: string): ExcelCatalogSelectOption[] {
  const options: ExcelCatalogSelectOption[] = fields.map((field) => ({
    value: field.columnName,
    label: `${field.name}（${field.modelField} → ${field.columnName}）`,
  }))
  const current = currentValue.trim()
  if (current && !fields.some((field) => field.columnName === current)) {
    options.unshift({
      value: current,
      label: `当前配置：${current}（模型中不存在）`,
      currentOnly: true,
    })
  }
  return options
}

export function selectExcelMatchStepModel(step: ExcelMatchStepConfig, tableName: string, models: ExcelMatchModel[]): ExcelMatchStepConfig {
  const model = models.find((item) => item.tableName === tableName)
  if (!model) {
    return { ...step, tableName }
  }
  const columns = new Set(model.fields.map((field) => field.columnName))
  return {
    ...step,
    tableName,
    dbMatchField: columns.has(step.dbMatchField) ? step.dbMatchField : '',
    dbValueField: columns.has(step.dbValueField) ? step.dbValueField : '',
  }
}

export function migrateExcelMatchSteps(config: ExcelMatchSchemeSource, fallbackStep: ExcelMatchStepConfig): ExcelMatchStepConfig[] {
  const configuredSteps: ExcelMatchStepConfig[] = Array.isArray(config.steps) && config.steps.length > 0
    ? config.steps.map((step, index): ExcelMatchStepConfig => ({
        name: step.name ?? `步骤 ${index + 1}`,
        filters: cloneFilters(step.filters),
        matchMode: normalizeMatchMode(step.matchMode),
        tableName: step.tableName ?? '',
        matchExcelColumn: step.matchExcelColumn ?? '',
        dbMatchField: step.dbMatchField ?? '',
        dbValueField: step.dbValueField ?? '',
        outputColumnName: step.outputColumnName ?? '',
        containsValue: step.containsValue ?? '',
        writeValue: step.writeValue ?? '',
        specExcelColumn: step.specExcelColumn ?? '',
        priceExcelColumn: step.priceExcelColumn ?? '',
        qtyExcelColumn: step.qtyExcelColumn ?? '',
      }))
    : [{
        name: '兼容旧方案',
        filters: [],
        matchMode: 'field',
        tableName: config.tableName || fallbackStep.tableName,
        matchExcelColumn: config.matchExcelColumn || fallbackStep.matchExcelColumn,
        dbMatchField: config.dbMatchField || fallbackStep.dbMatchField,
        dbValueField: config.dbValueField || fallbackStep.dbValueField,
        outputColumnName: config.outputColumnName || fallbackStep.outputColumnName,
        containsValue: '',
        writeValue: '',
        specExcelColumn: '',
        priceExcelColumn: '',
        qtyExcelColumn: '',
      }]
  const legacyFilters = cloneFilters(config.filters)
  if (legacyFilters.length > 0) {
    configuredSteps[0].filters = [...legacyFilters, ...configuredSteps[0].filters]
  }
  return configuredSteps
}

export function buildExcelExportConfig(input: ExcelExportConfigInput) {
  return {
    operation: 'export_match' as const,
    sheetName: input.sheetName.trim() || 'Sheet1',
    steps: input.steps.map((step) => ({
      name: step.name.trim(),
      matchMode: normalizeMatchMode(step.matchMode),
      filters: step.filters
        .map((filter) => {
          const op = filter.op.trim().toLowerCase() || 'eq'
          return {
            column: filter.column.trim(),
            op,
            value: op === 'empty' || op === 'not_empty' ? '' : filter.value.trim(),
          }
        })
        .filter((filter) => filter.column),
      tableName: step.tableName.trim(),
      matchExcelColumn: step.matchExcelColumn.trim(),
      dbMatchField: step.dbMatchField.trim(),
      dbValueField: step.dbValueField.trim(),
      outputColumnName: step.outputColumnName.trim(),
      containsValue: step.containsValue.trim(),
      writeValue: step.writeValue,
      specExcelColumn: step.specExcelColumn.trim(),
      priceExcelColumn: step.priceExcelColumn.trim(),
      qtyExcelColumn: step.qtyExcelColumn.trim(),
    })),
    emptyCellFills: cloneExcelEmptyCellFills(input.emptyCellFills)
      .map((fill) => ({
        targetColumn: fill.targetColumn.trim(),
        sourceColumn: fill.sourceColumn.trim(),
      }))
      .filter((fill) => fill.targetColumn || fill.sourceColumn),
    exportColumnFormats: input.exportColumnFormats,
    batchSize: input.batchSize,
  }
}
