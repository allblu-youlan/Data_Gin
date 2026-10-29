import { useLayoutEffect, useRef, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { previewNotification } from './api'
import { buildNotificationPayload, parseSampleRow } from './contracts'
import type { NotificationColumn, NotificationDraft, NotificationPreview, TemplateTarget } from './types'
import shared from '../officeMessage/OfficeMessage.module.css'
import styles from './TableEmailNotifications.module.css'

type Props = { client: WorkspaceApiClient; draft: NotificationDraft; columns: NotificationColumn[]; disabled: boolean; onChange: (draft: NotificationDraft) => void }
const targetLabels: Record<TemplateTarget, string> = { recipientsTemplate: '收件人', subjectTemplate: '邮件标题', bodyTemplate: '邮件正文' }

export function NotificationTemplatesEditor({ client, draft, columns, disabled, onChange }: Props) {
  const [target, setTarget] = useState<TemplateTarget>('bodyTemplate')
  const [sample, setSample] = useState('{}')
  const [preview, setPreview] = useState<NotificationPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [error, setError] = useState('')
  const recipientsRef = useRef<HTMLInputElement>(null)
  const subjectRef = useRef<HTMLInputElement>(null)
  const bodyRef = useRef<HTMLTextAreaElement>(null)
  const pendingSelection = useRef<{ target: TemplateTarget; position: number } | null>(null)
  const fields = { recipientsTemplate: recipientsRef, subjectTemplate: subjectRef, bodyTemplate: bodyRef }
  const busy = disabled || previewing

  useLayoutEffect(() => {
    const selection = pendingSelection.current
    if (!selection) return
    pendingSelection.current = null
    const input = selection.target === 'recipientsTemplate' ? recipientsRef.current : selection.target === 'subjectTemplate' ? subjectRef.current : bodyRef.current
    input?.focus()
    input?.setSelectionRange(selection.position, selection.position)
  }, [draft.recipientsTemplate, draft.subjectTemplate, draft.bodyTemplate])

  function change(key: TemplateTarget, value: string) { setPreview(null); setError(''); onChange({ ...draft, [key]: value }) }
  function insert(name: string) {
    const input = fields[target].current
    const value = draft[target]
    const start = input?.selectionStart ?? value.length
    const end = input?.selectionEnd ?? value.length
    const token = `{{${name}}}`
    pendingSelection.current = { target, position: start + token.length }
    change(target, value.slice(0, start) + token + value.slice(end))
  }
  async function showPreview() {
    let payload: unknown
    try { payload = { ...buildNotificationPayload(draft, columns), sample: parseSampleRow(sample) } } catch (reason) { setError(reason instanceof Error ? reason.message : '示例配置无效。'); return }
    setPreviewing(true)
    setError('')
    setPreview(null)
    const result = await previewNotification(client, payload)
    setPreviewing(false)
    if (result.ok) setPreview(result.data)
    else setError(result.error)
  }

  return <>
    <label>收件人邮箱或字段<input ref={recipientsRef} value={draft.recipientsTemplate} required maxLength={2000} disabled={busy} placeholder="ops@example.com, {{EMAIL}}" onFocus={() => setTarget('recipientsTemplate')} onChange={(event) => change('recipientsTemplate', event.target.value)} /><small>可填写固定邮箱、邮箱字段，或混合填写；多个收件人用英文逗号分隔。</small></label>
    <label>邮件标题<input ref={subjectRef} value={draft.subjectTemplate} required maxLength={1000} disabled={busy} placeholder="新订单 {{ORDER_NO}}" onFocus={() => setTarget('subjectTemplate')} onChange={(event) => change('subjectTemplate', event.target.value)} /></label>
    <label>邮件正文<textarea ref={bodyRef} value={draft.bodyTemplate} required rows={6} disabled={busy} placeholder="订单编号：{{ORDER_NO}}" onFocus={() => setTarget('bodyTemplate')} onChange={(event) => change('bodyTemplate', event.target.value)} /><small>正文使用纯文本，保留换行。字段名不区分大小写。</small></label>
    <section className={shared.editorBlock} aria-label="插入触发记录字段">
      <div className={styles.fieldHeading}><h3>触发记录字段</h3><label>插入位置<select value={target} disabled={busy} onChange={(event) => setTarget(event.target.value as TemplateTarget)}>{Object.entries(targetLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label></div>
      <p className={shared.contractNote}>点击字段插入到{targetLabels[target]}光标处。发送时会替换为新增记录中的实际值。</p>
      {columns.length ? <div className={styles.fieldButtons}>{columns.map((column) => <button key={column.name} type="button" disabled={busy} title={`${column.comment || column.name} · ${column.dataType}`} onClick={() => insert(column.name)}><code>{`{{${column.name}}}`}</code>{column.comment ? <small>{column.comment}</small> : null}</button>)}</div> : <p className={shared.inlineEmpty}>选择数据表后显示可用字段。</p>}
    </section>
    <details className={styles.previewPanel}>
      <summary>预览邮件</summary>
      <div className={styles.previewContent}>
        <p className={shared.contractNote}>填写一条 JSON 示例记录，检查字段替换后的收件人、标题和正文。预览不会发送邮件。</p>
        <label>JSON 示例行<textarea rows={5} className={shared.mono} spellCheck={false} value={sample} disabled={busy} onChange={(event) => { setSample(event.target.value); setPreview(null) }} /></label>
        <div className={shared.actions}>
          <button type="button" disabled={busy || !columns.length} onClick={() => { setSample(JSON.stringify(Object.fromEntries(columns.map((column) => [column.name, /EMAIL|MAIL/i.test(column.name) ? 'recipient@example.com' : column.dataType.toUpperCase().startsWith('NUMBER') ? 1 : '示例值'])), null, 2)); setPreview(null) }}>填入字段示例</button>
          <button type="button" disabled={busy || !columns.length} onClick={() => void showPreview()}>{previewing ? '预览中…' : '生成预览'}</button>
        </div>
        {error ? <p className={shared.formError} role="alert">{error}</p> : null}
        {preview ? <div className={styles.previewResult} role="status"><dl><dt>收件人</dt><dd>{preview.recipients.join(', ')}</dd><dt>邮件标题</dt><dd>{preview.subject}</dd><dt>邮件正文</dt><dd className={styles.bodyText}>{preview.body}</dd></dl></div> : null}
      </div>
    </details>
  </>
}
