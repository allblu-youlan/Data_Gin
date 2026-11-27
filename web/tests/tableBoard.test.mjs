import test from 'node:test'
import assert from 'node:assert/strict'
import { newBoard } from '../.test-dist/tableBoard/types.js'
import { differences, connectedMapping, matrix, newMetric, metricReferences } from '../.test-dist/tableBoard/formula.js'

test('单格仅保存归期差异，恢复继承后差异消失', () => {
  const base = { ...newMetric(), timeField: 'PAID_AT', valueField: 'PAID_AMOUNT' }
  assert.deepEqual(differences(base, { ...base, timeField: 'CREATED_AT' }), { timeField: 'CREATED_AT' })
  assert.equal(differences(base, { ...base, filters: [] }), undefined)
  assert.equal(connectedMapping(base, { datasourceId: 8, owner: 'OPS', tableName: 'ITEM' }).valueField, '')
})
test('矩阵先预览、默认跳过已有内容并保留门店前导零', () => {
  const {layout} = newBoard(), section = layout.sections[0], metric = newMetric()
  section.cells[2][1] = { kind: 'text', text: '保留' }
  const apply = (replace) => matrix(layout, section, 2, 1, metric, 'STORE', ['0007','0012'], ['TODAY','MONTH_TO_DATE','YEAR_TO_DATE'], replace, false)
  assert.equal(apply(false).filter(x => x.apply).length, 5)
  assert.equal(apply(true).filter(x => x.apply).length, 6)
  assert.equal(apply(true)[0].cell.filters[0].value, '0007')
  assert.deepEqual(section.cells[2][1], {kind:'text',text:'保留'})
  assert.throws(() => matrix(layout, section, 99, 1, metric, 'STORE', ['a','b'], ['TODAY'], false, false))
  assert.deepEqual(metricReferences(layout, metric.id), [])
})
