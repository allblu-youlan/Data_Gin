import { useRef, useState } from 'react'
import { Button, Dialog } from '../ui'
import { checkGridSize, gridWidth, maxColumns, maxRows, resizedCells } from './grid'
import type { BoardLayout, BoardSection } from './types'
import styles from './BoardCanvas.module.css'

type Props = {
  layout: BoardLayout
  section?: BoardSection
  onSave: (section: BoardSection) => void
  onRemove: () => void
  onClose: () => void
}

export function SectionEditor({ layout, section, onSave, onRemove, onClose }: Props) {
  const nameInput = useRef<HTMLInputElement>(null)
  const [title, setTitle] = useState(section?.title ?? `表格 ${layout.sections.length + 1}`)
  const [rows, setRows] = useState(String(section?.cells.length ?? 4))
  const [columns, setColumns] = useState(String(section ? gridWidth(section) : 4))
  const [error, setError] = useState('')
  function apply() {
    if (!title.trim()) { setError('请输入区块名称。'); return }
    const h = Number(rows)
    const w = Number(columns)
    try {
      checkGridSize(layout, section?.id ?? '', h, w)
      if (section && (h < section.cells.length || w < gridWidth(section))
        && !window.confirm('缩小表格将移除超出范围的单元格，继续调整吗？')) return
      const base = section ?? { id: globalThis.crypto?.randomUUID?.() ?? `section-${Date.now()}`, title: '', cells: [] }
      onSave({ ...base, title: title.trim(), cells: resizedCells(base, h, w) })
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '请检查区块设置。')
    }
  }
  return <Dialog open title={section ? '区块设置' : '添加区块'} onClose={onClose} initialFocusRef={nameInput}
    footer={<><Button onClick={onClose}>取消</Button><Button variant="primary" onClick={apply}>应用</Button></>}>
    <div className={styles.editor}>
      <label>区块名称<input ref={nameInput} value={title} maxLength={80} onChange={event => setTitle(event.target.value)} /></label>
      <div className={styles.inputs}>
        <label>行数<input type="number" value={rows} min={1} max={maxRows} onChange={event => setRows(event.target.value)} /></label>
        <label>列数<input type="number" value={columns} min={1} max={maxColumns} onChange={event => setColumns(event.target.value)} /></label>
      </div>
      {section && layout.sections.length > 1 && <div>
        <Button variant="danger" onClick={() => {
          if (window.confirm(`移除「${section.title}」及其中的单元格吗？`)) { onRemove(); onClose() }
        }}>移除区块</Button>
      </div>}
      {error && <p className={styles.error} role="alert">{error}</p>}
    </div>
  </Dialog>
}
