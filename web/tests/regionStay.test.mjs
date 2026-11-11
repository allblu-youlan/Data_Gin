import test from 'node:test'
import assert from 'node:assert/strict'
import { regionGroups, regionDailyChart, regionFilterBody, parseRegionOverview, parseRegionMissing, parseRegionBackfill, runRegionBackfill } from '../.test-dist/regionStay/contracts.js'

const filter = { startDate: '2026-11-01', endDate: '2026-11-03', mallId: '', regionName: '' }
const row = { mallId: 1, mallName: '测试门店', shopId: 'shop-1', statDate: '2026-11-01', regionName: '游乐区', exposureCount: 100, stayCount: 20, stayRate: 0.2, avgStayTime: 3.38, fetchedAt: '2026-11-02T01:00:00Z' }

test('区域比较按门店区分同名区域，停留率使用累计分子分母，平均时长不聚合', () => {
  const groups = regionGroups([row, { ...row, statDate: '2026-11-03', exposureCount: 10, stayCount: 10, stayRate: 1 }, { ...row, mallId: 2, shopId: 'shop-2' }])
  assert.equal(groups.length, 2)
  assert.equal(groups[0].exposureCount, 110)
  assert.equal(groups[0].stayRate, 30 / 110)
  assert.equal(groups[0].avgStayTime, undefined)
})

test('日趋势保留缺口，真实零保留为零，曝光零时停留率不展示', () => {
  const rows = [row, { ...row, statDate: '2026-11-03', exposureCount: 0, stayCount: 0, stayRate: 0 }]
  assert.deepEqual(regionDailyChart(rows, filter, 'stayCount').map(p => p.value), [20, undefined, 0])
  assert.deepEqual(regionDailyChart(rows, filter, 'stayRate').map(p => p.value), [20, undefined, undefined])
  assert.equal(regionDailyChart(rows, filter, 'stayCount')[0].time, '2026-11-01T00:00:00+08:00')
})

test('单区域平均时长保留源值，多个区域拒绝合成平均时长', () => {
  assert.equal(regionDailyChart([{ ...row, stayCount: 0 }], filter, 'avgStayTime')[0].value, 3.38)
  assert.equal(regionDailyChart([row, { ...row, regionName: '阅读区' }], filter, 'avgStayTime')[0].value, undefined)
})

test('筛选复用门店与31天限制，查询区域名原样发送', () => {
  assert.deepEqual(regionFilterBody({ ...filter, mallId: '2', regionName: '游乐区' }), { startDate: filter.startDate, endDate: filter.endDate, mallId: 2, regionName: '游乐区' })
  assert.throws(() => regionFilterBody({ ...filter, endDate: '2026-12-02' }))
})

test('解析保留日粒度与上海时间，非法指标不作零', () => {
  const data = { ...filter, timezone: 'Asia/Shanghai', items: [row], summary: { exposureCount: 100, stayCount: 20, recordCount: 1, mallCount: 1, regionCount: 1, latestFetchedAt: row.fetchedAt } }
  assert.ok(parseRegionOverview({ data }))
  assert.equal(parseRegionOverview({ data: { ...data, items: [{ ...row, avgStayTime: null }] } }), null)
  assert.equal(parseRegionOverview({ data: { ...data, timezone: 'UTC' } }), null)
})

test('同步检查保留空响应状态与之前成功快照时间', () => {
  const data = { ...filter, timezone: 'Asia/Shanghai', expectedDays: 1, successDays: 0, emptyDays: 1, failedDays: 0, unqueriedDays: 0, unboundMalls: 1, items: [{ ...row, state: 'empty', recordCount: 1, lastSuccessAt: row.fetchedAt }] }
  assert.equal(parseRegionMissing({ data })?.items[0].state, 'empty')
  assert.equal(parseRegionMissing({ data: { ...data, items: [{ ...data.items[0], state: 'unknown' }] } }), null)
})

const batch = { processedDays: 1, savedDays: 1, emptyDays: 0, failedDays: 0, sourceMissingDays: 0, done: true, nextCursor: '' }
test('补拉累加批次，停止后不再发送请求', async () => {
  const cursors = []
  const total = await runRegionBackfill(async cursor => { cursors.push(cursor); return { ...batch, done: false, nextCursor: 'next' } }, () => {}, () => true)
  assert.deepEqual(cursors, [''])
  assert.equal(total.savedDays, 1)
  assert.equal(total.done, false)
})

test('不一致的补拉游标被拒绝，重复游标终止循环', async () => {
  assert.equal(parseRegionBackfill({ data: { ...batch, done: false } }), null)
  await assert.rejects(runRegionBackfill(async () => ({ ...batch, done: false, nextCursor: 'same' }), () => {}, () => false), /进度未前进/)
})
