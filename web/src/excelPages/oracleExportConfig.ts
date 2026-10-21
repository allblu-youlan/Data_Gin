export type OracleExportTable = { owner: string; name: string; columnCount: number }
export type OracleExportColumn = { name: string; position: number; dataType: string; nullable: boolean }
export type OracleExportColumnDraft = OracleExportColumn & { selected: boolean; title: string }
export type OracleExportFilterOperator = 'eq' | 'neq' | 'contains' | 'gt' | 'gte' | 'lt' | 'lte' | 'empty' | 'not_empty'
export type OracleExportFilter = { field: string; op: OracleExportFilterOperator; value: string }
export type OracleExportFilterDraft = OracleExportFilter & { id: number }

export const oracleExportFilterOperators: Array<{ value: OracleExportFilterOperator; label: string }> = [
  { value: 'eq', label: '等于' }, { value: 'neq', label: '不等于' }, { value: 'contains', label: '包含' },
  { value: 'gt', label: '大于' }, { value: 'gte', label: '大于等于' }, { value: 'lt', label: '小于' },
  { value: 'lte', label: '小于等于' }, { value: 'empty', label: '为空' }, { value: 'not_empty', label: '不为空' },
]

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function list(payload: unknown, key: string): unknown[] {
  const root = record(payload)
  const data = record(root?.data) ?? root
  const items = data?.[key]
  if (!Array.isArray(items)) throw new Error('Oracle 目录响应格式不完整，请重试。')
  return items
}

export function parseOracleExportTables(payload: unknown): OracleExportTable[] {
  return list(payload, 'tables').map((value) => {
    const item = record(value)
    if (!item || typeof item.owner !== 'string' || !item.owner || typeof item.name !== 'string' || !item.name || typeof item.columnCount !== 'number') throw new Error('Oracle 表目录格式不完整，请重试。')
    return { owner: item.owner, name: item.name, columnCount: item.columnCount }
  })
}

export function parseOracleExportColumns(payload: unknown): OracleExportColumnDraft[] {
  return list(payload, 'columns').map((value) => {
    const item = record(value)
    if (!item || typeof item.name !== 'string' || !item.name || typeof item.position !== 'number' || typeof item.dataType !== 'string' || typeof item.nullable !== 'boolean') throw new Error('Oracle 字段目录格式不完整，请重试。')
    return { name: item.name, position: item.position, dataType: item.dataType, nullable: item.nullable, selected: true, title: item.name }
  }).sort((left, right) => left.position - right.position)
}

export function oracleExportFilterNeedsValue(op: OracleExportFilterOperator) {
  return op !== 'empty' && op !== 'not_empty'
}

export function buildOracleExportRequest(table: OracleExportTable, columns: OracleExportColumnDraft[], filters: OracleExportFilterDraft[]) {
  const selected = columns.filter((column) => column.selected)
  if (!selected.length) throw new Error('请至少选择一个导出字段。')
  const fieldNames = new Set(columns.map((column) => column.name))
  for (const filter of filters) {
    if (!fieldNames.has(filter.field)) throw new Error('请选择有效的筛选字段。')
    if (oracleExportFilterNeedsValue(filter.op) && !filter.value.trim()) throw new Error('请填写筛选值，空值请使用“为空”或“不为空”。')
  }
  return {
    owner: table.owner,
    tableName: table.name,
    columns: selected.map((column) => ({ field: column.name, title: column.title.trim() || column.name })),
    filters: filters.map(({ field, op, value }) => ({ field, op, value: oracleExportFilterNeedsValue(op) ? value.trim() : '' })),
  }
}
