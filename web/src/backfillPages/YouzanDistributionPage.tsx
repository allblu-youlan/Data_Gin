import { formatShanghaiDateTime } from '../dateTime.js'
import { AlertTriangle } from 'lucide-react'
import { useRef, useState, type FormEvent, type ReactNode } from 'react'
import type { ClientResponse } from '../api/client'
import { DataTable, Dialog, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, Section, StatusTag } from '../ui'
import styles from './YouzanDistributionPage.module.css'
import {
  backendDateTime,
  backfillStatusLabel,
  backfillStatusTone,
  formValue,
  previousDayDateTimeLocal,
  readResponseObject,
  timeFilterLabel,
  type LegacyTask,
  type LegacyTaskRunResult,
  type YouzanDistributionBackfillPayload,
  type YouzanDistributionBackfillResult,
  type YouzanDistributionTimeFilter,
} from './youzanDistributionSupport'

type YouzanClientOptions = {
  method: 'POST'
  body: unknown
  showResult?: boolean
}

type YouzanClient = (path: string, options: YouzanClientOptions) => Promise<ClientResponse>

type YouzanDistributionPageProps = {
  client: YouzanClient
  task?: LegacyTask
  loading: boolean
  onCompletedRefresh: () => Promise<unknown>
}

export function YouzanDistributionPage({ client, task, loading, onCompletedRefresh }: YouzanDistributionPageProps) {
  const previewVersionRef = useRef(0)
  const [timeFilter, setTimeFilter] = useState<YouzanDistributionTimeFilter>('created')
  const [payload, setPayload] = useState<YouzanDistributionBackfillPayload | null>(null)
  const [preview, setPreview] = useState<YouzanDistributionBackfillResult | null>(null)
  const [confirmed, setConfirmed] = useState<YouzanDistributionBackfillResult | null>(null)
  const [error, setError] = useState('')
  const [showManualRun, setShowManualRun] = useState(false)
  const [manualRunPayload, setManualRunPayload] = useState<YouzanDistributionBackfillPayload | null>(null)
  const [manualRunResult, setManualRunResult] = useState<LegacyTaskRunResult | null>(null)
  const [manualRunError, setManualRunError] = useState('')
  const [runningManualTask, setRunningManualTask] = useState(false)
  const [confirmingBackfill, setConfirmingBackfill] = useState(false)
  const [writingBackfill, setWritingBackfill] = useState(false)

  function invalidateBackfillPreview() {
    previewVersionRef.current += 1
    setPayload(null)
    setPreview(null)
    setConfirmed(null)
    setError('')
    setConfirmingBackfill(false)
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const nextPayload: YouzanDistributionBackfillPayload = {
      time_filter: timeFilter,
      start_time: backendDateTime(formValue(form, 'start_time')),
      end_time: backendDateTime(formValue(form, 'end_time')),
    }
    const requestVersion = previewVersionRef.current + 1
    invalidateBackfillPreview()
    try {
      const response = await client('/v1/youzan-distribution-order-backfill/preview', { method: 'POST', body: nextPayload })
      if (previewVersionRef.current !== requestVersion) return
      const result = response.ok ? readResponseObject<YouzanDistributionBackfillResult>(response, 'result') : null
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

  async function confirmBackfill() {
    if (!payload || !preview || writingBackfill) return
    setWritingBackfill(true)
    setError('')
    try {
      const response = await client('/v1/youzan-distribution-order-backfill/confirm', { method: 'POST', body: payload })
      const result = response.ok ? readResponseObject<YouzanDistributionBackfillResult>(response, 'result') : null
      if (!result) {
        setError(response.error?.message || '补拉写入失败，请重新预览后再试。')
        return
      }
      setConfirmed(result)
      setConfirmingBackfill(false)
      await onCompletedRefresh()
    } catch {
      setError('补拉写入失败，请稍后重试。')
    } finally {
      setWritingBackfill(false)
    }
  }

  function changeTimeFilter(value: string) {
    if (value !== 'created' && value !== 'success') return
    setTimeFilter(value)
    invalidateBackfillPreview()
  }

  function openManualRun() {
    setManualRunPayload(null)
    setManualRunResult(null)
    setManualRunError('')
    setShowManualRun(true)
  }

  function closeManualRun() {
    if (!runningManualTask) setShowManualRun(false)
  }

  function prepareManualRun(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setManualRunPayload({
      time_filter: formValue(form, 'time_filter') === 'success' ? 'success' : 'created',
      start_time: backendDateTime(formValue(form, 'start_time')),
      end_time: backendDateTime(formValue(form, 'end_time')),
    })
  }

  async function confirmManualRun() {
    if (!task || !manualRunPayload || runningManualTask) return
    setRunningManualTask(true)
    setManualRunError('')
    try {
      const response = await client(`/v1/legacy-tasks/${encodeURIComponent(task.code)}/run`, { method: 'POST', body: manualRunPayload })
      if (!response.ok) {
        setManualRunError(response.error?.message || '任务投递失败，请稍后重试。')
        return
      }
      const result = readResponseObject<LegacyTaskRunResult>(response, 'result')
      if (result?.id && result.queue && result.type) {
        setManualRunResult(result)
        await onCompletedRefresh()
      } else {
        setManualRunError('任务已提交，但未收到完整的队列任务信息。请在任务系统中核对执行状态。')
      }
    } catch {
      setManualRunError('任务投递失败，请稍后重试。')
    } finally {
      setRunningManualTask(false)
    }
  }

  return <PageCanvas>
    <PageHeader
      eyebrow="DATA BACKFILL"
      title="有赞分销订单"
      description="核对每日自动任务，并按时间范围预览、判重和确认写入有赞分销订单。"
      actions={task ? <button type="button" onClick={openManualRun} disabled={loading}>运行计划任务</button> : undefined}
      context={<StatusTag tone={task ? 'success' : 'warning'}>{task ? `计划任务 ${task.cron_expr}` : '任务定义未就绪'}</StatusTag>}
    />

    <Section title="每日自动拉取" description="任务定义由后端统一注册，本页仅展示当前执行约束。">
      {!task ? <FeedbackState kind="empty" title="尚未加载任务定义" description="请确认后端已部署有赞分销任务并刷新页面。" /> : <TaskDefinition task={task} />}
    </Section>

    <Section title="时间范围补拉" description="预览会真实拉取、批量解密并判重，但不会写入数据库。" actions={<StatusTag tone={preview ? 'success' : 'neutral'}>{preview ? '预览已就绪' : '等待预览'}</StatusTag>}>
      <form onSubmit={submit}>
        <FilterToolbar
          label="有赞分销补拉条件"
          actions={<div className={styles.actions}><button className={styles.primary} type="submit" disabled={loading || writingBackfill}>{loading ? '预览中…' : '预览补拉'}</button><button type="button" disabled={loading || writingBackfill || !preview || preview.writable_count === 0} onClick={() => setConfirmingBackfill(true)}>确认写入</button></div>}
        >
          <SelectField value={timeFilter} onChange={changeTimeFilter} />
          <DateTimeField label={timeFilter === 'created' ? '下单开始时间' : '完成开始时间'} name="start_time" defaultValue={previousDayDateTimeLocal(false)} onChange={invalidateBackfillPreview} />
          <DateTimeField label={timeFilter === 'created' ? '下单结束时间' : '完成结束时间'} name="end_time" defaultValue={previousDayDateTimeLocal(true)} onChange={invalidateBackfillPreview} />
        </FilterToolbar>
      </form>
      <div className={styles.warning}><AlertTriangle aria-hidden="true" /><span><strong>判重规则</strong>已有 tid 不覆盖；非空 fans_nickname 解密失败时，本页订单不会写入。</span></div>
    </Section>

    {error ? <FeedbackState kind="error" title="有赞分销补拉未完成" description={error} /> : null}
    {preview ? <BackfillResult title="预览结果" result={preview} /> : <FeedbackState kind="empty" title="等待补拉预览" description="选择筛选方式和时间范围后，可在这里核对订单样例与可写入数量。" />}
    {confirmed ? <BackfillResult title="本次写入结果" result={confirmed} /> : null}

    <Dialog open={confirmingBackfill && Boolean(preview)} title="确认写入有赞分销订单" role="alertdialog" closeDisabled={loading || writingBackfill} onClose={() => { if (!loading && !writingBackfill) setConfirmingBackfill(false) }} footer={<><button type="button" disabled={loading || writingBackfill} onClick={() => setConfirmingBackfill(false)}>返回预览</button><button className={styles.primary} type="button" disabled={loading || writingBackfill} onClick={() => void confirmBackfill()}>{writingBackfill ? '写入中…' : '确认写入'}</button></>}><p>确认写入 {preview?.writable_count ?? 0} 条有赞分销订单？系统会按 tid 判重，已有订单不会覆盖。</p></Dialog>

    <Dialog open={showManualRun && Boolean(task)} title="运行有赞分销计划任务" description="任务将投递至异步队列，不会直接写入本页。" closeDisabled={runningManualTask} onClose={closeManualRun} footer={manualRunPayload && !manualRunResult ? <><button type="button" onClick={() => setManualRunPayload(null)} disabled={runningManualTask}>返回修改</button><button className={styles.primary} type="button" onClick={() => void confirmManualRun()} disabled={runningManualTask}>{runningManualTask ? '投递中…' : '确认投递任务'}</button></> : undefined}>
      {task && (!manualRunPayload ? <ManualRunForm task={task} onSubmit={prepareManualRun} /> : <ManualRunConfirmation task={task} payload={manualRunPayload} result={manualRunResult} error={manualRunError} />)}
    </Dialog>
  </PageCanvas>
}

function TaskDefinition({ task }: { task: LegacyTask }) {
  return <dl className={styles.definition}>
    <Definition label="任务名称" value={task.name} />
    <Definition label="Cron" value={<code>{task.cron_expr}</code>} />
    <Definition label="数据来源" value={task.input_table} />
    <Definition label="写入表" value={<code>{task.output_table}</code>} />
    <Definition className={styles.wide} label="执行规则" value={task.description} />
    <Definition className={styles.wide} label="昵称处理" value="所有非空 fans_nickname 必须先批量解密；解密失败时本页订单不写入。" />
  </dl>
}

function Definition({ className, label, value }: { className?: string; label: string; value: ReactNode }) {
  return <div className={className}><dt>{label}</dt><dd>{value}</dd></div>
}

function BackfillResult({ title, result }: { title: string; result: YouzanDistributionBackfillResult }) {
  const samples = result.samples ?? []
  return <Section title={title} description={`${timeFilterLabel(result.time_filter)} / ${formatShanghaiDateTime(result.start_time)} ~ ${formatShanghaiDateTime(result.end_time)} / 拉取 ${result.fetch_pages} 页`} flush>
    <MetricStrip label={`${title}统计`} items={[{ key: 'total', label: '有赞返回', value: result.total_count }, { key: 'writable', label: '可写入', value: result.writable_count }, { key: 'existing', label: '已存在', value: result.existing_count }, { key: 'written', label: '已写入', value: result.saved_count }, { key: 'failed', label: '失败', value: result.failed_count }]} />
    {samples.length === 0 ? <FeedbackState kind="empty" title="暂无样例数据" /> : <DataTable minWidth={880} scrollLabel={`${title}订单样例`}><thead><tr><th scope="col">状态</th><th scope="col">订单号</th><th scope="col">成功时间</th><th scope="col">实付金额</th><th scope="col">解密昵称</th><th scope="col">说明</th></tr></thead><tbody>{samples.map((sample, index) => <tr key={`${sample.tid}-${sample.status}-${index}`}><td><StatusTag tone={backfillStatusTone(sample.status)}>{backfillStatusLabel(sample.status)}</StatusTag></td><td>{sample.tid || '-'}</td><td>{formatShanghaiDateTime(sample.success_time, '-')}</td><td>{sample.payment || '-'}</td><td>{sample.fans_nickname || '-'}</td><td>{sample.reason || '-'}</td></tr>)}</tbody></DataTable>}
  </Section>
}

function SelectField({ value, onChange }: { value: YouzanDistributionTimeFilter; onChange: (value: string) => void }) {
  return <label className={styles.field}>时间筛选方式<select name="time_filter" value={value} onChange={(event) => onChange(event.currentTarget.value)}><option value="created">下单时间</option><option value="success">订单完成时间</option></select></label>
}

function DateTimeField({ label, name, defaultValue, onChange }: { label: string; name: string; defaultValue: string; onChange?: () => void }) {
  return <label className={styles.field}>{label}<input name={name} type="datetime-local" step="1" defaultValue={defaultValue} onChange={onChange} required /></label>
}

function ManualRunForm({ task, onSubmit }: { task: LegacyTask; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  return <form className={styles.dialogForm} onSubmit={onSubmit}>
    <label className={styles.field}>任务编码<input name="task_code" value={task.code} readOnly /></label>
    <label className={styles.field}>时间筛选方式<select name="time_filter" defaultValue="created"><option value="created">下单时间</option><option value="success">订单完成时间</option></select></label>
    <DateTimeField label="开始时间" name="start_time" defaultValue={previousDayDateTimeLocal(false)} />
    <DateTimeField label="结束时间" name="end_time" defaultValue={previousDayDateTimeLocal(true)} />
    <div className={styles.dialogFormActions}><button className={styles.primary} type="submit">继续确认</button></div>
  </form>
}

function ManualRunConfirmation({ task, payload, result, error }: { task: LegacyTask; payload: YouzanDistributionBackfillPayload; result: LegacyTaskRunResult | null; error: string }) {
  return <div className={styles.confirmation}>
    <dl className={styles.definition}>
      <Definition label="任务编码" value={<code>{task.code}</code>} />
      <Definition label="筛选方式" value={timeFilterLabel(payload.time_filter)} />
      <Definition className={styles.wide} label="执行时间范围" value={`${formatShanghaiDateTime(payload.start_time)} ~ ${formatShanghaiDateTime(payload.end_time)}`} />
    </dl>
    {error ? <FeedbackState kind="error" title="任务未投递" description={error} /> : null}
    {result ? <FeedbackState kind="empty" title="任务已投递" description={`任务 ID ${result.id}，队列 ${result.queue}，类型 ${result.type}。`} /> : null}
  </div>
}
