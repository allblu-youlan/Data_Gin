import { useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import { Link2 } from 'lucide-react'
import { columnName } from './clipboard'
import { gridWidth } from './grid'
import { resolveCell, type BoardSection, type BoardValues, type DisplayCell } from './types'
import styles from './BoardCanvas.module.css'

type Props = {
  section: BoardSection
  display?: Map<string, DisplayCell>
  values: BoardValues
  editing: boolean
  onEdit: (row: number, column: number) => void
  onPaste: (row: number, column: number, text: string) => void
}

export function SectionGrid({ section, values, display, editing, onEdit, onPaste }: Props) {
  const width = gridWidth(section)
  const [focused, setFocused] = useState([0, 0])
  const activeRow = Math.min(focused[0], section.cells.length - 1)
  const activeCol = Math.min(focused[1], width - 1)

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
    event.currentTarget.closest('tbody')?.rows[r]?.cells[c + 1]?.querySelector('button')?.focus()
  }

  function paste(event: ClipboardEvent<HTMLButtonElement>, row: number, column: number) {
    const text = event.clipboardData.getData('text/plain')
    if (!text) return
    event.preventDefault()
    onPaste(row, column, text)
  }

  return <div className={styles.tableScroll}>
    <table className={styles.grid} aria-label={section.title}>
      {editing && <thead><tr><th aria-label="行号" />{Array.from({ length: width }, (_, c) =>
        <th scope="col" key={c}>{columnName(c)}</th>)}</tr></thead>}
      <tbody>{section.cells.map((line, r) => <tr key={r}>
        {editing && <th scope="row">{r + 1}</th>}
        {line.map((cell, c) => {
          const address = `${columnName(c)}${r + 1}`
          const result = display?.get(section.id + '/' + address)
          const value = (cell.kind !== 'text' ? result?.display : undefined) ?? (cell.kind === 'formula' ? '待计算' : resolveCell(cell, values))
          return <td key={c} title={result?.message}>
            {editing ? <button type="button" className={styles.cell}
              aria-label={`编辑 ${address}，${value || (cell.kind === 'field' ? cell.field : '空单元格')}`}
              tabIndex={r === activeRow && c === activeCol ? 0 : -1}
              onFocus={() => setFocused([r, c])} onKeyDown={event => navigate(event, r, c)}
              onClick={() => onEdit(r, c)} onPaste={event => paste(event, r, c)}>
              {cell.kind === 'field' && <Link2 aria-hidden="true" />}
              <span>{value || (cell.kind === 'field' ? cell.field : '')}</span>
            </button> : <span className={styles.value}>{value}</span>}
          </td>
        })}
      </tr>)}</tbody>
    </table>
  </div>
}
