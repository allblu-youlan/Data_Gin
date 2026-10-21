import { useState } from 'react'
import Tabs from 'antd/es/tabs'
import { Plus, Trash2 } from 'lucide-react'
import { Button, Dialog, StatusTag } from '../../../ui'
import { saveBojunTableConfiguration } from '../../bojunApi'
import type { ReportCenterClient } from '../../api'
import type { BojunColumn, BojunDataScope, BojunTable } from '../../types'
import styles from './BojunDictionaryPanel.module.css'

type ScopeRow = { key: number; userId: string; values: string }

export function BojunTableConfigurationDialog({ client, datasourceId, table, columns, onClose, onSaved }: { client: ReportCenterClient; datasourceId: number; table: BojunTable; columns: BojunColumn[]; onClose: () => void; onSaved: (table: BojunTable) => void }) {
  const [tab, setTab] = useState('display')
  const [enabled, setEnabled] = useState(table.enabled)
  const [primaryKeys, setPrimaryKeys] = useState(table.config?.primaryKeys ?? table.primaryKeys)
  const [displayFields, setDisplayFields] = useState(table.config?.displayFields ?? table.displayFields)
  const [searchFields, setSearchFields] = useState(table.config?.searchFields ?? table.searchFields)
  const [selectableColumns, setSelectableColumns] = useState(table.config?.selectableColumns ?? columns.filter((column) => column.isSelectable).map((column) => String(column.id)))
  const [detailColumns, setDetailColumns] = useState(table.config?.detailColumns ?? columns.filter((column) => column.isSelectable).map((column) => String(column.id)))
  const [overrides, setOverrides] = useState({ primary: Array.isArray(table.config?.primaryKeys), display: Array.isArray(table.config?.displayFields), search: Array.isArray(table.config?.searchFields), selectable: Array.isArray(table.config?.selectableColumns), detail: Array.isArray(table.config?.detailColumns) })
  const [mode, setMode] = useState<BojunDataScope['mode']>(table.dataScope?.mode ?? 'SCOPE_COLUMN')
  const [scopeColumnId, setScopeColumnId] = useState(table.dataScope?.columnId ?? '')
  const [approved, setApproved] = useState(table.dataScope?.mode === 'GLOBAL_APPROVED')
  const [scopeRows, setScopeRows] = useState<ScopeRow[]>(Object.entries(table.dataScope?.userValues ?? {}).map(([userId, values], index) => ({ key: index, userId, values: values.join(', ') })))
  const [state, setState] = useState({ busy: false, error: '' })
  const physical = columns.filter((column) => column.isPhysical && column.isSelectable)
  const fail = (message: string) => { setTab('scope'); setState({ busy: false, error: message }) }

  async function save() {
    if (enabled && table.validationStatus !== 'VALID') { setState({ busy: false, error: '请先重新核验此表，通过核验后才能启用。' }); return }
    if (enabled && mode === 'GLOBAL_APPROVED' && !approved) { fail('请确认批准全部记录，再启用此表。'); return }
    const userValues: Record<string, string[]> = {}
    if (mode === 'SCOPE_COLUMN') {
      for (const row of scopeRows) {
        const userId = row.userId.trim()
        const values = row.values.split(/[,，\n]/).map((value) => value.trim()).filter(Boolean)
        if (!userId && !values.length) continue
        if (!/^[1-9]\d*$/.test(userId) || !values.length) { fail('请为每条范围配置填写有效用户 ID 和允许值。'); return }
        userValues[userId] = [...new Set([...(userValues[userId] ?? []), ...values])]
      }
      if (enabled && (!scopeColumnId || !Object.keys(userValues).length)) { fail('启用前请选择范围字段，并添加至少一位用户的允许值。'); return }
    }
    setState({ busy: true, error: '' })
    const response = await saveBojunTableConfiguration(client, datasourceId, String(table.id), { ...table.config, expectedLockVersion: table.lockVersion, enabled, primaryKeys: overrides.primary ? primaryKeys : null, displayFields: overrides.display ? displayFields : null, searchFields: overrides.search ? searchFields : null, selectableColumns: overrides.selectable ? selectableColumns : null, detailColumns: overrides.detail ? detailColumns : null, dataScope: mode === 'GLOBAL_APPROVED' ? { mode } : { mode, columnId: scopeColumnId, userValues } })
    if (!response.ok) { setState({ busy: false, error: response.error }); return }
    onSaved(response.data)
  }

  function picker(label: string, type: keyof typeof overrides, value: string[], onChange: (value: string[]) => void) {
    return <ColumnPicker label={label} columns={physical} defaults={table.displayFields} value={value} onChange={onChange} overridden={overrides[type]} onOverride={(checked) => setOverrides({ ...overrides, [type]: checked })} />
  }

  return <Dialog open title={`配置 ${table.description || table.logicalName}`} description={`${table.logicalName} · 分别设置显示方式、字段权限与查询范围。`} className={styles.dialog} onClose={onClose} closeDisabled={state.busy} footer={<><Button disabled={state.busy} onClick={onClose}>取消</Button><Button variant="primary" disabled={state.busy} onClick={() => void save()}>{state.busy ? '保存中…' : '保存配置'}</Button></>}>
    <div className={styles.dialogContent}>
      <fieldset className={styles.formFields} disabled={state.busy}>
        <div className={styles.enable}><label className={styles.check}><input type="checkbox" checked={enabled} disabled={table.validationStatus !== 'VALID' && !enabled} onChange={(event) => setEnabled(event.currentTarget.checked)} />允许报表选用此表</label><StatusTag tone={table.validationStatus === 'VALID' ? 'success' : 'warning'}>{table.validationStatus === 'VALID' ? '核验通过' : '尚未通过核验'}</StatusTag></div>
        <Tabs activeKey={tab} onChange={setTab} items={[
          { key: 'display', label: '显示与搜索', children: <div className={styles.dialogContent}>{picker('默认显示字段', 'display', displayFields, setDisplayFields)}{picker('搜索字段', 'search', searchFields, setSearchFields)}<details><summary>记录定位字段（高级）</summary>{picker('记录标识字段', 'primary', primaryKeys, setPrimaryKeys)}</details></div> },
          { key: 'fields', label: '字段权限', children: <div className={styles.dialogContent}><p className={styles.scopeIntro}>报表列和筛选使用“允许查询字段”；记录详情只显示“允许详情字段”。</p>{picker('允许查询字段', 'selectable', selectableColumns, setSelectableColumns)}{picker('允许详情字段', 'detail', detailColumns, setDetailColumns)}</div> },
          { key: 'scope', label: '查询范围', children: <div className={styles.dialogContent}>
            <label className={styles.field}>记录范围<select value={mode} onChange={(event) => setMode(event.currentTarget.value === 'GLOBAL_APPROVED' ? 'GLOBAL_APPROVED' : 'SCOPE_COLUMN')}><option value="SCOPE_COLUMN">按用户限制记录</option><option value="GLOBAL_APPROVED">批准全部记录</option></select></label>
            {mode === 'GLOBAL_APPROVED' ? <><p className={styles.scopeIntro}>已获得报表权限的用户可查询此表的全部记录。</p><label className={styles.check}><input type="checkbox" checked={approved} onChange={(event) => setApproved(event.currentTarget.checked)} />确认批准全部记录供已授权报表查询</label></> : <>
              <label className={styles.field}>范围字段<select value={scopeColumnId} onChange={(event) => setScopeColumnId(event.currentTarget.value)}><option value="">请选择范围字段</option>{physical.map((column) => <option key={column.id} value={String(column.id)}>{column.description || column.dbName} · {column.dbName}</option>)}</select></label>
              <p className={styles.scopeIntro}>为用户填写允许的字段值，多个值用逗号分隔。数字编号按原文本保存。</p>
              <div className={styles.scopeRows}>{scopeRows.map((row, index) => <div className={styles.scopeRow} key={row.key}><label className={styles.field}>用户 ID<input inputMode="numeric" value={row.userId} onChange={(event) => { const userId = event.currentTarget.value; setScopeRows(scopeRows.map((item) => item.key === row.key ? { ...item, userId } : item)) }} /></label><label className={styles.field}>允许值<input placeholder="例如 1001, 1002" value={row.values} onChange={(event) => { const values = event.currentTarget.value; setScopeRows(scopeRows.map((item) => item.key === row.key ? { ...item, values } : item)) }} /></label><Button aria-label={`删除第 ${index + 1} 位用户的范围`} onClick={() => setScopeRows(scopeRows.filter((item) => item.key !== row.key))}><Trash2 aria-hidden="true" /></Button></div>)}</div>
              <div><Button onClick={() => setScopeRows([...scopeRows, { key: Math.max(-1, ...scopeRows.map((row) => row.key)) + 1, userId: '', values: '' }])}><Plus aria-hidden="true" />添加用户范围</Button></div>
            </>}
          </div> },
        ]} />
      </fieldset>
      {state.error ? <p role="alert" className={styles.error}>{state.error}</p> : null}
    </div>
  </Dialog>
}

function ColumnPicker({ label, columns, defaults, value, overridden, onChange, onOverride }: { label: string; columns: BojunColumn[]; defaults: string[]; value: string[]; overridden: boolean; onChange: (value: string[]) => void; onOverride: (value: boolean) => void }) {
  const [search, setSearch] = useState('')
  const selected = columns.filter((column) => value.includes(String(column.id)))
  const matched = columns.filter((column) => `${column.dbName} ${column.description}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  return <fieldset className={styles.picker}>
    <legend>{label}</legend>
    <div className={styles.pickerHead}><label className={styles.check}><input type="checkbox" checked={overridden} onChange={(event) => onOverride(event.currentTarget.checked)} />自定义{label}</label><span className={styles.hint}>{overridden ? `已选 ${selected.length} 个` : '继承字典默认配置'}</span></div>
    {!overridden ? <p className={styles.hint}>{selected.length ? selected.map((column) => `${column.description || column.dbName}（${column.dbName}）`).join('、') : '未指定字段'}</p> : <>
      <input type="search" aria-label={`搜索${label}`} placeholder="按名称查找字段" value={search} onChange={(event) => setSearch(event.currentTarget.value)} />
      <div className={styles.pickerList}>{matched.map((column) => { const id = String(column.id); return <label key={id} className={styles.pickerOption}><input type="checkbox" checked={value.includes(id)} onChange={() => onChange(value.includes(id) ? value.filter((current) => current !== id) : [...value, id])} /><span><span>{column.description || column.dbName}</span><code>{column.dbName}</code></span>{defaults.includes(id) ? <StatusTag tone="info" showDot={false}>当前默认显示</StatusTag> : null}</label> })}</div>
      {!matched.length ? <p className={styles.hint}>没有匹配字段。</p> : null}
      <p className={styles.hint}>全部取消会清空这项字段配置。</p>
    </>}
  </fieldset>
}
