import { useEffect, useState } from 'react'
import { Button, DataTable, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { previewReportQuery, validateReportQuery } from '../../bojunApi'
import { buildReportConditions, initialReportConditionValues, orderedReportInputEntries } from '../../refCursorConfig'
import type { ReportDraft, ReportQueryPreview, ReportQueryValidation } from '../../types'
import styles from './ReportQueryDesigner.module.css'

export function ReportQueryValidationPanel({ client, draft, dirty }: { client: ReportCenterClient; draft: ReportDraft; dirty: boolean }) {
  const [validation, setValidation] = useState<ReportQueryValidation | null>(null)
  const [preview, setPreview] = useState<ReportQueryPreview | null>(null)
  const [values, setValues] = useState<Record<string, unknown>>(() => initialReportConditionValues(draft.inputSchema))
  const [state, setState] = useState({ busy: false, error: '' })
  useEffect(() => { setValidation(null); setPreview(null) }, [draft.lockVersion])
  async function run(sample: boolean) {
    if (!draft.id || dirty || state.busy) return
    setState({ busy: true, error: '' })
    if (!sample) {
      const response = await validateReportQuery(client, draft.id, draft.lockVersion)
      if (!response.ok) { setState({ busy: false, error: response.error }); return }
      setValidation(response.data)
    } else {
      const conditions = buildReportConditions(draft.inputSchema, values)
      if (!conditions.ok) { setState({ busy: false, error: conditions.error }); return }
      const response = await previewReportQuery(client, draft.id, draft.lockVersion, conditions.conditions)
      if (!response.ok) { setState({ busy: false, error: response.error }); return }
      setPreview(response.data)
    }
    setState({ busy: false, error: '' })
  }
  return <div className={styles.editor}><p className={styles.notice}>试运行最多读取 100 行、最长 10 秒，仅用于核验样本。正式查询通过已发布版本执行并保存独立结果。</p>{dirty || !draft.id ? <p role="status">请先点击“仅保存草稿”，再核验或试运行当前配置。</p> : null}<div className={styles.form}>{orderedReportInputEntries(draft.inputSchema).map(([code, field]) => <label className={styles.field} key={code}>{field.displayName}{field.required ? ' *' : ''}<input value={typeof values[code] === 'object' ? JSON.stringify(values[code]) : String(values[code] ?? '')} onChange={(event) => { const value = event.currentTarget.value; setValues((previous) => ({ ...previous, [code]: value })) }} /><small>{code} · {field.type}</small></label>)}</div><div className={styles.actions}><button type="button" disabled={dirty || !draft.id || state.busy} onClick={() => void run(false)}>核验查询配置</button><Button variant="primary" disabled={dirty || !draft.id || state.busy} onClick={() => void run(true)}>有界试运行</Button></div>{state.busy ? <FeedbackState kind="loading" title="正在核验查询" /> : null}{state.error ? <p className={styles.error} role="alert">{state.error}</p> : null}{validation ? <section className={styles.section}><h3>{validation.valid ? '查询核验通过' : '查询配置需要修复'}</h3>{[...validation.errors, ...validation.warnings].map((issue, index) => <p key={`${issue.fieldPath}-${index}`} className={styles.error}><code>{issue.fieldPath}</code> · {issue.message}</p>)}</section> : null}{preview ? <section className={styles.section}><h3>样本 {preview.rows.length} 行 · {preview.elapsedMs} ms{preview.sampleOnly ? ' · 非完整结果' : ''}</h3><DataTable scrollLabel="查询试运行样本"><thead><tr>{preview.columns.map((column) => <th key={column.fieldId}>{column.header}</th>)}</tr></thead><tbody>{preview.rows.map((row) => <tr key={row.key}>{preview.columns.map((column) => <td key={column.fieldId}>{display(row.displayValues?.[column.code] ?? row.values[column.code])}</td>)}</tr>)}</tbody></DataTable></section> : null}</div>
}
function display(value: unknown) { return value === null || value === undefined ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value) }
