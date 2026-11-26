import type { ApiOperation } from './apiDocs'

const knownOperationIds = new Set([
  'weather-malls-query', 'weather-overview', 'weather-realtime', 'weather-history-day',
  'weather-history-range', 'weather-history-day-summary', 'weather-minutely', 'weather-hourly',
  'weather-daily', 'weather-alerts', 'weather-life-indices', 'weather-legacy-overview',
  'weather-legacy-realtime', 'weather-legacy-minutely', 'weather-legacy-hourly',
  'weather-legacy-daily', 'weather-legacy-alerts', 'weather-legacy-life-indices',
  'bojun-orders', 'bojun-order-details', 'bojun-products', 'bojun-sync-status',
  'bojun-stores', 'bojun-stocks', 'bojun-stock-snapshots', 'bojun-product-infos',
  'business-payments', 'tmall-orders-realtime', 'tmall-sycm', 'tmall-sycm-hourly',
  'tmall-qztg', 'tmall-qztg-hourly', 'tmall-tot',
  'monitoring-hourly', 'monitoring-daily', 'monitoring-areas',
])

const operationLabels: Record<string, string> = {
  'tmall-sycm': '商品日数据',
  'tmall-sycm-hourly': '商品小时数据',
  'tmall-qztg': '全站推广（日）',
  'tmall-qztg-hourly': '全站推广（小时）',
  'tmall-orders-realtime': '订单明细（小时）',
  'tmall-tot': '投放资料（日）',
  'weather-history-day': '历史单日实况',
  'weather-history-range': '历史区间实况',
  'weather-history-day-summary': '历史单日汇总',
}

export function apiOperationLabel(operation: ApiOperation): string {
  if (!knownOperationIds.has(operation.id)) return operation.title
  return operationLabels[operation.id] ?? operation.title
    .replace(/^(?:天猫|伯俊)/, '')
    .replace(/^查询|查询$/g, '')
    .replace(/（商场ID兼容路径）$/, '（商场ID兼容）')
}

export function apiOperationTitle(operation: ApiOperation): string {
  return operation.id === 'tmall-sycm' ? '查询商品日数据' : operation.title
}
