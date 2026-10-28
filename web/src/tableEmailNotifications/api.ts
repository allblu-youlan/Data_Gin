import type { WorkspaceApiClient, WorkspaceApiClientOptions } from '../appShell/WorkspaceRouter'
import { parseNotificationColumns, parseNotificationDatasources, parseNotificationDeliveries, parseNotificationPreview, parseNotificationRules, parseNotificationTables } from './contracts'

const basePath = '/v1/table-email-notifications'
type Result<T> = { ok: true; data: T } | { ok: false; error: string }

async function request<T>(client: WorkspaceApiClient, path: string, options: WorkspaceApiClientOptions, parser: (value: unknown) => T): Promise<Result<T>> {
  try {
    const result = await client(`${basePath}${path}`, { ...options, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
    if (!result.ok) return { ok: false, error: result.error?.message || '请求失败，请稍后重试。' }
    return { ok: true, data: parser(result.data) }
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : '请求失败，请稍后重试。' }
  }
}

export function getNotificationRules(client: WorkspaceApiClient, signal?: AbortSignal) {
  return request(client, '', { method: 'GET', signal }, parseNotificationRules)
}
export function getNotificationDatasources(client: WorkspaceApiClient, signal?: AbortSignal) {
  return request(client, '/datasources', { method: 'GET', signal }, parseNotificationDatasources)
}
export function getNotificationTables(client: WorkspaceApiClient, datasourceId: number, owner: string, search: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ datasourceId: String(datasourceId), owner: owner.trim(), search: search.trim() })
  return request(client, `/tables?${query}`, { method: 'GET', signal }, parseNotificationTables)
}
export function getNotificationColumns(client: WorkspaceApiClient, datasourceId: number, owner: string, tableName: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ datasourceId: String(datasourceId), owner, tableName })
  return request(client, `/columns?${query}`, { method: 'GET', signal }, parseNotificationColumns)
}
export function saveNotificationRule(client: WorkspaceApiClient, id: number | null, body: unknown) {
  return request(client, id ? `/${id}` : '', { method: id ? 'PUT' : 'POST', body }, () => undefined)
}
export function deleteNotificationRule(client: WorkspaceApiClient, id: number, lockVersion: number) {
  return request(client, `/${id}?expectedLockVersion=${lockVersion}`, { method: 'DELETE' }, () => undefined)
}
export function previewNotification(client: WorkspaceApiClient, body: unknown) {
  return request(client, '/preview', { method: 'POST', body }, parseNotificationPreview)
}
export function getNotificationDeliveries(client: WorkspaceApiClient, ruleId: number, signal?: AbortSignal) {
  return request(client, `/deliveries?ruleId=${ruleId}`, { method: 'GET', signal }, parseNotificationDeliveries)
}
export function retryNotificationDelivery(client: WorkspaceApiClient, id: number) {
  return request(client, `/deliveries/${id}/retry`, { method: 'POST', body: {} }, () => undefined)
}
