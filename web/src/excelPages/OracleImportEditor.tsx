import { useEffect, useRef, useState } from 'react'
import { Plus, Search, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { TextField } from './OperationField'
import { parseOracleExportColumns, type OracleExportColumnDraft } from './oracleExportConfig'
import type { OracleImportConfig } from './excelOperations'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './ExcelOperations.module.css'

export function OracleImportEditor({ config, onChange, disabled, client }: { config: OracleImportConfig; onChange: (config: OracleImportConfig) => void; disabled: boolean; client: ExcelPageClient }) {
 const [columns, setColumns] = useState<OracleExportColumnDraft[]>([])
 const [error, setError] = useState('')
 const [reading, setReading] = useState(false)
 const request = useRef<AbortController | null>(null)
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
 return <div className={styles.stack}>
  <div className={styles.fields}><TextField label="Oracle 用户 / Owner" required value={config.owner} disabled={disabled} spellCheck={false} onChange={e => { setColumns([]); onChange({ ...config, owner: e.target.value.toUpperCase() }) }} placeholder="例如：ERP…" /><TextField label="目标表名" required value={config.tableName} disabled={disabled} spellCheck={false} onChange={e => { setColumns([]); onChange({ ...config, tableName: e.target.value.toUpperCase() }) }} placeholder="例如：IMPORT_ORDERS…" /></div>
  <div className={styles.fields}><TextField label="工作表名称" required value={config.sheetName} disabled={disabled} onChange={e => onChange({ ...config, sheetName: e.target.value })} hint="填写上传文件的工作表名称。操作台使用匹配结果时会自动切换结果工作表。" /><TextField label="表头所在行" type="number" min={1} required value={config.headerRow} disabled={disabled} onChange={e => onChange({ ...config, headerRow: Number(e.target.value) })} hint="表头后的非空行逐行导入。" /></div>
  <div className={styles.toolbar}><div><strong>字段映射</strong><p className={styles.note}>Excel 列填写表头名称；Oracle 列可读取后选择。</p></div><Button type="button" variant="outline" disabled={disabled || reading || !config.owner || !config.tableName} onClick={() => void loadColumns()}><Search aria-hidden="true" />{reading ? '读取中…' : '读取 Oracle 字段'}</Button></div>
  {error && <p role="alert" className={styles.note}>{error}</p>}
  <div className={styles.table}><Table><TableHeader><TableRow><TableHead>Excel 表头</TableHead><TableHead>Oracle 字段</TableHead><TableHead className="w-16">操作</TableHead></TableRow></TableHeader><TableBody>{config.mappings.map((item, index) => <TableRow key={index}><TableCell><Input aria-label={`第 ${index + 1} 项 Excel 表头`} autoComplete="off" required disabled={disabled} value={item.excelColumn} onChange={e => mapping(index, { excelColumn: e.target.value })} placeholder="例如：订单号…" /></TableCell><TableCell>{columns.length ? <NativeSelect aria-label={`第 ${index + 1} 项 Oracle 字段`} required disabled={disabled} value={item.field} onChange={e => mapping(index, { field: e.target.value })}><option value="">选择字段</option>{item.field && !columns.some(c => c.name === item.field) && <option value={item.field}>{item.field}（待核对）</option>}{columns.map(c => <option key={c.name} value={c.name}>{c.name} · {c.dataType}</option>)}</NativeSelect> : <Input aria-label={`第 ${index + 1} 项 Oracle 字段`} required disabled={disabled} value={item.field} autoComplete="off" spellCheck={false} onChange={e => mapping(index, { field: e.target.value.toUpperCase() })} placeholder="例如：DOCNO…" />}</TableCell><TableCell><Button type="button" variant="ghost" size="icon-sm" aria-label={`删除第 ${index + 1} 项字段映射`} disabled={disabled || config.mappings.length === 1} onClick={() => onChange({ ...config, mappings: config.mappings.filter((_, i) => i !== index) })}><Trash2 aria-hidden="true" /></Button></TableCell></TableRow>)}</TableBody></Table></div>
  <div><Button type="button" variant="outline" disabled={disabled} onClick={() => onChange({ ...config, mappings: [...config.mappings, { excelColumn: '', field: '' }] })}><Plus aria-hidden="true" />添加映射</Button></div>
  <p className={styles.note}>导入采用追加写入，同一文件在一个事务中提交；任一行失败则回滚本次导入。请确保主键及必填字段完整。</p>
 </div>
}
