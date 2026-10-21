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
  assert.equal(schemes[0].config.tableName, 'OLD_ORDERS')
  assert.deepEqual(schemes[0].config, config)
})

test('restores exactly saved selection and titles without selecting newly added columns', () => {
  const restored = restoreOracleExportConfig(config, metadata)
  assert.deepEqual(restored.columns.map(({ name, selected, title }) => ({ name, selected, title })), [{ name: 'DOCNO', selected: true, title: '订单号' }, { name: 'CREATED_AT', selected: false, title: 'CREATED_AT' }, { name: 'ADDED_LATER', selected: false, title: 'ADDED_LATER' }])
  assert.deepEqual(restored.filters, [{ id: 1, field: 'CREATED_AT', op: 'gte', value: '2026-10-01T08:30:00' }])
  assert.deepEqual(restored.issues, [])
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
  const saved = await saveOracleExportScheme(client, ' 十月订单 ', config)
  await deleteOracleExportScheme(client, saved.id)
  assert.deepEqual(requests, [{ path: '/v1/excel-match-jobs/oracle-export-schemes', method: 'POST', body: { name: '十月订单', config } }, { path: '/v1/excel-match-jobs/oracle-export-schemes/7', method: 'DELETE', body: undefined }])
})

test('rejects incomplete persisted configurations and surfaces save failures', async () => {
  assert.throws(() => parseOracleExportSchemes({ schemes: [{ ...scheme, config: { ...config, filters: [{ field: 'DOCNO', op: 'raw_sql', value: 'x' }] } }] }), /筛选条件/)
  await assert.rejects(saveOracleExportScheme(async () => ({ ok: false, error: { message: '无管理权限' } }), '十月订单', config), /无管理权限/)
})
