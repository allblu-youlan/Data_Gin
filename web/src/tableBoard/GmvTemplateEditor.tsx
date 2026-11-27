import { useState } from 'react'
import { Button, Drawer } from '../ui'
import type { ReportCenterClient } from '../reportCenter/api'
import { numeric, date, businessDay, type CatalogColumn } from './catalogColumn'
import { MappingEditor } from './MappingEditor'
import { newMetric } from './formula'
import { buildGmvTemplate, defaultReportDate, gmvTable, roleFields, type TemplateRoles } from './gmvTemplate'
import type { Metric, TableBoard } from './types'
import styles from './BoardCanvas.module.css'

type Role = keyof TemplateRoles
const labels: Record<Role,string> = {
  paid:'支付金额', returned:'发货后退货金额', date:'营业日期',
  channel:'二级渠道', included:'纳入统计标志', status:'销售状态',
}
function supports(role:Role, column:CatalogColumn) {
  if (role==='paid' || role==='returned') return numeric(column)
  if (role==='date') return date(column) || businessDay(column)
  return true
}
function TemplateFields({ source, columns, onApply }: {
  source: Metric; columns: CatalogColumn[]; onApply: (board:TableBoard)=>void
}) {
  const [reportDate,setReportDate]=useState(defaultReportDate)
  const [history,setHistory]=useState(false)
  const [coded,setCoded]=useState(false)
  const [mapped,setMapped]=useState<Partial<TemplateRoles>>({})
  const fields={...roleFields,...mapped}
  const selected = (role:Role) => columns.find(c=>c.name===fields[role] && supports(role,c))
  const day=selected('date')
  const needsFormat=!!day && !date(day)
  const roles=Object.keys(labels) as Role[]
  const complete=source.tableName===gmvTable && roles.every(role=>!!selected(role)) && reportDate && (!needsFormat || coded)
  function apply(){
    if (!complete) return
    const columnIds = Object.fromEntries(columns.filter(c=>c.id && Object.values(fields).includes(c.name)).map(c=>[c.name,c.id!]))
    const board=buildGmvTemplate({
      datasourceId:source.datasourceId,owner:source.owner,tableName:source.tableName,bojunTableId:source.bojunTableId,
      fields,columnIds,timeFormat:needsFormat ? 'YYYYMMDD' : undefined,
    },reportDate,history)
    onApply(board)
  }
  if (!source.tableName) return <p>请选择包含 {gmvTable} 的连接与来源表。</p>
  if (source.tableName!==gmvTable) return <p className={styles.error}>该模板需要选择 {gmvTable}。</p>
  return <div className={styles.editor}>
    <h3>字段映射</h3>
    <div className={styles.inputs}>{roles.map(role=><label key={role}>{labels[role]}<select value={selected(role)?.name ?? ''} onChange={e=>{
      setMapped({...mapped,[role]:e.target.value})
      if (role==='date') setCoded(false)
    }}><option value="">请选择授权字段</option>{columns.filter(c=>supports(role,c)).map(c=><option key={c.name} value={c.name}>
      {[c.description,c.name,c.oracleType].filter(Boolean).join(' · ')}
    </option>)}</select></label>)}</div>
    {needsFormat && <label className={styles.check}><input type="checkbox" checked={coded} onChange={e=>setCoded(e.target.checked)}/>营业日期使用 YYYYMMDD 编码</label>}
    <label>报告日期<input type="date" required value={reportDate} onChange={e=>setReportDate(e.target.value)}/></label>
    <label className={styles.check}><input type="checkbox" checked={history} onChange={e=>setHistory(e.target.checked)}/>数据源已包含去年历史数据，启用同比</label>
    <div className={styles.preview}><strong>生成前核对</strong>
      <span>当前表仅含 2026 年数据时，去年与同比列显示「待接入历史数据」。启用同比需要同一表内包含上年同期记录，字段和报告周口径须一致。</span>
      <span>支付金额按纳入统计标志为「是」汇总。发货前退货取「发货前退货」状态的支付金额，发货后退货取「正常」状态的退货金额。</span>
      <span>原表唯品会手工金额及独立历史表已改为按本表渠道聚合，需对账。第 43、63 行原渠道为空，金额按 0 保留。</span>
    </div>
    <Button variant="primary" disabled={!complete} onClick={apply}>生成总 GMV 草稿</Button>
  </div>
}
export function GmvTemplateEditor({client,onApply,onClose}:{
  client:ReportCenterClient;onApply:(board:TableBoard)=>void;onClose:()=>void
}) {
  const [source,setSource]=useState(newMetric)
  return <Drawer open title="载入总 GMV 模板" description="销售结果同步 · 一、总GMV · A4:X71" size="wide" onClose={onClose}
    footer={<Button onClick={onClose}>取消</Button>}>
    <MappingEditor client={client} value={source} onChange={setSource} initialSearch={gmvTable}>
      {columns=><TemplateFields key={[source.datasourceId,source.owner,source.tableName,source.bojunTableId].join('/')} source={source} columns={columns} onApply={onApply}/>}
    </MappingEditor>
  </Drawer>
}
