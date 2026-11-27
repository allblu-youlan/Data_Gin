import type { ReportCenterClient } from '../reportCenter/api'
import type { HTTPMethod } from '../api/client'

export class BoardRequestError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

export async function boardRequest<T>(client: ReportCenterClient, path: string, method: HTTPMethod = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
  const reply = await client('/v1/table-boards' + path, { method, body, signal, timeoutMs: 65000, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
  if (!reply.ok) throw new BoardRequestError(reply.status, reply.error?.message || '请求失败，请重试。')
  const data = reply.data as { data?: T }
  if (!data || data.data === undefined) throw new Error('看板接口返回的数据格式无效。')
  return data.data
}
export function saveDownload(content: string, name: string, format: 'xlsx' | 'csv') {
  const bytes = Uint8Array.from(atob(content), c => c.charCodeAt(0))
  const type = format === 'csv' ? 'text/csv;charset=utf-8' : 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
  const url = URL.createObjectURL(new Blob([bytes], { type }))
  const link = document.createElement('a')
  link.href = url
  link.download = name + '.' + format
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
