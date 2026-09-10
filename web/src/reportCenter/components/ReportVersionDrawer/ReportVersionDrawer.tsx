import { useEffect, useRef, useState } from 'react'
import { ArrowRight, Eye, GitCompareArrows, Rocket } from 'lucide-react'
import { Button, DataTable, Dialog, Drawer, FeedbackState, StatusTag } from '../../../ui'
import { activateReportVersion, getReportVersion, getReportVersionDiff, getReportVersions, publishReportDraft, type ReportCenterClient } from '../../api'
import type { ReportPublication, ReportSummary, ReportVersionDetail, ReportVersionDiff, ReportVersionSummary } from '../../types'
import { createLatestRequestGuard } from './requestGuard'
import styles from './ReportVersionDrawer.module.css'

type PendingPublish = { kind: 'draft' } | { kind: 'version'; version: ReportVersionSummary }

export function ReportVersionDrawer({ client, report, onClose, onPublished, onChanged }: {
  client: ReportCenterClient
  report: ReportSummary | null
  onClose: () => void
  onPublished?: (publication: ReportPublication) => void
  onChanged?: () => void
}) {
  const [versions, setVersions] = useState<ReportVersionSummary[]>([])
  const [diff, setDiff] = useState<ReportVersionDiff | null>(null)
  const [detail, setDetail] = useState<ReportVersionDetail | null>(null)
  const [pending, setPending] = useState<PendingPublish | null>(null)
  const [selection, setSelection] = useState({ base: 0, target: 0 })
  const [page, setPage] = useState({ hasMore: false, nextAfterId: 0 })
  const [state, setState] = useState({ loading: false, error: '' })
  const [detailState, setDetailState] = useState({ loading: false, error: '' })
  const [publishState, setPublishState] = useState({ busy: false, error: '' })
  const [activeVersionId, setActiveVersionId] = useState(0)
  const listRequestGuard = useRef(createLatestRequestGuard())
  const contentRequestGuard = useRef(createLatestRequestGuard())
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const listGuard = listRequestGuard.current
    const contentGuard = contentRequestGuard.current
    if (!report) return () => { listGuard.cancel(); contentGuard.cancel() }
    const request = listGuard.begin()
    setState({ loading: true, error: '' })
    setVersions([])
    setDiff(null)
    setDetail(null)
    setPending(null)
    setPublishState({ busy: false, error: '' })
    setActiveVersionId(report.currentPublishedVersionId)
    void getReportVersions(client, report.id, 0, request.signal).then((response) => {
      if (!request.isCurrent()) return
      if (!response.ok) { setState({ loading: false, error: response.error }); return }
      const items = response.data.items
      setVersions(items)
      setPage({ hasMore: response.data.hasMore, nextAfterId: response.data.nextAfterId })
      setSelection({ base: items[1]?.id ?? 0, target: items[0]?.id ?? 0 })
      setState({ loading: false, error: '' })
    })
    return () => { listGuard.cancel(); contentGuard.cancel() }
  }, [client, report, reloadKey])

  async function loadMore() {
    if (!report || !page.hasMore || state.loading) return
    const request = listRequestGuard.current.begin()
    setState({ loading: true, error: '' })
    const response = await getReportVersions(client, report.id, page.nextAfterId, request.signal)
    if (!request.isCurrent()) return
    if (!response.ok) { setState({ loading: false, error: response.error }); return }
    setVersions((items) => [...items, ...response.data.items])
    setPage({ hasMore: response.data.hasMore, nextAfterId: response.data.nextAfterId })
    setState({ loading: false, error: '' })
  }

  async function compare() {
    if (!report || !selection.base || !selection.target || selection.base === selection.target) return
    const request = contentRequestGuard.current.begin()
    setState({ loading: true, error: '' })
    setDetailState({ loading: false, error: '' })
    const response = await getReportVersionDiff(client, report.id, selection.base, selection.target, request.signal)
    if (!request.isCurrent()) return
    if (!response.ok) { setState({ loading: false, error: response.error }); return }
    setDiff(response.data)
    setDetail(null)
    setState({ loading: false, error: '' })
  }

  async function viewVersion(version: ReportVersionSummary) {
    if (!report) return
    const request = contentRequestGuard.current.begin()
    setState((value) => ({ ...value, loading: false }))
    setDetailState({ loading: true, error: '' })
    setDiff(null)
    const response = await getReportVersion(client, report.id, version.id, request.signal)
    if (!request.isCurrent()) return
    if (!response.ok) { setDetailState({ loading: false, error: response.error }); return }
    setDetail(response.data)
    setDetailState({ loading: false, error: '' })
  }

  async function confirmPublish() {
    if (!report || !pending || publishState.busy) return
    setPublishState({ busy: true, error: '' })
    const response = pending.kind === 'draft'
      ? await publishReportDraft(client, report.id, report.lockVersion)
      : await activateReportVersion(client, report.id, pending.version.id, report.lockVersion)
    if (!response.ok) { setPublishState({ busy: false, error: response.error }); return }
    setActiveVersionId(response.data.versionId)
    setPending(null)
    setPublishState({ busy: false, error: '' })
    onChanged?.()
    onPublished?.(response.data)
    close()
  }

  function close() { listRequestGuard.current.cancel(); contentRequestGuard.current.cancel(); onClose() }
  const pendingLabel = pending?.kind === 'draft' ? '当前草稿' : pending ? `历史版本 v${pending.version.version}` : ''

  return <>
    <Drawer open={Boolean(report)} title="历史版本与上线" description={report ? `${report.name} · 查看不可变版本配置，并选择需要上线的版本。` : undefined} size="wide" onClose={close}>
      {report?.lockVersion ? <div className={styles.publishBar}><div><strong>当前草稿 v{report.lockVersion}</strong><span>上线前会重新核验 Oracle 过程、结果表和 Excel 契约。</span></div><Button variant="primary" type="button" disabled={publishState.busy} onClick={() => { setPublishState({ busy: false, error: '' }); setPending({ kind: 'draft' }) }}><Rocket aria-hidden="true" />上线当前草稿</Button></div> : null}
      {state.loading && !versions.length ? <FeedbackState kind="loading" title="正在读取版本历史" /> : null}
      {state.error ? <FeedbackState kind="error" title="版本数据不可用" description={state.error} action={!versions.length ? <button type="button" onClick={() => setReloadKey((value) => value + 1)}>重试</button> : undefined} /> : null}
      {!state.loading && !state.error && !versions.length ? <FeedbackState kind="empty" title="暂无已发布版本" description="可以先上线当前草稿形成第一个历史版本。" /> : null}
      {versions.length ? <>
        <div className={styles.compare}><VersionSelect label="基准版本" value={selection.base} versions={versions} disabled={state.loading} onChange={(base) => { setSelection((value) => ({ ...value, base })); setDiff(null) }} /><ArrowRight aria-hidden="true" /><VersionSelect label="目标版本" value={selection.target} versions={versions} disabled={state.loading} onChange={(target) => { setSelection((value) => ({ ...value, target })); setDiff(null) }} /><button type="button" disabled={state.loading || !selection.base || !selection.target || selection.base === selection.target} onClick={() => void compare()}><GitCompareArrows aria-hidden="true" />比较契约</button></div>
        <DataTable density="compact" minWidth={920} scrollLabel="报表版本历史"><thead><tr><th scope="col">版本</th><th scope="col">状态</th><th scope="col">发布时间</th><th scope="col">条件 / 字段 / 授权</th><th scope="col">契约指纹</th><th scope="col">操作</th></tr></thead><tbody>{versions.map((item) => { const active = item.id === activeVersionId; return <tr key={item.id}><td>v{item.version}</td><td><StatusTag tone={active ? 'success' : 'neutral'}>{active ? '当前上线' : '历史版本'}</StatusTag></td><td>{formatDate(item.publishedAt)}</td><td>{item.parameterCount} / {item.columnCount} / {item.grantCount}</td><td><code title={item.contractFingerprint}>{shortFingerprint(item.contractFingerprint)}</code></td><td><div className={styles.actions}><button type="button" disabled={detailState.loading} onClick={() => void viewVersion(item)}><Eye aria-hidden="true" />查看配置</button><Button variant="primary" type="button" disabled={active || publishState.busy} title={active ? '该版本已经在线上' : `上线 v${item.version}`} onClick={() => { setPublishState({ busy: false, error: '' }); setPending({ kind: 'version', version: item }) }}><Rocket aria-hidden="true" />{active ? '已上线' : '上线此版本'}</Button></div></td></tr> })}</tbody></DataTable>
        {page.hasMore ? <div className={styles.more}><button type="button" disabled={state.loading} onClick={() => void loadMore()}>{state.loading ? '加载中…' : '加载更多版本'}</button></div> : null}
        {detailState.loading ? <FeedbackState kind="loading" title="正在读取版本配置" /> : null}
        {detailState.error ? <FeedbackState kind="error" title="版本配置不可用" description={detailState.error} /> : null}
        {detail ? <VersionDetailView detail={detail} /> : diff ? <DiffView diff={diff} /> : <p className={styles.hint}>可查看任一历史版本的完整配置，或选择两个版本比较契约摘要。</p>}
      </> : null}
    </Drawer>
    <Dialog open={Boolean(pending)} role="alertdialog" title="确认上线报表版本" description={pendingLabel ? `即将上线：${pendingLabel}` : undefined} closeDisabled={publishState.busy} onClose={() => { setPending(null); setPublishState({ busy: false, error: '' }) }} footer={<><button type="button" disabled={publishState.busy} onClick={() => { setPending(null); setPublishState({ busy: false, error: '' }) }}>取消</button><Button variant="primary" disabled={publishState.busy} onClick={() => void confirmPublish()}>{publishState.busy ? '核验并上线中…' : '确认上线'}</Button></>}>
      <p className={styles.confirmNotice}>系统会先连接该版本绑定的数据源，重新核验 Oracle 过程、结果表和 Excel 契约；核验通过后才会切换线上版本，当前草稿不会被覆盖。</p>
      {publishState.error ? <p className={styles.publishError} role="alert">{publishState.error}</p> : null}
    </Dialog>
  </>
}

function VersionSelect({ label, value, versions, disabled, onChange }: { label: string; value: number; versions: ReportVersionSummary[]; disabled: boolean; onChange: (value: number) => void }) { return <label>{label}<select value={value || ''} disabled={disabled} onChange={(event) => onChange(Number(event.currentTarget.value))}><option value="">请选择</option>{versions.map((item) => <option key={item.id} value={item.id}>v{item.version}</option>)}</select></label> }

function VersionDetailView({ detail }: { detail: ReportVersionDetail }) {
  const config = detail.configuration
  const procedure = [config.procedure.owner, config.procedure.package, config.procedure.name].filter(Boolean).join('.')
  return <section className={styles.detail}><header><h3>v{detail.summary.version} 完整配置</h3><StatusTag tone="info">只读</StatusTag></header><dl className={styles.overview}><Item label="数据源" value={`#${config.datasourceId}`} /><Item label="执行模式" value={config.executionMode} /><Item label="存储过程" value={procedure} mono /><Item label="结果表" value={[config.result.tableOwner, config.result.tableName].filter(Boolean).join('.') || '-'} mono /></dl>{Object.keys(config.inputSchema).length ? <details open><summary>输入条件 Schema</summary><pre>{JSON.stringify(config.inputSchema, null, 2)}</pre></details> : null}{config.callTemplate ? <details><summary>调用模板</summary><pre>{config.callTemplate}</pre></details> : null}<h4>筛选参数（{config.parameters.length}）</h4>{config.parameters.length ? <DataTable density="compact" minWidth={700} scrollLabel="历史版本筛选参数"><thead><tr><th scope="col">顺序</th><th scope="col">编码</th><th scope="col">名称</th><th scope="col">Oracle 参数</th><th scope="col">类型</th></tr></thead><tbody>{config.parameters.map((item) => <tr key={item.code}><td>{item.displayOrder}</td><td><code>{item.code}</code></td><td>{item.label}</td><td><code>{item.procedureArgName}</code></td><td>{item.logicalType} / {item.oracleType}</td></tr>)}</tbody></DataTable> : <p>无筛选参数</p>}<h4>结果与 Excel 字段（{config.columns.length}）</h4><DataTable density="compact" minWidth={800} scrollLabel="历史版本结果字段"><thead><tr><th scope="col">导出顺序</th><th scope="col">Oracle 字段</th><th scope="col">Excel 表头</th><th scope="col">导出类型</th><th scope="col">导出</th></tr></thead><tbody>{config.columns.map((item) => <tr key={item.fieldId}><td>{item.exportOrder}</td><td><code>{item.databaseColumn}</code></td><td>{item.excelHeader}</td><td>{item.valueType}</td><td>{item.exportVisible && item.exportAllowed ? '是' : '否'}</td></tr>)}</tbody></DataTable><h4>授权（{config.grants.length}）</h4>{config.grants.length ? <ul className={styles.grants}>{config.grants.map((item) => <li key={`${item.subjectType}-${item.subjectId}`}><code>{item.subjectType} #{item.subjectId}</code><span>{item.actions.join(' / ')}</span></li>)}</ul> : <p>未配置报表级授权</p>}</section>
}

function Item({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) { return <div><dt>{label}</dt><dd className={mono ? styles.mono : undefined}>{value}</dd></div> }
function DiffView({ diff }: { diff: ReportVersionDiff }) { const total = diff.sections.reduce((count, section) => count + section.changes.length, 0); return <section className={styles.diff}><header><h3>v{diff.base.version} → v{diff.target.version}</h3><StatusTag tone={total ? 'warning' : 'success'}>{total ? `${total} 项变化` : '无差异'}</StatusTag></header>{diff.sections.map((section) => <section key={section.key}><h4>{section.label}</h4>{section.changes.length ? <ul>{section.changes.map((change) => <li key={change.key}><strong>{change.label}</strong><span><code>{displayDiffValue(change.before)}</code><ArrowRight aria-hidden="true" /><code>{displayDiffValue(change.after)}</code></span></li>)}</ul> : <p>无变化</p>}</section>)}</section> }
function formatDate(value: string | null) { if (!value) return '-'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN') }
function displayDiffValue(value: unknown) { if (value === null || value === undefined) return '-'; return typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value) }
function shortFingerprint(value: string) { return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value }
