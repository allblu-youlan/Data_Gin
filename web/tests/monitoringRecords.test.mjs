import assert from 'node:assert/strict'
import test from 'node:test'

import { buildDeliveryLogListQuery, buildDeliveryTaskListQuery, buildDestinationListQuery, buildExcelMatchJobListQuery, buildRunListQuery, buildSourceListQuery, buildTransformRuleListQuery, normalizeMonitoringPageNumber, parseMonitoringPage } from '../.test-dist/monitoringRecords.js'

test('builds bounded safe server-side run filters', () => {
  assert.equal(buildRunListQuery({ page: 2, pageSize: 20, status: 'failed', runType: 'delivery', traceID: ' trace-1 ', startTime: '2026-07-31T09:00', endTime: '2026-07-31T10:00' }), 'page=2&page_size=20&status=failed&run_type=delivery&trace_id=trace-1&start_time=2026-07-31T09%3A00&end_time=2026-07-31T10%3A00')
  assert.equal(buildRunListQuery({ page: 0, pageSize: 101, status: 'unknown', runType: 'other' }), 'page=1&page_size=20')
})

test('builds bounded safe server-side delivery-log filters', () => {
  assert.equal(buildDeliveryLogListQuery({ page: 1, pageSize: 30, destination: ' mall-a ', source: ' source-a ', success: 'false', businessKey: ' order-1 ', startTime: '2026-07-31T09:00', endTime: '2026-07-31T10:00' }), 'page=1&page_size=30&destination=mall-a&source=source-a&success=false&business_key=order-1&start_time=2026-07-31T09%3A00&end_time=2026-07-31T10%3A00')
})

test('builds only supported server-side configuration filters', () => {
  assert.equal(buildSourceListQuery({ page: 2, pageSize: 20, keyword: ' source-a ', enabled: 'true', sourceType: ' api_poll ' }), 'page=2&page_size=20&keyword=source-a&enabled=true&source_type=api_poll')
  assert.equal(buildTransformRuleListQuery({ page: 1, pageSize: 20, keyword: ' mapping ', enabled: 'false', ruleType: 'mapping', sourceID: '7' }), 'page=1&page_size=20&keyword=mapping&enabled=false&rule_type=mapping&source_id=7')
  assert.equal(buildDestinationListQuery({ page: 1, pageSize: 20, keyword: ' target-a ', destinationType: ' http ' }), 'page=1&page_size=20&keyword=target-a&destination_type=http')
  assert.equal(buildDeliveryTaskListQuery({ page: 1, pageSize: 20, keyword: ' task-a ', destinationID: '0' }), 'page=1&page_size=20&keyword=task-a')
})

test('builds bounded Excel job pagination and filters', () => {
  assert.equal(buildExcelMatchJobListQuery({ page: 3, pageSize: 20, keyword: ' orders August ', status: 'failed', operation: 'write' }), 'page=3&page_size=20&keyword=orders+August&status=failed&operation=write')
  assert.equal(buildExcelMatchJobListQuery({ page: 2, pageSize: 20, operation: ' match ' }), 'page=2&page_size=20&operation=match')
  assert.equal(buildExcelMatchJobListQuery({ page: 1, pageSize: 20, operation: 'export' }), 'page=1&page_size=20&operation=export')
  assert.equal(buildExcelMatchJobListQuery({ page: 0, pageSize: 101, keyword: ' x ', status: 'unknown', operation: 'all' }), 'page=1&page_size=20&keyword=x')
  assert.equal(buildExcelMatchJobListQuery({ page: 1, pageSize: 20, operation: 'export_match' }), 'page=1&page_size=20')
})

test('normalizes an out-of-range paginated result page', () => {
  assert.equal(normalizeMonitoringPageNumber(3, 2), 2)
  assert.equal(normalizeMonitoringPageNumber(3, 0), 1)
  assert.equal(normalizeMonitoringPageNumber(0, 2), 1)
})

test('accepts only complete paginated monitoring responses', () => {
  const page = parseMonitoringPage({ data: { runs: [{ id: 1 }], pagination: { page: 1, page_size: 20, total: 21, total_pages: 2 } } }, 'runs')
  assert.deepEqual(page, { list: [{ id: 1 }], pagination: { page: 1, pageSize: 20, total: 21, totalPages: 2 } })
  assert.equal(parseMonitoringPage({ data: { runs: [], pagination: { page: 0, page_size: 20, total: 0, total_pages: 0 } } }, 'runs'), null)
})
