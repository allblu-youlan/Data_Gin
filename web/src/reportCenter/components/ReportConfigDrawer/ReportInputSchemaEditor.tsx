import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { appendReportInputField, moveReportInputField, newReportInputField, orderedReportInputEntries, parseReportInputSchemaDocument, parseReportInputSchemaText, removeReportInputField, renameReportInputField, reportDateFormats, reportDateTimeFormats, reportInputControls, reportInputSchemaDocument, reportInputTypes, reportJSONContainsUnsafeNumber } from '../../refCursorConfig'
import type { ReportInputControl, ReportInputField, ReportInputFormat, ReportInputSchema, ReportInputType, ReportQueryRelation, ReportTimeDefault } from '../../types'
import { previewReportTimeDefault, reportTimeDefaultOptions } from '../../timeDefaults'
import { JSONDocumentEditor } from './JSONDocumentEditor'
import { getReportInputQueries, type ReportCenterClient } from '../../api'
import styles from './ReportInputSchemaEditor.module.css'

export function ReportInputSchemaEditor({ client, schema, onChange, relations = [], allowEmpty = false }: { client: ReportCenterClient; schema: ReportInputSchema; onChange: (schema: ReportInputSchema) => void; relations?: ReportQueryRelation[]; allowEmpty?: boolean }) {
  const entries = orderedReportInputEntries(schema)
	const [queries, setQueries] = useState<string[]>([])
	const [queryState, setQueryState] = useState({ loading: true, error: '' })
	useEffect(() => {
		const controller = new AbortController()
		void getReportInputQueries(client, controller.signal).then((response) => {
			if (controller.signal.aborted) return
			if (!response.ok) { setQueryState({ loading: false, error: response.error }); return }
			setQueries(response.data)
			setQueryState({ loading: false, error: '' })
		})
		return () => controller.abort()
	}, [client])
  function add() {
    let index = entries.length
    let next = newReportInputField(index)
    while (Object.hasOwnProperty.call(schema, next[0])) {
      index += 1
      next = newReportInputField(index)
    }
    onChange(appendReportInputField(schema, next[0], next[1]))
  }
  return <div className={styles.editor}>
    <JSONDocumentEditor label="筛选条件 JSON" description="每个键对应一个 conditions 参数。defaultTime 可配置动态时间，例如 CURRENT_MONTH 按执行时北京时间得到 202610；日期格式由 format 决定。" value={reportInputSchemaDocument(schema)} parse={(value) => parseReportInputSchemaDocument(value, allowEmpty)} parseText={allowEmpty ? (text) => parseReportInputSchemaDocument(JSON.parse(text) as unknown, true) : parseReportInputSchemaText} onChange={onChange} />
    <div className={styles.tableHeader}><div><h3>表格编辑</h3><p>表格修改会立即回写上方 JSON；日期只选择到日，日期时间选择到秒，最终传值仍是所选格式的字符串。</p></div><button type="button" onClick={add}><Plus aria-hidden="true" />新增筛选</button></div>
    <div className={styles.rows}>
      {entries.map(([code, field], index) => <InputFieldRow relations={relations} code={code} field={field} queries={queries} queryState={queryState} key={code} canMoveUp={index > 0} canMoveDown={index < entries.length - 1} onMove={(offset) => onChange(moveReportInputField(schema, code, offset))} onRename={(nextCode) => onChange(renameReportInputField(schema, code, nextCode))} onChange={(nextField) => onChange({ ...schema, [code]: nextField })} onDelete={() => onChange(removeReportInputField(schema, code))} />)}
      {entries.length === 0 ? <p className={styles.empty}>至少新增一个筛选条件，或在上方粘贴 JSON 后点击“应用 JSON”。</p> : null}
    </div>
  </div>
}

function InputFieldRow({ relations, code, field, queries, queryState, canMoveUp, canMoveDown, onMove, onRename, onChange, onDelete }: { relations: ReportQueryRelation[]; code: string; field: ReportInputField; queries: string[]; queryState: { loading: boolean; error: string }; canMoveUp: boolean; canMoveDown: boolean; onMove: (offset: -1 | 1) => void; onRename: (code: string) => void; onChange: (field: ReportInputField) => void; onDelete: () => void }) {
	const queryCompatible = ['str', 'number', 'list[str]', 'list[number]'].includes(field.type)
  const timeOptions = reportTimeDefaultOptions(field)
  return <div className={styles.row}>
    <EditorField label="条件字段"><input className={styles.mono} value={code} onChange={(event) => onRename(event.currentTarget.value)} /></EditorField>
    <EditorField label="筛选显示名"><input value={field.displayName} onChange={(event) => onChange({ ...field, displayName: event.currentTarget.value })} /></EditorField>
    <EditorField label="JSON 类型"><select value={field.type} onChange={(event) => onChange(changeInputType(field, event.currentTarget.value as ReportInputType))}>{reportInputTypes.map((type) => <option key={type}>{type}</option>)}</select></EditorField>
    <EditorField label="查询控件"><select value={field.control} onChange={(event) => onChange(changeInputControl(field, event.currentTarget.value as ReportInputControl | ''))}>{reportInputControls.map((control) => <option value={control} key={control || 'AUTO'}>{inputControlLabel(control)}</option>)}</select></EditorField>
		{queryCompatible ? <EditorField className={field.queryName ? styles.queryField : ''} label="Oracle 查询名称（可选）"><select value={field.queryName ?? ''} disabled={queryState.loading} onChange={(event) => onChange(withQueryName(field, event.currentTarget.value))}><option value="">{queryState.loading ? '正在加载查询…' : '不使用 Oracle 查询'}</option>{field.queryName && !queries.includes(field.queryName) ? <option value={field.queryName}>{field.queryName}（当前未启用）</option> : null}{queries.map((query) => <option value={query} key={query}>{query}</option>)}</select>{field.queryName ? <small>查询单独维护；这里仅绑定名称。页面显示 name，提交 id。</small> : queries.length === 0 && !queryState.loading ? <small>请先在报表配置页新增查询。</small> : null}{queryState.error ? <small className={styles.queryError}>{queryState.error}</small> : null}</EditorField> : null}
    {relations.length && ['str', 'list[str]'].includes(field.type) ? <EditorField label="关联记录选项（远程搜索）"><select value={field.relationId ?? ''} onChange={(event) => { const relationId = event.currentTarget.value; const next = { ...field }; delete next.queryName; delete next.allowedValues; delete next.defaultTime; if (relationId) { next.relationId = relationId; next.relationKeyColumnId = relations.find((relation) => relation.id === relationId)?.keyBindings[0]?.targetColumnId; next.control = 'SELECT' } else { delete next.relationId; delete next.relationKeyColumnId }; onChange(next) }}><option value="">不使用关联选项</option>{relations.map((relation) => <option key={relation.id} value={relation.id}>{relation.name || relation.id}</option>)}</select>{field.relationId ? <small>按已发布关系远程分页，提交原始键文本。</small> : null}</EditorField> : null}
    {field.relationId ? <EditorField label="提交主键分量"><select value={field.relationKeyColumnId ?? ''} onChange={(event) => onChange({ ...field, relationKeyColumnId: event.currentTarget.value || undefined })}><option value="">单键自动选择</option>{relations.find((relation) => relation.id === field.relationId)?.keyBindings.map((binding) => <option key={binding.targetColumnId} value={binding.targetColumnId}>目标字段 #{binding.targetColumnId}</option>)}</select></EditorField> : null}
    {field.control === 'DATE' || field.control === 'DATETIME' ? <EditorField className={styles.formatField} label="日期传值格式（必选）"><select className={styles.mono} value={field.format ?? defaultFormat(field.control)} onChange={(event) => onChange({ ...field, format: event.currentTarget.value as ReportInputFormat })}>{(field.control === 'DATE' ? reportDateFormats : reportDateTimeFormats).map((format) => <option value={format} key={format}>{inputFormatLabel(format)}</option>)}</select><small>传入示例：{inputFormatExample(field.format ?? defaultFormat(field.control))}</small></EditorField> : null}
    <JSONValueInput label="示例值" value={field.example} exists={Object.hasOwnProperty.call(field, 'example')} numeric={field.type === 'number' || field.type === 'list[number]'} onChange={(value, exists) => onChange(withOptional(field, 'example', value, exists))} />
    {timeOptions.length ? <EditorField label="默认值来源"><select value={field.defaultTime ?? ''} onChange={(event) => {
      const next = { ...field }
      if (event.currentTarget.value) { next.defaultTime = event.currentTarget.value as ReportTimeDefault; delete next.default; delete next.allowedValues }
      else delete next.defaultTime
      onChange(next)
    }}><option value="">固定值或留空</option>{timeOptions.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select>{field.defaultTime ? <small>当前预览：{previewReportTimeDefault(field)} · 每次查询按北京时间更新，可手动覆盖。</small> : null}</EditorField> : null}
    {field.defaultTime ? null : <JSONValueInput label="默认值" value={field.default} exists={Object.hasOwnProperty.call(field, 'default')} numeric={field.type === 'number' || field.type === 'list[number]'} onChange={(value, exists) => onChange(withOptional(field, 'default', value, exists))} />}
		{field.queryName || field.relationId || field.defaultTime ? null : <JSONValueInput label="允许值" value={field.allowedValues} exists={Boolean(field.allowedValues)} array numeric={field.type === 'number' || field.type === 'list[number]'} onChange={(value, exists) => onChange(withOptional(field, 'allowedValues', value as unknown[], exists))} />}
    <div className={styles.flags}><label><input type="checkbox" checked={field.required} onChange={(event) => onChange({ ...field, required: event.currentTarget.checked })} />必填</label></div>
		<div className={styles.actions}><button type="button" disabled={!canMoveUp} aria-label={`上移筛选条件 ${field.displayName || code}`} onClick={() => onMove(-1)}><ArrowUp aria-hidden="true" /></button><button type="button" disabled={!canMoveDown} aria-label={`下移筛选条件 ${field.displayName || code}`} onClick={() => onMove(1)}><ArrowDown aria-hidden="true" /></button><button className={styles.delete} type="button" aria-label={`删除筛选条件 ${field.displayName || code}`} onClick={onDelete}><Trash2 aria-hidden="true" /></button></div>
  </div>
}

function EditorField({ label, className = '', children }: { label: string; className?: string; children: React.ReactNode }) {
  return <label className={[styles.field, className].filter(Boolean).join(' ')}><span>{label}</span>{children}</label>
}

function JSONValueInput({ label, value, exists, array = false, numeric = false, onChange }: { label: string; value: unknown; exists: boolean; array?: boolean; numeric?: boolean; onChange: (value: unknown, exists: boolean) => void }) {
  const serialized = exists ? JSON.stringify(value) : ''
  const [text, setText] = useState(serialized)
  const [invalid, setInvalid] = useState(false)
  useEffect(() => { setText(serialized); setInvalid(false) }, [serialized])
  return <EditorField label={`${label} JSON`}><input className={styles.mono} value={text} aria-invalid={invalid || undefined} placeholder={array ? '["A","B"]' : '"01"'} onChange={(event) => setText(event.currentTarget.value)} onBlur={() => {
    if (!text.trim()) { setInvalid(false); onChange(undefined, false); return }
    try {
      if (numeric && reportJSONContainsUnsafeNumber(text)) { setInvalid(true); return }
      const decoded = JSON.parse(text) as unknown
      if (array && (!Array.isArray(decoded) || decoded.length === 0)) { setInvalid(true); return }
      setInvalid(false)
      onChange(decoded, true)
    } catch { setInvalid(true) }
  }} /></EditorField>
}

function withOptional<K extends 'example' | 'default' | 'allowedValues'>(field: ReportInputField, key: K, value: ReportInputField[K], exists: boolean): ReportInputField {
  const next = { ...field }
  if (exists) next[key] = value
  else delete next[key]
  return next
}

function changeInputType(field: ReportInputField, type: ReportInputType): ReportInputField {
	if (field.control !== 'DATE' && field.control !== 'DATETIME') {
		const next = { ...field, type }
		if (!['str', 'number', 'list[str]', 'list[number]'].includes(type)) delete next.queryName
    if (!reportTimeDefaultOptions(next).some((option) => option.value === next.defaultTime)) delete next.defaultTime
    if (!['str', 'list[str]'].includes(type)) { delete next.relationId; delete next.relationKeyColumnId }
		return next
	}
  const control: ReportInputControl = type === 'number' ? 'NUMBER' : type === 'bool' ? 'CHECKBOX' : type === 'str' ? field.control : 'TEXTAREA'
  const next = { ...field, type, control }
  if (control !== 'DATE' && control !== 'DATETIME') delete next.format
  if (!reportTimeDefaultOptions(next).some((option) => option.value === next.defaultTime)) delete next.defaultTime
  return next
}

function changeInputControl(field: ReportInputField, control: ReportInputControl | ''): ReportInputField {
  const next = { ...field, control, ...((control === 'DATE' || control === 'DATETIME') ? { type: 'str' as const, format: defaultFormat(control) } : {}) }
  if (control !== 'DATE' && control !== 'DATETIME') delete next.format
  if (!reportTimeDefaultOptions(next).some((option) => option.value === next.defaultTime)) delete next.defaultTime
	if (control !== 'SELECT') { delete next.queryName; delete next.relationId; delete next.relationKeyColumnId }
  return next
}

function withQueryName(field: ReportInputField, value: string): ReportInputField {
	const next = { ...field }
	const queryName = value.trim()
	if (!queryName) delete next.queryName
	else {
		next.control = 'SELECT'
		next.queryName = queryName
    delete next.defaultTime
    delete next.relationId
    delete next.relationKeyColumnId
		delete next.allowedValues
	}
	return next
}

function defaultFormat(control: 'DATE' | 'DATETIME'): ReportInputFormat {
  return control === 'DATE' ? 'YYYYMMDD' : 'YYYY-MM-DD HH:mm:ss'
}

function inputControlLabel(control: ReportInputControl | '') {
  if (!control) return '自动选择'
  if (control === 'DATE') return '日期（精确到日）'
  if (control === 'DATETIME') return '日期时间（精确到秒）'
  return control
}

function inputFormatLabel(format: ReportInputFormat) {
  return `${format}（${inputFormatExample(format)}）`
}

function inputFormatExample(format: ReportInputFormat) {
  const examples: Record<ReportInputFormat, string> = {
    YYYYMMDD: '20260504',
    'YYYY-MM-DD': '2026-05-04',
    YYYYMMDDHHmmss: '20260504132500',
    'YYYY-MM-DD HH:mm:ss': '2026-05-04 13:25:00',
    ISO8601: '2026-05-04T13:25:00',
  }
  return examples[format]
}
