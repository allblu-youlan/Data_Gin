import { useState } from 'react'
import { Button, Drawer } from '../ui'
import type { ReportCenterClient } from '../reportCenter/api'
import { DimensionPicker } from './DimensionPicker'
import { matrix, periods, addressOf } from './formula'
import { gridWidth, resizedCells } from './grid'
import type { BoardCell, BoardLayout, BoardSection, Period } from './types'
import styles from './BoardCanvas.module.css'

function summary(cell:BoardCell | undefined) {
  if (!cell) return ''
  if (cell.kind==='text') return cell.text
  if (cell.kind==='field') return cell.field
  return periods[cell.period]+' '+(cell.filters?.map(f=>f.value).join(', ') ?? '')
}
export function MatrixEditor({client,layout,section,row,column,onSave,onClose}: {
  client:ReportCenterClient;layout:BoardLayout;section:BoardSection;row:number;column:number;onSave:(s:BoardSection)=>void;onClose:()=>void
}) {
  const metrics=layout.metrics ?? []
  const [selected,setSelected]=useState(metrics[0]?.id)
  const metric=metrics.find(m=>m.id===selected)
  const field=metric?.dimensions?.[0] ?? ''
  const [stores,setStores]=useState<string[]>([])
  const [ranges,setRanges]=useState<Period[]>(['TODAY','MONTH_TO_DATE','YEAR_TO_DATE'])
  const [replace,setReplace]=useState(false)
  const [headers,setHeaders]=useState(false)
  let changes: ReturnType<typeof matrix> = []
  let error=''
  try { if(metric) changes=matrix(layout,section,row,column,metric,field,stores,ranges,replace,headers) } catch(e){error=e instanceof Error ? e.message : ''}
  const writes=changes.filter(c=>c.apply)
  const height=Math.max(section.cells.length,row+stores.length)
  const width=Math.max(gridWidth(section),column+ranges.length)
  function apply() {
    const cells=resizedCells(section,height,width)
    writes.forEach(({r,c,cell})=>{cells[r][c]=cell})
    onSave({...section,cells});onClose()
  }
  return <Drawer open title={'批量配置 · '+section.title+' / '+addressOf(row,column)} onClose={onClose} footer={<><Button onClick={onClose}>取消</Button><Button variant="primary" disabled={!writes.length} onClick={apply}>应用 {writes.length} 格</Button></>}>
    <div className={styles.editor}><label>指标<select value={selected ?? ''} onChange={e=>{setSelected(e.target.value);setStores([])}}>{metrics.map(m=><option key={m.id} value={m.id}>{m.name}</option>)}</select></label>
      {metric && field ? <DimensionPicker multiple client={client} metric={metric} field={field} values={stores} onChange={setStores}/> : <p>请先在指标管理中选择门店维度。</p>}
      <fieldset className={styles.choices}><legend>列期间</legend>{Object.entries(periods).map(([k,v])=><label key={k}><input type="checkbox" checked={ranges.includes(k as Period)} onChange={e=>setRanges(e.target.checked ? [...ranges,k as Period] : ranges.filter(r=>r!==k))}/>{v}</label>)}</fieldset>
      <div className={styles.choices}><label><input type="checkbox" checked={replace} onChange={e=>setReplace(e.target.checked)}/>替换已有内容</label><label><input type="checkbox" checked={headers} onChange={e=>setHeaders(e.target.checked)}/>写入行列标题</label></div>
      {error && <p role="status">{error}</p>}
      {changes.length>0 && <div className={styles.preview}><strong>覆盖预览 · {height} 行 × {width} 列</strong>
        <small>已有内容 {changes.filter(x=>x.occupied).length} 格，将写入 {writes.length} 格</small>
        <ul>{changes.map(x=><li key={x.r+'-'+x.c}>{addressOf(x.r,x.c)} · {x.occupied ? summary(x.previous)+' → ' : ''}{summary(x.cell)} · {x.apply ? x.occupied?'替换':'新建' : '跳过'}</li>)}</ul></div>}
    </div>
  </Drawer>
}
