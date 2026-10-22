import { useEffect, useState } from 'react'
import { Button, DataTable, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { previewReportQuery, validateReportQuery } from '../../bojunApi'
import { buildReportConditions, initialReportConditionValues, orderedReportInputEntries } from '../../refCursorConfig'
import type { ReportDraft, ReportQueryInputs, ReportQueryPreview, ReportQueryValidation, ReportRunContract } from '../../types'
import { ReportRuntimeQueryEditor } from './ReportRuntimeQueryEditor'
import { normalizeRuntimeQueryInputs, validateRuntimeQueryInputs } from './runtimeFilters'
import { useBojunMetadata } from './useBojunMetadata'
import { reportFilterFields, reportFilterKey } from './filterConfig'
import styles from './ReportQueryDesigner.module.css'

export function ReportQueryValidationPanel({ client, draft, dirty }: { client: ReportCenterClient; draft: ReportDraft; dirty: boolean }) {
  const [validation, setValidation] = useState<ReportQueryValidation | null>(null)
  const [preview, setPreview] = useState<ReportQueryPreview | null>(null)
  const [values, setValues] = useState<Record<string, unknown>>(() => initialReportConditionValues(draft.inputSchema))
  const [state, setState] = useState({ busy: false, error: '' })
  const [queryInputs, setQueryInputs] = useState<ReportQueryInputs>({ where: { logic: 'AND', items: [] }, orderBy: [] })
  const spec = draft.querySpec
  const lookup = useBojunMetadata(client, draft.datasourceId, spec ? [spec.base.tableId, ...spec.relations.map((relation) => relation.targetTableId)] : [])
  const fields = spec ? reportFilterFields(spec, lookup.metadata) : []
  const contract: ReportRunContract = { definitionId: draft.id, versionId: 0, code: draft.code, name: draft.name, description: draft.description, executionMode: 'SELECT_SNAPSHOT', jsonInput: true, inputSchema: draft.inputSchema, parameters: [], allowedFilters: spec?.allowedFilters?.map((filter) => ({ ...filter, logicalType: fields.find((field) => reportFilterKey(field) === reportFilterKey(filter))?.logicalType })), allowedSortFields: spec?.allowedSortFields, relations: spec?.relations }
  useEffect(() => { setValidation(null); setPreview(null); setQueryInputs({ where: { logic: 'AND', items: [] }, orderBy: [] }) }, [draft.lockVersion])
  async function run(sample: boolean) {
    if (!draft.id || dirty || state.busy) return
    setState({ busy: true, error: '' })
    if (!sample) {
      const response = await validateReportQuery(client, draft.id, draft.lockVersion)
      if (!response.ok) { setState({ busy: false, error: response.error }); return }
      setValidation(response.data)
    } else {
      const error = validateRuntimeQueryInputs(contract, queryInputs)
      if (error) { setState({ busy: false, error }); return }
      const conditions = buildReportConditions(draft.inputSchema, values)
      if (!conditions.ok) { setState({ busy: false, error: conditions.error }); return }
      const response = await previewReportQuery(client, draft.id, draft.lockVersion, conditions.conditions, undefined, normalizeRuntimeQueryInputs(queryInputs))
      if (!response.ok) { setState({ busy: false, error: response.error }); return }
      setPreview(response.data)
    }
    setState({ busy: false, error: '' })
  }
  return <div className={styles.editor}><p className={styles.notice}>试运行最多读取 100 行、最长 10 秒，仅用于核验样本。正式查询通过已发布版本执行并保存独立结果。</p>{dirty || !draft.id ? <p role="status">请先点击“仅保存草稿”，再核验或试运行当前配置。</p> : null}<div className={styles.form}>{orderedReportInputEntries(draft.inputSchema).map(([code, field]) => <label className={styles.field} key={code}>{field.displayName}{field.required ? ' *' : ''}<input value={typeof values[code] === 'object' ? JSON.stringify(values[code]) : String(values[code] ?? '')} onChange={(event) => { const value = event.currentTarget.value; setValues((previous) => ({ ...previous, [code]: value })) }} /><small>{code} · {field.type}</small></label>)}</div><ReportRuntimeQueryEditor contract={contract} value={queryInputs} disabled={state.busy || dirty} onChange={setQueryInputs} /><div className={styles.actions}><button type="button" disabled={dirty || !draft.id || state.busy} onClick={() => void run(false)}>核验查询配置</button><Button variant="primary" disabled={dirty || !draft.id || state.busy} onClick={() => void run(true)}>有界试运行</Button></div>{state.busy ? <FeedbackState kind="loading" title="正在核验查询" /> : null}{state.error ? <p className={styles.error} role="alert">{state.error}</p> : null}{validation ? <section className={styles.section}><h3>{validation.valid ? '查询核验通过' : '查询配置需要修复'}</h3>{[...validation.errors, ...validation.warnings].map((issue, index) => <p key={`${issue.fieldPath}-${index}`} className={styles.error}><code>{issue.fieldPath}</code> · {issue.message}</p>)}</section> : null}{preview ? <section className={styles.section}><h3>样本 {preview.rows.length} 行 · {preview.elapsedMs} ms{preview.sampleOnly ? ' · 非完整结果' : ''}</h3><DataTable scrollLabel="查询试运行样本"><thead><tr>{preview.columns.map((column) => <th key={column.fieldId}>{column.header}</th>)}</tr></thead><tbody>{preview.rows.map((row) => <tr key={row.key}>{preview.columns.map((column) => <td key={column.fieldId}>{display(row.displayValues?.[column.code] ?? row.values[column.code])}</td>)}</tr>)}</tbody></DataTable></section> : null}</div>
}
function display(value: unknown) { return value === null || value === undefined ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value) }
