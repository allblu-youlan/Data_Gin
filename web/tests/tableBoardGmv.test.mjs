import test from 'node:test'
import assert from 'node:assert/strict'
import { buildGmvTemplate, updateReportDate, roleFields, missingHistory } from '../.test-dist/tableBoard/gmvTemplate.js'

const source = { datasourceId:7,owner:'OPS',tableName:'ADS_IMPORT_SALES_PRODUCT_REPORT_QUERY',fields:roleFields }
const at = (layout, col, row) => layout.sections[0].cells[row-1][col.charCodeAt(0)-65]

test('总 GMV 草稿保持 A4:X71 坐标并绑定真实字段',()=>{
  const board = buildGmvTemplate(source,'2026-11-15')
  const {layout}=board
  assert.equal(board.id,0)
  assert.equal(layout.sections[0].cells.length,71)
  assert.ok(layout.sections[0].cells.every(row=>row.length===24))
  assert.equal(at(layout,'A',4).text,'一、总GMV')
  assert.equal(at(layout,'D',8).period,'QUARTER_TO_DATE')
  assert.equal(at(layout,'F',8).period,'WEEK_TO_DATE')
  assert.equal(at(layout,'H',8).expression,'=IFERROR((U8+V8)/B8,"")')
  assert.equal(at(layout,'H',8).format,'percent_2')
  assert.equal(at(layout,'B',57).kind,'formula')
  assert.equal(at(layout,'B',43).text,'0')
  assert.equal(at(layout,'B',63).text,'0')
  assert.equal(at(layout,'I',11).text,'')
  assert.equal(at(layout,'B',64).expression,'=B7+B10+B19+B48+B44')
  assert.equal(at(layout,'B',71).expression,'=B64')
  for (const col of 'KLMNOPQRSTWX') assert.equal(at(layout,col,8).text,missingHistory)
  assert.ok(!layout.sections[0].cells.flat().some(c=>c.yearOffset===-1))
  assert.equal(layout.metrics[2].valueField,'RETURN_AMOUNT')
  assert.deepEqual(layout.metrics[2].filters,[{field:'IS_INCLUDED',op:'EQ',value:'是'},{field:'STATUS',op:'EQ',value:'正常'}])
  assert.equal(at(layout,'B',8).filters[0].field,'SECOND_LEVEL_CHANNEL')
})
test('接入历史数据时绑定上一年同报告周的累计和周销',()=>{
  const {layout}=buildGmvTemplate(source,'2026-11-15',true)
  assert.equal(at(layout,'K',8).period,'YEAR_TO_WEEK')
  assert.equal(at(layout,'M',8).period,'REPORT_WEEK')
  assert.equal(at(layout,'W',8).yearOffset,-1)
  assert.equal(at(layout,'R',8).expression,'=IFERROR(B8/K8-1,"-")')
  const next=updateReportDate(layout,'2027-01-03')
  assert.equal(at(next,'B',5).text,'2027年')
  assert.equal(at(next,'W',5).text,'2026年')
  assert.equal(at(next,'D',6).text,'Q1')
  assert.equal(at(layout,'D',6).text,'Q4')
})
