import { isBojunColumnAllowed } from '../../bojunQueryConfig.js'
import type { BojunColumn, BojunTable, ReportQueryRelation, ReportQuerySpec } from '../../types'
import { appendDictionaryDefaultColumns, newWorkspaceRelation } from './tableWorkspace.js'

type DictionaryTable = { table: BojunTable; columns: BojunColumn[] }
type DictionaryMetadata = Record<string, DictionaryTable>
type KeyBinding = ReportQueryRelation['keyBindings'][number]
export type ReportReferenceStep = { sourceTableId: string; sourceColumnId: string; target: DictionaryTable }

function aliasTableId(spec: ReportQuerySpec, aliasId: string): string {
  const id = aliasId === spec.base.aliasId ? spec.base.tableId : spec.relations.find((relation) => relation.id === aliasId)?.targetTableId
  if (!id) throw new Error('来源业务表已变化，请返回配置后重新查看关联。')
  return id
}

function aliasDepth(spec: ReportQuerySpec, aliasId: string, visited = new Set<string>()): number {
  if (aliasId === spec.base.aliasId) return 0
  const relation = spec.relations.find((item) => item.id === aliasId)
  if (!relation || visited.has(aliasId)) throw new Error('现有关联路径不完整，请先检查业务表关系。')
  return 1 + aliasDepth(spec, relation.fromAliasId, new Set([...visited, aliasId]))
}

function availableTable(item: DictionaryTable | undefined, id: string): DictionaryTable {
  if (!item || String(item.table.id) !== id || !item.table.enabled || item.table.validationStatus !== 'VALID') throw new Error('关联路径中的业务表暂不可用，请重新读取字典。')
  return item
}

function sameKey(left: string[], right: string[]) {
  return left.length === right.length && [...left].sort().join(',') === [...right].sort().join(',')
}

function sameBindings(left: KeyBinding[], right: KeyBinding[]) {
  const key = (bindings: KeyBinding[]) => bindings.map((binding) => `${binding.sourceColumnId}:${binding.targetColumnId}`).sort().join(',')
  return left.length === right.length && key(left) === key(right)
}

function compatibleTypes(source: BojunColumn, target: BojunColumn) {
  return source.logicalType === target.logicalType || ['integer', 'decimal'].includes(source.logicalType) && ['integer', 'decimal'].includes(target.logicalType)
}

function referenceBindings(source: DictionaryTable, target: DictionaryTable, sourceColumnId: string): KeyBinding[] {
  const clicked = source.columns.find((column) => String(column.id) === sourceColumnId)
  if (!clicked || !isBojunColumnAllowed(source.table, clicked)) throw new Error('点击的关联字段不属于来源表或暂不可用。')
  if (!clicked.refTableId || clicked.refTableId !== target.table.bojunTableId || !clicked.refColumnId) throw new Error('字段关联定义与目标表不一致，请重新读取字典。')
  const matched = target.columns.find((column) => column.bojunColumnId === clicked.refColumnId)
  if (!matched || !isBojunColumnAllowed(target.table, matched)) throw new Error('目标关联字段不存在或暂不可用。')
  const verifiedKeys = target.table.uniqueKeys ?? [target.table.primaryKeys]
  const primary = verifiedKeys.find((key) => sameKey(key, target.table.primaryKeys))
  const keys = [...(primary ? [primary] : []), ...verifiedKeys.filter((key) => key !== primary)].filter((key) => key.includes(String(matched.id)) && new Set(key).size === key.length)
  if (!keys.length) throw new Error('点击的字段未指向已核验的完整唯一键，无法自动关联。')
  for (const key of keys) {
    const bindings: KeyBinding[] = []
    for (const targetColumnId of key) {
      const targetColumn = target.columns.find((column) => String(column.id) === targetColumnId && isBojunColumnAllowed(target.table, column))
      if (!targetColumn) break
      const candidates = targetColumnId === String(matched.id) ? [clicked] : source.columns.filter((column) => isBojunColumnAllowed(source.table, column) && column.refTableId === target.table.bojunTableId && column.refColumnId === targetColumn.bojunColumnId)
      if (candidates.length !== 1 || !compatibleTypes(candidates[0], targetColumn)) break
      bindings.push({ sourceColumnId: String(candidates[0].id), targetColumnId })
    }
    if (bindings.length === key.length && new Set(bindings.map((binding) => binding.sourceColumnId)).size === bindings.length) return bindings
  }
  throw new Error('关联键不完整或有多个候选字段，无法自动关联，请先补全伯俊字典。')
}

export function joinReportReferencePath(spec: ReportQuerySpec, metadata: DictionaryMetadata, sourceAliasId: string, steps: ReportReferenceStep[]): { spec: ReportQuerySpec; metadata: DictionaryMetadata; aliasId: string; addedCount: number } {
  let next = spec
  let aliasId = sourceAliasId
  let addedCount = 0
  const nextMetadata = { ...metadata }
  for (const step of steps) {
    const sourceTableId = aliasTableId(next, aliasId)
    if (sourceTableId !== step.sourceTableId) throw new Error('来源业务表与查看路径不一致，请重新查看关联。')
    const source = availableTable(nextMetadata[sourceTableId], sourceTableId)
    const targetTableId = String(step.target.table.id)
    const target = availableTable(step.target, targetTableId)
    const bindings = referenceBindings(source, target, step.sourceColumnId)
    if (aliasDepth(next, aliasId) >= 3) throw new Error('关联层级最多为 3 层，请从更上层业务表添加。')
    const existing = next.relations.find((relation) => relation.fromAliasId === aliasId && relation.targetTableId === targetTableId && sameBindings(relation.keyBindings, bindings))
    nextMetadata[targetTableId] = target
    if (existing) { aliasId = existing.id; continue }
    if (next.relations.length >= 8) throw new Error('每张报表最多关联 8 张业务表，请先移除不需要的表。')
    const relationId = `rel_${crypto.randomUUID().replace(/-/g, '').slice(0, 12)}`
    const defaults = target.table.displayFields.length ? target.table.displayFields : target.table.primaryKeys
    const relation: ReportQueryRelation = {
      ...newWorkspaceRelation(relationId, aliasId),
      name: target.table.description || target.table.logicalName,
      targetTableId,
      keyBindings: bindings,
      displayColumnIds: defaults.filter((id) => target.columns.some((column) => String(column.id) === id && isBojunColumnAllowed(target.table, column))),
      detailColumnIds: defaults.filter((id) => target.columns.some((column) => String(column.id) === id && isBojunColumnAllowed(target.table, column, true))),
    }
    next = appendDictionaryDefaultColumns({ ...next, relations: [...next.relations, relation] }, relationId, target.table, target.columns)
    aliasId = relationId
    addedCount += 1
  }
  return { spec: next, metadata: nextMetadata, aliasId, addedCount }
}
