import test from 'node:test'
import assert from 'node:assert/strict'
import { newBoard } from '../.test-dist/tableBoard/types.js'
import { newMetric } from '../.test-dist/tableBoard/formula.js'
import { parseBoardConfig, importedLayout, boardBindings, currentConnectionChoices, maxImportBytes } from '../.test-dist/tableBoard/importConfig.js'

function example(id) {
  const {layout}=newBoard()
  const metric={...newMetric(),id:'sales',name:'销售额',owner:'SHOP',tableName:'ORDERS',datasourceId:id,timeField:'POSTED_AT',valueField:'AMOUNT'}
  layout.metrics=[metric]
  layout.sections[0].cells[1][1]={kind:'formula',metricId:metric.id,period:'YEAR_TO_DATE'}
  layout.sections[0].cells[1][2]={kind:'expression',expression:'=B2+1',format:'percent_2'}
  return layout
}

test('导入仅取名称和布局，按物理表匹配已有连接并保留表达式',()=>{
  const source=example(0)
  const {layout,name}=parseBoardConfig(JSON.stringify({name:' 示例 ',layout:source,id:999,revision:999}))
  assert.equal(name,'示例')
  assert.equal(layout.metrics[0].datasourceId,0)
  assert.throws(()=>importedLayout(layout,new Map()),/请选择 SHOP.ORDERS/)
  const mapped=importedLayout(layout,currentConnectionChoices(example(7)))
  assert.equal(mapped.metrics[0].datasourceId,7)
  assert.deepEqual(mapped.sections[0].cells[1][2],source.sections[0].cells[1][2])
  assert.equal(layout.metrics[0].datasourceId,0)
})
test('同表多连接不自动选；不同物理表的格覆盖独立绑定',()=>{
  const current=example(7)
  current.metrics.push({...current.metrics[0],id:'other',datasourceId:9})
  assert.equal(currentConnectionChoices(current).has('SHOP.ORDERS'),false)
  const next=example(0)
  next.sections[0].cells[1][1].overrides={owner:'ARCHIVE',tableName:'HISTORY',datasourceId:0}
  assert.equal(boardBindings(next).length,2)
  const mapped=importedLayout(next,new Map([['SHOP.ORDERS',7],['ARCHIVE.HISTORY',9]]))
  assert.equal(mapped.metrics[0].datasourceId,7)
  assert.equal(mapped.sections[0].cells[1][1].overrides.datasourceId,9)
})
test('拒绝超限或无法安全显示的配置',()=>{
  assert.throws(()=>parseBoardConfig('界'.repeat(Math.ceil(maxImportBytes/3))),/2 MiB/)
  assert.throws(()=>parseBoardConfig('null'),/layout/)
  const config={layout:example(0)}
  config.layout.sections[0].cells[1][2]={kind:'custom'}
  assert.throws(()=>parseBoardConfig(JSON.stringify(config)),/单元格类型/)
})
