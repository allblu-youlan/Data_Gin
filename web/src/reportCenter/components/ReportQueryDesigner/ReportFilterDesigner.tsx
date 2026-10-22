import { useState } from 'react'
import { ArrowDown, ArrowUp, Check, ListFilter, Search, Trash2 } from 'lucide-react'
import { Button, DataTable } from '../../../ui'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import { moveReportFilter, reportFilterFields, reportFilterKey, setReportFilterEnabled } from './filterConfig'
import styles from './ReportFilterDesigner.module.css'

export function ReportFilterDesigner({ spec, metadata, loading, onChange }: { spec: ReportQuerySpec; metadata: BojunMetadata; loading: boolean; onChange: (spec: ReportQuerySpec) => void }) {
  const [search, setSearch] = useState('')
  const [source, setSource] = useState('')
  const [page, setPage] = useState(0)
  const fields = reportFilterFields(spec, metadata)
  const filters = spec.allowedFilters ?? []
  const selected = new Set(filters.map(reportFilterKey))
  const aliases = [...new Map(fields.map((field) => [field.aliasId, field.tableLabel])).entries()]
  const keyword = search.trim().toLocaleLowerCase()
  const shown = fields.filter((field) => (!source || field.aliasId === source) && `${field.name} ${field.columnName} ${field.tableLabel} ${field.tableName}`.toLocaleLowerCase().includes(keyword))
  const pages = Math.max(1, Math.ceil(shown.length / 30))
  const currentPage = Math.min(page, pages - 1)
  function remove(key: string) { onChange({ ...spec, allowedFilters: filters.filter((field) => reportFilterKey(field) !== key) }) }
  return <section className={styles.designer} aria-label="查询筛选字段配置">
    <header className={styles.heading}><div><h4><ListFilter aria-hidden="true" />定义可筛选字段</h4><p>勾选需要的字段，查询页自动生成输入框。筛选值在查询时填写，留空不筛选。</p></div><span className={styles.count} aria-live="polite">已选 <strong>{filters.length}</strong> 项</span></header>
    <div className={styles.layout}>
      <section className={styles.catalog} aria-label="可用筛选字段">
        <div className={styles.toolbar}><label className={styles.search}><Search aria-hidden="true" /><input name="report-filter-field-search" type="search" autoComplete="off" aria-label="搜索可筛选字段" placeholder="搜索字段名称或编码…" value={search} onChange={(event) => { setSearch(event.currentTarget.value); setPage(0) }} /></label><label className={styles.source}><span className={styles.srOnly}>筛选字段来源表</span><select value={source} onChange={(event) => { setSource(event.currentTarget.value); setPage(0) }}><option value="">全部业务表</option>{aliases.map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></label></div>
        <DataTable density="compact" minWidth={420} containerClassName={styles.fieldScroll} scrollLabel="可选查询筛选字段"><thead><tr><th>启用</th><th>字段 / 来源</th><th>数据类型</th><th>报表输出</th></tr></thead><tbody>{shown.slice(currentPage * 30, (currentPage + 1) * 30).map((field) => { const key = reportFilterKey(field); return <tr key={key} data-selected={selected.has(key)}><td><label className={styles.checkbox}><input type="checkbox" id={"report-filter-" + key} aria-label={`可筛选 ${field.tableLabel} · ${field.name}`} checked={selected.has(key)} onChange={(event) => onChange(setReportFilterEnabled(spec, field, event.currentTarget.checked))} /></label></td><td><label className={styles.identity} htmlFor={"report-filter-" + key}><strong>{field.name}</strong><span>{field.tableLabel} <code>{field.columnName}</code></span></label></td><td><span className={styles.type}>{typeLabel(field.logicalType)}</span></td><td>{field.isOutput ? <span className={styles.output}><Check aria-hidden="true" />已输出</span> : <span className={styles.muted}>可独立筛选</span>}</td></tr> })}</tbody></DataTable>
        {!shown.length ? <div className={styles.empty}>{loading ? '正在读取可筛选字段…' : !fields.length ? '先在“数据与字段”中加入业务表，即可选择筛选字段。' : '没有匹配的字段，请调整关键词或来源表。'}</div> : null}
        <footer className={styles.pagination}><span>{shown.length} 个可选字段 · 仅展示字典允许查询的字段</span>{pages > 1 ? <div><Button disabled={!currentPage} onClick={() => setPage(currentPage - 1)}>上一页</Button><span>{currentPage + 1} / {pages}</span><Button disabled={currentPage + 1 === pages} onClick={() => setPage(currentPage + 1)}>下一页</Button></div> : null}</footer>
      </section>
      <section className={styles.selection} aria-label="已定义的查询筛选字段"><header><div><h4>查询页筛选项</h4><p>按下方顺序展示，可直接修改显示名称。</p></div><span>{filters.length}</span></header>
        <ol>{filters.map((filter, index) => {
          const key = reportFilterKey(filter)
          const field = fields.find((field) => reportFilterKey(field) === key)
          const label = filter.label || field?.name || `字段 #${filter.columnId}`
          return <li key={key}><span className={styles.ordinal}>{String(index + 1).padStart(2, '0')}</span><div className={styles.selectedCopy}><label><span className={styles.srOnly}>筛选显示名称 {index + 1}</span><input name={`report-filter-label-${index}`} autoComplete="off" maxLength={128} value={label} onChange={(event) => onChange({ ...spec, allowedFilters: filters.map((item, position) => position === index ? { ...item, label: event.currentTarget.value } : item) })} /></label><small>{field ? <>{field.tableLabel} <code>{field.columnName}</code></> : loading ? '正在读取来源字段…' : '来源字段不可用，请移除后重新选择'}</small></div><div className={styles.rowActions}><Button aria-label={`上移筛选项 ${index + 1}`} disabled={!index} onClick={() => onChange(moveReportFilter(spec, index, -1))}><ArrowUp aria-hidden="true" /></Button><Button aria-label={`下移筛选项 ${index + 1}`} disabled={index + 1 === filters.length} onClick={() => onChange(moveReportFilter(spec, index, 1))}><ArrowDown aria-hidden="true" /></Button><Button aria-label={`移除筛选项 ${index + 1}`} onClick={() => remove(key)}><Trash2 aria-hidden="true" /></Button></div></li>
        })}</ol>
        {!filters.length ? <div className={styles.emptySelection}><ListFilter aria-hidden="true" /><strong>还未添加筛选项</strong><p>从左侧勾选业务字段。无需定义参数、固定值或条件分组。</p></div> : null}
        <footer><Check aria-hidden="true" /><span>查询时填写的条件同时生效，操作符根据字段类型自动提供。</span></footer>
      </section>
    </div>
  </section>
}

function typeLabel(type?: string) { return ({ string: '文本', integer: '整数', decimal: '数值', date: '日期', datetime: '日期时间', boolean: '布尔' })[type ?? ''] || type || '—' }
