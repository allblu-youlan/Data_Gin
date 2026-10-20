import { useEffect, useState } from 'react'
import { getBojunColumns, getBojunTable } from '../../bojunApi'
import type { ReportCenterClient } from '../../api'
import type { BojunColumn } from '../../types'
import type { BojunMetadata } from './BojunMetadata'

export function useBojunMetadata(client: ReportCenterClient, datasourceId: number, tableIds: string[], reloadKey = 0) {
  const identity = [...new Set(tableIds.filter(Boolean))].sort().join(',')
  const [metadata, setMetadata] = useState<BojunMetadata>({})
  const [state, setState] = useState({ loading: false, error: '' })
  useEffect(() => {
    const controller = new AbortController()
    setMetadata({})
    if (!datasourceId || !identity) { setState({ loading: false, error: '' }); return }
    setState({ loading: true, error: '' })
    void Promise.all(identity.split(',').map(async (tableId) => {
      const tableResponse = await getBojunTable(client, datasourceId, tableId, controller.signal)
      if (!tableResponse.ok) throw new Error(tableResponse.error)
      const columns: BojunColumn[] = []
      let after = ''
      do {
        const response = await getBojunColumns(client, datasourceId, tableId, { after, limit: 200 }, controller.signal)
        if (!response.ok) throw new Error(response.error)
        columns.push(...response.data.items)
        after = response.data.hasMore ? response.data.nextAfter : ''
        if (response.data.hasMore && !after) throw new Error('字段分页游标缺失，请重新同步字典。')
      } while (after && !controller.signal.aborted)
      return [tableId, { table: tableResponse.data, columns }] as const
    })).then((entries) => { if (!controller.signal.aborted) { setMetadata(Object.fromEntries(entries)); setState({ loading: false, error: '' }) } }).catch((error: unknown) => { if (!controller.signal.aborted) setState({ loading: false, error: error instanceof Error ? error.message : '字典读取失败。' }) })
    return () => controller.abort()
  }, [client, datasourceId, identity, reloadKey])
  return { metadata, ...state }
}

