import { useBojunMetadata } from '../ReportQueryDesigner/useBojunMetadata'
import { useEffect, useState } from 'react'
import { Database, RefreshCw } from 'lucide-react'
import { Button, DataTable, FeedbackState, StatusTag } from '../../../ui'
import { getBojunSync, saveBojunTableConfiguration, startBojunSync } from '../../bojunApi'
import type { ReportCenterClient } from '../../api'
import type { BojunColumn, BojunDataScope, BojunSyncRun, BojunTable, ReportDatasource } from '../../types'
import { BojunTableSelect } from '../ReportQueryDesigner/BojunMetadata'
import styles from '../ReportQueryDesigner/ReportQueryDesigner.module.css'

export function BojunDictionaryPanel({ client, datasources }: { client: ReportCenterClient; datasources: ReportDatasource[] }) {
  const [datasourceId, setDatasourceId] = useState(0)
  const [tableId, setTableId] = useState('')
  const [selected, setSelected] = useState<BojunTable | null>(null)
  const [reload, setReload] = useState(0)
  const { metadata, loading, error } = useBojunMetadata(client, datasourceId, [tableId], reload)
  const item = metadata[tableId]
  const [sync, setSync] = useState<BojunSyncRun | null>(null)
  const [operation, setOperation] = useState({ busy: false, error: '', notice: '' })
  useEffect(() => {
    if (!sync || !datasourceId || !['QUEUED', 'RUNNING', 'PENDING'].includes(sync.status)) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void getBojunSync(client, datasourceId, sync.syncRunId, controller.signal).then((response) => {
        if (controller.signal.aborted) return
        if (!response.ok) { setOperation({ busy: false, error: response.error, notice: '' }); return }
        setSync(response.data)
        if (response.data.status === 'SUCCEEDED' || response.data.status === 'SUCCESS') setReload((value) => value + 1)
      })
    }, 1500)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [client, datasourceId, sync])
  async function synchronize(single = false) {
    if (!datasourceId || operation.busy) return
    setOperation({ busy: true, error: '', notice: '' })
    const response = await startBojunSync(client, datasourceId, single ? tableId : undefined)
    if (!response.ok) { setOperation({ busy: false, error: response.error, notice: '' }); return }
    setSync(response.data); setOperation({ busy: false, error: '', notice: '' }); setReload((value) => value + 1)
  }
  return <div className={styles.editor}>
    <div className={styles.form}><label className={styles.field}>Oracle 数据源<select value={datasourceId || ''} onChange={(event) => { setDatasourceId(Number(event.currentTarget.value)); setTableId(''); setSelected(null); setSync(null); setOperation({ busy: false, error: '', notice: '' }) }}><option value="">请选择数据源</option>{datasources.map((source) => <option value={source.id} key={source.id} disabled={!source.enabled}>{source.name} · {source.code}</option>)}</select></label><div className={styles.actions}><Button variant="primary" disabled={!datasourceId || operation.busy || Boolean(sync && ['QUEUED', 'RUNNING', 'PENDING'].includes(sync.status))} onClick={() => void synchronize()}><Database aria-hidden="true" />同步可用表目录</Button></div><div className={`${styles.field} ${styles.wide}`}><span>表目录（固定可用标志 Y）</span><BojunTableSelect key={`${datasourceId}-${reload}`} client={client} datasourceId={datasourceId} value={tableId} selected={item?.table ?? selected ?? undefined} onChange={(table) => { setTableId(String(table.id)); setSelected(table) }} /></div></div>
    {sync ? <p className={styles.notice} role="status">同步 #{sync.syncRunId} · {sync.status} · {sync.tableCount} 张表 / {sync.columnCount} 个字段{sync.error ? ` · ${sync.error}` : ''}</p> : null}
    {operation.error ? <p className={styles.error} role="alert">{operation.error}</p> : null}{operation.notice ? <p role="status">{operation.notice}</p> : null}
    {loading ? <FeedbackState kind="loading" title="正在读取字段目录与有效配置" /> : null}{error ? <FeedbackState kind="error" title="字典不可用" description={error} action={<button type="button" onClick={() => setReload((value) => value + 1)}>重新读取</button>} /> : null}
    {item ? <><div className={styles.header}><h3>{item.table.logicalName} · {item.table.description}</h3><div className={styles.actions}><StatusTag tone={item.table.enabled ? 'success' : 'warning'}>{item.table.enabled ? '已启用' : '未启用'}</StatusTag><button type="button" disabled={operation.busy} onClick={() => void synchronize(true)}><RefreshCw aria-hidden="true" />刷新本表</button></div></div><dl className={styles.metadata}><dt>物理映射</dt><dd>{item.table.resolvedTable || '尚未核验'}</dd><dt>校验状态</dt><dd>{item.table.validationStatus} {item.table.validationReason}</dd><dt>最后同步</dt><dd>{item.table.lastSyncedAt ? new Date(item.table.lastSyncedAt).toLocaleString('zh-CN') : '—'}</dd></dl><TableConfiguration key={`${tableId}-${reload}`} client={client} datasourceId={datasourceId} table={item.table} columns={item.columns} onSaved={() => setReload((value) => value + 1)} /><DataTable density="compact" minWidth={850} scrollLabel="伯俊表字段目录"><thead><tr><th>字段</th><th>业务名称</th><th>类型</th><th>主键 / 显示 / 搜索</th><th>引用</th><th>能力</th></tr></thead><tbody>{item.columns.map((column) => <tr key={column.id}><td><code>{column.dbName || column.name}</code></td><td>{column.description}</td><td>{column.oracleType || '—'}</td><td>{[item.table.primaryKeys.includes(String(column.id)) ? 'PK' : '', item.table.displayFields.includes(String(column.id)) ? 'DK' : '', item.table.searchFields.includes(String(column.id)) ? 'AK/搜索' : ''].filter(Boolean).join(' / ') || '—'}</td><td>{column.refTableId ? `表 #${column.refTableId} → 字段 #${column.refColumnId}` : '—'}</td><td>{!column.isPhysical ? '虚拟字段，不参与查询' : column.isSelectable ? '可选择' : '尚未批准'}</td></tr>)}</tbody></DataTable></> : null}
  </div>
}

function TableConfiguration({ client, datasourceId, table, columns, onSaved }: { client: ReportCenterClient; datasourceId: number; table: BojunTable; columns: BojunColumn[]; onSaved: () => void }) {
  const [enabled, setEnabled] = useState(table.enabled)
  const [primaryKeys, setPrimaryKeys] = useState(table.config?.primaryKeys ?? table.primaryKeys)
  const [primaryOverride, setPrimaryOverride] = useState(Array.isArray(table.config?.primaryKeys))
  const [displayOverride, setDisplayOverride] = useState(Array.isArray(table.config?.displayFields))
  const [searchOverride, setSearchOverride] = useState(Array.isArray(table.config?.searchFields))
  const [selectableOverride, setSelectableOverride] = useState(Array.isArray(table.config?.selectableColumns))
  const [detailOverride, setDetailOverride] = useState(Array.isArray(table.config?.detailColumns))
  const [selectableColumns, setSelectableColumns] = useState(table.config?.selectableColumns ?? columns.filter((column) => column.isSelectable).map((column) => String(column.id)))
  const [detailColumns, setDetailColumns] = useState(table.config?.detailColumns ?? columns.filter((column) => column.isSelectable).map((column) => String(column.id)))
  const [displayFields, setDisplayFields] = useState(table.config?.displayFields ?? table.displayFields)
  const [searchFields, setSearchFields] = useState(table.config?.searchFields ?? table.searchFields)
  const [mode, setMode] = useState<BojunDataScope['mode']>(table.dataScope?.mode ?? 'SCOPE_COLUMN')
  const [scopeColumnId, setScopeColumnId] = useState(table.dataScope?.columnId ?? '')
  const [scopeText, setScopeText] = useState(Object.entries(table.dataScope?.userValues ?? {}).map(([userId, values]) => `${userId}:${values.join(',')}`).join('\n'))
  const [approved, setApproved] = useState(table.dataScope?.mode === 'GLOBAL_APPROVED')
  const [state, setState] = useState({ busy: false, error: '' })
  async function save() {
    if (enabled && mode === 'GLOBAL_APPROVED' && !approved) { setState({ busy: false, error: '请明确批准该表可供已授权报表查询全部记录。' }); return }
    const userValues: Record<string, string[]> = {}
    if (mode === 'SCOPE_COLUMN') {
      for (const line of scopeText.split('\n').filter((line) => line.trim())) { const separator = line.indexOf(':'); const userId = line.slice(0, separator).trim(); const values = line.slice(separator + 1).split(',').map((value) => value.trim()).filter(Boolean); if (separator < 1 || !/^[1-9]\d*$/.test(userId) || !values.length) { setState({ busy: false, error: '授权范围每行格式为 用户ID:值1,值2，主键值保留原文本。' }); return }; userValues[userId] = values }
      if (enabled && (!scopeColumnId || !Object.keys(userValues).length)) { setState({ busy: false, error: '启用前请选择范围字段并至少配置一个用户的允许值。' }); return }
    }
    setState({ busy: true, error: '' })
    const response = await saveBojunTableConfiguration(client, datasourceId, String(table.id), { ...table.config, expectedLockVersion: table.lockVersion, enabled, primaryKeys: primaryOverride ? primaryKeys : null, displayFields: displayOverride ? displayFields : null, searchFields: searchOverride ? searchFields : null, selectableColumns: selectableOverride ? selectableColumns : null, detailColumns: detailOverride ? detailColumns : null, dataScope: mode === 'GLOBAL_APPROVED' ? { mode } : { mode, columnId: scopeColumnId, userValues } })
    if (!response.ok) { setState({ busy: false, error: response.error }); return }
    setState({ busy: false, error: '' }); onSaved()
  }
  const physical = columns.filter((column) => column.isPhysical)
  return <section className={styles.section}><h3>显示与数据范围配置</h3><div className={styles.form}><ColumnOverride label="主键字段" columns={physical} value={primaryKeys} overridden={primaryOverride} onChange={setPrimaryKeys} onOverride={setPrimaryOverride} /><ColumnOverride label="默认显示字段" columns={physical} value={displayFields} overridden={displayOverride} onChange={setDisplayFields} onOverride={setDisplayOverride} /><ColumnOverride label="搜索字段" columns={physical} value={searchFields} overridden={searchOverride} onChange={setSearchFields} onOverride={setSearchOverride} /><ColumnOverride label="允许查询字段" columns={physical} value={selectableColumns} overridden={selectableOverride} onChange={setSelectableColumns} onOverride={setSelectableOverride} /><ColumnOverride label="允许详情字段" columns={physical} value={detailColumns} overridden={detailOverride} onChange={setDetailColumns} onOverride={setDetailOverride} /><label className={styles.field}>查询批准范围<select value={mode} onChange={(event) => setMode(event.currentTarget.value === 'GLOBAL_APPROVED' ? 'GLOBAL_APPROVED' : 'SCOPE_COLUMN')}><option value="SCOPE_COLUMN">按字段限制用户记录范围</option><option value="GLOBAL_APPROVED">明确批准全部记录</option></select></label>{mode === 'GLOBAL_APPROVED' ? <label className={styles.check}><input type="checkbox" checked={approved} onChange={(event) => setApproved(event.currentTarget.checked)} />批准该表全部记录供已授权报表查询</label> : <><label className={styles.field}>范围字段<select value={scopeColumnId} onChange={(event) => setScopeColumnId(event.currentTarget.value)}><option value="">请选择</option>{physical.map((column) => <option key={column.id} value={String(column.id)}>{column.dbName}</option>)}</select></label><label className={`${styles.field} ${styles.wide}`}>用户允许值（每行 用户ID:值1,值2）<textarea rows={4} value={scopeText} placeholder="例如 12:1001,1002" onChange={(event) => setScopeText(event.currentTarget.value)} /></label></>}</div><div className={styles.actions}><label className={styles.check}><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.currentTarget.checked)} />启用此表</label><Button variant="primary" disabled={state.busy} onClick={() => void save()}>{state.busy ? '保存中…' : '保存有效配置'}</Button></div>{state.error ? <p className={styles.error} role="alert">{state.error}</p> : null}</section>
}

function ColumnOverride({ label, columns, value, overridden, onChange, onOverride }: { label: string; columns: BojunColumn[]; value: string[]; overridden: boolean; onChange: (value: string[]) => void; onOverride: (value: boolean) => void }) {
  return <div className={styles.editor}><label className={styles.check}><input type="checkbox" checked={overridden} onChange={(event) => onOverride(event.currentTarget.checked)} />覆盖{label}</label>{overridden ? <label className={styles.field}>{label}（可多选，全部取消表示显式禁用）<select multiple value={value} onChange={(event) => onChange(Array.from(event.currentTarget.selectedOptions, (option) => option.value))}>{columns.map((column) => <option key={column.id} value={String(column.id)}>{column.dbName} · {column.description}</option>)}</select></label> : <p className={styles.hint}>继承已核验目录默认配置。</p>}</div>
}
