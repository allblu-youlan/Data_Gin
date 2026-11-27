import { columnLabels, rowLabels } from './gmvTemplateLabels.js'
import type { BoardCell, BoardLayout, BoardSection, Formula, Metric, Period, TableBoard } from './types'

export const gmvTable = 'ADS_IMPORT_SALES_PRODUCT_REPORT_QUERY'
export const templateSectionId = 'weekly-total-gmv'
export const defaultReportDate = '2026-11-15'
export const missingHistory = '待接入历史数据'
export const roleFields = {
  paid: 'BUYER_PAID_AMOUNT', returned: 'RETURN_AMOUNT', date: 'YMD_DATE',
  channel: 'SECOND_LEVEL_CHANNEL', included: 'IS_INCLUDED', status: 'STATUS',
}
export type TemplateRoles = Record<keyof typeof roleFields,string>
export type TemplateSource = Pick<Metric,'datasourceId'|'owner'|'tableName'|'bojunTableId'|'columnIds'> & {
  fields: TemplateRoles; timeFormat?: string
}
const columns = 'ABCDEFGHIJKLMNOPQRSTUVWX'
const amountColumns = 'BDEFKMUVWX'
const summaryRanges: Record<number, [number,number]> = {
  7:[8,9], 10:[11,18], 19:[20,43], 44:[45,47], 48:[49,63],
  65:[49,51], 66:[52,52], 67:[53,53], 68:[54,56], 69:[57,57], 70:[58,63], 71:[64,64],
}
const historical = (column: string) => 'KLMNOPQRSTWX'.includes(column)
const textCell = (text: string): BoardCell => ({kind:'text',text})

function sourceCell(row: number, col: string, channelField: string): BoardCell {
  const channel = rowLabels[row-7]
  if (!channel) return textCell('0')
  const periods: Record<string,Period> = {
    B:'YEAR_TO_DATE', D:'QUARTER_TO_DATE', E:'MONTH_TO_DATE', F:'WEEK_TO_DATE',
    K:'YEAR_TO_WEEK', M:'REPORT_WEEK', U:'YEAR_TO_DATE', V:'YEAR_TO_DATE', W:'YEAR_TO_WEEK', X:'YEAR_TO_WEEK',
  }
  const metricId = 'UW'.includes(col) ? 'before-refund' : 'VX'.includes(col) ? 'after-refund' : 'gmv'
  const cell: Formula = {kind:'formula', metricId, period:periods[col], filters:[{field:channelField,op:'EQ',value:channel}]}
  if (historical(col)) cell.yearOffset = -1
  return cell
}

function summaryExpression(row: number,column:string) {
  if (row === 64) return '=' + [7,10,19,48,44].map(r=>column+r).join('+')
  const [first,last] = summaryRanges[row]
  return first === last ? '=' + column + first : '=SUM(' + column + first + ':' + column + last + ')'
}

function expressionAt(row: number, col: string): string {
  if (amountColumns.includes(col) || (row >= 65 && 'CGLN'.includes(col))) return summaryExpression(row,col)
  const denominator = ({C:'B',G:'F',L:'K',N:'M'} as Record<string,string>)[col]
  if (denominator) return '=' + denominator + row + '/$' + denominator + '$64'
  const ratio:Record<string,string> = {
    H:'(U@+V@)/B@', I:'U@/$B@', J:'V@/$B@', O:'(W@+X@)/K@', P:'W@/$K@', Q:'X@/$K@',
    R:'B@/K@-1', S:'F@/M@-1', T:'H@-O@',
  }
  return '=IFERROR(' + ratio[col].replace(/@/g,String(row)) + ',' + ('RT'.includes(col) ? '"-"' : '""') + ')'
}

function dataCell(row:number,col:string,field:string,history:boolean):BoardCell {
  if (col === 'A') return textCell(rowLabels[row-7])
  if (!history && historical(col)) return textCell(missingHistory)
  if (row===11 && col==='I') return textCell('')
  const amount=amountColumns.includes(col)
  if (amount && row!==64 && !summaryRanges[row]) return sourceCell(row,col,field)
  return {kind:'expression',expression:expressionAt(row,col),format:amount ? 'money_2' : 'percent_2'}
}

function headerLabels(date:string) {
  const year=Number(date.slice(0,4)), lastYear=year-1
  return { A1:'报告日期', A2:date, A4:'一、总GMV', B5:year+'年', K5:lastYear+'年', R5:year+' vs '+lastYear, U5:year+'年', W5:lastYear+'年', D6:'Q'+Math.ceil(Number(date.slice(5,7))/3) }
}

export function updateReportDate(layout:BoardLayout, reportDate:string):BoardLayout {
  if (!reportDate && layout.sections.some(section=>section.id===templateSectionId)) {
    throw new Error('总 GMV 模板需要报告日期，已保留原日期。')
  }
  const next={...layout,reportDate:reportDate || undefined}
  if (!reportDate) return next
  const headers=headerLabels(reportDate)
  return {...next, sections:layout.sections.map(section=>{
    if (section.id !== templateSectionId) return section
    const cells=section.cells.map((line,r)=>r<6 ? [...line] : line)
    for(const [address,label] of Object.entries(headers)) {
      const r=Number(address.slice(1))-1,c=columns.indexOf(address[0])
      if(cells[r]?.[c]?.kind==='text') cells[r][c]=textCell(label)
    }
    return {...section,cells}
  })}
}

export function buildGmvTemplate(source:TemplateSource,reportDate:string,history=false):TableBoard {
  const f=source.fields
  const metrics:Metric[]=[
    {id:'gmv',name:'累计 GMV',valueField:f.paid},
    {id:'before-refund',name:'发货前退货 GMV',valueField:f.paid,status:'发货前退货'},
    {id:'after-refund',name:'发货后退货 GMV',valueField:f.returned,status:'正常'},
  ].map(def=>{
    const filters:Metric['filters']=[{field:f.included,op:'EQ',value:'是'}]
    if(def.status) filters.push({field:f.status,op:'EQ',value:def.status})
    const selected=[f.date,f.channel,def.valueField,...filters.map(filter=>filter.field)]
    const columnIds=source.bojunTableId ? Object.fromEntries(selected.map(name=>[name,source.columnIds![name]])) : undefined
    return {id:def.id,name:def.name,datasourceId:source.datasourceId,owner:source.owner,tableName:source.tableName,
      bojunTableId:source.bojunTableId,columnIds,aggregate:'SUM',valueField:def.valueField,timeField:f.date,
      timeFormat:source.timeFormat,timezone:'Asia/Shanghai',filters,dimensions:[f.channel],format:'money_2'}
  })
  const section:BoardSection={id:templateSectionId,title:'一、总GMV',cells:Array.from({length:71},(_,index)=>{
    const row=index+1
    return Array.from(columns,(column,i)=>row>=7 ? dataCell(row,column,f.channel,history) : textCell(row===6 ? columnLabels[i] : ''))
  })}
  const layout=updateReportDate({sections:[section],metrics,sources:[],foreground:'#171717',background:'#ffffff'},reportDate)
  return {id:0,revision:0,name:'一、总GMV',layout}
}
