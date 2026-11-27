import { useEffect, useRef, useState } from 'react'
import { getReportResultTables, getReportResultTableSchema, type ReportCenterClient } from '../reportCenter/api'
import { getBojunColumns, getBojunTable, getBojunTables } from '../reportCenter/bojunApi'
import { useReportDatasources } from '../reportCenter/useReportDatasources'
import { Button } from '../ui'
import { aggregates, connectedMapping } from './formula'
import type { Aggregate, Mapping, Metric } from './types'
import styles from './BoardCanvas.module.css'

type Table = { owner: string; name: string; id?: number; description?: string; pending?: boolean }
type Column = { name: string; oracleType: string; id?: number; description?: string }
const numeric = (c: Column) => /^(NUMBER|INTEGER|DECIMAL|NUMERIC|FLOAT|BINARY_FLOAT|BINARY_DOUBLE)(\b|\()/i.test(c.oracleType)
const date = (c: Column) => /^(DATE|TIMESTAMP)(\b|\()/i.test(c.oracleType)
const businessDay = (c: Column) => numeric(c) || /^(N?VARCHAR2?|N?CHAR)(\b|\()/i.test(c.oracleType)
const physicalName = (table: Table) => [table.owner,table.name].filter(Boolean).join('.')
const keyOf = (table: Table) => table.id ? 'bojun-' + table.id : physicalName(table)
const labelOf = (c: Column) => [c.description, c.name, c.oracleType].filter(Boolean).join(' · ')

export function MappingEditor({ client, value, onChange }: {client: ReportCenterClient; value: Metric; onChange: (metric: Metric) => void}) {
  const connections = useReportDatasources(client)
  const [managed, setManaged] = useState(!!value.bojunTableId)
  const [search, setSearch] = useState('')
  const [fieldSearch, setFieldSearch] = useState('')
  const [retry, setRetry] = useState(0)
  const [tables, setTables] = useState<Table[]>([])
  const [tableError, setTableError] = useState('')
  const [columns, setColumns] = useState<Column[]>([])
  const [columnError, setColumnError] = useState('')
  const [loading, setLoading] = useState(false)
  const [tableLoading, setTableLoading] = useState(false)
  const resolving = useRef<AbortController | null>(null)
  useEffect(() => () => resolving.current?.abort(), [])
  const { datasourceId, owner, tableName, bojunTableId } = value
  useEffect(() => {
    const controller = new AbortController()
    setTables([]); setTableError(''); setTableLoading(!!datasourceId)
    if (!datasourceId) return
    async function load() {
      // The server can return an empty authorized page with a continuation cursor.
      let after = ''
      const items: Table[] = []
      do {
        if (managed) {
          const r = await getBojunTables(client, datasourceId, { availableOnly: true, search, after }, controller.signal)
          if (!r.ok) throw new Error(r.error)
          items.push(...r.data.items.map(t => ({owner:t.owner ?? '',name:t.resolvedTable || t.logicalName,id:t.id,description:t.description,pending:!t.owner || !t.resolvedTable || t.validationStatus === 'PENDING'})))
          after = r.data.hasMore ? r.data.nextAfter : ''
        } else {
          const r = await getReportResultTables(client, datasourceId, {search, after}, controller.signal)
          if (!r.ok) throw new Error(r.error)
          items.push(...r.data.items)
          after = r.data.hasMore ? r.data.nextAfter : ''
        }
      } while (after && items.length < 100 && !controller.signal.aborted)
      if (!controller.signal.aborted) setTables(items)
    }
    const timer = setTimeout(() => void load().catch(e => { if (!controller.signal.aborted) setTableError(e.message) }).finally(() => { if (!controller.signal.aborted) setTableLoading(false) }), 160)
    return () => { clearTimeout(timer); controller.abort() }
  }, [client, datasourceId, managed, search, retry])
  useEffect(() => {
    const ac = new AbortController()
    setColumns([]); setColumnError(''); setLoading(!!tableName)
    if (!datasourceId || !tableName) return
    async function load() {
      const columns: Column[] = []
      if (bojunTableId) {
        let after = ''
        do {
          const r = await getBojunColumns(client, datasourceId, String(bojunTableId), { selectableOnly: true, after }, ac.signal)
          if (!r.ok) throw new Error(r.error)
          columns.push(...r.data.items.map(c => ({name:c.dbName || c.name, oracleType:c.oracleType, description:c.description, id:c.id})))
          after = r.data.hasMore ? r.data.nextAfter : ''
        } while (after && !ac.signal.aborted)
      } else {
        const r = await getReportResultTableSchema(client, datasourceId, {owner, name:tableName}, ac.signal)
        if (!r.ok) throw new Error(r.error)
        columns.push(...r.data.columns)
      }
      if (!ac.signal.aborted) setColumns(columns)
    }
    void load().catch(e => { if (!ac.signal.aborted) setColumnError(e.message) }).finally(() => { if (!ac.signal.aborted) setLoading(false) })
    return () => ac.abort()
  }, [client, datasourceId, owner, tableName, bojunTableId, retry])
  function update(patch: Partial<Mapping>) {
    const next = { ...value, ...patch }
    if (bojunTableId) {
      const used = [next.valueField, next.timeField, ...(next.dimensions ?? []), ...(next.filters ?? []).map(f => f.field)]
      next.columnIds = Object.fromEntries(columns.filter(c => c.id && used.includes(c.name)).map(c => [c.name,c.id!]))
    }
    onChange(next)
  }
  function source(patch: Pick<Mapping, 'owner'|'tableName'|'datasourceId'|'bojunTableId'>) {
    if (tableName && !window.confirm('更换来源将清空取值、时间、营业日格式、业务过滤及门店维度，请重新选择。')) return false
    resolving.current?.abort()
    setColumns([]); setFieldSearch('')
    onChange(connectedMapping(value, patch))
    return true
  }
  function options(items: Column[], selected?: string) {
    return <>
      <option value="">请选择字段</option>
      {selected && !items.some(c => c.name === selected) && <option value={selected} disabled>{selected} · {loading ? '加载中' : '字段失效，请重新选择'}</option>}
      {items.filter(c => c.name === selected || labelOf(c).toLowerCase().includes(fieldSearch.trim().toLowerCase())).map(c => <option key={c.name} value={c.name}>{labelOf(c)}</option>)}
    </>
  }
  async function selectTable(table:Table) {
    resolving.current?.abort()
    const controller=new AbortController()
    resolving.current=controller
    if (table.pending && table.id) {
      const response=await getBojunTable(client,datasourceId,String(table.id),controller.signal,true)
      if(controller.signal.aborted) return
      if(!response.ok) {setTableError(response.error);return}
      const detail=response.data
      if(!detail.enabled || detail.validationStatus!=='VALID' || !detail.owner || !detail.resolvedTable) {
        setTableError(detail.validationReason || '该目录项的物理表不可用，请重新选择。');return
      }
      table={...table,owner:detail.owner,name:detail.resolvedTable}
    }
    source({datasourceId,owner:table.owner,tableName:table.name,bojunTableId:table.id ?? 0})
  }
  const current = tableName ? keyOf({owner,name:tableName,id:bojunTableId}) : ''
  const coded = !!value.timeFormat
  return <div className={styles.editor}>
    <div className={styles.inputs}>
      <label>已有连接<select value={datasourceId} onChange={e => { if (source({datasourceId:Number(e.target.value),owner:'',tableName:'',bojunTableId:0})) setSearch('') }}>
        <option value={0}>请选择启用的连接</option>{connections.items.filter(item => item.enabled).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label>字段目录<select value={managed ? 'managed' : 'oracle'} onChange={e => {
        if (source({datasourceId,owner:'',tableName:'',bojunTableId:0})) { setManaged(e.target.value === 'managed'); setSearch('') }
      }}><option value="oracle">普通 Oracle</option><option value="managed">伯俊受管表</option></select></label>
    </div>
    <label>搜索表<input value={search} onChange={e => setSearch(e.target.value)} placeholder="输入表名或说明"/></label>
    <label>来源表<select value={current} disabled={!datasourceId} onChange={e => {
      const table = tables.find(t => keyOf(t) === e.target.value)
      if (table) void selectTable(table)
    }}><option value="">{tableLoading ? '正在加载表目录…' : !tables.length && datasourceId ? '无匹配或无可用表' : '请选择表'}</option>
      {current && !tables.some(t => keyOf(t) === current) && <option value={current}>{current}</option>}
      {tables.map(t => <option key={t.id ?? keyOf(t)} value={keyOf(t)}>{physicalName(t)} {t.description}</option>)}</select></label>
    {(connections.error || tableError || columnError) && <div role="alert" className={styles.error}>{connections.error || tableError || columnError} <Button onClick={() => { connections.reload(); setRetry(v => v+1) }}>重试目录</Button></div>}
    {tableName && <><label>搜索字段<input value={fieldSearch} onChange={e => setFieldSearch(e.target.value)} placeholder="物理名或中文说明"/></label>
      <small>{loading ? '正在加载字段…' : columns.length + ' 个可用字段'}</small></>}
    <div className={styles.inputs}>
      <label>汇总方式<select value={value.aggregate} onChange={e => {
        const aggregate = e.target.value as Aggregate
        update({aggregate,valueField:aggregate === 'COUNT' || (['SUM','AVG'].includes(aggregate) && !columns.some(c => c.name === value.valueField && numeric(c))) ? '' : value.valueField})
      }}>{Object.entries(aggregates).map(([id,name]) => <option key={id} value={id}>{name} · {id}</option>)}</select></label>
      {value.aggregate !== 'COUNT' && <label>取值字段<select value={value.valueField ?? ''} onChange={e => update({valueField:e.target.value})}>
        {options(['SUM','AVG'].includes(value.aggregate) ? columns.filter(numeric) : columns, value.valueField)}</select></label>}
      <label>时间字段类型<select value={coded ? 'YYYYMMDD' : ''} onChange={e => update({timeFormat:e.target.value,timeField:''})}><option value="">DATE / TIMESTAMP</option><option value="YYYYMMDD">营业日编码 · YYYYMMDD</option></select></label>
      <label>时间字段<select value={value.timeField} onChange={e => update({timeField:e.target.value})}>{options(columns.filter(coded ? businessDay : date), value.timeField)}</select></label>
      <label>业务时区<input value={value.timezone ?? 'Asia/Shanghai'} onChange={e => update({timezone:e.target.value})}/></label>
      <label>门店维度<select value={value.dimensions?.[0] ?? ''} onChange={e => update({dimensions:e.target.value ? [e.target.value] : []})}>{options(columns, value.dimensions?.[0])}</select></label>
    </div>
    <fieldset className={styles.conditions}><legend>业务过滤条件</legend>
      {(value.filters ?? []).map((filter, i, filters) => <div key={i} className={styles.filterRow}>
        <select aria-label={'过滤字段 ' + (i+1)} value={filter.field} onChange={e => update({filters:filters.map((f,j) => j === i ? {...f,field:e.target.value} : f)})}>{options(columns, filter.field)}</select>
        <select aria-label="比较方式" value={filter.op} onChange={e => update({filters:filters.map((f,j) => j === i ? {...f,op:e.target.value === 'EQ' ? 'EQ' : 'NE'} : f)})}><option value="EQ">等于</option><option value="NE">不等于</option></select>
        <input aria-label="条件值" value={filter.value} onChange={e => update({filters:filters.map((f,j) => j === i ? {...f,value:e.target.value} : f)})}/>
        <Button onClick={() => update({filters:filters.filter((_,j) => j !== i)})}>移除</Button>
      </div>)}
      <Button onClick={() => update({filters:[...(value.filters ?? []),{field:'',op:'EQ',value:''}]})}>添加条件</Button>
    </fieldset>
  </div>
}
