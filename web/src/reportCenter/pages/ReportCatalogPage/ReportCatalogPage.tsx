import { useMemo, useRef, useState, type ReactNode } from 'react'
import { FileText, GitCompareArrows, Plus, RefreshCcw, Search, Trash2, X } from 'lucide-react'
import { Button, DataTable, Dialog, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, Section, StatusTag, type StatusTagTone } from '../../../ui'
import { deleteReportDraft, type ReportCenterClient } from '../../api'
import { ReportConfigDrawer } from '../../components/ReportConfigDrawer/ReportConfigDrawer'
import { ReportValidationResultDrawer } from '../../components/ReportValidationResultDrawer/ReportValidationResultDrawer'
import { ReportVersionDrawer } from '../../components/ReportVersionDrawer/ReportVersionDrawer'
import type { ReportPublication, ReportSummary } from '../../types'
import { useReportCatalog } from '../../useReportCatalog'
import { useReportDatasources } from '../../useReportDatasources'
import styles from './ReportCatalogPage.module.css'

export function ReportCatalogPage({ client, canManage, navigation }: { client: ReportCenterClient; canManage: boolean; navigation?: ReactNode }) {
  const [draftSearch, setDraftSearch] = useState('')
  const [search, setSearch] = useState('')
  const [drawerState, setDrawerState] = useState<{ open: boolean; report: ReportSummary | null }>({ open: false, report: null })
  const [publication, setPublication] = useState<ReportPublication | null>(null)
  const [versionReport, setVersionReport] = useState<ReportSummary | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ReportSummary | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const refreshButtonRef = useRef<HTMLButtonElement>(null)
  const query = useMemo(() => ({ search, limit: 50 }), [search])
  const { items, loading, loadingMore, error, hasMore, reload, loadMore } = useReportCatalog(client, query)
  const datasources = useReportDatasources(client, canManage)

  async function confirmDelete() {
    if (!pendingDelete || deleting) return
    setDeleting(true)
    setDeleteError('')
    const result = await deleteReportDraft(client, pendingDelete.id, pendingDelete.lockVersion)
    if (!result.ok) {
      setDeleteError(result.error)
      setDeleting(false)
      return
    }
    setPendingDelete(null)
    setDeleting(false)
    reload()
  }

  return (
    <PageCanvas density="compact">
      {navigation}
      <PageHeader eyebrow="REPORT CENTER" title="报表目录" description="从 MySQL 配置中心查看报表定义、发布状态和当前版本。" actions={<><button ref={refreshButtonRef} type="button" onClick={reload} disabled={loading}><RefreshCcw aria-hidden="true" />刷新</button><Button variant="primary" onClick={() => setDrawerState({ open: true, report: null })} disabled={!canManage}><Plus aria-hidden="true" />创建报表</Button></>} />
      <MetricStrip label="当前加载的报表概览" items={[
        { key: 'total', label: '已加载', value: items.length, detail: hasMore ? '仍有更多' : '当前全部' },
        { key: 'active', label: '已发布', value: items.filter((item) => item.status === 'ACTIVE').length, detail: '可执行' },
        { key: 'draft', label: '草稿', value: items.filter((item) => item.status === 'DRAFT').length, detail: '待发布' },
        { key: 'shared', label: '共享给我', value: items.filter((item) => !item.isOwner).length, detail: '只读访问' },
      ]} />
      <FilterToolbar summary={`当前加载 ${items.length} 个报表`}>
        <form className={styles.search} onSubmit={(event) => { event.preventDefault(); setSearch(draftSearch.trim()) }}>
          <label><span>搜索报表</span><span className={styles.input}><Search aria-hidden="true" /><input type="search" value={draftSearch} onChange={(event) => setDraftSearch(event.currentTarget.value)} placeholder="名称或编码" /></span></label>
          {draftSearch ? <button className={styles.clear} type="button" aria-label="清空报表搜索" onClick={() => { setDraftSearch(''); setSearch('') }}><X aria-hidden="true" /></button> : null}
          <button type="submit" disabled={loading}>查询</button>
        </form>
      </FilterToolbar>
      <Section title="已登记报表" description="目录只展示后端真实返回的数据，不创建演示记录。" flush>
        {loading && items.length === 0 ? <FeedbackState kind="loading" title="正在加载报表目录" description="正在请求 GET /v1/reports。" /> : null}
        {error ? <FeedbackState kind="error" title="报表目录加载失败" description={error} action={<button type="button" onClick={reload}>重试</button>} /> : null}
        {!loading && !error && items.length === 0 ? <FeedbackState kind="empty" title="暂无报表" description="后端尚未返回可查看的报表定义。" action={canManage ? <button type="button" onClick={() => setDrawerState({ open: true, report: null })}>创建第一份报表</button> : null} /> : null}
        {items.length > 0 ? <ReportTable reports={items} onVersions={canManage ? setVersionReport : undefined} onEdit={canManage ? (report) => setDrawerState({ open: true, report }) : undefined} onDelete={canManage ? (report) => { setDeleteError(''); setPendingDelete(report) } : undefined} /> : null}
        {items.length > 0 && hasMore ? <div className={styles.pagination}><button type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? '正在加载…' : '加载更多报表'}</button></div> : null}
      </Section>
      {drawerState.open ? <ReportConfigDrawer client={client} report={drawerState.report} datasources={datasources.items} datasourcesLoading={datasources.loading} datasourcesError={datasources.error} onPublished={setPublication} onSaved={reload} onClose={() => setDrawerState({ open: false, report: null })} /> : null}
      <ReportVersionDrawer client={client} report={versionReport} onPublished={setPublication} onChanged={reload} onClose={() => setVersionReport(null)} />
      <ReportValidationResultDrawer publication={publication} onClose={() => setPublication(null)} />
      <Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除报表模板" description={pendingDelete ? `确认删除“${pendingDelete.name}”？` : undefined} closeDisabled={deleting} returnFocus={refreshButtonRef.current} onClose={() => { setDeleteError(''); setPendingDelete(null) }} footer={<><button type="button" disabled={deleting} onClick={() => { setDeleteError(''); setPendingDelete(null) }}>取消</button><Button variant="danger" disabled={deleting} onClick={() => void confirmDelete()}>{deleting ? '删除中…' : '确认删除'}</Button></>}>
        <p className={styles.dangerNotice}>仅未发布且从未运行的草稿模板可以删除。删除后无法恢复，已发布报表及运行历史不会被删除。</p>
        {deleteError ? <p className={styles.deleteError} role="alert">{deleteError}</p> : null}
      </Dialog>
    </PageCanvas>
  )
}

function ReportTable({ reports, onVersions, onEdit, onDelete }: { reports: ReportSummary[]; onVersions?: (report: ReportSummary) => void; onEdit?: (report: ReportSummary) => void; onDelete?: (report: ReportSummary) => void }) {
  return <DataTable minWidth={1040} scrollLabel="报表目录列表"><thead><tr><th scope="col">报表</th><th scope="col">分类</th><th scope="col">数据源</th><th scope="col">版本</th><th scope="col">状态</th><th scope="col">更新时间</th><th scope="col">操作</th></tr></thead><tbody>{reports.map((report) => <tr key={report.id}><td><span className={styles.reportName}><FileText aria-hidden="true" /><span><strong>{report.name}</strong><code>{report.code}</code></span></span></td><td>{report.category || '未分类'}</td><td>{report.datasourceId ? <span className={styles.datasource}>Oracle <code>#{report.datasourceId}</code></span> : report.isOwner ? '-' : <StatusTag tone="info">共享</StatusTag>}</td><td>{report.currentPublishedVersionId ? `已发布 #${report.currentPublishedVersionId}` : report.lockVersion ? `草稿 v${report.lockVersion}` : report.currentDraftVersionId ? `草稿 #${report.currentDraftVersionId}` : '-'}</td><td><StatusTag tone={statusTone(report.status)}>{statusLabel(report.status)}</StatusTag></td><td>{formatDate(report.updatedAt)}</td><td><div className={styles.actions}>{report.isOwner && onVersions ? <Button variant="primary" type="button" onClick={() => onVersions(report)}><GitCompareArrows aria-hidden="true" />{report.currentPublishedVersionId ? '上线 / 版本' : '上线'}</Button> : null}{report.isOwner ? <button type="button" onClick={() => onEdit?.(report)} disabled={!onEdit}>编辑配置</button> : <span className={styles.readOnly}>只读</span>}{report.isOwner && onDelete ? <Button variant="danger" title={report.status === 'DRAFT' ? '删除报表模板' : '已发布报表不能删除'} type="button" onClick={() => onDelete(report)} disabled={report.status !== 'DRAFT'}><Trash2 aria-hidden="true" />删除模板</Button> : null}</div></td></tr>)}</tbody></DataTable>
}

function statusTone(status: ReportSummary['status']): StatusTagTone { return status === 'ACTIVE' ? 'success' : status === 'DISABLED' ? 'danger' : 'warning' }
function statusLabel(status: ReportSummary['status']) { return status === 'ACTIVE' ? '已发布' : status === 'DISABLED' ? '已停用' : '草稿' }
function formatDate(value: string | null) { if (!value) return '-'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN') }
