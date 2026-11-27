import { useState } from 'react'
import type { ReportCenterClient } from '../reportCenter/api'
import { Button, Drawer } from '../ui'
import { MappingEditor } from './MappingEditor'
import { metricReferences, newMetric, validMapping } from './formula'
import type { BoardLayout, Metric } from './types'
import styles from './BoardCanvas.module.css'

export function MetricManager({client,layout,onSave,onClose}: {client: ReportCenterClient;layout: BoardLayout;onSave:(m:Metric)=>void;onClose:()=>void}) {
  const items = layout.metrics ?? []
  const [value, setValue] = useState<Metric>(() => items[0] ?? newMetric())
  const references = metricReferences(layout,value.id)
  return <Drawer open title="指标管理" size="wide" onClose={onClose} footer={<><Button onClick={onClose}>取消</Button>
    <Button variant="primary" disabled={!validMapping(value)} onClick={() => {
      if (references.length && !window.confirm('更新共享指标将影响 ' + references.length + ' 个引用格，确认应用到草稿？')) return
      onSave(value); onClose()
    }}>应用指标到草稿</Button></>}>
    <div className={styles.editor}>
      <div className={styles.actions}><select aria-label="共享指标" value={items.some(m => m.id === value.id) ? value.id : ''} onChange={e => setValue(items.find(m=>m.id===e.target.value) ?? newMetric())}>
        <option value="">新增指标</option>{items.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select>
        <Button onClick={() => setValue({...newMetric(),name:items.length ? '新指标' : 'GMV'})}>新增指标</Button></div>
      <label>指标名称<input value={value.name} maxLength={100} onChange={e => setValue({...value,name:e.target.value})}/></label>
      <MappingEditor key={value.id} client={client} value={value} onChange={setValue}/>
      <div><strong>受影响的单元格 · {references.length}</strong><p>{references.join('、') || '当前还没有单元格引用该指标。'}</p></div>
    </div>
  </Drawer>
}
