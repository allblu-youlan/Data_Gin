import assert from 'node:assert/strict'
import test from 'node:test'
import { buildOracleExportRequest, buildOracleExportWorkbookRequest, defaultOracleExportSheetName, moveOracleExportColumn, normalizeOracleExportFilterValue, oracleExportFilterInputType, oracleExportFilterOperatorForType, parseOracleExportColumns, parseOracleExportTables } from '../.test-dist/excelPages/oracleExportConfig.js'

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

test('defaults titles to trimmed Oracle comments and falls back for blank or absent comments', () => {
  const columns = parseOracleExportColumns({ columns: [
    { ...rawColumns[1], comment: ' 订单编号 ' },
    { ...rawColumns[0], comment: ' \n ' },
    { name: 'LEGACY', position: 3, dataType: 'VARCHAR2', nullable: true },
  ] })
  assert.deepEqual(columns.map(({ title }) => title), ['订单编号', 'TOTAL', 'LEGACY'])
  const cleared = columns.map((column) => ({ ...column, title: ' ' }))
  assert.deepEqual(buildOracleExportRequest(table, cleared, []).columns, [{ field: 'DOCNO', title: '订单编号' }, { field: 'TOTAL', title: 'TOTAL' }, { field: 'LEGACY', title: 'LEGACY' }])
  assert.equal(buildOracleExportRequest(table, [{ ...columns[0], title: '自定义订单号' }], []).columns[0].title, '自定义订单号')
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

test('Oracle Excel export submits dynamic dates, month ranges and filtered column maximum without fixing a literal value', () => {
  const columns = parseOracleExportColumns({ columns: [
    ...rawColumns,
    { name: 'BUSINESS_DATE', position: 3, dataType: 'DATE', nullable: false },
  ] })
  const filters = [
    { id: 1, field: 'TOTAL', op: 'between', value: 'old', valueSource: { kind: 'TIME_RANGE', code: 'CURRENT_MONTH' } },
    { id: 2, field: 'BUSINESS_DATE', op: 'lte', value: 'old', valueSource: { kind: 'TIME', code: 'MONTH_END' } },
    { id: 3, field: 'DOCNO', op: 'eq', value: 'old', valueSource: { kind: 'COLUMN_MAX' } },
  ]
  const request = buildOracleExportRequest(table, columns, filters)
  assert.deepEqual(request.filters, filters.map(({ id, ...filter }) => ({ ...filter, value: '' })))
  assert.throws(() => buildOracleExportRequest(table, columns, [{ ...filters[0], valueSource: undefined }]), /动态月份范围/)
  assert.throws(() => buildOracleExportRequest(table, columns, [{ ...filters[2], op: 'gte' }]), /等于/)
  assert.throws(() => buildOracleExportRequest(table, columns, [filters[2], { ...filters[2], id: 4, field: 'TOTAL' }]), /最多.*一项/)
  assert.throws(() => buildOracleExportRequest(table, columns, [{ ...filters[0], field: 'DOCNO' }]), /动态时间仅支持/)
})

test('builds independent table columns and date filters for each workbook sheet', () => {
  const columns = parseOracleExportColumns({ columns: rawColumns })
  const first = { sheetName: ' 订单 ', ...buildOracleExportRequest(table, columns, [{ id: 1, field: 'TOTAL', op: 'gte', value: '100' }]) }
  const second = { sheetName: '订单汇总', ...buildOracleExportRequest({ ...table, name: 'ORDER_SUMMARY' }, columns.map((column) => ({ ...column, selected: column.name === 'TOTAL', title: '合计' })), []) }
  const workbook = buildOracleExportWorkbookRequest([first, second])
  assert.deepEqual(workbook, { sheets: [{ ...first, sheetName: '订单' }, second] })
  assert.equal(workbook.sheets[0].filters.length, 1)
  assert.equal(workbook.sheets[1].filters.length, 0)
  assert.deepEqual(workbook.sheets[1].columns, [{ field: 'TOTAL', title: '合计' }])
})

test('defaults sheet names from table names with legal characters, length and unique suffixes', () => {
  assert.equal(defaultOracleExportSheetName('A[B]:C*D?E/F\\G'), 'A_B__C_D_E_F_G')
  assert.equal(defaultOracleExportSheetName('ORDERS', ['orders', 'ORDERS_2']), 'ORDERS_3')
  assert.equal(defaultOracleExportSheetName('A'.repeat(40)).length, 31)
  assert.equal(defaultOracleExportSheetName('A'.repeat(40), ['A'.repeat(31)]), `${'A'.repeat(29)}_2`)
})

test('rejects empty, illegal, overlong or case-insensitive duplicate sheet names and more than twenty sheets', () => {
  const sheet = { sheetName: 'Orders', owner: 'ERP', tableName: 'ORDERS', columns: [{ field: 'DOCNO', title: '订单号' }], filters: [] }
  for (const sheetName of ['', ' ', 'X'.repeat(32), "'Orders", "Orders'", 'Ord\u0007ers', ...'[]:*?/\\']) assert.throws(() => buildOracleExportWorkbookRequest([{ ...sheet, sheetName }]), /Sheet 名称/)
  assert.throws(() => buildOracleExportWorkbookRequest([sheet, { ...sheet, sheetName: 'orders' }]), /不区分大小写/)
  assert.throws(() => buildOracleExportWorkbookRequest([]), /1 至 20/)
  assert.throws(() => buildOracleExportWorkbookRequest(Array.from({ length: 21 }, (_, index) => ({ ...sheet, sheetName: `Sheet${index}` }))), /1 至 20/)
  assert.equal(buildOracleExportWorkbookRequest(Array.from({ length: 20 }, (_, index) => ({ ...sheet, sheetName: `Sheet${index}` }))).sheets.length, 20)
})


test('dropdown column positions reorder selected exports without changing titles or unselected fields', () => {
  const columns = parseOracleExportColumns({ columns: [...rawColumns, { name: 'LAST', position: 3, dataType: 'VARCHAR2', nullable: true }] })
  const reordered = moveOracleExportColumn(columns, 'LAST', 1)
  assert.deepEqual(reordered.map((column) => column.name), ['LAST', 'DOCNO', 'TOTAL'])
  assert.deepEqual(columns.map((column) => column.name), ['DOCNO', 'TOTAL', 'LAST'])
  const subset = reordered.map((column) => ({ ...column, selected: column.name !== 'TOTAL' }))
  const moved = moveOracleExportColumn(subset, 'LAST', 2)
  assert.deepEqual(buildOracleExportRequest(table, moved, []).columns.map((column) => column.field), ['DOCNO', 'LAST'])
  assert.equal(moved.find((column) => column.name === 'TOTAL').selected, false)
})
