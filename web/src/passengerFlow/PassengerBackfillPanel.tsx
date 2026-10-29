import { formatShanghaiDateTime } from '../dateTime.js'
import { useEffect, useRef, useState } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { Button, MetricStrip, Section } from '../ui'
import { passengerFilterBody, passengerQueryPath, type PassengerFilter } from './contracts'
import { parsePassengerBackfill, parsePassengerMissing, runPassengerBackfill, type PassengerBackfillBatch, type PassengerMissing } from './backfillContracts'
import styles from './PassengerFlowPage.module.css'

export function PassengerBackfillPanel({ client, filter, canManage, disabled, onUpdated }: {
  client: WorkspaceApiClient; filter: PassengerFilter; canManage: boolean; disabled: boolean; onUpdated: () => void
}) {
  const [missing, setMissing] = useState<PassengerMissing | null>(null)
  const [checking, setChecking] = useState(false)
  const [running, setRunning] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [progress, setProgress] = useState<PassengerBackfillBatch | null>(null)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const controller = useRef<AbortController | null>(null)
  const stop = useRef(false)
  useEffect(() => () => { stop.current = true; controller.current?.abort() }, [])

  async function inspect(signal: AbortSignal) {
    const path = passengerQueryPath(filter).replace('/kaiyu-passenger?', '/kaiyu-passenger/missing?')
    const response = await client(path, { method: 'GET', signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 60_000 })
    if (signal.aborted) return
    if (!response.ok) throw new Error(response.error?.message || '缺失小时检查失败，请重试。')
    const data = parsePassengerMissing(response.data)
    if (!data) throw new Error('缺失小时数据格式异常，请重试。')
    setMissing(data)
  }

  async function check() {
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    setChecking(true); setError(''); setMessage(''); setProgress(null)
    try { await inspect(current.signal) } catch (reason) {
      if (!current.signal.aborted) setError(reason instanceof Error ? reason.message : '检查失败，请重试。')
    } finally { if (!current.signal.aborted) setChecking(false) }
  }

  async function backfill() {
    if (!canManage || !missing?.missingHours || disabled || running) return
    const current = new AbortController()
    controller.current = current
    stop.current = false
    setRunning(true); setStopping(false); setError(''); setMessage(''); setProgress(null)
    try {
      const total = await runPassengerBackfill(async cursor => {
        const response = await client('/v1/kaiyu-passenger/backfill', {
          method: 'POST', body: { ...passengerFilterBody(filter), ...(cursor ? { cursor } : {}) }, signal: current.signal,
          retry: false, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 90_000,
        })
        if (current.signal.aborted) throw new Error('已停止')
        if (!response.ok) throw new Error(response.error?.message || '本批补拉未完成，请检查缺失小时后重试。')
        const batch = parsePassengerBackfill(response.data)
        if (!batch) throw new Error('补拉结果格式异常，请检查缺失小时。')
        return batch
      }, setProgress, () => stop.current)
      if (current.signal.aborted) return
      setMessage(total.nextCursor ? '已停止后续补拉，已写入的数据保留。' : total.remainingHours ? '本轮补拉结束，仍有缺失小时；源端未返回的数据会保留为空，可稍后重试。' : '所选范围的缺失小时已补齐。')
      onUpdated()
      await inspect(current.signal)
    } catch (reason) {
      if (!current.signal.aborted) {
        setError(reason instanceof Error ? reason.message : '补拉未完成，请重新检查缺失小时。')
        onUpdated()
      }
    } finally { if (!current.signal.aborted) { setRunning(false); setStopping(false) } }
  }

  return <Section title="缺失小时补充" description="按当前查询范围检查已绑定门店的完整小时。已有记录自动跳过，源端未返回的数据保留为空。" actions={<>
    <Button disabled={disabled || checking || running} onClick={() => void check()}>{checking ? '正在检查…' : '检查缺失'}</Button>
    {canManage && <Button disabled={disabled || checking || running || !missing?.missingHours} onClick={() => void backfill()}>{running ? '正在补拉…' : '补齐缺失小时'}</Button>}
    {running && <Button disabled={stopping} onClick={() => { stop.current = true; setStopping(true) }}>{stopping ? '当前批次结束后停止' : '停止后续补拉'}</Button>}
  </>}>
    {error && <p className={styles.error} role="alert">{error}</p>}
    {message && <p className={styles.notice} role="status">{message}</p>}
    {missing && <>
      <MetricStrip label="缺失小时检查" items={[
        { key: 'expected', label: '应有小时记录', value: missing.expectedHours, detail: '已绑定门店 × 已结束小时' },
        { key: 'existing', label: '已有记录', value: missing.existingHours },
        { key: 'missing', label: '缺失记录', value: missing.missingHours },
        { key: 'unbound', label: '未绑定门店', value: missing.unboundMalls, detail: '未参与查缺和补拉' },
      ]} />
      {missing.malls.length > 0 && <div className={styles.gaps} aria-label="门店缺失小时">
        {missing.malls.map(mall => <div key={mall.mallId} className={styles.gap}>
          <span><strong>{mall.mallName}</strong> · 缺失 {mall.missingHours} 小时</span>
          <span className={styles.barMeta}>{formatShanghaiDateTime(mall.firstMissingHour)} 至 {formatShanghaiDateTime(mall.lastMissingHour)}</span>
        </div>)}
      </div>}
    </>}
    {progress && <p className={styles.notice} role="status">已扫描 {progress.scannedDays} 个门店日期 · 补入 {progress.savedHours} 小时 · 源端未返回 {progress.sourceMissingHours} 小时 · 失败 {progress.failedDays} 个门店日期 · 仍缺 {progress.remainingHours} 小时</p>}
  </Section>
}
