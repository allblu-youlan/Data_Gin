import { type FormEvent, useMemo, useRef, useState } from 'react'
import { browserSessionStorage } from './browserStorage'
import { MallDetailsFields } from './MallWeatherMallEditor'
import { mallImportRequestWithinLimit, parseMallImportCSV, parseMallImportResult, type MallImportResult, type MallImportRow } from './mallImport'
import {
  clearMallWeatherPendingCreate,
  loadMallWeatherPendingCreate,
  mallWeatherCreateKey,
  mallWeatherCreateRequest,
  parseMallWeatherCreateResult,
  saveMallWeatherPendingCreate,
  type MallWeatherCreateInput,
  type MallWeatherMall,
  type MallWeatherPendingCreate,
} from './mallWeather'
import styles from './MallStoreOperations.module.css'

type ApiResult = { ok: boolean; status: number; data: unknown }
type ApiClient = (path: string, options?: {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
  headers?: Record<string, string>
  showResult?: boolean
  silentLoading?: boolean
  signal?: AbortSignal
}) => Promise<ApiResult>

const emptyMallCreateInput: MallWeatherCreateInput = {
  mallCode: '', nameCn: '', province: '', city: '', district: '', address: '',
}

export function MallImportPanel({ client, onImported }: { client: ApiClient; onImported: () => void }) {
  const [rows, setRows] = useState<MallImportRow[]>([])
  const [result, setResult] = useState<MallImportResult | null>(null)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [canRetryOriginal, setCanRetryOriginal] = useState(false)
  const requestKeyRef = useRef(mallWeatherCreateKey())
  const fileInputRef = useRef<HTMLInputElement>(null)
  const fileSequenceRef = useRef(0)
  const items = rows.flatMap((row) => row.item ? [row.item] : [])
  const invalidRows = rows.filter((row) => row.error)
  const validForSubmit = items.length > 0 && invalidRows.length === 0

  async function submit() {
    if (!validForSubmit) {
      setError('请先修正 CSV 中的错误行。')
      return
    }
    if (!mallImportRequestWithinLimit(items)) {
      setError('转换后的导入请求超过 1 MiB 服务端限制，请减少导入行数或缩短字段内容。')
      return
    }
    setSubmitting(true)
    setError('')
    setCanRetryOriginal(false)
    const response = await client('/v1/malls/import', {
      method: 'POST', body: { items }, headers: { 'Idempotency-Key': requestKeyRef.current }, showResult: false, silentLoading: true,
    })
    setSubmitting(false)
    if (!response.ok) {
      setConfirming(false)
      setCanRetryOriginal(response.status === 0 || response.status === 409 || response.status >= 500)
      setError(importSubmissionError(response.status))
      return
    }
    const parsed = parseMallImportResult(response.data, items.length)
    if (!parsed) {
      setConfirming(false)
      setCanRetryOriginal(true)
      setError('导入响应格式不正确；已保留原请求，请使用“重试原请求”确认结果。')
      return
    }
    setResult(parsed)
    setConfirming(false)
    onImported()
  }

  function chooseFile(file: File) {
    const sequence = ++fileSequenceRef.current
    void file.text().then((text) => {
      if (sequence !== fileSequenceRef.current) return
      try {
        const parsed = parseMallImportCSV(text)
        requestKeyRef.current = mallWeatherCreateKey()
        setRows(parsed)
        setResult(null)
        setError('')
        setConfirming(false)
        setCanRetryOriginal(false)
      } catch (cause) {
        setError(`${cause instanceof Error ? cause.message : 'CSV 解析失败'}；当前已解析内容和重试键未改变。`)
      }
    }).catch(() => {
      if (sequence === fileSequenceRef.current) setError('读取 CSV 文件失败；当前已解析内容和重试键未改变。')
    })
  }

  function abandon() {
    fileSequenceRef.current++
    requestKeyRef.current = mallWeatherCreateKey()
    if (fileInputRef.current) fileInputRef.current.value = ''
    setRows([])
    setResult(null)
    setError('')
    setConfirming(false)
    setCanRetryOriginal(false)
  }

  return <section className={`${styles.panel} ${styles.importPanel}`} aria-busy={submitting}>
    <div className={styles.sectionTitle}><div><strong>批量导入店铺</strong><span>仅支持 UTF-8 CSV，表头固定为 mallCode,nameCn,province,city,district,address；每次 1–200 行。</span></div></div>
    <label>CSV 文件<input ref={fileInputRef} type="file" accept=".csv,text/csv" disabled={submitting} onChange={(event) => {
      const file = event.currentTarget.files?.[0]
      if (file) chooseFile(file)
    }} /></label>
    {rows.length > 0 && <div className={styles.summary} role="status" aria-live="polite"><MetricItem label="已解析" value={`${rows.length} 行`} /><MetricItem label="可提交" value={`${items.length} 行`} /><MetricItem label="待修正" value={`${invalidRows.length} 行`} /></div>}
    {invalidRows.length > 0 && <ul className={styles.importErrors} role="alert">{invalidRows.map((row) => <li key={row.row}>CSV 第 {row.row} 行：{row.error}</li>)}</ul>}
    {validForSubmit && !result && <div className={styles.importActions}>
      {!confirming
        ? <button className={styles.primary} type="button" disabled={submitting} onClick={() => { setError(''); setConfirming(true) }}>提交导入</button>
        : <div className={styles.importConfirm} role="status"><span>将逐行创建 {items.length} 个店铺。成功店铺仍需确认坐标后才能启用天气。</span><button type="button" disabled={submitting} onClick={() => setConfirming(false)}>返回检查</button><button className={styles.primary} type="button" disabled={submitting} onClick={() => void submit()}>{submitting ? '导入中…' : '确认并提交'}</button></div>}
      {canRetryOriginal && <button type="button" disabled={submitting} onClick={() => void submit()}>重试原请求</button>}
      <button type="button" disabled={submitting} onClick={abandon}>放弃本次导入</button>
    </div>}
    {result && <MallImportResultPanel result={result} rows={rows} onAbandon={abandon} />}
    {error && <p className={`${styles.message} ${styles.error}`} role="alert">{error}</p>}
  </section>
}

function MallImportResultPanel({ result, rows, onAbandon }: { result: MallImportResult; rows: MallImportRow[]; onAbandon: () => void }) {
  return <div className={styles.importResult} aria-live="polite">
    <div className={styles.summary}><MetricItem label="已创建" value={`${result.created} 行`} /><MetricItem label="幂等重放" value={`${result.replayed} 行`} /><MetricItem label="失败" value={`${result.failed} 行`} /></div>
    <ul className={styles.importResultRows}>
      {result.rows.map((row) => <li key={row.row} data-status={row.status}>
        <strong>{row.status === 'CREATED' ? '已创建' : row.status === 'REPLAYED' ? '已确认创建' : '未创建'}</strong>
        <span>CSV 第 {rows[row.row - 1]?.row ?? row.row + 1} 行</span>
        {row.mallCode && <span>{row.mallCode}</span>}
        {row.reviewStatus && <span>后续：{row.reviewStatus === 'PENDING_GEOCODE' ? '等待确认坐标' : row.reviewStatus}</span>}
        {row.errorCode && <span>{mallImportErrorCodeLabel(row.errorCode)}</span>}
      </li>)}
    </ul>
    <button type="button" onClick={onAbandon}>开始下一次导入</button>
  </div>
}

function importSubmissionError(status: number) {
  if (status === 0) return '导入结果暂不确定；已保留原请求，请点击“重试原请求”确认。'
  if (status === 409) return '导入请求正在处理或发生冲突；已保留原请求，请点击“重试原请求”确认。'
  if (status === 403) return '当前账号缺少 mall.write 权限。'
  if (status === 413) return '导入文件或请求内容超过服务端限制。'
  if (status === 422) return '导入内容校验失败，请检查 CSV 后重新选择有效文件。'
  return actionError(status, '批量导入失败', '当前账号缺少 mall.write 权限')
}

function mallImportErrorCodeLabel(code: 'INVALID_INPUT' | 'CONFLICT' | 'UNAVAILABLE') {
  if (code === 'INVALID_INPUT') return '字段校验未通过'
  if (code === 'CONFLICT') return '商场编码或幂等状态冲突'
  return '服务暂不可用，可稍后重新提交本文件'
}

function pendingCreateInput(pending: MallWeatherPendingCreate | null): MallWeatherCreateInput {
  if (!pending) return emptyMallCreateInput
  return {
    mallCode: pending.body.mallCode,
    nameCn: pending.body.nameCn,
    kaiyuShopId: pending.body.kaiyuShopId,
    province: pending.body.province,
    city: pending.body.city,
    district: pending.body.district || '',
    address: pending.body.address,
  }
}

export function MallCreatePanel({ actorID, client, onCreated, onCancel }: {
  actorID: string
  client: ApiClient
  onCreated: (mall: MallWeatherMall) => void
  onCancel: () => void
}) {
  const restored = useMemo(() => loadMallWeatherPendingCreate(actorID, browserSessionStorage), [actorID])
  const [form, setForm] = useState<MallWeatherCreateInput>(() => pendingCreateInput(restored))
  const [pending, setPending] = useState<MallWeatherPendingCreate | null>(restored)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  function change(field: keyof MallWeatherCreateInput, value: string) {
    setForm((current) => ({ ...current, [field]: value }))
    setPending(null)
    clearMallWeatherPendingCreate(actorID, browserSessionStorage)
    setError('')
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    let request = pending
    if (!request) {
      try {
        request = { key: mallWeatherCreateKey(), body: mallWeatherCreateRequest(form) }
      } catch {
        setError('请完整填写商场编码、名称、省市和地址；编码仅支持字母、数字、下划线和连字符。')
        return
      }
      setPending(request)
      saveMallWeatherPendingCreate(actorID, request, browserSessionStorage)
    }
    setSubmitting(true)
    setError('')
    const response = await client('/v1/malls', {
      method: 'POST', body: request.body, headers: { 'Idempotency-Key': request.key }, showResult: false, silentLoading: true,
    })
    setSubmitting(false)
    if (!response.ok) {
      if (response.status === 0 || response.status === 409) {
        setError(response.status === 0 ? '创建结果暂不确定，已保留原请求；请点击“重试原请求”确认。' : '创建请求正在处理或发生冲突，请先重试原请求；仍失败时再修改表单。')
      } else {
        setPending(null)
        clearMallWeatherPendingCreate(actorID, browserSessionStorage)
        setError(actionError(response.status, '商场创建失败', '当前账号缺少 mall.write 权限'))
      }
      return
    }
    const created = parseMallWeatherCreateResult(response.data)
    if (!created) {
      setError('商场已提交，但响应格式不正确；请刷新列表确认结果。')
      return
    }
    setPending(null)
    clearMallWeatherPendingCreate(actorID, browserSessionStorage)
    onCreated({
      id: created.id,
      mallCode: created.mallCode,
      nameCn: request.body.nameCn,
      kaiyuShopId: request.body.kaiyuShopId,
      province: request.body.province,
      city: request.body.city,
      district: request.body.district || '',
      address: request.body.address,
      coordinateSystem: '',
      geocodeStatus: created.geocodeStatus,
      weatherEnabled: false,
      detailProfile: request.body.weather.detailProfile,
      coverageRadiusM: request.body.weather.coverageRadiusM,
      timeZone: 'Asia/Shanghai',
      status: created.status,
      version: created.version,
    })
  }

  return <section className={`${styles.panel} ${styles.createPanel}`} id="mall-weather-create-panel">
    <div className={styles.sectionTitle}><div><strong>新增商场</strong><span>创建后继续确认坐标并启用天气</span></div><span>天气口径：full · 1000 m</span></div>
    <form className={styles.createForm} onSubmit={submit} aria-busy={submitting}>
      <MallDetailsFields form={form} onChange={change} disabled={submitting} />
      <div className={`${styles.formActions} ${styles.wide}`}>
        <button className={styles.primary} type="submit" disabled={submitting}>{submitting ? '提交中' : pending ? '重试原请求' : '创建并继续'}</button>
        <button type="button" onClick={onCancel} disabled={submitting}>取消</button>
      </div>
    </form>
    {error && <p className={`${styles.message} ${styles.error}`} role="alert">{error}</p>}
  </section>
}

function MetricItem({ label, value }: { label: string; value: string }) {
  return <div className={styles.metricItem}><span>{label}</span><strong>{value || '—'}</strong></div>
}

function actionError(status: number, fallback: string, forbidden: string) {
  if (status === 0) return '无法连接服务，请检查网络后重试'
  if (status === 403) return forbidden
  if (status === 404) return '商场或坐标候选不存在，请刷新状态后重试'
  if (status === 409) return '商场状态已变化，请刷新后重试'
  if (status === 422) return '提交内容校验失败，请检查输入后重试'
  return `${fallback}（HTTP ${status}）`
}
