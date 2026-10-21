import { useState } from 'react'
import { ArrowRight, Link2, Network, Table2 } from 'lucide-react'
import { Button, Dialog } from '../../../ui'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import { ReportRelationMap } from './ReportRelationMap'
import { ReportDefaultFields } from './ReportDefaultFields'
import reportDesign from '../../reportDesign.module.css'
import styles from './ReportRelationsDialog.module.css'

export function ReportRelationsDialog({ spec, metadata, initialAlias, onClose }: { spec: ReportQuerySpec; metadata: BojunMetadata; initialAlias?: string; onClose: () => void }) {
  const [selectedAlias, setSelectedAlias] = useState(initialAlias || spec.base.aliasId)
  const tables = [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId }, ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId }))]
  const active = tables.find((table) => table.aliasId === selectedAlias) ?? tables[0]
  const detail = metadata[active.tableId]
  const relation = spec.relations.find((item) => item.id === active.aliasId)
  const connections = spec.relations.filter((item) => item.id === active.aliasId || item.fromAliasId === active.aliasId)
  const outputs = spec.columns.filter((column) => (column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId) === active.aliasId)
  function label(alias: string) {
    const table = tables.find((item) => item.aliasId === alias)
    return spec.relations.find((item) => item.id === alias)?.name || metadata[table?.tableId ?? '']?.table.description || metadata[table?.tableId ?? '']?.table.logicalName || '正在读取表…'
  }
  function field(alias: string, id: string) {
    const table = tables.find((item) => item.aliasId === alias)
    return metadata[table?.tableId ?? '']?.columns.find((column) => String(column.id) === id)?.dbName || (id ? `字段 #${id}` : '尚未指定')
  }
  return <Dialog open className={`${reportDesign.surface} ${styles.dialog}`} title={<span className={styles.title}><Network aria-hidden="true" />数据关系总览<span>只读</span></span>} description="沿用当前报表的表、关联键和默认显示定义。点击节点查看详情。" closeLabel="关闭关系总览" onClose={onClose} footer={<><span className={styles.summary}>{tables.length} 张业务表 · {spec.relations.length} 条关联 · {spec.columns.length} 个输出列</span><Button onClick={onClose}>返回字段配置</Button></>}>
    <div className={styles.layout}>
      <div className={styles.diagram}><ReportRelationMap spec={spec} metadata={metadata} tables={tables} activeAlias={active.aliasId} label={label} onSelect={setSelectedAlias} readOnly /></div>
      <aside className={styles.detail} aria-label="选中业务表的关联详情">
        <header className={styles.detailHeading}><Table2 aria-hidden="true" /><div><h3>{label(active.aliasId)}</h3><code>{detail?.table.logicalName || '正在读取字典…'}</code></div></header>
        {detail ? <ReportDefaultFields table={detail.table} columns={detail.columns} selected={relation?.displayColumnIds} /> : <p className={styles.hint}>字典字段尚未读取完成。</p>}
        <section className={styles.connections}><h3><Link2 aria-hidden="true" />连接定义 <span>{connections.length}</span></h3>{connections.map((connection) => <article key={connection.id}><header><strong>{label(connection.fromAliasId)}</strong><ArrowRight aria-hidden="true" /><strong>{label(connection.id)}</strong></header><p>{connection.cardinality === 'ONE_TO_ONE' ? '一对一' : '多对一'}<span>保留来源记录</span></p><div className={styles.bindings}>{connection.keyBindings.length ? connection.keyBindings.map((binding, index) => <div key={index}><code>{field(connection.fromAliasId, binding.sourceColumnId)}</code><span>=</span><code>{field(connection.id, binding.targetColumnId)}</code></div>) : <p className={styles.hint}>关联键尚未配置</p>}</div>{connection.matchConditions.items.length ? <small>已配置关联取值限制</small> : null}</article>)}{!connections.length ? <p className={styles.hint}>这张表尚未连接其他业务表。</p> : null}</section>
        <section className={styles.outputs}><h3>当前表输出 <span>{outputs.length}</span></h3><div>{outputs.map((column) => <span key={column.fieldId}>{column.header || (column.source.kind === 'RELATION_DISPLAY' ? '默认显示值' : field(active.aliasId, column.source.columnId))}</span>)}{!outputs.length ? <p className={styles.hint}>尚未选择这张表的输出字段。</p> : null}</div></section>
      </aside>
    </div>
  </Dialog>
}
