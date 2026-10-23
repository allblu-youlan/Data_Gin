import { useCallback, useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Play, Plus, RefreshCw, Save, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { PageCanvas, PageHeader, Section, FeedbackState } from '../ui'
import type { ClientResponse } from '../api/client'
import { SelectField, TextField } from './OperationField'
import { WorkflowRunPanel } from './WorkflowRunPanel'
import { ExcelFileSelection } from './ExcelFileSelection'
import { useExcelJobs } from './useExcelJobs'
import { useExcelUploads, type ExcelPageClient } from './useExcelUploads'
import { readList, readObject } from './excelPageSupport'
import { defaultWorkflowSteps, operationLabels, operationPaths, workflowIssues, workflowNeedsUploads, type ExcelOperation, type OperationScheme, type WorkflowConfig, type WorkflowStep } from './excelOperations'
import styles from './ExcelOperations.module.css'

type Props = { client: ExcelPageClient; token: string; permissions: string[]; refreshVersion: number; setLoading: (value: boolean) => void; setResult: (value: ClientResponse | null) => void; onNavigateToJobs: () => void }
const operations = Object.keys(operationLabels) as ExcelOperation[]
const links: Record<ExcelOperation, string> = { export_match: 'excel_schemes', oracle_import: 'excel_oracle_import', oracle_procedure: 'excel_procedure', oracle_export: 'excel_export' }
export function ExcelWorkflowPage({ client, token, permissions, refreshVersion, setLoading, setResult, onNavigateToJobs }: Props) {
 const [catalog, setCatalog] = useState<Record<ExcelOperation, OperationScheme[]>>({ export_match: [], oracle_import: [], oracle_procedure: [], oracle_export: [] })
 const [workflows, setWorkflows] = useState<OperationScheme<WorkflowConfig>[]>([])
 const [selected, setSelected] = useState(0)
 const [name, setName] = useState('')
 const [steps, setSteps] = useState<WorkflowStep[]>(defaultWorkflowSteps)
 const [addOperation, setAddOperation] = useState<ExcelOperation>('oracle_procedure')
 const [files, setFiles] = useState<File[]>([])
 const [busy, setBusy] = useState(false)
 const [reading, setReading] = useState(true)
 const [catalogError, setCatalogError] = useState('')
 const [error, setError] = useState('')
 const [message, setMessage] = useState('')
 const [showIssues, setShowIssues] = useState(false)
 const uploads = useExcelUploads(client)
 const jobs = useExcelJobs({ section: 'schemes', client, token, refreshVersion, setLoading, setResult })
 const canManage = permissions.includes('excel.manage')
 const running = Boolean(jobs.job && ['pending', 'running'].includes(jobs.job.status))
 const canExecute = permissions.includes('excel.execute')
 const needFile = workflowNeedsUploads(steps)
 const load = useCallback(async (signal?: AbortSignal) => {
  setReading(true); setCatalogError('')
  const responses = await Promise.allSettled([...operations.map(op => operationPaths[op]), '/v1/excel-match-jobs/workflows'].map(path => client(path, { method: 'GET', signal, showResult: false, silentLoading: true })))
  if (signal?.aborted) return
  const next = {} as Record<ExcelOperation, OperationScheme[]>
  const errors: string[] = []
  responses.forEach((result, i) => {
   const label = i < operations.length ? operationLabels[operations[i]] : '组合操作'
   if (result.status === 'rejected' || !result.value.ok) { errors.push(`${label}读取失败`); return }
   if (i < operations.length) next[operations[i]] = readList<OperationScheme>(result.value, 'schemes')
   else setWorkflows(readList<OperationScheme<WorkflowConfig>>(result.value, 'schemes'))
  })
  setCatalog(current => ({ ...current, ...next })); setCatalogError(errors.join('；')); setReading(false)
 }, [client])
 useEffect(() => { const request = new AbortController(); void load(request.signal); return () => request.abort() }, [load, refreshVersion])
 const issues = workflowIssues(steps)
 steps.forEach((step, i) => { if (step.schemeId && !catalog[step.operation].some(scheme => scheme.id === step.schemeId)) issues.push(`第 ${i + 1} 步的方法已不可用，请重新选择。`) })
 function update(index: number, value: Partial<WorkflowStep>) { setSteps(current => current.map((step, i) => i === index ? { ...step, ...value } : step)) }
 function move(index: number, offset: number) { setSteps(current => { const next = [...current]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; return next }) }
 function select(id: number) { setSelected(id); const workflow = workflows.find(item => item.id === id); setName(workflow?.name || ''); setSteps(workflow ? structuredClone(workflow.config.steps) : defaultWorkflowSteps()); setError(''); setMessage('') }
 async function save() {
  setShowIssues(true)
  if (!name.trim()) { setError('请填写组合操作名称。'); return }
  if (issues.length) { setError(issues.join(' ')); return }
  if (workflows.some(item => item.name === name.trim()) && !window.confirm(`覆盖已保存的组合操作「${name.trim()}」？`)) return
  setBusy(true); setError(''); setMessage('')
  try {
   const response = await client('/v1/excel-match-jobs/workflows', { method: 'POST', body: { name, config: { operation: 'excel_workflow', steps } }, retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true, timeoutMs: 120_000 })
   if (!response.ok) throw new Error(response.error?.message || '保存组合操作失败')
   const saved = readObject<OperationScheme<WorkflowConfig>>(response, 'scheme')
   await load(); if (saved) setSelected(saved.id); setMessage('组合操作已保存。执行时会保存各方法的配置快照。')
  } catch (cause) { setError(cause instanceof Error ? cause.message : '保存失败') } finally { setBusy(false) }
 }
 async function remove() {
  const saved = workflows.find(item => item.id === selected)
  if (!saved || !window.confirm(`删除组合操作「${saved.name}」？已保存的方法仍会保留。`)) return
  setBusy(true); setError('')
  try {
   const response = await client(`/v1/excel-match-jobs/workflows/${selected}`, { method: 'DELETE', retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true })
   if (!response.ok) throw new Error(response.error?.message || '删除组合操作失败')
   await load(); select(0)
  } catch (cause) { setError(cause instanceof Error ? cause.message : '删除失败') } finally { setBusy(false) }
 }
 async function execute() {
  if (running) return
  setShowIssues(true)
  if (issues.length) { setError(issues.join(' ')); return }
  if (needFile && (!files.length || files.some(file => file.size === 0))) { setError('请先选择有效的待处理 .xlsx 文件。'); return }
  setBusy(true); setError(''); setMessage('')
  try {
   const uploadIds = needFile ? await uploads.ensureExcelUploads('export', files) : []
   const response = await client('/v1/excel-match-jobs/workflow-runs', { method: 'POST', body: { config: { operation: 'excel_workflow', steps }, uploadIds }, retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true, timeoutMs: 120_000 })
   if (!response.ok) throw new Error(response.error?.message || '任务创建失败')
   jobs.registerCreatedJob(response, false)
  } catch (cause) { setError(cause instanceof Error ? cause.message : '任务创建失败') } finally { setBusy(false) }
 }
 return <PageCanvas className={styles.page}>
  <PageHeader title="复杂操作台" description="把已保存的方法编排为一个流程；前一步成功后，才会开始下一步。" actions={<Badge variant="secondary">串行执行 · 失败即停</Badge>} />
  <div className={styles.layout}>
   <div className={styles.stack}>
    <Section title="组合方案" actions={<Button type="button" variant="ghost" size="icon-sm" aria-label="刷新方法与组合方案" disabled={reading || busy} onClick={() => void load()}><RefreshCw aria-hidden="true" /></Button>}><div className={styles.stack}><SelectField label="已保存的组合操作" value={selected} disabled={busy || reading} onChange={e => select(Number(e.target.value))}><option value={0}>新建组合操作</option>{workflows.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</SelectField><TextField label="组合操作名称" maxLength={100} value={name} disabled={busy || !canManage} onChange={e => setName(e.target.value)} placeholder="例如：订单导入与结算…" /><div className={styles.actions}><Button type="button" variant="outline" disabled={busy || reading || !canManage || Boolean(catalogError)} onClick={() => void save()}><Save aria-hidden="true" />保存组合</Button><Button type="button" variant="ghost" size="icon" aria-label="删除当前组合操作" disabled={busy || !selected || !canManage} onClick={() => void remove()}><Trash2 aria-hidden="true" /></Button></div></div></Section>
    <Section title="方法准备" description="分别配置并保存，操作台负责顺序调用。"><div className={styles.stack}>{operations.map(op => <div key={op} className={styles.toolbar}><a className={styles.methodLink} href={`#${links[op]}`}>{operationLabels[op]}</a><Badge variant="outline">{catalog[op].length} 个</Badge></div>)}</div></Section>
   </div>
   <Section title="执行步骤" description="可增删和调整顺序，最多 20 步。每个步骤调用已保存的方法。"><div className={styles.stack}>
    {reading && <FeedbackState kind="loading" title="读取方法目录中…" />}
    {catalogError && <FeedbackState kind="error" title="方法目录未就绪" description={catalogError} action={<Button type="button" variant="outline" onClick={() => void load()}>重试</Button>} />}
    <ol className={styles.steps}>{steps.map((step, index) => <li className={styles.step} key={index}><div className={styles.stepHead}><div className={styles.stepTitle}><span className={styles.number}>{String(index + 1).padStart(2, '0')}</span>{operationLabels[step.operation]}</div><div className={styles.actions}><Button type="button" variant="ghost" size="icon-sm" disabled={busy || index === 0} aria-label={`上移第 ${index + 1} 步`} onClick={() => move(index, -1)}><ArrowUp aria-hidden="true" /></Button><Button type="button" variant="ghost" size="icon-sm" disabled={busy || index === steps.length - 1} aria-label={`下移第 ${index + 1} 步`} onClick={() => move(index, 1)}><ArrowDown aria-hidden="true" /></Button><Button type="button" variant="ghost" size="icon-sm" disabled={busy} aria-label={`删除第 ${index + 1} 步`} onClick={() => setSteps(current => current.filter((_, i) => i !== index))}><Trash2 aria-hidden="true" /></Button></div></div><div className={styles.fields}><SelectField label="调用已保存的方法" aria-invalid={showIssues && !step.schemeId} hint={showIssues && !step.schemeId ? "请选择一个已保存的方法。" : undefined} value={step.schemeId} disabled={busy || reading} onChange={e => update(index, { schemeId: Number(e.target.value) })}><option value={0}>请选择方法</option>{step.schemeId > 0 && !catalog[step.operation].some(item => item.id === step.schemeId) && <option value={step.schemeId}>方法已不可用，请重新选择</option>}{catalog[step.operation].map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</SelectField>{['export_match', 'oracle_import'].includes(step.operation) && <SelectField label="Excel 数据来源" value={step.source || 'upload'} disabled={busy} onChange={e => update(index, { source: e.target.value as 'upload' | 'previous' })}><option value="upload">本次上传的原文件</option><option value="previous">前面匹配步骤生成的 Excel</option></SelectField>}</div>{step.source === 'previous' && <p className={styles.note}>直接使用前面最近一次匹配步骤写完的全部结果文件；Oracle 导入自动逐个处理全部结果工作表，无需重新上传。</p>}{catalog[step.operation].length === 0 && !reading && <p className={styles.note}>尚无可用方法，请先到“方法准备”中的对应模块保存。</p>}</li>)}</ol>
    <div className={styles.toolbar}><SelectField label="添加操作类型" value={addOperation} disabled={busy || steps.length >= 20} onChange={e => setAddOperation(e.target.value as ExcelOperation)}>{operations.map(op => <option key={op} value={op}>{operationLabels[op]}</option>)}</SelectField><Button type="button" variant="outline" disabled={busy || steps.length >= 20} onClick={() => setSteps(current => [...current, { operation: addOperation, schemeId: 0, ...(['export_match', 'oracle_import'].includes(addOperation) ? { source: 'upload' as const } : {}) }])}><Plus aria-hidden="true" />添加步骤</Button></div>
    {needFile && <div className={styles.file}><ExcelFileSelection label="本次处理的 Excel 文件" files={files} disabled={busy || running || !canExecute} onChange={next => { setFiles(next); uploads.clearUploadRef('export') }} hint="全部文件共用此流程，逐文件顺序处理；来源选“前面匹配步骤”时自动使用全部匹配结果文件与工作表，无需再次上传。" /></div>}
    <div className={styles.note} aria-live="polite">{uploads.uploadProgress}</div>
    <div className={styles.footer}><p className={styles.note}>每个写入步骤独立提交，后续失败不会回滚此前成功的数据。<br />执行在后台继续，最终文件可在任务中下载。</p><Button type="button" disabled={busy || reading || Boolean(catalogError) || !canExecute || running} onClick={() => void execute()}><Play aria-hidden="true" />{busy ? '提交任务中…' : running ? '任务执行中…' : '按顺序执行'}</Button></div>
   </div></Section>
  </div>
  {error && <FeedbackState kind="error" title="操作未完成" description={error} />}{message && <p role="status" className={styles.note}>{message}</p>}
  <WorkflowRunPanel jobs={jobs} onNavigateToJobs={onNavigateToJobs} />
 </PageCanvas>
}
