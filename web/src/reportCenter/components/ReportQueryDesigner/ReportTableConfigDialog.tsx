import { useEffect, useRef, useState } from 'react'
import { Check, Database, Search, Table2, X } from 'lucide-react'
import { Button, Dialog, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { getBojunTables } from '../../bojunApi'
import { isBojunColumnAllowed, newReportQueryColumn } from '../../bojunQueryConfig'
import type { BojunColumn, BojunTable, ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import type { QueryFieldOption } from './ReportConditionEditor'
import { ReportRelationEditor } from './ReportRelationEditor'
import { useBojunMetadata } from './useBojunMetadata'
import { appendDictionaryDefaultColumns } from './tableWorkspace'
import reportDesign from '../../reportDesign.module.css'
import styles from './ReportTableConfigDialog.module.css'

const availableTable = (table: BojunTable) => table.enabled && ['VALID', 'PENDING'].includes(table.validationStatus)
const pageSize = 40

function selectTable(spec: ReportQuerySpec, aliasId: string, table: BojunTable): ReportQuerySpec {
  const relation = spec.relations.find((item) => item.id === aliasId)
  const tableId = String(table.id)
  if (relation) {
    if (relation.targetTableId === tableId) return spec
    return { ...spec, relations: spec.relations.map((item) => item.id === aliasId ? { ...item, name: table.description || table.logicalName, targetTableId: tableId, displayColumnIds: table.displayFields, detailColumnIds: table.displayFields.filter((id) => table.config?.detailColumns == null || table.config.detailColumns.includes(id)), keyBindings: table.primaryKeys.map((targetColumnId) => ({ sourceColumnId: '', targetColumnId })), matchConditions: { logic: 'AND', items: [] } } : item), columns: spec.columns.filter((column) => column.source.kind !== 'RELATION_COLUMN' || column.source.relationId !== aliasId) }
  }
  return spec.base.tableId === tableId ? spec : { ...spec, base: { ...spec.base, tableId }, columns: [] }
}

export function ReportTableConfigDialog({ client, datasourceId, initialSpec, aliasId, adding, initialMetadata, parameters, onClose, onSave }: { client: ReportCenterClient; datasourceId: number; initialSpec: ReportQuerySpec; aliasId: string; adding: boolean; initialMetadata: BojunMetadata; parameters: string[]; onClose: () => void; onSave: (spec: ReportQuerySpec) => void }) {
  const [spec, setSpec] = useState(() => structuredClone(initialSpec))
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [catalog, setCatalog] = useState(() => ({ items: Object.values(initialMetadata).map((item) => item.table).filter(availableTable), after: '', loading: true, error: '', syncing: false }))
  const [catalogReload, setCatalogReload] = useState(0)
  const [metadataReload, setMetadataReload] = useState(0)
  const [fieldSearch, setFieldSearch] = useState('')
  const [fieldPage, setFieldPage] = useState(0)
  const catalogRequest = useRef<AbortController | null>(null)
  const syncTimer = useRef<number | undefined>(undefined)
  const searchRef = useRef<HTMLInputElement>(null)
  const allFieldsRef = useRef<HTMLInputElement>(null)
  const relationSettingsRef = useRef<HTMLDetailsElement>(null)
  const initializedDefaults = useRef('')
  const lookup = useBojunMetadata(client, datasourceId, [spec.base.tableId, ...spec.relations.map((relation) => relation.targetTableId)], metadataReload)
  const metadata = { ...initialMetadata, ...lookup.metadata }
  const aliases = [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId }, ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId }))]
  const fields: QueryFieldOption[] = aliases.flatMap((alias) => (metadata[alias.tableId]?.columns ?? []).filter((column) => isBojunColumnAllowed(metadata[alias.tableId]?.table, column)).map((column) => ({ aliasId: alias.aliasId, columnId: String(column.id), label: `${metadata[alias.tableId]?.table.description || metadata[alias.tableId]?.table.logicalName} → ${column.dbName}`, operators: column.supportedOperators.length ? column.supportedOperators : ['EQ' as const], logicalType: column.logicalType })))
  const relation = spec.relations.find((item) => item.id === aliasId)
  const tableId = relation ? relation.targetTableId : spec.base.tableId
  const item = metadata[tableId]
  const label = relation?.name || item?.table.description || item?.table.logicalName || '业务表'
  const outputs = spec.columns.filter((column) => (column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId) === aliasId)
  const selectedIds = new Set(outputs.flatMap((column) => column.source.kind === 'RELATION_DISPLAY' ? [] : [column.source.columnId]))
  const keyword = fieldSearch.trim().toLocaleLowerCase()
  const matchingFields = (item?.columns ?? []).filter((column) => `${column.dbName} ${column.description} ${column.name}`.toLocaleLowerCase().includes(keyword))
  const page = Math.min(fieldPage, Math.max(0, Math.ceil(matchingFields.length / pageSize) - 1))
  const visibleFields = matchingFields.slice(page * pageSize, (page + 1) * pageSize)
  const usable = Boolean(item?.table.enabled && item.table.validationStatus === 'VALID' && !lookup.loading && !lookup.error)
  const selectableFields = visibleFields.filter((column) => item && isBojunColumnAllowed(item.table, column))
  const checkedCount = selectableFields.filter((column) => selectedIds.has(String(column.id))).length
  const defaultIds = item?.table.displayFields.length ? item.table.displayFields : item?.table.primaryKeys ?? []
  const displayIds = relation?.displayColumnIds.length ? relation.displayColumnIds : defaultIds

  useEffect(() => {
    if (!adding || !item || item.table.validationStatus !== 'VALID' || !item.table.enabled || initializedDefaults.current === tableId) return
    initializedDefaults.current = tableId
    setSpec((current) => appendDictionaryDefaultColumns(current, aliasId, item.table, item.columns))
  }, [adding, aliasId, item, tableId])

  useEffect(() => {
    catalogRequest.current?.abort()
    window.clearTimeout(syncTimer.current)
    const controller = new AbortController()
    catalogRequest.current = controller
    setCatalog((current) => ({ ...current, loading: true, error: '' }))
    async function load() {
      const response = await getBojunTables(client, datasourceId, { search, availableOnly: true, limit: 50 }, controller.signal)
      if (controller.signal.aborted) return
      if (!response.ok) { setCatalog((current) => ({ ...current, loading: false, error: response.error })); return }
      const items = response.data.items.filter(availableTable)
      setCatalog({ items, after: response.data.hasMore ? response.data.nextAfter : '', loading: response.data.syncing === true, syncing: response.data.syncing === true, error: '' })
      if (adding && items[0]) setSpec((current) => {
        const currentTableId = current.relations.find((item) => item.id === aliasId)?.targetTableId ?? current.base.tableId
        return currentTableId ? current : selectTable(current, aliasId, items[0])
      })
      if (response.data.syncing) syncTimer.current = window.setTimeout(() => void load(), 1500)
    }
    if (!datasourceId) { setCatalog({ items: [], after: '', loading: false, syncing: false, error: '请先选择报表 Oracle 连接。' }); return () => controller.abort() }
    const timer = window.setTimeout(() => void load(), search ? 200 : 0)
    return () => { window.clearTimeout(timer); window.clearTimeout(syncTimer.current); controller.abort(); catalogRequest.current?.abort() }
  }, [adding, aliasId, catalogReload, client, datasourceId, search])

  useEffect(() => {
    if (allFieldsRef.current) allFieldsRef.current.indeterminate = checkedCount > 0 && checkedCount < selectableFields.length
  }, [checkedCount, selectableFields.length])

  async function loadMore() {
    if (!catalog.after || catalog.loading) return
    catalogRequest.current?.abort()
    const controller = new AbortController()
    catalogRequest.current = controller
    setCatalog((current) => ({ ...current, loading: true, error: '' }))
    const response = await getBojunTables(client, datasourceId, { search, availableOnly: true, after: catalog.after, limit: 50 }, controller.signal)
    if (controller.signal.aborted) return
    if (!response.ok) { setCatalog((current) => ({ ...current, loading: false, error: response.error })); return }
    setCatalog((current) => ({ items: [...current.items, ...response.data.items.filter(availableTable)].filter((table, index, all) => all.findIndex((item) => item.id === table.id) === index), after: response.data.hasMore ? response.data.nextAfter : '', loading: response.data.syncing === true, syncing: response.data.syncing === true, error: '' }))
    if (response.data.syncing) syncTimer.current = window.setTimeout(() => setCatalogReload((value) => value + 1), 1500)
  }

  function choose(table: BojunTable) {
    if (!adding && String(table.id) !== tableId) return
    if (String(table.id) !== tableId) initializedDefaults.current = ''
    setSpec((current) => selectTable(current, aliasId, table))
    setFieldSearch(''); setFieldPage(0); setError('')
  }
  function toggleFields(columns: BojunColumn[], checked: boolean) {
    const ids = new Set(columns.map((column) => String(column.id)))
    setSpec((current) => {
      const existing = current.columns.filter((column) => (column.source.kind === 'BASE_COLUMN' ? current.base.aliasId : column.source.relationId) === aliasId)
      const selected = new Set(existing.flatMap((column) => column.source.kind === 'RELATION_DISPLAY' ? [] : [column.source.columnId]))
      if (!checked) return { ...current, columns: current.columns.filter((column) => !((column.source.kind === 'BASE_COLUMN' ? current.base.aliasId : column.source.relationId) === aliasId && column.source.kind !== 'RELATION_DISPLAY' && ids.has(column.source.columnId))) }
      return { ...current, columns: [...current.columns, ...columns.filter((column) => !selected.has(String(column.id))).map((column) => newReportQueryColumn(aliasId === current.base.aliasId ? { kind: 'BASE_COLUMN', columnId: String(column.id) } : { kind: 'RELATION_COLUMN', relationId: aliasId, columnId: String(column.id) }, column.description || column.dbName))] }
    })
  }
  function updateRelationFields(kind: 'displayColumnIds' | 'detailColumnIds', columnId: string, checked: boolean) {
    setSpec((current) => ({ ...current, relations: current.relations.map((item) => item.id === aliasId ? { ...item, [kind]: checked ? [...new Set([...(kind === 'displayColumnIds' ? displayIds : item.detailColumnIds), columnId])] : (kind === 'displayColumnIds' ? displayIds : item.detailColumnIds).filter((id) => id !== columnId) } : item) }))
  }
  function save() {
    if (!usable) { setError(lookup.error || '请等待字典核验完成，再选择可用表。'); return }
    if (relation && (!relation.fromAliasId || !relation.keyBindings.length || relation.keyBindings.some((binding) => !binding.sourceColumnId || !binding.targetColumnId))) {
      setError('请在“关联与取值设置”中补齐关联键。')
      const settings = relationSettingsRef.current
      if (settings) { settings.open = true; Array.from(settings.querySelectorAll<HTMLSelectElement>('select')).find((select) => !select.value)?.focus() }
      return
    }
    onSave(relation ? { ...spec, columns: spec.columns.map((column) => column.source.kind === 'RELATION_DISPLAY' && column.source.relationId === aliasId && adding ? { ...column, header: label } : column) } : spec)
  }
  const fieldNames = (ids: string[]) => ids.map((id) => item?.columns.find((column) => String(column.id) === id)?.dbName || `#${id}`).join('、') || '未定义'
  return <Dialog open className={`${reportDesign.surface} ${styles.dialog}`} title={<span className={styles.dialogTitle}><Table2 aria-hidden="true" />{adding ? '选择业务表与字段' : `配置业务表 · ${label}`}</span>} description="可用表直接展示；主键、默认显示与关联定义沿用伯俊字典，确认后应用到当前报表。" closeLabel="关闭表配置" initialFocusRef={searchRef} onClose={onClose} footer={<><div className={styles.footerSummary} aria-live="polite"><strong>{item ? label : '正在读取业务表'}</strong><span>当前表 {outputs.length} 个输出字段 · 报表共 {spec.columns.length} 列</span></div><Button onClick={onClose}>取消</Button><Button variant="primary" disabled={!usable} onClick={save}>{adding ? '确认加入报表' : '应用配置'}</Button></>}>
    <div className={styles.contextBar}><ol><li className={styles.current}><span>1</span>选择业务表</li><li><span>2</span>确认字段与关联</li><li><span>3</span>应用到报表</li></ol><span><Database aria-hidden="true" />沿用报表 Oracle 连接</span></div>
    <div className={styles.layout}>
      <aside className={styles.catalog} aria-label="可用业务表目录">
        <div className={styles.catalogToolbar}><label className={styles.search}><Search aria-hidden="true" /><input ref={searchRef} name="bojun-table-search" type="search" value={search} autoComplete="off" spellCheck={false} aria-label="搜索可用业务表" placeholder="搜索业务名称或表名…" onChange={(event) => setSearch(event.currentTarget.value)} /></label><Button aria-label="刷新可用业务表" disabled={catalog.loading} onClick={() => setCatalogReload((value) => value + 1)}>刷新</Button></div>
        <div className={styles.catalogHeading}><strong>可用业务表</strong><span>{catalog.items.length} 张已加载</span></div>
        <div className={styles.listHeading}><span>业务表 / 字典名称</span><span>字典状态</span></div>
        <div className={styles.tableList}>
          {catalog.loading ? <p className={styles.loading} role="status">{catalog.syncing ? '正在同步伯俊字典…' : '正在读取可用业务表…'}</p> : null}
          {catalog.error ? <div className={styles.catalogError} role="alert">{catalog.error}<Button onClick={() => setCatalogReload((value) => value + 1)}>重新读取</Button></div> : null}
          {!catalog.loading && !catalog.error && !catalog.items.length ? <FeedbackState kind="empty" title="没有匹配的可用表" description="调整业务名称或表名后重新搜索。" /> : null}
          {catalog.items.map((table) => <button type="button" className={styles.tableRow} data-active={String(table.id) === tableId} aria-pressed={String(table.id) === tableId} disabled={!adding && String(table.id) !== tableId} onClick={() => choose(table)} key={table.id}><span className={styles.selection}>{String(table.id) === tableId ? <Check aria-hidden="true" /> : null}</span><span className={styles.tableCopy}><strong>{table.description || table.logicalName}</strong><code>{table.logicalName}</code></span><span className={styles.tableStatus}>{table.validationStatus === 'VALID' ? '可用' : '待读取'}</span></button>)}
          {catalog.after ? <div className={styles.loadMore}><Button onClick={() => void loadMore()} disabled={catalog.loading}>加载更多业务表</Button></div> : null}
        </div>
        <p className={styles.catalogNote}><Check aria-hidden="true" />{adding ? '点击业务表，右侧立即查看字段与默认显示。' : '已绑定的业务表保持固定，可继续调整字段与关联。'}</p>
      </aside>
      <section className={styles.detail} aria-label="所选业务表字段预览">
        <header className={styles.detailHeading}><div><span>字段预览</span><h3>{item ? label : '正在读取业务表…'}</h3><code>{item?.table.logicalName || '伯俊业务字典'}</code></div><span className={styles.detailStatus} data-ready={usable}>{lookup.loading ? '读取与核验中…' : item?.table.validationStatus === 'VALID' ? '字典可用' : '等待核验'}</span></header>
        <div className={styles.dictionary}><span>记录标识 <strong>{fieldNames(item?.table.primaryKeys ?? [])}</strong></span><span>默认显示 <strong>{fieldNames(defaultIds)}</strong></span><span><Check aria-hidden="true" />关联定义自动继承</span></div>
        {lookup.error ? <div className={styles.error} role="alert">{lookup.error}<Button onClick={() => setMetadataReload((value) => value + 1)}>重试字段读取</Button></div> : null}
        <div className={styles.fieldToolbar}><span>选择输出字段 <strong>{outputs.length}</strong></span><label className={styles.search}><Search aria-hidden="true" /><input name="bojun-field-search" type="search" autoComplete="off" value={fieldSearch} aria-label="搜索当前表字段" placeholder="查找字段…" onChange={(event) => { setFieldSearch(event.currentTarget.value); setFieldPage(0) }} /></label></div>
        <div className={styles.fieldScroll} tabIndex={0} role="region" aria-label="当前业务表字段列表">
          <table className={styles.fields}><thead><tr><th className={styles.checkboxCell}><label><input ref={allFieldsRef} type="checkbox" aria-label="选择当前页可用输出字段" checked={selectableFields.length > 0 && checkedCount === selectableFields.length} disabled={!usable || !selectableFields.length} onChange={(event) => toggleFields(selectableFields, event.currentTarget.checked)} /></label></th><th>业务名称</th><th>字段编码</th><th>类型 / 定义</th>{relation ? <><th className={styles.flagCell}>显示</th><th className={styles.flagCell}>详情</th></> : null}</tr></thead><tbody>{visibleFields.map((column) => {
            const id = String(column.id)
            const allowed = Boolean(item && isBojunColumnAllowed(item.table, column))
            const detailAllowed = Boolean(item && isBojunColumnAllowed(item.table, column, true))
            return <tr key={id}><td className={styles.checkboxCell}><label><input type="checkbox" aria-label={`选择输出字段 ${column.description || column.dbName}`} checked={selectedIds.has(id)} disabled={!usable || (!allowed && !selectedIds.has(id))} onChange={(event) => toggleFields([column], event.currentTarget.checked)} /></label></td><td>{column.description || column.dbName}</td><td><code>{column.dbName}</code></td><td><span className={styles.oracleType}>{column.oracleType || '—'}</span>{defaultIds.includes(id) ? <span className={styles.defaultBadge}>默认显示</span> : item?.table.primaryKeys.includes(id) ? <span className={styles.keyBadge}>主键</span> : !column.isPhysical ? <span className={styles.keyBadge}>虚拟字段</span> : null}</td>{relation ? <><td className={styles.flagCell}><label><input type="checkbox" aria-label={`显示字段 ${column.description || column.dbName}`} checked={displayIds.includes(id)} disabled={!usable || (!allowed && !displayIds.includes(id))} onChange={(event) => updateRelationFields('displayColumnIds', id, event.currentTarget.checked)} /></label></td><td className={styles.flagCell}><label><input type="checkbox" aria-label={`详情字段 ${column.description || column.dbName}`} checked={relation.detailColumnIds.includes(id)} disabled={!usable || (!detailAllowed && !relation.detailColumnIds.includes(id))} onChange={(event) => updateRelationFields('detailColumnIds', id, event.currentTarget.checked)} /></label></td></> : null}</tr>
          })}</tbody></table>
          {lookup.loading && !visibleFields.length ? <FeedbackState kind="loading" title="正在读取与核验表字段…" /> : null}
          {!lookup.loading && !lookup.error && item && !matchingFields.length ? <FeedbackState kind="empty" title="没有匹配的字段" description="试试字段编码或业务名称。" /> : null}
        </div>
        {matchingFields.length > pageSize ? <div className={styles.fieldPagination}><span>第 {page + 1} / {Math.ceil(matchingFields.length / pageSize)} 页 · {matchingFields.length} 个字段</span><div><Button disabled={page === 0} onClick={() => setFieldPage(page - 1)}>上一页</Button><Button disabled={(page + 1) * pageSize >= matchingFields.length} onClick={() => setFieldPage(page + 1)}>下一页</Button></div></div> : null}
        {relation ? <details ref={relationSettingsRef} className={styles.relationSettings}><summary>关联与取值设置 <span>{relation.keyBindings.length} 个关联键 · 保留来源记录</span></summary><div><ReportRelationEditor client={client} datasourceId={datasourceId} relation={relation} spec={spec} metadata={metadata} fields={fields} parameters={parameters} tableLocked showTableSelection={false} showFields={false} onChange={(next) => setSpec((current) => ({ ...current, relations: current.relations.map((item) => item.id === aliasId ? next : item) }))} onAppend={(columnId) => { const column = item?.columns.find((field) => String(field.id) === columnId); if (column) toggleFields([column], true) }} /></div></details> : null}
        <section className={styles.selectedStrip} aria-label="当前业务表已选输出字段"><header><strong>当前表已选字段</strong><span>列名与顺序可在报表中继续调整</span></header><div>{outputs.length ? outputs.map((column) => <span className={styles.chip} key={column.fieldId}>{column.source.kind === 'RELATION_DISPLAY' && adding ? `${label}（默认显示）` : column.header || '未命名字段'}<button type="button" aria-label={`移除输出字段 ${column.header || '未命名字段'}`} onClick={() => setSpec((current) => ({ ...current, columns: current.columns.filter((item) => item.fieldId !== column.fieldId) }))}><X aria-hidden="true" /></button></span>) : <span className={styles.emptySelected}>勾选上方字段，加入报表输出。</span>}</div></section>
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
      </section>
    </div>
  </Dialog>
}
