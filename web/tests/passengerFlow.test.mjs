import assert from 'node:assert/strict'
import test from 'node:test'
import { passengerChartData, passengerFilterBody, passengerFilterError, passengerPreset, passengerQueryPath, parsePassengerMalls, parsePassengerOverview, shanghaiPassengerDate } from '../.test-dist/passengerFlow/contracts.js'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'

const counts = { passby: 100, traffic: 20, trafficCount: 18, trafficBatch: 10, captureRate: 0.2, recordCount: 1 }
const trend = [
  { ...counts, hourStart: '2026-10-29T08:00:00+08:00' },
  { ...counts, hourStart: '2026-10-29T10:00:00+08:00', traffic: 0, passby: 0, captureRate: 0 },
]
const overview = { startDate: '2026-10-29', endDate: '2026-10-29', timeZone: 'Asia/Shanghai', summary: { ...counts, mallCount: 1, latestHour: trend[1].hourStart }, trend, malls: [{ ...counts, mallId: 3, mallName: '门店甲', shopId: 'shop-3', shopName: '监控甲' }] }

test('monitoring uses data service permission', () => {
  assert.equal(canViewNavigationItem('passenger_flow', []), false)
  assert.equal(canViewNavigationItem('passenger_flow', ['data.read']), true)
  assert.equal(canViewNavigationItem('passenger_flow', ['mall.read']), false)
})

test('date presets follow Shanghai calendar and inclusive 31 day limit', () => {
  assert.equal(shanghaiPassengerDate(new Date('2026-10-28T17:00:00Z')), '2026-10-29')
  assert.deepEqual(passengerPreset(7, new Date('2026-10-28T17:00:00Z')), { startDate: '2026-10-23', endDate: '2026-10-29', mallId: '' })
  assert.equal(passengerFilterError({ startDate: '2026-10-01', endDate: '2026-10-31', mallId: '' }), '')
  for (const [startDate, endDate] of [['2026-10-01', '2026-11-01'], ['2026-10-29', '2026-10-28'], ['2026-02-30', '2026-03-01']]) {
    assert.ok(passengerFilterError({ startDate, endDate, mallId: '' }))
  }
})

test('query and export share the exact selected dates and mall', () => {
  const filter = { startDate: '2026-10-28', endDate: '2026-10-29', mallId: '3' }
  assert.equal(passengerQueryPath(filter), '/v1/kaiyu-passenger?startDate=2026-10-28&endDate=2026-10-29&mallId=3')
  assert.deepEqual(passengerFilterBody(filter), { startDate: '2026-10-28', endDate: '2026-10-29', mallId: 3 })
  assert.deepEqual(passengerFilterBody({ ...filter, mallId: '' }), { startDate: '2026-10-28', endDate: '2026-10-29' })
  assert.throws(() => passengerQueryPath({ ...filter, mallId: '3&mallId=4' }))
})

test('chart gaps remain missing and recorded zeros remain zero', () => {
  assert.deepEqual(passengerChartData([...trend].reverse(), 'traffic').map(point => point.value), [20, undefined, 0])
  assert.deepEqual(passengerChartData(trend, 'captureRate').map(point => point.value), [20, undefined, undefined])
  assert.equal(passengerChartData(trend, 'traffic')[1].time, '2026-10-29T09:00:00+08:00')
  assert.deepEqual(passengerChartData([], 'traffic'), [])
})

test('strictly parses the graph and mall option contracts', () => {
  assert.deepEqual(parsePassengerOverview({ data: overview }), overview)
  assert.deepEqual(parsePassengerMalls({ data: { items: [{ mallId: 3, mallName: '待绑定门店', shopId: '' }] } }), [{ mallId: 3, mallName: '待绑定门店', shopId: '' }])
  assert.equal(parsePassengerOverview({ data: { ...overview, summary: { ...overview.summary, traffic: '20' } } }), null)
  assert.equal(parsePassengerOverview({ data: { ...overview, trend: null } }), null)
  assert.equal(parsePassengerMalls({ data: { items: [{ mallId: 3 }] } }), null)
})
