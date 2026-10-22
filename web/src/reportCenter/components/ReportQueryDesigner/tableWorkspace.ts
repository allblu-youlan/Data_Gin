import { isBojunColumnAllowed, newReportQueryColumn } from '../../bojunQueryConfig.js'
import type { BojunColumn, BojunTable, ReportQueryCondition, ReportQueryConditionGroup, ReportQueryRelation, ReportQuerySpec } from '../../types'

export function tableDescendants(spec: ReportQuerySpec, aliasId: string) {
  const aliases = new Set([aliasId])
  let changed = true
  while (changed) {
    changed = false
    for (const relation of spec.relations) {
      if (aliases.has(relation.fromAliasId) && !aliases.has(relation.id)) {
        aliases.add(relation.id)
        changed = true
      }
    }
  }
  return aliases
}

export function relationSourceAliases(spec: ReportQuerySpec, relationId: string) {
  const excluded = tableDescendants(spec, relationId)
  return [spec.base.aliasId, ...spec.relations.filter((relation) => relation.targetTableId).map((relation) => relation.id)].filter((alias) => !excluded.has(alias))
}

function pruneGroup(group: ReportQueryConditionGroup, aliases: Set<string>): ReportQueryConditionGroup {
  return { ...group, items: group.items.flatMap((item): Array<ReportQueryCondition | ReportQueryConditionGroup> => {
    if ('logic' in item) {
      const next = pruneGroup(item, aliases)
      return next?.items.length ? [next] : []
    }
    return aliases.has(item.aliasId) || (item.value?.kind === 'FIELD' && aliases.has(item.value.aliasId)) ? [] : [item]
  }) }
}

export function removeReportTables(spec: ReportQuerySpec, aliasId: string) {
  const aliases = tableDescendants(spec, aliasId)
  const clear = aliases.has(spec.base.aliasId)
  const query: ReportQuerySpec = {
    ...spec,
    base: clear ? { ...spec.base, tableId: '' } : spec.base,
    relations: spec.relations.filter((relation) => !aliases.has(relation.id)).map((relation) => ({ ...relation, matchConditions: pruneGroup(relation.matchConditions, aliases) })),
    columns: spec.columns.filter((column) => !aliases.has(column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId)),
    where: pruneGroup(spec.where, aliases),
    orderBy: spec.orderBy.filter((order) => !aliases.has(order.aliasId)),
    allowedFilters: spec.allowedFilters?.filter((filter) => !aliases.has(filter.aliasId)),
    allowedSortFields: spec.allowedSortFields?.filter((order) => !aliases.has(order.aliasId)),
  }
  return { query, tableCount: aliases.size, columnCount: spec.columns.length - query.columns.length }
}

export function newWorkspaceRelation(id: string, fromAliasId: string): ReportQueryRelation {
  return { id, name: '', fromAliasId, targetTableId: '', keyBindings: [], cardinality: 'MANY_TO_ONE', joinType: 'LEFT', displayColumnIds: [], detailColumnIds: [], matchConditions: { logic: 'AND', items: [] } }
}

export function appendDictionaryDefaultColumns(spec: ReportQuerySpec, aliasId: string, table: BojunTable, columns: BojunColumn[]): ReportQuerySpec {
  const tableId = aliasId === spec.base.aliasId ? spec.base.tableId : spec.relations.find((relation) => relation.id === aliasId)?.targetTableId
  if (tableId !== String(table.id) || !table.enabled || table.validationStatus !== 'VALID') return spec
  const defaults = table.displayFields.length ? table.displayFields : table.primaryKeys
  const selected = new Set(spec.columns.flatMap((column) => {
    const source = column.source
    return source.kind !== 'RELATION_DISPLAY' && (source.kind === 'BASE_COLUMN' ? spec.base.aliasId : source.relationId) === aliasId ? [source.columnId] : []
  }))
  const additions = defaults.flatMap((id) => { const column = columns.find((item) => String(item.id) === id); return column && !selected.has(id) && isBojunColumnAllowed(table, column) ? [column] : [] }).map((column) => newReportQueryColumn(aliasId === spec.base.aliasId ? { kind: 'BASE_COLUMN', columnId: String(column.id) } : { kind: 'RELATION_COLUMN', relationId: aliasId, columnId: String(column.id) }, column.description || column.dbName))
  return additions.length ? { ...spec, columns: [...spec.columns, ...additions] } : spec
}


type DictionaryTable = { table: BojunTable; columns: BojunColumn[] }

export function inheritDictionaryRelationKeys(relation: ReportQueryRelation, source?: DictionaryTable, target?: DictionaryTable): ReportQueryRelation {
  if (!source || !target || !target.table.primaryKeys.length) return relation
  const primaryKeys = target.table.primaryKeys
  if (relation.keyBindings.some((binding) => !primaryKeys.includes(binding.targetColumnId))) return relation
  const inferred = primaryKeys.map((targetColumnId) => {
    const targetColumn = target.columns.find((column) => String(column.id) === targetColumnId && isBojunColumnAllowed(target.table, column))
    if (!targetColumn) return null
    const candidates = source.columns.filter((column) => isBojunColumnAllowed(source.table, column) && column.refTableId === target.table.bojunTableId && column.refColumnId === targetColumn.bojunColumnId)
    if (candidates.length !== 1) return null
    return { sourceColumnId: relation.keyBindings.find((binding) => binding.targetColumnId === targetColumnId)?.sourceColumnId || String(candidates[0].id), targetColumnId }
  })
  if (inferred.some((binding) => binding === null)) return relation
  const keyBindings = inferred.filter((binding) => binding !== null)
  return JSON.stringify(keyBindings) === JSON.stringify(relation.keyBindings) ? relation : { ...relation, keyBindings }
}
