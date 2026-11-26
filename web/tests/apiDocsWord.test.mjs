import test from 'node:test'
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFileSync } from 'node:fs'
import { buildApiWord } from '../.test-dist/apiDocsWord.js'

const requireDocx = createRequire(import.meta.resolve('docx'))
const { unzipSync, strFromU8 } = requireDocx('fflate')

const operation = {
  id: 'orders', method: 'POST', path: '/api/open/orders/query', title: '订单查询',
  description: '分页查询订单', permission: 'orders.read', parentPermission: 'commerce.read', authentication: '请求头 token，不加 Bearer 前缀。',
  requestFields: [{ name: 'token', type: 'string', required: true, location: 'header', description: '公共凭证' }, { name: 'keyword', type: 'string', required: false, nullable: true, location: 'query', description: 'A | B\n筛选值 < 10，路径 C:\\files' }],
  responseFields: [{ name: 'data', type: 'object', required: true, description: '结果', children: [{ name: 'items', type: 'object[]', required: true, description: '订单项', children: Array.from({ length: 12 }, (_, index) => ({ name: `amount${index}`, type: 'decimal(18,2)', required: true, description: `Oracle源类型：NUMBER(18,2)\n含义、关系：金额${index}\n单位/格式与使用说明：元` })) }] }],
  requestExample: { keyword: 'A | B', note: '保留 `代码` 和 \\ 路径' },
  responseExample: { code: 0, data: { items: [{ amount11: 12.5 }] } },
  errors: [{ status: 422, code: 'A|B', description: '请求失败\n请修正参数' }],
  notes: [{ title: '金额与 <范围>', body: '完整补充说明。\n\n- 按元返回。\n- 不做汇率换算。\n保留原文 \\| 和 &lt;。\n末尾内容必须保留。' }],
}
const document = { title: '开放接口文档', version: '2.1', groups: [{ code: 'commerce', name: '业务平台', children: [{ code: 'orders', name: '订单', operations: [operation, { ...operation, id: 'refunds', title: '退款查询', path: '/api/open/refunds/query' }] }] }] }

async function unpack(blob) {
  assert.equal(blob.type, 'application/vnd.openxmlformats-officedocument.wordprocessingml.document')
  const zip = unzipSync(new Uint8Array(await blob.arrayBuffer()))
  const xml = strFromU8(zip['word/document.xml'])
  const styles = strFromU8(zip['word/styles.xml'])
  const entities = { '&lt;': '<', '&gt;': '>', '&quot;': '"', '&apos;': "'", '&amp;': '&' }
  const texts = [...xml.matchAll(/<w:t(?:\s[^>]*)?>([\s\S]*?)<\/w:t>/g)]
    .map(match => match[1].replace(/&(?:lt|gt|quot|apos|amp);/g, entity => entities[entity]))
  return { xml, styles, text: texts.join('\n') }
}

test('creates editable Word headings, three-column tables and complete code examples', async () => {
  const { xml, styles, text } = await unpack(await buildApiWord(document, 'https://api.example.com', 'orders'))
  assert.ok(text.includes('订单查询'))
  assert.ok(text.includes('data.items[].amount11'))
  assert.ok(text.includes('string | null'))
  assert.ok(text.includes('query 参数。A | B'))
  assert.ok(text.includes('筛选值 < 10，路径 C:\\files'))
  assert.ok(text.includes('token: dg_open_REPLACE_WITH_YOUR_TOKEN'))
  assert.ok(text.includes('"note": "保留 `代码` 和 \\\\ 路径"'))
  assert.ok(text.includes('金额与 <范围>'))
  assert.ok(text.includes('保留原文 \\| 和 &lt;。'))
  assert.ok(text.includes('末尾内容必须保留。'))
  assert.doesNotMatch(text, /退款查询|Oracle源类型|<details>|<summary>/)
  assert.equal(text.split('授权说明').length - 1, 1)
  assert.match(xml, /<w:pStyle w:val="Title"/)
  assert.match(xml, /<w:pgSz w:w="11906" w:h="16838"/)
  assert.match(styles, /<w:color w:val="000000"/)
  const tables = [...xml.matchAll(/<w:tbl>[\s\S]*?<\/w:tbl>/g)].map(match => match[0])
  assert.equal(tables.length, 3)
  for (const table of tables) {
    assert.equal((table.match(/<w:gridCol /g) ?? []).length, 3)
    assert.match(table, /<w:tblLayout w:type="fixed"/)
    assert.match(table, /<w:tblHeader/)
    assert.match(table, /<w:tcW w:type="dxa" w:w="\d+"/)
  }
  assert.match(xml, /<w:wordWrap w:val="0"/)
  assert.match(xml, /<w:keepNext/)
  assert.doesNotMatch(xml, /<w:drawing|<w:pict/)
})

test('exports every real catalog operation and nested field without shortening notes', async () => {
  const catalog = JSON.parse(readFileSync(new URL('../../internal/apidoc/catalog.json', import.meta.url), 'utf8'))
  const fullDocument = { title: catalog.title, version: catalog.version, groups: [{ code: 'all', name: '全部接口', children: [{ code: 'catalog', name: '公开目录', operations: catalog.operations }] }] }
  const { text } = await unpack(await buildApiWord(fullDocument, 'https://api.example.com'))
  for (const item of catalog.operations) {
    assert.ok(text.includes(item.path), item.id)
    for (const note of item.notes ?? []) {
      const lastLine = note.body.trim().split('\n').at(-1)
      assert.ok(text.includes(lastLine), `${item.id}: ${note.title}`)
    }
  }
  assert.ok(text.includes('data.items[].avgVisitDuration'))
  assert.ok(text.includes('data.items[].captureRate'))
  assert.equal(text.split('授权说明').length - 1, 1)
})

test('rejects unknown operation IDs before producing a Word file', async () => {
  await assert.rejects(buildApiWord(document, '', 'missing'), /接口不存在/)
})
