import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Download, Maximize2, Minimize2, Plus, RefreshCw, Save } from 'lucide-react'
import { Button, Dialog } from '../ui'
import type { ReportCenterClient } from '../reportCenter/api'
import { GmvTemplateEditor } from './GmvTemplateEditor'
import { templateSectionId, updateReportDate } from './gmvTemplate'
import { snapshotValues } from './formula'
import { BoardCanvas } from './BoardCanvas'
import { BoardRequestError, boardRequest, saveDownload } from './api'
import { newBoard, type BoardLayout, type Snapshot, type TableBoard } from './types'
import styles from './BoardCanvas.module.css'

type Summary = Pick<TableBoard, 'id' | 'name' | 'revision'>
const errorText = (e: unknown) => e instanceof Error ? e.message : '操作失败，请重试。'

export function TableBoardPage({ client }: { client: ReportCenterClient }) {
  const [boards, setBoards] = useState<Summary[]>([])
  const [draft, setDraft] = useState<TableBoard>(newBoard)
  const [saved, setSaved] = useState<TableBoard | null>(null)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [dateError,setDateError]=useState('')
  const [template,setTemplate]=useState(false)
  const [busy, setBusy] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [large, setLarge] = useState(false)
  const [timed, setTimed] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [error, setError] = useState('')
  const [edited, setEdited] = useState(false)
  const [listRevision, setListRevision] = useState(0)
  const [leave,setLeave] = useState<{enter:()=>void; discard:()=>void} | null>(null)
  const [remoteRevision,setRemoteRevision] = useState<number | null>(null)
  const dateRequired=draft.layout.sections.some(section=>section.id===templateSectionId)
  const dirty = edited
  const generation = useRef(0)
  const calculation = useRef<AbortController | null>(null)
  const cancelCalculation = useCallback(() => {
    calculation.current?.abort()
    calculation.current = null
    setRefreshing(false)
  }, [])
  useEffect(() => () => { generation.current++; calculation.current?.abort() }, [])
  useEffect(() => {
    const abort = new AbortController()
    async function load() {
      const items: Summary[] = []
      let cursor = 0
      do {
        const page = await boardRequest<{items: Summary[]; nextAfterId: number}>(client, cursor ? '?afterId=' + cursor : '', 'GET', undefined, abort.signal)
        items.push(...page.items)
        cursor = page.nextAfterId
      } while (cursor && !abort.signal.aborted)
      if (!abort.signal.aborted) setBoards(items)
    }
    void load().catch(e => { if (!abort.signal.aborted) setError(errorText(e)) })
    return () => abort.abort()
  }, [client, listRevision])
  useEffect(() => {
    if (!dirty) return
    function beforeUnload(e: BeforeUnloadEvent) { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', beforeUnload)
    function beforeNavigate(e: Event) {
      e.preventDefault()
      const enter = (e as CustomEvent<{enter:()=>void}>).detail.enter
      setLeave({enter,discard:enter})
    }
    window.addEventListener('console-before-navigate',beforeNavigate)
    return () => {
      window.removeEventListener('beforeunload',beforeUnload)
      window.removeEventListener('console-before-navigate',beforeNavigate)
    }
  }, [dirty])
  const refresh = useCallback(async (board: TableBoard) => {
    if (calculation.current || !board.id) return
    const controller = new AbortController()
    calculation.current = controller
    setRefreshing(true)
    setError('')
    try {
      const result = await boardRequest<Snapshot>(client, '/' + board.id + '/evaluate', 'POST', { revision: board.revision }, controller.signal)
      if (!controller.signal.aborted && result.revision === board.revision) setSnapshot(result)
    } catch (e) {
      if (!controller.signal.aborted) {
        setError(errorText(e))
        setSnapshot(null)
        if (e instanceof BoardRequestError && e.status === 409) setConflict(true)
      }
    } finally {
      if (calculation.current === controller) { calculation.current = null; setRefreshing(false) }
    }
  }, [client])
  useEffect(() => {
    if (!large || !timed || !saved || dirty) return
    const timer = window.setInterval(() => void refresh(saved), 60000)
    return () => clearInterval(timer)
  }, [large, timed, saved, dirty, refresh])
  useEffect(() => {
    if (!conflict || !draft.id) { setRemoteRevision(null); return }
    const ac=new AbortController()
    void boardRequest<TableBoard>(client,'/'+draft.id,'GET',undefined,ac.signal).then(b=>{
      if (!ac.signal.aborted) setRemoteRevision(b.revision)
    }).catch(()=>{})
    return ()=>ac.abort()
  },[client,conflict,draft.id])
  function accept(board: TableBoard) {
    setDateError('')
    cancelCalculation()
    setSaved(board); setDraft(board); setEdited(false); setError(''); setConflict(false); setSnapshot(null)
    void refresh(board)
  }
  function open(id: number) {
    const next = () => void loadBoard(id)
    if (dirty) setLeave({enter:next,discard:next})
    else next()
  }
  async function loadBoard(id: number) {
    setDateError('')
    const sequence = ++generation.current
    cancelCalculation()
    setBusy(true); setError('')
    try {
      if (id) {
        const board = await boardRequest<TableBoard>(client, '/' + id)
        if (sequence === generation.current) accept(board)
      } else {
        setSaved(null); setDraft(newBoard()); setEdited(false); setSnapshot(null); setConflict(false)
      }
    } catch (e) { if (sequence === generation.current) setError(errorText(e)) }
    finally { if (sequence === generation.current) setBusy(false) }
  }
  function edit(next: TableBoard) { setDraft(next); setEdited(true); cancelCalculation() }
  async function save(copy = false) {
    setBusy(true); setError('')
    const id = copy ? 0 : draft.id
    try {
      const board = await boardRequest<TableBoard>(client, id ? '/' + id : '', id ? 'PUT' : 'POST', { name: draft.name, layout: draft.layout, revision: copy ? 0 : draft.revision })
      accept(board)
      setBoards(items => [board, ...items.filter(b => b.id !== board.id)])
      return true
    } catch (e) {
      setError(errorText(e))
      if (e instanceof BoardRequestError && e.status === 409) setConflict(true)
      if (e instanceof BoardRequestError && [401, 403].includes(e.status)) setSnapshot(null)
      return false
    } finally { setBusy(false) }
  }
  async function remove() {
    if (!draft.id || !window.confirm('删除此看板？')) return
    setBusy(true)
    try {
      await boardRequest(client, '/' + draft.id + '?revision=' + draft.revision, 'DELETE')
      cancelCalculation(); setBoards(items => items.filter(b => b.id !== draft.id)); setSaved(null); setDraft(newBoard()); setEdited(false); setSnapshot(null); setConflict(false); setError('')
    } catch (e) { setError(errorText(e)); if (e instanceof BoardRequestError && e.status === 409) setConflict(true) }
    finally { setBusy(false) }
  }
  async function download(format: 'csv' | 'xlsx') {
    if (!saved || !snapshot || dirty) return
    const seq = generation.current
    try {
      const reply = await boardRequest<{ content: string }>(client, '/' + saved.id + '/export', 'POST', { calculationId: snapshot.calculationId, revision: snapshot.revision, format })
      if (generation.current === seq) saveDownload(reply.content, saved.name, format)
    } catch (e) {
      if (generation.current !== seq) return
      setError(errorText(e))
      if (e instanceof BoardRequestError && [401,403,409,410].includes(e.status)) { cancelCalculation(); setSnapshot(null) }
    }
  }
  const display = useMemo(() => snapshotValues(saved?.layout,draft.layout,snapshot), [saved,draft.layout,snapshot])
  return <main className={large ? styles.largeScreen : styles.workspace} aria-label="表格看板">
    <header className={styles.pageHeader}><div><h2>{large ? draft.name : '表格看板'}</h2>
      <span className={styles.hint}>{dirty ? '有未保存更改' : saved ? '已保存 · 版本 ' + saved.revision : '新建草稿'}</span></div>
      <div className={styles.actions}>
        {!large && <><select aria-label="打开看板" value={draft.id} disabled={busy} onChange={e => void open(Number(e.target.value))}>
          <option value={0}>新建草稿</option>{boards.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}</select>
          <Button disabled={busy} onClick={() => void open(0)}><Plus/>新建看板</Button>
          <Button disabled={busy} onClick={()=>setTemplate(true)}>载入总 GMV 模板</Button></>}
        <Button disabled={!saved || busy || dirty || refreshing} onClick={() => saved && void refresh(saved)}><RefreshCw/>{refreshing ? '刷新中' : '刷新'}</Button>
        {(['xlsx','csv'] as const).map(format => <Button key={format} disabled={!snapshot || dirty || busy} onClick={() => void download(format)}><Download/>{format.toUpperCase()}</Button>)}
        <Button disabled={busy} onClick={async () => {
          const enter=()=>{setLarge(!large);setTimed(false)}
          if(!large && dirty) {
            setLeave({enter,discard:()=>{
              if(saved) {accept(saved);enter()} else {setDraft(newBoard());setEdited(false);enter()}
            }})
          } else if(large || saved || await save()) enter()
        }}>{large ? <Minimize2/> : <Maximize2/>}{large ? '退出大屏' : '进入大屏'}</Button>
      </div>
    </header>
    {!large && <div className={styles.actions}><label>看板名称 <input maxLength={100} value={draft.name} disabled={busy} onChange={e => edit({...draft, name:e.target.value})}/></label>
      <label>报告日期 <input type="date" required={dateRequired} aria-describedby={dateError ? 'board-date-error' : undefined}
        value={draft.layout.reportDate ?? ''} disabled={busy} onChange={e=>{
          try {
            const layout=updateReportDate(draft.layout,e.target.value)
            edit({...draft,layout});setDateError('')
          } catch (reason) {setDateError(errorText(reason))}
        }}/></label>
      {dateError && <small role="alert" id="board-date-error" className={styles.error}>{dateError}</small>}
      <Button variant="primary" disabled={busy || !draft.name.trim()} onClick={() => void save()}><Save/>保存看板</Button>
      {draft.id > 0 && <Button disabled={busy} onClick={() => void remove()}>删除看板</Button>}
    </div>}
    {error && <div role="alert" className={styles.error}>{error} <Button onClick={() => { setError(''); setListRevision(v => v + 1); if (saved && !dirty) void refresh(saved) }}>重试</Button></div>}
    {conflict && <div role="alert">打开时为版本 {draft.revision}，服务器版本{remoteRevision ?? '已变化'}。<Button disabled={busy} onClick={() => void save(true)}>复制为新看板</Button><Button disabled={busy} onClick={() => void open(draft.id)}>重新载入</Button></div>}
    <div className={styles.statusLine}><span>{dirty ? '草稿待保存并计算' + (snapshot ? ' · 上轮计算截至 ' + snapshot.asOf : '') : snapshot ? '计算截至 ' + snapshot.asOf : refreshing ? '正在计算…' : '尚未计算'}</span>
      {large && <label><input type="checkbox" checked={timed} onChange={e => setTimed(e.target.checked)}/>每 60 秒刷新</label>}</div>
    <fieldset className={styles.boardFieldset} disabled={busy}><BoardCanvas client={client} key={draft.id} layout={draft.layout} onChange={(layout: BoardLayout) => edit({...draft, layout})} editing={!large} values={{}} asOf={snapshot?.asOf} display={display} /></fieldset>
    {template && <GmvTemplateEditor client={client} onClose={()=>setTemplate(false)} onApply={board=>{
      setTemplate(false)
      const enter=()=>{
        generation.current++
        cancelCalculation();setSaved(null);setSnapshot(null);setConflict(false);setError('')
        setDraft(board);setEdited(true);setDateError('')
      }
      if (dirty) setLeave({enter,discard:enter})
      else enter()
    }}/>}
    {leave && <Dialog open title="有未保存更改" onClose={()=>setLeave(null)}
      footer={<><Button disabled={busy} onClick={()=>setLeave(null)}>留在编辑页</Button><Button disabled={busy} onClick={()=>{setLeave(null);leave.discard()}}>放弃这次修改</Button><Button variant="primary" disabled={busy || !draft.name.trim()} onClick={async()=>{if(await save()){setLeave(null);leave.enter()}else setLeave(null)}}>保存并继续</Button></>}>
      <p>看板「{draft.name}」的草稿尚未保存。</p>
    </Dialog>}
  </main>
}
