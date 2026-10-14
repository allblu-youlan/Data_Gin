import { useRef, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { runSingleFlight } from '../singleFlight'
import { Dialog } from '../ui'
import { mentionRequest } from './api'
import { mentionResendReportRequest } from './contracts'
import type { Report } from './types'
import styles from './OfficeMention.module.css'

export function ReportResendDialog({ client, report, onClose, onSaved }: { client: WorkspaceApiClient; report: Report; onClose: () => void; onSaved: () => void }) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inFlight = useRef(false)

  async function resend() {
    await runSingleFlight(inFlight, async () => {
      setSaving(true)
      setError('')
      try {
        const request = mentionResendReportRequest(report.id)
        await mentionRequest<Report>(client, request.path, request.options)
        onSaved()
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : '重发失败，请稍后重试。')
      } finally {
        setSaving(false)
      }
    })
  }

  return <Dialog open title="确认重发日报" role="alertdialog" closeDisabled={saving} onClose={onClose} footer={<><button type="button" disabled={saving} onClick={onClose}>取消</button><button type="button" className={styles.primary} disabled={saving} onClick={() => void resend()}>{saving ? '重新排队中…' : '确认重发'}</button></>}>
    <div className={styles.detail} aria-busy={saving}>
      <dl className={styles.facts}><dt>来源群</dt><dd>{report.chatName || report.chatId}</dd><dt>收件人</dt><dd>{report.recipientName || report.recipientOpenId}</dd><dt>报告日期</dt><dd>{report.reportDate}</dd><dt>原统计消息</dt><dd>{report.messageCount} 条</dd></dl>
      <p>将按原统计内容重新发送给这位收件人，并使用最新卡片样式。已成功发送过的日报也会再次收到。</p>
      {error ? <p role="alert" className={styles.error}>{error}</p> : null}
    </div>
  </Dialog>
}
