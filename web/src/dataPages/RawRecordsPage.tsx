import { formatShanghaiDateTime } from '../dateTime.js'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import type { ClientResponse } from '../api/client'
import { buildRawRecordsRequest, buildWarehouseRawRecordsQuery, parseRawRecordsPage, type RawRecordOrigin, type RawRecordsPage as RecordsPage } from '../rawRecords'
import { parseSourceFetchSummary } from '../sourceOperations'
import { DataTable, Dialog, Drawer, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, PaginationControls, Section, StatusTag } from '../ui'
import { backendDateTime, formatUnixTime, parseRetransformResult, parseWarehouseRawRecord, rawDataOrigin, rawStatusTone, redactedRawData, warehouseStatusLabel, type RawData, type WarehouseRawRecord } from './rawPageSupport'
import styles from './RawRecordsPage.module.css'

type RawClientOptions = { method?: 'GET' | 'POST'; body?: unknown; signal?: AbortSignal; retry?: boolean; showResult?: boolean; silentLoading?: boolean }
type RawClient = (path: string, options?: RawClientOptions) => Promise<ClientResponse>

export function RawRecordsPage({ title, origin, client, onFetchSource }: { title: string; origin: RawRecordOrigin; client: RawClient; onFetchSource: (sourceID: number) => Promise<ClientResponse> }) {
  const [source, setSource] = useState('')
  const [dataType, setDataType] = useState('')
  const [status, setStatus] = useState('')
  const [businessKey, setBusinessKey] = useState('')
  const [startTime, setStartTime] = useState('')
  const [endTime, setEndTime] = useState('')
  const [appliedQuery, setAppliedQuery] = useState({ source: '', dataType: '', status: '', businessKey: '', startTime: '', endTime: '' })
  const [page, setPage] = useState(1)
  const [recordsPage, setRecordsPage] = useState<RecordsPage<RawData> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<RawData | null>(null)
  const [pendingSourceFetchID, setPendingSourceFetchID] = useState<number | null>(null)
  const [fetchingSourceID, setFetchingSourceID] = useState<number | null>(null)
  const [sourceFetchMessage, setSourceFetchMessage] = useState('')
  const requestRef = useRef<AbortController | null>(null)

  useEffect(() => {
    requestRef.current?.abort()
    const controller = new AbortController()
    requestRef.current = controller
    setLoading(true)
    setError('')
    const body = buildRawRecordsRequest({ page, pageSize: 20, origin, ...appliedQuery })
    void client('/v1/data/raw/list', { method: 'POST', body, signal: controller.signal, showResult: false, silentLoading: true }).then((response) => {
      if (controller.signal.aborted) return
      const nextPage = response.ok ? parseRawRecordsPage<RawData>(response.data) : null
      if (nextPage) setRecordsPage(nextPage)
      else setError(response.error?.message || '记录查询暂时不可用，请稍后重试。')
    }).catch(() => {
      if (!controller.signal.aborted) setError('记录查询暂时不可用，请稍后重试。')
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [appliedQuery, client, origin, page])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPage(1)
    setAppliedQuery({ source, dataType, status, businessKey, startTime: backendDateTime(startTime), endTime: backendDateTime(endTime) })
  }

  function resetQuery() {
    setSource(''); setDataType(''); setStatus(''); setBusinessKey(''); setStartTime(''); setEndTime(''); setPage(1)
    setAppliedQuery({ source: '', dataType: '', status: '', businessKey: '', startTime: '', endTime: '' })
  }

  async function fetchSource() {
    const sourceID = pendingSourceFetchID
    if (!sourceID || fetchingSourceID !== null) return
    setFetchingSourceID(sourceID)
    setSourceFetchMessage('')
    try {
      const response = await onFetchSource(sourceID)
      const summary = response.ok ? parseSourceFetchSummary(response.data) : null
      setSourceFetchMessage(summary ? `数据源 #${sourceID} 拉取完成：成功 ${summary.successCount}/${summary.totalCount}，失败 ${summary.failedCount}。` : response.error?.message || '数据源拉取未完成，请稍后重试。')
      setPendingSourceFetchID(null)
    } catch {
      setSourceFetchMessage('数据源拉取未完成，请稍后重试。')
    } finally {
      setFetchingSourceID(null)
    }
  }

  const records = recordsPage?.list ?? []
  const totalPages = recordsPage?.totalPages ?? 0
  return <PageCanvas>
    <PageHeader eyebrow="DATA INGESTION" title={title} description="按来源、状态、业务键和时间范围执行服务端分页查询；原始内容与元数据仅以脱敏形式展示。" />
    <FilterToolbar summary={loading && !recordsPage ? '正在加载…' : `查询命中 ${recordsPage?.total ?? 0} 条`}>
      <form className={styles.filters} onSubmit={submit} aria-label={`${title}筛选`}>
        <TextField label="ID / 外部编号 / 内容" name="raw_business_key" value={businessKey} onChange={setBusinessKey} />
        <label>状态<select value={status || 'all'} onChange={(event) => setStatus(event.currentTarget.value === 'all' ? '' : event.currentTarget.value)}><option value="all">全部</option><option value="pending">待处理</option><option value="processing">处理中</option><option value="processed">已处理</option><option value="error">异常</option></select></label>
        <TextField label="来源" name="raw_source" value={source} onChange={setSource} />
        {origin === 'pull' ? <><TextField label="开始时间" name="raw_start_time" type="datetime-local" value={startTime} onChange={setStartTime} /><TextField label="结束时间" name="raw_end_time" type="datetime-local" value={endTime} onChange={setEndTime} /></> : null}
        <details className={styles.advancedFilters}><summary>更多筛选</summary><div><TextField label="数据类型" name="raw_data_type" value={dataType} onChange={setDataType} />{origin === 'receive' ? <><TextField label="开始时间" name="raw_start_time" type="datetime-local" value={startTime} onChange={setStartTime} /><TextField label="结束时间" name="raw_end_time" type="datetime-local" value={endTime} onChange={setEndTime} /></> : null}</div></details>
        <div className={styles.filterActions}><button type="button" onClick={resetQuery} disabled={loading}>重置筛选</button><button className={styles.primary} type="submit" disabled={loading}>{loading ? '查询中…' : '查询'}</button></div>
      </form>
    </FilterToolbar>
    <MetricStrip label={`${title}摘要`} items={[{ key: 'total', label: '当前结果', value: recordsPage?.total ?? 0 }, { key: 'page-size', label: '当前页', value: records.length }, { key: 'pages', label: '总页数', value: Math.max(totalPages, 1) }]} />
    <p className={styles.contractNote}>来源、类型、状态、外部业务键与时间范围均由服务端分页筛选；业务键对应原始记录的外部 ID。</p>
    {sourceFetchMessage ? <p className={styles.notice} role="status" aria-live="polite">{sourceFetchMessage}</p> : null}
    {error ? <FeedbackState kind="error" title="原始记录查询失败" description={`${error} 已保留最近一次成功数据。`} /> : null}
    <Section title={`${title}（含脱敏内容）`} description={loading && !recordsPage ? '正在加载…' : `共 ${recordsPage?.total ?? 0} 条`}>
      {loading && !recordsPage ? <FeedbackState kind="loading" title="正在加载原始记录" /> : records.length === 0 ? <FeedbackState kind="empty" title="暂无原始数据" /> : <RawDataList origin={origin} records={records} onSelect={setSelected} onRequestSourceFetch={setPendingSourceFetchID} />}
      <PaginationControls page={recordsPage?.page ?? page} totalPages={totalPages} loading={loading} onPrevious={() => setPage((current) => Math.max(1, current - 1))} onNext={() => setPage((current) => current + 1)} />
    </Section>
    <WarehouseRawRecordsPanel client={client} origin={origin} />
    <RawDataDrawer origin={origin} record={selected} onClose={() => setSelected(null)} />
    <Dialog open={pendingSourceFetchID !== null} title="确认拉取数据源" closeDisabled={fetchingSourceID !== null} onClose={() => { if (fetchingSourceID === null) setPendingSourceFetchID(null) }} footer={<><button type="button" disabled={fetchingSourceID !== null} onClick={() => setPendingSourceFetchID(null)}>取消</button><button className={styles.primary} type="button" disabled={fetchingSourceID !== null} onClick={() => void fetchSource()}>{fetchingSourceID === pendingSourceFetchID ? '拉取中…' : '确认拉取'}</button></>}><p>确认立即拉取数据源 #{pendingSourceFetchID}？该操作会向已配置的来源发起真实请求。</p></Dialog>
  </PageCanvas>
}

function WarehouseRawRecordsPanel({ client, origin }: { client: RawClient; origin: RawRecordOrigin }) {
  const [source, setSource] = useState(''); const [status, setStatus] = useState(''); const [traceID, setTraceID] = useState(''); const [startTime, setStartTime] = useState(''); const [endTime, setEndTime] = useState('')
  const [appliedQuery, setAppliedQuery] = useState({ source: '', status: '', traceID: '', startTime: '', endTime: '' })
  const [page, setPage] = useState(1); const [recordsPage, setRecordsPage] = useState<RecordsPage<WarehouseRawRecord> | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [message, setMessage] = useState('')
  const [pendingRetransform, setPendingRetransform] = useState<WarehouseRawRecord | null>(null); const [retransformingID, setRetransformingID] = useState<number | null>(null); const [reloadVersion, setReloadVersion] = useState(0)
  const requestRef = useRef<AbortController | null>(null)

  useEffect(() => {
    requestRef.current?.abort(); const controller = new AbortController(); requestRef.current = controller; setLoading(true); setError('')
    const query = buildWarehouseRawRecordsQuery({ page, pageSize: 20, origin, ...appliedQuery })
    void client(`/v1/raw-records?${query}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true }).then((response) => {
      if (controller.signal.aborted) return
      const parsed = response.ok ? parseRawRecordsPage<unknown>(response.data) : null
      const records = parsed?.list.map(parseWarehouseRawRecord) ?? []
      if (parsed && records.every((record): record is WarehouseRawRecord => record !== null)) setRecordsPage({ ...parsed, list: records })
      else setError(response.error?.message || '可重新处理记录查询暂时不可用，请稍后重试。')
    }).catch(() => { if (!controller.signal.aborted) setError('可重新处理记录查询暂时不可用，请稍后重试。') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [appliedQuery, client, origin, page, reloadVersion])

  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setPage(1); setAppliedQuery({ source, status, traceID, startTime: backendDateTime(startTime), endTime: backendDateTime(endTime) }) }
  async function retransform() {
    const record = pendingRetransform
    if (!record || retransformingID !== null) return
    setRetransformingID(record.id); setError('')
    try {
      const response = await client(`/v1/raw-records/${record.id}/retransform`, { method: 'POST', retry: false, showResult: false, silentLoading: true })
      if (!response.ok) { setError(response.error?.message || '重新处理未完成，请稍后重试。'); return }
      const result = parseRetransformResult(response.data)
      if (!result) { setError('重新处理已提交，但未收到可验证的结果摘要。'); return }
      setPendingRetransform(null); setMessage(`重新处理完成：追踪 ${result.traceID || '-'}，清洗记录 #${result.cleanRecordID}。`); setReloadVersion((version) => version + 1)
    } catch { setError('重新处理未完成，请稍后重试。') } finally { setRetransformingID(null) }
  }
  const records = recordsPage?.list ?? []
  return <Section title={`可重新处理${origin === 'pull' ? '拉取' : '接收'}记录（仅元数据）`} description="历史列表仍只读；只有此列表中的记录 ID 可安全重新处理。" flush>
    <FilterToolbar summary={loading && !recordsPage ? '正在加载…' : `共 ${recordsPage?.total ?? 0} 条`}><form className={styles.filters} onSubmit={submit}><TextField label="来源" name="warehouse_raw_source" value={source} onChange={setSource} /><label>状态<select value={status || 'all'} onChange={(event) => setStatus(event.currentTarget.value === 'all' ? '' : event.currentTarget.value)}><option value="all">全部</option><option value="received">已接收</option><option value="queued">排队中</option><option value="cleaning">处理中</option><option value="cleaned">已清洗</option><option value="failed">失败</option></select></label><TextField label="追踪 ID" name="warehouse_raw_trace_id" value={traceID} onChange={setTraceID} /><TextField label="开始时间" name="warehouse_raw_start_time" type="datetime-local" value={startTime} onChange={setStartTime} /><TextField label="结束时间" name="warehouse_raw_end_time" type="datetime-local" value={endTime} onChange={setEndTime} /><button className={styles.primary} type="submit" disabled={loading || retransformingID !== null}>{loading ? '查询中…' : '查询'}</button></form></FilterToolbar>
    {message ? <p className={styles.notice} role="status" aria-live="polite">{message}</p> : null}
    {error ? <FeedbackState kind="error" title="可重新处理记录查询失败" description={`${error} 已保留最近一次成功数据。`} /> : null}
    {loading && !recordsPage ? <FeedbackState kind="loading" title="正在加载可重新处理记录" /> : records.length === 0 ? <FeedbackState kind="empty" title="暂无可重新处理的原始记录" /> : <DataTable minWidth={820} scrollLabel="可重新处理原始记录列表"><thead><tr><th scope="col">记录 ID</th><th scope="col">来源</th><th scope="col">追踪 ID</th><th scope="col">接收时间</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>{records.map((record) => <tr key={record.id}><td>#{record.id}</td><td><span className={styles.identity}><strong>{record.sourceCode || '未命名来源'}</strong><small>来源 #{record.sourceID || '-'}</small></span></td><td>{record.traceID || '-'}</td><td>{formatShanghaiDateTime(record.receivedAt, formatUnixTime(record.createdAt))}</td><td><StatusTag tone={rawStatusTone(record.status)}>{warehouseStatusLabel(record.status)}</StatusTag></td><td><button type="button" disabled={retransformingID !== null || record.status === 'cleaning'} onClick={() => setPendingRetransform(record)}>{retransformingID === record.id ? '处理中…' : '重新处理'}</button></td></tr>)}</tbody></DataTable>}
    <PaginationControls page={recordsPage?.page ?? page} totalPages={recordsPage?.totalPages ?? 0} loading={loading || retransformingID !== null} onPrevious={() => setPage((current) => Math.max(1, current - 1))} onNext={() => setPage((current) => current + 1)} />
    <Dialog open={Boolean(pendingRetransform)} title="确认重新处理原始记录" closeDisabled={retransformingID !== null} onClose={() => { if (retransformingID === null) setPendingRetransform(null) }} footer={<><button type="button" disabled={retransformingID !== null} onClick={() => setPendingRetransform(null)}>取消</button><button className={styles.primary} type="button" disabled={retransformingID !== null} onClick={() => void retransform()}>{retransformingID === pendingRetransform?.id ? '处理中…' : '确认重新处理'}</button></>}><p>确认重新处理仓库原始记录 #{pendingRetransform?.id}？系统会创建新的清洗记录；原始内容不会在管理端展示。</p></Dialog>
  </Section>
}

function RawDataList({ origin, records, onSelect, onRequestSourceFetch }: { origin: RawRecordOrigin; records: RawData[]; onSelect: (record: RawData) => void; onRequestSourceFetch: (sourceID: number) => void }) {
  return <DataTable minWidth={880} scrollLabel="原始记录列表"><thead><tr><th scope="col">ID / 外部编号</th><th scope="col">数据类型</th><th scope="col">来源</th><th scope="col">{origin === 'pull' ? '拉取时间' : '接收时间'}</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>{records.map((record) => <tr key={record.id}><td><span className={styles.identity}><strong>#{record.id}</strong><small>{record.external_id || '-'}</small></span></td><td>{record.data_type || 'raw'}</td><td>{record.source || `#${record.data_source_id || '-'}`}</td><td>{formatUnixTime(record.created_at)}</td><td><StatusTag tone={rawStatusTone(record.status)}>{record.status || '未知'}</StatusTag></td><td><div className={styles.actions}><button type="button" onClick={() => onSelect(record)}>查看详情</button>{origin === 'pull' && record.data_source_id > 0 ? <button type="button" onClick={() => onRequestSourceFetch(record.data_source_id)}>重新拉取</button> : null}</div></td></tr>)}</tbody></DataTable>
}

function RawDataDrawer({ origin, record, onClose }: { origin: RawRecordOrigin; record: RawData | null; onClose: () => void }) {
  return <Drawer open={Boolean(record)} title={record ? `${origin === 'pull' ? '拉取详情' : '原始记录'} #${record.id}` : '原始记录'} description="原始内容和元数据已经过敏感信息过滤。" size="wide" onClose={onClose}>{record ? <><dl className={styles.detailList}><div><dt>外部编号</dt><dd>{record.external_id || '-'}</dd></div><div><dt>来源</dt><dd>{record.source || `数据源 #${record.data_source_id || '-'}`}</dd></div><div><dt>接入方式</dt><dd>{rawDataOrigin(record)}</dd></div><div><dt>记录时间</dt><dd>{formatUnixTime(record.created_at)}</dd></div><div><dt>数据类型</dt><dd>{record.data_type || '-'}</dd></div><div><dt>状态</dt><dd><StatusTag tone={rawStatusTone(record.status)}>{record.status || '未知'}</StatusTag></dd></div></dl><h3 className={styles.detailHeading}>脱敏原始内容与元数据</h3><pre className={styles.jsonPreview} aria-label="只读脱敏原始内容">{JSON.stringify(redactedRawData(record), null, 2)}</pre></> : null}</Drawer>
}

function TextField({ label, name, value, onChange, type = 'text' }: { label: string; name: string; value: string; onChange: (value: string) => void; type?: string }) {
  return <label>{label}<input name={name} type={type} value={value} onChange={(event) => onChange(event.currentTarget.value)} /></label>
}
