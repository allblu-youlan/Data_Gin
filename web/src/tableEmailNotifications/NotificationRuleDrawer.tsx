import { useEffect, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { Drawer } from '../ui'
import { getNotificationColumns, saveNotificationRule } from './api'
import { buildNotificationPayload, isCursorColumn } from './contracts'
import { NotificationTablePicker } from './NotificationTablePicker'
import { NotificationTemplatesEditor } from './NotificationTemplatesEditor'
import type { NotificationColumn, NotificationDatasource, NotificationDraft } from './types'
import shared from '../officeMessage/OfficeMessage.module.css'

type Props = { client: WorkspaceApiClient; draft: NotificationDraft; datasources: NotificationDatasource[]; onChange: (draft: NotificationDraft) => void; onClose: () => void; onSaved: () => void }

export function NotificationRuleDrawer({ client, draft, datasources, onChange, onClose, onSaved }: Props) {
  const [columns, setColumns] = useState<NotificationColumn[]>([])
  const [columnsLoading, setColumnsLoading] = useState(false)
  const [columnsError, setColumnsError] = useState('')
  const [reload, setReload] = useState(0)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    setColumns([])
    setColumnsError('')
    if (!draft.owner || !draft.tableName) { setColumnsLoading(false); return }
    const controller = new AbortController()
    setColumnsLoading(true)
    void getNotificationColumns(client, draft.datasourceId, draft.owner, draft.tableName, controller.signal).then((result) => {
      if (controller.signal.aborted) return
      if (result.ok) setColumns(result.data)
      else setColumnsError(result.error)
      setColumnsLoading(false)
    })
    return () => controller.abort()
  }, [client, draft.datasourceId, draft.owner, draft.tableName, reload])

  async function save() {
    if (saving) return
    let payload: unknown
    try { payload = buildNotificationPayload(draft, columns) } catch (reason) { setError(reason instanceof Error ? reason.message : '提醒配置无效。'); return }
    setSaving(true)
    setError('')
    const result = await saveNotificationRule(client, draft.id, payload)
    setSaving(false)
    if (result.ok) onSaved()
    else setError(result.error)
  }

  return <Drawer open title={draft.id ? '编辑数据邮件提醒' : '新增数据邮件提醒'} description="监听 Oracle 表中的新增记录，按记录字段生成并发送邮件。" size="wide" closeDisabled={saving} onClose={onClose} footer={<><button type="button" disabled={saving} onClick={onClose}>取消</button><button type="submit" form="table-email-rule-form" className={shared.primary} disabled={saving || columnsLoading || !columns.length}>{saving ? '保存中…' : '保存提醒'}</button></>}>
    <form id="table-email-rule-form" className={shared.form} onSubmit={(event) => { event.preventDefault(); void save() }}>
      <label>提醒名称<input required maxLength={100} value={draft.name} disabled={saving} onChange={(event) => onChange({ ...draft, name: event.target.value })} /></label>
      <label>Oracle 数据源<select value={draft.datasourceId} disabled={saving || !datasources.length} onChange={(event) => onChange({ ...draft, datasourceId: Number(event.target.value), owner: '', tableName: '', cursorColumn: '' })}>{!datasources.some((source) => source.id === draft.datasourceId) ? <option value={draft.datasourceId}>数据源 #{draft.datasourceId}（当前不可用）</option> : null}{datasources.map((source) => <option key={source.id} value={source.id}>{source.name}</option>)}</select></label>
      <NotificationTablePicker key={draft.datasourceId} client={client} datasourceId={draft.datasourceId} owner={draft.owner} tableName={draft.tableName} disabled={saving} onSelect={(table) => onChange({ ...draft, owner: table.owner, tableName: table.name, cursorColumn: '' })} />
      {columnsLoading ? <p className={shared.contractNote} role="status">正在读取表字段…</p> : columnsError ? <p className={shared.formError} role="alert">{columnsError} <button type="button" onClick={() => setReload((value) => value + 1)}>重新读取字段</button></p> : null}
      <label>递增游标字段<select required value={draft.cursorColumn} disabled={saving || columnsLoading || !columns.length} onChange={(event) => onChange({ ...draft, cursorColumn: event.target.value })}><option value="">选择非空整数 NUMBER 字段</option>{columns.filter(isCursorColumn).map((column) => <option key={column.name} value={column.name}>{column.name}{column.comment ? ` · ${column.comment}` : ''}</option>)}</select><small>必须是非空、非负、递增的整数 NUMBER 字段，且具有单列主键或唯一约束；仅监控游标值更大的新增记录。</small></label>
      {columns.length > 0 && !columns.some(isCursorColumn) ? <p className={shared.formError} role="alert">该表没有可选的非空 NUMBER 游标字段，请选择其他数据表。</p> : null}
      <NotificationTemplatesEditor key={`${draft.datasourceId}.${draft.owner}.${draft.tableName}`} client={client} draft={draft} columns={columns} disabled={saving || columnsLoading} onChange={onChange} />
      <label>检查间隔（秒）<input required type="number" min={60} max={86400} step={1} value={draft.pollIntervalSeconds} disabled={saving} onChange={(event) => onChange({ ...draft, pollIntervalSeconds: Number(event.target.value) })} /><small>最短 60 秒，最长 86400 秒。系统每分钟调度，到期后可能再等待最多 60 秒开始检查。</small></label>
      <label className={shared.checkbox}><input type="checkbox" checked={draft.enabled} disabled={saving} onChange={(event) => onChange({ ...draft, enabled: event.target.checked })} />启用邮件提醒</label>
      <p className={shared.contractNote}>首次启用、更换监听表或从暂停恢复时，以当时的最大游标值开始监听，不发送历史记录。</p>
      {error ? <p className={shared.formError} role="alert">{error}</p> : null}
    </form>
  </Drawer>
}
