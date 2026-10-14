import type { Job, JobStatus, JobType, MentionSection, Page, Recipient } from './types'
export const mentionNotice = '个人提醒与确认默认关闭；开启后记录本人确认，无法获取原群消息实际已读状态。'
export const sectionTitles: Record<MentionSection, string> = { chats: '监控群', recipients: '@ 消息记录', reports: '每日报告', jobs: '发送任务', status: '模块状态' }
export const viewLabels: Record<Recipient['viewStatus'], string> = { not_required: '无需确认', pending: '待确认查看', confirmed: '已确认查看' }
export const jobLabels: Record<JobType, string> = { initial_reminder: '首次提醒', followup_reminder: '二次提醒', daily_digest: '日报分段', card_update: '卡片更新' }
export const statusLabels: Record<JobStatus | 'ready', string> = { pending: '等待发送', sending: '发送中', sent: '发送成功', retry_wait: '等待重试', failed: '发送失败', cancelled: '已取消', ready: '待发送' }
export const cancelLabels: Record<string, string> = { confirmed: '本人已确认', group_disabled: '监控群已停用', personal_reminder_disabled: '个人提醒与确认已关闭', followup_disabled: '二次提醒已关闭' }
export function confirmationLabel(row: Pick<Recipient, 'viewStatus' | 'confirmationEnabled'>) {
  return viewLabels[row.viewStatus] + (row.viewStatus === 'pending' && !row.confirmationEnabled ? '（确认功能已关闭）' : '')
}
export function canRetryMentionJob(job: Pick<Job, 'status' | 'retryable'>, permissions: readonly string[]) {
  return job.status === 'failed' && job.retryable !== false && permissions.includes('office_mention.read') && permissions.includes('office_mention.retry')
}
export function mentionDigestSettingsRequest(digestTime: string) {
  if (digestTime.length !== 5 || !/^([01]\d|2[0-3]):[0-5]\d$/.test(digestTime)) throw new Error('请填写有效的日报时间，格式为 HH:mm（例如 18:00）。')
  return { path: '/digest-settings', options: { method: 'PUT' as const, body: { digestTime } } }
}
export function safeOriginalURL(value: string | null | undefined) {
  if (!value) return null
  try { const parsed = new URL(value); return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : null } catch { return null }
}
export function formatMentionTime(value: string | null | undefined) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
}
export function parseMentionData<T>(payload: unknown): T {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new Error('响应格式无效')
  const root = payload as Record<string, unknown>
  if (typeof root.code === 'number' && root.code !== 0) throw new Error('请求未成功')
  const data = 'data' in root ? root.data : root
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('响应格式无效')
  return data as T
}
export function parseMentionPage<T>(payload: unknown): Page<T> {
  const page = parseMentionData<Page<T>>(payload)
  if (!Array.isArray(page.result) || !page.pagination || !Number.isInteger(page.pagination.page) || page.pagination.page < 1 || !Number.isInteger(page.pagination.total_count)) throw new Error('分页响应无效')
  return page
}
export function mentionQuery(filters: Record<string, string>, page: number, perPage = 20) {
  const query = new URLSearchParams({ page: String(page), per_page: String(perPage) })
  for (const [key, value] of Object.entries(filters)) {
    if (!value.trim()) continue
    query.set(key, (key === 'receivedFrom' || key === 'receivedTo') ? `${value.length === 16 ? value + ':00' : value}+08:00` : value.trim())
  }
  return query.toString()
}
