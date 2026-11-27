import { useEffect, useState } from 'react'
import type { ExcelPageClient } from './useExcelUploads'

type Catalog<T> = { path: string; items: T[]; error: string }

export function useOracleImportCatalog<T>(client: ExcelPageClient, path: string | null, parse: (payload: unknown) => T[], failureMessage: string) {
 const [result, setResult] = useState<Catalog<T> | null>(null)
 const [attempt, setAttempt] = useState(0)

 useEffect(() => {
  if (!path) return
  const controller = new AbortController()
  // The table search and manually entered target can both change while typing.
  const timer = window.setTimeout(async () => {
   try {
    const response = await client(path, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
    if (controller.signal.aborted) return
    if (!response.ok) throw new Error(response.error?.message || failureMessage)
    setResult({ path, items: parse(response.data), error: '' })
   } catch (cause) {
    if (!controller.signal.aborted) setResult({ path, items: [], error: cause instanceof Error ? cause.message : failureMessage })
   }
  }, 300)
  return () => { window.clearTimeout(timer); controller.abort() }
 }, [attempt, client, failureMessage, parse, path])

 const current = result?.path === path ? result : null
 function retry() { setResult(null); setAttempt(value => value + 1) }
 return { items: current?.items ?? [], error: current?.error ?? '', loading: Boolean(path && !current), retry }
}
