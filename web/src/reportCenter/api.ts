import type { ClientFailureKind, ClientResponse, HTTPMethod } from '../api/client'
import { isReportInputQueryName } from './inputQueryName.js'
import { parseReportInputSchemaDocument, reportInputSchemaDocument } from './refCursorConfig.js'
import type { ReportAudit, ReportAuditPage, ReportAuditQuery, ReportCatalogPage, ReportCatalogQuery, ReportColumn, ReportDatasource, ReportDatasourceInput, ReportDatasourceTest, ReportDefinitionStatus, ReportDraft, ReportExport, ReportExportPage, ReportFilterOperator, ReportGrant, ReportInputOption, ReportInputQueryDefinition, ReportInputQueryDefinitionInput, ReportInputQueryTestResult, ReportParameter, ReportProcedureArgument, ReportProcedurePage, ReportProcedureRef, ReportProcedureSignature, ReportProcedureSummary, ReportPublication, ReportResultPage, ReportResultQuery, ReportResultTableColumn, ReportResultTablePage, ReportResultTableRef, ReportResultTableSchema, ReportResultTableSummary, ReportRun, ReportRunContract, ReportRunStatus, ReportSummary, ReportVersionDetail, ReportVersionDiff, ReportVersionPage, ReportVersionSummary } from './types'

type JsonRecord = Record<string, unknown>

export type ReportCenterClient = (path: string, options?: {
  method?: HTTPMethod
  body?: unknown
  signal?: AbortSignal
  showResult?: boolean
  silentLoading?: boolean
  acceptSafeErrorMessage?: boolean
}) => Promise<ClientResponse>

export type ReportAPIFailure = {
  ok: false
  error: string
  kind?: ClientFailureKind
  status?: number
  retryAfterSeconds?: number
}
export type ReportAPIResult<T> = { ok: true; data: T } | ReportAPIFailure

export function isRetryableReportPollFailure(result: ReportAPIFailure) {
  return result.kind === 'network'
    || result.kind === 'offline'
    || result.kind === 'timeout'
    || result.status === 502
    || result.status === 503
    || result.status === 504
}

export function reportPollRetryDelay(result: ReportAPIFailure, consecutiveFailures: number) {
  const attempt = Math.max(0, Math.min(2, consecutiveFailures - 1))
  const backoff = Math.min(5000, 1500 * (2 ** attempt))
  if (result.retryAfterSeconds === undefined) return backoff
  return Math.max(backoff, Math.max(0, result.retryAfterSeconds) * 1000)
}

export async function getReportCatalog(client: ReportCenterClient, query: ReportCatalogQuery, signal?: AbortSignal) {
	return getReportCatalogFromPath(client, '/v1/reports', query, signal, '报表目录加载失败，请稍后重试。')
}

export async function getReportDownloads(client: ReportCenterClient, query: ReportCatalogQuery, signal?: AbortSignal) {
	return getReportCatalogFromPath(client, '/v1/report-downloads', query, signal, '可下载报表加载失败，请稍后重试。')
}

async function getReportCatalogFromPath(client: ReportCenterClient, path: string, query: ReportCatalogQuery, signal: AbortSignal | undefined, fallbackError: string) {
  const search = new URLSearchParams()
  if (query.search?.trim()) search.set('search', query.search.trim())
  if (query.category?.trim()) search.set('category', query.category.trim())
  if (query.afterId && query.afterId > 0) search.set('afterId', String(query.afterId))
  search.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 50))))

  const response = await client(`${path}?${search.toString()}`, {
    method: 'GET',
    signal,
    showResult: false,
    silentLoading: true,
  })
  if (!response.ok) {
    return { ok: false as const, error: response.error?.message ?? fallbackError }
  }
  try {
    return { ok: true as const, page: parseReportCatalogPage(response.data) }
  } catch {
    return { ok: false as const, error: '服务返回的报表目录格式不完整，请稍后重试。' }
  }
}

export function parseReportCatalogPage(payload: unknown): ReportCatalogPage {
  const data = unwrapData(payload)
  const rawItems = firstArray(data.items, data.reports, data.definitions)
  const items = rawItems.flatMap((item) => {
    const parsed = parseReportSummary(item)
    return parsed ? [parsed] : []
  })
  const hasMore = data.hasMore === true
  const nextAfterId = positiveInteger(data.nextAfterId) ?? positiveInteger(data.next_after_id) ?? 0
  if (hasMore && (items.length === 0 || nextAfterId !== items[items.length - 1].id)) throw new Error('invalid report catalog cursor')
  return { items, hasMore, nextAfterId }
}

export async function getReportRunContract(client: ReportCenterClient, reportId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportRunContract>> {
  return requestAndParse(client, `/v1/reports/${reportId}/run-contract`, { method: 'GET', signal }, parseReportRunContract, '报表运行参数加载失败。')
}

export async function getReportInputQueries(client: ReportCenterClient, signal?: AbortSignal): Promise<ReportAPIResult<string[]>> {
	return requestAndParse(client, '/v1/report-input-queries', { method: 'GET', signal }, parseReportInputQueries, '报表输入查询配置加载失败。')
}

export async function getReportInputQueryDefinitions(client: ReportCenterClient, signal?: AbortSignal): Promise<ReportAPIResult<ReportInputQueryDefinition[]>> {
	return requestAndParse(client, '/v1/report-input-query-definitions', { method: 'GET', signal }, parseReportInputQueryDefinitions, '输入选项查询配置加载失败。')
}

export async function createReportInputQueryDefinition(client: ReportCenterClient, input: ReportInputQueryDefinitionInput): Promise<ReportAPIResult<ReportInputQueryDefinition>> {
	return requestAndParse(client, '/v1/report-input-query-definitions', { method: 'POST', body: input }, parseReportInputQueryDefinition, '输入选项查询创建失败。')
}

export async function updateReportInputQueryDefinition(client: ReportCenterClient, id: number, input: ReportInputQueryDefinitionInput): Promise<ReportAPIResult<ReportInputQueryDefinition>> {
	return requestAndParse(client, `/v1/report-input-query-definitions/${id}`, { method: 'PUT', body: input }, parseReportInputQueryDefinition, '输入选项查询更新失败。')
}

export async function deleteReportInputQueryDefinition(client: ReportCenterClient, id: number, expectedLockVersion: number): Promise<ReportAPIResult<{ id: number }>> {
	const search = new URLSearchParams({ expectedLockVersion: String(expectedLockVersion) })
	return requestAndParse(client, `/v1/report-input-query-definitions/${id}?${search}`, { method: 'DELETE' }, (payload) => {
		const deletedId = positiveInteger(unwrapData(payload).id)
		if (!deletedId) throw new Error('invalid deleted report input query')
		return { id: deletedId }
	}, '输入选项查询删除失败。')
}

export async function testReportInputQueryDefinition(client: ReportCenterClient, definitionId: number | null, selectSql: string, name = ''): Promise<ReportAPIResult<ReportInputQueryTestResult>> {
	const path = definitionId ? `/v1/report-input-query-definitions/${definitionId}/test` : '/v1/report-input-query-definition-tests'
	const body = definitionId ? { ...(name.trim() ? { name: name.trim() } : {}) } : { selectSql, ...(name.trim() ? { name: name.trim() } : {}) }
	return requestAndParse(client, path, { method: 'POST', body }, parseReportInputQueryTestResult, '输入选项查询测试失败。')
}

export async function getReportInputOptions(client: ReportCenterClient, reportId: number, conditionCode: string, signal?: AbortSignal): Promise<ReportAPIResult<ReportInputOption[]>> {
	return requestAndParse(client, `/v1/reports/${reportId}/input-options/${encodeURIComponent(conditionCode)}`, { method: 'GET', signal }, parseReportInputOptions, '报表输入选项加载失败。')
}

export function parseReportInputQueries(payload: unknown): string[] {
	const data = unwrapData(payload)
	if (!Array.isArray(data.items)) throw new Error('invalid report input queries')
	const items = data.items.filter((item): item is string => typeof item === 'string' && isReportInputQueryName(item))
	if (items.length !== data.items.length || new Set(items).size !== items.length) throw new Error('invalid report input queries')
	return items
}

export function parseReportInputQueryDefinitions(payload: unknown): ReportInputQueryDefinition[] {
	const data = unwrapData(payload)
	if (!Array.isArray(data.items)) throw new Error('invalid report input query definitions')
	return data.items.map(parseReportInputQueryDefinition)
}

export function parseReportInputQueryDefinition(payload: unknown): ReportInputQueryDefinition {
	const data = unwrapData(payload)
	const id = positiveInteger(data.id)
	const name = publicString(data.name, 64)
	const selectSql = typeof data.selectSql === 'string' ? data.selectSql.trim().slice(0, 65536) : ''
	const lockVersion = positiveInteger(data.lockVersion)
	const lastTestStatus = data.lastTestStatus === 'SUCCESS' || data.lastTestStatus === 'FAILED' ? data.lastTestStatus : 'NOT_TESTED'
	if (!id || !name || !isReportInputQueryName(name) || !selectSql || !lockVersion || typeof data.enabled !== 'boolean') throw new Error('invalid report input query definition')
	return {
		id, name, selectSql, lockVersion, enabled: data.enabled,
		lastTestStatus,
		lastTestError: publicString(data.lastTestError, 500),
		lastTestedAt: publicDate(data.lastTestedAt),
		createdAt: publicDate(data.createdAt),
		updatedAt: publicDate(data.updatedAt),
	}
}

export function parseReportInputQueryTestResult(payload: unknown): ReportInputQueryTestResult {
	const data = unwrapData(payload)
	if (data.status !== 'SUCCESS' && data.status !== 'FAILED') throw new Error('invalid report input query test result')
	if (!Array.isArray(data.items)) throw new Error('invalid report input query test items')
	const items = data.items.flatMap((item) => {
		if (!isRecord(item) || (typeof item.id !== 'string' && typeof item.id !== 'number')) return []
		const id = String(item.id)
		const name = publicString(item.name, 256)
		return id && name ? [{ id, name }] : []
	})
	if (items.length !== data.items.length) throw new Error('invalid report input query test items')
	return {
		status: data.status,
		testedAt: publicDate(data.testedAt),
		latencyMs: nonNegativeInteger(data.latencyMs),
		rowCount: nonNegativeInteger(data.rowCount),
		items,
		errorCode: publicString(data.errorCode, 64),
		message: publicString(data.message, 500),
	}
}

export function parseReportInputOptions(payload: unknown): ReportInputOption[] {
	const data = unwrapData(payload)
	if (!Array.isArray(data.items)) throw new Error('invalid report input options')
	const items = data.items.flatMap((item) => {
		if (!isRecord(item) || (typeof item.id !== 'string' && typeof item.id !== 'number')) return []
		const id = String(item.id)
		const name = publicString(item.name, 256)
		return id && name ? [{ id, name }] : []
	})
	if (items.length !== data.items.length) throw new Error('invalid report input options')
	return items
}

export async function getReportDatasources(client: ReportCenterClient, signal?: AbortSignal): Promise<ReportAPIResult<ReportDatasource[]>> {
  return requestAndParse(client, '/v1/report-datasources', { method: 'GET', signal }, parseReportDatasources, 'Oracle 数据源加载失败。')
}

export async function createReportDatasource(client: ReportCenterClient, input: ReportDatasourceInput): Promise<ReportAPIResult<ReportDatasource>> {
  return requestAndParse(client, '/v1/report-datasources', { method: 'POST', body: serializeReportDatasource(input) }, parseReportDatasource, 'Oracle 数据源创建失败。')
}

export async function updateReportDatasource(client: ReportCenterClient, datasourceId: number, input: ReportDatasourceInput): Promise<ReportAPIResult<ReportDatasource>> {
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}`, { method: 'PUT', body: serializeReportDatasource(input) }, parseReportDatasource, 'Oracle 数据源更新失败。')
}

export async function testReportDatasource(client: ReportCenterClient, datasourceId: number): Promise<ReportAPIResult<ReportDatasourceTest>> {
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}/test`, { method: 'POST' }, parseReportDatasourceTest, 'Oracle 连接测试失败。')
}

export async function testReportDatasourceConnection(client: ReportCenterClient, input: ReportDatasourceInput, datasourceId = 0): Promise<ReportAPIResult<ReportDatasourceTest>> {
  return requestAndParse(client, '/v1/report-datasource-connection-tests', { method: 'POST', body: {
    datasourceId: datasourceId || undefined,
    host: input.host, port: input.port, serviceName: input.serviceName, sid: input.sid,
    username: input.username, ...(input.password ? { password: input.password } : {}), sessionTimezone: input.sessionTimezone,
    connectTimeoutSeconds: input.connectTimeoutSeconds, queryTimeoutSeconds: input.queryTimeoutSeconds,
    maxOpenConnections: input.maxOpenConnections, maxIdleConnections: input.maxIdleConnections,
    prefetchRows: input.prefetchRows, arraySize: input.arraySize,
  } }, parseReportDatasourceTest, 'Oracle 连接测试失败。')
}

export async function getReportProcedures(client: ReportCenterClient, datasourceId: number, query: { owner?: string; search?: string; after?: string; limit?: number }, signal?: AbortSignal): Promise<ReportAPIResult<ReportProcedurePage>> {
  const search = new URLSearchParams()
  if (query.owner?.trim()) search.set('owner', query.owner.trim())
  if (query.search?.trim()) search.set('search', query.search.trim())
  if (query.after) search.set('after', query.after)
  search.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 50))))
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}/procedures?${search}`, { method: 'GET', signal }, parseReportProcedurePage, 'Oracle 存储过程目录加载失败。')
}

export async function getReportProcedureSignature(client: ReportCenterClient, datasourceId: number, procedure: ReportProcedureRef, signal?: AbortSignal): Promise<ReportAPIResult<ReportProcedureSignature>> {
  const search = new URLSearchParams({ owner: procedure.owner, name: procedure.name })
  if (procedure.package) search.set('package', procedure.package)
  if (procedure.overload) search.set('overload', procedure.overload)
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}/procedure-signature?${search}`, { method: 'GET', signal }, parseReportProcedureSignature, 'Oracle 存储过程签名加载失败。')
}

export async function getReportResultTables(client: ReportCenterClient, datasourceId: number, query: { owner?: string; search?: string; after?: string; limit?: number }, signal?: AbortSignal): Promise<ReportAPIResult<ReportResultTablePage>> {
  const search = new URLSearchParams()
  if (query.owner?.trim()) search.set('owner', query.owner.trim())
  if (query.search?.trim()) search.set('search', query.search.trim())
  if (query.after) search.set('after', query.after)
  search.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 50))))
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}/result-tables?${search}`, { method: 'GET', signal }, parseReportResultTablePage, 'Oracle 结果表目录加载失败。')
}

export async function getReportResultTableSchema(client: ReportCenterClient, datasourceId: number, table: ReportResultTableRef, signal?: AbortSignal): Promise<ReportAPIResult<ReportResultTableSchema>> {
  const search = new URLSearchParams({ owner: table.owner, name: table.name })
  return requestAndParse(client, `/v1/report-datasources/${datasourceId}/result-table-schema?${search}`, { method: 'GET', signal }, parseReportResultTableSchema, 'Oracle 结果表字段加载失败。')
}

export function parseReportDatasources(payload: unknown): ReportDatasource[] {
  const data = unwrapData(payload)
  const rawItems = firstArray(data.items)
  const items = rawItems.flatMap((value) => {
    try { return [parseReportDatasource(value)] } catch { return [] }
  })
  if (items.length !== rawItems.length) throw new Error('invalid report datasource list')
  return items
}

export function parseReportDatasource(payload: unknown): ReportDatasource {
  const data = unwrapData(payload)
  for (const forbidden of ['password', 'passwordCiphertext', 'credentialKeyVersion', 'sessionInitJSON', 'dsn']) {
    if (Object.prototype.hasOwnProperty.call(data, forbidden)) throw new Error('sensitive report datasource field')
  }
  const id = positiveInteger(data.id)
  const code = publicString(data.code, 64)
  const name = publicString(data.name, 128)
  const host = publicString(data.host, 255)
  const port = positiveInteger(data.port)
  const serviceName = publicString(data.serviceName, 128)
  const sid = publicString(data.sid, 128)
  const username = publicString(data.username, 128)
  if (!id || !code || !name || data.driver !== 'ORACLE' || !host || !port || port > 65535 || !username || (Boolean(serviceName) === Boolean(sid)) || typeof data.enabled !== 'boolean' || typeof data.hasPassword !== 'boolean') {
    throw new Error('invalid report datasource')
  }
  return {
    id, code, name, driver: 'ORACLE', host, port, serviceName, sid, username,
    hasPassword: data.hasPassword,
    sessionTimezone: publicString(data.sessionTimezone, 64) || 'Asia/Shanghai',
    connectTimeoutSeconds: positiveInteger(data.connectTimeoutSeconds) ?? 5,
    queryTimeoutSeconds: positiveInteger(data.queryTimeoutSeconds) ?? 300,
    maxOpenConnections: positiveInteger(data.maxOpenConnections) ?? 10,
    maxIdleConnections: nonNegativeInteger(data.maxIdleConnections),
    prefetchRows: positiveInteger(data.prefetchRows) ?? 1000,
    arraySize: positiveInteger(data.arraySize) ?? 1000,
    enabled: data.enabled,
    lastTestStatus: data.lastTestStatus === 'SUCCESS' || data.lastTestStatus === 'FAILED' ? data.lastTestStatus : 'NOT_TESTED',
    lastTestError: publicString(data.lastTestError, 300),
    lastTestedAt: publicDate(data.lastTestedAt),
  }
}

export function parseReportDatasourceTest(payload: unknown): ReportDatasourceTest {
  const data = unwrapData(payload)
  const status = data.status === 'SUCCESS' || data.status === 'FAILED' ? data.status : null
  const testedAt = publicDate(data.testedAt)
  const message = publicString(data.message, 300)
  if (!status || !testedAt || !message) throw new Error('invalid report datasource test')
  return { status, testedAt, latencyMs: nonNegativeInteger(data.latencyMs), errorCode: publicString(data.errorCode, 100), message }
}

export function parseReportProcedurePage(payload: unknown): ReportProcedurePage {
  const data = unwrapData(payload)
  if (!Array.isArray(data.items) || typeof data.hasMore !== 'boolean') throw new Error('invalid procedure page')
  const items = data.items.map(parseReportProcedureSummary)
  const nextAfter = publicString(data.nextAfter, 1024)
  if (data.hasMore && (!items.length || !nextAfter)) throw new Error('invalid procedure cursor')
  return { items, hasMore: data.hasMore, nextAfter }
}

export function parseReportProcedureSignature(payload: unknown): ReportProcedureSignature {
  const data = unwrapData(payload)
  const procedure = parseReportProcedureSummary(data.procedure)
  if (!Array.isArray(data.arguments) || !Array.isArray(data.blockingReasons) || typeof data.protocolReady !== 'boolean' || typeof data.allSupported !== 'boolean') throw new Error('invalid procedure signature')
  const argumentsList = data.arguments.map(parseReportProcedureArgument)
  const blockingReasons = data.blockingReasons.map((value) => publicString(value, 300))
  if (blockingReasons.some((value) => !value) || procedure.argumentCount !== argumentsList.length || data.protocolReady !== data.allSupported) throw new Error('invalid procedure signature')
  const inputArgName = publicString(data.inputArgName, 128)
  const outputArgName = publicString(data.outputArgName, 128)
  if (data.protocolReady && (!inputArgName || blockingReasons.length > 0)) throw new Error('invalid procedure protocol')
  return {
    procedure,
    arguments: argumentsList,
    allSupported: data.allSupported,
    protocolReady: data.protocolReady,
    inputArgName,
    outputArgName,
    callTemplate: typeof data.callTemplate === 'string' ? data.callTemplate.slice(0, 65536) : '',
    blockingReasons,
  }
}

export function parseReportResultTablePage(payload: unknown): ReportResultTablePage {
  const data = unwrapData(payload)
  if (!Array.isArray(data.items) || typeof data.hasMore !== 'boolean') throw new Error('invalid result table page')
  const items = data.items.map(parseReportResultTableSummary)
  const nextAfter = publicString(data.nextAfter, 1024)
  if (data.hasMore && (!items.length || !nextAfter)) throw new Error('invalid result table cursor')
  return { items, hasMore: data.hasMore, nextAfter }
}

export function parseReportResultTableSchema(payload: unknown): ReportResultTableSchema {
  const data = unwrapData(payload)
  if (!Array.isArray(data.columns) || data.columns.length === 0) throw new Error('invalid result table schema')
  const columns = data.columns.map(parseReportResultTableColumn)
  if (columns.some((column, index) => index > 0 && column.position <= columns[index - 1].position)) throw new Error('invalid result table column order')
  if (new Set(columns.map((column) => column.name.toUpperCase())).size !== columns.length) throw new Error('invalid result table column names')
  const table = parseReportResultTableSummary(data.table, columns.length)
  if (table.columnCount !== columns.length) throw new Error('invalid result table column count')
  return { table, columns }
}

export async function getReportDraft(client: ReportCenterClient, reportId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportDraft>> {
  return requestAndParse(client, `/v1/reports/${reportId}`, { method: 'GET', signal }, parseReportDraft, '报表草稿加载失败。')
}

export async function saveReportDraft(client: ReportCenterClient, draft: ReportDraft): Promise<ReportAPIResult<ReportDraft>> {
  const creating = draft.id === 0
  const body = serializeReportDraft(draft, creating)
  return requestAndParse(client, creating ? '/v1/reports' : `/v1/reports/${draft.id}`, { method: creating ? 'POST' : 'PUT', body }, parseReportDraft, '报表草稿保存失败。')
}

export async function deleteReportDraft(client: ReportCenterClient, reportId: number, expectedLockVersion: number): Promise<ReportAPIResult<{ id: number }>> {
  const search = new URLSearchParams({ expectedLockVersion: String(expectedLockVersion) })
  return requestAndParse(client, `/v1/reports/${reportId}?${search}`, { method: 'DELETE' }, parseReportDraftDelete, '报表模板删除失败。')
}

function parseReportDraftDelete(payload: unknown) {
  const id = positiveInteger(unwrapData(payload).id)
  if (!id) throw new Error('invalid deleted report')
  return { id }
}

export async function publishReportDraft(client: ReportCenterClient, reportId: number, expectedLockVersion: number): Promise<ReportAPIResult<ReportPublication>> {
  return requestAndParse(client, `/v1/reports/${reportId}/publish`, { method: 'POST', body: { expectedLockVersion } }, parsePublication, '报表发布与 Oracle 契约核验失败。')
}

export async function saveAndPublishReportDraft(client: ReportCenterClient, draft: ReportDraft): Promise<
  { ok: true; draft: ReportDraft; publication: ReportPublication } |
  { ok: false; error: string; draft?: ReportDraft }
> {
  const saved = await saveReportDraft(client, draft)
  if (saved.ok === false) return { ok: false, error: saved.error }
  const published = await publishReportDraft(client, saved.data.id, saved.data.lockVersion)
  if (published.ok === false) return { ok: false, error: published.error, draft: saved.data }
  return { ok: true, draft: saved.data, publication: published.data }
}

export async function getReportVersions(client: ReportCenterClient, reportId: number, afterId = 0, signal?: AbortSignal): Promise<ReportAPIResult<ReportVersionPage>> {
  const search = new URLSearchParams({ limit: '50' })
  if (afterId > 0) search.set('afterId', String(afterId))
  return requestAndParse(client, `/v1/reports/${reportId}/versions?${search}`, { method: 'GET', signal }, parseReportVersionPage, '报表版本历史加载失败。')
}

export async function getReportVersion(client: ReportCenterClient, reportId: number, versionId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportVersionDetail>> {
	return requestAndParse(client, `/v1/reports/${reportId}/versions/${versionId}`, { method: 'GET', signal }, parseReportVersionDetail, '报表历史版本配置加载失败。')
}

export async function activateReportVersion(client: ReportCenterClient, reportId: number, versionId: number, expectedLockVersion: number): Promise<ReportAPIResult<ReportPublication>> {
	return requestAndParse(client, `/v1/reports/${reportId}/active-version`, { method: 'PUT', body: { versionId, expectedLockVersion } }, parsePublication, '历史版本上线与 Oracle 契约核验失败。')
}

export async function getReportVersionDiff(client: ReportCenterClient, reportId: number, baseVersionId: number, targetVersionId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportVersionDiff>> {
  const search = new URLSearchParams({ baseVersionId: String(baseVersionId), targetVersionId: String(targetVersionId) })
  return requestAndParse(client, `/v1/reports/${reportId}/version-diff?${search}`, { method: 'GET', signal }, parseReportVersionDiff, '报表版本差异加载失败。')
}

export async function createReportRun(client: ReportCenterClient, reportId: number, input: Record<string, unknown>, executionMode: ReportDraft['executionMode'], jsonInput = executionMode === 'REF_CURSOR', signal?: AbortSignal): Promise<ReportAPIResult<ReportRun>> {
  const body = jsonInput ? { conditions: input } : { parameters: input }
  return requestAndParse(client, `/v1/reports/${reportId}/runs`, { method: 'POST', body, signal }, parseReportRun, '报表运行创建失败。')
}

export async function getReportRun(client: ReportCenterClient, runId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportRun>> {
  return requestAndParse(client, `/v1/report-runs/${runId}`, { method: 'GET', signal }, parseReportRun, '报表运行状态加载失败。')
}

export async function cancelReportRun(client: ReportCenterClient, runId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportRun>> {
  return requestAndParse(client, `/v1/report-runs/${runId}/cancel`, { method: 'POST', signal }, parseReportRun, '报表运行取消失败。')
}

export async function getReportResults(client: ReportCenterClient, runId: number, cursor = '', limit = 100, signal?: AbortSignal): Promise<ReportAPIResult<ReportResultPage>> {
  const query = new URLSearchParams({ limit: String(limit) })
  if (cursor) query.set('cursor', cursor)
  return requestAndParse(client, `/v1/report-runs/${runId}/results?${query}`, { method: 'GET', signal }, parseReportResultPage, '报表结果加载失败。')
}

export async function queryReportResults(client: ReportCenterClient, runId: number, query: ReportResultQuery, cursor = '', limit = 100, signal?: AbortSignal): Promise<ReportAPIResult<ReportResultPage>> {
	return requestAndParse(client, `/v1/report-runs/${runId}/results/query`, { method: 'POST', body: { ...query, cursor, limit }, signal }, parseReportResultPage, '报表结果加载失败。')
}

export async function createReportExport(client: ReportCenterClient, runId: number, query: ReportResultQuery, signal?: AbortSignal): Promise<ReportAPIResult<ReportExport>> {
	return requestAndParse(client, `/v1/report-runs/${runId}/export`, { method: 'POST', body: query, signal }, parseReportExport, '正式导出创建失败。')
}

export async function getReportExport(client: ReportCenterClient, exportId: number, signal?: AbortSignal): Promise<ReportAPIResult<ReportExport>> {
  return requestAndParse(client, `/v1/report-exports/${exportId}`, { method: 'GET', signal }, parseReportExport, '导出状态加载失败。')
}

export async function getReportExports(client: ReportCenterClient, query: { afterId?: number; limit?: number; status?: string }, signal?: AbortSignal): Promise<ReportAPIResult<ReportExportPage>> {
	const search = new URLSearchParams(); if (query.afterId) search.set('afterId', String(query.afterId)); if (query.limit) search.set('limit', String(query.limit)); if (query.status) search.set('status', query.status)
	return requestAndParse(client, `/v1/report-exports?${search}`, { method: 'GET', signal }, parseReportExportPage, '导出任务加载失败。')
}

export async function getReportExportDownload(client: ReportCenterClient, exportId: number): Promise<ReportAPIResult<{ url: string; expiresAt: string | null }>> {
  return requestAndParse(client, `/v1/report-exports/${exportId}/download`, { method: 'GET' }, parseReportExportDownload, '下载地址获取失败。')
}

export async function getReportAudits(client: ReportCenterClient, query: ReportAuditQuery, signal?: AbortSignal): Promise<ReportAPIResult<ReportAuditPage>> {
  const search = new URLSearchParams()
  if (query.action?.trim()) search.set('action', query.action.trim())
  if (query.targetType?.trim()) search.set('targetType', query.targetType.trim())
  if (query.targetId && query.targetId > 0) search.set('targetId', String(query.targetId))
  if (query.afterId && query.afterId > 0) search.set('afterId', String(query.afterId))
  search.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 50))))
  return requestAndParse(client, `/v1/report-audits?${search}`, { method: 'GET', signal }, parseReportAuditPage, '报表审计记录加载失败。')
}

export function parseReportRunContract(payload: unknown): ReportRunContract {
  const data = unwrapData(payload)
  const definitionId = positiveInteger(data.definitionId)
  const versionId = positiveInteger(data.versionId)
  if (!definitionId || !versionId) throw new Error('invalid run contract identity')
  const rawParameters = firstArray(data.parameters)
  const parameters = rawParameters.flatMap((value) => {
    const parameter = parseReportParameter(value)
    return parameter ? [parameter] : []
  })
  if (parameters.length !== rawParameters.length) throw new Error('invalid run contract parameters')
  const executionMode = data.executionMode === 'REF_CURSOR' ? 'REF_CURSOR' : 'TABLE_SNAPSHOT'
  const jsonInput = executionMode === 'REF_CURSOR' || data.inputSchema !== undefined
  const inputSchema = jsonInput ? parseReportInputSchemaDocument(data.inputSchema, true) : {}
  return {
    definitionId,
    versionId,
    code: publicString(data.code, 64),
    name: publicString(data.name, 128),
    description: publicString(data.description, 500),
    executionMode,
    jsonInput,
    inputSchema,
    parameters: parameters.sort((left, right) => left.displayOrder - right.displayOrder || left.position - right.position),
  }
}

export function parseReportDraft(payload: unknown): ReportDraft {
  const data = unwrapData(payload)
  const id = positiveInteger(data.id)
  const datasourceId = positiveInteger(data.datasourceId)
  const procedure = isRecord(data.procedure) ? data.procedure : {}
  const result = isRecord(data.result) ? data.result : {}
  const rawParameters = firstArray(data.parameters)
  const parameters = rawParameters.flatMap((value) => { const item = parseReportParameter(value); return item ? [item] : [] })
  const rawColumns = firstArray(data.columns)
  const columns = rawColumns.flatMap((value) => { const item = parseReportColumn(value); return item ? [item] : [] })
  const rawGrants = firstArray(data.grants)
  const grants = rawGrants.flatMap((value) => { const item = parseReportGrant(value); return item ? [item] : [] })
  const executionMode = data.executionMode === 'REF_CURSOR' ? 'REF_CURSOR' : 'TABLE_SNAPSHOT'
  const jsonTableSnapshot = executionMode === 'TABLE_SNAPSHOT' && Boolean(publicString(procedure.jsonInputArgName, 128))
  let inputSchema: ReportDraft['inputSchema'] = {}
  if (executionMode === 'REF_CURSOR' || jsonTableSnapshot) inputSchema = parseReportInputSchemaDocument(data.inputSchema, true)
  if (!id || !datasourceId || parameters.length !== rawParameters.length || columns.length !== rawColumns.length || grants.length !== rawGrants.length) throw new Error('invalid report draft')
  return {
    id, datasourceId, code: publicString(data.code, 64), name: publicString(data.name, 128), category: publicString(data.category, 64), description: publicString(data.description, 500),
    status: reportStatus(data.status), lockVersion: positiveInteger(data.lockVersion) ?? 0,
    executionMode,
    procedure: {
      owner: publicString(procedure.owner, 128), package: publicString(procedure.package, 128), name: publicString(procedure.name, 128), overload: publicString(procedure.overload, 32),
      jsonInputArgName: publicString(procedure.jsonInputArgName, 128), resultCursorArgName: publicString(procedure.resultCursorArgName, 128),
    },
    inputSchema,
    result: { tableOwner: publicString(result.tableOwner, 128), tableName: publicString(result.tableName, 128) },
    callTemplate: typeof data.callTemplate === 'string' ? data.callTemplate.slice(0, 65536) : '', parameters, columns, grants,
    createdAt: publicDate(data.createdAt), updatedAt: publicDate(data.updatedAt),
  }
}

export function parseReportRun(payload: unknown): ReportRun {
  const data = unwrapData(payload)
  const id = positiveInteger(data.id)
  const definitionId = positiveInteger(data.definitionId)
  const versionId = positiveInteger(data.versionId)
  if (!id || !definitionId || !versionId) throw new Error('invalid report run')
  return {
    id,
    runUuid: publicString(data.runUuid, 64),
    definitionId,
    versionId,
    status: reportRunStatus(data.status),
    rowCount: nonNegativeInteger(data.rowCount),
    cancelRequested: data.cancelRequested === true,
    createdAt: publicDate(data.createdAt),
    startedAt: publicDate(data.startedAt),
    finishedAt: publicDate(data.finishedAt),
    resultExpiresAt: publicDate(data.resultExpiresAt),
    errorCode: publicString(data.errorCode, 100),
    errorMessage: publicString(data.errorMessage, 500),
    canCancel: data.canCancel === true,
    resultAvailable: data.resultAvailable === true,
  }
}

export function parseReportResultPage(payload: unknown): ReportResultPage {
  const data = unwrapData(payload)
  const run = parseReportRun(data.run)
  const pagination = isRecord(data.pagination) ? data.pagination : {}
  const rawColumns = firstArray(data.columns)
  const columns = rawColumns.flatMap((value) => {
    if (!isRecord(value)) return []
    const fieldId = publicString(value.fieldId, 64)
    const code = publicString(value.code, 64)
    if (!fieldId || !code) return []
		const allowedOperators = firstArray(value.allowedOperators).map(reportFilterOperator)
		return [{ fieldId, code, header: publicString(value.header, 128) || code, valueType: publicString(value.valueType, 32), nullable: value.nullable === true, nullDisplay: publicString(value.nullDisplay, 32), filterable: value.filterable === true, sortable: value.sortable === true, allowedOperators }]
  })
  const rawRows = firstArray(data.rows)
  const rows = rawRows.flatMap((value) => {
    if (!isRecord(value) || !isRecord(value.values)) return []
    const key = publicString(value.key, 128)
    return key ? [{ key, values: value.values }] : []
  })
  if (columns.length !== rawColumns.length || rows.length !== rawRows.length) throw new Error('invalid report result page')
  const nextCursor = publicString(pagination.nextCursor, 1024)
  if (pagination.hasMore === true && !nextCursor) throw new Error('invalid report result cursor')
  return {
    run,
    columns,
    rows,
    pagination: {
      pageSize: positiveInteger(pagination.pageSize) ?? 100,
      hasMore: pagination.hasMore === true,
      nextCursor,
    },
  }
}

function reportFilterOperator(value: unknown): ReportFilterOperator {
	const operator = publicString(value, 32) as ReportFilterOperator
	if (!['EQ','NE','GT','GTE','LT','LTE','IN','NOT_IN','IS_NULL','IS_NOT_NULL','CONTAINS','STARTS_WITH','BETWEEN'].includes(operator)) throw new Error('invalid report filter operator')
	return operator
}

export function parseReportExport(payload: unknown): ReportExport {
  const data = unwrapData(payload)
  const id = positiveInteger(data.id)
  const runId = positiveInteger(data.runId)
  if (!id || !runId) throw new Error('invalid report export')
  return {
    id, runId,
    exportUuid: publicString(data.exportUuid, 64),
    status: reportExportStatus(data.status),
    processedRows: nonNegativeInteger(data.processedRows),
    exportedRows: nonNegativeInteger(data.exportedRows),
    currentSheet: publicString(data.currentSheet, 64),
    sheetCount: nonNegativeInteger(data.sheetCount),
    truncatedCellCount: nonNegativeInteger(data.truncatedCellCount),
    fileSizeBytes: nonNegativeInteger(data.fileSizeBytes),
    createdAt: publicDate(data.createdAt), startedAt: publicDate(data.startedAt), readyAt: publicDate(data.readyAt),
    expiresAt: publicDate(data.expiresAt), purgedAt: publicDate(data.purgedAt),
    errorCode: publicString(data.errorCode, 100), errorMessage: publicString(data.errorMessage, 500),
    canDownload: data.canDownload === true,
		reportName: publicString(data.reportName, 128), purgedRows: nonNegativeInteger(data.purgedRows), purgeStartedAt: publicDate(data.purgeStartedAt),
  }
}

export function parseReportExportPage(payload: unknown): ReportExportPage {
	const data = unwrapData(payload); const rawItems = firstArray(data.items); const items = rawItems.map((item) => parseReportExport({ data: item }));
	const hasMore = data.hasMore === true
	const nextAfterId = nonNegativeInteger(data.nextAfterId)
	if (hasMore && nextAfterId < 1) throw new Error('invalid report export cursor')
	return { items, hasMore, nextAfterId }
}

export function parseReportAuditPage(payload: unknown): ReportAuditPage {
  const data = unwrapData(payload)
  if (!Array.isArray(data.items) || typeof data.hasMore !== 'boolean') throw new Error('invalid report audit page')
  const rawItems = data.items
  const items = rawItems.map((value) => {
    if (!isRecord(value)) throw new Error('invalid report audit')
    const id = positiveInteger(value.id)
    const actorType: ReportAudit['actorType'] | '' = value.actorType === 'SYSTEM'
      ? 'SYSTEM'
      : value.actorType === 'USER' || value.actorType === undefined
        ? 'USER'
        : ''
    const parsedActorUserId = actorType === 'SYSTEM' ? nonNegativeInteger(value.actorUserId) : positiveInteger(value.actorUserId)
    const action = publicString(value.action, 64)
    const targetType = publicString(value.targetType, 32)
    const targetId = positiveInteger(value.targetId)
    const requestId = publicString(value.requestId, 128)
    const createdAt = publicDate(value.createdAt)
    if (!id || !actorType || parsedActorUserId === null || (actorType === 'USER' ? parsedActorUserId < 1 : parsedActorUserId !== 0) || !action || !targetType || !targetId || !requestId || !createdAt) throw new Error('invalid report audit')
    const actorUserId = parsedActorUserId
    return { id, actorType, actorUserId, action, targetType, targetId, requestId, detail: isRecord(value.detail) ? value.detail : {}, createdAt }
  })
  for (let index = 1; index < items.length; index += 1) {
    if (items[index - 1].id <= items[index].id) throw new Error('invalid report audit order')
  }
  const hasMore = data.hasMore
  const nextAfterId = nonNegativeInteger(data.nextAfterId)
  if (hasMore && (items.length === 0 || nextAfterId !== items[items.length - 1].id)) throw new Error('invalid report audit cursor')
  if (!hasMore && nextAfterId !== 0) throw new Error('unexpected report audit cursor')
  return { items, hasMore, nextAfterId }
}

async function requestAndParse<T>(client: ReportCenterClient, path: string, options: Parameters<ReportCenterClient>[1], parse: (payload: unknown) => T, fallback: string): Promise<ReportAPIResult<T>> {
  const response = await client(path, { ...options, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
  if (!response.ok) {
    const failure: Extract<ReportAPIResult<T>, { ok: false }> = { ok: false, error: response.error?.message ?? fallback }
    if (response.error?.kind) failure.kind = response.error.kind
    if (Number.isFinite(response.status)) failure.status = response.status
    if (response.error?.retryAfterSeconds !== undefined) failure.retryAfterSeconds = response.error.retryAfterSeconds
    return failure
  }
  try {
    return { ok: true, data: parse(response.data) }
  } catch {
    return { ok: false, error: '服务返回的数据格式不完整，请稍后重试。' }
  }
}

function parseReportExportDownload(payload: unknown) {
  const data = unwrapData(payload)
  const allowLocalHTTP = typeof window !== 'undefined' && ['localhost', '127.0.0.1', '::1'].includes(window.location.hostname)
  const url = typeof data.url === 'string' && (/^https:\/\//.test(data.url) || (allowLocalHTTP && /^http:\/\/(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?\//.test(data.url))) ? data.url : ''
  if (!url) throw new Error('invalid report download')
  return { url, expiresAt: publicDate(data.expiresAt) }
}

function parseReportParameter(value: unknown): ReportParameter | null {
  if (!isRecord(value)) return null
  const code = publicString(value.code, 64)
  if (!code) return null
  const allowedValues = Array.isArray(value.allowedValues) ? value.allowedValues.filter((item): item is string => typeof item === 'string') : []
  const normalizer = isRecord(value.normalizer) ? { ...value.normalizer } : {}
  if (typeof normalizer.case === 'string') normalizer.case = normalizer.case.trim().toUpperCase()
  const valueSource = isRecord(value.valueSource) ? { ...value.valueSource } : {}
  if (typeof valueSource.source === 'string') valueSource.source = valueSource.source.trim().toUpperCase()
  return {
    code,
    label: publicString(value.label, 128) || code,
    displayOrder: nonNegativeInteger(value.displayOrder),
    controlType: reportControlType(value.controlType),
    logicalType: reportLogicalType(value.logicalType),
    cardinality: value.cardinality === 'MULTIPLE' ? 'MULTIPLE' : 'SINGLE',
    procedureArgName: publicString(value.procedureArgName, 128),
    position: positiveInteger(value.position) ?? 0,
    oracleType: publicString(value.oracleType, 64),
    precision: nullableInteger(value.precision), scale: nullableInteger(value.scale), maxLength: nullableInteger(value.maxLength),
    required: value.required === true, nullable: value.nullable === true, systemInjected: value.systemInjected === true, sensitive: value.sensitive === true,
    defaultValue: value.defaultValue,
    allowedValues,
    validation: isRecord(value.validation) ? value.validation : {},
    normalizer,
    valueSource,
    timezone: publicString(value.timezone, 64),
    nullPolicy: publicString(value.nullPolicy, 32) || 'TYPED_NULL',
    errorMessage: publicString(value.errorMessage, 300),
    collectionEncoding: publicString(value.collectionEncoding, 32),
  }
}

function parseReportProcedureSummary(value: unknown): ReportProcedureSummary {
  if (!isRecord(value)) throw new Error('invalid procedure')
  const owner = publicString(value.owner, 128)
  const packageName = publicString(value.package, 128)
  const name = publicString(value.name, 128)
  const overload = publicString(value.overload, 32)
  const argumentCount = strictNonNegativeInteger(value.argumentCount)
  const qualifiedName = publicString(value.qualifiedName, 520)
  if (!owner || !name || !qualifiedName) throw new Error('invalid procedure')
  return { owner, package: packageName, name, overload, argumentCount, qualifiedName }
}

function parseReportProcedureArgument(value: unknown): ReportProcedureArgument {
  if (!isRecord(value)) throw new Error('invalid procedure argument')
  const name = publicString(value.name, 128)
  const position = positiveInteger(value.position)
  const sequence = positiveInteger(value.sequence)
  const direction = publicString(value.direction, 16)
  const oracleType = publicString(value.oracleType, 64)
  const role = publicString(value.role, 32)
  if (!name || !position || !sequence || !direction || !oracleType || !role || typeof value.defaulted !== 'boolean' || typeof value.supported !== 'boolean') throw new Error('invalid procedure argument')
  return {
    name, position, sequence, direction, oracleType,
    dataLength: nullableInteger(value.dataLength), precision: nullableInteger(value.precision), scale: nullableInteger(value.scale),
    typeOwner: publicString(value.typeOwner, 128), typeName: publicString(value.typeName, 128),
    defaulted: value.defaulted, supported: value.supported,
    unsupportedReason: publicString(value.unsupportedReason, 300), suggestedCode: publicString(value.suggestedCode, 64),
    suggestedLogicalType: publicString(value.suggestedLogicalType, 32), suggestedControlType: publicString(value.suggestedControlType, 32),
    suggestedSystemValue: publicString(value.suggestedSystemValue, 32), role,
  }
}

function parseReportResultTableSummary(value: unknown, fallbackColumnCount?: number): ReportResultTableSummary {
  if (!isRecord(value)) throw new Error('invalid result table')
  const owner = publicString(value.owner, 128)
  const name = publicString(value.name, 128)
  const columnCount = value.columnCount === undefined && fallbackColumnCount !== undefined
    ? fallbackColumnCount
    : strictNonNegativeInteger(value.columnCount)
  const qualifiedName = publicString(value.qualifiedName, 257) || (owner && name ? `${owner}.${name}` : '')
  if (!owner || !name || !qualifiedName) throw new Error('invalid result table')
  return { owner, name, columnCount, qualifiedName }
}

function parseReportResultTableColumn(value: unknown): ReportResultTableColumn {
  if (!isRecord(value)) throw new Error('invalid result table column')
  const name = publicString(value.name, 128)
  const position = positiveInteger(value.position)
  const oracleType = publicString(value.oracleType, 64)
  if (!name || !position || !oracleType || typeof value.nullable !== 'boolean') throw new Error('invalid result table column')
  return {
    name,
    position,
    oracleType,
    dataLength: nullableInteger(value.dataLength),
    precision: nullableInteger(value.precision),
    scale: nullableInteger(value.scale),
    nullable: value.nullable,
  }
}

function parseReportColumn(value: unknown): ReportColumn | null {
  if (!isRecord(value)) return null
  const fieldId = publicString(value.fieldId, 36)
  const logicalCode = publicString(value.logicalCode, 64)
  const databaseColumn = publicString(value.databaseColumn, 128)
  if (!fieldId || !logicalCode || !databaseColumn) return null
  return {
    fieldId, logicalCode, databaseColumn, sourceOracleType: publicString(value.sourceOracleType, 64), precision: nullableInteger(value.precision), scale: nullableInteger(value.scale), nullable: value.nullable === true,
    valueType: publicString(value.valueType, 32), previewHeader: publicString(value.previewHeader, 255), excelHeader: publicString(value.excelHeader, 255), displayOrder: nonNegativeInteger(value.displayOrder), exportOrder: nonNegativeInteger(value.exportOrder),
    previewVisible: value.previewVisible === true, exportVisible: value.exportVisible === true, filterable: value.filterable === true, sortable: value.sortable === true, exportAllowed: value.exportAllowed === true,
    allowedOperators: value.allowedOperators, format: value.format, dictionaryVersion: value.dictionaryVersion, maskingPolicy: value.maskingPolicy, excelWidth: typeof value.excelWidth === 'number' ? value.excelWidth : 0, nullDisplay: publicString(value.nullDisplay, 64),
  }
}

function parseReportGrant(value: unknown): ReportGrant | null {
  if (!isRecord(value) || (value.subjectType !== 'USER' && value.subjectType !== 'ROLE')) return null
  const subjectId = positiveInteger(value.subjectId)
  const actions = Array.isArray(value.actions) ? value.actions.filter((item): item is string => item === 'QUERY' || item === 'EXPORT') : []
  return subjectId && actions.length ? { subjectType: value.subjectType, subjectId, actions } : null
}

function serializeReportDraft(draft: ReportDraft, creating: boolean) {
  const jsonTableSnapshot = draft.executionMode === 'TABLE_SNAPSHOT' && Boolean(draft.procedure.jsonInputArgName)
  const jsonInput = draft.executionMode === 'REF_CURSOR' || jsonTableSnapshot
  return {
    code: draft.code, name: draft.name, category: draft.category, description: draft.description, datasourceId: draft.datasourceId,
    ...(creating ? {} : { expectedLockVersion: draft.lockVersion }), executionMode: draft.executionMode, procedure: draft.procedure,
    inputSchema: jsonInput ? reportInputSchemaDocument(draft.inputSchema) : undefined,
    result: draft.executionMode === 'REF_CURSOR' ? {} : { tableOwner: draft.result.tableOwner, tableName: draft.result.tableName },
    callTemplate: jsonTableSnapshot ? draft.callTemplate : jsonInput ? '' : draft.callTemplate,
    parameters: jsonInput ? [] : draft.parameters.map((parameter) => ({ ...parameter, defaultValue: parameter.sensitive ? undefined : parameter.defaultValue, allowedValues: parameter.allowedValues.length ? parameter.allowedValues : undefined, validation: Object.keys(parameter.validation).length ? parameter.validation : undefined, normalizer: Object.keys(parameter.normalizer).length ? parameter.normalizer : undefined, valueSource: Object.keys(parameter.valueSource).length ? parameter.valueSource : undefined, nullPolicy: parameter.nullPolicy || 'TYPED_NULL' })),
    columns: draft.columns,
    grants: draft.grants,
  }
}

function serializeReportDatasource(input: ReportDatasourceInput) {
  return {
    code: input.code,
    name: input.name,
    host: input.host,
    port: input.port,
    serviceName: input.serviceName,
    sid: input.sid,
    username: input.username,
    password: input.password || undefined,
    sessionTimezone: input.sessionTimezone,
    connectTimeoutSeconds: input.connectTimeoutSeconds,
    queryTimeoutSeconds: input.queryTimeoutSeconds,
    maxOpenConnections: input.maxOpenConnections,
    maxIdleConnections: input.maxIdleConnections,
    prefetchRows: input.prefetchRows,
    arraySize: input.arraySize,
    enabled: input.enabled,
  }
}

export function parsePublication(payload: unknown): ReportPublication {
  const data = unwrapData(payload)
  const definitionId = positiveInteger(data.definitionId)
  const versionId = positiveInteger(data.versionId)
  const version = positiveInteger(data.version)
  const contractHash = safeHash(data.contractHash)
  const validation = data.validation === undefined || data.validation === null ? null : parseReportValidationSummary(data.validation)
  if (!definitionId || !versionId || !version || !contractHash) throw new Error('invalid publication')
  return { definitionId, versionId, version, status: publicString(data.status, 32), contractHash, publishedAt: publicDate(data.publishedAt), validation }
}

function parseReportValidationSummary(value: unknown) {
  if (!isRecord(value)) throw new Error('invalid publication validation')
  const validation = value
  const procedure = isRecord(validation.procedure) ? validation.procedure : {}
  const result = isRecord(validation.result) ? validation.result : {}
  const snapshot = isRecord(validation.snapshot) ? validation.snapshot : {}
  const reportExport = isRecord(validation.export) ? validation.export : {}
  const validatedAt = publicDate(validation.validatedAt)
  const procedureSignatureHash = safeHash(procedure.signatureHash)
  const resultSchemaHash = safeHash(result.schemaHash)
  const exportSchemaHash = safeHash(reportExport.schemaHash)
  const argumentCount = positiveInteger(procedure.argumentCount)
  const columnCount = positiveInteger(result.columnCount)
  const exportableColumnCount = positiveInteger(reportExport.exportableColumnCount)
  const owner = publicString(procedure.owner, 128)
  const name = publicString(procedure.name, 128)
  const tableOwner = publicString(result.tableOwner, 128)
  const tableName = publicString(result.tableName, 128)
  if (!validatedAt || !procedureSignatureHash || !resultSchemaHash || !exportSchemaHash || !owner || !name || !tableOwner || !tableName || argumentCount === null || columnCount === null || exportableColumnCount === null || snapshot.resultTableValidated !== true) throw new Error('invalid publication validation')
  return {
      validatedAt,
      procedure: {
        owner,
        package: publicString(procedure.package, 128),
        name,
        overload: publicString(procedure.overload, 32),
        argumentCount,
        signatureHash: procedureSignatureHash,
      },
      result: {
        tableOwner,
        tableName,
        columnCount,
        schemaHash: resultSchemaHash,
      },
      snapshot: {
        resultTableValidated: true,
      },
      export: {
        exportableColumnCount,
        schemaHash: exportSchemaHash,
      },
  }
}

const reportVersionDiffContract = {
  procedure: { label: '存储过程', changes: { procedureSignatureHash: '过程签名' } },
  parameters: { label: '筛选条件', legacyLabel: '{{形参}}', changes: { parameterCount: '条件数量', parameterSchemaHash: '条件 Schema' } },
  results: { label: '结果字段与 Excel', changes: { columnCount: '字段数量', resultSchemaHash: '结果 Schema' } },
  excel: { label: 'Excel 契约', changes: { exportSchemaHash: 'Excel Schema' } },
  permissions: { label: '权限', changes: { grantCount: '授权数量', permissionHash: '权限契约' } },
} as const

type ReportVersionSectionKey = keyof typeof reportVersionDiffContract

export function parseReportVersionPage(payload: unknown): ReportVersionPage {
  const data = unwrapData(payload)
  if (!Array.isArray(data.items) || typeof data.hasMore !== 'boolean') throw new Error('invalid report version page')
  const items = data.items.map(parseReportVersionSummary)
  const nextAfterId = nonNegativeInteger(data.nextAfterId)
  if (items.some((item, index) => index > 0 && item.id >= items[index - 1].id)) throw new Error('invalid report version order')
  if (data.hasMore && (!items.length || nextAfterId !== items[items.length - 1].id)) throw new Error('invalid report version cursor')
  if (!data.hasMore && nextAfterId !== 0 && (!items.length || nextAfterId !== items[items.length - 1].id)) throw new Error('invalid report version cursor')
  return { items, hasMore: data.hasMore, nextAfterId }
}

export function parseReportVersionDiff(payload: unknown): ReportVersionDiff {
  const data = unwrapData(payload)
  const base = parseReportVersionSummary(data.base)
  const target = parseReportVersionSummary(data.target)
  if (base.id === target.id || !Array.isArray(data.sections) || data.sections.length !== Object.keys(reportVersionDiffContract).length) throw new Error('invalid report version diff')
  const seenSections = new Set<string>()
  const sections = data.sections.map((section) => {
    if (!isRecord(section) || !Array.isArray(section.changes)) throw new Error('invalid report version diff')
    const key = publicString(section.key, 64) as ReportVersionSectionKey
    const contract = reportVersionDiffContract[key]
    const labelMatches = contract && (section.label === contract.label || ('legacyLabel' in contract && section.label === contract.legacyLabel))
    if (!contract || seenSections.has(key) || !labelMatches) throw new Error('invalid report version diff')
    seenSections.add(key)
    const seenChanges = new Set<string>()
    const changes = section.changes.map((change) => {
      if (!isRecord(change) || change.kind !== 'CHANGED') throw new Error('invalid report version change')
      const changeKey = publicString(change.key, 64)
      const changeLabel = contract.changes[changeKey as keyof typeof contract.changes]
      const legacyChangeLabel = key === 'parameters' ? ({ parameterCount: '参数数量', parameterSchemaHash: '参数 Schema' } as const)[changeKey as 'parameterCount' | 'parameterSchemaHash'] : undefined
      if (!changeLabel || seenChanges.has(changeKey) || (change.label !== changeLabel && change.label !== legacyChangeLabel)) throw new Error('invalid report version change')
      seenChanges.add(changeKey)
      const countChange = changeKey.endsWith('Count')
      const before = countChange ? strictNonNegativeInteger(change.before) : shortVersionHash(change.before)
      const after = countChange ? strictNonNegativeInteger(change.after) : shortVersionHash(change.after)
      return { kind: 'CHANGED' as const, key: changeKey, label: changeLabel, before, after }
    })
    return { key, label: contract.label, changes }
  })
  return { base, target, sections }
}

export function parseReportVersionDetail(payload: unknown): ReportVersionDetail {
	const data = unwrapData(payload)
	const summary = parseReportVersionSummary(data.summary)
	if (!isRecord(data.configuration)) throw new Error('invalid report version configuration')
	const draft = parseReportDraft({ data: {
		...data.configuration,
		id: summary.id,
		code: 'historical_version',
		name: '历史版本',
		status: 'DRAFT',
		lockVersion: summary.version,
	} })
	return {
		summary,
		configuration: {
			datasourceId: draft.datasourceId, procedure: draft.procedure, executionMode: draft.executionMode,
			inputSchema: draft.inputSchema, result: draft.result, callTemplate: draft.callTemplate,
			parameters: draft.parameters, columns: draft.columns, grants: draft.grants,
		},
	}
}

function parseReportVersionSummary(value: unknown): ReportVersionSummary {
  if (!isRecord(value)) throw new Error('invalid report version')
  const id = positiveInteger(value.id)
  const version = positiveInteger(value.version)
  const contractFingerprint = shortVersionHash(value.contractFingerprint)
  if (!id || !version || value.status !== 'PUBLISHED') throw new Error('invalid report version')
  return {
    id,
    version,
    status: value.status,
    publishedAt: publicDate(value.publishedAt),
    contractFingerprint,
    parameterCount: strictNonNegativeInteger(value.parameterCount),
    columnCount: strictNonNegativeInteger(value.columnCount),
    grantCount: strictNonNegativeInteger(value.grantCount),
  }
}

function parseReportSummary(value: unknown): ReportSummary | null {
  if (!isRecord(value)) return null
  const definition = isRecord(value.definition) ? value.definition : value
  const id = positiveInteger(definition.id)
  const code = publicString(definition.code, 64)
  const name = publicString(definition.name, 128)
  if (!id || !code || !name) return null
  return {
    id,
    code,
    name,
    category: publicString(definition.category, 64),
    description: publicString(definition.description, 500),
    datasourceId: positiveInteger(definition.datasourceId) ?? 0,
    status: reportStatus(definition.status),
    currentDraftVersionId: positiveInteger(definition.currentDraftVersionId) ?? 0,
    currentPublishedVersionId: positiveInteger(definition.currentPublishedVersionId) ?? 0,
    lockVersion: positiveInteger(value.lockVersion) ?? 0,
    isOwner: value.isOwner === true,
    updatedAt: publicDate(definition.updatedAt),
  }
}

function unwrapData(payload: unknown): JsonRecord {
  if (!isRecord(payload)) return {}
  return isRecord(payload.data) ? payload.data : payload
}

function firstArray(...values: unknown[]) {
  return values.find(Array.isArray) ?? []
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function publicString(value: unknown, maximumLength: number) {
  return typeof value === 'string' ? value.trim().slice(0, maximumLength) : ''
}

function safeHash(value: unknown) {
  const hash = publicString(value, 128)
  return /^[a-f\d]{64}$/i.test(hash) ? hash : ''
}

function positiveInteger(value: unknown) {
  if (typeof value === 'number' && Number.isSafeInteger(value) && value > 0) return value
  if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) return null
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : null
}

function nonNegativeInteger(value: unknown) {
  if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return value
  if (typeof value !== 'string' || !/^\d+$/.test(value)) return 0
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : 0
}

function strictNonNegativeInteger(value: unknown) {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new Error('invalid non-negative integer')
  return value
}

function shortVersionHash(value: unknown) {
  if (typeof value !== 'string' || !/^[a-f\d]{12}$/i.test(value)) throw new Error('invalid report version fingerprint')
  return value
}

function nullableInteger(value: unknown) {
  if (typeof value === 'number' && Number.isSafeInteger(value)) return value
  return null
}

function publicDate(value: unknown) {
  if (typeof value !== 'string' || !value.trim()) return null
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? null : value
}

function reportStatus(value: unknown): ReportDefinitionStatus {
  return value === 'ACTIVE' || value === 'DISABLED' ? value : 'DRAFT'
}

function reportRunStatus(value: unknown): ReportRunStatus {
  const allowed: ReportRunStatus[] = ['QUEUED', 'RUNNING', 'CANCEL_REQUESTED', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'UNKNOWN', 'RECONCILING', 'EXPORTING', 'EXPORTED', 'RESULT_PURGING', 'RESULT_PURGED', 'SUPERSEDED']
  return typeof value === 'string' && allowed.includes(value as ReportRunStatus) ? value as ReportRunStatus : 'UNKNOWN'
}

function reportExportStatus(value: unknown): ReportExport['status'] {
  const allowed: ReportExport['status'][] = ['PENDING', 'RUNNING', 'READY', 'FAILED', 'CANCELLED', 'EXPIRED']
  return typeof value === 'string' && allowed.includes(value as ReportExport['status']) ? value as ReportExport['status'] : 'FAILED'
}

function reportControlType(value: unknown): ReportParameter['controlType'] {
  const allowed: ReportParameter['controlType'][] = ['TEXT', 'TEXTAREA', 'NUMBER', 'CHECKBOX', 'DATE', 'DATETIME', 'SELECT', 'MULTI_SELECT']
  return typeof value === 'string' && allowed.includes(value as ReportParameter['controlType']) ? value as ReportParameter['controlType'] : 'TEXT'
}

function reportLogicalType(value: unknown): ReportParameter['logicalType'] {
  const allowed: ReportParameter['logicalType'][] = ['string', 'integer', 'decimal', 'boolean', 'date', 'datetime', 'enum', 'multi_enum', 'json']
  return typeof value === 'string' && allowed.includes(value as ReportParameter['logicalType']) ? value as ReportParameter['logicalType'] : 'string'
}
