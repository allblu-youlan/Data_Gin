import { useState } from 'react'
import { DataTable, FeedbackState, PaginationControls, Section } from '../../ui'
import { filterETLSeries, formatETLValue, type ETLSeries } from './contracts'
import styles from './ETLMonitorPage.module.css'

export function ETLSeriesTable({ series }: { series: ETLSeries[] }) {
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const filtered = filterETLSeries(series, query)
  const totalPages = Math.ceil(filtered.length / 20)
  const currentPage = Math.min(page, Math.max(1, totalPages))
  return <Section title="全部指标明细" description={`当前 ${series.length} 条指标序列，筛选后 ${filtered.length} 条。保留组件标签；耗时与桶边界单位以原始指标名和 Bento 定义为准。`} flush
    actions={<label className={styles.search}>搜索指标 / 标签<input type="search" value={query} onChange={(event) => { setQuery(event.currentTarget.value); setPage(1) }} placeholder="例如 input、jushuitan、label=" /></label>}>
    {filtered.length === 0 ? <FeedbackState kind="empty" title={series.length ? '没有匹配的指标' : '暂无可用指标序列'} /> : <>
      <DataTable minWidth={820} scrollLabel="ETL 原始指标明细"><thead><tr><th scope="col">指标名</th><th scope="col">类型</th><th scope="col">组件与标签</th><th scope="col">当前值</th><th scope="col">每秒速率</th></tr></thead><tbody>{filtered.slice((currentPage - 1) * 20, currentPage * 20).map((item) => <tr key={`${item.name}:${JSON.stringify(item.labels)}`}><td><code>{item.name}</code></td><td>{item.type}</td><td className={styles.labels}>{Object.entries(item.labels).map(([key, value]) => <span key={key}>{key}=<strong>{value}</strong></span>)}{Object.keys(item.labels).length === 0 ? '—' : null}</td><td className={styles.numeric}>{formatETLValue(item.value)}</td><td className={styles.numeric}>{formatETLValue(item.rate_per_second)}</td></tr>)}</tbody></DataTable>
      <PaginationControls page={currentPage} totalPages={totalPages} onPrevious={() => setPage(currentPage - 1)} onNext={() => setPage(currentPage + 1)} />
    </>}
  </Section>
}
