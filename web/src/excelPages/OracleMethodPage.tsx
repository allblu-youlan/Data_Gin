import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ListChecks, Play, Plus, Save, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Drawer, FeedbackState } from '../ui'
import type { ClientResponse } from '../api/client'
import { TextField } from './OperationField'
import { OracleImportEditor } from './OracleImportEditor'
import { ExcelFileSelection } from './ExcelFileSelection'
import { OracleProcedureEditor } from './OracleProcedureEditor'
import { WorkflowRunPanel } from './WorkflowRunPanel'
import { ExcelEditor, ExcelEditorSection, ExcelWorkspace, ExcelWorkspacePage } from './ExcelWorkspace'
import { useExcelJobs } from './useExcelJobs'
import { useExcelUploads, type ExcelPageClient } from './useExcelUploads'
import { readList, readObject } from './excelPageSupport'
import { emptyImportConfig, emptyProcedureConfig, normalizeOracleImportConfig, operationLabels, operationPaths, type OracleMethodConfig, type OracleMethodOperation, type OperationScheme } from './excelOperations'
import styles from './ExcelOperations.module.css'

type Props = { operation: OracleMethodOperation; client: ExcelPageClient; token: string; permissions: string[]; refreshVersion: number; setLoading: (value: boolean) => void; setResult: (value: ClientResponse | null) => void; onNavigateToJobs: () => void }
function normalizeMethodConfig(config: OracleMethodConfig): OracleMethodConfig { return config.operation === 'oracle_import' ? normalizeOracleImportConfig(config) : config }
export function OracleMethodPage({ operation, client, token, permissions, refreshVersion, setLoading, setResult, onNavigateToJobs }: Props) {
 const [schemes, setSchemes] = useState<OperationScheme<OracleMethodConfig>[]>([])
 const [selected, setSelected] = useState(0)
 const [name, setName] = useState('')
 const [query, setQuery] = useState('')
 const [libraryOpen, setLibraryOpen] = useState(false)
 const [config, setConfig] = useState<OracleMethodConfig>(() => operation === 'oracle_import' ? emptyImportConfig() : emptyProcedureConfig())
 const [files, setFiles] = useState<File[]>([])
 const [busy, setBusy] = useState(false)
 const [reading, setReading] = useState(true)
 const [error, setError] = useState('')
 const [message, setMessage] = useState('')
 const canManage = permissions.includes('excel.manage')
 const canExecute = permissions.includes('excel.execute')
 const uploads = useExcelUploads(client)
 const jobs = useExcelJobs({ section: 'schemes', client, token, refreshVersion, setLoading, setResult })
 const load = useCallback(async (signal?: AbortSignal) => {
  setReading(true)
  const response = await client(operationPaths[operation], { method: 'GET', signal, showResult: false, silentLoading: true })
  if (!signal?.aborted) { if (response.ok) { setSchemes(readList<OperationScheme<OracleMethodConfig>>(response, 'schemes').map(item => ({ ...item, config: normalizeMethodConfig(item.config) }))); setError('') } else setError(response.error?.message || '方法读取失败'); setReading(false) }
 }, [client, operation])
 useEffect(() => { const request = new AbortController(); void load(request.signal); return () => request.abort() }, [load, refreshVersion])
 const running = Boolean(jobs.job && ['pending', 'running'].includes(jobs.job.status))
 const saved = schemes.find(item => item.id === selected)
 const unchanged = saved?.name === name && JSON.stringify(saved.config) === JSON.stringify(config)
 function select(item: OperationScheme<OracleMethodConfig>) { setSelected(item.id); setName(item.name); setConfig(normalizeMethodConfig(structuredClone(item.config))); setError(''); setMessage('') }
 function reset() { setSelected(0); setName(''); setConfig(operation === 'oracle_import' ? emptyImportConfig() : emptyProcedureConfig()); setError(''); setMessage('') }
 async function save(event: FormEvent) {
  event.preventDefault()
  const existing = schemes.find(item => item.name === name.trim())
  if (existing && !window.confirm(`覆盖已保存的方法「${existing.name}」？`)) return
  setBusy(true); setError(''); setMessage('')
  try {
   const response = await client(operationPaths[operation], { method: 'POST', body: { name, config: normalizeMethodConfig(config) }, retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true, timeoutMs: 60_000 })
   if (!response.ok) throw new Error(response.error?.message || '保存方法失败')
   const item = readObject<OperationScheme<OracleMethodConfig>>(response, 'scheme')
   if (!item) throw new Error('服务未返回保存后的方法')
   await load(); select(item); setMessage('方法已保存，可单独执行或加入复杂操作台。')
  } catch (cause) { setError(cause instanceof Error ? cause.message : '保存方法失败') } finally { setBusy(false) }
 }
 async function remove(item: OperationScheme) {
  if (!window.confirm(`删除方法「${item.name}」？引用此方法的组合操作需要重新配置。`)) return
  setBusy(true); setError('')
  try {
   const response = await client(`${operationPaths[operation]}/${item.id}`, { method: 'DELETE', retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true })
   if (!response.ok) throw new Error(response.error?.message || '删除方法失败')
   await load(); if (selected === item.id) reset()
  } catch (cause) { setError(cause instanceof Error ? cause.message : '删除方法失败') } finally { setBusy(false) }
 }
 async function execute() {
  if (!saved || !unchanged || running) return
  if (operation === 'oracle_import' && (!files.length || files.some(file => file.size === 0))) { setError('请先选择有效的待导入 .xlsx 文件。'); return }
  setBusy(true); setError(''); setMessage('')
  try {
   const uploadIds = operation === 'oracle_import' ? await uploads.ensureExcelUploads('import', files) : []
   const response = await client('/v1/excel-match-jobs/workflow-runs', { method: 'POST', body: { config: { operation: 'excel_workflow', steps: [{ operation, schemeId: selected, ...(operation === 'oracle_import' ? { source: 'upload' } : {}) }] }, uploadIds }, retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true, timeoutMs: 120_000 })
   if (!response.ok) throw new Error(response.error?.message || '创建任务失败')
   jobs.registerCreatedJob(response, false)
  } catch (cause) { setError(cause instanceof Error ? cause.message : '创建任务失败') } finally { setBusy(false) }
 }
 const visible = schemes.filter(item => item.name.toLowerCase().includes(query.toLowerCase()))
 return <ExcelWorkspacePage title={operationLabels[operation]} description={operation === 'oracle_import' ? '配置 Excel 与 Oracle 字段映射，保存为可重复调用的方法。' : '配置过程参数和成功标志，保存后可单独执行或加入组合操作。'}>
  <ExcelWorkspace title={saved?.name || (selected ? '编辑方法' : '新建方法')} description="保存时将核对 Oracle 中的表字段或存储过程签名。" actions={<Button type="button" variant="outline" disabled={busy} onClick={() => setLibraryOpen(true)}><ListChecks aria-hidden="true" />方案库 <span>{schemes.length}</span></Button>}>
   <form onSubmit={event => void save(event)} className={styles.stack}>
    <FieldGroup>
     <div className={styles.methodToolbar}>
      <TextField label="方法名称" required maxLength={100} value={name} disabled={busy || !canManage} onChange={e => setName(e.target.value)} placeholder="例如：订单暂存表导入…" />
      <div className={styles.actions}><Button type="button" variant="outline" disabled={busy} onClick={reset}><Plus aria-hidden="true" />新建方法</Button><Button type="submit" disabled={busy || !canManage}><Save aria-hidden="true" />{busy ? '处理中…' : '保存方法'}</Button></div>
     </div>
     {config.operation === 'oracle_import' ? <OracleImportEditor config={config} onChange={setConfig} disabled={busy || !canManage} client={client} /> : <OracleProcedureEditor config={config} onChange={setConfig} disabled={busy || !canManage} client={client} />}
    </FieldGroup>
    <p className={styles.note}>{canManage ? '同名方法保存时将覆盖原配置。' : '当前账号可查看，配置保存需要 Excel 管理权限。'}</p>
   </form>
  </ExcelWorkspace>
  <ExcelWorkspace title="单独执行" description="使用已保存的方法创建后台任务。">
   <ExcelEditor><ExcelEditorSection title="执行设置">
    <div className={styles.stack}>
     {operation === 'oracle_import' && <ExcelFileSelection label="待导入的 Excel 文件" files={files} disabled={busy || running || !canExecute} onChange={next => { setFiles(next); uploads.clearUploadRef('import') }} hint="全部文件共用已保存的方法配置，按选择顺序逐个导入。" />}
     {!unchanged && <p className={styles.note}>请先选择或保存方法，再执行。</p>}
     <div aria-live="polite" className={styles.note}>{uploads.uploadProgress}</div>
     <div><Button type="button" disabled={busy || !canExecute || !unchanged || running} onClick={() => void execute()}><Play aria-hidden="true" />{busy ? '提交任务中…' : '执行已保存的方法'}</Button></div>
    </div>
   </ExcelEditorSection></ExcelEditor>
  </ExcelWorkspace>
  <Drawer open={libraryOpen} size="wide" title="已保存的方法" description="搜索并选择方法继续编辑。" closeDisabled={busy} onClose={() => setLibraryOpen(false)}>
   <div className={styles.library}><div className={styles.stack}>
    <Input aria-label="搜索已保存的方法" name="methodSearch" autoComplete="off" placeholder="搜索方法名称…" value={query} onChange={e => setQuery(e.target.value)} />
    {reading ? <FeedbackState kind="loading" title="读取方法中…" /> : visible.length ? <div className={styles.methodList}>{visible.map(item => <div className={styles.method} key={item.id}><Button type="button" variant={selected === item.id ? 'secondary' : 'ghost'} disabled={busy} onClick={() => { select(item); setLibraryOpen(false) }}>{item.name}</Button>{canManage && <Button type="button" variant="ghost" size="icon-sm" aria-label={`删除方法 ${item.name}`} disabled={busy} onClick={() => void remove(item)}><Trash2 aria-hidden="true" /></Button>}</div>)}</div> : <FeedbackState kind="empty" title={query ? '没有匹配的方法' : '尚未保存方法'} description="填写方法配置并保存。" />}
    {error && <FeedbackState kind="error" title="方法操作未完成" description={error} action={<Button type="button" variant="outline" onClick={() => void load()}>重新读取方法</Button>} />}
   </div></div>
  </Drawer>
  {error && <FeedbackState kind="error" title="操作未完成" description={error} action={<Button type="button" variant="outline" onClick={() => void load()}>重新读取方法</Button>} />}
  {message && <p role="status" className={styles.note}>{message}</p>}
  <WorkflowRunPanel jobs={jobs} onNavigateToJobs={onNavigateToJobs} />
 </ExcelWorkspacePage>
}
