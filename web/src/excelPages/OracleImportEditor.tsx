import { useEffect, useId, useRef, useState } from 'react'
import { Plus, Search, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { SelectField, TextField } from './OperationField'
import { ExcelEditor, ExcelEditorSection } from './ExcelWorkspace'
import { parseOracleExportColumns, type OracleExportColumnDraft } from './oracleExportConfig'
import { normalizeOracleImportConfig, type OracleImportConfig } from './excelOperations'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './ExcelOperations.module.css'

export function OracleImportEditor({ config, onChange: commitChange, disabled, client }: { config: OracleImportConfig; onChange: (config: OracleImportConfig) => void; disabled: boolean; client: ExcelPageClient }) {
 const [columns, setColumns] = useState<OracleExportColumnDraft[]>([])
 const [error, setError] = useState('')
 const [reading, setReading] = useState(false)
 const request = useRef<AbortController | null>(null)
 const allSheetsID = useId()
 const identifierID = useId()
 const identifier = normalizeOracleImportConfig(config).fixedIdentifier
 function onChange(next: OracleImportConfig) { commitChange(normalizeOracleImportConfig(next)) }
 function updateIdentifier(value: Partial<typeof identifier>) { onChange({ ...config, fixedIdentifier: { ...identifier, ...value } }) }
 useEffect(() => {
  setColumns([]); setError(''); setReading(false)
  return () => request.current?.abort()
 }, [config.owner, config.tableName])
 async function loadColumns() {
  request.current?.abort()
  const controller = new AbortController()
  request.current = controller
  setReading(true); setError('')
  try {
   const response = await client(`/v1/excel-match-jobs/oracle-columns?${new URLSearchParams({ owner: config.owner, name: config.tableName })}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
   if (controller.signal.aborted) return
   if (!response.ok) throw new Error(response.error?.message || '读取字段失败')
   setColumns(parseOracleExportColumns(response.data))
  } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '读取字段失败') } finally { if (!controller.signal.aborted) setReading(false) }
 }
 function mapping(index: number, value: Partial<OracleImportConfig['mappings'][number]>) { onChange({ ...config, mappings: config.mappings.map((item, i) => i === index ? { ...item, ...value } : item) }) }
 return <ExcelEditor>
  <ExcelEditorSection title="目标配置" description="选择目标 Oracle 表、工作表范围与表头行。"><div className={styles.stack}>
  <div className={styles.fields}><TextField label="Oracle 用户 / Owner" required value={config.owner} disabled={disabled} spellCheck={false} onChange={e => { setColumns([]); onChange({ ...config, owner: e.target.value.toUpperCase() }) }} placeholder="例如：ERP…" /><TextField label="目标表名" required value={config.tableName} disabled={disabled} spellCheck={false} onChange={e => { setColumns([]); onChange({ ...config, tableName: e.target.value.toUpperCase() }) }} placeholder="例如：IMPORT_ORDERS…" /></div>
  <div className={styles.checkOption}><input id={allSheetsID} type="checkbox" checked={Boolean(config.allSheets)} disabled={disabled} onChange={e => onChange({ ...config, allSheets: e.target.checked })} /><FieldLabel htmlFor={allSheetsID}>逐个导入所有工作表</FieldLabel></div>
  <div className={styles.fields}><TextField label="工作表名称" required={!config.allSheets} value={config.sheetName} disabled={disabled || config.allSheets} onChange={e => onChange({ ...config, sheetName: e.target.value })} hint={config.allSheets ? '按工作簿顺序导入全部工作表，所有工作表使用相同表头行与字段映射。' : '只导入指定工作表；操作台使用匹配结果时自动导入全部结果文件与工作表。'} /><TextField label="表头所在行" type="number" min={1} required value={config.headerRow} disabled={disabled} onChange={e => onChange({ ...config, headerRow: Number(e.target.value) })} hint="表头后的非空行逐行导入。" /></div>
  </div></ExcelEditorSection>
  <ExcelEditorSection title="字段映射" description="Excel 列填写表头名称；Oracle 列可读取后选择。" actions={<Button type="button" variant="outline" disabled={disabled || reading || !config.owner || !config.tableName} onClick={() => void loadColumns()}><Search aria-hidden="true" />{reading ? '读取中…' : '读取 Oracle 字段'}</Button>}><div className={styles.stack}>
  {error && <p role="alert" className={styles.note}>{error}</p>}
  <div className={styles.table}><Table><TableHeader><TableRow><TableHead>Excel 表头</TableHead><TableHead>Oracle 字段</TableHead><TableHead className="w-16">操作</TableHead></TableRow></TableHeader><TableBody>{config.mappings.map((item, index) => <TableRow key={index}><TableCell><Input aria-label={`第 ${index + 1} 项 Excel 表头`} autoComplete="off" required disabled={disabled} value={item.excelColumn} onChange={e => mapping(index, { excelColumn: e.target.value })} placeholder="例如：订单号…" /></TableCell><TableCell>{columns.length ? <NativeSelect aria-label={`第 ${index + 1} 项 Oracle 字段`} required disabled={disabled} value={item.field} onChange={e => mapping(index, { field: e.target.value })}><option value="">选择字段</option>{item.field && !columns.some(c => c.name === item.field) && <option value={item.field}>{item.field}（待核对）</option>}{columns.map(c => <option key={c.name} value={c.name}>{c.name} · {c.dataType}</option>)}</NativeSelect> : <Input aria-label={`第 ${index + 1} 项 Oracle 字段`} required disabled={disabled} value={item.field} autoComplete="off" spellCheck={false} onChange={e => mapping(index, { field: e.target.value.toUpperCase() })} placeholder="例如：DOCNO…" />}</TableCell><TableCell><Button type="button" variant="ghost" size="icon-sm" aria-label={`删除第 ${index + 1} 项字段映射`} disabled={disabled || config.mappings.length === 1} onClick={() => onChange({ ...config, mappings: config.mappings.filter((_, i) => i !== index) })}><Trash2 aria-hidden="true" /></Button></TableCell></TableRow>)}</TableBody></Table></div>
  <div><Button type="button" variant="outline" disabled={disabled} onClick={() => onChange({ ...config, mappings: [...config.mappings, { excelColumn: '', field: '' }] })}><Plus aria-hidden="true" />添加映射</Button></div>
  </div></ExcelEditorSection>
  <ExcelEditorSection title="自动导入标号（可选）"><div className={styles.stack}><div className={styles.checkOption}><input id={identifierID} type="checkbox" checked={identifier.enabled} disabled={disabled} onChange={e => updateIdentifier({ enabled: e.target.checked })} /><FieldLabel htmlFor={identifierID}>启用服务端自动生成导入标号</FieldLabel></div><div className={styles.fields}>{columns.length ? <SelectField label="导入标号写入的 Oracle 字段" required={identifier.enabled} value={identifier.field} disabled={disabled || !identifier.enabled} onChange={e => updateIdentifier({ field: e.target.value })}><option value="">选择字段</option>{identifier.field && !columns.some(column => column.name === identifier.field) && <option value={identifier.field}>{identifier.field}（待核对）</option>}{columns.map(column => <option key={column.name} value={column.name}>{column.name} · {column.dataType}</option>)}</SelectField> : <TextField label="导入标号写入的 Oracle 字段" required={identifier.enabled} value={identifier.field} disabled={disabled || !identifier.enabled} spellCheck={false} placeholder="例如：BATCH_NO…" onChange={e => updateIdentifier({ field: e.target.value.toUpperCase() })} />}</div><p className={styles.note}>标号由服务端在执行时自动生成。同一次导入的全部文件、全部 Sheet 和所有行使用同一个标号，每次重新执行自动递增。配置只保存启用状态和目标字段，不保存运行标号；无需在 Excel 中新增该列。</p></div></ExcelEditorSection>
  <p className={styles.note}>导入采用追加写入，多个文件逐个处理；请确保主键及必填字段完整。</p>
 </ExcelEditor>
}
