import test from 'node:test'
import assert from 'node:assert/strict'
import { newBoard } from '../.test-dist/tableBoard/types.js'
import { differences, connectedMapping, matrix, newMetric, metricReferences, metricFormat, snapshotValues } from '../.test-dist/tableBoard/formula.js'

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


test('单格草稿隐藏变更格的旧值，其他格保留上一轮时间和结果', () => {
  const original = newBoard().layout
  const metric = newMetric()
  original.metrics = [metric]
  const f = {kind:'formula', metricId:metric.id, period:'MONTH_TO_DATE'}
  original.sections[0].cells[1][1] = f
  original.sections[0].cells[1][2] = f
  const snapshot = {cells:['B2','C2'].map(address=>({sectionId:'first',address,display:'12,400.00'}))}
  const draft = {...original,sections:original.sections.map(section=>({...section,
    cells:section.cells.map((line,r)=>line.map((cell,c)=>r===1 && c===2 ? {...f,overrides:{timeField:'CREATED_AT'}} : cell))}))}
  assert.deepEqual([...snapshotValues(original,draft,snapshot).keys()], ['first/B2'])
  assert.equal(snapshotValues(original,{...draft,metrics:[{...metric,timeField:'CREATED_AT'}]},snapshot).size,0)
})


test('矩阵公式继承指标格式，金额默认两位，计数默认整数', () => {
  const board = newBoard()
  const metric = newMetric()
  assert.equal(metricFormat(metric), 'money_2')
  assert.equal(metricFormat({...metric,aggregate:'AVG'}), 'money_2')
  assert.equal(metricFormat({...metric,aggregate:'COUNT_DISTINCT'}), 'integer')
  const shared = {...metric,aggregate:'COUNT',format:'number'}
  const [entry] = matrix(board.layout,board.layout.sections[0],1,1,shared,'STORE',['0007'],['TODAY'],false,false)
  assert.equal(entry.cell.format, undefined)
  assert.equal(metricFormat(shared), 'number')
})
