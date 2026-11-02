import { formatReportCell } from '../../formatReportCell.js'
import { useState } from 'react'
import { Copy, Info } from 'lucide-react'
import { DataTable } from '../../../ui'
import type { ReportResultColumn, ReportResultPage } from '../../types'
import type { RecordDetailSelection } from '../ReportRecordDetailDrawer/ReportRecordDetailDrawer'
import styles from '../../pages/ReportQueryPage/ReportQueryPage.module.css'

export function ReportResultTable({ page, onInspect, onRecord }: { page: ReportResultPage; onInspect: (column: ReportResultColumn) => void; onRecord: (selection: RecordDetailSelection) => void }) {
  const [notice, setNotice] = useState('')
  async function copy(value: unknown) { try { await navigator.clipboard.writeText(formatReportCell(value, '')); setNotice('原始值已复制。') } catch { setNotice('当前浏览器不支持复制，请从记录详情查看。') } }
  return <><DataTable scrollLabel="报表查询结果"><thead><tr>{page.columns.map((column) => <th key={column.fieldId} scope="col"><span className={styles.columnHeader}>{column.header}<button type="button" aria-label={`查看 ${column.header} 字段详情`} onClick={() => onInspect(column)}><Info aria-hidden="true" /></button></span></th>)}</tr></thead><tbody>{page.rows.map((row) => <tr key={row.key}>{page.columns.map((column) => {
    const relation = column.relationId ? row.relationStates?.[column.relationId] : undefined
    const canDetail = Boolean(column.relationId && column.capabilities?.canViewDetail && relation?.canViewDetail)
    const value = row.displayValues && Object.prototype.hasOwnProperty.call(row.displayValues, column.code) && column.valueMode !== 'RAW' ? row.displayValues[column.code] : row.values[column.code]
    return <td className={numericValueType(column.valueType) ? styles.numericCell : undefined} key={column.fieldId}>{canDetail ? <button type="button" className={styles.recordLink} aria-label={`查看 ${column.header} 对应记录：${formatReportCell(value, column.nullDisplay, column.valueType)}`} onClick={(event) => onRecord({ runId: page.run.id, rowKey: row.key, relationId: column.relationId ?? '', trigger: event.currentTarget })}>{formatReportCell(value, column.nullDisplay, column.valueType)}</button> : <span title={relation?.status === 'UNMATCHED' ? '未匹配到关联记录' : undefined}>{formatReportCell(value, column.nullDisplay, column.valueType)}</span>}{column.relationId && column.capabilities?.canReadRaw && Object.prototype.hasOwnProperty.call(row.values, column.code) ? <button type="button" className={styles.rawCopy} aria-label={`复制 ${column.header} 原始值`} onClick={() => void copy(row.values[column.code])}><Copy aria-hidden="true" /></button> : null}</td>
  })}</tr>)}</tbody></DataTable>{!page.rows.length ? <p role="status">本次查询没有匹配记录。</p> : null}{notice ? <p role="status">{notice}</p> : null}</>
}
function numericValueType(valueType: string) { return valueType === 'integer' || valueType === 'decimal' || valueType === 'number' }
