import test from 'node:test'
import assert from 'node:assert/strict'
import { parseApiDocument, filterApiGroups, buildCurl } from '../.test-dist/apiDocs.js'

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
