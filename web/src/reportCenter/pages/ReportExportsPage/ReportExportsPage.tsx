import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Download, RefreshCw } from 'lucide-react'
import { DataTable, FeedbackState, FilterToolbar, PageCanvas, PageHeader, Section, StatusTag, type StatusTagTone } from '../../../ui'
import { getReportExportDownload, getReportExports, type ReportCenterClient } from '../../api'
import type { ReportExport, ReportExportStatus } from '../../types'
import design from '../../reportDesign.module.css'
import styles from './ReportExportsPage.module.css'

export function ReportExportsPage({ client, navigation }: { client: ReportCenterClient; navigation?: ReactNode }) {
	const [status, setStatus] = useState('')
	const [items, setItems] = useState<ReportExport[]>([])
	const [nextAfterId, setNextAfterId] = useState(0)
	const [hasMore, setHasMore] = useState(false)
	const [state, setState] = useState({ loading: true, error: '' })
	const requestSequence = useRef(0)

	const load = useCallback(async (afterId = 0, append = false, signal?: AbortSignal) => {
		const requestID = ++requestSequence.current
		setState({ loading: true, error: '' })
		if (!append) {
			setItems([])
			setHasMore(false)
			setNextAfterId(0)
		}
		const response = await getReportExports(client, { afterId: afterId || undefined, limit: 50, status: status || undefined }, signal)
		if (signal?.aborted || requestID !== requestSequence.current) return
		if (!response.ok) { setState({ loading: false, error: response.error }); return }
		setItems((current) => append ? [...current, ...response.data.items] : response.data.items)
		setHasMore(response.data.hasMore); setNextAfterId(response.data.nextAfterId); setState({ loading: false, error: '' })
	}, [client, status])

	useEffect(() => {
		const controller = new AbortController()
		void load(0, false, controller.signal)
		return () => controller.abort()
	}, [load])

	async function download(item: ReportExport) {
		const response = await getReportExportDownload(client, item.id)
		if (!response.ok) { setState({ loading: false, error: response.error }); return }
		window.location.assign(response.data.url)
	}

	return <PageCanvas density="compact" className={`${design.surface} ${design.page}`}>{navigation}<PageHeader eyebrow="EXPORT ARCHIVE" title="下载记录" description="已生成的报表文件集中保存在这里，随时查看进度与有效期。" actions={<button type="button" onClick={() => void load()} disabled={state.loading}><RefreshCw aria-hidden="true" />刷新</button>} /><FilterToolbar summary={<StatusTag tone="neutral">{items.length} 个任务</StatusTag>}><div className={styles.filters}><label>任务状态<select name="reportExportStatus" value={status} onChange={(event) => setStatus(event.currentTarget.value)}><option value="">全部状态</option>{['PENDING','RUNNING','READY','FAILED','CANCELLED','EXPIRED'].map((value) => <option key={value} value={value}>{exportLabel(value as ReportExportStatus)}</option>)}</select></label></div></FilterToolbar><Section title="导出任务" description="文件就绪后可重复下载；请在有效期内保存到本地。" flush>
		{state.error ? <FeedbackState kind="error" title="导出任务加载失败" description={state.error} action={<button type="button" onClick={() => void load()}>重试</button>} /> : null}
		{state.loading && items.length === 0 ? <FeedbackState kind="loading" title="正在加载导出任务" /> : null}
		{!state.loading && !state.error && items.length === 0 ? <FeedbackState kind="empty" title="暂无导出任务" description="在报表查询中生成正式 Excel 后，任务会出现在这里。" /> : null}
		{items.length ? <DataTable containerClassName={styles.table} scrollLabel="报表导出任务"><thead><tr><th scope="col">报表 / 任务</th><th scope="col">状态</th><th scope="col">进度</th><th scope="col">文件</th><th scope="col">结果留存</th><th scope="col">过期时间</th><th scope="col">操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong>{item.reportName || `报表运行 #${item.runId}`}</strong><small>导出 #{item.id} · 运行 #{item.runId}</small></td><td><StatusTag tone={exportTone(item)}>{exportLabel(item.status)}</StatusTag>{item.errorMessage ? <small>{item.errorMessage}</small> : null}</td><td>{item.status === 'READY' ? `${item.exportedRows.toLocaleString('zh-CN')} 行` : `${item.processedRows.toLocaleString('zh-CN')} 行`}{item.currentSheet ? <small>{item.currentSheet}</small> : null}</td><td>{item.fileSizeBytes ? formatBytes(item.fileSizeBytes) : '-'}{item.sheetCount ? <small>{item.sheetCount} 个工作表</small> : null}</td><td>{item.purgedAt ? '已清理' : item.purgeStartedAt ? '清理中' : item.status === 'READY' ? '等待清理' : '-'}{item.purgedRows ? <small>{item.purgedRows.toLocaleString('zh-CN')} 行</small> : null}</td><td>{formatDate(item.expiresAt)}</td><td><button type="button" disabled={!item.canDownload} onClick={() => void download(item)}><Download aria-hidden="true" />下载</button></td></tr>)}</tbody></DataTable> : null}
		{hasMore ? <div className={styles.more}><button type="button" disabled={state.loading} onClick={() => void load(nextAfterId, true)}>加载更多</button></div> : null}
	</Section></PageCanvas>
}

function exportTone(item: ReportExport): StatusTagTone { return item.status === 'READY' ? 'success' : item.status === 'FAILED' || item.status === 'CANCELLED' || item.status === 'EXPIRED' ? 'danger' : 'running' }
function exportLabel(status: ReportExportStatus) { return ({ PENDING: '等待导出', RUNNING: '生成中', READY: '文件就绪', FAILED: '导出失败', CANCELLED: '已取消', EXPIRED: '已过期' })[status] }
function formatDate(value: string | null) { if (!value) return '-'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN') }
function formatBytes(value: number) { if (value < 1024) return `${value} B`; if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`; return `${(value / 1024 / 1024).toFixed(1)} MB` }
