import assert from 'node:assert/strict'
import test from 'node:test'
import { inheritDictionaryRelationKeys, newWorkspaceRelation, relationSourceAliases, removeReportTables } from '../.test-dist/reportCenter/components/ReportQueryDesigner/tableWorkspace.js'

function fixture() {
 const relations = [newWorkspaceRelation('store', 'base'), newWorkspaceRelation('region', 'store'), newWorkspaceRelation('product', 'base')].map((relation, index) => ({ ...relation, targetTableId: String(index + 2) }))
 const condition = (aliasId, value) => ({ aliasId, columnId: '11', operator: 'EQ', value: value || { kind: 'CONSTANT', value: 'x' } })
 relations[2].matchConditions.items.push(condition('product', { kind: 'FIELD', aliasId: 'store', columnId: '12' }))
 return { version: 1, base: { tableId: '1', aliasId: 'base' }, relations, columns: [{ fieldId: 'a', source: { kind: 'BASE_COLUMN', columnId: '10' } }, ...relations.map((relation) => ({ fieldId: relation.id, source: { kind: 'RELATION_DISPLAY', relationId: relation.id } }))], where: { logic: 'AND', items: [condition('base'), { logic: 'OR', items: [condition('store'), condition('region')] }, condition('product')] }, orderBy: [{ aliasId: 'store', columnId: '10', direction: 'ASC' }, { aliasId: 'product', columnId: '10', direction: 'DESC' }], allowedFilters: [{ aliasId: 'region', columnId: '10', operators: ['EQ'] }], allowedSortFields: [{ aliasId: 'product', columnId: '10' }] }
}

test('connection sources include all joined tables while excluding self and descendants', () => {
 const spec = fixture()
 assert.deepEqual(relationSourceAliases(spec, 'store'), ['base', 'product'])
 assert.deepEqual(relationSourceAliases(spec, 'region'), ['base', 'store', 'product'])
})

test('removing a table clears downstream tables, outputs, predicates and runtime references', () => {
 const spec = fixture()
 const removal = removeReportTables(spec, 'store')
 assert.equal(removal.tableCount, 2)
 assert.equal(removal.columnCount, 2)
 assert.deepEqual(removal.query.relations.map((relation) => relation.id), ['product'])
 assert.deepEqual(removal.query.columns.map((column) => column.fieldId), ['a', 'product'])
 assert.deepEqual(removal.query.where.items.map((condition) => condition.aliasId), ['base', 'product'])
 assert.deepEqual(removal.query.relations[0].matchConditions.items, [])
 assert.deepEqual(removal.query.orderBy.map((order) => order.aliasId), ['product'])
 assert.deepEqual(removal.query.allowedFilters, [])
 assert.equal(spec.relations.length, 3)
 assert.equal(spec.where.items.length, 3)
})

test('removing the initial joined table clears the graph so another table can be added', () => {
 const removal = removeReportTables(fixture(), 'base')
 assert.equal(removal.tableCount, 4)
 assert.equal(removal.columnCount, 4)
 assert.equal(removal.query.base.tableId, '')
 assert.deepEqual(removal.query.relations, [])
 assert.deepEqual(removal.query.columns, [])
 assert.deepEqual(removal.query.where.items, [])
 assert.deepEqual(removal.query.orderBy, [])
 assert.deepEqual(removal.query.allowedSortFields, [])
})

test('new connections retain complete editable key and display contracts', () => {
 const relation = newWorkspaceRelation('new_table', 'region')
 assert.equal(relation.fromAliasId, 'region')
 assert.equal(relation.joinType, 'LEFT')
 assert.equal(relation.cardinality, 'MANY_TO_ONE')
 assert.deepEqual(relation.keyBindings, [])
 assert.deepEqual(relation.matchConditions, { logic: 'AND', items: [] })
})


function dictionaryRelationFixture() {
 const column = (id, bojunColumnId, refTableId = '', refColumnId = '') => ({ id, bojunColumnId, refTableId, refColumnId, isPhysical: true, isSelectable: true })
 const source = { table: { bojunTableId: 'order' }, columns: [column(11, 'store_fk', 'store', 'store_id'), column(12, 'tenant_fk', 'store', 'tenant_id'), column(13, 'manual_id')] }
 const target = { table: { bojunTableId: 'store', primaryKeys: ['21', '22'] }, columns: [column(21, 'store_id'), column(22, 'tenant_id')] }
 const relation = { ...newWorkspaceRelation('store', 'base'), targetTableId: '2', keyBindings: [{ sourceColumnId: '', targetColumnId: '21' }, { sourceColumnId: '', targetColumnId: '22' }] }
 return { source, target, relation }
}

test('dictionary relationships populate complete composite keys using dictionary IDs', () => {
 const { relation, source, target } = dictionaryRelationFixture()
 const inherited = inheritDictionaryRelationKeys(relation, source, target)
 assert.deepEqual(inherited.keyBindings, [{ sourceColumnId: '11', targetColumnId: '21' }, { sourceColumnId: '12', targetColumnId: '22' }])
 assert.equal(relation.keyBindings[0].sourceColumnId, '')
 assert.equal(inheritDictionaryRelationKeys(inherited, source, target), inherited)
})

test('dictionary relationships preserve manually selected source fields', () => {
 const { relation, source, target } = dictionaryRelationFixture()
 relation.keyBindings[0].sourceColumnId = '13'
 assert.deepEqual(inheritDictionaryRelationKeys(relation, source, target).keyBindings, [{ sourceColumnId: '13', targetColumnId: '21' }, { sourceColumnId: '12', targetColumnId: '22' }])
})

test('incomplete and ambiguous dictionary relationships stay editable without partial guesses', () => {
 const { relation, source, target } = dictionaryRelationFixture()
 assert.equal(inheritDictionaryRelationKeys(relation, { ...source, columns: source.columns.slice(0, 1) }, target), relation)
 assert.equal(inheritDictionaryRelationKeys(relation, { ...source, columns: [...source.columns, { ...source.columns[0], id: 14 }] }, target), relation)
 assert.equal(inheritDictionaryRelationKeys(relation, source), relation)
})

test('dictionary relationships respect restricted fields and custom target keys', () => {
 const { relation, source, target } = dictionaryRelationFixture()
 assert.equal(inheritDictionaryRelationKeys(relation, { ...source, table: { ...source.table, config: { selectableColumns: ['11'] } } }, target), relation)
 const manual = { ...relation, keyBindings: [{ sourceColumnId: '13', targetColumnId: '23' }] }
 assert.equal(inheritDictionaryRelationKeys(manual, source, target), manual)
})
