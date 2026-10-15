import { useEffect, useId, useRef } from 'react'
import { isExcelMultiValueFilter, normalizeExcelFilterValues, selectExcelMatchFilterOperator, type ExcelMatchFilterConfig } from '../excelMatchConfig'
import { excelMatchFilterOperatorOptions } from './excelPageSupport'
import { Field } from './ExcelMatchPageParts'
import styles from './ExcelMatchPage.module.css'

export function ExcelMatchFilterRow({ filter, stepIndex, index, onChange, onRemove }: {
  filter: ExcelMatchFilterConfig
  stepIndex: number
  index: number
  onChange: (patch: Partial<ExcelMatchFilterConfig>) => void
  onRemove: () => void
}) {
  const hintID = useId()
  const valuesRef = useRef<HTMLTextAreaElement>(null)
  const multiple = isExcelMultiValueFilter(filter.op)
  const equalsAny = filter.op === 'eq_any'
  const includesEmpty = equalsAny && filter.includeEmpty === true
  const noValue = filter.op === 'empty' || filter.op === 'not_empty'

  useEffect(() => {
    const valid = normalizeExcelFilterValues(filter.values).length > 0 || includesEmpty
    valuesRef.current?.setCustomValidity(valid ? '' : equalsAny ? '请填写至少一个候选值，或勾选“空值也算匹配”。' : '请填写至少一个非空匹配内容。')
  }, [equalsAny, filter.values, includesEmpty, multiple])

  return (
    <div className={`${styles.stepFilterRow} ${styles.matchFilterRow}`} role="group" aria-label={`规则 ${stepIndex + 1} 筛选条件 ${index + 1}`}>
      <Field label={`条件 ${index + 1} · Excel 列`} name={`step_filter_column_${stepIndex}_${index}`} value={filter.column} onChange={(column) => onChange({ column })} required />
      <label>运算符<select value={filter.op} onChange={(event) => onChange(selectExcelMatchFilterOperator(filter, event.currentTarget.value))}>
        {excelMatchFilterOperatorOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select></label>
      {multiple ? (
        <div className={styles.filterValueEditor}>
          <label>候选值（每行一个）<textarea
            ref={valuesRef}
            name={`step_filter_values_${stepIndex}_${index}`}
            value={(filter.values ?? []).join('\n')}
            rows={3}
            placeholder={equalsAny ? '例如：0\n或填写其他完整内容' : '例如：天猫\n抖音'}
            required={!includesEmpty}
            aria-describedby={hintID}
            onChange={(event) => onChange({ values: event.currentTarget.value.split('\n') })}
          /></label>
          {equalsAny && <label className={styles.filterEmptyOption}><input type="checkbox" checked={includesEmpty} onChange={(event) => onChange({ includeEmpty: event.currentTarget.checked })} />空值也算匹配</label>}
          <small id={hintID}>{equalsAny ? '完整值匹配任意一项即可；填 0 并勾选上方选项，表示“0 或空”。' : '包含任意一项即可，例如“天猫旗舰店”或“抖音商城”。空行会忽略。'}</small>
        </div>
      ) : noValue ? <label>筛选值<input value="无需填写" readOnly disabled /></label> : (
        <Field label="筛选值" name={`step_filter_value_${stepIndex}_${index}`} value={filter.value} onChange={(value) => onChange({ value })} required />
      )}
      <button type="button" onClick={onRemove} aria-label={`删除规则 ${stepIndex + 1} 的条件 ${index + 1}`}>删除条件</button>
    </div>
  )
}
