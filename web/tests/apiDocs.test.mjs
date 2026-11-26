import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { parseApiDocument, filterApiGroups, buildCurl, buildApiDocsLink, formatApiFieldType, formatApiFieldDescription, buildApiMarkdown, apiMarkdownFilename } from '../.test-dist/apiDocs.js'

test('shows public JSON types without database widths or named object schemas', () => {
  const cases = [
    ['VARCHAR2(32 字节)', 'string'],
    ['decimal(18, 2)', 'number'],
    ['NUMBER(38,18)', 'number'],
    ['bigint unsigned', 'integer'],
    ['integer(int64)', 'integer'],
    ['datetime', 'string'],
    ['boolean', 'boolean'],
    ['WeatherMeta', 'object'],
    ['QualityWarning[]', 'object[]'],
    ['string[]', 'string[]'],
    ['array<object>', 'object[]'],
    ['array<string / integer>', '(string | integer)[]'],
    ['integer / null', 'integer | null'],
    ['object / null', 'object | null'],
  ]
  for (const [source, expected] of cases) assert.equal(formatApiFieldType(source), expected, source)
})

test('removes storage metadata while retaining field meaning, filtering and units', () => {
  const description = 'Oracle源类型：NUMBER(18,2)\n精确filters：否\n含义、关系：商品价格；按源值返回。\n单位/格式与使用说明：人民币元，不换算'
  assert.equal(formatApiFieldDescription(description), '精确筛选：不支持\n商品价格；按源值返回。\n人民币元，不换算')
  assert.equal(formatApiFieldDescription('Oracle源类型：VARCHAR2(32 字节)\n精确filters：是\n含义、关系：主订单编号，不可单独去重。\n单位/格式与使用说明：标识字符串'), '精确筛选：支持\n主订单编号，不可单独去重。')
})

test('keeps missing-value and date semantics without repeating field-presence metadata', () => {
  assert.equal(formatApiFieldDescription('空值 / 可选性：字段必返，不为 null\n开始时间，YYYY-MM-DD HH:mm:ss'), '开始时间，YYYY-MM-DD HH:mm:ss')
  assert.equal(formatApiFieldDescription('空值 / 可选性：源数据库NULL时返回空字符串\n店仓名称'), '为空时返回空字符串\n店仓名称')
  assert.equal(formatApiFieldDescription('空值 / 可选性：值缺失时省略字段，不返回 null\n生效时间'), '值缺失时省略字段，不返回 null\n生效时间')
  assert.equal(formatApiFieldDescription('访客数，跨小时不去重。'), '访客数，跨小时不去重。')
})

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

const exportOperation = {
  ...operation,
  path: '/api/open/orders/:id',
  parentPermission: 'commerce.read',
  requestFields: [
    { name: 'TOKEN', type: 'string', required: true, location: 'header', description: '公共凭证' },
    { name: 'Content-Type', type: 'string', required: true, location: 'header', description: 'application/json' },
    { name: 'X-Trace', type: 'string', required: false, location: 'header', description: '跟踪标识' },
    { name: 'id', type: 'integer(int64)', required: true, nullable: false, location: 'path', description: '订单编号', example: 12 },
    { name: 'keyword', type: 'string', required: false, nullable: true, location: 'query', description: 'A | B\r\n筛选值 < 10', example: 'red bag' },
    { name: 'token', type: 'string', required: false, location: 'body', description: '业务令牌字段，非请求头' },
  ],
  responseFields: [{ name: 'data', type: 'object', required: true, description: '结果', children: [{ name: 'items', type: 'object[]', required: true, description: '订单项', children: [{ name: 'amount', type: 'decimal(18, 2)', required: true, nullable: false, description: 'Oracle源类型：NUMBER(18,2)\n含义、关系：实付金额\n单位/格式与使用说明：元' }] }] }],
  errors: [{ status: 422, code: 'A|B', description: '请求失败\n请修正参数' }],
  notes: [{ title: '金额与 <范围>', body: '完整补充说明。\n\n- 金额按元返回。\n- 不做汇率换算。' }],
}
const exportDocument = { ...document, groups: [{ ...document.groups[0], children: [{ ...document.groups[0].children[0], operations: [exportOperation, { ...operation, id: 'refunds', title: '退款查询', path: '/api/open/refunds/query' }] }] }, { code: 'weather', name: '天气服务', children: [{ code: 'current', name: '当前天气', operations: [{ ...operation, id: 'weather', title: '天气查询', path: '/api/open/weather/query' }] }] }] }

test('exports all groups with one shared authorization section and complete operation examples', () => {
  const markdown = buildApiMarkdown(exportDocument, 'https://api.example.com/')
  assert.equal(markdown.split('## 授权说明').length - 1, 1)
  assert.ok(markdown.includes('token: dg_open_REPLACE_WITH_YOUR_TOKEN'))
  for (const heading of ['## 业务平台', '### 订单', '#### 订单查询', '#### 退款查询', '## 天气服务', '### 当前天气', '#### 天气查询']) assert.ok(markdown.includes(heading), heading)
  assert.ok(markdown.includes('orders.read'))
  assert.ok(markdown.includes('commerce.read'))
  assert.ok(markdown.includes(buildCurl(exportOperation, 'https://api.example.com/')))
  assert.ok(markdown.includes(JSON.stringify(exportOperation.requestExample, null, 2)))
  assert.ok(markdown.includes(JSON.stringify(exportOperation.responseExample, null, 2)))
  assert.ok(markdown.includes(exportOperation.notes[0].body))
})

test('exports complete field paths and preserves input locations and nullability', () => {
  const markdown = buildApiMarkdown(exportDocument, 'https://api.example.com', 'orders')
  assert.ok(markdown.includes('| 字段 | 类型 | 含义 |'))
  assert.ok(markdown.includes('| data.items[].amount | number | 实付金额<br>元 |'))
  assert.ok(markdown.includes('| id | integer | 必填 · path 参数。订单编号 |'))
  assert.ok(markdown.includes('| keyword | string \\| null | query 参数。A \\| B<br>筛选值 &lt; 10 |'))
  assert.ok(markdown.includes('| X-Trace | string | header 参数。跟踪标识 |'))
  assert.ok(markdown.includes('| token | string | 业务令牌字段，非请求头 |'))
  assert.doesNotMatch(markdown, /\| TOKEN \||\| Content-Type \||Oracle源类型|NUMBER\(18,2\)/)
  assert.ok(markdown.includes('| 422 | A\\|B | 请求失败<br>请修正参数 |'))
  assert.ok(markdown.includes('<summary>金额与 &lt;范围&gt;</summary>'))
})

test('keeps optional response semantics and omits request JSON when there is no request body', () => {
  const noBody = { ...operation, method: 'GET', requestExample: {}, responseFields: [{ name: 'discount', type: 'number / null', required: false, nullable: true, description: '优惠金额' }] }
  const noBodyDocument = { ...document, groups: [{ ...document.groups[0], children: [{ ...document.groups[0].children[0], operations: [noBody] }] }] }
  const markdown = buildApiMarkdown(noBodyDocument, 'https://api.example.com')
  assert.doesNotMatch(markdown, /##### 请求 JSON|--data-raw/)
  assert.ok(markdown.includes('| discount | number \\| null | 可能省略。优惠金额 |'))
  assert.ok(markdown.includes('##### cURL 示例'))
  assert.ok(markdown.includes('##### 响应 JSON'))
})

test('exports only the requested operation and rejects unknown IDs', () => {
  const markdown = buildApiMarkdown(exportDocument, 'https://api.example.com', 'orders')
  assert.ok(markdown.includes('#### 订单查询'))
  assert.doesNotMatch(markdown, /退款查询|天气服务|天气查询/)
  assert.equal(markdown.split('## 授权说明').length - 1, 1)
  assert.throws(() => buildApiMarkdown(exportDocument, '', 'missing'), /接口不存在/)
  assert.throws(() => buildApiMarkdown(exportDocument, '', ''), /接口不存在/)
  assert.equal(exportDocument.groups[0].children[0].operations.length, 2)
})

test('names full and single Markdown downloads without filesystem separators', () => {
  assert.equal(apiMarkdownFilename(document), '接口文档-v1.md')
  assert.equal(apiMarkdownFilename(document, 'orders'), '接口文档-orders-v1.md')
  assert.doesNotMatch(apiMarkdownFilename({ ...document, title: '开放/接口:文档?', version: '2/1' }), /[\\/:*?"<>|]/)
  assert.throws(() => apiMarkdownFilename(document, 'missing'), /接口不存在/)
})

test('exports the entire real catalog without truncating deeply nested fields or notes', () => {
  const fullDocument = { title: catalog.title, version: catalog.version, groups: [{ code: 'all', name: '全部接口', children: [{ code: 'catalog', name: '公开目录', operations: catalog.operations }] }] }
  const markdown = buildApiMarkdown(fullDocument, 'https://api.example.com')
  assert.equal((markdown.match(/^#### /gm) ?? []).length, catalog.operations.length)
  assert.equal((markdown.match(/^##### 请求参数$/gm) ?? []).length, catalog.operations.length)
  assert.equal((markdown.match(/^##### 响应字段$/gm) ?? []).length, catalog.operations.length)
  const fieldCount = fields => fields.reduce((total, field) => total + 1 + fieldCount(field.children ?? []), 0)
  const responseTables = [...markdown.matchAll(/##### 响应字段\n\n([\s\S]*?)\n\n##### 响应 JSON/g)]
  assert.equal(responseTables.reduce((total, match) => total + match[1].split('\n').length - 2, 0), catalog.operations.reduce((total, item) => total + fieldCount(item.responseFields), 0))
  for (const item of catalog.operations) {
    assert.ok(markdown.includes(item.path), item.id)
    assert.ok(markdown.includes(JSON.stringify(item.responseExample, null, 2)), item.id)
    for (const note of item.notes ?? []) assert.ok(markdown.includes(note.body), `${item.id}: ${note.title}`)
  }
  assert.ok(markdown.includes('data.items[].avgVisitDuration'))
  assert.ok(markdown.includes('data.items[].captureRate'))
})
