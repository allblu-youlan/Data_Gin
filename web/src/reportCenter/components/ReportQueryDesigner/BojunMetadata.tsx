import { useEffect, useRef, useState } from 'react'
import Select from 'antd/es/select'
import { getBojunTables } from '../../bojunApi'
import { Button } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import type { BojunColumn, BojunTable } from '../../types'
import styles from './ReportQueryDesigner.module.css'

export type BojunMetadata = Record<string, { table: BojunTable; columns: BojunColumn[] }>

export function BojunTableSelect({ client, datasourceId, value, selected, onChange, disabled, label = '选择伯俊表', availableOnly = true }: { client: ReportCenterClient; datasourceId: number; value: string; selected?: BojunTable; onChange: (table: BojunTable) => void; disabled?: boolean; label?: string; availableOnly?: boolean }) {
  const [search, setSearch] = useState('')
  const [items, setItems] = useState<BojunTable[]>([])
  const [cursor, setCursor] = useState('')
  const [state, setState] = useState({ loading: false, error: '' })
  const [refresh, setRefresh] = useState(0)
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setItems([]); setCursor('')
    if (!datasourceId) { setState({ loading: false, error: '' }); return }
    setState({ loading: true, error: '' })
    const timer = window.setTimeout(() => {
      void getBojunTables(client, datasourceId, { search, availableOnly, limit: 50 }, controller.signal).then((response) => {
        if (controller.signal.aborted) return
        if (!response.ok) { setState({ loading: false, error: response.error }); return }
        setItems(response.data.items); setCursor(response.data.hasMore ? response.data.nextAfter : ''); setState({ loading: false, error: '' })
      })
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort(); request.current?.abort() }
  }, [client, datasourceId, search, availableOnly, refresh])
  async function more() {
    if (!cursor || state.loading) return
    request.current?.abort(); const controller = new AbortController(); request.current = controller
    setState({ loading: true, error: '' })
    const response = await getBojunTables(client, datasourceId, { search, availableOnly, after: cursor, limit: 50 }, controller.signal)
    if (controller.signal.aborted) return
    if (!response.ok) { setState({ loading: false, error: response.error }); return }
    setItems((current) => [...current, ...response.data.items]); setCursor(response.data.hasMore ? response.data.nextAfter : ''); setState({ loading: false, error: '' })
  }
  const available = (table: BojunTable) => table.enabled && table.validationStatus === 'VALID'
  const listed = availableOnly ? items.filter(available) : items
  const options = selected && !listed.some((item) => item.id === selected.id) ? [selected, ...listed] : listed
  const unavailableSelection = availableOnly && selected && !available(selected)
  const empty = search ? '没有匹配的表，请调整搜索关键词。' : availableOnly ? '暂无可用表，请先在伯俊字典中同步、核验并启用表。' : '暂无已同步的表，请先同步伯俊字典。'
  return <div className={styles.editor}><Select aria-label={label} value={value || undefined} disabled={disabled || !datasourceId} showSearch searchValue={search} filterOption={false} onSearch={setSearch} onOpenChange={(open) => { if (open) { setSearch(''); setRefresh((current) => current + 1) } }} getPopupContainer={(trigger) => trigger.parentElement ?? trigger} loading={state.loading} placeholder="按表名或业务名称搜索" className={styles.select} options={options.map((table) => ({ value: String(table.id), disabled: availableOnly && !available(table), label: `${table.logicalName} · ${table.description || '无说明'}${availableOnly && !available(table) ? '（当前不可用）' : !table.enabled ? '（未启用）' : ''}` }))} onChange={(id) => { const table = options.find((item) => String(item.id) === id); if (table && (!availableOnly || available(table))) onChange(table) }} onPopupScroll={(event) => { const element = event.currentTarget; if (element.scrollTop + element.clientHeight >= element.scrollHeight - 24) void more() }} notFoundContent={state.loading ? '正在读取可用表…' : state.error || empty} />{unavailableSelection ? <small className={styles.error} role="status">已选表当前不可用，请在字典中核验后启用，或更换表。</small> : null}{state.error ? <div className={styles.actions}><small className={styles.error} role="alert">{state.error}</small><Button disabled={state.loading} onClick={() => setRefresh((current) => current + 1)}>重新读取</Button></div> : null}{cursor ? <Button onClick={() => void more()} disabled={state.loading}>加载更多表</Button> : null}</div>
}
