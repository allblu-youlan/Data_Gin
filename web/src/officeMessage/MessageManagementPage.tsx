import { formatShanghaiDateTime } from '../dateTime.js'
import { useEffect, useState } from 'react'
import { FileSpreadsheet, Pencil, Plus, Trash2 } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { DataTable, Dialog, FeedbackState, PageCanvas, PageHeader, Section, StatusTag } from '../ui'
import { deleteOfficeMessage, getOfficeMessages, saveOfficeMessage } from './api'
import { buildOfficeMessagePayload, emptyOfficeMessageDraft, officeMessageDraftFrom, officeSourceLabels } from './contracts'
import { MessageEditorDrawer } from './MessageEditorDrawer'
import type { OfficeMessage, OfficeMessageDraft } from './types'
import styles from './OfficeMessage.module.css'

type Props = { client: WorkspaceApiClient; permissions: string[] }
type Notice = { text: string; error: boolean }

export function MessageManagementPage({ client, permissions }: Props) {
  const [messages, setMessages] = useState<OfficeMessage[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [reload, setReload] = useState(0)
  const [draft, setDraft] = useState<OfficeMessageDraft | null>(null)
  const [draftError, setDraftError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<OfficeMessage | null>(null)
  const [notice, setNotice] = useState<Notice | null>(null)
  const canManage = permissions.includes('office_message.manage')

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    void getOfficeMessages(client, controller.signal).then((result) => {
      if (controller.signal.aborted) return
      if (result.ok) {
        setMessages(result.data)
        setNotice(null)
      } else setNotice({ text: result.error, error: true })
      setLoading(false)
    })
    return () => controller.abort()
  }, [client, reload])

  function showNotice(text: string, error = false) { setNotice({ text, error }) }
  async function save() {
    if (!draft || saving) return
    let payload: unknown
    try { payload = buildOfficeMessagePayload(draft) } catch (error) { setDraftError(error instanceof Error ? error.message : '消息配置无效。'); return }
    setSaving(true)
    setDraftError('')
    const result = await saveOfficeMessage(client, draft, payload)
    setSaving(false)
    if (!result.ok) return setDraftError(result.error)
    setDraft(null)
    showNotice('办公消息已保存。')
    setReload((value) => value + 1)
  }
  async function remove() {
    if (!pendingDelete || saving) return
    setSaving(true)
    const result = await deleteOfficeMessage(client, pendingDelete.id, pendingDelete.lockVersion)
    setSaving(false)
    if (!result.ok) return showNotice(result.error, true)
    setPendingDelete(null)
    showNotice('办公消息已删除。')
    setReload((value) => value + 1)
  }

  return <PageCanvas>
    <PageHeader eyebrow="OFFICE MESSAGING" title="消息管理" description="维护自编辑文本、Oracle 存储过程结果和带形参 SELECT 结果三类消息来源。" actions={canManage ? <button type="button" className={styles.primary} onClick={() => { setDraftError(''); setDraft(emptyOfficeMessageDraft()) }}><Plus aria-hidden="true" />新增消息</button> : null} />
    {notice ? <p className={notice.error ? styles.errorNotice : styles.notice} role={notice.error ? 'alert' : 'status'}>{notice.text}</p> : null}
    <Section title="消息配置" description={`共 ${messages.length} 条；Excel 来源支持列名对照，SELECT 来源支持 yyyyMMdd 等日期输入格式。`} flush>
      {loading && messages.length === 0 ? <FeedbackState kind="loading" title="正在加载办公消息" /> : messages.length === 0 ? <FeedbackState kind="empty" title="暂无办公消息" description={canManage ? '可新增第一条办公消息。' : '当前没有可查看的消息。'} /> : <DataTable containerClassName={styles.table} minWidth={900} scrollLabel="办公消息列表"><thead><tr><th scope="col">消息</th><th scope="col">来源</th><th scope="col">导出列 / 参数</th><th scope="col">状态</th><th scope="col">更新时间</th>{canManage ? <th scope="col">操作</th> : null}</tr></thead><tbody>{messages.map((message) => <tr key={message.id}><td><span className={styles.identity}><FileSpreadsheet aria-hidden="true" /><span><strong>{message.name}</strong><code>#{message.id}</code></span></span></td><td><StatusTag tone={message.sourceType === 'EDITED' ? 'neutral' : 'info'}>{officeSourceLabels[message.sourceType]}</StatusTag>{message.sourceType === 'ORACLE_QUERY' ? <small className={styles.cellHint}>{message.selectSql.slice(0, 80)}</small> : null}</td><td>{message.sourceType === 'EDITED' ? '—' : `${message.columnMapping.length} 列`}{message.parameters.length ? <small className={styles.cellHint}>{message.parameters.map((item) => `${item.label}${item.format ? `(${item.format})` : ''}`).join('、')}</small> : null}</td><td><StatusTag tone={message.enabled ? 'success' : 'neutral'}>{message.enabled ? '启用' : '停用'}</StatusTag></td><td>{formatTime(message.updatedAt)}</td>{canManage ? <td><div className={styles.actions}><button type="button" onClick={() => { setDraftError(''); setDraft(officeMessageDraftFrom(message)) }}><Pencil aria-hidden="true" />编辑</button><button type="button" onClick={() => setPendingDelete(message)}><Trash2 aria-hidden="true" />删除</button></div></td> : null}</tr>)}</tbody></DataTable>}
    </Section>
    <MessageEditorDrawer client={client} draft={draft} saving={saving} error={draftError} onChange={setDraft} onClose={() => { if (!saving) setDraft(null) }} onSave={() => void save()} onNotice={showNotice} />
    <Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除办公消息" description="已被推送配置引用的消息不能删除。" closeDisabled={saving} onClose={() => setPendingDelete(null)} footer={<><button type="button" disabled={saving} onClick={() => setPendingDelete(null)}>取消</button><button type="button" className={styles.danger} disabled={saving} onClick={() => void remove()}>{saving ? '删除中…' : '确认删除'}</button></>}><p className={styles.dialogCopy}>确认删除“{pendingDelete?.name}”？该操作不可撤销。</p></Dialog>
  </PageCanvas>
}
function formatTime(value: string) { return formatShanghaiDateTime(value, '—') }
