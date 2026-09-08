import type {
  OfficeColumnMapping,
  OfficeColumnValueType,
  OfficeFeishuBot,
  OfficeMessage,
  OfficeMessageDraft,
  OfficeMessageSourceType,
  OfficeParameterType,
  OfficeProcedureSummary,
  OfficePushRun,
  OfficePushRunStatus,
  OfficePushSchedule,
  OfficeScheduleParameter,
  OfficePushTarget,
  OfficeQueryParameter,
  OfficeResultColumn,
  OfficeResultTableSummary,
  OfficeSelectColumn,
} from './types'

type RecordValue = Record<string, unknown>

export function unwrapOfficeData(payload: unknown): RecordValue {
  const root = record(payload)
  return record(root.data ?? root)
}

export function parseOfficeMessages(payload: unknown): OfficeMessage[] {
  return items(payload).map(parseOfficeMessage)
}

export function parseOfficeMessage(payload: unknown): OfficeMessage {
  const value = unwrapOfficeData(payload)
  const sourceType = enumValue(value.sourceType, ['EDITED', 'ORACLE_PROCEDURE', 'ORACLE_QUERY'] as const)
  const name = text(value.name)
  return {
    id: positiveInteger(value.id), name, sourceType,
    content: text(value.content), procedureOwner: text(value.procedureOwner), packageName: text(value.packageName),
    procedureName: text(value.procedureName), procedureOverload: text(value.procedureOverload),
    resultTableOwner: text(value.resultTableOwner), resultTableName: text(value.resultTableName), selectSql: text(value.selectSql),
    fileNameTemplate: text(value.fileNameTemplate) || (sourceType === 'EDITED' ? '' : legacyOfficeWorkbookFileName(name)),
    parameters: array(value.parameters).map(parseParameter), columnMapping: array(value.columnMapping).map(parseMapping),
    enabled: booleanValue(value.enabled), lockVersion: positiveInteger(value.lockVersion), updatedAt: text(value.updatedAt),
  }
}

export function parseOfficeTargets(payload: unknown): OfficePushTarget[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    if ('webdavPassword' in value || 'webdavPasswordCiphertext' in value || 'credentialKeyVersion' in value) throw new Error('WebDAV 凭据不应返回浏览器')
    const channel = enumValue(value.channel || 'FEISHU', ['FEISHU', 'WEBDAV'] as const)
    return {
      id: positiveInteger(value.id), name: text(value.name), messageId: positiveInteger(value.messageId), channel,
      botAppId: text(value.botAppId),
      receiveIdType: channel === 'FEISHU' ? enumValue(value.receiveIdType, ['chat_id', 'open_id', 'user_id', 'union_id', 'email'] as const) : '',
      receiveId: text(value.receiveId), webdavUrl: text(value.webdavUrl), webdavUsername: text(value.webdavUsername),
      webdavPath: text(value.webdavPath), hasWebdavPassword: optionalBooleanValue(value.hasWebdavPassword),
      enabled: booleanValue(value.enabled), lockVersion: positiveInteger(value.lockVersion), updatedAt: text(value.updatedAt),
    }
  })
}

export function parseOfficeFeishuBots(payload: unknown): OfficeFeishuBot[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    return {
      id: text(value.id),
      name: text(value.name),
      source: enumValue(value.source, ['ENVIRONMENT'] as const),
    }
  })
}

export function parseOfficeRuns(payload: unknown): OfficePushRun[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    return {
      id: positiveInteger(value.id), runUuid: text(value.runUuid), targetId: positiveInteger(value.targetId), messageId: positiveInteger(value.messageId),
      status: enumValue(value.status, ['QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'UNKNOWN'] as const) as OfficePushRunStatus,
      attemptCount: nonNegativeInteger(value.attemptCount), rowCount: nonNegativeInteger(value.rowCount), errorMessage: text(value.errorMessage),
      triggerType: enumValue(value.triggerType, ['MANUAL', 'SCHEDULE'] as const), scheduleId: optionalNonNegativeInteger(value.scheduleId),
      scheduledFor: text(value.scheduledFor),
      createdAt: text(value.createdAt), finishedAt: text(value.finishedAt),
    }
  })
}

export function parseOfficeSchedules(payload: unknown): OfficePushSchedule[] {
  return items(payload).map(parseOfficeScheduleValue)
}

export function parseOfficeSchedule(payload: unknown): OfficePushSchedule {
  return parseOfficeScheduleValue(unwrapOfficeData(payload))
}

function parseOfficeScheduleValue(raw: unknown): OfficePushSchedule {
  const value = record(raw)
  return {
    id: positiveInteger(value.id), name: text(value.name), targetId: positiveInteger(value.targetId), cronExpr: text(value.cronExpr),
    timeZone: enumValue(value.timeZone, ['Asia/Shanghai'] as const), parameters: parseScheduleParameters(value.parameters),
    enabled: booleanValue(value.enabled), nextRunAt: text(value.nextRunAt), lastScheduledAt: text(value.lastScheduledAt),
    lastError: text(value.lastError), lockVersion: nonNegativeInteger(value.lockVersion), updatedAt: text(value.updatedAt),
  }
}

function parseScheduleParameters(raw: unknown): Record<string, OfficeScheduleParameter> {
  const values = record(raw)
  return Object.fromEntries(Object.entries(values).map(([code, parameter]) => {
    const value = record(parameter)
    return [code, {
      mode: enumValue(value.mode, ['LITERAL', 'SCHEDULED_DATE'] as const), value: text(value.value), offsetDays: optionalInteger(value.offsetDays),
    }]
  }))
}

export function parseProcedureSummaries(payload: unknown): OfficeProcedureSummary[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    return { owner: text(value.owner), packageName: text(value.packageName), name: text(value.name), overload: text(value.overload), argumentCount: nonNegativeInteger(value.argumentCount) }
  })
}

export function parseResultTableSummaries(payload: unknown): OfficeResultTableSummary[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    return { owner: text(value.owner), name: text(value.name), columnCount: nonNegativeInteger(value.columnCount) }
  })
}

export function parseResultColumns(payload: unknown): OfficeResultColumn[] {
  return items(payload).map((raw) => {
    const value = record(raw)
    return { name: text(value.name), position: positiveInteger(value.position), dataType: text(value.dataType), nullable: booleanValue(value.nullable) }
  })
}

export function parseSelectColumns(payload: unknown): OfficeSelectColumn[] {
  return array(unwrapOfficeData(payload).columns).map((raw) => {
    const value = record(raw)
    return { name: text(value.name), databaseType: text(value.databaseType), nullable: booleanValue(value.nullable) }
  })
}

export function emptyOfficeMessageDraft(): OfficeMessageDraft {
  return {
    id: null, name: '', sourceType: 'EDITED', content: '', procedureOwner: '', packageName: '', procedureName: '', procedureOverload: '',
    resultTableOwner: '', resultTableName: '', selectSql: 'SELECT\n  \nFROM\n  \nWHERE 1 = 1', parameters: [], columnMapping: [],
    fileNameTemplate: '办公消息_{{date:yyyyMMdd}}.xlsx',
    enabled: true, lockVersion: 0,
  }
}

export function officeMessageDraftFrom(message: OfficeMessage): OfficeMessageDraft {
  return {
    id: message.id, name: message.name, sourceType: message.sourceType, content: message.content,
    procedureOwner: message.procedureOwner, packageName: message.packageName, procedureName: message.procedureName,
    procedureOverload: message.procedureOverload, resultTableOwner: message.resultTableOwner, resultTableName: message.resultTableName,
    selectSql: message.selectSql, fileNameTemplate: message.fileNameTemplate, parameters: message.parameters, columnMapping: message.columnMapping,
    enabled: message.enabled, lockVersion: message.lockVersion,
  }
}

export function buildOfficeMessagePayload(draft: OfficeMessageDraft) {
  const name = draft.name.trim()
  if (!name) throw new Error('请填写消息名称。')
  if (draft.sourceType === 'EDITED' && !draft.content.trim()) throw new Error('请填写消息正文。')
  if (draft.sourceType === 'ORACLE_PROCEDURE' && (!draft.procedureOwner.trim() || !draft.procedureName.trim() || !draft.resultTableOwner.trim() || !draft.resultTableName.trim())) throw new Error('请完整配置存储过程和结果表。')
  if (draft.sourceType === 'ORACLE_QUERY' && !draft.selectSql.trim()) throw new Error('请填写 SELECT 语句。')
  if (draft.sourceType !== 'EDITED' && draft.columnMapping.length === 0) throw new Error('请至少配置一个导出列。')
  const fileNameTemplate = draft.fileNameTemplate.trim()
  if (draft.sourceType !== 'EDITED' && !validOfficeWorkbookFileNameTemplate(fileNameTemplate)) throw new Error('请填写有效的 Excel 文件名模板。')
  if (draft.sourceType === 'ORACLE_QUERY') validateQueryParameters(draft.selectSql, draft.parameters)
  return {
    name, sourceType: draft.sourceType, content: draft.sourceType === 'EDITED' ? draft.content.trim() : '',
    procedureOwner: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.procedureOwner.trim() : '', packageName: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.packageName.trim() : '',
    procedureName: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.procedureName.trim() : '', procedureOverload: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.procedureOverload.trim() : '',
    resultTableOwner: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.resultTableOwner.trim() : '', resultTableName: draft.sourceType === 'ORACLE_PROCEDURE' ? draft.resultTableName.trim() : '',
    selectSql: draft.sourceType === 'ORACLE_QUERY' ? draft.selectSql.trim() : '', fileNameTemplate: draft.sourceType === 'EDITED' ? '' : fileNameTemplate,
    parameters: draft.sourceType === 'ORACLE_QUERY' ? draft.parameters : [],
    columnMapping: draft.sourceType === 'EDITED' ? [] : draft.columnMapping.map((mapping, order) => ({ ...mapping, sourceColumn: mapping.sourceColumn.trim().toUpperCase(), header: mapping.header.trim(), order })),
    enabled: draft.enabled, expectedLockVersion: draft.id ? draft.lockVersion : 0,
  }
}

function validOfficeWorkbookFileNameTemplate(value: string) {
  if (!value || new TextEncoder().encode(value).length > 255 || Array.from(value).some(isUnsafeWorkbookFileNameCharacter) || !value.toLowerCase().endsWith('.xlsx')) return false
  const remaining = value.replace(/\{\{date(?::(?:yyyyMMdd|yyyy-MM-dd))?\}\}/g, '')
  return !remaining.includes('{{') && !remaining.includes('}}')
}

function legacyOfficeWorkbookFileName(name: string) {
  const sanitized = Array.from(name.trim()).map((character) => isUnsafeWorkbookFileNameCharacter(character) ? '-' : character).join('') || '办公消息'
  return `${Array.from(sanitized).slice(0, 80).join('')}.xlsx`
}

function isUnsafeWorkbookFileNameCharacter(character: string) {
  const codePoint = character.codePointAt(0) ?? 0
  return character === '/' || character === '\\' || codePoint <= 31 || (codePoint >= 127 && codePoint <= 159)
}

export function mappingsFromColumns(columns: Array<{ name: string; dataType?: string }>): OfficeColumnMapping[] {
  return columns.map((column, order) => ({ sourceColumn: column.name.toUpperCase(), header: column.name, valueType: oracleValueType(column.dataType ?? ''), order, width: 18 }))
}

function validateQueryParameters(sql: string, parameters: OfficeQueryParameter[]) {
  const configured = new Set<string>()
  for (const parameter of parameters) {
    const code = parameter.code.trim().toLowerCase()
    if (!/^[a-z][a-z0-9_]{0,63}$/.test(code) || !parameter.label.trim()) throw new Error('SELECT 参数名或显示名称无效。')
    if (configured.has(code)) throw new Error(`SELECT 参数 ${code} 重复。`)
    configured.add(code)
    if (parameter.valueType === 'date' && !parameter.format) throw new Error(`请选择参数 ${code} 的日期格式。`)
  }
  const bound = queryBindNames(sql)
  if (bound.size !== configured.size || [...bound].some((code) => !configured.has(code))) throw new Error('SELECT 中的命名绑定与参数配置不一致。')
}

function queryBindNames(sql: string) {
  const bound = new Set<string>()
  let inString = false
  for (let index = 0; index < sql.length; index += 1) {
    const character = sql[index]
    if (character === "'") {
      if (inString && sql[index + 1] === "'") { index += 1; continue }
      inString = !inString
      continue
    }
    if (inString || character !== ':') continue
    const match = sql.slice(index + 1).match(/^[A-Za-z][A-Za-z0-9_]*/)
    if (match) {
      bound.add(match[0].toLowerCase())
      index += match[0].length
    }
  }
  return bound
}

function parseParameter(raw: unknown): OfficeQueryParameter {
  const value = record(raw)
  const valueType = enumValue(value.valueType, ['string', 'integer', 'decimal', 'date'] as const) as OfficeParameterType
  const format = valueType === 'date' ? enumValue(value.format, ['yyyyMMdd', 'yyyy-MM-dd', 'yyyy-MM-dd HH:mm:ss'] as const) : undefined
  return { code: text(value.code), label: text(value.label), valueType, format, required: booleanValue(value.required) }
}

function parseMapping(raw: unknown): OfficeColumnMapping {
  const value = record(raw)
  return { sourceColumn: text(value.sourceColumn), header: text(value.header), valueType: enumValue(value.valueType, ['string', 'integer', 'decimal', 'date', 'datetime', 'boolean'] as const) as OfficeColumnValueType, order: nonNegativeInteger(value.order), width: numberValue(value.width) }
}

function oracleValueType(dataType: string): OfficeColumnValueType {
  const value = dataType.toUpperCase()
  if (value.includes('DATE') || value.includes('TIMESTAMP')) return value.includes('TIMESTAMP') ? 'datetime' : 'date'
  if (value.includes('NUMBER') || value.includes('DECIMAL') || value.includes('FLOAT')) return 'decimal'
  return 'string'
}

function items(payload: unknown) { return array(unwrapOfficeData(payload).items) }
function record(value: unknown): RecordValue { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('办公消息响应格式无效'); return value as RecordValue }
function array(value: unknown): unknown[] { if (!Array.isArray(value)) throw new Error('办公消息列表格式无效'); return value }
function text(value: unknown): string { return typeof value === 'string' ? value : '' }
function booleanValue(value: unknown): boolean { if (typeof value !== 'boolean') throw new Error('办公消息布尔字段无效'); return value }
function optionalBooleanValue(value: unknown): boolean { return value === undefined ? false : booleanValue(value) }
function numberValue(value: unknown): number { const parsed = Number(value); if (!Number.isFinite(parsed) || parsed < 0) throw new Error('办公消息数字字段无效'); return parsed }
function positiveInteger(value: unknown): number { const parsed = numberValue(value); if (!Number.isInteger(parsed) || parsed <= 0) throw new Error('办公消息 ID 无效'); return parsed }
function nonNegativeInteger(value: unknown): number { const parsed = numberValue(value); if (!Number.isInteger(parsed)) throw new Error('办公消息数字字段无效'); return parsed }
function integer(value: unknown): number { const parsed = Number(value); if (!Number.isInteger(parsed)) throw new Error('办公消息数字字段无效'); return parsed }
function optionalInteger(value: unknown): number { return value === undefined || value === null ? 0 : integer(value) }
function optionalNonNegativeInteger(value: unknown): number { return value === undefined || value === null ? 0 : nonNegativeInteger(value) }
function enumValue<const T extends readonly string[]>(value: unknown, allowed: T): T[number] { if (typeof value !== 'string' || !allowed.includes(value)) throw new Error('办公消息枚举字段无效'); return value as T[number] }

export const officeSourceLabels: Record<OfficeMessageSourceType, string> = { EDITED: '自编辑消息', ORACLE_PROCEDURE: '存储过程结果 Excel', ORACLE_QUERY: 'SELECT 结果 Excel' }
