import { useEffect, useState } from 'react'
import { History, Mail, Pause, Pencil, Play, Plus, RefreshCw, Trash2 } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { DataTable, Dialog, FeedbackState, PageCanvas, PageHeader, Section, StatusTag } from '../ui'
import { deleteNotificationRule, getNotificationDatasources, getNotificationRules, saveNotificationRule } from './api'
import { emptyNotificationDraft, formatNotificationTime, notificationEnabledPayload } from './contracts'
import { NotificationDeliveriesDrawer } from './NotificationDeliveriesDrawer'
import { NotificationRuleDrawer } from './NotificationRuleDrawer'
import type { NotificationDatasource, NotificationDraft, NotificationRule } from './types'
import shared from '../officeMessage/OfficeMessage.module.css'
import styles from './TableEmailNotifications.module.css'

type Props = { client: WorkspaceApiClient; permissions: string[] }

export function TableEmailNotificationsPage({ client, permissions }: Props) {
  const [rules, setRules] = useState<NotificationRule[]>([])
  const [datasources, setDatasources] = useState<NotificationDatasource[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [reload, setReload] = useState(0)
  const [draft, setDraft] = useState<NotificationDraft | null>(null)
  const [history, setHistory] = useState<NotificationRule | null>(null)
  const [pendingDelete, setPendingDelete] = useState<NotificationRule | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const [toggling, setToggling] = useState<number | null>(null)
  const canManage = permissions.includes('office_message.manage')
  const canRetry = permissions.includes('office_message.push')

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    void Promise.all([getNotificationRules(client, controller.signal), getNotificationDatasources(client, controller.signal)]).then(([ruleResult, datasourceResult]) => {
      if (controller.signal.aborted) return
      if (ruleResult.ok) setRules(ruleResult.data)
      if (datasourceResult.ok) setDatasources(datasourceResult.data)
      const messages = [ruleResult.ok ? '' : ruleResult.error, datasourceResult.ok ? '' : datasourceResult.error].filter(Boolean)
      setError(messages.join(' '))
      setLoading(false)
    })
    return () => controller.abort()
  }, [client, reload])

  async function remove() {
    if (!pendingDelete || deleting) return
    setDeleting(true)
    setDeleteError('')
    const result = await deleteNotificationRule(client, pendingDelete.id, pendingDelete.lockVersion)
    setDeleting(false)
    if (!result.ok) { setDeleteError(result.error); return }
    setPendingDelete(null)
    setNotice('邮件提醒已删除。')
    setReload((value) => value + 1)
  }

  async function toggle(rule: NotificationRule) {
    if (toggling) return
    setToggling(rule.id)
    setError('')
    const result = await saveNotificationRule(client, rule.id, notificationEnabledPayload(rule, !rule.enabled))
    setToggling(null)
    if (!result.ok) { setError(result.error); return }
    setNotice(rule.enabled ? '邮件提醒已暂停，尚未发送的邮件将取消。' : '邮件提醒已启用，从当前最大游标值开始监听。')
    setReload((value) => value + 1)
  }

  return <PageCanvas>
    <PageHeader eyebrow="DATA EMAIL NOTIFICATIONS" title="数据邮件提醒" description="选择 Oracle 表，在新增记录出现时，用字段生成收件人、标题与正文。" actions={<div className={styles.toolbar}><button type="button" disabled={loading} onClick={() => setReload((value) => value + 1)}><RefreshCw aria-hidden="true" />刷新</button>{canManage ? <button type="button" className={shared.primary} disabled={!datasources.length} onClick={() => setDraft({ ...emptyNotificationDraft(), datasourceId: datasources[0].id })}><Plus aria-hidden="true" />新增提醒</button> : null}</div>} />
    {notice ? <p className={shared.notice} role="status">{notice}</p> : null}
    {error ? <FeedbackState kind="error" title="邮件提醒加载失败" description={error} /> : null}
    <Section title="邮件提醒配置" description={`共 ${rules.length} 条。首次启用和从暂停恢复时从当前数据开始，不补发历史记录。`} flush>
      {loading && !rules.length ? <FeedbackState kind="loading" title="正在加载邮件提醒" /> : !rules.length && !error ? <FeedbackState kind="empty" title="暂无数据邮件提醒" description={canManage ? '新增提醒，选择监听表并配置要发送的邮件。' : '当前没有可查看的邮件提醒。'} /> : rules.length ? <DataTable minWidth={1060} scrollLabel="数据邮件提醒列表"><thead><tr><th scope="col">提醒 / 数据表</th><th scope="col">收件人</th><th scope="col">运行状态</th><th scope="col">最近检查</th><th scope="col">操作</th></tr></thead><tbody>{rules.map((rule) => <tr key={rule.id}>
        <td><span className={shared.identity}><Mail aria-hidden="true" /><span><strong>{rule.name}</strong><code>{rule.owner}.{rule.tableName}</code><small className={shared.cellHint}>{datasources.find((source) => source.id === rule.datasourceId)?.name || `数据源 #${rule.datasourceId}`}</small></span></span></td>
        <td><span className={styles.cellText}>{rule.recipientsTemplate}</span><small className={shared.cellHint}>{rule.subjectTemplate}</small></td>
        <td><StatusTag tone={!rule.enabled ? 'neutral' : rule.lastError ? 'danger' : 'success'}>{!rule.enabled ? '已暂停' : rule.lastError ? '检查异常' : '监听中'}</StatusTag><small className={shared.cellHint}>每 {rule.pollIntervalSeconds} 秒检查</small>{rule.lastError ? <small className={styles.statusError}>{rule.lastError}</small> : null}</td>
        <td>{formatNotificationTime(rule.lastCheckedAt)}<small className={shared.cellHint}>{rule.cursorColumn}：{rule.cursorValue || '尚未初始化'}</small></td>
        <td><div className={shared.actions}><button type="button" onClick={() => setHistory(rule)}><History aria-hidden="true" />发送记录</button>{canManage ? <><button type="button" disabled={Boolean(toggling)} onClick={() => void toggle(rule)}>{rule.enabled ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}{toggling === rule.id ? '处理中…' : rule.enabled ? '暂停' : '启用'}</button><button type="button" disabled={Boolean(toggling)} onClick={() => setDraft({ ...rule })}><Pencil aria-hidden="true" />编辑</button><button type="button" disabled={Boolean(toggling)} onClick={() => { setDeleteError(''); setPendingDelete(rule) }}><Trash2 aria-hidden="true" />删除</button></> : null}</div></td>
      </tr>)}</tbody></DataTable> : null}
    </Section>
    {draft ? <NotificationRuleDrawer key={draft.id ?? 'new'} client={client} draft={draft} datasources={datasources} onChange={setDraft} onClose={() => setDraft(null)} onSaved={() => { setDraft(null); setNotice('邮件提醒已保存。'); setReload((value) => value + 1) }} /> : null}
    {history ? <NotificationDeliveriesDrawer client={client} rule={history} canRetry={canRetry} onClose={() => setHistory(null)} /> : null}
    <Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除邮件提醒" description="删除后停止监听该提醒的数据表。" closeDisabled={deleting} onClose={() => setPendingDelete(null)} footer={<><button type="button" disabled={deleting} onClick={() => setPendingDelete(null)}>取消</button><button type="button" className={shared.danger} disabled={deleting} onClick={() => void remove()}>{deleting ? '删除中…' : '确认删除'}</button></>}><p>确认删除“{pendingDelete?.name}”？</p>{deleteError ? <p className={shared.formError} role="alert">{deleteError}</p> : null}</Dialog>
  </PageCanvas>
}
