import { formatShanghaiDateTime } from '../../../dateTime.js'
import { useState, type ReactNode } from 'react'
import { ArrowUpRight, FileBarChart2, GitCompareArrows, Search, Trash2, X } from 'lucide-react'
import { Button, FeedbackState, StatusTag } from '../../../ui'
import type { ReportSummary } from '../../types'
import styles from './ReportLibrary.module.css'

type Props = {
  reports: ReportSummary[]
  loading: boolean
  loadingMore: boolean
  hasMore: boolean
  error: string
  search: string
  onSearch: (value: string) => void
  onReload: () => void
  onLoadMore: () => void
  onOpen?: (report: ReportSummary) => void
  onVersions?: (report: ReportSummary) => void
  onDelete?: (report: ReportSummary) => void
  emptyAction?: ReactNode
}

export function ReportLibrary({ reports, loading, loadingMore, hasMore, error, search, onSearch, onReload, onLoadMore, onOpen, onVersions, onDelete, emptyAction }: Props) {
  const [draftSearch, setDraftSearch] = useState(search)
  const [status, setStatus] = useState('')
  const [category, setCategory] = useState('')
  const categories = [...new Set(reports.map((report) => report.category || '未分类'))]
  const matches = reports.filter((report) => (!status || report.status === status) && (!category || (report.category || '未分类') === category))
  const hasFilters = Boolean(status || category || search)

  return <section className={styles.library} aria-label="报表列表">
    <div className={styles.toolbar}>
      <form className={styles.search} role="search" onSubmit={(event) => { event.preventDefault(); onSearch(draftSearch.trim()) }}>
        <label className={styles.searchInput}><Search aria-hidden="true" /><span className={styles.srOnly}>搜索报表名称或编码</span><input type="search" name="reportSearch" autoComplete="off" spellCheck={false} placeholder="搜索报表名称或编码…" value={draftSearch} onChange={(event) => setDraftSearch(event.currentTarget.value)} /></label>
        {draftSearch ? <Button aria-label="清空报表搜索" onClick={() => { setDraftSearch(''); onSearch('') }}><X aria-hidden="true" /></Button> : null}
        <Button type="submit" disabled={loading}>搜索</Button>
      </form>
      <div className={styles.filters}>
        <label><span className={styles.srOnly}>按发布状态筛选</span><select name="reportStatus" aria-label="按发布状态筛选" value={status} onChange={(event) => setStatus(event.currentTarget.value)}><option value="">全部状态</option><option value="ACTIVE">已发布</option><option value="DRAFT">草稿</option><option value="DISABLED">已停用</option></select></label>
        <label><span className={styles.srOnly}>按分类筛选</span><select name="reportCategory" aria-label="按分类筛选" value={category} onChange={(event) => setCategory(event.currentTarget.value)}><option value="">全部分类</option>{category && !categories.includes(category) ? <option value={category}>{category}</option> : null}{categories.map((item) => <option key={item}>{item}</option>)}</select></label>
      </div>
    </div>
    <div className={styles.listHeading}><span>报表 / 编码</span><span>分类</span><span>发布状态</span><span>最近更新</span><span>操作</span></div>
    {loading ? <FeedbackState kind="loading" title="正在读取报表…" /> : null}
    {error ? <FeedbackState kind="error" title="报表读取失败" description={error} action={<Button onClick={onReload}>重新读取</Button>} /> : null}
    {!loading && !error && !matches.length ? <FeedbackState kind="empty" title={hasFilters ? '没有符合条件的报表' : '从第一份业务报表开始'} description={hasFilters ? '调整搜索词、状态或分类，也可以继续加载更多报表。' : '关联业务表，挑选字段，保存为可重复查询的报表。'} action={hasFilters ? <Button onClick={() => { setStatus(''); setCategory(''); setDraftSearch(''); onSearch('') }}>清除筛选</Button> : emptyAction} /> : null}
    {!loading && matches.length > 0 ? <ul className={styles.list}>{matches.map((report) => <li className={styles.row} key={report.id}>
      <div className={styles.identity}><span className={styles.fileIcon}><FileBarChart2 aria-hidden="true" /></span><div>{onOpen && report.isOwner ? <button className={styles.name} type="button" onClick={() => onOpen(report)}>{report.name}<ArrowUpRight aria-hidden="true" /></button> : <strong className={styles.name}>{report.name}</strong>}<code translate="no">{report.code}</code>{report.description ? <p>{report.description}</p> : null}</div></div>
      <span className={styles.category}>{report.category || '未分类'}</span>
      <div className={styles.status}><StatusTag tone={report.status === 'ACTIVE' ? 'success' : report.status === 'DISABLED' ? 'neutral' : 'warning'}>{report.status === 'ACTIVE' ? '已发布' : report.status === 'DISABLED' ? '已停用' : '草稿'}</StatusTag><small>{report.currentPublishedVersionId ? `版本 #${report.currentPublishedVersionId}` : '待核验发布'}</small></div>
      <time className={styles.updated} dateTime={report.updatedAt || undefined}>{formatDate(report.updatedAt)}</time>
      <div className={styles.actions}>{report.isOwner && onOpen ? <Button onClick={() => onOpen(report)}>编辑报表</Button> : <span className={styles.readOnly}>共享只读</span>}{report.isOwner && onVersions ? <Button aria-label={`查看 ${report.name} 历史版本与上线`} title="版本与上线" onClick={() => onVersions(report)}><GitCompareArrows aria-hidden="true" /></Button> : null}{report.isOwner && onDelete ? <Button variant="danger" aria-label={`删除 ${report.name} 模板`} title={report.status === 'DRAFT' ? '删除草稿' : '已发布报表不能删除'} disabled={report.status !== 'DRAFT'} onClick={() => onDelete(report)}><Trash2 aria-hidden="true" /></Button> : null}</div>
    </li>)}</ul> : null}
    <footer className={styles.footer}><span aria-live="polite">已加载 <strong>{reports.length.toLocaleString('zh-CN')}</strong> 份{hasFilters ? ` · 当前显示 ${matches.length.toLocaleString('zh-CN')} 份` : ''}{hasMore ? ' · 还有更多报表' : ''}</span>{hasMore ? <Button disabled={loading || loadingMore} onClick={onLoadMore}>{loadingMore ? '正在加载…' : '加载更多报表'}</Button> : null}</footer>
  </section>
}

function formatDate(value: string | null) { return formatShanghaiDateTime(value, '-') }
