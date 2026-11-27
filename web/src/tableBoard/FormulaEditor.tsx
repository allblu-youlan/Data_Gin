import { useEffect, useRef, useState } from 'react'
import type { ReportCenterClient } from '../reportCenter/api'
import { useReportDatasources } from '../reportCenter/useReportDatasources'
import { Button } from '../ui'
import { MappingEditor } from './MappingEditor'
import { DimensionPicker } from './DimensionPicker'
import { aggregates, differences, formats, metricFormat, periods, validMapping } from './formula'
import { boardRequest } from './api'
import type { CellResult, Formula, Mapping, Metric, Period } from './types'
import styles from './BoardCanvas.module.css'

type Props = {client: ReportCenterClient; metrics: Metric[]; initial?:Formula; address:string; onApply:(cell:Formula)=>void; onManage:()=>void}
type Preview = {asOf:string;before:CellResult;after:CellResult}
function resultText(r: CellResult) { return r.status === 'ok' ? r.display || r.value : r.status === 'null' ? '— / 数据缺失' : r.message || '读取失败' }
export function FormulaEditor({client,metrics,initial,address,onApply,onManage}:Props) {
  const [cell,setCell] = useState<Formula>(initial ?? {kind:'formula',metricId:metrics[0]?.id ?? '',period:'MONTH_TO_DATE'})
  const [mapping,setMapping] = useState(false)
  const [preview,setPreview] = useState<{key:string;result:Preview} | null>(null)
  const [error,setError] = useState('')
  const [busy,setBusy] = useState(false)
  const pending = useRef<AbortController | null>(null)
  useEffect(()=>()=>pending.current?.abort(),[])
  const connections = useReportDatasources(client)
  const base = metrics.find(m => m.id === cell.metricId) ?? metrics[0]
  if (!base) return <div className={styles.editor}><p>先从已有连接定义共享指标，再选择期间和门店。</p><Button onClick={onManage}>创建共享指标 GMV</Button></div>
  const effective:Metric = {...base,...cell.overrides}
  const field = effective.dimensions?.[0] ?? ''
  const formula:Formula = {...cell,metricId:base.id}
  const key = JSON.stringify([base,formula])
  const result = preview?.key === key ? preview.result : undefined
  function change(next: Formula) { setCell(next); setError(''); setPreview(null) }
  async function calculate(apply: boolean) {
    const ac=new AbortController()
    pending.current=ac
    setBusy(true); setError('')
    try {
      const response = result ?? await boardRequest<Preview>(client,'/preview','POST',{metric:base,cell:formula},ac.signal)
      if(ac.signal.aborted) return
      setPreview({key,result:response})
      if (apply && response.after.status !== 'error') onApply(formula)
    } catch(e) { if (!ac.signal.aborted) setError(e instanceof Error ? e.message : '预览失败') }
    finally { if (!ac.signal.aborted) setBusy(false) }
  }
  function restore(k:keyof Mapping) {
    const next={...effective,[k]:base[k]}
    if (k==='timeField') next.timeFormat=base.timeFormat
    if (base.bojunTableId && base.bojunTableId===effective.bojunTableId) {
      const fields=[next.valueField,next.timeField,...(next.dimensions ?? []),...(next.filters ?? []).map(f=>f.field)]
      const ids={...base.columnIds,...effective.columnIds}
      next.columnIds=Object.fromEntries(fields.filter(f=>f && ids[f]).map(f=>[f,ids[f]]))
    }
    change({...cell,overrides:differences(base,next),filters:cell.filters?.filter(f=>next.dimensions?.includes(f.field))})
  }
  function row(label:string,k:keyof Mapping,v:string) {
    return <div key={k}><dt>{label}</dt><dd>{v || '—'} <small>{cell.overrides && k in cell.overrides ? '仅本格' : '继承 ' + base.name}</small>
      {cell.overrides && k in cell.overrides && ['valueField','timeField','aggregate','filters','dimensions','timezone'].includes(k) &&
        effective.datasourceId===base.datasourceId && effective.owner===base.owner && effective.tableName===base.tableName &&
        <Button onClick={()=>restore(k)}>恢复{label}继承</Button>}</dd></div>
  }
  return <fieldset disabled={busy} className={styles.formula}>
    <div className={styles.inputs}><label>指标<select value={base.id} onChange={e=>{
      if(cell.overrides && !window.confirm('更换指标将清除本格已有的字段覆盖和门店选择，继续？')) return
      change({...cell, metricId:e.target.value,overrides:undefined,filters:undefined}); setMapping(false)
    }}>{metrics.map(m=><option key={m.id} value={m.id}>{m.name}</option>)}</select></label>
      <label>统计期间<select value={cell.period} onChange={e=>change({...cell,period:e.target.value as Period})}>
        {Object.entries(periods).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label>
      <label>展示格式<select value={cell.format ?? ''} onChange={e=>change({...cell,format:e.target.value || undefined})}>
        <option value="">继承指标 · {formats[metricFormat(base)]}</option>
        {Object.entries(formats).map(([key,label])=><option key={key} value={key}>{label}</option>)}</select></label></div>
    <DimensionPicker client={client} metric={effective} field={field} values={cell.filters?.filter(f=>f.field===field).map(f=>f.value) ?? []}
      onChange={v=>change({...cell,filters:v.length ? [{field,op:'EQ',value:v[0]}] : undefined})}/>
    <dl className={styles.mappingSummary}>
      {row('当前连接','datasourceId',connections.items.find(d=>d.id===effective.datasourceId)?.name ?? String(effective.datasourceId))}
      {row('来源表','tableName',effective.owner+'.'+effective.tableName)}{row('取值字段','valueField',effective.valueField ?? '全部记录')}
      {row('汇总方式','aggregate',aggregates[effective.aggregate])}{row('时间字段','timeField',effective.timeField)}
      {row('业务时区','timezone',effective.timezone || 'Asia/Shanghai')}{row('门店维度','dimensions',effective.dimensions?.join('、') || '无')}
      {row('业务过滤','filters',effective.filters?.map(f=>f.field+' '+f.op+' '+f.value).join('，') || '无附加条件')}
    </dl>
    <div className={styles.actions}><Button onClick={()=>setMapping(!mapping)}>{mapping ? '收起字段映射' : '字段映射'}</Button>
      {cell.overrides && <Button onClick={()=>change({...cell,overrides:undefined,filters:cell.filters?.filter(f=>base.dimensions?.includes(f.field))})}>恢复继承</Button>}</div>
    {mapping && <MappingEditor key={base.id} client={client} value={effective} onChange={m=>{
      const filters = m.datasourceId!==effective.datasourceId || m.owner!==effective.owner || m.tableName!==effective.tableName ? undefined : cell.filters?.filter(f=>m.dimensions?.includes(f.field))
      change({...cell,filters,overrides:differences(base,m)})
    }}/>}
    {result && <div className={styles.preview} aria-live="polite"><small>计算截至 {result.asOf}</small>
      {(['before','after'] as const).map(k=><div key={k}><strong>{k==='before'?'继承口径':'本格口径'} · {resultText(result[k])}</strong>
        <div>{result[k].start} 至 {result[k].end}</div><small>匹配 {result[k].matched ?? '—'} 条</small></div>)}</div>}
    {error && <p role="alert" className={styles.error}>{error}</p>}
    <div className={styles.actions}><Button disabled={!validMapping(effective)} onClick={()=>void calculate(false)}>{busy?'计算中…':'预览修改前后'}</Button>
      <Button variant="primary" disabled={!validMapping(effective)} onClick={()=>void calculate(true)}>仅应用到 {address}</Button></div>
  </fieldset>
}
