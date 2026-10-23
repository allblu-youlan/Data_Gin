import { reportTimeDefaultOptions } from '../../timeDefaults'
import type { ReportMonthRange, ReportTimeDefault } from '../../types'
import { computedReportValuePreview, reportMonthRanges, reportTimeField, type ComputedReportValue } from './computedValues'
import styles from './ReportComputedValueEditor.module.css'

export function ReportComputedValueEditor({ value, logicalType, disabled, label, className, onChange }: { value: ComputedReportValue; logicalType?: string; disabled: boolean; label: string; className?: string; onChange: (value: ComputedReportValue) => void }) {
  return <div className={[styles.editor, className].filter(Boolean).join(' ')}>
    {value.kind === 'COLUMN_MAX' ? null : <label>{value.kind === 'TIME' ? '时间取值' : '月份范围'}<select aria-label={`${label}的动态时间`} disabled={disabled} value={value.code} onChange={(event) => onChange(value.kind === 'TIME' ? { kind: 'TIME', code: event.currentTarget.value as ReportTimeDefault } : { kind: 'TIME_RANGE', code: event.currentTarget.value as ReportMonthRange })}>{value.kind === 'TIME' ? reportTimeDefaultOptions(reportTimeField(logicalType)).map((option) => <option key={option.value} value={option.value}>{option.label}</option>) : reportMonthRanges.map((option) => <option key={option.value} value={option.value}>{option.label}首日至末日</option>)}</select></label>}
    <small>{computedReportValuePreview(value, logicalType)}</small>
  </div>
}
