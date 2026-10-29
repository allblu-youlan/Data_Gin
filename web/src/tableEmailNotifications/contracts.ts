import type { NotificationColumn, NotificationDatasource, NotificationDelivery, NotificationDraft, NotificationPreview, NotificationRule, NotificationTable } from './types'

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('邮件提醒响应格式无效。')
  return value as Record<string, unknown>
}
function data(value: unknown) { const root = object(value); return object(root.data ?? root) }
function items(value: unknown) {
  const list = data(value).items
  if (!Array.isArray(list)) throw new Error('邮件提醒列表格式无效。')
  return list.map(object)
}
function text(value: unknown) { return typeof value === 'string' ? value : '' }
function integer(value: unknown, minimum = 0) {
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < minimum) throw new Error('邮件提醒数字字段无效。')
  return number
}
function bool(value: unknown) {
  if (typeof value !== 'boolean') throw new Error('邮件提醒状态格式无效。')
  return value
}

export function parseNotificationDatasources(value: unknown): NotificationDatasource[] {
  return items(value).map((row) => ({ id: integer(row.id), name: text(row.name) }))
}
export function parseNotificationTables(value: unknown): NotificationTable[] {
  return items(value).map((row) => ({ owner: text(row.owner), name: text(row.name), columnCount: integer(row.columnCount) }))
}
export function parseNotificationColumns(value: unknown): NotificationColumn[] {
  return items(value).map((row) => ({ name: text(row.name), comment: text(row.comment), dataType: text(row.dataType), nullable: bool(row.nullable), position: integer(row.position, 1) }))
}
export function parseNotificationRules(value: unknown): NotificationRule[] {
  return items(value).map((row) => ({
    id: integer(row.id, 1), name: text(row.name), datasourceId: integer(row.datasourceId), owner: text(row.owner), tableName: text(row.tableName),
    cursorColumn: text(row.cursorColumn), recipientsTemplate: text(row.recipientsTemplate), subjectTemplate: text(row.subjectTemplate), bodyTemplate: text(row.bodyTemplate),
    enabled: bool(row.enabled), pollIntervalSeconds: integer(row.pollIntervalSeconds, 60), lockVersion: integer(row.lockVersion), cursorValue: text(row.cursorValue),
    lastCheckedAt: text(row.lastCheckedAt) || null, lastError: text(row.lastError), createdAt: text(row.createdAt), updatedAt: text(row.updatedAt),
  }))
}
function preview(row: Record<string, unknown>): NotificationPreview {
  if (!Array.isArray(row.recipients) || row.recipients.some((value) => typeof value !== 'string')) throw new Error('邮件收件人格式无效。')
  return { recipients: row.recipients as string[], subject: text(row.subject), body: text(row.body) }
}
export function parseNotificationPreview(value: unknown) { return preview(data(value)) }
export function parseNotificationDeliveries(value: unknown): NotificationDelivery[] {
  return items(value).map((row) => {
    if (!['queued', 'retrying', 'sent', 'failed', 'cancelled'].includes(text(row.status))) throw new Error('邮件发送状态无效。')
    return {
      ...preview(row), id: integer(row.id, 1), ruleId: integer(row.ruleId, 1), sourceValue: text(row.sourceValue), status: row.status as NotificationDelivery['status'],
      attempts: integer(row.attempts), lastError: text(row.lastError), createdAt: text(row.createdAt), sentAt: text(row.sentAt) || null,
    }
  })
}

export function emptyNotificationDraft(): NotificationDraft {
  return { id: null, name: '', datasourceId: 0, owner: '', tableName: '', cursorColumn: '', recipientsTemplate: '', subjectTemplate: '新增数据提醒', bodyTemplate: '', enabled: true, pollIntervalSeconds: 60, lockVersion: 0 }
}

export function notificationEnabledPayload(rule: NotificationRule, enabled: boolean) {
  return {
    name: rule.name, datasourceId: rule.datasourceId, owner: rule.owner, tableName: rule.tableName, cursorColumn: rule.cursorColumn,
    recipientsTemplate: rule.recipientsTemplate, subjectTemplate: rule.subjectTemplate, bodyTemplate: rule.bodyTemplate,
    enabled, pollIntervalSeconds: rule.pollIntervalSeconds, lockVersion: rule.lockVersion,
  }
}

export function isCursorColumn(column: NotificationColumn) {
  return !column.nullable && /^NUMBER(?:\(\d+(?:,\s*0)?\))?$/i.test(column.dataType.trim())
}

export function validateTemplateFields(template: string, columns: NotificationColumn[]) {
  const available = new Set(columns.map((column) => column.name.toUpperCase()))
  const remainder = template.replace(/\{\{\s*([A-Za-z][A-Za-z0-9_$#]*)\s*\}\}/g, (_match, name: string) => {
    if (!available.has(name.toUpperCase())) throw new Error(`模板字段 ${name} 不在当前表中，请重新选择字段。`)
    return ''
  })
  if (remainder.includes('{{') || remainder.includes('}}')) throw new Error('模板字段格式应为 {{字段名}}。')
}

export function buildNotificationPayload(draft: NotificationDraft, columns: NotificationColumn[]) {
  const name = draft.name.trim()
  const owner = draft.owner.trim()
  const tableName = draft.tableName.trim()
  const recipientsTemplate = draft.recipientsTemplate.trim()
  const subjectTemplate = draft.subjectTemplate.trim()
  if (!name) throw new Error('请填写提醒名称。')
  if (!Number.isSafeInteger(draft.datasourceId) || draft.datasourceId < 0) throw new Error('请选择 Oracle 数据源。')
  if (!owner || !tableName || columns.length === 0) throw new Error('请选择数据表并读取字段。')
  const cursor = columns.find((column) => column.name === draft.cursorColumn)
  if (!cursor || !isCursorColumn(cursor)) throw new Error('请选择非空的整数 NUMBER 游标字段，并确认字段值唯一且递增。')
  if (!recipientsTemplate) throw new Error('请填写收件人邮箱或引用邮箱字段。')
  if (!subjectTemplate) throw new Error('请填写邮件标题。')
  if (!draft.bodyTemplate.trim()) throw new Error('请填写邮件正文。')
  if (!Number.isInteger(draft.pollIntervalSeconds) || draft.pollIntervalSeconds < 60 || draft.pollIntervalSeconds > 86400) throw new Error('检查间隔应为 60 至 86400 秒的整数。')
  for (const template of [recipientsTemplate, subjectTemplate, draft.bodyTemplate]) validateTemplateFields(template, columns)
  return {
    name, datasourceId: draft.datasourceId, owner, tableName, cursorColumn: draft.cursorColumn,
    recipientsTemplate, subjectTemplate, bodyTemplate: draft.bodyTemplate, enabled: draft.enabled, pollIntervalSeconds: draft.pollIntervalSeconds,
    ...(draft.id ? { lockVersion: draft.lockVersion } : {}),
  }
}

export function parseSampleRow(value: string): Record<string, unknown> {
  let parsed: unknown
  try { parsed = JSON.parse(value) } catch { throw new Error('请填写有效的 JSON 示例行。') }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('示例行应为一个 JSON 对象。')
  return parsed as Record<string, unknown>
}

export function formatNotificationTime(value: string | null) {
  if (!value) return '尚未执行'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12: false })
}
