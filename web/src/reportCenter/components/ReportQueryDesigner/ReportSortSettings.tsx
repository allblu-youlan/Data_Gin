import type { ReportQuerySpec } from '../../types'
import type { QueryFieldOption } from './ReportConditionEditor'
import styles from './ReportQueryDesigner.module.css'

export function ReportSortSettings({ spec, fields, onChange }: { spec: ReportQuerySpec; fields: QueryFieldOption[]; onChange: (spec: ReportQuerySpec) => void }) {
  return <details className={styles.advanced}><summary>排序设置（可选）</summary><div className={styles.editor}>
    <p className={styles.hint}>默认顺序与查询时可调整的排序字段，按需设置即可。</p>
    {spec.orderBy.map((order, index) => <div className={styles.actions} key={index}>
      <label className={styles.field}>默认排序字段<select aria-label={`默认排序字段 ${index + 1}`} value={`${order.aliasId}:${order.columnId}`} onChange={(event) => { const field = fields.find((field) => `${field.aliasId}:${field.columnId}` === event.currentTarget.value); if (field) onChange({ ...spec, orderBy: spec.orderBy.map((item, position) => position === index ? { ...item, aliasId: field.aliasId, columnId: field.columnId } : item) }) }}>{!fields.some((field) => field.aliasId === order.aliasId && field.columnId === order.columnId) ? <option value={`${order.aliasId}:${order.columnId}`}>已失效字段</option> : null}{fields.map((field) => <option key={`${field.aliasId}:${field.columnId}`} value={`${field.aliasId}:${field.columnId}`}>{field.label}</option>)}</select></label>
      <label className={styles.field}>方向<select aria-label={`默认排序方向 ${index + 1}`} value={order.direction} onChange={(event) => { const direction = event.currentTarget.value === 'DESC' ? 'DESC' : 'ASC'; onChange({ ...spec, orderBy: spec.orderBy.map((item, position) => position === index ? { ...item, direction } : item) }) }}><option value="ASC">升序</option><option value="DESC">降序</option></select></label><button type="button" aria-label={`移除默认排序 ${index + 1}`} onClick={() => onChange({ ...spec, orderBy: spec.orderBy.filter((_, position) => position !== index) })}>移除</button>
    </div>)}
    <div><button type="button" disabled={!fields.length || spec.orderBy.length >= 3} onClick={() => { const field = fields[0]; if (field) onChange({ ...spec, orderBy: [...spec.orderBy, { aliasId: field.aliasId, columnId: field.columnId, direction: 'ASC' }] }) }}>添加默认排序</button></div>
    <div className={styles.fields} aria-label="查询时允许排序的字段">{fields.map((field) => { const selected = spec.allowedSortFields?.some((item) => item.aliasId === field.aliasId && item.columnId === field.columnId); return <label className={styles.check} key={`${field.aliasId}:${field.columnId}`}><input type="checkbox" checked={Boolean(selected)} onChange={() => onChange({ ...spec, allowedSortFields: selected ? spec.allowedSortFields?.filter((item) => item.aliasId !== field.aliasId || item.columnId !== field.columnId) : [...(spec.allowedSortFields ?? []), { aliasId: field.aliasId, columnId: field.columnId }] })} />{field.label}</label> })}</div>
  </div></details>
}
