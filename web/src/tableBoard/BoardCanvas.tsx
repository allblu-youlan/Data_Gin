import { useRef, useState, type CSSProperties } from 'react'
import { ArrowDown, ArrowUp, Bookmark, GalleryVerticalEnd, PanelTop, Plus, Settings2 } from 'lucide-react'
import { Button } from '../ui'
import controls from '../ui/Controls/Controls.module.css'
import type { ReportCenterClient } from '../reportCenter/api'
import { MetricManager } from './MetricManager'
import { MatrixEditor } from './MatrixEditor'
import { addressOf } from './formula'
import { CellEditor } from './CellEditor'
import { SectionEditor } from './SectionEditor'
import { SectionGrid } from './SectionGrid'
import { parseClipboardTable } from './clipboard'
import { gridWidth, maxSections, writeBlock } from './grid'
import type { BoardCell, BoardLayout, BoardSection, BoardValues, DisplayCell } from './types'
import styles from './BoardCanvas.module.css'

type Props = { asOf?: string; client: ReportCenterClient; display?: Map<string, DisplayCell>; layout: BoardLayout; onChange: (layout: BoardLayout) => void; values: BoardValues; editing: boolean }
type Cursor = { id: string; row: number; column: number }

export function BoardCanvas({ client, asOf, layout, onChange, values, display, editing }: Props) {
  const [single, setSingle] = useState(false)
  const [activeId, setActiveId] = useState('')
  const [cursor, setCursor] = useState<Cursor | null>(null)
  const [sectionDraft, setSectionDraft] = useState<{ id?: string } | null>(null)
  const [manage,setManage] = useState(false)
  const [batch,setBatch] = useState(false)
  const [undo,setUndo] = useState<{previous:BoardSection;applied:BoardSection} | null>(null)
  const [error, setError] = useState('')
  const copiedBlock = useRef<{token:string;cells:BoardCell[][]} | null>(null)
  const nodes = useRef(new Map<string, HTMLElement>())
  const active = layout.sections.find(section => section.id === activeId) ?? layout.sections[0]
  const current = layout.sections.find(section => section.id === cursor?.id)
  const visible = single && active ? [active] : layout.sections

  function select(id: string) {
    setActiveId(id)
    if (!single) nodes.current.get(id)?.scrollIntoView({ block: 'start', behavior: 'auto' })
  }
  function replace(section: BoardSection) {
    onChange({ ...layout, sections: layout.sections.map(item => item.id === section.id ? section : item) })
  }
  function shift(id: string, offset: number) {
    const sections = [...layout.sections]
    const index = sections.findIndex(section => section.id === id)
    sections.splice(index + offset, 0, ...sections.splice(index, 1))
    onChange({ ...layout, sections })
  }
  function write(section: BoardSection, row: number, column: number, block: BoardCell[][]) {
    replace(writeBlock(layout, section, row, column, block))
    setError('')
  }
  function paste(section: BoardSection, row: number, column: number, text: string, token?: string) {
    try {
      if (token && copiedBlock.current?.token === token) {
        write(section,row,column,copiedBlock.current.cells)
        return
      }
      if (!text) return
      const block: BoardCell[][] = parseClipboardTable(text).map(line => line.map(value => ({ kind: 'text', text: value })))
      write(section, row, column, block)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '粘贴失败。')
    }
  }
  const palette = { '--board-ink': layout.foreground, '--board-paper': layout.background } as CSSProperties

  return <div className={`${styles.canvas} ${controls.surface}`} style={palette}>
    <div className={styles.tools}>
      <div className={styles.segment} role="group" aria-label="展示方式">
        <button type="button" aria-pressed={!single} onClick={() => setSingle(false)}><GalleryVerticalEnd />纵向排列</button>
        <button type="button" aria-pressed={single} onClick={() => setSingle(true)}><PanelTop />单块展示</button>
      </div>
      {editing && <div className={styles.palette}>
        <Button onClick={()=>setManage(true)}>指标管理</Button>
        {undo && layout.sections.includes(undo.applied) && <Button onClick={()=>{replace(undo.previous);setUndo(null)}}>撤销批量操作</Button>}
        <label>文字<input type="color" value={layout.foreground} onChange={event => onChange({ ...layout, foreground: event.target.value })} /></label>
        <label>底色<input type="color" value={layout.background} onChange={event => onChange({ ...layout, background: event.target.value })} /></label>
        <Button onClick={() => onChange({ ...layout, foreground: '#171717', background: '#ffffff' })}>恢复黑白</Button>
      </div>}
    </div>
    <nav aria-label="看板区块" className={styles.tabs}>
      {layout.sections.map(section => <button type="button" key={section.id} aria-current={active?.id === section.id ? 'location' : undefined}
        onClick={() => select(section.id)}><Bookmark aria-hidden="true" />{section.title}</button>)}
      {editing && <Button disabled={layout.sections.length >= maxSections} onClick={() => setSectionDraft({})}><Plus />添加区块</Button>}
    </nav>
    {error && <p className={styles.error} role="alert">{error}</p>}
    <div className={styles.stack}>{visible.map(section => {
      const index = layout.sections.indexOf(section)
      return <section key={section.id} className={styles.section} aria-label={section.title}
        ref={node => { if (node) nodes.current.set(section.id, node); else nodes.current.delete(section.id) }}>
        <header className={styles.sectionTitle}>
          <h3>{section.title}</h3>
          {editing && <div className={styles.sectionActions}>
            <span className={styles.size}>{section.cells.length} 行 × {gridWidth(section)} 列</span>
            <Button disabled={!index} aria-label={`上移${section.title}`} title="上移区块" onClick={() => shift(section.id, -1)}><ArrowUp /></Button>
            <Button disabled={index === layout.sections.length - 1} aria-label={`下移${section.title}`} title="下移区块" onClick={() => shift(section.id, 1)}><ArrowDown /></Button>
            <Button onClick={() => setSectionDraft({ id: section.id })}><Settings2 />区块设置</Button>
          </div>}
        </header>
        <SectionGrid display={display} editing={editing} section={section} values={values}
          onPaste={(r, c, text, token) => paste(section, r, c, text, token)}
          onCopy={cells=>{
            const token=globalThis.crypto?.randomUUID?.() ?? String(Date.now())
            copiedBlock.current={token,cells}
            return token
          }}
          onChangePeriods={(top,left,bottom,right,period)=>{
            const cells=section.cells.map((line,r)=>line.map((cell,c)=>
              r>=top && r<=bottom && c>=left && c<=right && cell.kind==='formula' ? {...cell,period} : cell))
            const applied={...section,cells}
            setUndo({previous:section,applied});replace(applied)
          }}
          onEdit={(row, column) => { setActiveId(section.id); setCursor({ id: section.id, row, column }) }} />
      </section>
    })}</div>
    {editing && !manage && !batch && cursor && current && <CellEditor result={display?.get(cursor.id + '/' + addressOf(cursor.row,cursor.column))} asOf={asOf} client={client} metrics={layout.metrics ?? []} onManage={()=>setManage(true)} onBatch={()=>setBatch(true)} key={`${cursor.id}-${cursor.row}-${cursor.column}`} section={current}
      sources={layout.sources} values={values} row={cursor.row} column={cursor.column} onClose={() => setCursor(null)}
      onWrite={block => write(current, cursor.row, cursor.column, block)} />}
    {editing && manage && <MetricManager layout={layout} client={client} onClose={()=>setManage(false)} onSave={m=>{
      const metrics=layout.metrics ?? []
      onChange({...layout,metrics:metrics.some(item=>item.id===m.id) ? metrics.map(item=>item.id===m.id ? m : item) : [...metrics,m]})
    }}/>}
    {editing && batch && cursor && current && <MatrixEditor client={client} layout={layout} section={current} row={cursor.row} column={cursor.column} onClose={()=>{setBatch(false);setCursor(null)}} onSave={section=>{setUndo({previous:current,applied:section});replace(section)}}/>}
    {editing && sectionDraft && <SectionEditor layout={layout} section={layout.sections.find(item => item.id === sectionDraft.id)}
      onClose={() => setSectionDraft(null)} onRemove={() => onChange({ ...layout, sections: layout.sections.filter(section => section.id !== sectionDraft.id) })}
      onSave={section => {
        if (sectionDraft.id) replace(section)
        else {
          onChange({ ...layout, sections: [...layout.sections, section] })
          setActiveId(section.id)
          if (!single) requestAnimationFrame(() => nodes.current.get(section.id)?.scrollIntoView({ block: 'start' }))
        }
      }} />}
  </div>
}
