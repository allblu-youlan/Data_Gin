import { columnName } from './clipboard.js'
import { checkGridSize, gridWidth } from './grid.js'
import type { BoardCell, BoardLayout, BoardSection, Formula, Mapping, Metric, Period } from './types'

export const periods: Record<Period, string> = { TODAY: '今日累计', MONTH_TO_DATE: '本月累计', YEAR_TO_DATE: '今年累计', LAST_MONTH: '上月全月' }
export const aggregates = { SUM: '金额求和', AVG: '平均值', COUNT: '记录计数', COUNT_DISTINCT: '订单号去重计数' }
export const addressOf = (r: number, c: number) => columnName(c) + (r + 1)
export function newMetric(): Metric {
  return { id: 'metric-' + (globalThis.crypto?.randomUUID?.() ?? Date.now()), name: 'GMV', aggregate: 'SUM', datasourceId: 0, owner: '', tableName: '', valueField: '', timeField: '', timezone: 'Asia/Shanghai' }
}
const mappingDefaults: Mapping = { datasourceId: 0, owner: '', tableName: '', aggregate: 'SUM', valueField: '', timeField: '', timeFormat: '', timezone: 'Asia/Shanghai', filters: [], dimensions: [], bojunTableId: 0, columnIds: {} }
export function differences(base: Metric, current: Mapping): Partial<Mapping> | undefined {
  const diff = Object.fromEntries((Object.keys(mappingDefaults) as (keyof Mapping)[]).flatMap(key => {
    const original = base[key] ?? mappingDefaults[key]
    const updated = current[key] ?? mappingDefaults[key]
    return JSON.stringify(original) === JSON.stringify(updated) ? [] : [[key, updated]]
  }))
  return Object.keys(diff).length ? diff : undefined
}
export function connectedMapping(metric: Metric, patch: Pick<Mapping, 'owner' | 'tableName' | 'datasourceId' | 'bojunTableId'>): Metric {
  return { ...metric, ...patch, valueField: '', timeField: '', timeFormat: '', filters: [], dimensions: [], columnIds: {} }
}
export function metricReferences(layout: BoardLayout, id: string) {
  return layout.sections.flatMap(section => section.cells.flatMap((line, r) => line.flatMap((cell, c) =>
    cell.kind === 'formula' && cell.metricId === id ? [section.title + ' / ' + addressOf(r, c) + (cell.overrides ? '（部分覆盖）' : '（继承）')] : [])))
}
export function matrix(layout: BoardLayout, section: BoardSection, row: number, col: number, metric: Metric, dimension: string, stores: string[], ranges: Period[], replace: boolean, headers: boolean) {
  if (!stores.length || !ranges.length || !dimension) throw new Error('请选择门店和期间。')
  if (headers && (!row || !col)) throw new Error('写入标题需要在起始格的上方和左侧各留一格。')
  checkGridSize(layout, section.id, Math.max(section.cells.length, row + stores.length), Math.max(gridWidth(section), col + ranges.length))
  const changes: { r: number; c: number; cell: BoardCell; previous?: BoardCell; occupied: boolean; apply: boolean }[] = []
  function add(r: number, c: number, cell: BoardCell) {
    const previous = section.cells[r]?.[c]
    const occupied = !!previous && (previous.kind !== 'text' || !!previous.text)
    changes.push({ r, c, cell, previous, occupied, apply: !occupied || replace })
  }
  if (headers) ranges.forEach((range, c) => add(row - 1, col + c, { kind: 'text', text: periods[range] }))
  stores.forEach((store, r) => {
    if (headers) add(row + r, col - 1, { kind: 'text', text: store })
    ranges.forEach((period, c) => {
      const cell: Formula = { kind: 'formula', metricId: metric.id, period, filters: [{ field: dimension, op: 'EQ', value: store }] }
      add(row + r, col + c, cell)
    })
  })
  return changes
}
