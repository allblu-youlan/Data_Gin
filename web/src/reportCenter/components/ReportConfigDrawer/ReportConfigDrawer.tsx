import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { defaultReportExecutionPolicy, emptyReportQuerySpec } from '../../bojunQueryConfig'
import { ReportQueryDesigner } from '../ReportQueryDesigner/ReportQueryDesigner'
import { ReportQueryValidationPanel } from '../ReportQueryDesigner/ReportQueryValidationPanel'
import { Button, Dialog } from '../../../ui'
import { ReportValidationResultContent } from '../ReportValidationResultDrawer/ReportValidationResultContent'
import { getReportDraft, getReportResultTableSchema, publishReportDraft, saveAndPublishReportDraft, saveReportDraft, type ReportCenterClient } from '../../api'
import { countReportColumnsMissingFromResultSchema, newReportInputField, parseExcelMappingDocument, parseReportInputSchemaDocument, excelMappingFromColumns, refreshReportColumnMetadataPreservingUnknown } from '../../refCursorConfig'
import type { ReportDatasource, ReportDraft, ReportPublication, ReportResultTableColumn, ReportSummary } from '../../types'
import { ReportExcelMappingEditor, type ExcelMappingSchemaState } from './ReportExcelMappingEditor'
import { ReportInputSchemaEditor } from './ReportInputSchemaEditor'
import { ReportProcedureEditor } from './ReportProcedureEditor'
import { ReportEditorNavigation, type ReportEditorTab } from './ReportEditorNavigation'
import reportDesign from '../../reportDesign.module.css'
import styles from './ReportConfigDrawer.module.css'

type Tab = ReportEditorTab
const legacyTabs: Array<{ key: Tab; label: string }> = [{ key: 'basic', label: '基本信息' }, { key: 'procedure', label: '存储过程' }, { key: 'conditions', label: '筛选条件' }, { key: 'excel', label: 'Excel 映射' }]
const oracleIdentifierPattern = /^[A-Za-z][A-Za-z0-9_$#]{0,127}$/

export function ReportConfigDrawer({ client, report, datasources, datasourcesLoading = false, datasourcesError = '', onClose, onSaved }: { client: ReportCenterClient; report: ReportSummary | null; datasources: ReportDatasource[]; datasourcesLoading?: boolean; datasourcesError?: string; onClose: () => void; onSaved?: () => void }) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const [tab, setTab] = useState<Tab>('basic')
  const [draft, setDraft] = useState<ReportDraft>(() => emptyDraft(datasources))
  const [savedFingerprint, setSavedFingerprint] = useState(() => draftFingerprint(emptyDraft(datasources)))
  const reportId = report?.id ?? 0
  const [publication, setPublication] = useState<ReportPublication | null>(null)
  const [draftRefreshFailed, setDraftRefreshFailed] = useState(false)
  const continueEditingRef = useRef<HTMLButtonElement>(null)
  const [state, setState] = useState({ loading: Boolean(report), saving: false, error: '', notice: '' })
  const [closeConfirmationOpen, setCloseConfirmationOpen] = useState(false)
  const [excelSchemaState, setExcelSchemaState] = useState<ExcelMappingSchemaState>({ status: 'idle', error: '' })
  const [excelSchema, setExcelSchema] = useState<ReportResultTableColumn[] | null>(null)
  const [excelSchemaReload, setExcelSchemaReload] = useState(0)
  const editorTabs = draft.executionMode === 'SELECT_SNAPSHOT' ? [{ key: 'basic' as const, label: '基本信息' }, { key: 'procedure' as const, label: '关联表' }, { key: 'conditions' as const, label: '查询条件' }, { key: 'excel' as const, label: '列与导出' }, { key: 'validation' as const, label: '核验与试运行' }] : legacyTabs
  const tabs = publication ? [...editorTabs, { key: 'publication' as const, label: '发布结果' }] : editorTabs

  useEffect(() => {
    if (!reportId) return
    const controller = new AbortController()
    void getReportDraft(client, reportId, controller.signal).then((response) => {
      if (controller.signal.aborted) return
      if (!response.ok) setState((current) => ({ ...current, loading: false, error: response.error }))
      else {
        setDraft(response.data)
        setSavedFingerprint(draftFingerprint(response.data))
        setState((current) => ({ ...current, loading: false }))
      }
    })
    return () => controller.abort()
  }, [client, reportId])

  useEffect(() => {
    const systemDatasource = datasources.find((item) => item.code === 'system_environment_oracle' && item.enabled)
    if (!reportId && systemDatasource) {
      setDraft((current) => current.id === 0 && current.datasourceId === 0 ? { ...current, datasourceId: systemDatasource.id } : current)
      setSavedFingerprint((current) => {
        const initial = emptyDraft()
        return current === draftFingerprint(initial) ? draftFingerprint({ ...initial, datasourceId: systemDatasource.id }) : current
      })
    }
  }, [datasources, reportId])

  useEffect(() => {
    if (closeConfirmationOpen) continueEditingRef.current?.focus()
  }, [closeConfirmationOpen])

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
      if (!draft.querySpec?.base.tableId || !draft.querySpec.columns.length) return '请加入至少一张表并添加报表列。'
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

  async function save(closeAfterSaving = false) {
    const error = validationError()
    if (error) { setState((current) => ({ ...current, error })); return }
    setState((current) => ({ ...current, saving: true, error: '', notice: '' }))
    const response = await saveReportDraft(client, draft)
    if (!response.ok) { setState((current) => ({ ...current, saving: false, error: response.error })); return }
    setDraft(response.data)
    setSavedFingerprint(draftFingerprint(response.data))
    setState((current) => ({ ...current, saving: false, notice: '草稿已保存到 MySQL。' }))
    setCloseConfirmationOpen(false)
    onSaved?.()
    if (closeAfterSaving) onClose()
  }

  async function publish() {
    const error = validationError(true)
    if (error) { setState((current) => ({ ...current, error })); return }
    setState((current) => ({ ...current, saving: true, error: '', notice: '' }))
    let published: ReportPublication
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
      published = response.publication
    } else {
      const response = await publishReportDraft(client, draft.id, draft.lockVersion)
      if (!response.ok) { setState((current) => ({ ...current, saving: false, error: response.error })); return }
      published = response.data
    }
    setPublication(published)
    setTab('publication')
    setCloseConfirmationOpen(false)
    onSaved?.()
    await refreshPublishedDraft(published.definitionId)
  }

  async function refreshPublishedDraft(definitionId: number) {
    setState((current) => ({ ...current, saving: true, error: '' }))
    const response = await getReportDraft(client, definitionId)
    setDraftRefreshFailed(!response.ok)
    if (!response.ok) {
      setState((current) => ({ ...current, saving: false, error: `版本已发布，后续草稿读取失败：${response.error}` }))
      return
    }
    setDraft(response.data)
    setSavedFingerprint(draftFingerprint(response.data))
    setState((current) => ({ ...current, saving: false, notice: '版本已发布，后续草稿已就绪，可继续编辑。' }))
  }

  const dirty = draftFingerprint(draft) !== savedFingerprint
  const requestClose = () => { if (state.saving) return; if (dirty) setCloseConfirmationOpen(true); else onClose() }
  const footer = closeConfirmationOpen ? <div className={styles.closeConfirmation} role="alert"><div><strong>当前配置有未保存修改</strong><span>保存后关闭，或放弃本次修改。</span></div><div className={styles.closeActions}><button type="button" ref={continueEditingRef} disabled={state.saving} onClick={() => { setCloseConfirmationOpen(false); document.getElementById(`report-config-tab-${tab}`)?.focus() }}>继续编辑</button><Button variant="danger" disabled={state.saving} onClick={onClose}>不保存并关闭</Button><Button variant="primary" disabled={state.loading || state.saving || draftRefreshFailed} onClick={() => void save(true)}>保存并关闭</Button></div></div> : <><span className={styles.version}>版本锁 {draft.lockVersion || '新建'} · {dirty ? '有未保存修改' : draft.id ? '草稿已保存' : '新建配置'}</span><button type="button" disabled={state.saving} onClick={requestClose}>关闭</button><button type="button" onClick={() => void save()} disabled={state.loading || state.saving || draftRefreshFailed || !dirty}>{state.saving ? '处理中…' : '仅保存草稿'}</button><Button variant="primary" onClick={() => void publish()} disabled={state.loading || state.saving || draftRefreshFailed}>{state.saving ? '处理中…' : dirty || !draft.id ? '保存并核验发布' : '核验并发布'}</Button></>
  const datasource = datasources.find((item) => item.id === draft.datasourceId)
  const sourceName = datasource?.code === 'system_environment_oracle' ? '系统环境 Oracle' : datasource?.name || '尚未选择连接'
  const stepTitles: Record<Tab, string> = { basic: '报表信息', procedure: draft.executionMode === 'SELECT_SNAPSHOT' ? '表与关系' : '存储过程', conditions: '查询条件', excel: '输出与导出', validation: '核验与试运行', publication: '发布结果' }
  const stepDescriptions: Record<Tab, string> = { basic: '为业务报表命名，并确定数据连接。', procedure: draft.executionMode === 'SELECT_SNAPSHOT' ? '连接业务数据，选择需要呈现的字段。' : '绑定 Oracle 过程和结果表。', conditions: '定义输入参数与筛选规则，让结果符合业务需要。', excel: '调整列名称、顺序，以及预览和 Excel 中的呈现。', validation: '检查配置并查看查询样本，确认后即可发布。', publication: '查看本次核验结果，或继续编辑下一版草稿。' }
  return <Dialog open className={`${reportDesign.surface} ${styles.dialog}`} title={draft.name.trim() || '新建报表'} description={<span className={styles.studioCaption}>报表工作室 <span aria-hidden="true">/</span> {draft.executionMode === 'SELECT_SNAPSHOT' ? '自定义查询' : '存储过程报表'} <span aria-hidden="true">·</span> {draft.id ? `草稿 #${draft.id}` : '未保存'}</span>} closeLabel="关闭报表配置" closeDisabled={state.saving} onClose={requestClose} footer={footer}>
    <div className={styles.studioLayout}>
    <ReportEditorNavigation tabs={tabs} active={tab} datasource={sourceName} onSelect={setTab} />
    <div id="report-config-panel" role="tabpanel" aria-labelledby={`report-config-tab-${tab}`} tabIndex={0} ref={bodyRef} className={styles.body}>
      <header className={styles.stepHeading}><div><h3>{stepTitles[tab]}</h3><p>{stepDescriptions[tab]}</p></div><span className={styles.stepProgress}>{String(tabs.findIndex((item) => item.key === tab) + 1).padStart(2, '0')} <span>/ {String(tabs.length).padStart(2, '0')}</span></span></header>
      {state.loading ? <p>正在读取草稿…</p> : tab === 'publication' && publication ? <ReportValidationResultContent publication={publication} /> : <fieldset className={styles.editorFieldset} disabled={state.saving || draftRefreshFailed} aria-busy={state.saving || undefined}><Editor dirty={dirty} tab={tab} client={client} draft={draft} datasources={datasources} datasourcesLoading={datasourcesLoading} datasourcesError={datasourcesError} onChange={setDraft} excelSchema={excelSchema} excelSchemaState={excelSchemaState} onExcelSchemaRetry={() => setExcelSchemaReload((value) => value + 1)} /></fieldset>}
      {state.error ? <div className={styles.error} role="alert">{state.error}{draftRefreshFailed && publication ? <Button disabled={state.saving} onClick={() => void refreshPublishedDraft(publication.definitionId)}>重新读取后续草稿</Button> : null}</div> : null}
      {state.notice ? <div className={styles.notice} role="status">{state.notice}</div> : null}
    </div>
    </div>
  </Dialog>
}

function Editor({ dirty, tab, client, draft, datasources, datasourcesLoading, datasourcesError, onChange, excelSchema, excelSchemaState, onExcelSchemaRetry }: { dirty: boolean; tab: Tab; client: ReportCenterClient; draft: ReportDraft; datasources: ReportDatasource[]; datasourcesLoading: boolean; datasourcesError: string; onChange: Dispatch<SetStateAction<ReportDraft>>; excelSchema: ReportResultTableColumn[] | null; excelSchemaState: ExcelMappingSchemaState; onExcelSchemaRetry: () => void }) {
  const set = <K extends keyof ReportDraft>(key: K, value: ReportDraft[K]) => onChange((current) => ({ ...current, [key]: value }))
  if (tab === 'basic') {
    const selected = datasources.find((item) => item.id === draft.datasourceId)
    const unavailable = draft.datasourceId > 0 && !selected
    return <div className={styles.form}>
      <Field label="报表类型"><select value={draft.executionMode === 'SELECT_SNAPSHOT' ? 'SELECT_SNAPSHOT' : 'PROCEDURE'} disabled={draft.id > 0} onChange={(event) => onChange(event.currentTarget.value === 'SELECT_SNAPSHOT' ? { ...draft, executionMode: 'SELECT_SNAPSHOT', querySpec: emptyReportQuerySpec(), executionPolicy: { ...defaultReportExecutionPolicy }, procedure: emptyProcedure(), result: emptyResult(), columns: [], inputSchema: {} } : { ...draft, executionMode: 'TABLE_SNAPSHOT', querySpec: undefined, executionPolicy: undefined, inputSchema: Object.keys(draft.inputSchema).length ? draft.inputSchema : Object.fromEntries([newReportInputField(0)]) })}><option value="PROCEDURE">存储过程报表</option><option value="SELECT_SNAPSHOT">自定义查询报表</option></select><small>已有报表类型保持固定。</small></Field>
      <Field label="报表名称"><input name="report-name" autoComplete="off" value={draft.name} onChange={(event) => set('name', event.currentTarget.value)} /></Field>
      <Field label="报表编码"><input name="report-code" autoComplete="off" spellCheck={false} value={draft.code} onChange={(event) => set('code', event.currentTarget.value)} /></Field>
      <Field label="分类"><input name="report-category" autoComplete="off" required disabled={draft.status === 'ACTIVE' && Boolean(draft.category.trim())} value={draft.category} onChange={(event) => set('category', event.currentTarget.value)} />{draft.status === 'ACTIVE' ? <small>{draft.category.trim() ? '已上线报表不能直接更换分类，避免权限边界变化。' : '历史未分类报表可补录一次分类，保存后不能直接更换。'}</small> : null}</Field>
      <div className={styles.datasourceField}><span>Oracle 数据源</span><div className={styles.datasourceSummary}><strong>{selected?.code === 'system_environment_oracle' ? '系统环境 Oracle' : selected?.name || (datasourcesLoading ? '正在加载数据源…' : '请选择已登记的数据源')}</strong><small>{selected?.code === 'system_environment_oracle' ? '使用系统环境连接，无需重复录入账号。' : '连接由服务端统一管理。'}</small></div><details open={unavailable || (!selected && !datasourcesLoading)}><summary>高级：更换已登记数据源</summary><select aria-label="选择已登记 Oracle 数据源" value={draft.datasourceId || ''} disabled={datasourcesLoading} onChange={(event) => {
        const datasourceId = Number(event.currentTarget.value)
        onChange(datasourceId === draft.datasourceId ? draft : { ...draft, datasourceId, procedure: emptyProcedure(), executionMode: draft.executionMode, result: emptyResult(), ...(draft.executionMode === 'SELECT_SNAPSHOT' ? { querySpec: emptyReportQuerySpec(), columns: [] } : {}) })
      }}><option value="">{datasourcesLoading ? '正在加载数据源…' : '请选择数据源'}</option>{unavailable ? <option value={draft.datasourceId}>#{draft.datasourceId}（当前不可读取，请勿误切换）</option> : null}{datasources.map((item) => <option value={item.id} disabled={!item.enabled} key={item.id}>{item.name} · {item.code}{item.enabled ? '' : '（已停用）'}</option>)}</select></details>{datasourcesError ? <small className={styles.fieldError}>{datasourcesError}</small> : null}{selected && !selected.enabled ? <small className={styles.fieldError}>当前绑定数据源已停用，不能发布或创建新运行。</small> : null}</div>
      <Field label="说明" wide><textarea name="report-description" autoComplete="off" rows={4} value={draft.description} onChange={(event) => set('description', event.currentTarget.value)} /></Field>
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
function emptyDraft(datasources: ReportDatasource[] = []): ReportDraft { return { id: 0, code: '', name: '', category: '', description: '', datasourceId: datasources.find((item) => item.code === 'system_environment_oracle' && item.enabled)?.id ?? 0, status: 'DRAFT', lockVersion: 0, executionMode: 'SELECT_SNAPSHOT', querySpec: emptyReportQuerySpec(), executionPolicy: { ...defaultReportExecutionPolicy }, procedure: emptyProcedure(), inputSchema: {}, result: emptyResult(), callTemplate: '', parameters: [], columns: [], grants: [], createdAt: null, updatedAt: null } }
function draftFingerprint(draft: ReportDraft) { return JSON.stringify(draft) }
