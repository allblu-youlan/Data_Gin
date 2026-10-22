import { isBojunColumnAllowed } from '../../bojunQueryConfig.js'
import type { BojunColumn, BojunTable, ReportAllowedFilter, ReportQuerySpec } from '../../types'

export type ReportFilterField = ReportAllowedFilter & { name: string; columnName: string; tableLabel: string; tableName: string; isOutput: boolean }
type Metadata = Record<string, { table: BojunTable; columns: BojunColumn[] }>
export const reportFilterKey = (field: { aliasId: string; columnId: string }) => `${field.aliasId}:${field.columnId}`

export function reportFilterFields(spec: ReportQuerySpec, metadata: Metadata): ReportFilterField[] {
  const aliases = [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId, name: '' }, ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId, name: relation.name }))]
  return aliases.flatMap((alias) => {
    const item = metadata[alias.tableId]
    if (!item?.table.enabled || item.table.validationStatus !== 'VALID') return []
    const tableLabel = alias.name || item.table.description || item.table.logicalName
    return item.columns.filter((column) => isBojunColumnAllowed(item.table, column) && column.supportedOperators.length > 0).map((column) => {
      const columnId = String(column.id)
      const name = column.description || column.dbName
      const isOutput = spec.columns.some(({ source }) => source.kind !== 'RELATION_DISPLAY' && (source.kind === 'BASE_COLUMN' ? spec.base.aliasId : source.relationId) === alias.aliasId && source.columnId === columnId)
      return { aliasId: alias.aliasId, columnId, name, label: `${tableLabel} · ${name}`, columnName: column.dbName, tableLabel, tableName: item.table.logicalName, operators: column.supportedOperators, logicalType: column.logicalType, isOutput }
    })
  })
}

export function setReportFilterEnabled(spec: ReportQuerySpec, field: ReportFilterField, enabled: boolean): ReportQuerySpec {
  const filters = spec.allowedFilters ?? []
  const key = reportFilterKey(field)
  if (enabled && filters.some((item) => reportFilterKey(item) === key)) return spec
  const { aliasId, columnId, operators, label } = field
  return { ...spec, allowedFilters: enabled ? [...filters, { aliasId, columnId, operators: [...operators], label }] : filters.filter((item) => reportFilterKey(item) !== key) }
}

export function moveReportFilter(spec: ReportQuerySpec, index: number, offset: -1 | 1): ReportQuerySpec {
  const filters = [...(spec.allowedFilters ?? [])]
  const target = index + offset
  if (index < 0 || target < 0 || index >= filters.length || target >= filters.length) return spec
  ;[filters[index], filters[target]] = [filters[target], filters[index]]
  return { ...spec, allowedFilters: filters }
}
