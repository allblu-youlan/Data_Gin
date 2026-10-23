import { type FormEvent, useEffect, useRef, useState } from 'react'
import { Dialog, FeedbackState } from '../ui'
import { ExcelWorkspace } from './ExcelWorkspace'
import { deleteOracleExportScheme, getOracleExportSchemes, saveOracleExportScheme, type OracleExportConfig, type OracleExportScheme } from './oracleExportSchemes'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './OracleExportPage.module.css'

export function OracleExportSchemeBar({ client, canManage, disabled, canSave, refreshVersion, getConfig, onLoad }: {
  client: ExcelPageClient
  canManage: boolean
  disabled: boolean
  canSave: boolean
  refreshVersion: number
  getConfig: () => OracleExportConfig
  onLoad: (scheme: OracleExportScheme) => Promise<boolean>
}) {
  const [schemes, setSchemes] = useState<OracleExportScheme[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [activeID, setActiveID] = useState<number | null>(null)
  const [revision, setRevision] = useState(0)
  const [listLoading, setListLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [saveOpen, setSaveOpen] = useState(false)
  const [name, setName] = useState('')
  const [overwrite, setOverwrite] = useState<{ name: string; config: OracleExportConfig } | null>(null)
  const [pendingDelete, setPendingDelete] = useState<OracleExportScheme | null>(null)
  const mutation = useRef(false)
  const selected = schemes.find((scheme) => String(scheme.id) === selectedID)
  const active = schemes.find((scheme) => scheme.id === activeID)

  useEffect(() => {
    const controller = new AbortController()
    setListLoading(true)
    setError('')
    void getOracleExportSchemes(client, controller.signal).then((items) => {
      if (!controller.signal.aborted) setSchemes(items)
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '导出配置加载失败，请重试。')
    }).finally(() => { if (!controller.signal.aborted) setListLoading(false) })
    return () => controller.abort()
  }, [client, refreshVersion, revision])

  async function loadSelected() {
    if (!selected || busy || disabled) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      if (await onLoad(selected)) { setActiveID(selected.id); setNotice(`已加载配置“${selected.name}”。保存配置不会执行导出。`) }
    } finally { setBusy(false) }
  }

  async function save(saveName: string, config: OracleExportConfig) {
    if (mutation.current) return
    mutation.current = true
    setBusy(true)
    setError('')
    try {
      const scheme = await saveOracleExportScheme(client, saveName, config)
      setSchemes((current) => [scheme, ...current.filter((item) => item.id !== scheme.id)])
      setSelectedID(String(scheme.id))
      setActiveID(scheme.id)
      setSaveOpen(false)
      setOverwrite(null)
      setNotice(`配置“${scheme.name}”已保存，未创建导出任务。`)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '导出配置保存失败，请重试。') }
    finally { mutation.current = false; setBusy(false) }
  }

  function updateCurrent() {
    if (!active) return
    try { void save(active.name, getConfig()) }
    catch (cause) { setError(cause instanceof Error ? cause.message : '请先完成导出配置。') }
  }

  function submitSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    try {
      const config = getConfig()
      const trimmed = name.trim()
      if (!trimmed) throw new Error('请填写配置名称。')
      if (schemes.some((scheme) => scheme.name.toLocaleLowerCase() === trimmed.toLocaleLowerCase())) setOverwrite({ name: trimmed, config })
      else void save(trimmed, config)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '请先完成导出配置。') }
  }

  async function confirmDelete() {
    if (!pendingDelete || mutation.current) return
    mutation.current = true
    setBusy(true)
    setError('')
    try {
      await deleteOracleExportScheme(client, pendingDelete.id)
      setSchemes((current) => current.filter((item) => item.id !== pendingDelete.id))
      if (activeID === pendingDelete.id) setActiveID(null)
      setSelectedID('')
      setPendingDelete(null)
      setNotice('导出配置已删除，当前表单内容保留。')
    } catch (cause) { setError(cause instanceof Error ? cause.message : '导出配置删除失败，请重试。') }
    finally { mutation.current = false; setBusy(false) }
  }

  const blocked = disabled || busy || listLoading
  return <ExcelWorkspace className={styles.configuration} title="导出配置" description={active ? `当前配置：${active.name}。修改表单后可更新当前配置，或另存为新配置。` : '保存各 Sheet 的名称、数据表、导出字段、Excel 列名和筛选条件，便于下次直接加载。'}>
    <div className={styles.stack}>
      <div className={styles.schemeBar}>
        <label>已保存配置<select aria-label="已保存导出配置" value={selectedID} disabled={blocked || listLoading} onChange={(event) => setSelectedID(event.currentTarget.value)}><option value="">{listLoading ? '正在读取配置…' : '选择已保存配置'}</option>{schemes.map((scheme) => <option key={scheme.id} value={scheme.id}>{scheme.name} · {scheme.config.sheets.length} 个 Sheet</option>)}</select></label>
        <div className={styles.actions}><button type="button" disabled={blocked || !selected} onClick={() => void loadSelected()}>加载配置</button>{canManage && <><button type="button" disabled={blocked || !canSave} onClick={() => { setName(''); setError(''); setSaveOpen(true) }}>{active ? '另存为新配置' : '保存配置'}</button>{active && <button type="button" disabled={blocked || !canSave} onClick={updateCurrent}>更新当前配置</button>}<button type="button" disabled={blocked || !selected} onClick={() => { setError(''); setPendingDelete(selected ?? null) }}>删除配置</button></>}</div>
      </div>
      {notice && <p className={styles.hint} role="status">{notice}</p>}
      {error && !saveOpen && !pendingDelete && <FeedbackState kind="error" title="导出配置操作未完成" description={error} action={<button type="button" disabled={blocked || listLoading} onClick={() => setRevision((value) => value + 1)}>刷新配置列表</button>} />}
      {!canManage && <p className={styles.hint}>当前账号可加载配置，保存和删除需要 Excel 配置管理权限。</p>}
    </div>
    <Dialog open={saveOpen} className={`${styles.page} ${styles.configuration}`} title={overwrite ? '确认覆盖已有配置' : '保存导出配置'} closeDisabled={busy} onClose={() => { setSaveOpen(false); setOverwrite(null); setError('') }}>
      {overwrite ? <div className={styles.stack}><p>已存在同名配置“{overwrite.name}”。确认后将覆盖该配置全部 Sheet 的名称、数据表、字段、列名和筛选条件。</p><div className={styles.actions}><button type="button" disabled={busy} onClick={() => setOverwrite(null)}>返回修改名称</button><button type="button" disabled={busy} onClick={() => void save(overwrite.name, overwrite.config)}>确认覆盖配置</button></div></div> : <form className={styles.stack} onSubmit={submitSave}><label>配置名称<input aria-label="导出配置名称" value={name} maxLength={100} required onChange={(event) => setName(event.currentTarget.value)} disabled={busy} /></label><p className={styles.hint}>只保存当前配置，不执行导出。</p><div className={styles.actions}><button type="submit" disabled={busy}>{busy ? '保存中…' : '保存配置'}</button></div></form>}
      {error && <FeedbackState kind="error" title="配置未保存" description={error} />}
    </Dialog>
    <Dialog open={Boolean(pendingDelete)} className={`${styles.page} ${styles.configuration}`} title="删除导出配置" closeDisabled={busy} onClose={() => { setPendingDelete(null); setError('') }}>
      <div className={styles.stack}><p>确认删除配置“{pendingDelete?.name}”？已有导出任务和当前表单内容会保留。</p>{error && <FeedbackState kind="error" title="配置未删除" description={error} />}<div className={styles.actions}><button type="button" disabled={busy} onClick={() => setPendingDelete(null)}>取消</button><button type="button" disabled={busy} onClick={() => void confirmDelete()}>{busy ? '删除中…' : '确认删除配置'}</button></div></div>
    </Dialog>
  </ExcelWorkspace>
}
