export type BoardSource = {
  id: string
  name: string
  datasourceId: number
  owner: string
  tableName: string
  fields: string[]
  orderBy: string
  limit: number
}

export type BoardFilter = { field: string; op: 'EQ' | 'NE'; value: string }
export type Aggregate = 'SUM' | 'AVG' | 'COUNT' | 'COUNT_DISTINCT'
export type Mapping = {
  datasourceId: number
  owner: string
  tableName: string
  aggregate: Aggregate
  valueField: string
  timeField: string
  timeFormat?: string
  timezone?: string
  filters?: BoardFilter[]
  dimensions?: string[]
  bojunTableId?: number
  columnIds?: Record<string, number>
}
export type Metric = Mapping & { id: string; name: string }
export type Period = 'TODAY' | 'MONTH_TO_DATE' | 'YEAR_TO_DATE' | 'LAST_MONTH'
export type Formula = { kind: 'formula'; metricId: string; period: Period; format?: string; filters?: BoardFilter[]; overrides?: Partial<Mapping> }
export type BoardCell = { kind: 'text'; text: string } | { kind: 'field'; sourceId: string; field: string; row: number } | Formula
export type BoardSection = { id: string; title: string; cells: BoardCell[][] }
export type BoardLayout = { sources: BoardSource[]; metrics?: Metric[]; sections: BoardSection[]; foreground: string; background: string }
export type BoardValues = Record<string, Record<string, string | null>[]>
export type TableBoard = { id: number; revision: number; name: string; layout: BoardLayout }
export type CellResult = { display?: string; status: 'ok' | 'error' | 'null'; value: string | null; message?: string; start?: string; end?: string; matched?: number }
export type DisplayCell = CellResult & { sectionId: string; address: string; display: string }
export type Snapshot = { revision: number; asOf: string; calculationId: string; cells: DisplayCell[] }

export function resolveCell(cell: BoardCell, values: BoardValues): string {
  if (cell.kind === 'text') return cell.text
  if (cell.kind === 'field') return values[cell.sourceId]?.[cell.row]?.[cell.field] ?? ''
  return ''
}

export function newBoard(): TableBoard {
  return {
    id: 0, revision: 0, name: '新看板',
    layout: {
      foreground: '#171717', background: '#ffffff', sources: [], metrics: [],
      sections: [{ id: 'first', title: '经营概览', cells: Array.from({ length: 5 }, () => Array.from({ length: 5 }, () => ({ kind: 'text', text: '' }))) }],
    },
  }
}
