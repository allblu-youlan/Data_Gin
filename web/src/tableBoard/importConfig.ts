import { maxCells, maxColumns, maxRows, maxSections } from './grid.js'
import { periods } from './formula.js'
import type { BoardFilter, BoardLayout, BoardSource, Mapping, Metric } from './types'

type RecordValue = Record<string,unknown>
type Binding = Pick<Mapping,'owner'|'tableName'|'datasourceId'>
export type ImportedConfig = {name?:string;layout:BoardLayout}
export const maxImportBytes = 2 * 1024 * 1024

function object(value:unknown): value is RecordValue {
  return typeof value==='object' && value!==null && !Array.isArray(value)
}
function check(condition:unknown, message:string): asserts condition {
  if (!condition) throw new Error(message)
}
const strings = (v:unknown):v is string[] => Array.isArray(v) && v.every(x=>typeof x==='string')
const connection = (v:unknown) => typeof v==='number' && Number.isSafeInteger(v) && v>=0
const filters = (v:unknown):v is BoardFilter[] => Array.isArray(v) && v.every(f=>object(f) &&
  typeof f.field==='string' && (f.op==='EQ' || f.op==='NE') && typeof f.value==='string')
function checkMapping(m:RecordValue) {
  for(const key of ['owner','tableName','timeField','valueField','timeFormat','timezone','format','currency']) {
    check(m[key]===undefined || typeof m[key]==='string', '指标字段 '+key+' 必须是文本。')
  }
  check(m.datasourceId===undefined || connection(m.datasourceId),'连接编号应为非负整数。')
  check(m.aggregate===undefined || ['SUM','AVG','COUNT','COUNT_DISTINCT'].includes(String(m.aggregate)),'汇总方式无效。')
  check(m.filters===undefined || filters(m.filters),'指标过滤条件格式无效。')
  check(m.dimensions===undefined || strings(m.dimensions),'指标维度格式无效。')
  check(m.columnIds===undefined || (object(m.columnIds) && Object.values(m.columnIds).every(connection)),'目录字段编号无效。')
}
function checkBinding(v:RecordValue) {
  check(typeof v.id==='string' && v.id && typeof v.name==='string' && v.name &&
    typeof v.owner==='string' && v.owner && typeof v.tableName==='string' && v.tableName && connection(v.datasourceId),
  '指标或明细来源缺少名称、标识、连接或物理表。')
}

export function parseBoardConfig(text:string):ImportedConfig {
  check(new TextEncoder().encode(text).length<=maxImportBytes,'配置超过 2 MiB。')
  let input:unknown
  try { input=JSON.parse(text.replace(/^\uFEFF/,'')) } catch { throw new Error('JSON 格式无效。') }
  check(object(input) && object(input.layout),'配置需要包含 layout 对象。')
  const layout=input.layout
  check(input.name===undefined || typeof input.name==='string','配置名称必须是文本。')
  check(typeof layout.foreground==='string' && /^#[0-9a-f]{6}$/i.test(layout.foreground) &&
    typeof layout.background==='string' && /^#[0-9a-f]{6}$/i.test(layout.background),'请提供六位十六进制文字色和底色。')
  check(layout.reportDate===undefined || layout.reportDate==='' || (typeof layout.reportDate==='string' && /^\d{4}-\d{2}-\d{2}$/.test(layout.reportDate)),'报告日期格式应为 YYYY-MM-DD。')
  check(Array.isArray(layout.sources) && layout.sources.length<=16,'sources 应为最多 16 项的数组。')
  for(const source of layout.sources) {
    check(object(source),'明细来源格式无效。');checkBinding(source)
    check(strings(source.fields) && typeof source.orderBy==='string' && Number.isInteger(source.limit),'明细来源字段、排序或条数无效。')
  }
  const metrics=layout.metrics ?? []
  check(Array.isArray(metrics) && metrics.length<=128,'metrics 应为最多 128 项的数组。')
  for(const m of metrics) {
    check(object(m),'指标格式无效。');checkBinding(m);checkMapping(m)
    check(typeof m.aggregate==='string' && typeof m.timeField==='string','指标需要汇总方式与时间字段。')
  }
  const metricIds = new Set(metrics.map(m=>m.id))
  const sourceIds = new Set(layout.sources.map(s=>s.id))
  check(Array.isArray(layout.sections) && layout.sections.length && layout.sections.length<=maxSections,'请提供 1 至 20 个区块。')
  let count=0
  for(const s of layout.sections) {
    check(object(s) && typeof s.id==='string' && s.id && typeof s.title==='string' && s.title,'区块缺少标识或标题。')
    check(Array.isArray(s.cells) && s.cells.length && s.cells.length<=maxRows && Array.isArray(s.cells[0]),'区块行数无效。')
    const width=s.cells[0].length
    check(width>0 && width<=maxColumns,'区块列数无效。')
    count+=s.cells.length*width
    check(count<=maxCells,'整板超过 10000 格。')
    for(const row of s.cells) {
      check(Array.isArray(row) && row.length===width,'同一区块每一行的列数须一致。')
      for(const c of row) {
        check(object(c),'单元格格式无效。')
        switch(c.kind) {
          case 'text': check(typeof c.text==='string','文本格内容无效。');break
          case 'field': check(sourceIds.has(c.sourceId) && typeof c.field==='string' && Number.isInteger(c.row),'明细格绑定无效。');break
          case 'expression': check(typeof c.expression==='string' && c.expression.trim(),'格间公式为空。');break
          case 'formula':
            check(metricIds.has(c.metricId) && typeof c.period==='string' && Object.prototype.hasOwnProperty.call(periods,c.period),'指标公式的指标引用或期间无效。')
            check(c.period!=='FILTER_ONLY' || (c.yearOffset ?? 0)===0,'按业务过滤条件统计的单格不能同时设置年度偏移。')
            check(c.filters===undefined || filters(c.filters),'单格过滤条件格式无效。')
            if(c.overrides!==undefined) {check(object(c.overrides),'单格覆盖格式无效。');checkMapping(c.overrides)}
            break
          default: throw new Error('未知的单元格类型。')
        }
      }
    }
  }
  return {name:typeof input.name==='string' ? input.name.trim() : undefined,layout:layout as BoardLayout}
}

export const tableIdentity = ({owner,tableName}:Binding) => owner.toUpperCase()+'.'+tableName.toUpperCase()

export function boardBindings(layout:BoardLayout):Binding[] {
  const metrics=new Map((layout.metrics ?? []).map(m=>[m.id,m]))
  const items:Binding[]=[...layout.sources,...metrics.values()]
  for(const section of layout.sections) for(const line of section.cells) for(const cell of line) {
    if(cell.kind==='formula' && cell.overrides) {
      const m=metrics.get(cell.metricId)
      if (m) items.push({...m,...cell.overrides})
    }
  }
  const unique=new Map(items.map(item=>[tableIdentity(item)+'/'+item.datasourceId,item]))
  return [...unique.values()]
}

export function currentConnectionChoices(layout:BoardLayout):Map<string,number> {
  const groups = new Map<string,Set<number>>()
  for (const binding of boardBindings(layout)) {
    if (!binding.datasourceId) continue
    const key=tableIdentity(binding)
    const ids=groups.get(key) ?? new Set()
    ids.add(binding.datasourceId)
    groups.set(key,ids)
  }
  return new Map([...groups].filter(([,ids])=>ids.size===1).map(([key,ids])=>[key,[...ids][0]]))
}

export function importedLayout(layout:BoardLayout, selected:ReadonlyMap<string,number>):BoardLayout {
  function bind<T extends Binding>(binding:T):T {
    if(binding.datasourceId) return binding
    const datasourceId=selected.get(tableIdentity(binding))
    if(!datasourceId) throw new Error('请选择 '+tableIdentity(binding)+' 的连接。')
    return {...binding,datasourceId}
  }
  const metrics=layout.metrics?.map(m=>bind<Metric>(m))
  const bases=new Map((layout.metrics ?? []).map(m=>[m.id,m]))
  const resolved=new Map((metrics ?? []).map(m=>[m.id,m]))
  return {...layout,metrics,sources:layout.sources.map(s=>bind<BoardSource>(s)),sections:layout.sections.map(section=>({...section,
    cells:section.cells.map(row=>row.map(cell=>{
      if(cell.kind!=='formula' || !cell.overrides) return cell
      const base=bases.get(cell.metricId)!
      const effective={...base,...cell.overrides}
      if(effective.datasourceId) return cell
      const id=bind(effective).datasourceId
      if(cell.overrides.datasourceId===undefined && id===resolved.get(cell.metricId)?.datasourceId) return cell
      return {...cell,overrides:{...cell.overrides,datasourceId:id}}
    }))
  }))}
}
