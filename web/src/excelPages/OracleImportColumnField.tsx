import { Select } from 'antd'
import { Input } from '@/components/ui/input'
import type { OracleExportColumn } from './oracleExportConfig'
import styles from './ExcelOperations.module.css'

type Props = {
 id?: string
 label: string
 value: string
 columns: OracleExportColumn[]
 manual: boolean
 disabled: boolean
 required: boolean
 onChange: (name: string) => void
}

export function OracleImportColumnField({ id, label, value, columns, manual, disabled, required, onChange }: Props) {
 if (manual) {
  return <Input id={id} aria-label={label} required={required} disabled={disabled} value={value} autoComplete="off" spellCheck={false}
   placeholder="输入 Oracle 字段名" onChange={event => onChange(event.target.value.toUpperCase())} />
 }
 const column = columns.find(item => item.name === value)
 const options = columns.map(item => ({ value: item.name, label: [item.name, item.comment?.trim(), item.dataType].filter(Boolean).join(' · ') }))

 return <div className={styles.oracleField}>
  <Select id={id} aria-label={label} aria-required={required} className={styles.oracleSelect} showSearch optionFilterProp="label"
   placeholder="选择字段，支持搜索名称或注释" notFoundContent="没有匹配的字段" disabled={disabled || !columns.length}
   options={options} value={value || undefined} onChange={onChange} optionLabelProp="value" />
  {column && <span className={styles.oracleComment}>{column.comment?.trim() || '无字段注释'} · {column.dataType}</span>}
  {value && columns.length > 0 && !column && <span className={styles.sourceError}>当前表中未找到此字段，请核对。</span>}
 </div>
}
