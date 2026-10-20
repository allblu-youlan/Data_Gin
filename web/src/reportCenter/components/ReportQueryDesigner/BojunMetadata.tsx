import { useEffect, useRef, useState } from 'react'
import Select from 'antd/es/select'
import { getBojunTables } from '../../bojunApi'
import type { ReportCenterClient } from '../../api'
import type { BojunColumn, BojunTable } from '../../types'
import styles from './ReportQueryDesigner.module.css'

export type BojunMetadata = Record<string, { table: BojunTable; columns: BojunColumn[] }>

export function BojunTableSelect({ client, datasourceId, value, selected, onChange, disabled, label = '选择伯俊表' }: { client: ReportCenterClient; datasourceId: number; value: string; selected?: BojunTable; onChange: (table: BojunTable) => void; disabled?: boolean; label?: string }) {
  const [search, setSearch] = useState('')
  const [items, setItems] = useState<BojunTable[]>([])
  const [cursor, setCursor] = useState('')
  const [state, setState] = useState({ loading: false, error: '' })
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setItems([]); setCursor('')
    if (!datasourceId) return
    const timer = window.setTimeout(() => {
      setState({ loading: true, error: '' })
      void getBojunTables(client, datasourceId, { search, limit: 50 }, controller.signal).then((response) => {
        if (controller.signal.aborted) return
        if (!response.ok) { setState({ loading: false, error: response.error }); return }
        setItems(response.data.items); setCursor(response.data.hasMore ? response.data.nextAfter : ''); setState({ loading: false, error: '' })
      })
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [client, datasourceId, search])
  async function more() {
    if (!cursor || state.loading) return
    request.current?.abort(); const controller = new AbortController(); request.current = controller
    setState({ loading: true, error: '' })
    const response = await getBojunTables(client, datasourceId, { search, after: cursor, limit: 50 }, controller.signal)
    if (controller.signal.aborted) return
    if (!response.ok) { setState({ loading: false, error: response.error }); return }
    setItems((current) => [...current, ...response.data.items]); setCursor(response.data.hasMore ? response.data.nextAfter : ''); setState({ loading: false, error: '' })
  }
  const options = selected && !items.some((item) => item.id === selected.id) ? [selected, ...items] : items
  return <div><Select aria-label={label} value={value || undefined} disabled={disabled || !datasourceId} showSearch filterOption={false} onSearch={setSearch} loading={state.loading} placeholder="按表名或显示名称搜索" className={styles.select} options={options.map((table) => ({ value: String(table.id), label: `${table.logicalName} · ${table.description || '无说明'}${table.enabled ? '' : '（未启用）'}` }))} onChange={(id) => { const table = options.find((item) => String(item.id) === id); if (table) onChange(table) }} onPopupScroll={(event) => { const element = event.currentTarget; if (element.scrollTop + element.clientHeight >= element.scrollHeight - 24) void more() }} notFoundContent={state.loading ? '正在读取…' : state.error || '没有匹配的可用目录'} />{state.error ? <small role="alert">{state.error}</small> : null}{cursor ? <button type="button" onClick={() => void more()} disabled={state.loading}>加载更多表</button> : null}</div>
}
