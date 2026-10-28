import { useEffect, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { Drawer, FeedbackState, StatusTag, type StatusTagTone } from '../ui'
import { getNotificationDeliveries, retryNotificationDelivery } from './api'
import { formatNotificationTime } from './contracts'
import type { NotificationDelivery, NotificationRule } from './types'
import shared from '../officeMessage/OfficeMessage.module.css'
import styles from './TableEmailNotifications.module.css'

type Props = { client: WorkspaceApiClient; rule: NotificationRule; canRetry: boolean; onClose: () => void }
const labels: Record<NotificationDelivery['status'], string> = { queued: '等待发送', retrying: '等待重试', sent: '已发送', failed: '发送失败', cancelled: '已取消' }
const tones: Record<NotificationDelivery['status'], StatusTagTone> = { queued: 'info', retrying: 'warning', sent: 'success', failed: 'danger', cancelled: 'neutral' }

export function NotificationDeliveriesDrawer({ client, rule, canRetry, onClose }: Props) {
  const [deliveries, setDeliveries] = useState<NotificationDelivery[]>([])
  const [loading, setLoading] = useState(true)
  const [retrying, setRetrying] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [reload, setReload] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    void getNotificationDeliveries(client, rule.id, controller.signal).then((result) => {
      if (controller.signal.aborted) return
      if (result.ok) setDeliveries(result.data)
      else setError(result.error)
      setLoading(false)
    })
    return () => controller.abort()
  }, [client, rule.id, reload])

  async function retry(id: number) {
    if (retrying) return
    setRetrying(id)
    setNotice('')
    const result = await retryNotificationDelivery(client, id)
    setRetrying(null)
    if (!result.ok) setError(result.error)
    else { setNotice('已安排重新发送，请稍后刷新查看结果。'); setReload((value) => value + 1) }
  }

  return <Drawer open title={`发送记录 · ${rule.name}`} description="展示最近 100 条邮件发送记录；失败记录可手动重试。" size="wide" closeDisabled={Boolean(retrying)} onClose={onClose} footer={<button type="button" disabled={loading || Boolean(retrying)} onClick={() => setReload((value) => value + 1)}>{loading ? '刷新中…' : '刷新记录'}</button>}>
    {notice ? <p className={shared.notice} role="status">{notice}</p> : null}
    {error ? <p className={shared.formError} role="alert">{error}</p> : null}
    {loading && !deliveries.length ? <FeedbackState kind="loading" title="正在读取邮件发送记录" /> : !deliveries.length && !error ? <FeedbackState kind="empty" title="暂无邮件发送记录" description="启用后新增的记录会在检查时生成邮件。" /> : <div className={styles.deliveryList}>{deliveries.map((delivery) => <article key={delivery.id} className={styles.deliveryItem}>
      <div className={styles.deliveryHeading}><StatusTag tone={tones[delivery.status]}>{labels[delivery.status]}</StatusTag>{canRetry && delivery.status === 'failed' ? <button type="button" disabled={Boolean(retrying)} onClick={() => void retry(delivery.id)}>{retrying === delivery.id ? '提交中…' : '重试发送'}</button> : null}</div>
      <div className={styles.deliveryMeta}><span>记录 #{delivery.id}</span><span>{rule.cursorColumn} = {delivery.sourceValue}</span><span>已尝试 {delivery.attempts} 次</span></div>
      <h3>{delivery.subject}</h3><p className={styles.cellText}>收件人：{delivery.recipients.join(', ')}</p>
      <div className={styles.deliveryMeta}><span>创建：{formatNotificationTime(delivery.createdAt)}</span>{delivery.sentAt ? <span>发送：{formatNotificationTime(delivery.sentAt)}</span> : null}</div>
      {delivery.lastError ? <p className={shared.formError}>{delivery.lastError}</p> : null}
      <details><summary>查看邮件正文</summary><p className={styles.bodyText}>{delivery.body}</p></details>
    </article>)}</div>}
  </Drawer>
}
