import assert from 'node:assert/strict'
import test from 'node:test'
import { parseOracleExportColumns } from '../.test-dist/excelPages/oracleExportConfig.js'
import { deleteOracleExportScheme, getOracleExportSchemes, parseOracleExportSchemes, restoreOracleExportConfig, saveOracleExportScheme } from '../.test-dist/excelPages/oracleExportSchemes.js'

const config = { owner: 'ARCHIVE', tableName: 'OLD_ORDERS', columns: [{ field: 'DOCNO', title: '订单号' }], filters: [{ field: 'CREATED_AT', op: 'gte', value: '2026-10-01 08:30:00' }] }
const scheme = { id: 7, name: '十月订单', operation: 'oracle_export', config: { operation: 'oracle_export', ...config }, updated_at: 1792548000 }
const metadata = parseOracleExportColumns({ columns: [{ name: 'DOCNO', position: 1, dataType: 'VARCHAR2', nullable: false }, { name: 'CREATED_AT', position: 2, dataType: 'TIMESTAMP(6)', nullable: true }, { name: 'ADDED_LATER', position: 3, dataType: 'NUMBER', nullable: true }] })

test('loads persisted export configuration through its dedicated API and retains a table outside the catalog', async () => {
  const requests = []
  const client = async (path, options) => { requests.push({ path, method: options.method }); return { ok: true, data: { data: { schemes: [scheme] } } } }
  const schemes = await getOracleExportSchemes(client)
  assert.deepEqual(requests, [{ path: '/v1/excel-match-jobs/oracle-export-schemes', method: 'GET' }])
  assert.equal(schemes[0].config.sheets[0].tableName, 'OLD_ORDERS')
  assert.deepEqual(schemes[0].config, { sheets: [{ sheetName: 'OLD_ORDERS', ...config }] })
})

test('restores exactly saved selection and titles without selecting newly added columns', () => {
  const restored = restoreOracleExportConfig(config, metadata)
  assert.deepEqual(restored.columns.map(({ name, selected, title }) => ({ name, selected, title })), [{ name: 'DOCNO', selected: true, title: '订单号' }, { name: 'CREATED_AT', selected: false, title: 'CREATED_AT' }, { name: 'ADDED_LATER', selected: false, title: 'ADDED_LATER' }])
  assert.deepEqual(restored.filters, [{ id: 1, field: 'CREATED_AT', op: 'gte', value: '2026-10-01T08:30:00' }])
  assert.deepEqual(restored.issues, [])
})

test('restores blank saved titles from comments while preserving every nonempty custom title', () => {
  const commented = metadata.map((column) => ({ ...column, comment: column.name === 'DOCNO' ? ' Oracle订单注释 ' : ' ' }))
  const saved = { ...config, columns: [{ field: 'DOCNO', title: ' ' }, { field: 'CREATED_AT', title: '' }] }
  const restored = restoreOracleExportConfig(saved, commented)
  assert.equal(restored.columns[0].title, 'Oracle订单注释')
  assert.equal(restored.columns[1].title, 'CREATED_AT')
  assert.equal(restoreOracleExportConfig(config, commented).columns[0].title, '订单号')
  assert.equal(restoreOracleExportConfig({ ...saved, columns: [{ field: 'DOCNO', title: 'DOCNO' }] }, commented).columns[0].title, 'DOCNO')
})

test('reports vanished selected and filter fields rather than widening saved exports', () => {
  const missing = { ...config, columns: [{ field: 'REMOVED', title: '已移除' }], filters: [{ field: 'OLD_DATE', op: 'gte', value: '2026-10-01' }] }
  const restored = restoreOracleExportConfig(missing, metadata)
  assert.equal(restored.columns.some((column) => column.selected), false)
  assert.deepEqual(restored.issues, ['导出字段已失效：REMOVED', '筛选字段已失效：OLD_DATE'])
  assert.equal(restored.filters[0].field, 'OLD_DATE')
})

test('saves configuration without creating an export job and deletes only the selected configuration', async () => {
  const requests = []
  const client = async (path, options) => { requests.push({ path, method: options.method, body: options.body }); return { ok: true, data: { data: { scheme } } } }
  const workbook = { sheets: [{ sheetName: '订单', ...config }, { ...config, sheetName: '汇总', tableName: 'TOTALS', filters: [] }] }
  const saved = await saveOracleExportScheme(client, ' 十月订单 ', workbook)
  await deleteOracleExportScheme(client, saved.id)
  assert.deepEqual(requests, [{ path: '/v1/excel-match-jobs/oracle-export-schemes', method: 'POST', body: { name: '十月订单', config: workbook } }, { path: '/v1/excel-match-jobs/oracle-export-schemes/7', method: 'DELETE', body: undefined }])
})

test('rejects incomplete persisted configurations and surfaces save failures', async () => {
  assert.throws(() => parseOracleExportSchemes({ schemes: [{ ...scheme, config: { ...config, filters: [{ field: 'DOCNO', op: 'raw_sql', value: 'x' }] } }] }), /筛选条件/)
  await assert.rejects(saveOracleExportScheme(async () => ({ ok: false, error: { message: '无管理权限' } }), '十月订单', { sheets: [{ sheetName: '订单', ...config }] }), /无管理权限/)
})

test('parses all sheets from saved workbooks and keeps their independent titles and filters', () => {
  const sheets = [{ sheetName: '订单', ...config }, { ...config, sheetName: '汇总', tableName: 'TOTALS', columns: [{ field: 'ADDED_LATER', title: '合计' }], filters: [] }]
  const parsed = parseOracleExportSchemes({ schemes: [{ ...scheme, config: { sheets } }] })[0]
  assert.deepEqual(parsed.config.sheets, sheets)
  assert.equal(restoreOracleExportConfig(parsed.config.sheets[0], metadata).columns[0].selected, true)
  const summary = restoreOracleExportConfig(parsed.config.sheets[1], metadata)
  assert.deepEqual(summary.columns.filter((column) => column.selected).map((column) => [column.name, column.title]), [['ADDED_LATER', '合计']])
  assert.deepEqual(summary.filters, [])
  assert.throws(() => parseOracleExportSchemes({ schemes: [{ ...scheme, config: { ...config, sheets: [] } }] }), /1 至 20/)
})

test('Oracle export schemes save, load and restore computed filter sources for the actual Excel export page', async () => {
  const filters = [
    { field: 'CREATED_AT', op: 'between', value: '', valueSource: { kind: 'TIME_RANGE', code: 'PREVIOUS_MONTH' } },
    { field: 'DOCNO', op: 'eq', value: '', valueSource: { kind: 'COLUMN_MAX' } },
  ]
  const workbook = { sheets: [{ sheetName: '动态订单', ...config, filters }] }
  const calls = []
  const client = async (path, options) => { calls.push({ path, body: options.body }); return { ok: true, data: { data: { scheme: { ...scheme, config: workbook } } } } }
  const saved = await saveOracleExportScheme(client, '动态订单', workbook)
  assert.deepEqual(calls[0].body.config.sheets[0].filters, filters)
  assert.deepEqual(saved.config.sheets[0].filters, filters)
  assert.deepEqual(restoreOracleExportConfig(saved.config.sheets[0], metadata).filters, filters.map((filter, index) => ({ ...filter, id: index + 1 })))
  assert.throws(() => parseOracleExportSchemes({ schemes: [{ ...scheme, config: { ...config, filters: [{ field: 'CREATED_AT', op: 'gte', value: '', valueSource: { kind: 'TIME', code: 'unknown' } }] } }] }), /来源无效/)
})


test('restoring an export scheme follows its saved column order rather than Oracle metadata order', () => {
  const saved = { ...config, columns: [{ field: 'ADDED_LATER', title: '第三字段放第一列' }, { field: 'DOCNO', title: '订单号' }] }
  const restored = restoreOracleExportConfig(saved, metadata)
  assert.deepEqual(restored.columns.map((column) => column.name), ['ADDED_LATER', 'DOCNO', 'CREATED_AT'])
  assert.deepEqual(restored.columns.filter((column) => column.selected).map((column) => column.title), ['第三字段放第一列', '订单号'])
})
