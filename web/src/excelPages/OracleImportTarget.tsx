import { useId, useState } from 'react'
import { Select } from 'antd'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { TextField } from './OperationField'
import { parseOracleExportTables } from './oracleExportConfig'
import type { ExcelPageClient } from './useExcelUploads'
import { useOracleImportCatalog } from './useOracleImportCatalog'
import styles from './ExcelOperations.module.css'

type Target = { owner: string; tableName: string }

export function OracleImportTarget({ owner, tableName, disabled, client, onChange }: Target & { disabled: boolean; client: ExcelPageClient; onChange: (target: Target) => void }) {
 const id = useId()
 const [manual, setManual] = useState(false)
 const [open, setOpen] = useState(false)
 const [search, setSearch] = useState('')
 const parts = search.trim().split('.')
 const query = new URLSearchParams({ owner: parts.length > 1 ? parts[0] : '', search: parts[parts.length - 1], limit: '100' })
 const catalog = useOracleImportCatalog(client, open && !manual ? `/v1/excel-match-jobs/oracle-tables?${query}` : null, parseOracleExportTables, '读取目标表目录失败')
 const selected = owner && tableName ? `${owner}.${tableName}` : undefined

 return <div className={styles.stack}>
  <div className={styles.toolbar}>
   <FieldLabel htmlFor={manual ? undefined : id}>Oracle 目标表</FieldLabel>
   <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={() => { setOpen(false); setManual(!manual) }}>{manual ? '从目录选择' : '手动填写表名'}</Button>
  </div>
  {manual ? <div className={styles.fields}>
   <TextField label="Oracle 用户 / Owner" required value={owner} disabled={disabled} spellCheck={false} onChange={e => onChange({ owner: e.target.value.toUpperCase(), tableName })} placeholder="例如：ERP" />
   <TextField label="目标表名" required value={tableName} disabled={disabled} spellCheck={false} onChange={e => onChange({ owner, tableName: e.target.value.toUpperCase() })} placeholder="例如：IMPORT_ORDERS" />
  </div> : <Field>
   <Select id={id} className={styles.oracleSelect} aria-label="Oracle 目标表" aria-required="true" showSearch filterOption={false} disabled={disabled} value={selected}
    placeholder="选择或搜索表名，可输入 OWNER.表名" loading={catalog.loading}
    onOpenChange={next => { setOpen(next); if (!next) setSearch('') }} onSearch={setSearch}
    options={catalog.items.map(table => ({ value: `${table.owner}.${table.name}`, label: `${table.owner}.${table.name}` }))}
    notFoundContent={catalog.loading ? '正在读取目标表…' : catalog.error || '没有匹配的表，请调整搜索条件。'}
    onChange={value => { const [nextOwner, nextTableName] = value.split('.'); onChange({ owner: nextOwner, tableName: nextTableName }) }} />
  </Field>}
 </div>
}
