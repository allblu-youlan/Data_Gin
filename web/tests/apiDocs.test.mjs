import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { parseApiDocument, filterApiGroups, filterApiFields, buildCurl, buildApiDocsLink } from '../.test-dist/apiDocs.js'

const operation = { id: 'orders', method: 'POST', path: '/api/open/orders/query', title: '订单查询', description: '分页查询订单', permission: 'orders.read', authentication: 'token 请求头', requestFields: [], responseFields: [{ name: 'data', type: 'object', required: true, description: '结果', children: [{ name: 'externalOrderNo', type: 'string', required: true, description: '外部订单号' }] }], requestExample: { note: "customer's order" }, responseExample: { code: 0 }, errors: [] }
const document = { title: '接口文档', version: '1', groups: [{ code: 'commerce', name: '业务平台', children: [{ code: 'orders', name: '订单', operations: [operation] }] }] }

test('reads backend-owned groups without a fixed platform or account catalog', () => {
  assert.deepEqual(parseApiDocument({ code: 0, data: { document } }), document)
  assert.throws(() => parseApiDocument({ data: { accounts: [] } }), /文档/)
})

test('searches nested field descriptions and preserves the two-level directory', () => {
  assert.equal(filterApiGroups(document.groups, '外部订单号')[0].children[0].operations[0].id, 'orders')
  assert.equal(filterApiGroups(document.groups, '业务平台')[0].children.length, 1)
  assert.equal(filterApiGroups(document.groups, 'missing').length, 0)
  assert.equal(filterApiGroups(document.groups, ' POST ')[0].children[0].operations.length, 1)
})

test('finds permission codes independently of display names and descriptive text', () => {
  const groups = [{ ...document.groups[0], code: 'commerce.api.read', children: [{ ...document.groups[0].children[0], code: 'orders.category', operations: [{ ...operation, parentPermission: 'orders.api.read' }] }] }]
  for (const query of ['commerce.api.read', 'orders.category', ' ORDERS.READ ', 'orders.api.read']) {
    assert.equal(filterApiGroups(groups, query)[0].children[0].operations[0].id, 'orders')
  }
  assert.equal(filterApiGroups(groups, 'orders.write').length, 0)
})

test('builds permission links for the public docs page without hardcoded operation IDs', () => {
  assert.equal(buildApiDocsLink(), '?view=api-docs')
  assert.equal(buildApiDocsLink('  '), '?view=api-docs')
  const url = new URL(buildApiDocsLink(' monitoring.hourly.read '), 'https://app.example.com/')
  assert.equal(url.searchParams.get('view'), 'api-docs')
  assert.equal(url.searchParams.get('permission'), 'monitoring.hourly.read')
  assert.equal(url.hash, '')
  const encoded = new URL(buildApiDocsLink('example.read&extra=value'), url)
  assert.equal(encoded.searchParams.get('permission'), 'example.read&extra=value')
  assert.equal(encoded.searchParams.has('extra'), false)
})

test('produces usable curl with placeholder credentials and shell-safe example body', () => {
  const curl = buildCurl(operation, 'https://api.example.com/')
  assert.match(curl, /https:\/\/api.example.com\/api\/open\/orders\/query/)
  assert.match(curl, /token: dg_open_REPLACE_WITH_YOUR_TOKEN/)
  assert.ok(curl.includes("customer'\"'\"'s order"))
  assert.doesNotMatch(buildCurl({ ...operation, method: 'GET', permission: '' }, ''), /--data|Authorization/)
})

test('finds inline rules without sending readers to another document', () => {
  const groups = [{ ...document.groups[0], children: [{ ...document.groups[0].children[0], operations: [{ ...operation, notes: [{ title: '金额口径', body: '金额使用人民币元。\n订单与退款通过原订单号关联。' }] }] }] }]
  assert.equal(filterApiGroups(groups, '人民币元')[0].children[0].operations[0].id, 'orders')
  assert.equal(filterApiGroups(groups, '金额口径')[0].children[0].operations.length, 1)
})

test('substitutes documented path and query examples in curl', () => {
  const getOperation = { ...operation, method: 'GET', path: '/api/v1/sources/:id', requestFields: [{ name: 'id', location: 'path', type: 'integer', required: true, description: '数据源 ID', example: 12 }, { name: 'schema', location: 'query', type: 'string', required: false, description: '模式', example: 'sales data' }] }
  const curl = buildCurl(getOperation, 'https://api.example.com/')
  assert.match(curl, /https:\/\/api.example.com\/api\/v1\/sources\/12\?schema=sales\+data/)
  assert.doesNotMatch(curl, /--data-raw/)
  assert.match(buildCurl({ ...getOperation, path: '/api/v1/sources/{id}', requestFields: [] }, 'https://api.example.com'), /sources\/7/)
})

const fields = [
  { name: 'code', type: 'integer', required: true, description: '业务码' },
  { name: 'data', type: 'object | null', required: true, description: '查询结果', children: [
    { name: 'items', type: 'OrderItem[]', required: true, description: '订单明细', children: [
      { name: 'SKU', type: 'string', required: true, description: '商品编码' },
      { name: 'amount', type: 'decimal', required: true, description: '实付人民币金额' },
      { name: 'payments', type: 'array<object>', required: false, description: '支付记录', children: [
        { name: 'paymentId', type: 'string', required: true, description: '支付流水号' },
        { name: 'status', type: 'string', required: true, description: '支付状态' },
      ] },
    ] },
    { name: 'total', type: 'integer', required: true, description: '记录总数' },
    { name: 'summary', type: 'object', required: true, description: '汇总', children: [
      { name: 'SKU', type: 'string', required: true, description: '展示编码' },
    ] },
  ] },
]

test('field search preserves matching ancestors and removes unrelated branches', () => {
  const result = filterApiFields(fields, '实付人民币')
  assert.deepEqual(result.map(field => field.name), ['data'])
  assert.deepEqual(result[0].children.map(field => field.name), ['items'])
  assert.deepEqual(result[0].children[0].children.map(field => field.name), ['amount'])
})

test('field search matches names and types without case or surrounding whitespace', () => {
  const names = filterApiFields(fields, ' sku ')[0].children
  assert.deepEqual(names.map(field => field.name), ['items', 'summary'])
  assert.deepEqual(names[0].children.map(field => field.name), ['SKU'])
  assert.deepEqual(names[1].children.map(field => field.name), ['SKU'])
  assert.equal(filterApiFields(fields, ' DECIMAL ')[0].children[0].children[0].name, 'amount')
})

test('field search accepts full paths through both documented array formats', () => {
  const result = filterApiFields(fields, 'DATA.items[].sku')
  assert.deepEqual(result[0].children.map(field => field.name), ['items'])
  assert.deepEqual(result[0].children[0].children.map(field => field.name), ['SKU'])
  const payment = filterApiFields(fields, 'data.items[].payments[].paymentId')
  assert.deepEqual(payment[0].children[0].children.map(field => field.name), ['payments'])
  assert.deepEqual(payment[0].children[0].children[0].children.map(field => field.name), ['paymentId'])
})

test('matching a parent preserves its entire child tree', () => {
  const result = filterApiFields(fields, '订单明细')
  assert.deepEqual(result[0].children.map(field => field.name), ['items'])
  assert.deepEqual(result[0].children[0], fields[1].children[0])
})

test('field search returns the original array for an empty query and no rows for a missing term', () => {
  assert.equal(filterApiFields(fields, ''), fields)
  assert.equal(filterApiFields(fields, ' \n '), fields)
  assert.deepEqual(filterApiFields(fields, 'missing'), [])
})

test('filtering nested fields leaves the original document intact', () => {
  const before = structuredClone(fields)
  filterApiFields(fields, 'paymentId')
  assert.deepEqual(fields, before)
})

const catalog = JSON.parse(readFileSync(new URL('../../internal/apidoc/catalog.json', import.meta.url), 'utf8'))
const monitoringOperations = catalog.operations.filter(item => item.parentPermission === 'monitoring.api.read')
const monitoringDocument = {
  title: catalog.title,
  version: catalog.version,
  groups: [{ code: 'monitoring.api.read', name: '监控数据', children: monitoringOperations.map(item => ({ code: item.permission, name: item.title, operations: [item] })) }],
}
const operationIDs = groups => groups.flatMap(group => group.children.flatMap(child => child.operations.map(item => item.id)))
const fieldAt = (fields, path) => path.reduce((field, name) => field.children.find(item => item.name === name), { children: fields })

test('reads and searches all three monitoring contracts from the backend catalog', () => {
  const parsed = parseApiDocument({ code: 0, data: { document: monitoringDocument } })
  const ids = ['monitoring-hourly', 'monitoring-daily', 'monitoring-areas']
  assert.equal(parsed.version, catalog.version)
  assert.deepEqual(operationIDs(parsed.groups), ids)
  assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, 'monitoring.api.read')), ids)
  assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, '监控数据')), ids)
  for (const item of monitoringOperations) {
    assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, item.permission)), [item.id])
    assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, item.path)), [item.id])
  }
  assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, 'avgStayTime')), ['monitoring-areas'])
  assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, 'avgVisitDuration')), ['monitoring-hourly'])
  assert.deepEqual(operationIDs(filterApiGroups(parsed.groups, 'mall_code')), ids)
})

test('keeps monitoring examples as JSON bodies with the documented open token', () => {
  for (const item of monitoringOperations) {
    const curl = buildCurl(item, 'https://api.example.com/')
    assert.ok(curl.startsWith(`curl --request POST 'https://api.example.com${item.path}'`))
    assert.match(curl, /--header 'token: dg_open_REPLACE_WITH_YOUR_TOKEN'/)
    assert.match(curl, /--header 'Content-Type: application\/json'/)
    assert.ok(curl.includes(`--data-raw '${JSON.stringify(item.requestExample, null, 2)}'`))
    assert.doesNotMatch(curl, /Bearer|Authorization|\?shopCode=/)
    assert.equal(item.requestFields.some(field => field.location === 'query'), false)
    assert.equal(item.requestExample.shopCode, 'SH001')
    if (item.id === 'monitoring-areas') {
      assert.equal(item.requestExample.date, '2026-11-01')
      assert.equal(item.requestExample.regionName, '入口区')
    } else {
      assert.deepEqual(item.requestExample.dates, ['2026-11-01', '2026-11-03'])
      assert.ok(item.requestFields.some(field => field.name === 'startDate'))
      assert.ok(item.requestFields.some(field => field.name === 'endDate'))
    }
  }
})

test('retains nested monitoring field units, coverage and partial-result examples', () => {
  const [hourly, daily, areas] = monitoringOperations
  assert.match(fieldAt(hourly.responseFields, ['data', 'items', 'avgVisitDuration']).description, /秒/)
  assert.match(fieldAt(hourly.responseFields, ['data', 'items', 'netSales']).description, /分/)
  assert.match(fieldAt(hourly.responseFields, ['data', 'items', 'captureRate']).description, /0\.25表示25%/)
  assert.equal(hourly.responseExample.data.coverage[0].complete, false)
  assert.deepEqual(hourly.responseExample.data.missingDates, ['2026-11-03'])
  assert.match(fieldAt(daily.responseFields, ['data', 'items', 'trafficCount']).description, /跨小时不去重/)
  assert.equal(daily.responseExample.data.items[0].complete, false)
  assert.match(fieldAt(areas.responseFields, ['data', 'items', 'avgStayTime']).description, /秒/)
  assert.match(fieldAt(areas.responseFields, ['data', 'items', 'stayRate']).description, /0\.25表示25%/)
  assert.equal(areas.responseExample.data.hasData, true)
})
