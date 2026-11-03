import test from 'node:test'
import assert from 'node:assert/strict'
import { isSalesDate, parseSalesSummary, salesPeriodRange, salesSummaryPath, shanghaiSalesDate } from '../.test-dist/salesSummary/contracts.js'

const range = { startDate: '2026-10-01', endDate: '2026-10-31' }
const payload = {
  code: 0,
  data: {
    ...range,
    mall: { id: 1, mallCode: 'SH-001', nameCn: '上海商场' },
    metrics: {
      salesAmount: 380998, salesTarget: 0, salesAchievementRate: 0, averageOrderValue: 599.05,
      itemsPerOrder: 3.94, quantityTarget: 0, conversionRate: 0, memberSalesRatio: 0,
      totalQuantity: 2437, salesQuantity: 2503, returnQuantity: -66, orderCount: 636,
      averageItemPrice: 599.05, discountRate: 82.16,
    },
    weather: { date: '2026-11-02', temperatureMinC: 16, temperatureMaxC: 26, skycon: 'LIGHT_RAIN' },
  },
}

test('preserves returned sales and daily weather data', () => {
  assert.deepEqual(parseSalesSummary(payload, 'SH-001', range), payload.data)
  const missingWeather = structuredClone(payload)
  missingWeather.data.weather = null
  assert.equal(parseSalesSummary(missingWeather, 'SH-001', range).weather, null)
})

test('rejects mismatched mall/date and malformed amounts', () => {
  assert.equal(parseSalesSummary(payload, 'SH-002', range), null)
  assert.equal(parseSalesSummary(payload, 'SH-001', { ...range, endDate: '2026-10-30' }), null)
  const malformed = structuredClone(payload)
  malformed.data.metrics.salesAmount = '380998'
  assert.equal(parseSalesSummary(malformed, 'SH-001', range), null)
  delete malformed.data.metrics.salesAmount
  assert.equal(parseSalesSummary(malformed, 'SH-001', range), null)
})

test('calculates calendar periods in Shanghai with Monday as the start of week', () => {
  assert.equal(shanghaiSalesDate(new Date('2026-11-01T16:01:00Z')), '2026-11-02')
  assert.deepEqual(salesPeriodRange('today', '2026-11-02'), { startDate: '2026-11-02', endDate: '2026-11-02' })
  assert.deepEqual(salesPeriodRange('week', '2026-11-01'), { startDate: '2026-10-26', endDate: '2026-11-01' })
  assert.deepEqual(salesPeriodRange('month', '2026-11-02'), { startDate: '2026-11-01', endDate: '2026-11-02' })
})

test('validates calendar dates and encodes selected query parameters', () => {
  assert.equal(isSalesDate('2026-02-29'), false)
  assert.equal(isSalesDate('2026-13-01'), false)
  assert.equal(isSalesDate('2028-02-29'), true)
  const url = new URL(salesSummaryPath('SH-001', range), 'https://example.com')
  assert.equal(url.pathname, '/v1/sales-summary')
  assert.equal(url.searchParams.get('mallCode'), 'SH-001')
  assert.equal(url.searchParams.get('startDate'), range.startDate)
  assert.equal(url.searchParams.get('endDate'), range.endDate)
})
