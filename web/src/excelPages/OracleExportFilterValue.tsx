import { ReportComputedValueEditor } from '../reportCenter/components/ReportQueryDesigner/ReportComputedValueEditor'
import { computedReportValuePreview, reportMonthRanges, reportTimeField } from '../reportCenter/components/ReportQueryDesigner/computedValues'
import { reportTimeDefaultOptions } from '../reportCenter/timeDefaults'
import type { ReportMonthRange, ReportTimeDefault } from '../reportCenter/types'
import { oracleExportDateInputValue, oracleExportFilterInputType, oracleExportFilterNeedsValue, type OracleExportFilterDraft } from './oracleExportConfig'
import { formatOracleExportDate, isOracleExportDateFormat, oracleExportDateFormats, oracleExportFormattedTimePreview } from './oracleExportDateFormats'
import { oracleExportFilterLogicalType } from './oracleExportFilterSources'
import styles from './OracleExportPage.module.css'

export function OracleExportFilterValue({ filter, index, dataType, disabled, onChange }: { filter: OracleExportFilterDraft; index: number; dataType: string; disabled: boolean; onChange: (filter: OracleExportFilterDraft) => void }) {
  const inputType = oracleExportFilterInputType(dataType)
  const source = filter.valueSource
  const needsValue = oracleExportFilterNeedsValue(filter.op)
  if (source?.kind === 'COLUMN_MAX') return <ReportComputedValueEditor value={source} logicalType={oracleExportFilterLogicalType(dataType)} disabled={disabled} label={`条件 ${index + 1}`} onChange={(valueSource) => onChange({ ...filter, valueSource })} />
  const dateFormat = filter.dateFormat
  const supportsDate = inputType !== 'text' || /^(?:N?VARCHAR2?|N?CHAR)(?:\b|\()/i.test(dataType.trim())
  function changeFormat(value: string) {
    const nextFormat = isOracleExportDateFormat(value) ? value : undefined
    const nextValue = nextFormat ? formatOracleExportDate(filter.value, nextFormat) : inputType === 'datetime-local' ? oracleExportDateInputValue(formatOracleExportDate(filter.value, 'yyyy-MM-dd HH:mm:ss')) : filter.value
    onChange({ ...filter, value: nextValue, dateFormat: nextFormat })
  }
  const logicalType = dateFormat && inputType === 'text' ? 'decimal' : oracleExportFilterLogicalType(dataType)
  return <div className={styles.filterValue}>
    {supportsDate && needsValue ? <label>日期格式<select aria-label={`条件 ${index + 1} 日期格式`} value={dateFormat ?? ''} disabled={disabled} onChange={(event) => changeFormat(event.currentTarget.value)}><option value="">{source ? '按原方案格式' : inputType === 'datetime-local' ? '日期时间（原格式）' : '普通筛选值'}</option>{oracleExportDateFormats.filter((format) => inputType !== 'number' || format === 'yyyyMMdd').map((format) => <option key={format} value={format}>{format}</option>)}</select></label> : null}
    {source ? <>
      <label>{source.kind === 'TIME' ? '时间取值' : '月份范围'}<select aria-label={`条件 ${index + 1}的动态时间`} value={source.code} disabled={disabled} onChange={(event) => onChange({ ...filter, value: '', valueSource: source.kind === 'TIME' ? { kind: 'TIME', code: event.currentTarget.value as ReportTimeDefault } : { kind: 'TIME_RANGE', code: event.currentTarget.value as ReportMonthRange } })}>{source.kind === 'TIME' ? reportTimeDefaultOptions(reportTimeField(logicalType)).map((option) => <option key={option.value} value={option.value}>{dateFormat ? option.label.replace('（YYYYMM）', '首日').replace('（YYYY）', '首日') : option.label}</option>) : reportMonthRanges.map((option) => <option key={option.value} value={option.value}>{option.label}首日至末日</option>)}</select></label>
      <small className={styles.hint}>{dateFormat ? oracleExportFormattedTimePreview(source, dateFormat) : computedReportValuePreview(source, logicalType)}</small>
    </> : <label>{dateFormat || inputType === 'datetime-local' ? '筛选时间' : '筛选值'}<input aria-label={`条件 ${index + 1} ${dateFormat || inputType === 'datetime-local' ? '筛选时间' : '筛选值'}`} type={dateFormat ? 'text' : inputType} step={!dateFormat && inputType === 'datetime-local' ? 1 : inputType === 'number' ? 'any' : undefined} value={needsValue ? !dateFormat && inputType === 'datetime-local' ? oracleExportDateInputValue(filter.value) : filter.value : ''} disabled={disabled || !needsValue} required={needsValue} placeholder={needsValue ? dateFormat ? formatOracleExportDate('2026-10-01 00:00:00', dateFormat) : '输入筛选值' : '无需填写'} onChange={(event) => onChange({ ...filter, value: event.currentTarget.value })} />{dateFormat ? <small className={styles.hint}>按 {dateFormat} 填写{inputType === 'number' ? '，例如 20261001' : ''}。</small> : inputType === 'datetime-local' ? <small className={styles.hint}>YYYY-MM-DD HH:mm:ss，精确到秒。</small> : null}</label>}
  </div>
}
