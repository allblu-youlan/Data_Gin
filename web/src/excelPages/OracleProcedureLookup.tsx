import { useEffect, useRef, useState } from 'react'
import { Database, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { SelectField, TextField } from './OperationField'
import { ExcelEditorSection } from './ExcelWorkspace'
import { applyOracleProcedureSignature, parseOracleProcedureArguments, parseOracleProcedures, type OracleProcedureRef, type OracleProcedureSummary } from './oracleProcedureConfig'
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
 const [notice, setNotice] = useState('')
 const request = useRef<AbortController | null>(null)
 const currentConfig = useRef(config)
 currentConfig.current = config
 const busy = disabled || Boolean(reading)

 useEffect(() => () => request.current?.abort(), [])
 useEffect(() => { request.current?.abort(); setReading(''); setError('') }, [config.owner, config.packageName, config.procedureName])
 useEffect(() => { setProcedures([]); setSearched(false) }, [config.owner])

 async function findProcedures() {
  request.current?.abort()
  const controller = new AbortController()
  request.current = controller
  setReading('search'); setError(''); setNotice(''); setProcedures([]); setSearched(false)
  try {
   const query = new URLSearchParams({ owner: config.owner.trim(), search: search.trim(), limit: '50' })
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
  setReading('signature'); setError(''); setNotice('')
  try {
   const query = new URLSearchParams({ owner: procedure.owner, package: procedure.packageName, name: procedure.name })
   const response = await client(`/v1/excel-match-jobs/oracle-procedure-signature?${query}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
   if (controller.signal.aborted) return
   if (!response.ok) throw new Error(response.error?.message || 'Oracle 存储过程参数读取失败，请重试。')
   const argumentsList = parseOracleProcedureArguments(response.data)
   onChange(applyOracleProcedureSignature(currentConfig.current, procedure, argumentsList))
   const defaultInputs = argumentsList.filter(argument => argument.direction === 'IN' && argument.defaulted)
   setNotice(`已读取 ${argumentsList.length} 个参数，输入值可留空传 NULL。${defaultInputs.length ? `有默认值的 IN 参数未配置时使用 Oracle 默认值；如需传 NULL，可手动添加。` : ''}`)
  } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Oracle 存储过程参数读取失败，请重试。') }
  finally { if (!controller.signal.aborted) setReading('') }
 }

 function updateTarget(value: Partial<ProcedureConfig>) { setError(''); setNotice(''); onChange({ ...config, ...value }) }

 return <ExcelEditorSection title="过程配置" description="按过程名或包名搜索，选择后自动读取参数；也可填写名称后读取。">
  <FieldGroup>
   <div className={styles.fieldsThree}>
    <TextField label="Oracle 用户 / Owner" required disabled={busy} value={config.owner} spellCheck={false} onChange={event => updateTarget({ owner: event.target.value.toUpperCase() })} placeholder="例如：ERP…" />
    <TextField label="包名（可选）" disabled={busy} value={config.packageName} spellCheck={false} onChange={event => updateTarget({ packageName: event.target.value.toUpperCase() })} placeholder="例如：PKG_ORDER…" />
    <TextField label="存储过程名称" required disabled={busy} value={config.procedureName} spellCheck={false} onChange={event => updateTarget({ procedureName: event.target.value.toUpperCase() })} placeholder="例如：PROCESS_IMPORT…" />
   </div>
   <div className={styles.fields}>
    <TextField label="搜索存储过程" disabled={busy} value={search} onChange={event => setSearch(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); if (config.owner.trim()) void findProcedures() } }} placeholder="输入过程名或包名关键词…" hint="每次最多显示 50 个结果，可细化关键词后重新查询。" />
    <div className={styles.actions}>
     <Button type="button" variant="outline" disabled={busy || !config.owner.trim()} onClick={() => void findProcedures()}><Search aria-hidden="true" data-icon="inline-start" />{reading === 'search' ? '查询中…' : '查询存储过程'}</Button>
     <Button type="button" variant="outline" disabled={busy || !config.owner.trim() || !config.procedureName.trim()} onClick={() => void loadSignature({ owner: config.owner.trim(), packageName: config.packageName.trim(), name: config.procedureName.trim() })}><Database aria-hidden="true" data-icon="inline-start" />{reading === 'signature' ? '读取中…' : '读取过程参数'}</Button>
    </div>
   </div>
   {searched && <SelectField label="查询结果" disabled={busy || !procedures.length} value="" onChange={event => { const procedure = procedures[Number(event.target.value)]; if (procedure && !procedure.overload) void loadSignature(procedure) }} hint={procedures.length ? `找到 ${procedures.length} 个过程；选择后将更新过程名称及参数。重载过程暂不支持。` : '未找到存储过程，请调整 Owner 或关键词后重试。'}>
    <option value="">{procedures.length ? '选择存储过程…' : '没有匹配的存储过程'}</option>
    {procedures.map((procedure, index) => <option key={`${procedure.owner}.${procedure.packageName}.${procedure.name}.${procedure.overload}`} value={index} disabled={Boolean(procedure.overload)}>{[procedure.owner, procedure.packageName, procedure.name].filter(Boolean).join('.')} · {procedure.argumentCount} 个参数{procedure.overload ? `（重载 ${procedure.overload}，暂不支持）` : ''}</option>)}
   </SelectField>}
   {error && <p role="alert" className={styles.sourceError}>{error}</p>}
   {notice && <p role="status" className={styles.note}>{notice}</p>}
  </FieldGroup>
 </ExcelEditorSection>
}
