import { useMemo, useState } from 'react'
import { Button, Drawer } from '../ui'
import type { ReportCenterClient } from '../reportCenter/api'
import { useReportDatasources } from '../reportCenter/useReportDatasources'
import { boardBindings, currentConnectionChoices, importedLayout, parseBoardConfig, tableIdentity, type ImportedConfig } from './importConfig'
import type { BoardLayout } from './types'
import styles from './BoardCanvas.module.css'

type Props = {client:ReportCenterClient; current:BoardLayout; onApply:(config:ImportedConfig)=>void; onClose:()=>void}

export function ImportBoardConfig({client,current,onApply,onClose}:Props) {
  const [text,setText]=useState('')
  const [preview,setPreview]=useState<ImportedConfig | null>(null)
  const [error,setError]=useState('')
  const [useName,setUseName]=useState(false)
  const [choices,setChoices]=useState<Record<string,number>>({})
  const bindings=preview ? boardBindings(preview.layout) : []
  const missing=bindings.filter(b=>!b.datasourceId)
  const connections=useReportDatasources(client,missing.length>0)
  const enabled=connections.items.filter(c=>c.enabled)
  const available=new Set(enabled.map(c=>c.id))
  const currentIds=useMemo(()=>currentConnectionChoices(current),[current])
  const selected=new Map(Object.entries(choices).filter(([key,id])=>id>0 && (available.has(id) || currentIds.get(key)===id)))
  function inspect() {
    try {
      const next=parseBoardConfig(text)
      setChoices(Object.fromEntries(currentIds))
      setPreview(next);setUseName(false);setError('')
    } catch(e) {
      setPreview(null);setError(e instanceof Error ? e.message : '配置格式无效。')
    }
  }
  function apply() {
    if (!preview) return
    try {
      onApply({layout:importedLayout(preview.layout,selected),name:useName ? preview.name : undefined})
    } catch (e) {setError(e instanceof Error ? e.message : '连接映射无效。')}
  }
  return <Drawer open title="导入看板配置" size="wide" onClose={onClose} footer={<>
    <Button onClick={onClose}>取消</Button>
    <Button variant="primary" disabled={!preview || missing.some(b=>!selected.has(tableIdentity(b)))} onClick={apply}>应用到当前草稿</Button>
  </>}>
    <div className={styles.editor}>
      <label>配置 JSON<textarea rows={9} value={text} aria-describedby="board-import-format" onChange={e=>{
        setText(e.target.value);setPreview(null);setError('')
      }}/></label>
      <small id="board-import-format">粘贴包含 layout 和可选 name 的 JSON，最大 2 MiB。</small>
      <Button disabled={!text.trim()} onClick={inspect}>预览配置</Button>
      {error && <p role="alert" className={styles.error}>{error}</p>}
      {preview && <div className={styles.preview}>
        <strong>{preview.layout.sections.length} 个区块 · {preview.layout.metrics?.length ?? 0} 个指标 · {bindings.length} 个数据来源</strong>
        <ul>{preview.layout.sections.map((section,i)=><li key={i}>{section.title} · {section.cells.length} 行 × {section.cells[0].length} 列</li>)}</ul>
        {!!preview.layout.metrics?.length && <p>指标 · {preview.layout.metrics.map(m=>m.name).join('、')}</p>}
        {bindings.map(b=>{
          const key=tableIdentity(b)
          return b.datasourceId ? <span key={key+'/'+b.datasourceId}>{key} · 连接 #{b.datasourceId}</span> :
            <label key={key}>{key} 的连接<select value={selected.get(key) ?? ''} onChange={e=>setChoices({...choices,[key]:Number(e.target.value)})}>
              <option value="">{connections.loading ? '正在读取连接…' : '选择已有连接'}</option>
              {currentIds.has(key) && !available.has(currentIds.get(key)!) && <option value={currentIds.get(key)}>当前草稿连接 #{currentIds.get(key)}</option>}
              {enabled.map(c=><option key={c.id} value={c.id}>{c.name} · #{c.id}</option>)}
            </select></label>
        })}
        {missing.length>0 && connections.error && <div role="alert">{connections.error} <Button onClick={connections.reload}>重试连接目录</Button></div>}
        {preview.name && <label><span><input type="checkbox" checked={useName} onChange={e=>setUseName(e.target.checked)}/> 使用配置名称「{preview.name}」</span></label>}
        <small>应用将替换本地草稿布局。点击「保存看板」后才写入当前看板。</small>
      </div>}
    </div>
  </Drawer>
}
