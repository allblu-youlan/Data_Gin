import { useMemo, useRef, useState, type ReactNode } from 'react'
import { Plus, RefreshCcw } from 'lucide-react'
import { Button, Dialog, PageCanvas, PageHeader } from '../../../ui'
import { deleteReportDraft, type ReportCenterClient } from '../../api'
import { ReportConfigDrawer } from '../../components/ReportConfigDrawer/ReportConfigDrawer'
import { ReportValidationResultDrawer } from '../../components/ReportValidationResultDrawer/ReportValidationResultDrawer'
import { ReportVersionDrawer } from '../../components/ReportVersionDrawer/ReportVersionDrawer'
import type { ReportPublication, ReportSummary } from '../../types'
import { useReportCatalog } from '../../useReportCatalog'
import { useReportDatasources } from '../../useReportDatasources'
import { ReportLibrary } from '../../components/ReportLibrary/ReportLibrary'
import design from '../../reportDesign.module.css'
import styles from './ReportCatalogPage.module.css'

export function ReportCatalogPage({ client, canManage, navigation }: { client: ReportCenterClient; canManage: boolean; navigation?: ReactNode }) {
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
    <PageCanvas density="compact" className={`${design.surface} ${design.page}`}>
      {navigation}
      <PageHeader eyebrow="REPORT LIBRARY" title="报表目录" description="统一查看业务报表，管理发布版本与共享访问。" actions={<><button ref={refreshButtonRef} type="button" onClick={reload} disabled={loading}><RefreshCcw aria-hidden="true" />刷新</button><Button variant="primary" onClick={() => setDrawerState({ open: true, report: null })} disabled={!canManage}><Plus aria-hidden="true" />创建报表</Button></>} context={<div className={design.caption}><strong>{items.length} 份报表{hasMore ? '已加载' : ''}</strong><span>·</span><span>{items.filter((item) => item.status === 'ACTIVE').length} 份已发布</span><span>·</span><span>{items.filter((item) => !item.isOwner).length} 份共享给我</span></div>} />
      <ReportLibrary reports={items} loading={loading} loadingMore={loadingMore} hasMore={hasMore} error={error} search={search} onSearch={setSearch} onReload={reload} onLoadMore={() => void loadMore()} onOpen={canManage ? (report) => setDrawerState({ open: true, report }) : undefined} onVersions={canManage ? setVersionReport : undefined} onDelete={canManage ? (report) => { setDeleteError(''); setPendingDelete(report) } : undefined} emptyAction={canManage ? <Button onClick={() => setDrawerState({ open: true, report: null })}>创建第一份报表</Button> : undefined} />
      {drawerState.open ? <ReportConfigDrawer client={client} report={drawerState.report} datasources={datasources.items} datasourcesLoading={datasources.loading} datasourcesError={datasources.error} onSaved={reload} onClose={() => setDrawerState({ open: false, report: null })} /> : null}
      <ReportVersionDrawer client={client} report={versionReport} onPublished={setPublication} onChanged={reload} onClose={() => setVersionReport(null)} />
      <ReportValidationResultDrawer publication={publication} onClose={() => setPublication(null)} />
      <Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除报表模板" description={pendingDelete ? `确认删除“${pendingDelete.name}”？` : undefined} closeDisabled={deleting} returnFocus={refreshButtonRef.current} onClose={() => { setDeleteError(''); setPendingDelete(null) }} footer={<><button type="button" disabled={deleting} onClick={() => { setDeleteError(''); setPendingDelete(null) }}>取消</button><Button variant="danger" disabled={deleting} onClick={() => void confirmDelete()}>{deleting ? '删除中…' : '确认删除'}</Button></>}>
        <p className={styles.dangerNotice}>仅未发布且从未运行的草稿模板可以删除。删除后无法恢复，已发布报表及运行历史不会被删除。</p>
        {deleteError ? <p className={styles.deleteError} role="alert">{deleteError}</p> : null}
      </Dialog>
    </PageCanvas>
  )
}
