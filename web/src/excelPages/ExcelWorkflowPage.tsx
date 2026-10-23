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
import { defaultWorkflowSteps, newWorkflowStep, operationLabels, operationNeedsExcel, operationPaths, prepareWorkflowSteps, workflowIssues, workflowSourceIssue, workflowUploadIssues, type ExcelOperation, type OperationScheme, type WorkflowConfig, type WorkflowEditorStep } from './excelOperations'
import styles from './ExcelOperations.module.css'

type Props = { client: ExcelPageClient; token: string; permissions: string[]; refreshVersion: number; setLoading: (value: boolean) => void; setResult: (value: ClientResponse | null) => void; onNavigateToJobs: () => void }
const operations = Object.keys(operationLabels) as ExcelOperation[]
const links: Record<ExcelOperation, string> = { export_match: 'excel_schemes', import_update: 'excel_write', oracle_import: 'excel_oracle_import', oracle_procedure: 'excel_procedure', oracle_export: 'excel_export' }

export function ExcelWorkflowPage({ client, token, permissions, refreshVersion, setLoading, setResult, onNavigateToJobs }: Props) {
 const [catalog, setCatalog] = useState<Record<ExcelOperation, OperationScheme[]>>({ export_match: [], import_update: [], oracle_import: [], oracle_procedure: [], oracle_export: [] })
 const [workflows, setWorkflows] = useState<OperationScheme<WorkflowConfig>[]>([])
 const [selected, setSelected] = useState(0)
 const [name, setName] = useState('')
 const [steps, setSteps] = useState<WorkflowEditorStep[]>(defaultWorkflowSteps)
 const [addOperation, setAddOperation] = useState<ExcelOperation>('oracle_procedure')
 const [filesByStep, setFilesByStep] = useState<Record<string, File[]>>({})
 const [busy, setBusy] = useState(false)
 const [reading, setReading] = useState(true)
 const [catalogError, setCatalogError] = useState('')
 const [error, setError] = useState('')
 const [message, setMessage] = useState('')
 const [showIssues, setShowIssues] = useState(false)
 const uploads = useExcelUploads(client)
 const jobs = useExcelJobs({ section: 'schemes', client, token, refreshVersion, setLoading, setResult })
 const canManage = permissions.includes('excel.manage')
 const canExecute = permissions.includes('excel.execute')
 const hasWriteStep = steps.some(step => step.operation === 'import_update')
 const running = Boolean(jobs.job && ['pending', 'running'].includes(jobs.job.status))
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
 const invalidSource = steps.some((step, index) => Boolean(workflowSourceIssue(step, index, steps)))
 steps.forEach((step, i) => { if (step.schemeId && !catalog[step.operation].some(scheme => scheme.id === step.schemeId)) issues.push(`第 ${i + 1} 步的方法已不可用，请重新选择。`) })
 function update(id: string, value: Partial<Omit<WorkflowEditorStep, 'id'>>) { setSteps(current => current.map(step => step.id === id ? { ...step, ...value } : step)) }
 function move(index: number, offset: number) { setSteps(current => { const next = [...current]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; return next }) }
 function removeStep(id: string) {
  setSteps(current => current.filter(step => step.id !== id))
  setFilesByStep(current => { const next = { ...current }; delete next[id]; return next })
  uploads.clearUploadRef(`workflow:${id}`)
 }
 function select(id: number) {
  const workflow = workflows.find(item => item.id === id)
  setSelected(id); setName(workflow?.name || '')
  setSteps(workflow ? prepareWorkflowSteps(workflow.config.steps) : defaultWorkflowSteps())
  setFilesByStep({}); uploads.resetUploads(); setError(''); setMessage(''); setShowIssues(false)
 }
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
   await load(); if (saved) setSelected(saved.id); setMessage('组合操作已保存。仅保存方法和数据来源关联，原始 Excel 文件需在执行前选择。')
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
  if (busy || running) return
  setShowIssues(true)
  if (!canExecute || (hasWriteStep && !canManage)) { setError(hasWriteStep ? '包含 Excel 写入的流程需要 Excel 管理和执行权限。' : '当前账号没有执行 Excel 任务的权限。'); return }
  const executionIssues = [...issues, ...workflowUploadIssues(steps, filesByStep)]
  if (executionIssues.length) { setError(executionIssues.join(' ')); return }
  if (hasWriteStep && !window.confirm('此流程包含“Excel 写入（回写数据）”，将实际写入数据库，只填充符合规则的空字段。后续步骤失败时，此前成功步骤的数据会保留。确认执行？')) return
  setBusy(true); setError(''); setMessage('')
  try {
   const stepUploads: Record<string, string[]> = {}
   for (const [index, step] of steps.entries()) {
    if (operationNeedsExcel(step.operation) && step.source !== 'previous') {
     stepUploads[step.id] = await uploads.ensureExcelUploads(`workflow:${step.id}`, filesByStep[step.id], `第 ${index + 1} 步 · `)
    }
   }
   const response = await client('/v1/excel-match-jobs/workflow-runs', { method: 'POST', body: { config: { operation: 'excel_workflow', steps }, stepUploads, ...(hasWriteStep ? { confirmWrite: true } : {}) }, retry: false, acceptSafeErrorMessage: true, showResult: false, silentLoading: true, timeoutMs: 120_000 })
   if (!response.ok) throw new Error(response.error?.message || '任务创建失败')
   jobs.registerCreatedJob(response, false)
  } catch (cause) { setError(cause instanceof Error ? cause.message : '任务创建失败') } finally { setBusy(false) }
 }
 return <PageCanvas className={styles.page}>
  <PageHeader title="复杂操作台" description="为每一步选择方法和 Excel 来源；前一步成功后，才会开始下一步。" actions={<Badge variant="secondary">串行执行 · 失败即停</Badge>} />
  <div className={styles.layout}>
   <div className={styles.stack}>
    <Section title="组合方案" actions={<Button type="button" variant="ghost" size="icon-sm" aria-label="刷新方法与组合方案" disabled={reading || busy} onClick={() => void load()}><RefreshCw aria-hidden="true" /></Button>}>
     <div className={styles.stack}>
      <SelectField label="已保存的组合操作" value={selected} disabled={busy || reading} onChange={e => select(Number(e.target.value))}><option value={0}>新建组合操作</option>{workflows.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</SelectField>
      <TextField label="组合操作名称" maxLength={100} value={name} disabled={busy || !canManage} onChange={e => setName(e.target.value)} placeholder="例如：订单导入与结算…" />
      <div className={styles.actions}><Button type="button" variant="outline" disabled={busy || reading || !canManage || Boolean(catalogError)} onClick={() => void save()}><Save aria-hidden="true" />保存组合</Button><Button type="button" variant="ghost" size="icon" aria-label="删除当前组合操作" disabled={busy || !selected || !canManage} onClick={() => void remove()}><Trash2 aria-hidden="true" /></Button></div>
      <p className={styles.note}>方案保存每一步的方法和来源关联，不保存本次选择的文件。</p>
     </div>
    </Section>
    <Section title="方法准备" description="分别配置并保存，再由步骤独立调用。"><div className={styles.stack}>{operations.map(op => <div key={op} className={styles.toolbar}><a className={styles.methodLink} href={`#${links[op]}`}>{operationLabels[op]}</a><Badge variant="outline">{catalog[op].length} 个</Badge></div>)}</div></Section>
   </div>
   <Section title="执行步骤" description="每个步骤独立选择原始文件，或明确使用此前某一步的匹配结果。最多 20 步。">
    <div className={styles.stack}>
     {reading && <FeedbackState kind="loading" title="读取方法目录中…" />}
     {catalogError && <FeedbackState kind="error" title="方法目录未就绪" description={catalogError} action={<Button type="button" variant="outline" onClick={() => void load()}>重试</Button>} />}
     <ol className={styles.steps}>{steps.map((step, index) => {
      const sourceIssue = workflowSourceIssue(step, index, steps)
      const sourceIndex = steps.findIndex(item => item.id === step.sourceStepId)
      const sourceStep = steps[sourceIndex]
      const sourceName = sourceStep ? catalog[sourceStep.operation].find(item => item.id === sourceStep.schemeId)?.name || operationLabels[sourceStep.operation] : ''
      const previousMatches = steps.slice(0, index).flatMap((item, i) => item.operation === 'export_match' ? [{ step: item, index: i }] : [])
      const methodMissing = !step.schemeId || (!reading && !catalog[step.operation].some(item => item.id === step.schemeId))
      return <li className={styles.step} key={step.id}>
       <div className={styles.stepHead}>
        <div className={styles.stepTitle}><span className={styles.number}>{String(index + 1).padStart(2, '0')}</span>{operationLabels[step.operation]}</div>
        <div className={styles.actions}>
         <Button type="button" variant="ghost" size="icon-sm" disabled={busy || index === 0} aria-label={`上移第 ${index + 1} 步`} onClick={() => move(index, -1)}><ArrowUp aria-hidden="true" /></Button>
         <Button type="button" variant="ghost" size="icon-sm" disabled={busy || index === steps.length - 1} aria-label={`下移第 ${index + 1} 步`} onClick={() => move(index, 1)}><ArrowDown aria-hidden="true" /></Button>
         <Button type="button" variant="ghost" size="icon-sm" disabled={busy} aria-label={`删除第 ${index + 1} 步`} onClick={() => removeStep(step.id)}><Trash2 aria-hidden="true" /></Button>
        </div>
       </div>
       <div className={styles.fields}>
        <SelectField label="调用已保存的方法" aria-label={`第 ${index + 1} 步调用的方法`} aria-invalid={showIssues && methodMissing} hint={showIssues && methodMissing ? '请选择一个可用的已保存方法。' : undefined} value={step.schemeId} disabled={busy || reading} onChange={e => update(step.id, { schemeId: Number(e.target.value) })}>
         <option value={0}>请选择方法</option>{step.schemeId > 0 && !catalog[step.operation].some(item => item.id === step.schemeId) && <option value={step.schemeId}>方法已不可用，请重新选择</option>}{catalog[step.operation].map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
        </SelectField>
        {operationNeedsExcel(step.operation) && <SelectField label="Excel 数据来源" aria-label={`第 ${index + 1} 步的 Excel 数据来源`} aria-invalid={Boolean(sourceIssue)} value={step.source === 'previous' ? `previous:${step.sourceStepId || ''}` : 'upload'} disabled={busy} onChange={e => update(step.id, e.target.value === 'upload' ? { source: 'upload', sourceStepId: undefined } : { source: 'previous', sourceStepId: e.target.value.slice('previous:'.length) })}>
         <option value="upload">为本步骤选择原始 Excel 文件</option>
         {sourceIssue && <option value={`previous:${step.sourceStepId || ''}`}>{sourceIndex >= 0 ? `引用失效：第 ${sourceIndex + 1} 步，请重新选择` : '匹配结果来源失效，请重新选择'}</option>}
         {previousMatches.map(item => <option key={item.step.id} value={`previous:${item.step.id}`}>第 {item.index + 1} 步 · {catalog.export_match.find(method => method.id === item.step.schemeId)?.name || 'Excel 匹配（未选方法）'}的结果</option>)}
        </SelectField>}
       </div>
       {sourceIssue && <p className={styles.sourceError} role="alert">{sourceIssue}</p>}
       {operationNeedsExcel(step.operation) && step.source !== 'previous' && <div className={styles.file}>
        <ExcelFileSelection label={`第 ${index + 1} 步 · ${operationLabels[step.operation]}的原始 Excel`} name={`workflow-${step.id}`} files={filesByStep[step.id] ?? []} disabled={busy || running || !canExecute || (step.operation === 'import_update' && !canManage)} onChange={files => { setFilesByStep(current => ({ ...current, [step.id]: files })); uploads.clearUploadRef(`workflow:${step.id}`) }} hint="这些文件仅供本步骤使用，多个文件按选择顺序处理；调整步骤顺序不会改变文件归属。" />
       </div>}
       {operationNeedsExcel(step.operation) && step.source === 'previous' && !sourceIssue && <p className={styles.sourceSummary}>使用第 {sourceIndex + 1} 步“{sourceName}”的全部匹配结果文件{step.operation === 'oracle_import' ? '及全部结果工作表' : ''}，无需上传原始文件。</p>}
       {catalog[step.operation].length === 0 && !reading && <p className={styles.note}>尚无可用方法，请先到“方法准备”中的对应模块保存。</p>}
      </li>
     })}</ol>
     <div className={styles.toolbar}><SelectField label="添加操作类型" value={addOperation} disabled={busy || steps.length >= 20} onChange={e => setAddOperation(e.target.value as ExcelOperation)}>{operations.map(op => <option key={op} value={op}>{operationLabels[op]}</option>)}</SelectField><Button type="button" variant="outline" disabled={busy || steps.length >= 20} onClick={() => setSteps(current => [...current, newWorkflowStep(addOperation)])}><Plus aria-hidden="true" />添加步骤</Button></div>
     <div className={styles.note} aria-live="polite">{uploads.uploadProgress}</div>
     {hasWriteStep && !canManage && <p className={styles.sourceError} role="alert">Excel 写入会回写数据库，当前账号需要 Excel 管理权限后才能执行此流程。</p>}
     <div className={styles.footer}><p className={styles.note}>每个写入步骤独立提交，后续失败不会回滚此前成功的数据。<br />执行在后台继续，最终文件可在任务中下载。</p><Button type="button" disabled={busy || reading || Boolean(catalogError) || !canExecute || (hasWriteStep && !canManage) || running || invalidSource} onClick={() => void execute()}><Play aria-hidden="true" />{busy ? '提交任务中…' : running ? '任务执行中…' : '按顺序执行'}</Button></div>
    </div>
   </Section>
  </div>
  {error && <FeedbackState kind="error" title="操作未完成" description={error} />}{message && <p role="status" className={styles.note}>{message}</p>}
  <WorkflowRunPanel jobs={jobs} onNavigateToJobs={onNavigateToJobs} />
 </PageCanvas>
}
