import { useEffect, useState } from 'react'
import { DataTable, Drawer, FeedbackState, StatusTag } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { getReportRecordDetail } from '../../bojunApi'
import type { ReportRecordDetail } from '../../types'
import styles from '../ReportQueryDesigner/ReportQueryDesigner.module.css'

export type RecordDetailSelection = { runId: number; rowKey: string; relationId: string; trigger: HTMLElement }
export function ReportRecordDetailDrawer({ client, selection, onClose }: { client: ReportCenterClient; selection: RecordDetailSelection | null; onClose: () => void }) {
  const [mode, setMode] = useState<'SNAPSHOT' | 'LIVE'>('SNAPSHOT')
  const [detail, setDetail] = useState<ReportRecordDetail | null>(null)
  const [state, setState] = useState({ loading: false, error: '' })
  const [reload, setReload] = useState(0)
  useEffect(() => { setMode('SNAPSHOT') }, [selection])
  useEffect(() => {
    setDetail(null)
    if (!selection) return
    const controller = new AbortController()
    setState({ loading: true, error: '' })
    void getReportRecordDetail(client, selection.runId, selection.rowKey, selection.relationId, mode, controller.signal).then((response) => {
      if (controller.signal.aborted) return
      if (!response.ok) { setState({ loading: false, error: response.error }); return }
      setDetail(response.data); setState({ loading: false, error: '' })
    })
    return () => controller.abort()
  }, [client, mode, selection, reload])
  return <Drawer open={Boolean(selection)} title={detail?.relationName || '关联记录详情'} description="默认查看本次报表快照；当前源记录可能已发生变化。" returnFocus={selection?.trigger} onClose={onClose} size="medium"><div className={styles.editor}><div className={styles.actions}><button type="button" aria-pressed={mode === 'SNAPSHOT'} onClick={() => setMode('SNAPSHOT')}>本次运行快照</button><button type="button" aria-pressed={mode === 'LIVE'} onClick={() => setMode('LIVE')}>查看当前源记录</button><StatusTag tone={mode === 'LIVE' ? 'warning' : 'info'}>{mode === 'LIVE' ? '当前源数据' : '运行快照'}</StatusTag></div>{detail ? <p className={styles.notice} role="status">{detail.detailSource || mode}{detail.queriedAt ? ` · 查询时间 ${new Date(detail.queriedAt).toLocaleString('zh-CN')}` : ''}{mode === 'LIVE' ? ' · 数据可能与报表生成时不同' : ' · 与本次导出使用相同快照'}</p> : null}{state.loading ? <FeedbackState kind="loading" title="正在读取受权记录" /> : state.error ? <FeedbackState kind="error" title="记录详情暂不可用" description={state.error} action={<button type="button" onClick={() => setReload((value) => value + 1)}>重试</button>} /> : detail ? <DataTable minWidth={360} scrollLabel="关联记录只读字段"><thead><tr><th>字段</th><th>显示值</th><th>原始值</th></tr></thead><tbody>{detail.fields.map((field, index) => <tr key={`${field.columnId}-${index}`}><td>{field.label || field.name || field.columnId}</td><td>{display('displayValue' in field ? field.displayValue : field.rawValue)}</td><td>{'rawValue' in field ? display(field.rawValue) : '未授权'}</td></tr>)}</tbody></DataTable> : null}</div></Drawer>
}
function display(value: unknown) { return value === null || value === undefined ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value) }
