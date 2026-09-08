import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import type { OfficeColumnMapping } from './types'
import styles from './OfficeMessage.module.css'

type Props = { value: OfficeColumnMapping[]; disabled?: boolean; onChange: (value: OfficeColumnMapping[]) => void }

export function ColumnMappingEditor({ value, disabled = false, onChange }: Props) {
  function update(index: number, patch: Partial<OfficeColumnMapping>) {
    onChange(value.map((item, current) => current === index ? { ...item, ...patch } : item))
  }
  function move(index: number, offset: number) {
    const target = index + offset
    if (target < 0 || target >= value.length) return
    const next = [...value]
    ;[next[index], next[target]] = [next[target], next[index]]
    onChange(next.map((item, order) => ({ ...item, order })))
  }
  return <div className={styles.editorBlock}>
    <div className={styles.editorHeading}><div><h3>导出列名对照</h3><p>源列使用 Oracle 返回列名；表头是 Excel 中显示的名称。</p></div><button type="button" disabled={disabled} onClick={() => onChange([...value, { sourceColumn: '', header: '', valueType: 'string', order: value.length, width: 18 }])}><Plus aria-hidden="true" />新增列</button></div>
    {value.length === 0 ? <p className={styles.inlineEmpty}>尚未配置导出列，可从结果表字段或 SELECT 测试结果自动生成。</p> : <div className={styles.mappingList}>
      {value.map((mapping, index) => <div className={styles.mappingRow} key={`${index}-${mapping.sourceColumn}`}>
        <label>源列<input value={mapping.sourceColumn} disabled={disabled} className={styles.mono} onChange={(event) => update(index, { sourceColumn: event.currentTarget.value.toUpperCase() })} /></label>
        <label>Excel 表头<input value={mapping.header} disabled={disabled} onChange={(event) => update(index, { header: event.currentTarget.value })} /></label>
        <label>数据类型<select value={mapping.valueType} disabled={disabled} onChange={(event) => update(index, { valueType: event.currentTarget.value as OfficeColumnMapping['valueType'] })}><option value="string">文本</option><option value="integer">整数</option><option value="decimal">小数（保留两位）</option><option value="date">日期</option><option value="datetime">日期时间</option><option value="boolean">布尔</option></select></label>
        <label>列宽<input type="number" min={0} max={255} value={mapping.width} disabled={disabled} onChange={(event) => update(index, { width: Number(event.currentTarget.value) })} /></label>
        <div className={styles.iconActions} aria-label={`调整第 ${index + 1} 个导出列`}><button type="button" aria-label="上移" disabled={disabled || index === 0} onClick={() => move(index, -1)}><ArrowUp aria-hidden="true" /></button><button type="button" aria-label="下移" disabled={disabled || index === value.length - 1} onClick={() => move(index, 1)}><ArrowDown aria-hidden="true" /></button><button type="button" aria-label="删除列" disabled={disabled} onClick={() => onChange(value.filter((_, current) => current !== index))}><Trash2 aria-hidden="true" /></button></div>
      </div>)}
    </div>}
  </div>
}
