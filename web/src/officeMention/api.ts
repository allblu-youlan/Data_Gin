import { useEffect, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { parseMentionData, parseMentionPage } from './contracts'
import type { Page } from './types'
export const mentionRoot = '/v1/feishu-mention'
export async function mentionRequest<T>(client: WorkspaceApiClient, path: string, options: { method?: 'GET' | 'POST' | 'PUT'; body?: unknown; signal?: AbortSignal; paginated?: boolean } = {}): Promise<T> {
  const response = await client(`${mentionRoot}${path}`, { method: options.method ?? 'GET', body: options.body, signal: options.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, retry: false })
  if (!response.ok) throw new Error(response.error?.message || '操作失败，请稍后重试。')
  return options.paginated ? parseMentionPage(response.data) as T : parseMentionData<T>(response.data)
}
export function useMentionResource<T>(client: WorkspaceApiClient, path: string | null, reload = 0, paginated = false) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(Boolean(path))
  useEffect(() => {
    const controller = new AbortController()
    setData(null); setError(''); setLoading(Boolean(path))
    if (path) void mentionRequest<T>(client, path, { signal: controller.signal, paginated }).then((result) => { if (!controller.signal.aborted) setData(result) }).catch((reason: unknown) => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : '加载失败。') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [client, path, reload, paginated])
  return { data, error, loading }
}
export function useMentionPage<T>(client: WorkspaceApiClient, path: string, reload = 0) { return useMentionResource<Page<T>>(client, path, reload, true) }
