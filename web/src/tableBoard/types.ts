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

export type BoardCell = { kind: 'text'; text: string } | { kind: 'field'; sourceId: string; field: string; row: number }
export type BoardSection = { id: string; title: string; cells: BoardCell[][] }
export type BoardLayout = { sources: BoardSource[]; sections: BoardSection[]; foreground: string; background: string }
export type BoardValues = Record<string, Record<string, string | null>[]>
export type TableBoard = { id: number; revision: number; name: string; layout: BoardLayout }

export function resolveCell(cell: BoardCell, values: BoardValues): string {
  return cell.kind === 'text' ? cell.text : values[cell.sourceId]?.[cell.row]?.[cell.field] ?? ''
}

export function newBoard(): TableBoard {
  return {
    id: 0, revision: 0, name: '新看板',
    layout: {
      foreground: '#171717', background: '#ffffff', sources: [],
      sections: [{ id: 'first', title: '表格一', cells: [[{ kind: 'text', text: '' }]] }],
    },
  }
}
