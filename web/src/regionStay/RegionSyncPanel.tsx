import { useEffect, useRef, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { formatShanghaiDateTime } from '../dateTime'
import { Button, DataTable, MetricStrip, PaginationControls, Section } from '../ui'
import { parseRegionBackfill, parseRegionMissing, regionFilterBody, regionQueryPath, regionStateLabels, runRegionBackfill, type RegionBackfillBatch, type RegionFilter, type RegionMissing } from './contracts'
import styles from './RegionStayPage.module.css'

export function RegionSyncPanel({ client, filter, canManage, disabled, onUpdated }: {
  client: WorkspaceApiClient; filter: RegionFilter; canManage: boolean; disabled: boolean; onUpdated: () => void
}) {
  const [missing, setMissing] = useState<RegionMissing | null>(null)
  const [checking, setChecking] = useState(false)
  const [running, setRunning] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [progress, setProgress] = useState<RegionBackfillBatch | null>(null)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [page, setPage] = useState(1)
  const controller = useRef<AbortController | null>(null)
  const stop = useRef(false)
  useEffect(() => () => { stop.current = true; controller.current?.abort() }, [])
  async function inspect(signal: AbortSignal) {
    const response = await client(regionQueryPath(filter, '/missing'), { method: 'GET', signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 60_000 })
    if (signal.aborted) return
    if (!response.ok) throw new Error(response.error?.message || '同步状态查询失败，请重试。')
    const data = parseRegionMissing(response.data)
    if (!data) throw new Error('同步状态格式异常，请重试。')
    setMissing(data); setPage(1)
  }
  async function check() {
    controller.current?.abort()
    const current = new AbortController(); controller.current = current
    setChecking(true); setError(''); setMessage(''); setProgress(null)
    try { await inspect(current.signal) } catch (reason) {
      if (!current.signal.aborted) setError(reason instanceof Error ? reason.message : '检查失败，请重试。')
    } finally { if (!current.signal.aborted) setChecking(false) }
  }
  async function backfill(force: boolean) {
    if (!canManage || !missing || disabled || running) return
    const current = new AbortController(); controller.current = current; stop.current = false
    setRunning(true); setStopping(false); setError(''); setMessage(''); setProgress(null)
    try {
      const total = await runRegionBackfill(async cursor => {
        const response = await client('/v1/kaiyu-region-stay/backfill', { method: 'POST', body: { ...regionFilterBody(filter), force, ...(cursor ? { cursor } : {}) }, signal: current.signal,
          retry: false, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 90_000 })
        if (current.signal.aborted) throw new Error('已停止')
        if (!response.ok) throw new Error(response.error?.message || '本批补拉未完成，请检查状态后重试。')
        const batch = parseRegionBackfill(response.data)
        if (!batch) throw new Error('补拉结果格式异常，请重新检查同步状态。')
        return batch
      }, setProgress, () => stop.current)
      if (current.signal.aborted) return
      setMessage(!total.done ? '已停止后续补拉，已入库的数据保留。' : total.emptyDays || total.failedDays ? '本轮取数结束，仍有源端未返回或失败的门店日期，可稍后重试。' : '本轮取数完成。')
      onUpdated()
      await inspect(current.signal)
    } catch (reason) {
      if (!current.signal.aborted) { setError(reason instanceof Error ? reason.message : '补拉未完成，请重新检查状态。'); onUpdated() }
    } finally { if (!current.signal.aborted) { setRunning(false); setStopping(false) } }
  }
  const rows = missing?.items.filter(item => item.state !== 'success') || []
  return <Section title="区域数据同步" description="按门店和日期检查，覆盖门店的全部区域。空响应和失败不会清除历史成功明细；今天的数据可能仍在更新。" actions={<>
    <Button disabled={disabled || checking || running} onClick={() => void check()}>{checking ? '正在检查…' : '检查同步状态'}</Button>
    {canManage && <><Button disabled={disabled || checking || running || !rows.length} onClick={() => void backfill(false)}>补拉缺失日期</Button>
      <Button disabled={disabled || checking || running || !missing?.expectedDays} onClick={() => void backfill(true)}>重新取数（含已有）</Button></>}
    {running && <Button disabled={stopping} onClick={() => { stop.current = true; setStopping(true) }}>{stopping ? '当前批次结束后停止' : '停止后续补拉'}</Button>}
  </>}>
    {error && <p className={styles.error} role="alert">{error}</p>}
    {message && <p className={styles.notice} role="status">{message}</p>}
    {missing && <>
      <MetricStrip label="区域同步状态汇总" items={[
        { key: 'success', label: '已取到数据', value: missing.successDays, detail: `共 ${missing.expectedDays} 个门店日期` },
        { key: 'empty', label: '源端未返回', value: missing.emptyDays, detail: '不等于零客流' },
        { key: 'failed', label: '失败 / 未查询', value: `${missing.failedDays} / ${missing.unqueriedDays}` },
        { key: 'unbound', label: '未绑定门店', value: missing.unboundMalls, detail: '填写开域店铺 ID 后参与采集' },
      ]} />
      {rows.length > 0 && <>
        <DataTable density="compact" minWidth={780} scrollLabel="待补充区域数据的门店日期">
          <thead><tr><th scope="col">门店</th><th scope="col">日期</th><th scope="col">最近结果</th><th scope="col">最近尝试</th><th scope="col">保留的成功数据时间</th></tr></thead>
          <tbody>{rows.slice((page - 1) * 20, page * 20).map(row => <tr key={`${row.mallId}:${row.statDate}`}>
            <td>{row.mallName}</td><td>{row.statDate}</td><td>{regionStateLabels[row.state]}</td><td>{formatShanghaiDateTime(row.fetchedAt)}</td><td>{formatShanghaiDateTime(row.lastSuccessAt)}</td>
          </tr>)}</tbody>
        </DataTable>
        <PaginationControls page={page} totalPages={Math.ceil(rows.length / 20)} onPrevious={() => setPage(p => p - 1)} onNext={() => setPage(p => p + 1)} />
      </>}
    </>}
    {progress && <p className={styles.notice} role="status">已处理 {progress.processedDays} 个门店日期 · 已保存 {progress.savedDays} · 源端空数据 {progress.emptyDays} · 源端未返回门店 {progress.sourceMissingDays} · 失败 {progress.failedDays}</p>}
  </Section>
}
