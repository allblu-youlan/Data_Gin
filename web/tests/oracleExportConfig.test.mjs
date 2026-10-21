import assert from 'node:assert/strict'
import test from 'node:test'
import { buildOracleExportRequest, parseOracleExportColumns, parseOracleExportTables } from '../.test-dist/excelPages/oracleExportConfig.js'

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
