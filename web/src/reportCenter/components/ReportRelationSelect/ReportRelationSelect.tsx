import { useEffect, useRef, useState } from 'react'
import Select from 'antd/es/select'
import type { ReportCenterClient } from '../../api'
import { getReportRelationOptions } from '../../bojunApi'
import type { ReportRelationOption } from '../../types'
import styles from '../ReportQueryDesigner/ReportQueryDesigner.module.css'

export function ReportRelationSelect({ client, reportId, versionId, relationId, keyColumnId, value, onChange, disabled, required, inputId, multiple = false }: { client: ReportCenterClient; reportId: number; versionId: number; relationId: string; keyColumnId?: string; value: unknown; onChange: (value: unknown) => void; disabled: boolean; required: boolean; inputId: string; multiple?: boolean }) {
  const [search, setSearch] = useState('')
  const [options, setOptions] = useState<ReportRelationOption[]>([])
  const [cursor, setCursor] = useState('')
  const [state, setState] = useState({ loading: false, error: '' })
  const request = useRef<AbortController | null>(null)
  function optionValue(option: ReportRelationOption) { return keyColumnId ? option.keyValues[keyColumnId] ?? '' : Object.keys(option.keyValues).length === 1 ? Object.values(option.keyValues)[0] : JSON.stringify(option.keyValues) }
  useEffect(() => {
    request.current?.abort(); const controller = new AbortController(); request.current = controller
    setCursor('')
    const timer = window.setTimeout(() => {
      setState({ loading: true, error: '' })
      void getReportRelationOptions(client, reportId, { versionId, relationId, search, limit: 20 }, controller.signal).then((response) => {
        if (controller.signal.aborted) return
        if (!response.ok) { setState({ loading: false, error: response.error }); return }
        setOptions(response.data.items); setCursor(response.data.hasMore ? response.data.nextCursor : ''); setState({ loading: false, error: '' })
      })
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [client, reportId, versionId, relationId, search])
  async function loadMore() {
    if (!cursor || state.loading) return
    request.current?.abort(); const controller = new AbortController(); request.current = controller
    setState({ loading: true, error: '' })
    const response = await getReportRelationOptions(client, reportId, { versionId, relationId, search, cursor, limit: 20 }, controller.signal)
    if (controller.signal.aborted) return
    if (!response.ok) { setState({ loading: false, error: response.error }); return }
    setOptions((current) => [...current, ...response.data.items]); setCursor(response.data.hasMore ? response.data.nextCursor : ''); setState({ loading: false, error: '' })
  }
  const selected = Array.isArray(value) ? value.map(String) : value === '' || value === null || value === undefined ? undefined : String(value)
  return <div className={styles.editor}><Select id={inputId} className={styles.select} aria-label="搜索关联记录" aria-required={required} disabled={disabled} allowClear={!required} showSearch filterOption={false} mode={multiple ? 'multiple' : undefined} onSearch={setSearch} loading={state.loading} value={selected} options={options.map((option) => ({ value: optionValue(option), label: option.displayValue }))} onChange={(next: string | string[] | undefined) => onChange(next ?? (multiple ? [] : ''))} notFoundContent={state.loading ? '正在查询…' : state.error || '没有可访问的匹配记录'} onPopupScroll={(event) => { const element = event.currentTarget; if (element.scrollTop + element.clientHeight >= element.scrollHeight - 24) void loadMore() }} />{cursor ? <button type="button" disabled={state.loading} onClick={() => void loadMore()}>加载更多记录</button> : null}{state.error ? <small className={styles.error} role="alert">{state.error}</small> : <small className={styles.hint}>搜索源记录 · 显示名称，提交原始键</small>}</div>
}
