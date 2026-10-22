import assert from 'node:assert/strict'
import test from 'node:test'
import { reportFilterFields, setReportFilterEnabled, moveReportFilter } from '../.test-dist/reportCenter/components/ReportQueryDesigner/filterConfig.js'
import { previewReportQuery, validateReportQuery } from '../.test-dist/reportCenter/bojunApi.js'

function fixture() {
  const column = (id, description, overrides = {}) => ({ id, description, dbName: `COL_${id}`, logicalType: 'string', isPhysical: true, isSelectable: true, supportedOperators: ['EQ', 'CONTAINS'], ...overrides })
  const table = { enabled: true, validationStatus: 'VALID', logicalName: 'M_RETAIL', description: '零售单', config: {} }
  const metadata = { '1': { table, columns: [column(11, '单号'), column(12, '虚拟', { isPhysical: false }), column(13, '不可查询', { supportedOperators: [] }), column(14, '日期')] } }
  const spec = { base: { aliasId: 'base', tableId: '1' }, relations: [{ id: 'other', fromAliasId: 'base', targetTableId: '1', name: '退货单' }], columns: [{ source: { kind: 'BASE_COLUMN', columnId: '11' } }], where: { logic: 'AND', items: [] }, orderBy: [], allowedFilters: [] }
  return { spec, metadata }
}

test('filter catalog uses available physical fields and distinguishes aliases of one table', () => {
  const { spec, metadata } = fixture()
  const fields = reportFilterFields(spec, metadata)
  assert.equal(fields.length, 4)
  assert.deepEqual(fields.map((field) => [field.aliasId, field.columnId, field.tableLabel, field.isOutput]), [['base', '11', '零售单', true], ['base', '14', '零售单', false], ['other', '11', '退货单', false], ['other', '14', '退货单', false]])
  metadata['1'].table.config.selectableColumns = ['14']
  assert.equal(reportFilterFields(spec, metadata).length, 2)
  metadata['1'].table.enabled = false
  assert.deepEqual(reportFilterFields(spec, metadata), [])
})

test('selecting filter fields inherits verified operators and keeps output and fixed rules intact', () => {
  const { spec, metadata } = fixture()
  const fields = reportFilterFields(spec, metadata)
  let next = setReportFilterEnabled(spec, fields[0], true)
  next = setReportFilterEnabled(next, fields[2], true)
  assert.deepEqual(next.allowedFilters.map((field) => [field.aliasId, field.columnId, field.label, field.operators]), [['base', '11', '零售单 · 单号', ['EQ', 'CONTAINS']], ['other', '11', '退货单 · 单号', ['EQ', 'CONTAINS']]])
  assert.equal(setReportFilterEnabled(next, fields[0], true), next)
  assert.equal(next.columns, spec.columns)
  assert.equal(next.where, spec.where)
  assert.equal(next.orderBy, spec.orderBy)
  assert.deepEqual(spec.allowedFilters, [])
  next = setReportFilterEnabled(next, fields[0], false)
  assert.deepEqual(next.allowedFilters.map((field) => field.aliasId), ['other'])
})

test('filter order can be changed without resetting labels or approved operators', () => {
  const { spec, metadata } = fixture()
  const fields = reportFilterFields(spec, metadata)
  const selected = fields.slice(0, 2).reduce((query, field) => setReportFilterEnabled(query, field, true), spec)
  selected.allowedFilters[0].label = '业务单号'
  const next = moveReportFilter(selected, 0, 1)
  assert.deepEqual(next.allowedFilters.map((field) => field.label), ['零售单 · 日期', '业务单号'])
  assert.deepEqual(selected.allowedFilters.map((field) => field.label), ['业务单号', '零售单 · 日期'])
  assert.equal(moveReportFilter(selected, 0, -1), selected)
})

test('validation and sample preview follow the controller version contract and carry query filters', async () => {
  const calls = []
  const controller = new AbortController()
  const client = async (path, options) => { calls.push({ path, options }); return { ok: true, status: 200, data: { data: path.includes('validations') ? { valid: true, errors: [], warnings: [] } : { sampleOnly: true, rows: [], columns: [] } } } }
  const queryInputs = { where: { logic: 'AND', items: [{ aliasId: 'base', columnId: '11', operator: 'EQ', value: { kind: 'CONSTANT', value: '001' } }] }, orderBy: [] }
  assert.equal((await validateReportQuery(client, 9, 7)).ok, true)
  assert.deepEqual(calls[0].options.body, { expectedLockVersion: 7 })
  assert.equal((await previewReportQuery(client, 9, 7, {}, controller.signal, queryInputs)).ok, true)
  assert.deepEqual(calls[1].options.body, { expectedLockVersion: 7, conditions: {}, queryInputs })
  assert.equal(calls[1].options.signal, controller.signal)
})
