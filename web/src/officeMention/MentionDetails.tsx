import { useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { Drawer, FeedbackState, PaginationControls, StatusTag } from '../ui'
import { useMentionResource } from './api'
import { cancelLabels, confirmationLabel, formatMentionTime as time, jobLabels, reportKindLabel, safeOriginalURL, statusLabels } from './contracts'
import type { Job, Page, Recipient, Report, ReportPart, SnapshotMessage } from './types'
import styles from './OfficeMention.module.css'

export function JobDetails({ job }: { job: Job | null }) {
  if (!job) return <p className={styles.notice}>未安排提醒</p>
  return <dl className={styles.facts}><dt>任务与发送状态</dt><dd>#{job.jobId} · {statusLabels[job.status]}</dd><dt>计划 / 发送时间</dt><dd>{time(job.scheduledAt)} / {time(job.sentAt)}</dd><dt>尝试次数</dt><dd>累计 {job.attempts} 次，本轮 {job.cycleAttempts} 次</dd>{job.lastErrorCode || job.lastErrorMessage ? <><dt>发送错误</dt><dd><code>{job.lastErrorCode}</code><pre className={styles.body}>{job.lastErrorMessage}</pre></dd></> : null}{job.cancelReason ? <><dt>取消原因</dt><dd>{cancelLabels[job.cancelReason] ?? job.cancelReason}</dd></> : null}</dl>
}
export function JobDetail({ job, onClose, onRecipient, onReport }: { job: Job; onClose: () => void; onRecipient: (id: number) => void; onReport: (id: number) => void }) {
  return <Drawer open title={`${jobLabels[job.jobType]} #${job.id}`} description="显示本次列表查询的任务状态；关闭后可刷新列表获取最新状态。" onClose={onClose} size="wide">
    <div className={styles.detail}><dl className={styles.facts}><dt>来源群</dt><dd>{job.chatId}</dd><dt>接收人</dt><dd>{job.recipientOpenId}</dd>{job.partNo ? <><dt>日报分段</dt><dd>第 {job.partNo} 段</dd></> : null}</dl><JobDetails job={job} /><div className={styles.actions}>{job.reportId ? <button type="button" onClick={() => onReport(job.reportId!)}>查看统计详情</button> : null}{job.recipientRecordId ? <button type="button" onClick={() => onRecipient(job.recipientRecordId!)}>查看来源消息</button> : null}</div></div>
  </Drawer>
}
function OriginalLink({ url }: { url: string | null }) {
  const href = safeOriginalURL(url)
  return href ? <a href={href} target="_blank" rel="noopener noreferrer">前往飞书原消息</a> : null
}
export function RecipientDetail({ client, id, onClose, onReport }: { client: WorkspaceApiClient; id: number; onClose: () => void; onReport: (id: number) => void }) {
  const [reload, setReload] = useState(0)
  const { data, error, loading } = useMentionResource<Recipient>(client, `/recipients/${id}`, reload)
  return <Drawer open title={data ? `${data.chatName}｜${data.recipientName || data.recipientOpenId}` : '@ 消息详情'} description="时间按 Asia/Shanghai 展示；原群消息实际已读状态不可获取。" onClose={onClose} size="wide" footer={<button type="button" disabled={loading} onClick={() => setReload((r) => r + 1)}>刷新消息状态</button>}>
    {loading ? <FeedbackState kind="loading" title="正在加载消息详情" /> : error ? <FeedbackState kind="error" title="消息加载失败" description={error} /> : data ? <div className={styles.detail}>
      <dl className={styles.facts}><dt>来源群</dt><dd>{data.chatName}（{data.chatId}）</dd><dt>被 @ 人员</dt><dd>{data.recipientName}（{data.recipientOpenId}）</dd><dt>发送人</dt><dd>{data.senderName}（{data.senderOpenId}）</dd><dt>消息时间</dt><dd>{time(data.messageCreatedAt)}</dd><dt>接收时间</dt><dd>{time(data.receivedAt)}</dd><dt>本人确认</dt><dd>{confirmationLabel(data)}；确认时间：{time(data.confirmedAt)}</dd><dt>原群实际已读</dt><dd>不可获取</dd><dt>消息标识 / 类型</dt><dd>{data.messageId} / {data.messageType}</dd><dt>所属日报</dt><dd>{data.firstReportId ? <button type="button" onClick={() => onReport(data.firstReportId!)}>查看日报 #{data.firstReportId}</button> : '尚未生成／本期无符合消息'}</dd></dl>
      <h3>完整原文</h3><pre className={styles.body}>{data.displayText}</pre><OriginalLink url={data.originalUrl} />
      <h3>首次提醒</h3><JobDetails job={data.initialReminder} /><h3>二次提醒</h3><JobDetails job={data.followupReminder} />
    </div> : null}
  </Drawer>
}
export function ReportDetail({ client, id, reload, notice, onRefresh, onClose, retryButton, resendButton }: { notice?: {text: string; error: boolean} | null; client: WorkspaceApiClient; id: number; reload: number; onRefresh: () => void; onClose: () => void; retryButton: (job: Job) => React.ReactNode; resendButton: (report: Report) => React.ReactNode }) {
  const [page, setPage] = useState(1)
  const [partNo, setPartNo] = useState<number | null>(null)
  const report = useMentionResource<Report>(client, `/reports/${id}`, reload)
  const messages = useMentionResource<Page<SnapshotMessage>>(client, `/reports/${id}/messages?page=${page}&per_page=10`, reload, true)
  const part = useMentionResource<ReportPart>(client, partNo ? `/reports/${id}/parts/${partNo}` : null, reload)
  const data = report.data
  return <Drawer open title={data ? `${data.chatName}｜${data.recipientName || data.recipientOpenId}的${reportKindLabel(data)}` : '报告详情'} onClose={onClose} size="wide" description="显示当前统计快照的消息清单和统计窗口；时间按 Asia/Shanghai 展示。" footer={<><button type="button" disabled={report.loading} onClick={onRefresh}>刷新统计详情</button>{data && !report.loading && !report.error ? resendButton(data) : null}</>}>
    {notice ? <p role={notice.error ? 'alert' : 'status'} className={notice.error ? styles.error : styles.success}>{notice.text}</p> : null}
    {report.loading ? <FeedbackState kind="loading" title="正在加载每日报告" /> : report.error ? <FeedbackState kind="error" title="日报加载失败" description={report.error} /> : data ? <div className={styles.detail}>
      <dl className={styles.facts}><dt>来源群 / 人员</dt><dd>{data.chatName}（{data.chatId}） / {data.recipientName}（{data.recipientOpenId}）</dd><dt>统计类型 / 日期</dt><dd>{reportKindLabel(data)} · {data.reportDate} · {statusLabels[data.status]}</dd><dt>统计窗口</dt><dd>{time(data.windowStart)} ～ {time(data.windowEnd)}</dd><dt>消息数 / 已发送段数</dt><dd>{data.messageCount} 条 / {data.sentPartCount} / {data.partCount} 段</dd><dt>生成 / 发送时间</dt><dd>{time(data.generatedAt)} / {time(data.sentAt)}</dd></dl>
      {data.summary ? <><h3>摘要</h3><pre className={styles.body}>{data.summary}</pre></> : null}
      <h3>发送分段</h3>{(data.parts ?? []).map((job, index, parts) => <div key={job.id} className={styles.entry}><div className={styles.entryHeader}><strong>第 {job.partNo} 段</strong><StatusTag tone={job.status === 'failed' ? 'danger' : job.status === 'sent' ? 'success' : 'neutral'}>{job.status === 'pending' && index > 0 && parts[index - 1].status !== 'sent' ? '等待前一段' : statusLabels[job.status]}</StatusTag><div className={styles.actions}><button type="button" onClick={() => setPartNo(job.partNo)}>查看发送快照</button>{retryButton(job)}</div></div><JobDetails job={job} /></div>)}
      <h3>当前统计消息清单</h3>{messages.loading ? <FeedbackState kind="loading" title="正在加载原文清单" /> : messages.error ? <FeedbackState kind="error" title="原文清单加载失败" description={messages.error} /> : messages.data?.result.length ? messages.data.result.map((message) => <article key={message.recipientRecordId} className={styles.entry}><strong>{message.senderName || message.senderOpenId} · {time(message.messageCreatedAt)}</strong><span className={styles.cellHint}>接收于 {time(message.receivedAt)} · {message.messageId} · 人员记录 #{message.recipientRecordId}</span><pre className={styles.body}>{message.displayText}</pre><OriginalLink url={message.originalUrl} /></article>) : <p>本期无符合消息</p>}
      <PaginationControls page={page} totalPages={messages.data?.pagination.total_page ?? 0} loading={messages.loading} onPrevious={() => setPage((p) => p - 1)} onNext={() => setPage((p) => p + 1)} />
    </div> : null}
    <Drawer open={partNo !== null} title={part.data?.title ?? `第 ${partNo ?? ''} 段发送快照`} onClose={() => setPartNo(null)} size="wide" description="内容来自固定分段与实际业务发送内容快照。">
      {part.loading ? <FeedbackState kind="loading" title="正在加载发送快照" /> : part.error ? <FeedbackState kind="error" title="分段加载失败" description={part.error} /> : part.data ? <div className={styles.detail}><JobDetails job={part.data.job} />{part.data.fragments.map((fragment) => <article key={`${fragment.recipientRecordId}-${fragment.fragmentNo}`} className={styles.entry}><strong>人员记录 #{fragment.recipientRecordId} · 片段 {fragment.fragmentNo}/{fragment.fragmentCount}</strong><pre className={styles.body}>{fragment.text}</pre></article>)}<details><summary>实际发送内容</summary><pre className={styles.body}>{JSON.stringify(part.data.content, null, 2)}</pre></details></div> : null}
    </Drawer>
  </Drawer>
}
