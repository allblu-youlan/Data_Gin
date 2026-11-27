import { useId } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { TextField } from './OperationField'
import { ExcelEditor, ExcelEditorSection } from './ExcelWorkspace'
import { parseOracleExportColumns } from './oracleExportConfig'
import { normalizeOracleImportConfig, type OracleImportConfig } from './excelOperations'
import type { ExcelPageClient } from './useExcelUploads'
import { OracleImportColumnField } from './OracleImportColumnField'
import { OracleImportTarget } from './OracleImportTarget'
import { useOracleImportCatalog } from './useOracleImportCatalog'
import styles from './ExcelOperations.module.css'

export function OracleImportEditor({ config, onChange: commitChange, disabled, client }: { config: OracleImportConfig; onChange: (config: OracleImportConfig) => void; disabled: boolean; client: ExcelPageClient }) {
 const owner = config.owner.trim()
 const tableName = config.tableName.trim()
 const path = owner && tableName ? `/v1/excel-match-jobs/oracle-columns?${new URLSearchParams({ owner, name: tableName })}` : null
 const catalog = useOracleImportCatalog(client, path, parseOracleExportColumns, '读取 Oracle 字段失败')
 const manual = Boolean(path && !catalog.loading && !catalog.items.length)
 const identifierFieldID = useId()
 const allSheetsID = useId()
 const rawCellValuesID = useId()
 const identifierID = useId()
 const identifier = normalizeOracleImportConfig(config).fixedIdentifier
 function onChange(next: OracleImportConfig) { commitChange(normalizeOracleImportConfig(next)) }
 function updateIdentifier(value: Partial<typeof identifier>) { onChange({ ...config, fixedIdentifier: { ...identifier, ...value } }) }
 function mapping(index: number, value: Partial<OracleImportConfig['mappings'][number]>) { onChange({ ...config, mappings: config.mappings.map((item, i) => i === index ? { ...item, ...value } : item) }) }
 return <ExcelEditor>
  <ExcelEditorSection title="目标配置" description="选择目标 Oracle 表、工作表范围与表头行。"><div className={styles.stack}>
  <OracleImportTarget owner={config.owner} tableName={config.tableName} disabled={disabled} client={client} onChange={target => onChange({ ...config, ...target })} />
  <div className={styles.checkOption}><input id={allSheetsID} type="checkbox" checked={Boolean(config.allSheets)} disabled={disabled} onChange={e => onChange({ ...config, allSheets: e.target.checked })} /><FieldLabel htmlFor={allSheetsID}>逐个导入所有工作表</FieldLabel></div>
  <div className={styles.checkOption}><input id={rawCellValuesID} type="checkbox" checked={config.rawCellValues ?? false} disabled={disabled} aria-describedby={`${rawCellValuesID}-hint`} onChange={e => onChange({ ...config, rawCellValues: e.target.checked })} /><FieldLabel htmlFor={rawCellValuesID}>快速读取原始值</FieldLabel></div>
  <p id={`${rawCellValuesID}-hint`} className={styles.note}>跳过数字和日期的显示格式以加快导入；文本编号的前导零保留。若编号依赖数字补零格式（如000123），请关闭。</p>
  <div className={styles.fields}><TextField label="工作表名称" required={!config.allSheets} value={config.sheetName} disabled={disabled || config.allSheets} onChange={e => onChange({ ...config, sheetName: e.target.value })} hint={config.allSheets ? '按工作簿顺序导入全部工作表，所有工作表使用相同表头行与字段映射。' : '只导入指定工作表；操作台使用匹配结果时自动导入全部结果文件与工作表。'} /><TextField label="表头所在行" type="number" min={1} required value={config.headerRow} disabled={disabled} onChange={e => onChange({ ...config, headerRow: Number(e.target.value) })} hint="表头后的非空行逐行导入。" /></div>
  </div></ExcelEditorSection>
  <ExcelEditorSection title="字段映射" description="填写 Excel 表头，再按字段名或注释选择对应的 Oracle 字段。" actions={
   <Button type="button" variant="outline" disabled={!path || catalog.loading} onClick={catalog.retry}><RefreshCw aria-hidden="true" />刷新字段</Button>
  }><div className={styles.stack}>
   {catalog.error ? <p role="alert" className={styles.sourceError}>{catalog.error}。可刷新字段或手动填写字段名。</p> :
    <p role="status" className={styles.note}>{!path ? '请先选择目标表。' : catalog.loading ? '正在读取 Oracle 字段…' :
     catalog.items.length ? `${owner}.${tableName} · ${catalog.items.length} 个字段` : '该表未返回字段，可刷新字段或手动填写字段名。'}</p>}
   <div className={styles.table}><Table className={styles.oracleMapping}><TableHeader><TableRow>
    <TableHead>Excel 表头</TableHead><TableHead>Oracle 字段</TableHead><TableHead className="w-16">操作</TableHead>
   </TableRow></TableHeader><TableBody>{config.mappings.map((item, index) => <TableRow key={index}>
    <TableCell><Input aria-label={`第 ${index + 1} 项 Excel 表头`} autoComplete="off" required disabled={disabled} value={item.excelColumn}
     onChange={e => mapping(index, { excelColumn: e.target.value })} placeholder="例如：订单号…" /></TableCell>
    <TableCell><OracleImportColumnField label={`第 ${index + 1} 项 Oracle 字段`} required value={item.field} columns={catalog.items}
     disabled={disabled} manual={manual} onChange={field => mapping(index, { field })} /></TableCell>
    <TableCell><Button type="button" variant="ghost" size="icon-sm" aria-label={`删除第 ${index + 1} 项字段映射`} disabled={disabled || config.mappings.length === 1}
     onClick={() => onChange({ ...config, mappings: config.mappings.filter((_, i) => i !== index) })}><Trash2 aria-hidden="true" /></Button></TableCell>
   </TableRow>)}</TableBody></Table></div>
  <div><Button type="button" variant="outline" disabled={disabled} onClick={() => onChange({ ...config, mappings: [...config.mappings, { excelColumn: '', field: '' }] })}><Plus aria-hidden="true" />添加映射</Button></div>
  </div></ExcelEditorSection>
  <ExcelEditorSection title="自动导入标号（可选）"><div className={styles.stack}>
   <div className={styles.checkOption}><input id={identifierID} type="checkbox" checked={identifier.enabled} disabled={disabled} onChange={e => updateIdentifier({ enabled: e.target.checked })} /><FieldLabel htmlFor={identifierID}>启用服务端自动生成导入标号</FieldLabel></div>
   <div className={styles.fields}><Field><FieldLabel htmlFor={identifierFieldID}>导入标号写入的 Oracle 字段</FieldLabel>
    <OracleImportColumnField id={identifierFieldID} label="导入标号写入的 Oracle 字段" required={identifier.enabled}
     disabled={disabled || !identifier.enabled} manual={manual} columns={catalog.items} value={identifier.field} onChange={field => updateIdentifier({ field })} />
   </Field></div>
   <p className={styles.note}>标号由服务端在执行时自动生成。同一次导入的全部文件、全部 Sheet 和所有行使用同一个标号，每次重新执行自动递增。配置只保存启用状态和目标字段，不保存运行标号；无需在 Excel 中新增该列。</p>
  </div></ExcelEditorSection>
  <p className={styles.note}>导入采用追加写入，多个文件逐个处理；请确保主键及必填字段完整。</p>
 </ExcelEditor>
}
