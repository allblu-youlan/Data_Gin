import { useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import { Button } from '../ui'
import { addressOf, periods } from './formula'
import { Link2 } from 'lucide-react'
import { columnName } from './clipboard'
import { gridWidth } from './grid'
import { resolveCell, type BoardCell, type Period, type BoardSection, type BoardValues, type DisplayCell } from './types'
import styles from './BoardCanvas.module.css'

type Props = {
  section: BoardSection
  display?: Map<string, DisplayCell>
  values: BoardValues
  editing: boolean
  onEdit: (row: number, column: number) => void
  onPaste: (row: number, column: number, text: string, token?: string) => void
  onCopy: (block: BoardCell[][]) => string
  onChangePeriods: (top:number,left:number,bottom:number,right:number,period:Period) => void
}

export function SectionGrid({ section, values, display, editing, onEdit, onPaste, onCopy, onChangePeriods }: Props) {
  const width = gridWidth(section)
  const [focused, setFocused] = useState([0, 0])
  const activeRow = Math.min(focused[0], section.cells.length - 1)
  const activeCol = Math.min(focused[1], width - 1)

  const [anchor,setAnchor] = useState<[number,number] | null>(null)
  const [period,setPeriod] = useState<Period>('MONTH_TO_DATE')
  const top = Math.min(anchor?.[0] ?? activeRow, activeRow)
  const bottom = Math.max(anchor?.[0] ?? activeRow, activeRow)
  const left = Math.min(anchor?.[1] ?? activeCol, activeCol)
  const right = Math.max(anchor?.[1] ?? activeCol, activeCol)
  const range = section.cells.slice(top,bottom+1).map(line=>line.slice(left,right+1))
  function copy(event:ClipboardEvent<HTMLButtonElement>) {
    event.preventDefault()
    event.clipboardData.setData('application/x-table-board',onCopy(range))
    const text=range.map((line,r)=>line.map((cell,c)=>{
      const result=display?.get(section.id+'/'+addressOf(top+r,left+c))
      const content=cell.kind==='text' ? cell.text : result?.display ?? ''
      return /["\t\n]/.test(content) ? '"'+content.replace(/"/g,'""')+'"' : content
    }).join('\t')).join('\n')
    event.clipboardData.setData('text/plain',text)
  }
  function navigate(event: KeyboardEvent<HTMLButtonElement>, row: number, column: number) {
    let r = row
    let c = column
    switch (event.key) {
      case 'ArrowUp': r -= 1; break
      case 'ArrowDown': r += 1; break
      case 'ArrowLeft': c -= 1; break
      case 'ArrowRight': c += 1; break
      case 'Home': c = 0; break
      case 'End': c = width - 1; break
      default: return
    }
    event.preventDefault()
    const next=event.currentTarget.closest('tbody')?.rows[r]?.cells[c + 1]?.querySelector('button')
    if (!next) return
    setAnchor(event.shiftKey ? (anchor ?? [row,column]) : null)
    next.focus()
  }

  function paste(event: ClipboardEvent<HTMLButtonElement>, row: number, column: number) {
    const text = event.clipboardData.getData('text/plain')
    const token=event.clipboardData.getData('application/x-table-board')
    if (!text && !token) return
    event.preventDefault()
    onPaste(row, column, text, token)
  }

  return <div className={styles.tableScroll}>
    {editing && anchor && <div className={styles.actions}>
      <strong>{addressOf(top,left)} 至 {addressOf(bottom,right)}</strong><label>只改期间 <select value={period} onChange={e=>setPeriod(e.target.value as Period)}>{Object.entries(periods).map(([id,name])=><option key={id} value={id}>{name}</option>)}</select></label>
      <Button disabled={!range.some(line=>line.some(c=>c.kind==='formula'))} onClick={()=>onChangePeriods(top,left,bottom,right,period)}>应用到选区公式</Button>
    </div>}
    <table className={styles.grid} aria-label={section.title}>
      {editing && <thead><tr><th aria-label="行号" />{Array.from({ length: width }, (_, c) =>
        <th scope="col" key={c}>{columnName(c)}</th>)}</tr></thead>}
      <tbody>{section.cells.map((line, r) => <tr key={r}>
        {editing && <th scope="row">{r + 1}</th>}
        {line.map((cell, c) => {
          const address = `${columnName(c)}${r + 1}`
          const result = display?.get(section.id + '/' + address)
          const value = (cell.kind !== 'text' ? result?.display : undefined) ?? (cell.kind === 'formula' ? '待计算' : resolveCell(cell, values))
          return <td key={c} title={result?.message} data-range={editing && !!anchor && r>=top && r<=bottom && c>=left && c<=right || undefined}>
            {editing ? <button type="button" className={styles.cell}
              aria-label={`编辑 ${address}，${value || (cell.kind === 'field' ? cell.field : '空单元格')}`}
              tabIndex={r === activeRow && c === activeCol ? 0 : -1}
              onFocus={() => setFocused([r, c])} onKeyDown={event => navigate(event, r, c)}
              onMouseDown={event=>{if(event.shiftKey) setAnchor(anchor ?? [activeRow,activeCol]); else setAnchor(null)}}
              onClick={event => { if(!event.shiftKey) onEdit(r, c) }} onCopy={copy} onPaste={event => paste(event, r, c)}>
              {cell.kind === 'field' && <Link2 aria-hidden="true" />}
              <span>{value || (cell.kind === 'field' ? cell.field : '')}</span>
            </button> : <span className={styles.value}>{value}</span>}
          </td>
        })}
      </tr>)}</tbody>
    </table>
  </div>
}
