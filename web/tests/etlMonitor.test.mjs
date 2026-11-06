import assert from 'node:assert/strict'
import test from 'node:test'
import { parseETLMonitor, formatETLValue, filterETLSeries, etlProbeLabel } from '../.test-dist/monitoringPages/ETLMonitorPage/contracts.js'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'

const probe = { state: 'unknown', success: null, http_status: 0, error: '', checked_at: null, last_success_at: null, duration_ms: 0, consecutive_failures: 0, consecutive_successes: 0, alerting: false }
const snapshot = { configured: false, config_error: '', target: '', checked_at: null, last_metrics_at: null, stale: true, health_interval_seconds: 15, metrics_interval_seconds: 30, probes: { ping: probe, ready: probe, metrics: probe }, summary: [], series: [], history: [] }

test('keeps an unconfigured target unknown without inventing healthy or zero values', () => {
  assert.deepEqual(parseETLMonitor({ data: snapshot }), snapshot)
  assert.equal(etlProbeLabel(probe), '尚未探测')
  assert.equal(formatETLValue(null, 'msg/s'), '—')
  assert.equal(formatETLValue(0, 'msg/s'), '0 msg/s')
})

test('parses collected values, labels and unknown rates from the real envelope', () => {
  const data = { ...snapshot, configured: true, target: 'https://etl.example/proxy', summary: [{ key: 'input_rate', label: '输入速率', value: null, unit: 'msg/s', description: '等待第二次采样' }], series: [{ name: 'input_received', type: 'counter', labels: { label: 'jushuitan_orders' }, value: 42, rate_per_second: null }], history: [{ at: '2026-11-06T00:00:00Z', input_rate: null, output_rate: 0, error_rate: 0, cpu_percent: null, rss_bytes: 1024 }] }
  assert.deepEqual(parseETLMonitor({ data }), data)
  assert.equal(filterETLSeries(data.series, 'jushuitan').length, 1)
  assert.equal(filterETLSeries(data.series, 'INPUT_RECEIVED').length, 1)
  assert.equal(filterETLSeries(data.series, 'missing').length, 0)
})

test('rejects malformed states and missing payloads instead of showing healthy data', () => {
  assert.equal(parseETLMonitor(snapshot), null)
  assert.equal(parseETLMonitor({ data: { ...snapshot, configured: 'false' } }), null)
  assert.equal(parseETLMonitor({ data: { ...snapshot, probes: { ...snapshot.probes, ping: { ...probe, success: 'true' } } } }), null)
  assert.equal(parseETLMonitor({ data: { ...snapshot, summary: [{ key: 'cpu', label: 'CPU', value: Infinity, unit: '%', description: '' }] } }), null)
})

test('distinguishes failing, recovery and settled probe results', () => {
  assert.equal(etlProbeLabel({ ...probe, state: 'failing', success: false }), '探测失败 · 待确认')
  assert.equal(etlProbeLabel({ ...probe, state: 'unhealthy', success: false, alerting: true }), '持续异常')
  assert.equal(etlProbeLabel({ ...probe, state: 'recovering', success: true, alerting: true }), '恢复确认中')
  assert.equal(etlProbeLabel({ ...probe, state: 'healthy', success: true }), '正常')
})

test('ETL monitor requires the same read permission as ingestion monitoring', () => {
  assert.equal(canViewNavigationItem('etl_monitor', []), false)
  assert.equal(canViewNavigationItem('etl_monitor', ['pipeline.read']), false)
  assert.equal(canViewNavigationItem('etl_monitor', ['data.read']), true)
})
