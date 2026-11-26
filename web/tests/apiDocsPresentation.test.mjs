import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { apiOperationLabel, apiOperationTitle } from '../.test-dist/apiDocsPresentation.js'
import { filterApiGroups } from '../.test-dist/apiDocs.js'

const catalog = JSON.parse(readFileSync(new URL('../../internal/apidoc/catalog.json', import.meta.url), 'utf8'))
const operation = id => catalog.operations.find(item => item.id === id)

test('shortens known names without losing frequency and detail distinctions', () => {
  const cases = [
    ['tmall-sycm', '商品日数据'],
    ['tmall-sycm-hourly', '商品小时数据'],
    ['tmall-qztg', '全站推广（日）'],
    ['tmall-qztg-hourly', '全站推广（小时）'],
    ['tmall-orders-realtime', '订单明细（小时）'],
    ['bojun-orders', '订单'],
    ['bojun-order-details', '单订单完整明细'],
    ['bojun-products', '单商品'],
    ['bojun-product-infos', '基础商品资料'],
    ['weather-realtime', '当前实况'],
    ['weather-legacy-realtime', '当前实况（商场ID兼容）'],
  ]
  for (const [id, expected] of cases) assert.equal(apiOperationLabel(operation(id)), expected, id)
})

test('uses the approved product-day heading while preserving other original titles', () => {
  assert.equal(apiOperationTitle(operation('tmall-sycm')), '查询商品日数据')
  for (const item of catalog.operations.filter(item => item.id !== 'tmall-sycm')) assert.equal(apiOperationTitle(item), item.title)
})

test('keeps every current catalog entry distinguishable without changing source content', () => {
  const before = JSON.stringify(catalog)
  const labels = catalog.operations.map(apiOperationLabel)
  assert.equal(new Set(labels).size, catalog.operations.length)
  for (const label of labels) assert.ok(label.trim().length > 0)
  assert.equal(JSON.stringify(catalog), before)
})

test('leaves new interfaces unchanged even when their titles match shortening patterns', () => {
  const item = { ...operation('tmall-sycm'), id: 'tmall-new-query', title: '天猫新商品小时汇总查询' }
  assert.equal(apiOperationLabel(item), item.title)
  assert.equal(apiOperationTitle(item), item.title)
})

test('finds interfaces by their visible directory name and approved page heading', () => {
  const groups = [{ code: 'tmall', name: '天猫', children: [{ code: 'products', name: '商品', operations: catalog.operations }] }]
  const displayText = item => apiOperationTitle(item) + ' ' + apiOperationLabel(item)
  for (const query of ['商品日数据', '查询商品日数据']) {
    const result = filterApiGroups(groups, query, displayText)
    assert.deepEqual(result.flatMap(group => group.children.flatMap(child => child.operations.map(item => item.id))), ['tmall-sycm'])
  }
})
