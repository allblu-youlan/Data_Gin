import { Link2, Plus, Table2 } from 'lucide-react'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import styles from './ReportQueryDesigner.module.css'

type TableNode = { aliasId: string; tableId: string }
const nodeWidth = 216
const nodeHeight = 104
const columnGap = 72
const rowGap = 24

export function ReportRelationMap({ spec, metadata, tables, activeAlias, label, onSelect, onAdd, canAdd }: { spec: ReportQuerySpec; metadata: BojunMetadata; tables: TableNode[]; activeAlias?: string; label: (alias: string) => string; onSelect: (alias: string) => void; onAdd: () => void; canAdd: boolean }) {
  const depths = new Map<string, number>([[spec.base.aliasId, 0]])
  for (let pass = 0; pass < spec.relations.length; pass++) for (const relation of spec.relations) {
    const sourceDepth = depths.get(relation.fromAliasId)
    if (sourceDepth !== undefined && !depths.has(relation.id)) depths.set(relation.id, sourceDepth + 1)
  }
  const groups = tables.reduce<Record<number, TableNode[]>>((all, table) => { const depth = depths.get(table.aliasId) ?? 0; (all[depth] ??= []).push(table); return all }, {})
  const rows = Math.max(1, ...Object.values(groups).map((group) => group.length))
  const height = rows * (nodeHeight + rowGap) - rowGap + 24
  const positions = new Map(tables.map((table) => {
    const depth = depths.get(table.aliasId) ?? 0
    const group = groups[depth]
    return [table.aliasId, { x: 20 + depth * (nodeWidth + columnGap), y: 12 + group.indexOf(table) * (nodeHeight + rowGap) + (height - 24 - (group.length * (nodeHeight + rowGap) - rowGap)) / 2 }]
  }))
  const width = Math.max(1, ...depths.values()) * (nodeWidth + columnGap) + nodeWidth + 40
  return <section className={styles.relationshipMap} aria-label="已加入的表与关系">
    <div className={styles.mapToolbar}><div><span className={styles.eyebrow}>DATA MODEL</span><h4>关系图谱 <span>{tables.length} 张表 · {spec.relations.length} 条连接</span></h4></div><button type="button" className={styles.mapAdd} disabled={!canAdd} onClick={onAdd}><Plus aria-hidden="true" />加入表</button></div>
    <div className={styles.mapScroll} tabIndex={0} role="region" aria-label="关系图谱，可滚动查看所有业务表">
      <div className={styles.mapCanvas} style={{ width, height }}>
        <svg className={styles.mapEdges} width={width} height={height} aria-hidden="true">{spec.relations.map((relation) => { const source = positions.get(relation.fromAliasId); const target = positions.get(relation.id); if (!source || !target) return null; const x1 = source.x + nodeWidth, y1 = source.y + nodeHeight / 2, x2 = target.x, y2 = target.y + nodeHeight / 2; return <g key={relation.id} data-active={activeAlias === relation.id || activeAlias === relation.fromAliasId}><path d={`M ${x1} ${y1} C ${x1 + columnGap / 2} ${y1}, ${x2 - columnGap / 2} ${y2}, ${x2} ${y2}`} /><circle cx={x2} cy={y2} r="3" /></g> })}</svg>
        {tables.map((table, index) => {
          const position = positions.get(table.aliasId)!
          const detail = metadata[table.tableId]
          const relation = spec.relations.find((item) => item.id === table.aliasId)
          const defaults = relation?.displayColumnIds.length ? relation.displayColumnIds : detail?.table.displayFields.length ? detail.table.displayFields : detail?.table.primaryKeys ?? []
          const outputCount = spec.columns.filter((column) => (column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId) === table.aliasId).length
          const available = detail?.table.enabled && detail.table.validationStatus === 'VALID'
          return <button type="button" key={table.aliasId} className={styles.tableNode} style={{ left: position.x, top: position.y, width: nodeWidth, height: nodeHeight }} data-active={activeAlias === table.aliasId} aria-pressed={activeAlias === table.aliasId} aria-label={`查看 ${label(table.aliasId)} 的字段${relation ? `，来自 ${label(relation.fromAliasId)}` : ''}`} onClick={() => onSelect(table.aliasId)}>
            <span className={styles.nodeHeading}><Table2 aria-hidden="true" /><strong>{label(table.aliasId)}</strong><span>{String(index + 1).padStart(2, '0')}</span></span>
            <code>{detail?.table.logicalName || '正在读取字典…'}</code>
            <span className={styles.nodeDefaults}>显示：{defaults.map((id) => { const field = detail?.columns.find((column) => String(column.id) === id); return field?.description || field?.dbName || id }).join(' / ') || '未指定'}</span>
            <span className={styles.nodeFooter}><span className={available ? styles.nodeAvailable : styles.nodePending}>{available ? '可用' : detail ? '待检查' : '读取中'}</span><span>{outputCount} 个输出字段</span></span>
          </button>
        })}
        {tables.length === 1 ? <div className={styles.mapNext} style={{ left: 20 + nodeWidth + columnGap, top: (height - nodeHeight) / 2 }}><Link2 aria-hidden="true" /><strong>继续连接业务数据</strong><span>字典中的关联键会自动带入</span><button type="button" disabled={!canAdd} onClick={onAdd}>连接另一张表</button></div> : null}
      </div>
    </div>
    <div className={styles.mapLegend}><span><span aria-hidden="true" />保留来源记录的连接</span><span>点击一张表，查看字段与输出</span></div>
  </section>
}
