import { useRef, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { runSingleFlight } from '../singleFlight'
import { Dialog } from '../ui'
import { mentionRequest } from './api'
import { mentionResendReportRequest } from './contracts'
import type { Report } from './types'
import styles from './OfficeMention.module.css'

export function ReportResendDialog({ client, report, onClose, onSaved }: { client: WorkspaceApiClient; report: Report; onClose: () => void; onSaved: (report: Report) => void }) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inFlight = useRef(false)

  async function resend() {
    await runSingleFlight(inFlight, async () => {
      setSaving(true)
      setError('')
      try {
        const request = mentionResendReportRequest(report.id)
        const result = await mentionRequest<Report>(client, request.path, request.options)
        onSaved(result)
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : '重新统计失败，请稍后重试。')
      } finally {
        setSaving(false)
      }
    })
  }

  return <Dialog open title="重新统计并发送" role="alertdialog" closeDisabled={saving} onClose={onClose} footer={<><button type="button" disabled={saving} onClick={onClose}>取消</button><button type="button" className={styles.primary} disabled={saving} onClick={() => void resend()}>{saving ? '正在重新统计…' : '确认统计并发送'}</button></>}>
    <div className={styles.detail} aria-busy={saving}>
      <dl className={styles.facts}><dt>来源群</dt><dd>{report.chatName || report.chatId}</dd><dt>收件人</dt><dd>{report.recipientName || report.recipientOpenId}</dd><dt>本次统计时间</dt><dd>今天 00:00 至点击“确认统计并发送”的时刻（上海时间）</dd></dl>
      <p>重新查询该群在上述时段内已采集、且 @ 这位收件人的消息，生成独立的手动统计并发送给收件人。</p>
      <p className={styles.notice}>正式定时日报及其统计窗口保持不变。已发送过的消息也会纳入本次重新统计。</p>
      {error ? <p role="alert" className={styles.error}>{error}</p> : null}
    </div>
  </Dialog>
}
