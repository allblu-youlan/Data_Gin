import type { ReportQueryCondition, ReportQueryConditionGroup, ReportQueryRelation, ReportQuerySpec } from '../../types'

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
