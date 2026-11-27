import { useEffect, useState } from 'react'
import type { ReportCenterClient } from '../reportCenter/api'
import { Button } from '../ui'
import { boardRequest } from './api'
import type { Metric } from './types'
import styles from './BoardCanvas.module.css'

export function DimensionPicker({ client, metric, field, values, onChange, multiple = false }: {
  client: ReportCenterClient; metric: Metric; field: string; values: string[]; onChange:(values:string[])=>void; multiple?: boolean
}) {
  const [items, setItems] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [error, setError] = useState('')
  const [retry,setRetry] = useState(0)
  const identity = JSON.stringify(metric)
  useEffect(() => {
    const ac = new AbortController()
    setItems([]); setError('')
    if (!field) return
    const timer = setTimeout(() => {
      void boardRequest<{values:string[]}>(client, '/dimension-values','POST',{metric:JSON.parse(identity),field,search},ac.signal).then(r => {
        if (!ac.signal.aborted) setItems(r.values ?? [])
      }).catch(e => { if (!ac.signal.aborted) setError(e.message) })
    },160)
    return () => { clearTimeout(timer); ac.abort() }
  }, [client,identity,field,search,retry])
  if (!field) return <span>全部授权门店</span>
  return <div className={styles.editor}><label>搜索门店<input value={search} onChange={e => setSearch(e.target.value)}/></label>
    {multiple ? <fieldset className={styles.choices}><legend>选择门店</legend>{Array.from(new Set([...values,...items])).map(item =>
      <label key={item}><input type="checkbox" checked={values.includes(item)} onChange={e => onChange(e.target.checked ? [...values,item] : values.filter(v=>v!==item))}/>{item}</label>)}</fieldset>
      : <label>门店范围<select value={values[0] ?? ''} onChange={e => onChange(e.target.value ? [e.target.value] : [])}><option value="">全部门店</option>
        {Array.from(new Set([...values,...items])).map(item => <option key={item} value={item}>{item}</option>)}</select></label>}
    {error && <div role="alert">{error}<Button onClick={()=>setRetry(r=>r+1)}>重试</Button></div>}</div>
}
