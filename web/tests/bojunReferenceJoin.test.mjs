import assert from 'node:assert/strict'
import test from 'node:test'
import { parseBojunTable } from '../.test-dist/reportCenter/bojunApi.js'
import { joinReportReferencePath } from '../.test-dist/reportCenter/components/ReportQueryDesigner/referenceJoin.js'

function column(id, bojunColumnId, refTableId = '', refColumnId = '', logicalType = 'integer') {
 return { id, bojunColumnId, refTableId, refColumnId, logicalType, dbName: `COL_${id}`, description: `字段 ${id}`, isPhysical: true, isSelectable: true, nullable: false, supportedOperators: [], ordinal: id }
}
function table(id, bojunTableId, primaryKeys, displayFields, columns) {
 return { table: { id, bojunTableId, logicalName: `TABLE_${id}`, description: `业务表 ${id}`, primaryKeys, uniqueKeys: [primaryKeys], displayFields, searchFields: [], enabled: true, validationStatus: 'VALID', config: {} }, columns }
}
function fixture() {
 const base = table(1, 'dict_order', ['10'], ['10'], [column(10, 'dict_order_id'), column(11, 'store_fk', 'dict_store', 'dict_store_id'), column(12, 'warehouse_fk', 'dict_store', 'dict_store_id')])
 const store = table(2, 'dict_store', ['21'], ['22'], [column(21, 'dict_store_id'), column(22, 'dict_store_name', '', '', 'string'), column(23, 'region_fk', 'dict_region', 'dict_region_id')])
 const region = table(3, 'dict_region', ['31'], ['32'], [column(31, 'dict_region_id'), column(32, 'dict_region_name', '', '', 'string'), column(33, 'country_fk', 'dict_country', 'dict_country_id')])
 const country = table(4, 'dict_country', ['41'], ['42'], [column(41, 'dict_country_id'), column(42, 'dict_country_name', '', '', 'string'), column(43, 'next_store_fk', 'dict_store', 'dict_store_id')])
 const spec = { version: 1, base: { tableId: '1', aliasId: 'base' }, relations: [], columns: [], where: { logic: 'AND', items: [] }, orderBy: [] }
 return { base, store, region, country, spec, metadata: { '1': base } }
}
const step = (sourceTableId, sourceColumnId, target) => ({ sourceTableId, sourceColumnId, target })

test('automatic join retains the clicked field when two source foreign keys point at the same table', () => {
 const { spec, metadata, store } = fixture()
 const first = joinReportReferencePath(spec, metadata, 'base', [step('1', '12', store)])
 assert.deepEqual(first.spec.relations[0].keyBindings, [{ sourceColumnId: '12', targetColumnId: '21' }])
 assert.equal(first.spec.relations[0].joinType, 'LEFT')
 assert.deepEqual(first.spec.relations[0].displayColumnIds, ['22'])
 assert.deepEqual(first.spec.relations[0].detailColumnIds, ['22'])
 assert.deepEqual(first.spec.columns[0].source, { kind: 'RELATION_COLUMN', relationId: first.aliasId, columnId: '22' })
 const second = joinReportReferencePath(first.spec, first.metadata, 'base', [step('1', '11', store)])
 assert.equal(second.addedCount, 1)
 assert.notEqual(first.aliasId, second.aliasId)
 assert.equal(second.spec.relations.length, 2)
 assert.equal(second.spec.columns.length, 2)
 assert.deepEqual(second.spec.relations[1].keyBindings, [{ sourceColumnId: '11', targetColumnId: '21' }])
 assert.equal(spec.relations.length, 0)
 assert.deepEqual(Object.keys(metadata), ['1'])
})

test('joining a nested detail adds all missing intermediate tables in order', () => {
 const { spec, metadata, store, region } = fixture()
 const result = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store), step('2', '23', region)])
 assert.equal(result.addedCount, 2)
 assert.equal(result.spec.relations[1].fromAliasId, result.spec.relations[0].id)
 assert.equal(result.aliasId, result.spec.relations[1].id)
 assert.deepEqual(result.spec.columns.map((output) => output.source.columnId), ['22', '32'])
 assert.deepEqual(Object.keys(result.metadata), ['1', '2', '3'])
})

test('reusing a complete existing path preserves custom filters, display settings and cancelled default outputs', () => {
 const { spec, metadata, store, region } = fixture()
 const path = [step('1', '11', store), step('2', '23', region)]
 const initial = joinReportReferencePath(spec, metadata, 'base', path)
 const configured = structuredClone(initial.spec)
 configured.columns = []
 configured.relations[0].displayColumnIds = ['21']
 configured.relations[0].detailColumnIds = []
 configured.relations[0].name = '自定义门店'
 configured.relations[0].matchConditions.items = [{ aliasId: 'base', columnId: '10', operator: 'EQ', value: { kind: 'CONSTANT', value: '123' } }]
 const result = joinReportReferencePath(configured, initial.metadata, 'base', path)
 assert.equal(result.addedCount, 0)
 assert.equal(result.spec, configured)
 assert.equal(result.aliasId, initial.aliasId)
 assert.deepEqual(result.spec.columns, [])
 assert.equal(result.spec.relations[0].name, '自定义门店')
 assert.deepEqual(result.spec.relations[0].displayColumnIds, ['21'])
 assert.equal(result.spec.relations[0].matchConditions.items.length, 1)
})

test('an existing first relation is reused while the missing tail is appended', () => {
 const { spec, metadata, store, region } = fixture()
 const initial = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)])
 const result = joinReportReferencePath(initial.spec, initial.metadata, 'base', [step('1', '11', store), step('2', '23', region)])
 assert.equal(result.addedCount, 1)
 assert.equal(result.spec.relations[0], initial.spec.relations[0])
 assert.equal(result.spec.relations[1].fromAliasId, initial.aliasId)
})

test('composite keys retain the explicit clicked component and infer only an unambiguous remaining component', () => {
 const { spec, metadata, base, store } = fixture()
 base.columns.push(column(13, 'tenant_fk', 'dict_store', 'dict_tenant_id'))
 store.columns.push(column(24, 'dict_tenant_id'))
 store.table.primaryKeys = ['21', '24']
 store.table.uniqueKeys = [['21', '24']]
 const result = joinReportReferencePath(spec, metadata, 'base', [step('1', '12', store)])
 assert.deepEqual(result.spec.relations[0].keyBindings, [{ sourceColumnId: '12', targetColumnId: '21' }, { sourceColumnId: '13', targetColumnId: '24' }])
 const reversed = { ...result.spec, relations: result.spec.relations.map((relation) => ({ ...relation, keyBindings: [...relation.keyBindings].reverse() })) }
 assert.equal(joinReportReferencePath(reversed, result.metadata, 'base', [step('1', '12', store)]).addedCount, 0)
 base.columns.push(column(14, 'another_tenant_fk', 'dict_store', 'dict_tenant_id'))
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', '12', store)]), /多个候选/)
 base.columns = base.columns.filter((item) => ![13, 14].includes(item.id))
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', '12', store)]), /关联键不完整/)
})

test('a verified alternate unique key may be used when the clicked field targets it', () => {
 const { spec, metadata, base, store } = fixture()
 base.columns[1] = { ...base.columns[1], refColumnId: 'dict_store_name', logicalType: 'string' }
 store.table.uniqueKeys = [['21'], ['22']]
 const result = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)])
 assert.deepEqual(result.spec.relations[0].keyBindings, [{ sourceColumnId: '11', targetColumnId: '22' }])
 store.table.uniqueKeys = []
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)]), /已核验/)
})

test('older table payloads without uniqueKeys fall back to the primary key', () => {
 const { spec, metadata, store } = fixture()
 delete store.table.uniqueKeys
 assert.equal(joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)]).addedCount, 1)
})

test('source identities and dictionary references are checked before automatic binding', () => {
 const { spec, metadata, store } = fixture()
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('2', '11', store)]), /来源业务表与查看路径不一致/)
 assert.throws(() => joinReportReferencePath(spec, metadata, 'missing', [step('1', '11', store)]), /来源业务表已变化/)
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', 'store_fk', store)]), /不属于来源表/)
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', '11', { ...store, table: { ...store.table, bojunTableId: 'another_dictionary_id' } })]), /关联定义与目标表不一致/)
})

test('unavailable source, target and restricted binding fields cannot be joined', () => {
 for (const mutate of [
  ({ base }) => { base.table.enabled = false },
  ({ store }) => { store.table.validationStatus = 'UNSUPPORTED' },
  ({ base }) => { base.table.config.selectableColumns = ['10', '12'] },
  ({ store }) => { store.columns[0].isPhysical = false },
  ({ store }) => { store.table.config.selectableColumns = ['22'] },
 ]) {
  const input = fixture()
  mutate(input)
  assert.throws(() => joinReportReferencePath(input.spec, input.metadata, 'base', [step('1', '11', input.store)]), /不可用/)
 }
})

test('automatic defaults respect detail restrictions and primary key fallback', () => {
 const { spec, metadata, store } = fixture()
 store.table.config.detailColumns = []
 let result = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)])
 assert.deepEqual(result.spec.relations[0].detailColumnIds, [])
 assert.deepEqual(result.spec.columns.map((output) => output.source.columnId), ['22'])
 store.table.displayFields = []
 result = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)])
 assert.deepEqual(result.spec.relations[0].displayColumnIds, ['21'])
 assert.deepEqual(result.spec.columns.map((output) => output.source.columnId), ['21'])
})

test('relation count limits allow reuse but reject an additional foreign key alias', () => {
 const { spec, metadata, store } = fixture()
 const initial = joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store)])
 const full = { ...initial.spec, relations: [...initial.spec.relations, ...Array.from({ length: 7 }, (_, index) => ({ ...initial.spec.relations[0], id: `extra_${index}`, keyBindings: [{ sourceColumnId: '10', targetColumnId: '21' }] }))] }
 assert.equal(joinReportReferencePath(full, initial.metadata, 'base', [step('1', '11', store)]).addedCount, 0)
 assert.throws(() => joinReportReferencePath(full, initial.metadata, 'base', [step('1', '12', store)]), /最多关联 8/)
})

test('a path can reach depth three but a fourth relation is rejected atomically', () => {
 const { spec, metadata, store, region, country } = fixture()
 const path = [step('1', '11', store), step('2', '23', region), step('3', '33', country)]
 assert.equal(joinReportReferencePath(spec, metadata, 'base', path).addedCount, 3)
 const originalSpec = structuredClone(spec)
 const originalMetadata = structuredClone(metadata)
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [...path, step('4', '43', store)]), /最多为 3 层/)
 assert.deepEqual(spec, originalSpec)
 assert.deepEqual(metadata, originalMetadata)
})

test('failure after a valid first step leaves both original configuration and metadata untouched', () => {
 const { spec, metadata, store, region } = fixture()
 const beforeSpec = structuredClone(spec)
 const beforeMetadata = structuredClone(metadata)
 assert.throws(() => joinReportReferencePath(spec, metadata, 'base', [step('1', '11', store), step('2', 'missing', region)]), /不属于来源表/)
 assert.deepEqual(spec, beforeSpec)
 assert.deepEqual(metadata, beforeMetadata)
})

test('catalog parsing preserves verified local unique keys and distinguishes missing from empty keys', () => {
 const payload = { id: '2', logicalName: 'C_STORE', uniqueKeys: [['21'], ['22', '24']] }
 assert.deepEqual(parseBojunTable(payload).uniqueKeys, [['21'], ['22', '24']])
 assert.deepEqual(parseBojunTable({ ...payload, uniqueKeys: [] }).uniqueKeys, [])
 delete payload.uniqueKeys
 assert.equal(parseBojunTable(payload).uniqueKeys, undefined)
})
