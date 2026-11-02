import { formatShanghaiDateTime } from '../../../dateTime.js'
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { RefreshCw, Search } from 'lucide-react'
import { DataTable, Drawer, FeedbackState, StatusTag } from '../../../ui'
import { getReportAudits, type ReportCenterClient } from '../../api'
import type { ReportAudit, ReportAuditQuery, ReportSummary } from '../../types'
import styles from './ReportAuditDrawer.module.css'

const defaultFilters: ReportAuditQuery = { limit: 50 }

export function ReportAuditDrawer({ client, open, reports, onClose }: {
  client: ReportCenterClient
  open: boolean
  reports: ReportSummary[]
  onClose: () => void
}) {
  const [draft, setDraft] = useState<ReportAuditQuery>(defaultFilters)
  const [filters, setFilters] = useState<ReportAuditQuery>(defaultFilters)
  const [items, setItems] = useState<ReportAudit[]>([])
  const [nextAfterId, setNextAfterId] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [state, setState] = useState({ loading: false, error: '' })
  const requestSequence = useRef(0)

  useEffect(() => {
    if (!open) requestSequence.current += 1
  }, [open])

  const load = useCallback(async (query: ReportAuditQuery, append = false, signal?: AbortSignal) => {
    const requestId = ++requestSequence.current
    setState({ loading: true, error: '' })
    if (!append) {
      setItems([])
      setHasMore(false)
      setNextAfterId(0)
    }
    const response = await getReportAudits(client, query, signal)
    if (signal?.aborted || requestId !== requestSequence.current) return
    if (!response.ok) {
      setState({ loading: false, error: response.error })
      return
    }
    setItems((current) => append ? [...current, ...response.data.items] : response.data.items)
    setHasMore(response.data.hasMore)
    setNextAfterId(response.data.nextAfterId)
    setState({ loading: false, error: '' })
  }, [client])

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    void load(filters, false, controller.signal)
    return () => controller.abort()
  }, [filters, load, open])

  function applyFilters(event: FormEvent) {
    event.preventDefault()
    setFilters({
      action: draft.action?.trim() || undefined,
      targetType: draft.targetType?.trim() || undefined,
      targetId: draft.targetId || undefined,
      limit: 50,
    })
  }

  function updateDraft<K extends keyof ReportAuditQuery>(key: K, value: ReportAuditQuery[K]) {
    setDraft((current) => ({ ...current, [key]: value }))
  }

  return (
    <Drawer open={open} title="报表审计记录" description="查看配置、运行、查询、导出、下载及结果清理的安全审计元数据。" size="wide" onClose={onClose}>
      <form className={styles.filters} onSubmit={applyFilters}>
        <label>动作<input value={draft.action ?? ''} maxLength={64} placeholder="例如 REPORT_RESULT_QUERY_SUCCESS" onChange={(event) => updateDraft('action', event.currentTarget.value)} /></label>
        <label>目标类型<select value={draft.targetType ?? ''} onChange={(event) => updateDraft('targetType', event.currentTarget.value)}><option value="">全部类型</option><option value="REPORT_DATASOURCE">Oracle 数据源</option><option value="REPORT_DEFINITION">报表配置</option><option value="REPORT_RUN">报表运行</option><option value="REPORT_EXPORT">报表导出</option></select></label>
        <label>目标 ID<input type="number" min="1" step="1" value={draft.targetId ?? ''} placeholder="可选，输入精确 ID" onChange={(event) => updateDraft('targetId', Number(event.currentTarget.value) || undefined)} /></label>
        {draft.targetType === 'REPORT_DEFINITION' && reports.length > 0 ? <label>当前报表快捷选择<select value={draft.targetId ?? ''} onChange={(event) => updateDraft('targetId', Number(event.currentTarget.value) || undefined)}><option value="">手动输入 / 全部</option>{reports.map((report) => <option key={report.id} value={report.id}>{report.name}</option>)}</select></label> : null}
        <button type="submit" disabled={state.loading}><Search aria-hidden="true" />查询</button>
        <button type="button" disabled={state.loading} onClick={() => void load(filters)}><RefreshCw aria-hidden="true" />刷新</button>
      </form>
      {state.error ? <FeedbackState kind="error" title="审计记录加载失败" description={state.error} action={<button type="button" onClick={() => void load(filters)}>重试</button>} /> : null}
      {state.loading && items.length === 0 ? <FeedbackState kind="loading" title="正在读取报表审计记录" /> : null}
      {!state.loading && !state.error && items.length === 0 ? <FeedbackState kind="empty" title="暂无匹配的审计记录" /> : null}
      {items.length > 0 ? <div aria-busy={state.loading}><AuditTable items={items} /></div> : null}
      {state.loading && items.length > 0 ? <p className={styles.loadingMore} role="status" aria-live="polite">正在加载更多审计记录</p> : null}
      {hasMore ? <div className={styles.more}><button type="button" disabled={state.loading} onClick={() => void load({ ...filters, afterId: nextAfterId }, true)}>{state.loading ? '加载中…' : '加载更多'}</button></div> : null}
    </Drawer>
  )
}

function AuditTable({ items }: { items: ReportAudit[] }) {
  return <DataTable containerClassName={styles.table} density="compact" minWidth={920} scrollLabel="报表审计记录"><thead><tr><th scope="col">时间</th><th scope="col">动作</th><th scope="col">目标</th><th scope="col">操作人</th><th scope="col">请求标识</th><th scope="col">详情</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td>{formatDate(item.createdAt)}</td><td><StatusTag tone={auditTone(item.action)}>{auditActionLabel(item.action)}</StatusTag></td><td><strong>{targetLabel(item.targetType)}</strong><small>#{item.targetId}</small></td><td>{item.actorType === 'SYSTEM' ? '系统任务' : `用户 #${item.actorUserId}`}</td><td><code>{item.requestId}</code></td><td><details className={styles.detail}><summary>{Object.keys(item.detail).length} 个字段</summary><pre>{JSON.stringify(item.detail, null, 2)}</pre></details></td></tr>)}</tbody></DataTable>
}

function auditTone(action: string) {
  if (action.includes('DENIED') || action.includes('REJECTED') || action.includes('FAILED')) return 'danger' as const
  if (action.includes('DOWNLOAD') || action.includes('READ')) return 'info' as const
  if (action.includes('PUBLISH') || action.includes('PURGE')) return 'success' as const
  return 'neutral' as const
}

function auditActionLabel(action: string) {
  return ({
    REPORT_DRAFT_CREATE: '创建草稿', REPORT_DRAFT_UPDATE: '更新草稿', REPORT_DRAFT_COLLECTIONS_UPDATE: '更新配置集合',
    REPORT_PUBLISH: '发布报表', REPORT_RUN_CREATE: '创建运行', REPORT_RUN_CANCEL_REQUEST: '取消运行',
    REPORT_RUN_SUCCEEDED: '运行完成', REPORT_RUN_FAILED: '运行失败', REPORT_RUN_CANCELLED: '运行已取消',
    REPORT_RUN_UNKNOWN: '运行结果待确认', REPORT_RUN_RECONCILED: '运行结果已确认', REPORT_RUN_RECONCILE_PENDING: '运行结果仍待确认',
    REPORT_RUN_STARTED: '运行已开始', REPORT_RUN_ORACLE_STARTED: 'Oracle 过程已开始', REPORT_RUN_RETRY_QUEUED: '运行已重新排队', REPORT_RUN_RECONCILIATION_STARTED: '运行对账已开始',
    REPORT_RESULT_QUERY_SUCCESS: '查询结果', REPORT_RESULT_QUERY_DENIED: '查询被拒绝', REPORT_EXPORT_CREATE: '创建导出',
    REPORT_EXPORT_READY: '导出完成', REPORT_EXPORT_FAILED: '导出失败', REPORT_EXPORT_CANCELLED: '导出已取消',
    REPORT_EXPORT_STARTED: '导出已开始', REPORT_EXPORT_RETRY_QUEUED: '导出已重新排队',
    REPORT_RESULT_PURGE_STARTED: '结果清理已开始', REPORT_RESULT_PURGE_RETRY: '结果清理等待重试', REPORT_RESULT_PURGED: '结果数据已清理', REPORT_RUN_SUPERSEDED: '结果已被新运行替代', REPORT_EXPORT_FILE_EXPIRED: '导出文件已过期',
    REPORT_EXPORT_DOWNLOAD_SIGN_SUCCESS: '获取下载地址', REPORT_EXPORT_DOWNLOAD_SIGN_DENIED: '下载被拒绝',
  } as Record<string, string>)[action] ?? action
}

function targetLabel(targetType: string) {
  return ({ REPORT_DATASOURCE: '数据源', REPORT_DEFINITION: '报表', REPORT_RUN: '运行', REPORT_EXPORT: '导出' } as Record<string, string>)[targetType] ?? targetType
}

function formatDate(value: string) { return formatShanghaiDateTime(value, '-') }
