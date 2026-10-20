import assert from 'node:assert/strict'
import test from 'node:test'
import { businessOverviewMallsPath, businessOverviewPaymentsPath, createMockBusinessSummaries, emptyBusinessSummary, filterBusinessSummary, mergeBusinessOverviewMalls, parseBusinessOverviewMalls, parseBusinessOverviewPayments, queryMockBusinessOverview, recordPaymentDetail, recordTotal } from '../.test-dist/businessOverview.js'
import { canViewNavigationItem, resolveAccessibleNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'
import { canCommitOverviewResponse, overviewRequestPlan, overviewSignalAccess, restrictOverviewData } from '../.test-dist/appShell/overviewWorkspacePolicy.js'

test('aggregates the full day from cashier-level payment records', () => {
  const summary = createMockBusinessSummaries('2026-09-01')['2026-09-01']
  assert.equal(summary.storeAmount, 1379.7)
  assert.deepEqual(summary.payments, [{ name: '支付宝', amount: 663.3 }, { name: '微信', amount: 716.4 }])
  assert.equal(summary.actualAmount, 1348.7)
  assert.equal(summary.records.reduce((total, record) => total + recordTotal(record), 0), summary.storeAmount)
  assert.equal(summary.records.reduce((total, record) => total + record.unsettled, 0), summary.unsettledAmount)
  assert.equal(summary.unsettledAmount, 1379.7)
})

test('keeps cashier payment totals and detail text on one source of truth', () => {
  const summary = createMockBusinessSummaries('2026-09-01')['2026-09-01']
  const cashier = filterBusinessSummary(summary, 'counter-01')
  assert.equal(cashier.storeAmount, 736.2)
  assert.equal(cashier.unsettledAmount, 736.2)
  assert.deepEqual(cashier.payments, [{ name: '支付宝', amount: 358.3 }, { name: '微信', amount: 377.9 }])
  assert.equal(cashier.publicExpense, 8)
  assert.equal(recordPaymentDetail(cashier.records[0]), '支付宝 358.3 / 微信 377.9')
})

test('builds mock data relative to the supplied business date', () => {
  const summaries = createMockBusinessSummaries('2027-01-01')
  assert.deepEqual(Object.keys(summaries), ['2027-01-01', '2026-12-31'])
  assert.equal(emptyBusinessSummary('2027-01-02').records.length, 0)
})

test('uses mall code as the mock business overview query input', () => {
  const firstMallCode = 'SH-PD-001'
  const secondMallCode = 'SH-JA-001'
  const firstMall = queryMockBusinessOverview({ mallCode: firstMallCode, date: '2026-09-01', cashierID: 'all' }, '2026-09-01')
  const secondMall = queryMockBusinessOverview({ mallCode: secondMallCode, date: '2026-09-01', cashierID: 'all' }, '2026-09-01')
  assert.equal(firstMall.storeAmount, 1379.7)
  assert.equal(secondMall.storeAmount, 1131.36)
  assert.equal(secondMall.unsettledAmount, secondMall.storeAmount)
  assert.equal(Math.round(secondMall.records.reduce((total, record) => total + record.unsettled, 0) * 100) / 100, secondMall.unsettledAmount)
  assert.notEqual(firstMall.records[0].id, secondMall.records[0].id)
  assert.match(secondMall.records[0].id, new RegExp(`^${secondMallCode}-`))
})

test('guards business overview navigation with its dedicated permission', () => {
  assert.equal(canViewNavigationItem('business_overview', []), false)
  assert.equal(canViewNavigationItem('business_overview', ['mall.read']), false)
  assert.equal(canViewNavigationItem('business_overview', ['business_overview.read']), true)
})

test('resolves an inaccessible landing page to the first permitted navigation item', () => {
  const orderedItems = ['business_overview', 'store_info', 'overview', 'runs']
  assert.equal(resolveAccessibleNavigationItem('overview', orderedItems, ['business_overview.read']), 'business_overview')
  assert.equal(resolveAccessibleNavigationItem('runs', orderedItems, ['pipeline.read']), 'runs')
  assert.equal(resolveAccessibleNavigationItem('overview', orderedItems, []), null)
})

test('only exposes the operations overview when at least one overview signal is readable', () => {
  assert.equal(canViewNavigationItem('overview', []), false)
  assert.equal(canViewNavigationItem('overview', ['business_overview.read']), false)
  assert.equal(canViewNavigationItem('overview', ['data.read']), true)
  assert.equal(canViewNavigationItem('overview', ['pipeline.manage']), true)
  assert.deepEqual(overviewSignalAccess(['delivery.manage', 'data.read']), {
    runs: false,
    deliveryLogs: true,
    statistics: true,
    weather: false,
  })
})

test('only applies manage-to-read inference supported by the backend authorizer', () => {
  assert.equal(canViewNavigationItem('sources', ['source.manage']), true)
  assert.equal(canViewNavigationItem('runs', ['pipeline.manage']), true)
  assert.equal(canViewNavigationItem('delivery_logs', ['delivery.manage']), true)
  assert.equal(canViewNavigationItem('receive', ['data.manage']), false)
  assert.equal(canViewNavigationItem('excel_jobs', ['excel.manage']), false)
  assert.equal(canViewNavigationItem('office_messages', ['office_message.manage']), false)
})

test('separates report administration from the download-only navigation', () => {
  const downloadPermissions = ['report.read', 'report.execute', 'report.export']
  assert.equal(canViewNavigationItem('report_query', downloadPermissions), true)
  assert.equal(canViewNavigationItem('report_exports', downloadPermissions), true)
  assert.equal(canViewNavigationItem('report_catalog', downloadPermissions), false)
  assert.equal(canViewNavigationItem('report_configuration', downloadPermissions), false)

  const administrationPermissions = ['report.read', 'report.manage']
  assert.equal(canViewNavigationItem('report_catalog', administrationPermissions), true)
  assert.equal(canViewNavigationItem('report_configuration', administrationPermissions), true)
  assert.equal(canViewNavigationItem('report_query', administrationPermissions), false)
})

test('builds overview requests only for readable signals', () => {
  assert.deepEqual(overviewRequestPlan(['pipeline.read', 'data.read'], '2026-09-01T00:00'), {
    runs: '/v1/runs?page=1&page_size=100&start_time=2026-09-01T00%3A00',
    deliveryLogs: null,
    statistics: '/v1/data/statistics',
    weather: null,
    health: '/health',
  })
})

test('clears overview data when permissions are reduced or removed', () => {
  const current = {
    runs: [{ id: 1 }],
    deliveryLogs: [{ id: 2 }],
    overviewTotals: { runs: 1, deliveryLogs: 1 },
    monitoring: { statistics: { total: 3 }, weather: { total: 4 }, health: { status: 'ok' } },
  }
  assert.deepEqual(restrictOverviewData(current, ['delivery.read', 'weather.read']), {
    runs: [],
    deliveryLogs: [{ id: 2 }],
    overviewTotals: { runs: null, deliveryLogs: 1 },
    monitoring: { statistics: null, weather: { total: 4 }, health: { status: 'ok' } },
  })
  assert.deepEqual(restrictOverviewData(current, []), {
    runs: [],
    deliveryLogs: [],
    overviewTotals: { runs: null, deliveryLogs: null },
    monitoring: { statistics: null, weather: null, health: { status: 'ok' } },
  })
})

test('clears overview data across navigation away and does not restore stale data on return', () => {
  const current = {
    runs: [{ id: 1 }],
    deliveryLogs: [{ id: 2 }],
    overviewTotals: { runs: 1, deliveryLogs: 1 },
    monitoring: { statistics: { total: 3 }, weather: { total: 4 }, health: { status: 'ok' } },
  }
  const afterLeavingOverview = restrictOverviewData(current, ['pipeline.read', 'delivery.read', 'data.read', 'weather.read'], false)
  assert.deepEqual(afterLeavingOverview, {
    runs: [],
    deliveryLogs: [],
    overviewTotals: { runs: null, deliveryLogs: null },
    monitoring: { statistics: null, weather: null, health: null },
  })
  assert.strictEqual(
    restrictOverviewData(afterLeavingOverview, ['pipeline.read', 'delivery.read', 'data.read', 'weather.read'], true),
    afterLeavingOverview,
  )
})

test('rejects a late overview response after its request is aborted', async () => {
  const controller = new AbortController()
  let applied = false
  const lateResponse = Promise.resolve().then(() => {
    if (canCommitOverviewResponse(controller.signal)) applied = true
  })
  controller.abort()
  await lateResponse
  assert.equal(applied, false)
})

test('builds the payment query path from ISO date and mall code', () => {
  assert.equal(
    businessOverviewPaymentsPath('2026-09-01', 'abcn001a002'),
    '/v1/business-overview/payments?date=20260901&mallCode=ABCN001A002',
  )
  assert.throws(() => businessOverviewPaymentsPath('2026-02-30', 'ABCN001A002'))
  assert.throws(() => businessOverviewPaymentsPath('2026-09-01', "A' OR 1=1--"))
})

test('parses and paginates the dedicated business overview mall list', () => {
  assert.equal(businessOverviewMallsPath(), '/v1/business-overview/malls?limit=50')
  assert.equal(businessOverviewMallsPath(8, 25), '/v1/business-overview/malls?limit=25&afterId=8')
  assert.deepEqual(parseBusinessOverviewMalls({ code: 0, data: {
    items: [{ id: 8, mallCode: 'ABCN001A002', nameCn: ' 徐汇万科 ' }],
    nextAfterId: 8,
  } }), { items: [{ id: 8, mallCode: 'ABCN001A002', nameCn: '徐汇万科' }], nextAfterId: 8 })
  assert.equal(parseBusinessOverviewMalls({ code: 0, data: { items: [{ id: 0, mallCode: 'BAD', nameCn: '商场' }], nextAfterId: 0 } }), null)
  assert.deepEqual(mergeBusinessOverviewMalls(
    [{ id: 1, mallCode: 'MALL-001', nameCn: '旧名称' }],
    [{ id: 1, mallCode: 'MALL-001', nameCn: '新名称' }, { id: 2, mallCode: 'MALL-002', nameCn: '二号商场' }],
  ), [{ id: 1, mallCode: 'MALL-001', nameCn: '新名称' }, { id: 2, mallCode: 'MALL-002', nameCn: '二号商场' }])
})

test('maps Oracle payment rows into the existing business summary', () => {
  const summary = parseBusinessOverviewPayments({ code: 0, data: {
    date: '20260901',
    mallCode: 'ABCN001A002',
    items: [
      { billDate: 20260901, storeId: 462, storeName: 'ALLBLU（上海徐汇区徐汇万科广场店）', storeCode: 'ABCN001A002', paywayId: 24, payAmount: 3164.76, paywayName: '微信' },
      { billDate: 20260901, storeId: 462, storeName: 'ALLBLU（上海徐汇区徐汇万科广场店）', storeCode: 'ABCN001A002', paywayId: 25, payAmount: 1836.22, paywayName: '支付宝' },
      { billDate: 20260901, storeId: 462, storeName: 'ALLBLU（上海徐汇区徐汇万科广场店）', storeCode: 'ABCN001A002', paywayId: 28, payAmount: 1627.2, paywayName: '刷卡POS支付' },
    ],
  } }, '2026-09-01', 'ABCN001A002')
  assert.ok(summary)
  assert.equal(summary.storeAmount, 6628.18)
  assert.equal(summary.actualAmount, 6628.18)
  assert.deepEqual(summary.payments, [
    { name: '微信', amount: 3164.76 },
    { name: '支付宝', amount: 1836.22 },
    { name: '刷卡POS支付', amount: 1627.2 },
  ])
  assert.equal(summary.records[0].cashierName, '全部收银机')
})

test('accepts empty payment data and rejects mismatched or malformed rows', () => {
  const empty = parseBusinessOverviewPayments({ code: 0, data: { date: '20260901', mallCode: 'ABCN001A002', items: [] } }, '2026-09-01', 'ABCN001A002')
  assert.ok(empty)
  assert.equal(empty.records.length, 0)
  assert.equal(parseBusinessOverviewPayments({ code: 0, data: { date: '20260902', mallCode: 'ABCN001A002', items: [] } }, '2026-09-01', 'ABCN001A002'), null)
  assert.equal(parseBusinessOverviewPayments({ code: 0, data: { date: '20260901', mallCode: 'ABCN001A002', items: [
    { billDate: 20260901, storeId: 462, storeName: 'store', storeCode: 'ABCN001A002', paywayId: 24, payAmount: '3164.76', paywayName: '微信' },
  ] } }, '2026-09-01', 'ABCN001A002'), null)
})


test('query-only report users can enter query navigation without export access', () => {
  assert.equal(canViewNavigationItem('report_query', ['report.read', 'report.execute']), true)
  assert.equal(canViewNavigationItem('report_exports', ['report.read', 'report.execute']), false)
  assert.equal(canViewNavigationItem('report_query', ['report.execute']), false)
})
