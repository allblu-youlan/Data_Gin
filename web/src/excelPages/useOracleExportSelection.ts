import { useCallback, useEffect, useRef, useState } from 'react'
import { parseOracleExportColumns, type OracleExportColumnDraft, type OracleExportFilterDraft, type OracleExportTable, type OracleExportTableConfig } from './oracleExportConfig'
import { restoreOracleExportConfig } from './oracleExportSchemes'
import type { ExcelPageClient } from './useExcelUploads'

export function useOracleExportSelection(client: ExcelPageClient) {
  const [table, setTable] = useState<OracleExportTable | null>(null)
  const [columns, setColumns] = useState<OracleExportColumnDraft[]>([])
  const [filters, setFilters] = useState<OracleExportFilterDraft[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [issues, setIssues] = useState<string[]>([])
  const request = useRef<AbortController | null>(null)
  const retryConfig = useRef<OracleExportTableConfig | undefined>()
  const filterID = useRef(0)

  useEffect(() => () => request.current?.abort(), [])

  const load = useCallback(async (nextTable: OracleExportTable | null, config?: OracleExportTableConfig) => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    retryConfig.current = config
    setTable(nextTable)
    setColumns([])
    setFilters([])
    setError('')
    setIssues([])
    setLoading(Boolean(nextTable))
    if (!nextTable) return true
    const params = new URLSearchParams({ owner: nextTable.owner, name: nextTable.name })
    try {
      const response = await client(`/v1/excel-match-jobs/oracle-columns?${params}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
      if (controller.signal.aborted) return false
      if (!response.ok) throw new Error(response.error?.message || 'Oracle 字段加载失败，请重试。')
      const metadata = parseOracleExportColumns(response.data)
      const restored = config ? restoreOracleExportConfig(config, metadata) : { columns: metadata, filters: [], issues: [] }
      setColumns(restored.columns)
      setFilters(restored.filters)
      setIssues(restored.issues)
      filterID.current = restored.filters.length
      return true
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Oracle 字段加载失败，请重试。')
      return false
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [client])

  function removeInvalidReferences() {
    const available = new Set(columns.map((column) => column.name))
    setFilters((current) => current.filter((filter) => available.has(filter.field)))
    setIssues([])
    retryConfig.current = undefined
  }

  return {
    table, columns, setColumns, filters, setFilters, loading, error, issues, load,
    retry: () => load(table, retryConfig.current),
    addFilter: (defaults: Partial<Pick<OracleExportFilterDraft, 'op' | 'valueSource'>> = {}) => {
      const id = ++filterID.current
      setFilters((current) => [...current, { id, field: '', op: 'eq', value: '', ...defaults }])
    },
    removeInvalidReferences,
  }
}
