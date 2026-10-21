import assert from 'node:assert/strict'
import test from 'node:test'
import { buildOracleExportRequest, normalizeOracleExportFilterValue, oracleExportFilterInputType, oracleExportFilterOperatorForType, parseOracleExportColumns, parseOracleExportTables } from '../.test-dist/excelPages/oracleExportConfig.js'

const table = { owner: 'ERP', name: 'ORDERS', columnCount: 2 }
const rawColumns = [
  { name: 'TOTAL', position: 2, dataType: 'NUMBER', nullable: true },
  { name: 'DOCNO', position: 1, dataType: 'VARCHAR2', nullable: false },
]

test('defaults to every column in Oracle order and all rows with original column titles', () => {
  const columns = parseOracleExportColumns({ data: { columns: rawColumns } })
  assert.deepEqual(buildOracleExportRequest(table, columns, []), {
    owner: 'ERP', tableName: 'ORDERS',
    columns: [{ field: 'DOCNO', title: 'DOCNO' }, { field: 'TOTAL', title: 'TOTAL' }], filters: [],
  })
  assert.deepEqual(parseOracleExportTables({ data: { tables: [table] } }), [table])
})

test('exports selected columns with custom titles and can filter an unexported field', () => {
  const columns = parseOracleExportColumns({ columns: rawColumns }).map((column) => ({ ...column, selected: column.name === 'DOCNO', title: column.name === 'DOCNO' ? ' 订单号 ' : column.name }))
  assert.deepEqual(buildOracleExportRequest(table, columns, [{ id: 1, field: 'TOTAL', op: 'gte', value: ' 100 ' }]), {
    owner: 'ERP', tableName: 'ORDERS', columns: [{ field: 'DOCNO', title: '订单号' }],
    filters: [{ field: 'TOTAL', op: 'gte', value: '100' }],
  })
})

test('restores blank column titles and retains zero-prefixed text filter values', () => {
  const columns = parseOracleExportColumns({ columns: rawColumns }).map((column) => ({ ...column, title: ' ' }))
  const request = buildOracleExportRequest(table, columns, [{ id: 1, field: 'DOCNO', op: 'eq', value: '00123' }])
  assert.equal(request.columns[0].title, 'DOCNO')
  assert.equal(request.filters[0].value, '00123')
})

test('drops stale comparison text when changing a filter to empty or not empty', () => {
  const columns = parseOracleExportColumns({ columns: rawColumns })
  const request = buildOracleExportRequest(table, columns, [{ id: 1, field: 'TOTAL', op: 'empty', value: '100' }, { id: 2, field: 'DOCNO', op: 'not_empty', value: 'old' }])
  assert.deepEqual(request.filters, [{ field: 'TOTAL', op: 'empty', value: '' }, { field: 'DOCNO', op: 'not_empty', value: '' }])
})

test('prevents accidental all-column export after deselecting every field and rejects incomplete filters', () => {
  const columns = parseOracleExportColumns({ columns: rawColumns })
  assert.throws(() => buildOracleExportRequest(table, columns.map((column) => ({ ...column, selected: false })), []), /至少选择一个/)
  assert.throws(() => buildOracleExportRequest(table, columns, [{ id: 1, field: '', op: 'eq', value: '1' }]), /有效的筛选字段/)
  assert.throws(() => buildOracleExportRequest(table, columns, [{ id: 1, field: 'DOCNO', op: 'eq', value: ' ' }]), /填写筛选值/)
})

test('surfaces incomplete Oracle metadata instead of silently exporting an incomplete field list', () => {
  assert.throws(() => parseOracleExportColumns({ columns: [{ name: 'DOCNO' }] }), /字段目录/)
  assert.throws(() => parseOracleExportTables({ data: {} }), /目录响应/)
})

test('selects datetime controls for Oracle DATE and TIMESTAMP variants while retaining numeric and text controls', () => {
  assert.equal(oracleExportFilterInputType('DATE'), 'datetime-local')
  assert.equal(oracleExportFilterInputType('TIMESTAMP(6) WITH TIME ZONE'), 'datetime-local')
  assert.equal(oracleExportFilterInputType('NUMBER(12,2)'), 'number')
  assert.equal(oracleExportFilterInputType('VARCHAR2'), 'text')
})

test('submits DATE and TIMESTAMP range filters as database local time with seconds', () => {
  const columns = parseOracleExportColumns({ columns: [{ name: 'CREATED_AT', position: 1, dataType: 'DATE', nullable: false }] })
  const request = buildOracleExportRequest(table, columns, [{ id: 1, field: 'CREATED_AT', op: 'gte', value: '2026-10-01T08:30' }, { id: 2, field: 'CREATED_AT', op: 'lte', value: '2026-10-21T18:45:59' }])
  assert.deepEqual(request.filters, [{ field: 'CREATED_AT', op: 'gte', value: '2026-10-01 08:30:00' }, { field: 'CREATED_AT', op: 'lte', value: '2026-10-21 18:45:59' }])
  assert.equal(normalizeOracleExportFilterValue('2026-10-01', 'DATE'), '2026-10-01 00:00:00')
  assert.equal(normalizeOracleExportFilterValue('00123', 'VARCHAR2'), '00123')
  assert.throws(() => normalizeOracleExportFilterValue('昨天', 'DATE'), /日期时间筛选值格式/)
})

test('keeps contains for text and switches it to equality when choosing number or time fields', () => {
  assert.equal(oracleExportFilterOperatorForType('contains', 'VARCHAR2'), 'contains')
  assert.equal(oracleExportFilterOperatorForType('contains', 'NUMBER'), 'eq')
  assert.equal(oracleExportFilterOperatorForType('contains', 'DATE'), 'eq')
  assert.equal(oracleExportFilterOperatorForType('gte', 'TIMESTAMP(6)'), 'gte')
})
