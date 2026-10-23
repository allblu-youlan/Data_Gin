import type { FormEvent } from 'react'
import type { ReportAllowedFilter, ReportQueryInputs, ReportQueryOperator, ReportRunContract } from '../../types'
import { ReportConditionEditor } from './ReportConditionEditor'
import { ReportComputedValueEditor } from './ReportComputedValueEditor'
import { computedReportValueError, isComputedReportValue, reportTimeOperator, supportsComputedReportValue, supportsReportTimeValue } from './computedValues'
import { isSimpleRuntimeWhere, readRuntimeFilter, runtimeCollection, runtimeDateInput, runtimeDateValue, runtimeFilterKey, runtimeNoValue, runtimeOperatorLabels, updateRuntimeFilter } from './runtimeFilters'
import styles from './ReportRuntimeQueryEditor.module.css'

export function ReportRuntimeQueryEditor({ contract, value, disabled, onChange }: { contract: ReportRunContract; value: ReportQueryInputs; disabled: boolean; onChange: (value: ReportQueryInputs) => void }) {
  const fields = (contract.allowedFilters ?? []).map((field) => ({ ...field, label: field.label || `${contract.relations?.find((relation) => relation.id === field.aliasId)?.name || (field.aliasId === 'base' ? '表 1' : field.aliasId)} → 字段 #${field.columnId}` }))
  const sortable = contract.allowedSortFields ?? []
  const orders = value.orderBy ?? []
  const simple = isSimpleRuntimeWhere(contract, value)
  if (!fields.length && !sortable.length) return null
  return <section className={styles.editor} aria-label="报表筛选条件">
    {fields.length ? <><header className={styles.heading}><strong>筛选条件 <span>{fields.length}</span></strong><small>填写需要筛选的字段，留空查询全部</small></header>{simple ? <div className={styles.filters}>{fields.map((field) => <RuntimeFilter key={runtimeFilterKey(field)} field={field} value={value} disabled={disabled} onChange={onChange} />)}</div> : <details className={styles.compatibility}><summary>已有筛选 · 兼容编辑</summary><p>原有筛选值与组合条件继续保留，可在此调整，或清空后改用字段筛选。</p><button type="button" disabled={disabled} onClick={() => onChange({ ...value, where: { logic: 'AND', items: [] } })}>清空并改用字段筛选</button><ReportConditionEditor fields={fields} value={value.where ?? { logic: 'AND', items: [] }} runtime disabled={disabled} onChange={(where) => onChange({ ...value, where })} /></details>}</> : null}
    {sortable.length ? <details className={styles.sort}><summary>本次排序{orders.length ? ` · ${orders.length} 项` : ''}</summary><div className={styles.orders}>{orders.map((order, index) => <div className={styles.order} key={index}><label>排序字段<select value={runtimeFilterKey(order)} disabled={disabled} onChange={(event) => { const field = sortable.find((field) => runtimeFilterKey(field) === event.currentTarget.value); if (field) onChange({ ...value, orderBy: orders.map((item, itemIndex) => itemIndex === index ? { aliasId: field.aliasId, columnId: field.columnId, direction: item.direction } : item) }) }}>{sortable.map((field) => <option key={runtimeFilterKey(field)} value={runtimeFilterKey(field)}>{field.label || fields.find((item) => runtimeFilterKey(item) === runtimeFilterKey(field))?.label || `${field.aliasId} → #${field.columnId}`}</option>)}</select></label><label>方向<select disabled={disabled} value={order.direction} onChange={(event) => { const direction = event.currentTarget.value === 'DESC' ? 'DESC' : 'ASC'; onChange({ ...value, orderBy: orders.map((item, itemIndex) => itemIndex === index ? { ...item, direction } : item) }) }}><option value="ASC">升序</option><option value="DESC">降序</option></select></label><button type="button" disabled={disabled} onClick={() => onChange({ ...value, orderBy: orders.filter((_, itemIndex) => itemIndex !== index) })}>删除</button></div>)}<button type="button" disabled={disabled || orders.length >= 3} onClick={() => { const field = sortable[0]; if (field) onChange({ ...value, orderBy: [...orders, { aliasId: field.aliasId, columnId: field.columnId, direction: 'ASC' }] }) }}>增加排序</button></div></details> : null}
  </section>
}

function RuntimeFilter({ field, value, disabled, onChange }: { field: ReportAllowedFilter; value: ReportQueryInputs; disabled: boolean; onChange: (value: ReportQueryInputs) => void }) {
  const condition = readRuntimeFilter(value, field)
  const operator = condition?.operator ?? ''
  const effective = operator || field.operators.find((item) => !runtimeNoValue(item))
  const valueOperator = operator || effective || ''
  const raw = condition?.value?.kind === 'CONSTANT' ? condition.value.value : ''
  const computed = isComputedReportValue(condition?.value) ? condition.value : undefined
  const timeOperator = reportTimeOperator(field.operators, operator)
  const inputType = field.logicalType === 'date' ? 'date' : field.logicalType === 'datetime' ? 'datetime-local' : 'text'
  const inputMode = field.logicalType === 'integer' ? 'numeric' : field.logicalType === 'decimal' ? 'decimal' : undefined
  function update(raw: unknown) { if (effective) onChange(updateRuntimeFilter(value, field, effective, raw)) }
  const input = (entry: unknown, ariaLabel: string, onValue: (text: string) => void) => {
    const updateValue = (event: FormEvent<HTMLInputElement>) => onValue(runtimeDateValue(event.currentTarget.value, field.logicalType))
    return <input aria-label={ariaLabel} disabled={disabled || !effective} type={inputType} inputMode={inputMode} step={field.logicalType === 'datetime' ? 1 : undefined} autoComplete="off" value={runtimeDateInput(entry, field.logicalType)} placeholder="输入筛选值…" onChange={updateValue} onInput={inputType === 'text' ? undefined : updateValue} />
  }
  return <fieldset className={styles.filter} disabled={disabled}><legend>{field.label}</legend><div className={styles.controls}><select aria-label={`${field.label}的筛选方式`} value={operator} onChange={(event) => {
    const selected = event.currentTarget.value
    const next: ReportQueryOperator | '' = field.operators.find((item) => item === selected) ?? ''
    const nextRaw = next === 'BETWEEN' ? Array.isArray(raw) && raw.length === 2 ? raw : ['', ''] : runtimeCollection(next) ? Array.isArray(raw) ? raw : [] : Array.isArray(raw) ? '' : raw
    onChange(updateRuntimeFilter(value, field, next, nextRaw, next && computed && !computedReportValueError(computed, next, field.logicalType) ? computed : undefined))
  }}><option value="">不限</option>{field.operators.map((item) => <option value={item} key={item}>{runtimeOperatorLabels[item]}</option>)}</select>{computed ? <span className={styles.noValue}>{computed.kind === 'COLUMN_MAX' ? '本列最大值' : '按查询时间计算'}</span> : runtimeNoValue(valueOperator) ? <span className={styles.noValue}>无需填写值</span> : valueOperator === 'BETWEEN' ? <div className={styles.range}>{input(Array.isArray(raw) ? raw[0] : '', `${field.label}区间起始值`, (text) => update([text, Array.isArray(raw) ? raw[1] ?? '' : '']))}<span>至</span>{input(Array.isArray(raw) ? raw[1] : '', `${field.label}区间结束值`, (text) => update([Array.isArray(raw) ? raw[0] ?? '' : '', text]))}</div> : runtimeCollection(valueOperator) ? <input aria-label={`${field.label}的集合值`} type="text" autoComplete="off" placeholder="多个值用逗号分隔…" value={Array.isArray(raw) ? raw.join(',') : String(raw ?? '')} onChange={(event) => update(event.currentTarget.value.split(/[,，\n]/))} /> : input(raw, `${field.label}的筛选值`, update)}</div>
    {supportsComputedReportValue(field.logicalType) ? <div className={styles.valueSource}><label>值来源<select aria-label={`${field.label}的值来源`} value={computed?.kind ?? 'CONSTANT'} onChange={(event) => {
      const kind = event.currentTarget.value
      if (kind === 'COLUMN_MAX') onChange(updateRuntimeFilter(value, field, 'EQ', undefined, { kind }))
      else if (kind === 'TIME_RANGE') onChange(updateRuntimeFilter(value, field, 'BETWEEN', undefined, { kind, code: 'CURRENT_MONTH' }))
      else if (kind === 'TIME' && timeOperator) onChange(updateRuntimeFilter(value, field, timeOperator, undefined, { kind, code: 'MONTH_START' }))
      else onChange(updateRuntimeFilter(value, field, operator || effective || '', ''))
    }}><option value="CONSTANT">手动填写</option>{supportsReportTimeValue(field.logicalType) && field.operators.includes('BETWEEN') ? <option value="TIME_RANGE">动态月份范围</option> : null}{supportsReportTimeValue(field.logicalType) && timeOperator ? <option value="TIME">动态时间</option> : null}{field.operators.includes('EQ') ? <option value="COLUMN_MAX">本列最大值</option> : null}</select></label>{computed ? <ReportComputedValueEditor value={computed} logicalType={field.logicalType} label={field.label || field.columnId} disabled={disabled} className={styles.computed} onChange={(source) => onChange(updateRuntimeFilter(value, field, operator, undefined, source))} /> : null}</div> : null}
  </fieldset>
}
