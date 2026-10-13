import { useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { DataTable, Drawer, FeedbackState, PageCanvas, PageHeader, PaginationControls, Section, StatusTag } from '../ui'
import { mentionRequest, useMentionPage, useMentionResource } from './api'
import { cancelLabels, canRetryMentionJob, confirmationLabel, formatMentionTime as time, jobLabels, mentionNotice, mentionQuery, sectionTitles, statusLabels, viewLabels } from './contracts'
import { JobDetail, RecipientDetail, ReportDetail } from './MentionDetails'
import type { Chat, Job, MentionSection, ModuleStatus, Recipient, Report } from './types'
import styles from './OfficeMention.module.css'

type Props = { client: WorkspaceApiClient; permissions: string[]; section: MentionSection }
type Row = Chat | Recipient | Report | Job
type Filter = { key: string; label: string; type?: string; options?: Record<string, string> }
const commonFilters: Filter[] = [{ key: 'chatId', label: '来源群 ID' }, { key: 'recipientOpenId', label: '收件人 open_id' }]
const filtersBySection: Record<Exclude<MentionSection, 'status'>, Filter[]> = {
  chats: [{ key: 'keyword', label: '群名称' }, { key: 'chatId', label: '来源群 ID' }, { key: 'enabled', label: '启用状态', options: { true: '启用', false: '停用' } }],
  recipients: [...commonFilters, { key: 'viewStatus', label: '本人确认状态', options: viewLabels }, { key: 'receivedFrom', label: '接收时间起（上海）', type: 'datetime-local' }, { key: 'receivedTo', label: '接收时间止，不含（上海）', type: 'datetime-local' }],
  reports: [...commonFilters, { key: 'reportDateFrom', label: '报告日期起', type: 'date' }, { key: 'reportDateTo', label: '报告日期止', type: 'date' }, { key: 'status', label: '发送状态', options: { ready: '待发送', sent: '发送成功', failed: '发送失败' } }],
  jobs: [...commonFilters, { key: 'recipientRecordId', label: '人员记录 ID', type: 'number' }, { key: 'reportId', label: '日报 ID', type: 'number' }, { key: 'jobType', label: '任务类型', options: jobLabels }, { key: 'status', label: '任务状态', options: { pending: '等待发送', sending: '发送中', sent: '发送成功', retry_wait: '等待重试', failed: '发送失败', cancelled: '已取消' } }],
}
export function OfficeMentionPage(props: Props) {
  return props.section === 'status' ? <MentionStatusPage client={props.client} /> : <MentionListPage key={props.section} {...props} section={props.section} />
}
function MentionListPage({ client, permissions, section }: Props & { section: Exclude<MentionSection, 'status'> }) {
  const [draftFilters, setDraftFilters] = useState<Record<string, string>>({})
  const [filters, setFilters] = useState<Record<string, string>>({})
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [reload, setReload] = useState(0)
  const [notice, setNotice] = useState<{ text: string; error: boolean } | null>(null)
  const [selected, setSelected] = useState<{ kind: 'recipient' | 'report'; id: number } | { kind: 'job'; job: Job } | null>(null)
  const [draft, setDraft] = useState<Partial<Chat> | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [retrying, setRetrying] = useState<number[]>([])
  const { data, loading, error } = useMentionPage<Row>(client, `/${section}?${mentionQuery(filters, page, perPage)}`, reload)
  const canManage = permissions.includes('office_mention.manage')
  const notify = (text: string, error = false) => setNotice({ text, error })
  function filter(event: FormEvent) {
    event.preventDefault()
    const from = draftFilters.receivedFrom || draftFilters.reportDateFrom
    const to = draftFilters.receivedTo || draftFilters.reportDateTo
    if (Boolean(from) !== Boolean(to) || (from && to && (from > to || (section === 'recipients' && from === to)))) { notify('请同时填写有效的起止时间。', true); return }
    setFilters(draftFilters); setPage(1); setNotice(null)
  }
  async function retry(job: Job) {
    if (retrying.includes(job.id)) return
    setRetrying((ids) => [...ids, job.id])
    try { await mentionRequest(client, `/jobs/${job.id}/retry`, { method: 'POST', body: {} }); notify('已重新排队'); setReload((r) => r + 1) } catch (reason) { notify(reason instanceof Error ? reason.message : '重试失败。', true) } finally { setRetrying((ids) => ids.filter((id) => id !== job.id)) }
  }
  function retryButton(job: Job) { return canRetryMentionJob(job, permissions) ? <button type="button" disabled={retrying.includes(job.id)} onClick={() => void retry(job)}>{retrying.includes(job.id) ? '重新排队中…' : '重试失败任务'}</button> : null }
  async function saveChat(event: FormEvent) {
    event.preventDefault(); if (!draft || saving) return
    if (!draft.chatName?.trim() || (!draft.id && !draft.chatId?.trim()) || (draft.followupMinutes !== undefined && (!Number.isInteger(draft.followupMinutes) || draft.followupMinutes <= 0)) || (Boolean(draft.id) && draft.followupMinutes === undefined)) { setSaveError('请填写群 ID、群名；提醒间隔须为正整数，新建时可留空使用服务端默认值。'); return }
    setSaving(true); setSaveError('')
    const body = { ...(!draft.id ? { chatId: draft.chatId?.trim() } : {}), chatName: draft.chatName.trim(), isExternal: draft.isExternal ?? false, enabled: draft.enabled ?? true, followupMinutes: draft.followupMinutes }
    try { await mentionRequest(client, draft.id ? `/chats/${draft.id}` : '/chats', { method: draft.id ? 'PUT' : 'POST', body }); setDraft(null); setReload((r) => r + 1); notify('监控群设置已保存。登记成功后仍需确认机器人已入群且权限可用。') } catch (reason) { setSaveError(reason instanceof Error ? reason.message : '保存失败。') } finally { setSaving(false) }
  }
  const table = renderTable(section, data?.result ?? [], { onEdit: (chat) => { setDraft(chat); setSaveError('') }, onRecipient: (id) => setSelected({ kind: 'recipient', id }), onReport: (id) => setSelected({ kind: 'report', id }), onJob: (job) => setSelected({ kind: 'job', job }), retryButton, canManage })
  return <PageCanvas>
    <PageHeader eyebrow="FEISHU MENTIONS" title={sectionTitles[section]} description={mentionNotice} actions={<div className={styles.actions}><button type="button" onClick={() => setReload((r) => r + 1)} disabled={loading}>刷新</button>{section === 'chats' && canManage ? <button type="button" className={styles.primary} onClick={() => { setDraft({ chatId: '', chatName: '', enabled: true, isExternal: false }); setSaveError('') }}>新增监控群</button> : null}</div>} />
    {notice ? <p role={notice.error ? 'alert' : 'status'} className={notice.error ? styles.error : styles.success}>{notice.text}</p> : null}
    <Section title={sectionTitles[section]} description={data ? `共 ${data.pagination.total_count} 条；时间按 Asia/Shanghai 展示。` : '按来源群与人员标识查询。'} flush>
      <form className={styles.filters} onSubmit={filter}>{filtersBySection[section].map((field) => <label key={field.key}>{field.label}{field.options ? <select value={draftFilters[field.key] ?? ''} onChange={(event) => setDraftFilters({ ...draftFilters, [field.key]: event.target.value })}><option value="">全部</option>{Object.entries(field.options).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select> : <input type={field.type ?? 'text'} min={field.type === 'number' ? 1 : undefined} value={draftFilters[field.key] ?? ''} onChange={(event) => setDraftFilters({ ...draftFilters, [field.key]: event.target.value })} />}</label>)}<button type="submit" disabled={loading}>筛选</button><button type="button" onClick={() => { setDraftFilters({}); setFilters({}); setPage(1); setNotice(null) }}>重置</button><label>每页<select value={perPage} onChange={(event) => { setPerPage(Number(event.target.value)); setPage(1) }}>{[20, 50, 100].map((n) => <option key={n} value={n}>{n} 条</option>)}</select></label></form>
      {loading ? <FeedbackState kind="loading" title="正在加载" /> : error ? <FeedbackState kind="error" title="加载失败" description={error} /> : data?.result.length ? table : <FeedbackState kind="empty" title="暂无符合条件的记录" description={section === 'reports' ? '尚未生成／本期无符合消息。' : '可调整筛选条件或刷新查看。'} />}
      <PaginationControls page={page} totalPages={data?.pagination.total_page ?? 0} loading={loading} onPrevious={() => setPage((p) => p - 1)} onNext={() => setPage((p) => p + 1)} />
    </Section>
    <Drawer open={Boolean(draft)} title={draft?.id ? '编辑监控群' : '新增监控群'} description="停用将停止采集新消息和未发提醒，已归档内容仍进入本期日报。重新启用不会恢复已取消的提醒。" closeDisabled={saving} onClose={() => setDraft(null)} footer={<><button type="button" disabled={saving} onClick={() => setDraft(null)}>取消</button><button type="submit" form="mention-chat-form" className={styles.primary} disabled={saving}>{saving ? '保存中…' : '保存'}</button></>}>
      {draft ? <form id="mention-chat-form" className={styles.form} onSubmit={(event) => void saveChat(event)}><label>群 ID<input value={draft.chatId ?? ''} disabled={Boolean(draft.id)} required maxLength={128} onChange={(event) => setDraft({ ...draft, chatId: event.target.value })} /></label><label>群名称<input value={draft.chatName ?? ''} required maxLength={255} onChange={(event) => setDraft({ ...draft, chatName: event.target.value })} /></label><label>二次提醒间隔（分钟）<input type="number" value={draft.followupMinutes ?? ''} placeholder="留空使用服务端默认值" required={Boolean(draft.id)} min={1} step={1} onChange={(event) => setDraft({ ...draft, followupMinutes: event.target.value === '' ? undefined : Number(event.target.value) })} /></label><label className={styles.check}><input type="checkbox" checked={draft.isExternal ?? false} onChange={(event) => setDraft({ ...draft, isExternal: event.target.checked })} />外部群（人工登记，不修改飞书共享权限）</label><label className={styles.check}><input type="checkbox" checked={draft.enabled ?? true} onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })} />启用监控</label>{saveError ? <p role="alert" className={styles.error}>{saveError}</p> : null}</form> : null}
    </Drawer>
    {selected?.kind === 'job' ? <JobDetail job={selected.job} onClose={() => setSelected(null)} onRecipient={(id) => setSelected({ kind: 'recipient', id })} onReport={(id) => setSelected({ kind: 'report', id })} /> : null}
    {selected?.kind === 'recipient' ? <RecipientDetail key={selected.id} client={client} id={selected.id} onClose={() => setSelected(null)} onReport={(id) => setSelected({ kind: 'report', id })} /> : null}
    {selected?.kind === 'report' ? <ReportDetail key={selected.id} client={client} id={selected.id} reload={reload} notice={notice} onRefresh={() => setReload((r) => r + 1)} onClose={() => setSelected(null)} retryButton={retryButton} /> : null}
  </PageCanvas>
}
function identity(name: string, id: string) { return <><strong>{name || id}</strong>{name ? <code className={styles.cellHint}>{id}</code> : null}</> }
function status(value: Job['status'] | Report['status']) { return <StatusTag tone={value === 'sent' ? 'success' : value === 'failed' ? 'danger' : 'neutral'}>{statusLabels[value]}</StatusTag> }
function reminder(job: Job | null) { return job ? <>{status(job.status)}<small className={styles.cellHint}>{time(job.sentAt)}</small></> : '未安排提醒' }
function renderTable(section: Exclude<MentionSection, 'status'>, rows: Row[], actions: { onEdit: (row: Chat) => void; onRecipient: (id: number) => void; onReport: (id: number) => void; onJob: (job: Job) => void; retryButton: (job: Job) => ReactNode; canManage: boolean }) {
  let headers: string[], body: ReactNode
  if (section === 'chats') { headers = ['来源群', '群类型', '监控状态', '提醒间隔', '更新时间', '操作']; body = (rows as Chat[]).map((r) => <tr key={r.id}><td>{identity(r.chatName, r.chatId)}</td><td>{r.isExternal ? '外部群' : '内部群'}</td><td><StatusTag tone={r.enabled ? 'success' : 'neutral'}>{r.enabled ? '启用' : '停用'}</StatusTag></td><td>{r.followupMinutes} 分钟</td><td>{time(r.updatedAt)}</td><td>{actions.canManage ? <button type="button" onClick={() => actions.onEdit(r)}>编辑 / 启停</button> : '—'}</td></tr>) }
  else if (section === 'recipients') { headers = ['来源群 / 收件人', '消息', '本人确认', '首次提醒', '二次提醒', '操作']; body = (rows as Recipient[]).map((r) => <tr key={r.id}><td>{identity(r.chatName, r.chatId)}<br />{identity(r.recipientName, r.recipientOpenId)}</td><td><span className={styles.preview}>{r.contentPreview}</span><small className={styles.cellHint}>{r.senderName} · 消息于 {time(r.messageCreatedAt)}</small><small className={styles.cellHint}>接收于 {time(r.receivedAt)}</small></td><td>{confirmationLabel(r)}<small className={styles.cellHint}>{time(r.confirmedAt)}</small></td><td>{reminder(r.initialReminder)}</td><td>{reminder(r.followupReminder)}</td><td><button type="button" onClick={() => actions.onRecipient(r.id)}>查看全文</button></td></tr>) }
  else if (section === 'reports') { headers = ['来源群 / 收件人', '报告日期', '消息数', '已发送段数', '发送状态', '操作']; body = (rows as Report[]).map((r) => <tr key={r.id}><td>{identity(r.chatName, r.chatId)}<br />{identity(r.recipientName, r.recipientOpenId)}</td><td>{r.reportDate}</td><td>{r.messageCount}</td><td>{r.sentPartCount} / {r.partCount}</td><td>{status(r.status)}<small className={styles.cellHint}>{time(r.sentAt)}</small></td><td><button type="button" onClick={() => actions.onReport(r.id)}>查看固定日报</button></td></tr>) }
  else { headers = ['任务', '来源群 / 收件人', '发送状态', '计划 / 发送时间', '尝试次数', '错误 / 取消原因', '操作']; body = (rows as Job[]).map((r) => <tr key={r.id}><td>{jobLabels[r.jobType]}<small className={styles.cellHint}>#{r.id}{r.partNo ? ` · 第 ${r.partNo} 段` : ''}</small></td><td><code>{r.chatId}</code><small className={styles.cellHint}>{r.recipientOpenId}</small></td><td>{status(r.status)}</td><td>{time(r.scheduledAt)}<small className={styles.cellHint}>{time(r.sentAt)}</small></td><td>累计 {r.attempts}<small className={styles.cellHint}>本轮 {r.cycleAttempts}</small></td><td><span className={styles.preview}>{r.lastErrorMessage || cancelLabels[r.cancelReason] || r.cancelReason || '—'}</span><small className={styles.cellHint}>{r.lastErrorCode}</small></td><td><div className={styles.actions}><button type="button" onClick={() => actions.onJob(r)}>任务详情</button>{r.reportId ? <button type="button" onClick={() => actions.onReport(r.reportId!)}>日报</button> : r.recipientRecordId ? <button type="button" onClick={() => actions.onRecipient(r.recipientRecordId!)}>原文</button> : null}{actions.retryButton(r)}</div></td></tr>) }
  return <DataTable minWidth={section === 'jobs' ? 1150 : 950} scrollLabel={`${sectionTitles[section]}列表`}><thead><tr>{headers.map((header) => <th scope="col" key={header}>{header}</th>)}</tr></thead><tbody>{body}</tbody></DataTable>
}
function MentionStatusPage({ client }: { client: WorkspaceApiClient }) {
  const [reload, setReload] = useState(0)
  const { data, loading, error } = useMentionResource<ModuleStatus>(client, '/status', reload)
  const observations = data?.observations
  const flag = (value: boolean | null | undefined) => value == null ? '未知' : value ? '可用' : '不可用'
  return <PageCanvas><PageHeader eyebrow="FEISHU MENTIONS" title="模块状态" description={mentionNotice} actions={<button type="button" disabled={loading} onClick={() => setReload((r) => r + 1)}>刷新状态</button>} />{loading ? <FeedbackState kind="loading" title="正在读取运行状态" /> : error ? <FeedbackState kind="error" title="状态加载失败" description={error} /> : data ? <>
    <Section title="业务开关" description={data.pauseReason || '配置由服务端统一管理。'}><dl className={styles.facts}>{([['模块', data.enabled], ['个人提醒与确认', data.personalReminderEnabled], ['二次提醒', data.followupEnabled], ['每日报告', data.digestEnabled]] as [string, boolean][]).map(([name, enabled]) => <div key={name} style={{ display: 'contents' }}><dt>{name}</dt><dd>{enabled ? '开启' : '关闭'}{name === '二次提醒' && enabled && !data.personalReminderEnabled ? '（个人提醒与确认已关闭，不会安排二次提醒）' : ''}</dd></div>)}<dt>时区</dt><dd>{data.timezone}</dd></dl></Section>
    <Section title="当前实例接收状态" description="以下连接观测仅代表当前实例；多个实例上线时需结合各实例指标判断。"><dl className={styles.facts}><dt>当前应用</dt><dd><code>{data.appId}</code></dd><dt>接收角色</dt><dd>{data.receiver.enabled ? '开启' : '关闭'}</dd><dt>当前连接</dt><dd>{flag(data.receiver.connected)}</dd><dt>最近事件</dt><dd>{time(data.receiver.lastEventAt)}</dd><dt>最近错误</dt><dd>{time(data.receiver.lastErrorAt)}</dd><dt>数据库 / 队列</dt><dd>{flag(data.dependencies.database)} / {flag(data.dependencies.queue)}</dd></dl></Section>
    <Section title="共享业务积压与日报调度"><dl className={styles.facts}>{data.backlog ? Object.entries(statusLabels).filter(([key]) => key !== 'ready').map(([key, label]) => <div key={key} style={{ display: 'contents' }}><dt>{label}</dt><dd>{data.backlog?.[key] ?? 0}</dd></div>) : <><dt>任务积压</dt><dd>未知</dd></>}<dt>持久化日报调度</dt><dd>{data.digest.scheduleEnabled == null ? '尚未初始化 / 未知' : data.digest.scheduleEnabled ? '开启' : '暂停'}</dd><dt>下期窗口起点</dt><dd>{time(data.digest.nextWindowStartAt)}</dd><dt>下期截止时刻</dt><dd>{time(data.digest.nextCutoffAt)}</dd><dt>最近规划</dt><dd>{time(data.digest.lastPlannedAt)}</dd></dl></Section>
    <Section title="Outbox 发布" description="未发布记录尚未交给队列；成功入队不代表飞书已发送成功。统计包含当前应用各投递代次。"><dl className={styles.facts}><dt>未发布</dt><dd>{observations?.outbox.unpublished ?? '未知'}</dd><dt>发布失败待恢复</dt><dd>{observations?.outbox.publishFailed ?? '未知'}</dd><dt>最早计划发布时间</dt><dd>{time(observations?.outbox.earliestAvailableAt)}</dd></dl></Section>
    <Section title="发送等待与租约" description="到期等待包含等待前一段的日报分段；可在发送任务页检查具体任务。"><dl className={styles.facts}><dt>到期等待</dt><dd>{observations?.delivery.dueWaiting ?? '未知'}</dd><dt>最早等待时间</dt><dd>{time(observations?.delivery.oldestWaitingAt)}</dd><dt>最早发送租约到期</dt><dd>{time(observations?.delivery.earliestLeaseUntil)}</dd><dt>已过期发送租约</dt><dd>{observations?.delivery.expiredLeases ?? '未知'}</dd><dt>卡片更新失败</dt><dd>{observations?.delivery.cardUpdateFailed ?? '未知'}</dd></dl></Section>
    <Section title="归档与本人确认" description="按当前应用统计数据库保留的记录；当前窗口按上方持久化起止时间统计。"><dl className={styles.facts}><dt>启用监控群</dt><dd>{observations?.archive.enabledChats ?? '未知'}</dd><dt>已归档消息</dt><dd>{observations?.archive.messages ?? '未知'}</dd><dt>当前窗口归档消息</dt><dd>{observations?.archive.windowMessages ?? '未知'}</dd><dt>最近归档</dt><dd>{time(observations?.archive.latestReceivedAt)}</dd>{Object.entries(viewLabels).map(([key, label]) => <div key={key} style={{ display: 'contents' }}><dt>{label}</dt><dd>{observations ? observations.confirmation[key] ?? 0 : '未知'}</dd></div>)}</dl></Section>
    <Section title="日报发送汇总"><dl className={styles.facts}>{(['ready', 'sent', 'failed'] as const).map((key) => <div key={key} style={{ display: 'contents' }}><dt>{statusLabels[key]}</dt><dd>{observations ? observations.reports[key] ?? 0 : '未知'}</dd></div>)}<dt>失败分段</dt><dd>{observations?.delivery.digestPartsFailed ?? '未知'}</dd></dl></Section>
  </> : null}</PageCanvas>
}
