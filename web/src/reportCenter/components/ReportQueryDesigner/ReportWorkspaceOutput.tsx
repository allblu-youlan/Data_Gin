import { ArrowDown, ArrowUp, Columns3, Copy, Eye, X } from 'lucide-react'
import { Button } from '../../../ui'
import { newReportQueryColumn } from '../../bojunQueryConfig'
import type { ReportQueryColumn, ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import styles from './ReportTablesWorkspace.module.css'

export function ReportWorkspaceOutput({ spec, metadata, activeAlias, label, onChange, preview = false }: { spec: ReportQuerySpec; metadata: BojunMetadata; activeAlias?: string; label: (alias: string) => string; onChange: (spec: ReportQuerySpec) => void; preview?: boolean }) {
  const alias = (column: ReportQueryColumn) => column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId
  function fieldCode(column: ReportQueryColumn) {
    const source = column.source
    if (source.kind === 'RELATION_DISPLAY') return '默认显示值'
    const tableId = source.kind === 'BASE_COLUMN' ? spec.base.tableId : spec.relations.find((relation) => relation.id === source.relationId)?.targetTableId
    return metadata[tableId ?? '']?.columns.find((field) => String(field.id) === source.columnId)?.dbName || `字段 #${source.columnId}`
  }
  function move(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= spec.columns.length) return
    const columns = [...spec.columns]
    ;[columns[index], columns[target]] = [columns[target], columns[index]]
    onChange({ ...spec, columns })
  }
  function duplicate(column: ReportQueryColumn, index: number) {
    const identity = newReportQueryColumn(column.source, column.header)
    const columns = [...spec.columns]
    columns.splice(index + 1, 0, { ...column, fieldId: identity.fieldId, code: identity.code })
    onChange({ ...spec, columns })
  }
  if (preview) return <section className={styles.preview} aria-label="结果结构预览"><header><strong><Columns3 aria-hidden="true" />结果结构预览</strong><span>{spec.columns.length} 个输出列 · 实际数据可在核验后试运行</span></header><div className={styles.previewScroll} tabIndex={0} role="region" aria-label="报表输出结构，可横向滚动">{spec.columns.length ? <table><thead><tr>{spec.columns.map((column) => <th key={column.fieldId}>{column.header || fieldCode(column)}</th>)}</tr></thead><tbody><tr>{spec.columns.map((column) => <td key={column.fieldId}><span>{label(alias(column))}</span><code>{fieldCode(column)}</code></td>)}</tr></tbody></table> : <p className={styles.emptyPreview}>选好字段后，这里会按输出顺序展示报表结构。</p>}</div></section>
  return <aside className={styles.output} aria-label="全部报表输出列">
    <header className={styles.paneHeading}><h3><Columns3 aria-hidden="true" />报表输出 <span>{spec.columns.length}</span></h3></header><p className={styles.outputCaption}>最终报表与 Excel 共用的列清单</p>
    <ol className={styles.outputList}>{spec.columns.map((column, index) => <li key={column.fieldId} data-active={alias(column) === activeAlias}><span className={styles.outputOrdinal}>{String(index + 1).padStart(2, '0')}</span><div className={styles.outputCopy}><input name={`report-output-${column.fieldId}`} autoComplete="off" aria-label={`第 ${index + 1} 个输出列表头`} value={column.header} placeholder={fieldCode(column)} onChange={(event) => { const header = event.currentTarget.value; onChange({ ...spec, columns: spec.columns.map((item) => item.fieldId === column.fieldId ? { ...item, header } : item) }) }} /><small title={`${label(alias(column))} · ${fieldCode(column)}`}>{label(alias(column))} · {fieldCode(column)}</small></div><div className={styles.outputActions}><Button title="复制输出列" aria-label={`复制输出列 ${index + 1}`} onClick={() => duplicate(column, index)}><Copy aria-hidden="true" /></Button><Button aria-label={`上移输出列 ${index + 1}`} disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp aria-hidden="true" /></Button><Button aria-label={`下移输出列 ${index + 1}`} disabled={index === spec.columns.length - 1} onClick={() => move(index, 1)}><ArrowDown aria-hidden="true" /></Button><Button aria-label={`移除输出字段 ${column.header || '未命名列'}`} onClick={() => onChange({ ...spec, columns: spec.columns.filter((item) => item.fieldId !== column.fieldId) })}><X aria-hidden="true" /></Button></div></li>)}</ol>
    {!spec.columns.length ? <p className={styles.emptyHint}>勾选中间的字段即可加入输出</p> : null}<footer className={styles.outputFooter}><Eye aria-hidden="true" /><span>直接修改表头或调整顺序<br />更多格式在“列与导出”中设置</span></footer>
  </aside>
}
