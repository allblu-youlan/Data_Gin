import { useEffect, useRef, useState } from 'react'
import { Database, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { TextField } from './OperationField'
import { ExcelEditorSection } from './ExcelWorkspace'
import { OracleProcedureResults } from './OracleProcedureResults'
import { applyOracleProcedureSignature, oracleProcedureIdentity, oracleProcedureQualifiedName, oracleProcedureResultLimit, parseOracleProcedureArguments, parseOracleProcedures, type OracleProcedureRef, type OracleProcedureSummary } from './oracleProcedureConfig'
import type { ProcedureConfig } from './excelOperations'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './ExcelOperations.module.css'

type Props = { config: ProcedureConfig; onChange: (config: ProcedureConfig) => void; disabled: boolean; client: ExcelPageClient }

export function OracleProcedureLookup({ config, onChange, disabled, client }: Props) {
 const [search, setSearch] = useState('')
 const [procedures, setProcedures] = useState<OracleProcedureSummary[]>([])
 const [searched, setSearched] = useState(false)
 const [reading, setReading] = useState<'search' | 'signature' | ''>('')
 const [error, setError] = useState('')
 const [signature, setSignature] = useState<{ key: string; argumentCount: number; defaultInputs: string[] } | null>(null)
 const [readingProcedure, setReadingProcedure] = useState<OracleProcedureRef | null>(null)
 const request = useRef<AbortController | null>(null)
 const currentConfig = useRef(config)
 currentConfig.current = config
 const busy = disabled || Boolean(reading)
 const currentProcedure = config.owner.trim() && config.procedureName.trim() ? { owner: config.owner.trim(), packageName: config.packageName.trim(), name: config.procedureName.trim() } : null
 const currentSignature = currentProcedure && signature?.key === oracleProcedureIdentity(currentProcedure) ? signature : null

 useEffect(() => () => request.current?.abort(), [])
 useEffect(() => { request.current?.abort(); setReading(''); setReadingProcedure(null); setError('') }, [config.owner, config.packageName, config.procedureName])
 useEffect(() => { setProcedures([]); setSearched(false) }, [config.owner])

 async function findProcedures() {
  request.current?.abort()
  const controller = new AbortController()
  request.current = controller
  setReading('search'); setReadingProcedure(null); setError(''); setProcedures([]); setSearched(false)
  try {
   const query = new URLSearchParams({ owner: config.owner.trim(), search: search.trim(), limit: String(oracleProcedureResultLimit) })
   const response = await client(`/v1/excel-match-jobs/oracle-procedures?${query}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
   if (controller.signal.aborted) return
   if (!response.ok) throw new Error(response.error?.message || 'Oracle 存储过程查询失败，请重试。')
   setProcedures(parseOracleProcedures(response.data)); setSearched(true)
  } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Oracle 存储过程查询失败，请重试。') }
  finally { if (!controller.signal.aborted) setReading('') }
 }

 async function loadSignature(procedure: OracleProcedureRef) {
  request.current?.abort()
  const controller = new AbortController()
  request.current = controller
  setReading('signature'); setReadingProcedure(procedure); setError('')
  try {
   const query = new URLSearchParams({ owner: procedure.owner, package: procedure.packageName, name: procedure.name })
   const response = await client(`/v1/excel-match-jobs/oracle-procedure-signature?${query}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
   if (controller.signal.aborted) return
   if (!response.ok) throw new Error(response.error?.message || 'Oracle 存储过程参数读取失败，请重试。')
   const argumentsList = parseOracleProcedureArguments(response.data)
   onChange(applyOracleProcedureSignature(currentConfig.current, procedure, argumentsList))
   setSignature({ key: oracleProcedureIdentity(procedure), argumentCount: argumentsList.length, defaultInputs: argumentsList.filter(argument => argument.direction === 'IN' && argument.defaulted).map(argument => `${argument.name}（${argument.dataType}）`) })
  } catch (cause) { if (!controller.signal.aborted) setError(`读取 ${oracleProcedureQualifiedName(procedure)} 的参数失败：${cause instanceof Error ? cause.message : '请重试。'} 当前配置目标未更改。`) }
  finally { if (!controller.signal.aborted) { setReading(''); setReadingProcedure(null) } }
 }

 function updateTarget(value: Partial<ProcedureConfig>) { setError(''); setSignature(null); onChange({ ...config, ...value }) }

 return <ExcelEditorSection title="过程配置" description="按过程名或包名搜索，选择后自动读取参数；也可填写名称后读取。">
  <FieldGroup>
   <div className={styles.fieldsThree}>
    <TextField label="Oracle 用户 / Owner" required disabled={busy} value={config.owner} spellCheck={false} onChange={event => updateTarget({ owner: event.target.value.toUpperCase() })} placeholder="例如：ERP…" />
    <TextField label="包名（可选）" disabled={busy} value={config.packageName} spellCheck={false} onChange={event => updateTarget({ packageName: event.target.value.toUpperCase() })} placeholder="例如：PKG_ORDER…" />
    <TextField label="存储过程名称" required disabled={busy} value={config.procedureName} spellCheck={false} onChange={event => updateTarget({ procedureName: event.target.value.toUpperCase() })} placeholder="例如：PROCESS_IMPORT…" />
   </div>
   <div className={styles.fields}>
    <TextField label="搜索存储过程" disabled={busy} value={search} onChange={event => setSearch(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); if (config.owner.trim()) void findProcedures() } }} placeholder="输入过程名或包名关键词…" hint={`每次最多显示 ${oracleProcedureResultLimit} 个结果，支持按过程名或包名收窄查询。`} />
    <div className={styles.actions}>
     <Button type="button" variant="outline" disabled={busy || !config.owner.trim()} onClick={() => void findProcedures()}><Search aria-hidden="true" data-icon="inline-start" />{reading === 'search' ? '查询中…' : '查询存储过程'}</Button>
     <Button type="button" variant="outline" disabled={busy || !config.owner.trim() || !config.procedureName.trim()} onClick={() => void loadSignature({ owner: config.owner.trim(), packageName: config.packageName.trim(), name: config.procedureName.trim() })}><Database aria-hidden="true" data-icon="inline-start" />{reading === 'signature' ? '读取中…' : '读取过程参数'}</Button>
    </div>
   </div>
   <div className={styles.procedureSelection} role="region" aria-label="当前已选过程">
    <span className={styles.note}>当前已选过程</span>
    <strong className={styles.procedureName}>{currentProcedure ? oracleProcedureQualifiedName(currentProcedure) : '尚未选择存储过程'}</strong>
    {currentProcedure && <span className={styles.procedureMeta}>Owner：{currentProcedure.owner} · {currentProcedure.packageName ? `包名：${currentProcedure.packageName}` : '独立过程'}</span>}
    <p role="status" aria-live="polite" className={styles.note}>{currentSignature ? `参数读取成功：${currentSignature.argumentCount} 个参数${currentSignature.argumentCount === 0 ? '，这是无参数过程。' : `，当前配置 ${config.parameters.length} 个，输入值可留空传 NULL。`}` : currentProcedure ? `当前配置 ${config.parameters.length} 个参数，尚未读取 Oracle 签名。请选择查询结果或点击“读取过程参数”核对。` : '从查询结果中选择，或填写名称后读取参数。'}</p>
    {currentSignature && currentSignature.defaultInputs.length > 0 && <p className={styles.note}>可选 IN 参数：{currentSignature.defaultInputs.join('、')}。未配置时使用 Oracle 默认值；如需传 NULL，可手动添加。</p>}
    {readingProcedure && <p role="status" className={styles.note}>正在读取 {oracleProcedureQualifiedName(readingProcedure)} 的参数，读取成功后更新当前已选过程…</p>}
   </div>
   {searched && <OracleProcedureResults procedures={procedures} current={currentProcedure} disabled={busy} onSelect={procedure => void loadSignature(procedure)} />}
   {error && <p role="alert" className={styles.sourceError}>{error}</p>}
  </FieldGroup>
 </ExcelEditorSection>
}
