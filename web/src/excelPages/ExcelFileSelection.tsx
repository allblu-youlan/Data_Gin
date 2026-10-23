import { useId } from 'react'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { excelFileSize } from './excelPageSupport'
import styles from './ExcelOperations.module.css'

export function ExcelFileSelection({ label, files, disabled, onChange, hint, name = 'file' }: {
 label: string
 files: readonly File[]
 disabled: boolean
 onChange: (files: File[]) => void
 hint?: string
 name?: string
}) {
 const id = useId()
 const totalSize = files.reduce((total, file) => total + file.size, 0)
 return <div className={styles.stack}>
  <Field data-disabled={disabled || undefined}>
   <FieldLabel htmlFor={id}>{label}</FieldLabel>
   <Input id={id} name={name} type="file" multiple accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" disabled={disabled} aria-describedby={`${id}-summary${hint ? ` ${id}-hint` : ''}`} onChange={event => onChange(Array.from(event.currentTarget.files ?? []))} />
   <FieldDescription id={`${id}-summary`} aria-live="polite">{files.length ? `已选择 ${files.length} 个文件 · 总计 ${excelFileSize(totalSize)}` : '可一次选择多个 .xlsx 文件，按选择顺序处理。'}</FieldDescription>
   {hint && <FieldDescription id={`${id}-hint`}>{hint}</FieldDescription>}
  </Field>
  {files.length > 0 && <ol className={styles.fileList} aria-label="已选择的 Excel 文件">{files.map((file, index) => <li key={`${index}-${file.name}-${file.lastModified}`}><span>{index + 1}. {file.name}</span><span>{excelFileSize(file.size)}</span></li>)}</ol>}
 </div>
}
