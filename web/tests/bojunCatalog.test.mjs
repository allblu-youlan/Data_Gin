import assert from 'node:assert/strict'
import test from 'node:test'
import { getBojunColumns, getBojunReferenceTable, getBojunTables, getReportRecordDetail, getReportRelationOptions, parseBojunTable, saveBojunTableConfiguration, startBojunSync } from '../.test-dist/reportCenter/bojunApi.js'

const table = { id: '1', bojunTableId: '12888', logicalName: 'C_STORE', primaryKeys: ['10'], displayFields: ['11'], searchFields: ['12'], enabled: true, config: { primaryKeys: null, displayFields: null, searchFields: [], selectableColumns: ['10', '11'], detailColumns: [], requiredFilterColumns: ['12'], dataScope: { mode: 'SCOPE_COLUMN', columnId: '12', userValues: { '7': ['001', '002'] } } } }
test('catalog parses local string ids and keeps inherited, empty and approved field overrides distinct', () => {
 const parsed = parseBojunTable({ data: table })
 assert.equal(parsed.id, 1)
 assert.equal(parsed.displayFields[0], '11')
 assert.equal(parsed.config.displayFields, null)
 assert.deepEqual(parsed.config.searchFields, [])
 assert.deepEqual(parsed.config.selectableColumns, ['10', '11'])
 assert.deepEqual(parsed.config.detailColumns, [])
 assert.deepEqual(parsed.dataScope.userValues, { '7': ['001', '002'] })
})
test('catalog cursor queries are bounded and table sync uses the controller numeric local id', async () => {
 const calls = []
 const client = async (path, options) => { calls.push({ path, options }); return { ok: true, status: 200, data: { data: path.includes('sync-runs') ? { syncRunId: 8, status: 'QUEUED' } : { items: [], hasMore: false, nextAfter: '', generation: 2 } } } }
 assert.equal((await getBojunTables(client, 3, { search: '零售', after: 'signed cursor', limit: 1000 })).ok, true)
 assert.match(calls[0].path, /limit=200/)
 assert.match(calls[0].path, /after=signed\+cursor/)
 await getBojunColumns(client, 3, '1', { after: '123', limit: 1000 })
 assert.match(calls[1].path, /limit=200/)
 await startBojunSync(client, 3, '1')
 assert.deepEqual(calls[2].options.body, { scope: 'TABLE', tableId: 1 })
})
test('designer requests available tables while dictionary management can request all rows', async () => {
 const paths = []
 const client = async (path) => { paths.push(path); return { ok: true, status: 200, data: { data: { items: [], hasMore: false, nextAfter: '', generation: 0 } } } }
 await getBojunTables(client, 3, { availableOnly: true })
 await getBojunTables(client, 3, { availableOnly: false, after: 'approved-page' })
 assert.equal(new URL(paths[0], 'https://local.test').searchParams.get('availableOnly'), 'true')
 assert.equal(new URL(paths[1], 'https://local.test').searchParams.has('availableOnly'), false)
 assert.equal(new URL(paths[1], 'https://local.test').searchParams.get('after'), 'approved-page')
})
test('reference navigation resolves the local field through the server and preserves cancellation', async () => {
 const calls = []
 const controller = new AbortController()
 const client = async (path, options) => { calls.push({ path, options }); return { ok: true, status: 200, data: { data: table } } }
 const response = await getBojunReferenceTable(client, 3, '17', '29', controller.signal)
 assert.equal(calls[0].path, '/v1/report-datasources/3/bojun-tables/17/columns/29/reference')
 assert.equal(calls[0].options.method, 'GET')
 assert.equal(calls[0].options.signal, controller.signal)
 assert.equal(response.ok, true)
 assert.equal(response.data.id, 1)
 assert.equal(response.data.bojunTableId, '12888')
 assert.equal(response.data.logicalName, 'C_STORE')
})
test('catalog configuration preserves explicit field restrictions and administrator scopes', async () => {
 let sent
 const parsed = parseBojunTable(table)
 const response = await saveBojunTableConfiguration(async (_path, options) => { sent = options.body; return { ok: true, status: 200, data: { data: table } } }, 3, '1', { ...parsed.config, enabled: true, expectedLockVersion: 2, dataScope: parsed.dataScope })
 assert.equal(response.ok, true)
 assert.deepEqual(sent.selectableColumns, ['10', '11'])
 assert.deepEqual(sent.detailColumns, [])
 assert.equal(sent.displayFields, null)
 assert.deepEqual(sent.dataScope.userValues, { '7': ['001', '002'] })
})
test('relation options preserve raw decimal text and detail requests use only run row context', async () => {
 const calls = []
 const client = async (path, options) => { calls.push({ path, options }); return { ok: true, status: 200, data: { data: path.includes('record-details') ? { mode: 'SNAPSHOT', detailSource: 'SNAPSHOT', fields: [{ columnId: '11', name: 'NAME', label: '名称', displayValue: '店仓' }] } : { items: [{ keyValues: { '10': '00012345678901234567890' }, displayValue: '店仓' }], hasMore: false, nextCursor: '' } } } }
 const result = await getReportRelationOptions(client, 9, { versionId: 21, relationId: 'store', search: '店仓' })
 assert.equal(result.data.items[0].keyValues['10'], '00012345678901234567890')
 const detail = await getReportRecordDetail(client, 31, 'signed-row', 'store', 'SNAPSHOT')
 assert.deepEqual(calls[1].options.body, { rowKey: 'signed-row', relationId: 'store', mode: 'SNAPSHOT' })
 assert.equal(Object.hasOwn(detail.data.fields[0], 'rawValue'), false)
})
test('relation option pages carry current dependency values without changing precise text keys', async () => {
 const calls = []
 const client = async (_path, options) => { calls.push(options.body); return { ok: true, status: 200, data: { data: { items: [], hasMore: false, nextCursor: '' } } } }
 const dependencyValues = { date: '20261020', identifiers: ['00012345678901234567890'], region: '华东' }
 await getReportRelationOptions(client, 9, { versionId: 21, relationId: 'store', search: '店仓', dependencyValues })
 await getReportRelationOptions(client, 9, { versionId: 21, relationId: 'store', search: '店仓', dependencyValues, cursor: 'signed-page' })
 assert.deepEqual(calls[0].dependencyValues, dependencyValues)
 assert.deepEqual(calls[1].dependencyValues, dependencyValues)
 assert.equal(calls[1].cursor, 'signed-page')
})
