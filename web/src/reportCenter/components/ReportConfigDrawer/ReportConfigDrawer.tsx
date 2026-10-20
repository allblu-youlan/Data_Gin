import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { defaultReportExecutionPolicy, emptyReportQuerySpec } from '../../bojunQueryConfig'
import { ReportQueryDesigner } from '../ReportQueryDesigner/ReportQueryDesigner'
import { ReportQueryValidationPanel } from '../ReportQueryDesigner/ReportQueryValidationPanel'
import { Button, Dialog, Drawer } from '../../../ui'
import { getReportDraft, getReportResultTableSchema, publishReportDraft, saveAndPublishReportDraft, saveReportDraft, type ReportCenterClient } from '../../api'
import { countReportColumnsMissingFromResultSchema, newReportInputField, parseExcelMappingDocument, parseReportInputSchemaDocument, excelMappingFromColumns, refreshReportColumnMetadataPreservingUnknown } from '../../refCursorConfig'
import type { ReportDatasource, ReportDraft, ReportPublication, ReportResultTableColumn, ReportSummary } from '../../types'
import { ReportExcelMappingEditor, type ExcelMappingSchemaState } from './ReportExcelMappingEditor'
import { ReportInputSchemaEditor } from './ReportInputSchemaEditor'
import { ReportProcedureEditor } from './ReportProcedureEditor'
import styles from './ReportConfigDrawer.module.css'

type Tab = 'basic' | 'procedure' | 'conditions' | 'excel' | 'validation'
const legacyTabs: Array<{ key: Tab; label: string }> = [{ key: 'basic', label: '基本信息' }, { key: 'procedure', label: '存储过程' }, { key: 'conditions', label: '筛选条件' }, { key: 'excel', label: 'Excel 映射' }]
const oracleIdentifierPattern = /^[A-Za-z][A-Za-z0-9_$#]{0,127}$/

export function ReportConfigDrawer({ client, report, datasources, datasourcesLoading = false, datasourcesError = '', onClose, onPublished, onSaved }: { client: ReportCenterClient; report: ReportSummary | null; datasources: ReportDatasource[]; datasourcesLoading?: boolean; datasourcesError?: string; onClose: () => void; onPublished?: (publication: ReportPublication) => void; onSaved?: () => void }) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const [tab, setTab] = useState<Tab>('basic')
  const [draft, setDraft] = useState<ReportDraft>(() => emptyDraft())
  const [savedFingerprint, setSavedFingerprint] = useState('')
  const [state, setState] = useState({ loading: Boolean(report), saving: false, error: '', notice: '' })
  const [closeConfirmationOpen, setCloseConfirmationOpen] = useState(false)
  const [excelSchemaState, setExcelSchemaState] = useState<ExcelMappingSchemaState>({ status: 'idle', error: '' })
  const [excelSchema, setExcelSchema] = useState<ReportResultTableColumn[] | null>(null)
  const [excelSchemaReload, setExcelSchemaReload] = useState(0)
  const tabs = draft.executionMode === 'SELECT_SNAPSHOT' ? [{ key: 'basic' as const, label: '基本信息' }, { key: 'procedure' as const, label: '主表与关联' }, { key: 'conditions' as const, label: '查询条件' }, { key: 'excel' as const, label: '列与导出' }, { key: 'validation' as const, label: '核验与试运行' }] : legacyTabs

  useEffect(() => {
    if (!report) return
    const controller = new AbortController()
    void getReportDraft(client, report.id, controller.signal).then((response) => {
      if (controller.signal.aborted) return
      if (!response.ok) setState((current) => ({ ...current, loading: false, error: response.error }))
      else {
        setDraft(response.data)
        setSavedFingerprint(draftFingerprint(response.data))
        setState((current) => ({ ...current, loading: false }))
      }
    })
    return () => controller.abort()
  }, [client, report])

  useEffect(() => {
    if (draft.executionMode === 'SELECT_SNAPSHOT' || !draft.datasourceId || !draft.result.tableOwner || !draft.result.tableName) {
      setExcelSchema(null)
      setExcelSchemaState({ status: 'idle', error: '' })
      return
    }
    const datasourceId = draft.datasourceId
    const tableOwner = draft.result.tableOwner
    const tableName = draft.result.tableName
    const controller = new AbortController()
    setExcelSchema(null)
    setExcelSchemaState({ status: 'loading', error: '' })
    void getReportResultTableSchema(client, datasourceId, { owner: tableOwner, name: tableName }, controller.signal).then((response) => {
      if (controller.signal.aborted) return
      if (!response.ok) {
        setExcelSchemaState({ status: 'error', error: response.error })
        return
      }
      setExcelSchema(response.data.columns)
      setExcelSchemaState({ status: 'ready', error: '' })
      setDraft((current) => {
        if (current.datasourceId !== datasourceId || current.result.tableOwner !== tableOwner || current.result.tableName !== tableName) return current
        const columns = refreshReportColumnMetadataPreservingUnknown(response.data.columns, current.columns)
        return JSON.stringify(columns) === JSON.stringify(current.columns) ? current : { ...current, columns }
      })
    })
    return () => controller.abort()
  }, [client, draft.datasourceId, draft.executionMode, draft.result.tableName, draft.result.tableOwner, excelSchemaReload])

  function validationError(publishing = false) {
    if (bodyRef.current?.querySelector('[aria-invalid="true"]')) return '请先修正标红的配置项。'
    if (!draft.name.trim() || draft.name.trim().length > 128 || !/^[A-Za-z][A-Za-z0-9_-]{2,63}$/.test(draft.code.trim()) || !draft.category.trim() || !draft.datasourceId) return '请完整填写报表名称、合法编码、分类和 Oracle 数据源。'
    if (draft.category.trim().length > 64 || draft.description.trim().length > 500) return '分类最多 64 字，说明最多 500 字。'
    if (datasources.find((item) => item.id === draft.datasourceId)?.enabled === false) return '当前 Oracle 数据源已停用，请先启用或更换数据源。'
    if (draft.executionMode === 'SELECT_SNAPSHOT') {
      if (!publishing) return ''
      if (!draft.querySpec?.base.tableId || !draft.querySpec.columns.length) return '请选择主表并至少添加一列。'
      try { parseReportInputSchemaDocument(draft.inputSchema, true) } catch (error) { return error instanceof Error ? error.message : '输入参数不完整。' }
      return ''
    }
    if (!draft.procedure.owner || !draft.procedure.name) return '请从 Oracle 查询结果中选择存储过程。'
    if (!draft.procedure.jsonInputArgName || draft.procedure.resultCursorArgName) return '所选过程必须包含可绑定的 JSON 输入参数；允许附带字符类型的 R_ERROR OUT 参数。'
    if (!draft.result.tableOwner || !draft.result.tableName) return '请绑定 Oracle 结果表。'
    if (![draft.procedure.owner, draft.procedure.name, draft.procedure.jsonInputArgName, draft.result.tableOwner, draft.result.tableName].every((value) => oracleIdentifierPattern.test(value))) return 'Oracle 对象或字段名不合法，请从 Oracle 搜索结果中重新选择。'
    if (draft.procedure.package && !oracleIdentifierPattern.test(draft.procedure.package)) return 'Oracle 包名不合法，请重新选择存储过程。'
    if (excelSchemaState.status === 'idle' || excelSchemaState.status === 'loading') return '正在读取 Oracle 结果表字段，请稍后再保存或发布。'
    if (excelSchemaState.status === 'error') return excelSchemaState.error || 'Oracle 结果表字段读取失败，请重试。'
    if (excelSchema && countReportColumnsMissingFromResultSchema(draft.columns, excelSchema) > 0) return 'Excel 映射中有字段不属于当前 Oracle 结果表，请重新选择或删除。'
    try { parseReportInputSchemaDocument(draft.inputSchema) } catch (error) { return error instanceof Error ? error.message : '筛选条件配置不完整。' }
    try {
      const mapping = parseExcelMappingDocument(excelMappingFromColumns(draft.columns))
      if (Object.keys(mapping).length === 0) return '请至少配置一个 Oracle 结果表字段到 Excel 表头的映射。'
    } catch (error) { return error instanceof Error ? error.message : 'Excel 字段映射不完整。' }
    return ''
  }

  async function save() {
    const error = validationError()
    if (error) { setState((current) => ({ ...current, error })); return }
    setState((current) => ({ ...current, saving: true, error: '', notice: '' }))
    const response = await saveReportDraft(client, draft)
    if (!response.ok) { setState((current) => ({ ...current, saving: false, error: response.error })); return }
    setDraft(response.data)
    setSavedFingerprint(draftFingerprint(response.data))
    setState((current) => ({ ...current, saving: false, notice: '草稿已保存到 MySQL。' }))
    onSaved?.()
  }

  async function publish() {
    const error = validationError(true)
    if (error) { setState((current) => ({ ...current, error })); return }
    setState((current) => ({ ...current, saving: true, error: '', notice: '' }))
    let publication: ReportPublication
    if (dirty || !draft.id || !draft.lockVersion) {
      const response = await saveAndPublishReportDraft(client, draft)
      if (!response.ok) {
        if (response.draft) {
          setDraft(response.draft)
          setSavedFingerprint(draftFingerprint(response.draft))
        }
        setState((current) => ({ ...current, saving: false, error: response.error }))
        return
      }
      setDraft(response.draft)
      setSavedFingerprint(draftFingerprint(response.draft))
      publication = response.publication
    } else {
      const response = await publishReportDraft(client, draft.id, draft.lockVersion)
      if (!response.ok) { setState((current) => ({ ...current, saving: false, error: response.error })); return }
      publication = response.data
    }
    setState((current) => ({ ...current, saving: false, notice: `Oracle 契约核验通过，已发布版本 #${publication.versionId}。` }))
    onSaved?.()
    onClose()
    onPublished?.(publication)
  }

  const dirty = draftFingerprint(draft) !== savedFingerprint
  const requestClose = () => dirty ? setCloseConfirmationOpen(true) : onClose()
  const footer = <><span className={styles.version}>版本锁 {draft.lockVersion || '新建'} · {dirty ? '有未保存修改' : '草稿已保存'}</span><button type="button" onClick={requestClose}>关闭</button><button type="button" onClick={() => void save()} disabled={state.loading || state.saving || !dirty}>{state.saving ? '处理中…' : '仅保存草稿'}</button><Button variant="primary" onClick={() => void publish()} disabled={state.loading || state.saving}>{state.saving ? '处理中…' : dirty || !draft.id ? '保存并核验发布' : '核验并发布'}</Button></>
  return <>
    <Drawer open title={report ? '编辑报表配置' : '创建报表配置'} description={draft.executionMode === 'SELECT_SNAPSHOT' ? '通过伯俊目录配置主表、关联、字段与条件；源表只读，结果按运行隔离保留。' : '配置保存于 MySQL；Oracle 过程仅接收一份 JSON，系统整表读取运行结果并在导出成功后清理。'} size="wide" closeDisabled={state.saving} onClose={requestClose} footer={footer}>
      <div className={styles.tabs} role="tablist" aria-label="报表配置步骤" onKeyDown={(event) => { const current = tabs.findIndex((item) => item.key === tab); const delta = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0; if (!delta) return; event.preventDefault(); const next = tabs[(current + delta + tabs.length) % tabs.length]; setTab(next.key); document.getElementById(`report-config-tab-${next.key}`)?.focus() }}>{tabs.map((item, index) => <button id={`report-config-tab-${item.key}`} type="button" role="tab" aria-selected={tab === item.key} aria-controls="report-config-panel" tabIndex={tab === item.key ? 0 : -1} className={tab === item.key ? styles.active : ''} onClick={() => setTab(item.key)} key={item.key}><span aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>{item.label}</button>)}</div>
      <div id="report-config-panel" role="tabpanel" aria-labelledby={`report-config-tab-${tab}`} tabIndex={0} ref={bodyRef} className={styles.body}>
        {state.loading ? <p>正在读取草稿…</p> : <fieldset className={styles.editorFieldset} disabled={state.saving} aria-busy={state.saving || undefined}><Editor dirty={dirty} tab={tab} client={client} draft={draft} datasources={datasources} datasourcesLoading={datasourcesLoading} datasourcesError={datasourcesError} onChange={setDraft} excelSchema={excelSchema} excelSchemaState={excelSchemaState} onExcelSchemaRetry={() => setExcelSchemaReload((value) => value + 1)} /></fieldset>}
        {state.error ? <div className={styles.error} role="alert">{state.error}</div> : null}
        {state.notice ? <div className={styles.notice} role="status">{state.notice}</div> : null}
      </div>
    </Drawer>
    <Dialog open={closeConfirmationOpen} role="alertdialog" title="放弃未保存修改？" description="当前报表配置仍有未保存修改，关闭后无法恢复。" onClose={() => setCloseConfirmationOpen(false)} footer={<><button type="button" onClick={() => setCloseConfirmationOpen(false)}>继续编辑</button><Button variant="danger" onClick={onClose}>放弃修改并关闭</Button></>}><p className={styles.closeWarning}>若需要保留当前内容，请返回配置并点击“仅保存草稿”。</p></Dialog>
  </>
}

function Editor({ dirty, tab, client, draft, datasources, datasourcesLoading, datasourcesError, onChange, excelSchema, excelSchemaState, onExcelSchemaRetry }: { dirty: boolean; tab: Tab; client: ReportCenterClient; draft: ReportDraft; datasources: ReportDatasource[]; datasourcesLoading: boolean; datasourcesError: string; onChange: Dispatch<SetStateAction<ReportDraft>>; excelSchema: ReportResultTableColumn[] | null; excelSchemaState: ExcelMappingSchemaState; onExcelSchemaRetry: () => void }) {
  const set = <K extends keyof ReportDraft>(key: K, value: ReportDraft[K]) => onChange((current) => ({ ...current, [key]: value }))
  if (tab === 'basic') {
    const selected = datasources.find((item) => item.id === draft.datasourceId)
    const unavailable = draft.datasourceId > 0 && !selected
    return <div className={styles.form}>
      <Field label="报表类型"><select value={draft.executionMode === 'SELECT_SNAPSHOT' ? 'SELECT_SNAPSHOT' : 'PROCEDURE'} disabled={draft.id > 0} onChange={(event) => onChange(event.currentTarget.value === 'SELECT_SNAPSHOT' ? { ...draft, executionMode: 'SELECT_SNAPSHOT', querySpec: emptyReportQuerySpec(), executionPolicy: { ...defaultReportExecutionPolicy }, procedure: emptyProcedure(), result: emptyResult(), columns: [], inputSchema: {} } : { ...draft, executionMode: 'TABLE_SNAPSHOT', querySpec: undefined, executionPolicy: undefined })}><option value="PROCEDURE">存储过程报表</option><option value="SELECT_SNAPSHOT">自定义查询报表</option></select><small>已有报表类型保持固定。</small></Field>
      <Field label="报表名称"><input value={draft.name} onChange={(event) => set('name', event.currentTarget.value)} /></Field>
      <Field label="报表编码"><input value={draft.code} onChange={(event) => set('code', event.currentTarget.value)} /></Field>
      <Field label="分类"><input required disabled={draft.status === 'ACTIVE' && Boolean(draft.category.trim())} value={draft.category} onChange={(event) => set('category', event.currentTarget.value)} />{draft.status === 'ACTIVE' ? <small>{draft.category.trim() ? '已上线报表不能直接更换分类，避免权限边界变化。' : '历史未分类报表可补录一次分类，保存后不能直接更换。'}</small> : null}</Field>
      <Field label="Oracle 数据源"><select value={draft.datasourceId || ''} disabled={datasourcesLoading} onChange={(event) => {
        const datasourceId = Number(event.currentTarget.value)
        onChange(datasourceId === draft.datasourceId ? draft : { ...draft, datasourceId, procedure: emptyProcedure(), executionMode: draft.executionMode, result: emptyResult(), ...(draft.executionMode === 'SELECT_SNAPSHOT' ? { querySpec: emptyReportQuerySpec(), columns: [] } : {}) })
      }}><option value="">{datasourcesLoading ? '正在加载数据源…' : '请选择数据源'}</option>{unavailable ? <option value={draft.datasourceId}>#{draft.datasourceId}（当前不可读取，请勿误切换）</option> : null}{datasources.map((item) => <option value={item.id} disabled={!item.enabled} key={item.id}>{item.name} · {item.code}{item.enabled ? '' : '（已停用）'}</option>)}</select>{datasourcesError ? <small className={styles.fieldError}>{datasourcesError}</small> : null}{selected && !selected.enabled ? <small className={styles.fieldError}>当前绑定数据源已停用，不能发布或创建新运行。</small> : null}</Field>
      <Field label="说明" wide><textarea rows={4} value={draft.description} onChange={(event) => set('description', event.currentTarget.value)} /></Field>
    </div>
  }
  if (draft.executionMode === 'SELECT_SNAPSHOT') {
    if (tab === 'validation') return <ReportQueryValidationPanel client={client} draft={draft} dirty={dirty} />
    if (tab === 'conditions') return <><ReportInputSchemaEditor allowEmpty relations={draft.querySpec?.relations} client={client} schema={draft.inputSchema} onChange={(inputSchema) => set('inputSchema', inputSchema)} /><ReportQueryDesigner client={client} draft={draft} section="conditions" onChange={onChange} /></>
    return <ReportQueryDesigner client={client} draft={draft} section={tab === 'procedure' ? 'sources' : 'columns'} onChange={onChange} />
  }
  if (tab === 'procedure') return <ReportProcedureEditor client={client} draft={draft} onChange={onChange} />
  if (tab === 'conditions') return <ReportInputSchemaEditor client={client} schema={draft.inputSchema} onChange={(inputSchema) => set('inputSchema', inputSchema)} />
  return <ReportExcelMappingEditor result={draft.result} columns={draft.columns} schema={excelSchema} schemaState={excelSchemaState} onChange={(columns) => set('columns', columns)} onRetry={onExcelSchemaRetry} />
}

function Field({ label, wide, children }: { label: string; wide?: boolean; children: React.ReactNode }) { return <label className={wide ? styles.wide : ''}>{label}{children}</label> }
function emptyProcedure(): ReportDraft['procedure'] { return { owner: '', package: '', name: '', overload: '', jsonInputArgName: '', resultCursorArgName: '' } }
function emptyResult(): ReportDraft['result'] { return { tableOwner: '', tableName: '' } }
function emptyDraft(): ReportDraft { const [code, field] = newReportInputField(0); return { id: 0, code: '', name: '', category: '', description: '', datasourceId: 0, status: 'DRAFT', lockVersion: 0, executionMode: 'TABLE_SNAPSHOT', procedure: emptyProcedure(), inputSchema: { [code]: field }, result: emptyResult(), callTemplate: '', parameters: [], columns: [], grants: [], createdAt: null, updatedAt: null } }
function draftFingerprint(draft: ReportDraft) { return JSON.stringify(draft) }
