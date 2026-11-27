import { useRef, useState } from 'react'
import { Button, Drawer } from '../ui'
import { columnName, parseClipboardTable } from './clipboard'
import { maxColumns, maxRows, maxTextLength } from './grid'
import type { ReportCenterClient } from "../reportCenter/api"
import { FormulaEditor } from "./FormulaEditor"
import type { DisplayCell, Metric, BoardCell, BoardSection, BoardSource, BoardValues } from './types'
import styles from './BoardCanvas.module.css'

type CellEditorProps = {
  result?: DisplayCell
  asOf?: string
  client: ReportCenterClient
  metrics: Metric[]
  onManage: () => void
  onBatch: () => void
  section: BoardSection
  row: number
  column: number
  sources: BoardSource[]
  values: BoardValues
  onWrite: (block: BoardCell[][]) => void
  onClose: () => void
}

export function CellEditor({ client, result, asOf, metrics, onBatch, onManage, section, row, column, sources, values, onWrite, onClose }: CellEditorProps) {
  const cell = section.cells[row][column]
  const [mode, setMode] = useState<'field' | 'text' | 'paste' | 'formula'>(cell.kind)
  const [text, setText] = useState(cell.kind === 'text' ? cell.text : '')
  const [paste, setPaste] = useState('')
  const [sourceId, setSourceId] = useState(cell.kind === 'field' ? cell.sourceId : sources[0]?.id ?? '')
  const source = sources.find(item => item.id === sourceId)
  const [field, setField] = useState(cell.kind === 'field' ? cell.field : source?.fields[0] ?? '')
  const [start, setStart] = useState(String(cell.kind === 'field' ? cell.row + 1 : 1))
  const [height, setHeight] = useState('1')
  const [width, setWidth] = useState('1')
  const [withHeaders, setWithHeaders] = useState(false)
  const textInput = useRef<HTMLTextAreaElement>(null)
  const sourceSelect = useRef<HTMLSelectElement>(null)
  const [error, setError] = useState('')
  const address = `${columnName(column)}${row + 1}`
  const index = source?.fields.indexOf(field) ?? -1
  const availableColumns = Math.min(maxColumns - column, (source?.fields.length ?? 0) - Math.max(0, index))
  const availableRows = Math.min(maxRows - row - Number(withHeaders), (source?.limit ?? 0) - Number(start) + 1)

  const loadedRows = Math.min(availableRows, (values[sourceId]?.length ?? 0) - Number(start) + 1)

  function apply() {
    try {
      let block: BoardCell[][]
      if (mode === 'text') {
        block = [[{ kind: 'text', text }]]
      } else if (mode === 'paste') {
        if (!paste) throw new Error('请先粘贴表格。')
        block = parseClipboardTable(paste).map(line => line.map(value => ({ kind: 'text', text: value })))
      } else {
        if (!source || index < 0) throw new Error('请选择可用的数据源和起始字段。')
        const [first, rows, cols] = [Number(start), Number(height), Number(width)]
        if (![first, rows, cols].every(n => Number.isInteger(n) && n >= 1) || cols > availableColumns || rows > availableRows) {
          throw new Error('请检查起始记录和填充范围，范围须在区块容量和数据源取数上限内。')
        }
        block = Array.from({ length: rows }, (_, r) =>
          Array.from({ length: cols }, (_, c) => ({
            kind: 'field', sourceId: source.id, field: source.fields[index + c], row: first + r - 1,
          })),
        )
        if (withHeaders) block.unshift(source.fields.slice(index, index + cols).map(name => ({ kind: 'text', text: name })))
      }
      onWrite(block)
      onClose()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '写入单元格失败。')
    }
  }

  return <Drawer open title={`编辑单元格 ${address}`} description={section.title} onClose={onClose} initialFocusRef={cell.kind === 'text' ? textInput : sourceSelect}
    footer={<><Button onClick={onClose}>取消</Button>{mode !== 'formula' && <Button variant="primary" onClick={apply}>应用</Button>}</>}>
    <div className={styles.editor}>
      {result && <div className={styles.preview}><strong>当前画面 · {result.display}</strong>
        {result.message && <span>{result.message}</span>}
        <small>计算截至 {asOf}{result.start && ' · 统计期间 ' + result.start + ' 至 ' + result.end}{cell.kind === 'formula' && result.matched !== undefined && ' · 匹配 ' + result.matched + ' 条'}</small>
      </div>}
      <div className={styles.segment} role="group" aria-label="单元格内容类型">
        {(['text', 'formula', 'field', 'paste'] as const).map(key => <button type="button" key={key} aria-pressed={mode === key}
          onClick={() => { setMode(key); setError('') }}>{{ text: '固定文本', formula:'指标公式', field: '数据字段', paste: '粘贴表格' }[key]}</button>)}
      </div>
      {mode === 'formula' && <FormulaEditor client={client} metrics={metrics} initial={cell.kind === 'formula' ? cell : undefined} address={address} onApply={f=>{onWrite([[f]]);onClose()}} onManage={onManage}/>}
      <Button onClick={onBatch}>批量配置 · 门店 × 期间</Button>
      {mode === 'text' && <label>单元格内容<textarea ref={textInput} rows={4} value={text} maxLength={maxTextLength}
        onPaste={event => {
          const copied = event.clipboardData.getData('text/plain')
          if (copied.includes('\t') || (copied.includes('\n') && /<table[\s>]/i.test(event.clipboardData.getData('text/html')))) {
            event.preventDefault()
            setPaste(copied)
            setMode('paste')
          }
        }}
        onChange={event => setText(event.target.value)} /></label>}
      {mode === 'paste' && <label>从 Excel 复制的单元格<textarea rows={6} value={paste}
        onChange={event => setPaste(event.target.value)} placeholder={`从 ${address} 向右、向下填入，原有内容将被覆盖`} /></label>}
      {mode === 'field' && (sources.length > 0 ? <>
        <label>数据源<select ref={sourceSelect} value={sourceId} onChange={event => {
          const next = sources.find(item => item.id === event.target.value)
          setSourceId(event.target.value); setField(next?.fields[0] ?? ''); setStart('1'); setWidth('1'); setHeight('1')
        }}>
          {!source && <option value={sourceId}>请选择数据源</option>}
          {sources.map(item => <option value={item.id} key={item.id}>{item.name}</option>)}
        </select></label>
        <div className={styles.inputs}>
          <label>起始字段<select value={field} onChange={event => { setField(event.target.value); setWidth('1') }}>
            {index < 0 && <option value={field}>请选择字段</option>}
            {source?.fields.map(key => <option key={key} value={key}>{key}</option>)}
          </select></label>
          <label>起始记录（第几条）<input type="number" min={1} max={source?.limit} value={start} onChange={event => setStart(event.target.value)} /></label>
          <label>向下填充（行）<input type="number" min={1} max={availableRows} value={height} onChange={event => setHeight(event.target.value)} /></label>
          <label>向右填充（字段数）<input type="number" min={1} max={availableColumns} value={width} onChange={event => setWidth(event.target.value)} /></label>
        </div>
        <div className={styles.fillOptions}>
          <label><input type="checkbox" checked={withHeaders} onChange={event => setWithHeaders(event.target.checked)} />首行写入字段名</label>
          <Button disabled={index < 0 || loadedRows < 1 || availableColumns < 1} onClick={() => {
            setHeight(String(loadedRows)); setWidth(String(availableColumns))
          }}>按已加载结果填充范围</Button>
        </div>
        <p className={styles.hint}>从 {address} 开始按记录和字段顺序填充，覆盖所选范围。数据刷新时绑定值会同步更新。</p>
      </> : <p className={styles.hint}>请先在看板中添加数据源，再选择字段。</p>)}
      {error && <p className={styles.error} role="alert">{error}</p>}
    </div>
  </Drawer>
}
