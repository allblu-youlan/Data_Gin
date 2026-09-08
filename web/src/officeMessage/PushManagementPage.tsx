import { useCallback, useEffect, useMemo, useState } from 'react'
import { Calendar, Pencil, Plus, RefreshCw, Send, Trash2 } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { DataTable, Dialog, FeedbackState, PageCanvas, PageHeader, Section, StatusTag } from '../ui'
import { createOfficeRun, deleteOfficeSchedule, deleteOfficeTarget, getOfficeFeishuBots, getOfficeMessages, getOfficeRuns, getOfficeSchedules, getOfficeTargets, saveOfficeSchedule, saveOfficeTarget } from './api'
import type { OfficeFeishuBot, OfficeMessage, OfficePushRun, OfficePushSchedule, OfficePushTarget } from './types'
import { PushTargetDrawer } from './PushTargetDrawer'
import { buildPushTargetPayload, emptyPushTarget, targetDraftFrom, type PushTargetDraft } from './pushTargetDraft'
import { PushRunDialog } from './PushRunDialog'
import { PushScheduleDrawer } from './PushScheduleDrawer'
import { emptyPushSchedule, parametersForTarget, scheduleDraftFrom, type PushScheduleDraft } from './pushScheduleDraft'
import styles from './OfficeMessage.module.css'

type Props = { client: WorkspaceApiClient; permissions: string[] }
type Notice = { text: string; error: boolean }
type BotLoadState = 'loading' | 'ready' | 'failed'

export function PushManagementPage({ client, permissions }: Props) {
  const [messages, setMessages] = useState<OfficeMessage[]>([])
  const [bots, setBots] = useState<OfficeFeishuBot[]>([])
  const [botLoadState, setBotLoadState] = useState<BotLoadState>('loading')
  const [targets, setTargets] = useState<OfficePushTarget[]>([])
  const [runs, setRuns] = useState<OfficePushRun[]>([])
  const [schedules, setSchedules] = useState<OfficePushSchedule[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [reload, setReload] = useState(0)
  const [draft, setDraft] = useState<PushTargetDraft | null>(null)
  const [draftError, setDraftError] = useState('')
  const [scheduleDraft, setScheduleDraft] = useState<PushScheduleDraft | null>(null)
  const [scheduleError, setScheduleError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<OfficePushTarget | null>(null)
  const [pendingScheduleDelete, setPendingScheduleDelete] = useState<OfficePushSchedule | null>(null)
  const [pushTarget, setPushTarget] = useState<OfficePushTarget | null>(null)
  const [pushValues, setPushValues] = useState<Record<string, string>>({})
  const [pushRequestId, setPushRequestId] = useState('')
  const [pushError, setPushError] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)
  const canManage = permissions.includes('office_message.manage')
  const canPush = permissions.includes('office_message.push')
  const messageById = useMemo(() => new Map(messages.map((message) => [message.id, message])), [messages])
  const botById = useMemo(() => new Map(bots.map((bot) => [bot.id, bot])), [bots])
  const targetById = useMemo(() => new Map(targets.map((target) => [target.id, target])), [targets])

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setBotLoadState('loading')
    const [botResult, messageResult, targetResult, scheduleResult, runResult] = await Promise.all([getOfficeFeishuBots(client, signal), getOfficeMessages(client, signal), getOfficeTargets(client, signal), getOfficeSchedules(client, signal), getOfficeRuns(client, signal)])
    if (signal?.aborted) return
    if (botResult.ok) {
      setBots(botResult.data)
      setBotLoadState('ready')
    } else {
      setBots([])
      setBotLoadState('failed')
    }
    if (messageResult.ok) setMessages(messageResult.data)
    if (targetResult.ok) setTargets(targetResult.data)
    if (scheduleResult.ok) setSchedules(scheduleResult.data)
    if (runResult.ok) setRuns(runResult.data)
    const failed = [botResult, messageResult, targetResult, scheduleResult, runResult].find((result) => !result.ok)
    setNotice(failed && !failed.ok ? { text: failed.error, error: true } : null)
    setLoading(false)
  }, [client])

  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort() }, [load, reload])
  useEffect(() => {
    if (!runs.some((run) => run.status === 'QUEUED' || run.status === 'RUNNING')) return
    const timer = window.setInterval(() => setReload((value) => value + 1), 8000)
    return () => window.clearInterval(timer)
  }, [runs])

  function showNotice(text: string, error = false) { setNotice({ text, error }) }
  async function save() {
    if (!draft || saving) return
    let body: ReturnType<typeof buildPushTargetPayload>
    try {
      body = buildPushTargetPayload(draft, messages)
    } catch (error) {
      return setDraftError(error instanceof Error ? error.message : '推送配置无效。')
    }
    setSaving(true); setDraftError('')
    const result = await saveOfficeTarget(client, draft, body)
    setSaving(false)
    if (!result.ok) return setDraftError(result.error)
    setDraft(null); showNotice(`${pushChannelLabel(draft.channel)}推送配置已保存。`); setReload((value) => value + 1)
  }
  async function remove() {
    if (!pendingDelete || saving) return
    setSaving(true)
    const result = await deleteOfficeTarget(client, pendingDelete.id, pendingDelete.lockVersion)
    setSaving(false)
    if (!result.ok) return showNotice(result.error, true)
    setPendingDelete(null); showNotice('推送配置已删除。'); setReload((value) => value + 1)
  }
  async function saveSchedule() {
    if (!scheduleDraft || saving) return
    const cronExpr = scheduleDraft.cronExpr.trim().replace(/\s+/g, ' ')
    if (!scheduleDraft.name.trim() || !scheduleDraft.targetId) return setScheduleError('请填写计划名称并选择推送配置。')
    if (cronExpr.split(' ').length !== 5) return setScheduleError('Cron 表达式必须包含五段。')
    const target = targetById.get(scheduleDraft.targetId)
    const message = messages.find((item) => item.id === target?.messageId)
    if (!target || !message) return setScheduleError('所选推送配置或消息已不存在，请刷新后重试。')
    const parameters = parametersForTarget(scheduleDraft.targetId, targets, messages, scheduleDraft.parameters)
    const invalid = message?.parameters.find((parameter) => {
      const value = parameters[parameter.code]
      if (!value || !Number.isInteger(value.offsetDays)) return true
      if (parameter.valueType !== 'date' && value.mode !== 'LITERAL') return true
      return parameter.required && value.mode === 'LITERAL' && !value.value.trim()
    })
    if (invalid) return setScheduleError(`请正确配置参数“${invalid.label}”。`)
    setSaving(true); setScheduleError('')
    const result = await saveOfficeSchedule(client, scheduleDraft, { name: scheduleDraft.name.trim(), targetId: scheduleDraft.targetId, cronExpr, timeZone: scheduleDraft.timeZone, parameters, enabled: scheduleDraft.enabled, expectedLockVersion: scheduleDraft.id ? scheduleDraft.lockVersion : 0 })
    setSaving(false)
    if (!result.ok) return setScheduleError(result.error)
    setScheduleDraft(null); showNotice('定时计划已保存。'); setReload((value) => value + 1)
  }
  async function removeSchedule() {
    if (!pendingScheduleDelete || saving) return
    setSaving(true)
    const result = await deleteOfficeSchedule(client, pendingScheduleDelete.id, pendingScheduleDelete.lockVersion)
    setSaving(false)
    if (!result.ok) return showNotice(result.error, true)
    setPendingScheduleDelete(null); showNotice('定时计划已删除。'); setReload((value) => value + 1)
  }
  async function push() {
    if (!pushTarget || saving) return
    const message = messageById.get(pushTarget.messageId)
    const missing = message?.parameters.find((parameter) => parameter.required && !pushValues[parameter.code]?.trim())
    if (missing) return setPushError(`请填写${missing.label}。`)
    setSaving(true); setPushError('')
    const result = await createOfficeRun(client, pushTarget.id, pushRequestId, pushValues)
    setSaving(false)
    if (!result.ok) return setPushError(result.error)
    setPushTarget(null); setPushValues({}); setPushRequestId(''); showNotice(`推送任务 #${result.data.id} 已进入队列。`); setReload((value) => value + 1)
  }
  function pushDisabled(target: OfficePushTarget) {
    const message = messageById.get(target.messageId)
    if (!target.enabled || !message?.enabled) return true
    if (target.channel === 'WEBDAV') return message.sourceType === 'EDITED' || !target.hasWebdavPassword
    if (botLoadState === 'loading') return true
    return botLoadState === 'ready' && !botById.has(target.botAppId)
  }

  return <PageCanvas>
    <PageHeader eyebrow="DELIVERY AUTOMATION" title="推送管理" description="维护飞书消息和 WebDAV Excel 文件推送，支持手动或按计划发起持久化异步任务。" actions={<div className={styles.actions}><button type="button" disabled={loading} onClick={() => setReload((value) => value + 1)}><RefreshCw aria-hidden="true" />刷新</button>{canManage ? <button type="button" className={styles.primary} disabled={loading} onClick={() => { setDraftError(''); setDraft(emptyPushTarget(messages, bots)) }}><Plus aria-hidden="true" />新增配置</button> : null}</div>} />
    {notice ? <p className={notice.error ? styles.errorNotice : styles.notice} role={notice.error ? 'alert' : 'status'}>{notice.text}</p> : null}
    {!loading && botLoadState === 'ready' && bots.length === 0 ? <p className={styles.notice} role="status">服务端尚未配置飞书机器人；飞书通道暂不可用，仍可新增 WebDAV 文件推送配置。</p> : null}
    <Section title="推送配置" description={botLoadState === 'failed' ? `共 ${targets.length} 个目标；飞书机器人列表加载失败，WebDAV 配置不受影响。` : `共 ${targets.length} 个目标；支持飞书消息和 WebDAV Excel 文件。`} flush>
      {loading && targets.length === 0 ? <FeedbackState kind="loading" title="正在加载推送配置" /> : targets.length === 0 ? <FeedbackState kind="empty" title="暂无推送配置" /> : <DataTable containerClassName={styles.table} minWidth={1160}><thead><tr><th scope="col">配置</th><th scope="col">通道</th><th scope="col">推送目标</th><th scope="col">消息</th><th scope="col">接收方 / 目录</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>{targets.map((target) => <tr key={target.id}><td><span className={styles.identity}><Send aria-hidden="true" /><span><strong>{target.name}</strong><code>#{target.id}</code></span></span></td><td><StatusTag tone={target.channel === 'WEBDAV' ? 'info' : 'neutral'}>{pushChannelLabel(target.channel)}</StatusTag></td><td>{target.channel === 'WEBDAV' ? <><strong>{target.webdavUsername}</strong><small className={styles.cellHint}><code>{target.webdavUrl}</code></small></> : <><strong>{botLoadState === 'failed' ? '机器人状态待确认' : botById.get(target.botAppId)?.name ?? '未知机器人'}</strong><small className={styles.cellHint}><code>{target.botAppId || '未绑定'}</code></small></>}</td><td>{messageById.get(target.messageId)?.name ?? `消息 #${target.messageId}`}</td><td><code>{target.channel === 'WEBDAV' ? target.webdavPath : `${target.receiveIdType}:${target.receiveId}`}</code></td><td><StatusTag tone={target.enabled ? 'success' : 'neutral'}>{target.enabled ? '启用' : '停用'}</StatusTag></td><td><div className={styles.actions}>{canPush ? <button type="button" disabled={pushDisabled(target)} onClick={() => { setPushError(''); setPushValues({}); setPushRequestId(crypto.randomUUID()); setPushTarget(target) }}><Send aria-hidden="true" />立即推送</button> : null}{canManage ? <><button type="button" onClick={() => { setDraftError(''); setDraft(targetDraftFrom(target)) }}><Pencil aria-hidden="true" />编辑</button><button type="button" onClick={() => setPendingDelete(target)}><Trash2 aria-hidden="true" />删除</button></> : null}</div></td></tr>)}</tbody></DataTable>}
    </Section>
    <Section title="定时计划" description={`共 ${schedules.length} 个计划；统一按 Asia/Shanghai 时区执行。`} actions={canManage ? <button type="button" className={styles.primary} disabled={targets.length === 0} onClick={() => { setScheduleError(''); setScheduleDraft(emptyPushSchedule(targets, messages)) }}><Plus aria-hidden="true" />新增计划</button> : undefined} flush>
      {loading && schedules.length === 0 ? <FeedbackState kind="loading" title="正在加载定时计划" /> : schedules.length === 0 ? <FeedbackState kind="empty" title="暂无定时计划" /> : <DataTable containerClassName={styles.table} minWidth={1080}><thead><tr><th scope="col">计划</th><th scope="col">推送配置</th><th scope="col">Cron / 时区</th><th scope="col">下次执行</th><th scope="col">上次执行</th><th scope="col">状态 / 错误</th><th scope="col">操作</th></tr></thead><tbody>{schedules.map((schedule) => <tr key={schedule.id}><td><span className={styles.identity}><Calendar aria-hidden="true" /><span><strong>{schedule.name}</strong><code>#{schedule.id}</code></span></span></td><td>{targetById.get(schedule.targetId)?.name ?? `配置 #${schedule.targetId}`}</td><td><code>{schedule.cronExpr}</code><small className={styles.cellHint}>{schedule.timeZone}</small></td><td>{formatTime(schedule.nextRunAt)}</td><td>{formatTime(schedule.lastScheduledAt)}</td><td><StatusTag tone={schedule.enabled ? 'success' : 'neutral'}>{schedule.enabled ? '启用' : '停用'}</StatusTag>{schedule.lastError ? <small className={styles.cellHint}>{schedule.lastError}</small> : null}</td><td><div className={styles.actions}>{canManage ? <><button type="button" onClick={() => { setScheduleError(''); setScheduleDraft(scheduleDraftFrom(schedule)) }}><Pencil aria-hidden="true" />编辑</button><button type="button" onClick={() => setPendingScheduleDelete(schedule)}><Trash2 aria-hidden="true" />删除</button></> : null}</div></td></tr>)}</tbody></DataTable>}
    </Section>
    <Section title="最近推送记录" description="排队和运行中的记录会每 8 秒自动刷新。" flush>{loading && runs.length === 0 ? <FeedbackState kind="loading" title="正在加载推送记录" /> : runs.length === 0 ? <FeedbackState kind="empty" title="暂无推送记录" /> : <DataTable containerClassName={styles.table} minWidth={1080}><thead><tr><th scope="col">任务</th><th scope="col">触发方式</th><th scope="col">配置 / 通道</th><th scope="col">消息</th><th scope="col">状态</th><th scope="col">尝试</th><th scope="col">行数</th><th scope="col">创建时间</th><th scope="col">结果</th></tr></thead><tbody>{runs.map((run) => { const target = targetById.get(run.targetId); return <tr key={run.id}><td><strong>#{run.id}</strong><small className={styles.cellHint}>{run.runUuid.slice(0, 8)}</small></td><td>{run.triggerType === 'SCHEDULE' ? <><strong>定时计划 #{run.scheduleId}</strong><small className={styles.cellHint}>{formatTime(run.scheduledFor)}</small></> : '手动推送'}</td><td>{target?.name ?? `配置 #${run.targetId}`}<small className={styles.cellHint}>{target ? pushChannelLabel(target.channel) : '通道未知'}</small></td><td>{messageById.get(run.messageId)?.name ?? `消息 #${run.messageId}`}</td><td><RunStatus status={run.status} /></td><td>{run.attemptCount}</td><td>{run.rowCount || '—'}</td><td>{formatTime(run.createdAt)}</td><td>{run.errorMessage || (run.finishedAt ? formatTime(run.finishedAt) : '—')}</td></tr> })}</tbody></DataTable>}</Section>
    <PushTargetDrawer draft={draft} messages={messages} bots={bots} saving={saving} error={draftError} onChange={setDraft} onClose={() => { if (!saving) setDraft(null) }} onSave={() => void save()} />
    <PushRunDialog target={pushTarget} message={pushTarget ? messageById.get(pushTarget.messageId) ?? null : null} values={pushValues} sending={saving} error={pushError} onValuesChange={setPushValues} onClose={() => { if (!saving) { setPushTarget(null); setPushRequestId('') } }} onConfirm={() => void push()} />
    <PushScheduleDrawer draft={scheduleDraft} targets={targets} messages={messages} saving={saving} error={scheduleError} onChange={setScheduleDraft} onClose={() => { if (!saving) setScheduleDraft(null) }} onSave={() => void saveSchedule()} />
    <Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除推送配置" description="已创建的历史运行记录会保留。" closeDisabled={saving} onClose={() => setPendingDelete(null)} footer={<><button type="button" disabled={saving} onClick={() => setPendingDelete(null)}>取消</button><button type="button" className={styles.danger} disabled={saving} onClick={() => void remove()}>{saving ? '删除中…' : '确认删除'}</button></>}><p className={styles.dialogCopy}>确认删除“{pendingDelete?.name}”？</p></Dialog>
    <Dialog open={Boolean(pendingScheduleDelete)} role="alertdialog" title="删除定时计划" description="已创建的历史运行记录会保留。" closeDisabled={saving} onClose={() => setPendingScheduleDelete(null)} footer={<><button type="button" disabled={saving} onClick={() => setPendingScheduleDelete(null)}>取消</button><button type="button" className={styles.danger} disabled={saving} onClick={() => void removeSchedule()}>{saving ? '删除中…' : '确认删除'}</button></>}><p className={styles.dialogCopy}>确认删除“{pendingScheduleDelete?.name}”？</p></Dialog>
  </PageCanvas>
}

function RunStatus({ status }: { status: OfficePushRun['status'] }) {
  const labels = { QUEUED: '排队中', RUNNING: '发送中', SUCCEEDED: '成功', FAILED: '失败', UNKNOWN: '状态未知' }
  const tones = { QUEUED: 'info', RUNNING: 'running', SUCCEEDED: 'success', FAILED: 'danger', UNKNOWN: 'warning' } as const
  return <StatusTag tone={tones[status]}>{labels[status]}</StatusTag>
}

function pushChannelLabel(channel: OfficePushTarget['channel']) { return channel === 'WEBDAV' ? 'WebDAV' : '飞书' }

function formatTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12: false, timeZone: 'Asia/Shanghai' }) }
