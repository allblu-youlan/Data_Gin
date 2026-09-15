import { AlertTriangle } from 'lucide-react'
import { useRef, useState, type FormEvent } from 'react'
import type { ClientResponse } from '../api/client'
import { DataTable, Dialog, FeedbackState, MetricStrip, PageCanvas, PageHeader, Section, StatusTag } from '../ui'
import styles from './BojunBackfillPage.module.css'
import type { LegacyTaskRunResult } from './youzanDistributionSupport'

type BackfillClient = (path: string, options: { method: 'POST'; body: unknown; showResult?: boolean }) => Promise<ClientResponse>

type BojunOrderBackfillSample = {
  docno: string
  otherdocno: string
  c_store_code: string
  c_store_name: string
  order_type_code: string
  order_type_name: string
  billdate: number
  tot_qty: number
  tot_amt_actual: number
  status: string
  reason: string
}

type BojunOrderBackfillResult = {
  start_time: string
  end_time: string
  page_size: number
  max_pages: number
  fetch_pages: number
  total_count: number
  preview_count: number
  writable_count: number
  existing_count: number
  saved_count: number
  updated_count: number
  retail_count: number
  skipped_count: number
  failed_count: number
  samples: BojunOrderBackfillSample[]
  failed_samples: BojunOrderBackfillSample[]
}

export function BojunBackfillPage({ client, loading, onCompletedRefresh }: { client: BackfillClient; loading: boolean; onCompletedRefresh: () => Promise<unknown> }) {
  const previewVersionRef = useRef(0)
  const [payload, setPayload] = useState<{ start_time: string; end_time: string } | null>(null)
  const [preview, setPreview] = useState<BojunOrderBackfillResult | null>(null)
  const [queuedTask, setQueuedTask] = useState<LegacyTaskRunResult | null>(null)
  const [confirmingWrite, setConfirmingWrite] = useState(false)
  const [writing, setWriting] = useState(false)
  const [error, setError] = useState('')

  function invalidatePreview() {
    previewVersionRef.current += 1
    setPayload(null)
    setPreview(null)
    setQueuedTask(null)
    setConfirmingWrite(false)
    setError('')
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const nextPayload = { start_time: formValue(form, 'start_time'), end_time: formValue(form, 'end_time') }
    const requestVersion = previewVersionRef.current + 1
    invalidatePreview()
    try {
      const response = await client('/v1/bojun-order-backfill/preview', { method: 'POST', body: nextPayload, showResult: false })
      if (previewVersionRef.current !== requestVersion) return
      const result = response.ok ? readResult(response) : null
      if (!result) {
        setError(response.error?.message || '补拉预览失败，请检查时间范围后重试。')
        return
      }
      setPayload(nextPayload)
      setPreview(result)
    } catch {
      if (previewVersionRef.current === requestVersion) setError('补拉预览失败，请稍后重试。')
    }
  }

  async function confirmWrite() {
    if (!payload || !preview || writing) return
    setWriting(true)
    setError('')
    try {
      const response = await client('/v1/bojun-order-backfill/confirm', { method: 'POST', body: payload, showResult: false })
      const task = response.ok ? readTaskResult(response) : null
      if (!task) {
        setError(response.error?.message || '补拉任务投递失败，请重新预览后再试。')
        return
      }
      setQueuedTask(task)
      setConfirmingWrite(false)
      await onCompletedRefresh()
    } catch {
      setError('补拉任务投递失败，请稍后重试。')
    } finally {
      setWriting(false)
    }
  }

  return <PageCanvas>
    <PageHeader eyebrow="DATA BACKFILL" title="伯俊订单补拉" description="先真实查询并预览，再按相同时间范围投递后台补拉任务。" />
    <Section title="补拉范围" description="Oracle 模式按完成时间查询默认 Oracle：本地不存在的订单完整新增，已存在的订单只更新商品和付款明细；API 模式保持原有补拉流程。预览阶段不会写入数据库。" actions={<StatusTag tone={preview ? 'success' : 'neutral'}>{preview ? '预览已就绪' : '等待预览'}</StatusTag>}>
      <form className={styles.form} onSubmit={submit}>
        <Field label="开始时间" name="start_time" defaultValue={datetimeLocalMinutesAgo(60)} onChange={invalidatePreview} />
        <Field label="结束时间" name="end_time" defaultValue={datetimeLocalMinutesAgo(0)} onChange={invalidatePreview} />
        <div className={styles.actions}><button className={styles.primary} type="submit" disabled={loading || writing}>{loading ? '预览中…' : '预览补拉'}</button><button type="button" disabled={loading || writing || Boolean(queuedTask) || !preview || preview.writable_count === 0} onClick={() => setConfirmingWrite(true)}>投递补拉</button></div>
      </form>
      <div className={styles.warning}><AlertTriangle aria-hidden="true" /><span><strong>投递前请确认</strong>后台任务会重新拉取相同时间范围，请以可写入数量为核对依据。</span></div>
    </Section>
    {error ? <FeedbackState kind="error" title="伯俊补拉未完成" description={error} /> : null}
    {preview ? <BackfillResult title="预览结果" result={preview} /> : <FeedbackState kind="empty" title="等待补拉预览" description="选择时间范围并预览后，可在这里核对订单样例与写入数量。" />}
    {queuedTask ? <FeedbackState kind="empty" title="补拉任务已投递" description={`任务 ID ${queuedTask.id}，队列 ${queuedTask.queue}，类型 ${queuedTask.type}。后台完成后可刷新数据查看结果。`} /> : null}
    <Dialog open={confirmingWrite && Boolean(preview)} title="确认投递伯俊补拉任务" role="alertdialog" closeDisabled={loading || writing} onClose={() => { if (!loading && !writing) setConfirmingWrite(false) }} footer={<><button type="button" disabled={loading || writing} onClick={() => setConfirmingWrite(false)}>取消</button><button className={styles.primary} type="button" disabled={loading || writing} onClick={() => void confirmWrite()}>{writing ? '投递中…' : '确认并投递'}</button></>}><p>确认投递预计处理 {preview?.writable_count ?? 0} 条订单的后台任务？Oracle 模式会完整新增缺失订单，已有订单只覆盖 items_json 和 pay_items_json。</p></Dialog>
  </PageCanvas>
}

function BackfillResult({ title, result }: { title: string; result: BojunOrderBackfillResult }) {
  const samples = [...(result.samples ?? []), ...(result.failed_samples ?? [])].slice(0, 12)
  return <Section title={title} description={`${result.start_time} ~ ${result.end_time} / 拉取 ${result.fetch_pages} 页`} flush>
    <MetricStrip label={`${title}统计`} items={[{ key: 'total', label: 'Oracle 返回', value: result.total_count }, { key: 'writable', label: '可处理', value: result.writable_count }, { key: 'existing', label: '已存在', value: result.existing_count }, { key: 'created', label: '已新增', value: result.retail_count }, { key: 'updated', label: '已更新', value: result.updated_count }, { key: 'failed', label: '失败', value: result.failed_count }]} />
    {samples.length === 0 ? <FeedbackState kind="empty" title="暂无样例数据" /> : <DataTable minWidth={860} scrollLabel={`${title}订单样例`}><thead><tr><th scope="col">状态</th><th scope="col">订单号</th><th scope="col">门店</th><th scope="col">类型</th><th scope="col">数量</th><th scope="col">金额</th><th scope="col">说明</th></tr></thead><tbody>{samples.map((sample, index) => <tr key={`${sample.docno || 'empty'}-${sample.status}-${index}`}><td><StatusTag tone={sampleTone(sample.status)}>{statusLabel(sample.status)}</StatusTag></td><td>{sample.docno || '-'}</td><td>{sample.c_store_name || sample.c_store_code || '-'}</td><td>{sample.order_type_name || sample.order_type_code || '-'}</td><td>{sample.tot_qty ?? '-'}</td><td>{sample.tot_amt_actual ?? '-'}</td><td>{sample.reason || '-'}</td></tr>)}</tbody></DataTable>}
  </Section>
}

function Field({ label, name, defaultValue, onChange }: { label: string; name: string; defaultValue: string; onChange: () => void }) {
  return <label>{label}<input name={name} type="datetime-local" defaultValue={defaultValue} onChange={onChange} required /></label>
}

function readResult(response: ClientResponse) {
  if (!response.data || typeof response.data !== 'object') return null
  const data = (response.data as { data?: Record<string, unknown> }).data
  return data?.result && typeof data.result === 'object' ? data.result as BojunOrderBackfillResult : null
}

function readTaskResult(response: ClientResponse) {
  if (!response.data || typeof response.data !== 'object') return null
  const data = (response.data as { data?: Record<string, unknown> }).data
  const result = data?.result
  if (!result || typeof result !== 'object' || Array.isArray(result)) return null
  const task = result as Record<string, unknown>
  return typeof task.id === 'string' && task.id.trim() !== '' && typeof task.queue === 'string' && task.queue.trim() !== '' && typeof task.type === 'string' && task.type.trim() !== ''
    ? { id: task.id, queue: task.queue, type: task.type }
    : null
}

function formValue(form: FormData, key: string) { const value = form.get(key); return typeof value === 'string' ? value : '' }
function datetimeLocalMinutesAgo(minutes: number) { const date = new Date(Date.now() - minutes * 60 * 1000); const pad = (value: number) => String(value).padStart(2, '0'); return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}` }
function statusLabel(value: string) { return ({ pending: '待写入', created: '已新增', updated: '已更新', exists: '已存在', invalid: '无效', failed: '失败', push_failed: '推送失败' } as Record<string, string>)[value] ?? (value || '-') }
function sampleTone(value: string) { return /created|updated/.test(value) ? 'success' as const : /failed|invalid/.test(value) ? 'danger' as const : value === 'pending' ? 'warning' as const : 'neutral' as const }
