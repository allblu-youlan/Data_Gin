import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { Database, Download, FileJson, ListChecks, RefreshCcw, Search, Upload } from 'lucide-react'
import type { ClientResponse } from '../api/client'
import { Dialog, Drawer, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, PaginationControls, Section } from '../ui'
import { bojunImportWriteConfirmation, bojunImportWriteFieldOptions, buildExcelExportConfig, cloneExcelEmptyCellFills, cloneExcelImportWriteMappings, cloneExcelMatchSteps, excelMatchSchemePath, selectExcelMatchStepModel, type ExcelMatchFilterConfig, type ExcelEmptyCellFillConfig, type ExcelImportWriteMappingConfig, type ExcelMatchModel, type ExcelMatchStepConfig } from '../excelMatchConfig'
import { bojunMatchFieldOptions, canDownloadExcelJob, defaultExcelExportScheme, defaultExcelImportScheme, excelJobStatusLabel, exportSchemeDefaults, filterSensitiveExcelModels, formValue, importSchemeDefaults, parseExportColumnFormats, readDataField, readObject, type ExcelDialogMode, type ExcelExportSchemeConfig, type ExcelImportSchemeConfig, type ExcelMatchJob, type ExcelMatchPreviewResult, type ExcelMatchScheme, type PendingSchemeSave } from './excelPageSupport'
import { ExcelJobDetailContent, ExcelJobHistoryTable, ExcelMatchPreviewPanel, ExcelSchemeList, Field, Metric, Panel, SelectFilter } from './ExcelMatchPageParts'
import { buildExcelUploadPayload, useExcelUploads, type ExcelPageClient } from './useExcelUploads'
import { useExcelJobs } from './useExcelJobs'
import { ExcelRuleEditor } from './ExcelRuleEditor'
import styles from './ExcelMatchPage.module.css'

export type { ExcelPageClient, ExcelPageClientOptions } from './useExcelUploads'

export function ExcelMatchPage({
  section,
  client,
  token,
  loading,
  refreshVersion,
  setLoading,
  setResult,
  onNavigateToJobs,
}: {
  section: 'jobs' | 'schemes' | 'write'
  client: ExcelPageClient
  token: string
  loading: boolean
  refreshVersion: number
  setLoading: (value: boolean) => void
  setResult: (value: ClientResponse | null) => void
  onNavigateToJobs: () => void
}) {
  const jobs = useExcelJobs({ section, client, token, refreshVersion, setLoading, setResult })
  const [selectedExportFileName, setSelectedExportFileName] = useState('')
  const [selectedImportFileName, setSelectedImportFileName] = useState('')
  const [selectedClearFileName, setSelectedClearFileName] = useState('')
  const [excelDialog, setExcelDialog] = useState<ExcelDialogMode | null>(null)
  const [previewResult, setPreviewResult] = useState<ExcelMatchPreviewResult | null>(null)
  const { uploadProgress, clearUploadRef, ensureExcelUpload, resetUploads } = useExcelUploads(client)
  const [exportSchemes, setExportSchemes] = useState<ExcelMatchScheme[]>([])
  const [importSchemes, setImportSchemes] = useState<ExcelMatchScheme[]>([])
  const [exportDefaults, setExportDefaults] = useState<ExcelExportSchemeConfig>(defaultExcelExportScheme)
  const [exportSteps, setExportSteps] = useState<ExcelMatchStepConfig[]>(cloneExcelMatchSteps(defaultExcelExportScheme.steps))
  const [exportEmptyCellFills, setExportEmptyCellFills] = useState<ExcelEmptyCellFillConfig[]>(cloneExcelEmptyCellFills(defaultExcelExportScheme.emptyCellFills))
  const [excelModels, setExcelModels] = useState<ExcelMatchModel[]>([])
  const [excelModelsLoading, setExcelModelsLoading] = useState(false)
  const [excelModelsError, setExcelModelsError] = useState('')
  const [importDefaults, setImportDefaults] = useState<ExcelImportSchemeConfig>(defaultExcelImportScheme)
  const [importWriteMappings, setImportWriteMappings] = useState<ExcelImportWriteMappingConfig[]>(cloneExcelImportWriteMappings(defaultExcelImportScheme.writeMappings))
  const [expandedStep, setExpandedStep] = useState<number | null>(null)
  const [schemeLibraryOpen, setSchemeLibraryOpen] = useState(false)
  const [exportFormKey, setExportFormKey] = useState(0)
  const [importFormKey, setImportFormKey] = useState(0)
  const [selectedExportSchemeID, setSelectedExportSchemeID] = useState('')
  const [selectedImportSchemeID, setSelectedImportSchemeID] = useState('')
  const [pendingSchemeDelete, setPendingSchemeDelete] = useState<ExcelMatchScheme | null>(null)
  const [pendingSchemeSave, setPendingSchemeSave] = useState<PendingSchemeSave | null>(null)
  const [schemeSaveError, setSchemeSaveError] = useState('')
  const schemeSaveInFlightRef = useRef(false)
  const [deletingSchemeID, setDeletingSchemeID] = useState<number | null>(null)
  const [pendingWrite, setPendingWrite] = useState<{ slot: 'import' | 'clear'; file: File; config: unknown; message: string } | null>(null)
  const [writeMode, setWriteMode] = useState<'import' | 'clear'>('import')
  const [latestWriteJob, setLatestWriteJob] = useState<ExcelMatchJob | null>(null)
  const selectedJobProgress = jobs.job && jobs.job.total_rows > 0
    ? Math.min(100, Math.round(jobs.job.processed_rows / jobs.job.total_rows * 100))
    : 0
  const pendingSchemeNameConflict = pendingSchemeSave
    ? (pendingSchemeSave.operation === 'export_match' ? exportSchemes : importSchemes)
      .find((scheme) => scheme.name === pendingSchemeSave.name.trim()) ?? null
    : null
  const enabledStepCount = exportSteps.filter((step) => step.enabled !== false).length
  const usesExcelModels = exportSteps.some((step) => step.enabled !== false && step.matchMode !== 'conditional_write')
  const requestErrorMessage = (response: ClientResponse, fallback: string) => response.error?.message || fallback

  function resetExcelDialogFiles() {
    setSelectedExportFileName('')
    setSelectedImportFileName('')
    setSelectedClearFileName('')
    setPreviewResult(null)
    resetUploads()
  }

  function closeExcelDialog() {
    setPendingWrite(null)
    setPendingSchemeSave(null)
    setSchemeSaveError('')
    setExcelDialog(null)
    resetExcelDialogFiles()
  }

  function buildExportConfig(form: FormData) {
    return buildExcelExportConfig({
      sheetName: formValue(form, 'sheetName').trim() || 'Sheet1',
      steps: exportSteps,
      emptyCellFills: exportEmptyCellFills,
      exportColumnFormats: parseExportColumnFormats(formValue(form, 'exportColumnFormats')),
      batchSize: Number(formValue(form, 'batchSize') || 1000),
    })
  }

  function updateExportStep(index: number, key: Exclude<keyof ExcelMatchStepConfig, 'filters' | 'enabled'>, value: string) {
    setExportSteps((current) => current.map((step, stepIndex) => stepIndex === index ? { ...step, [key]: value } : step))
  }

  function selectExportStepModel(index: number, tableName: string) {
    setExportSteps((current) => current.map((step, stepIndex) => stepIndex === index
      ? selectExcelMatchStepModel(step, tableName, excelModels)
      : step))
  }

  function updateExportStepFilter(stepIndex: number, filterIndex: number, patch: Partial<ExcelMatchFilterConfig>) {
    setExportSteps((current) => current.map((step, currentStepIndex) => currentStepIndex === stepIndex
      ? {
          ...step,
          filters: step.filters.map((filter, currentFilterIndex) => currentFilterIndex === filterIndex ? { ...filter, ...patch } : filter),
        }
      : step))
  }

  function addExportStepFilter(stepIndex: number) {
    setExportSteps((current) => current.map((step, currentStepIndex) => currentStepIndex === stepIndex
      ? { ...step, filters: [...step.filters, { column: '', op: 'eq', value: '' }] }
      : step))
  }

  function removeExportStepFilter(stepIndex: number, filterIndex: number) {
    setExportSteps((current) => current.map((step, currentStepIndex) => currentStepIndex === stepIndex
      ? { ...step, filters: step.filters.filter((_, currentFilterIndex) => currentFilterIndex !== filterIndex) }
      : step))
  }

  function addExportEmptyCellFill() {
    setExportEmptyCellFills((current) => [...current, { targetColumn: '', sourceColumn: '' }])
  }

  function updateExportEmptyCellFill(index: number, key: keyof ExcelEmptyCellFillConfig, value: string) {
    setExportEmptyCellFills((current) => current.map((fill, fillIndex) => fillIndex === index ? { ...fill, [key]: value } : fill))
  }

  function removeExportEmptyCellFill(index: number) {
    setExportEmptyCellFills((current) => current.filter((_, fillIndex) => fillIndex !== index))
  }

  function addExportStep() {
    setPreviewResult(null)
    setExpandedStep(Math.min(exportSteps.length, 19))
    setExportSteps((current) => {
      if (current.length >= 20) return current
      return [...current, {
        name: `规则 ${current.length + 1}`,
        enabled: true,
        filters: [],
        matchMode: 'field',
        tableName: '',
        matchExcelColumn: current.filter((step) => step.enabled !== false).slice(-1)[0]?.outputColumnName ?? '',
        dbMatchField: '',
        dbValueField: '',
        outputColumnName: '',
        conditionOp: 'contains',
        containsValue: '',
        writeValue: '',
        specExcelColumn: '',
        priceExcelColumn: '',
        qtyExcelColumn: '',
      }]
    })
  }

  function removeExportStep(index: number) {
    setPreviewResult(null)
    setExpandedStep(null)
    setExportSteps((current) => current.length === 1 ? current : current.filter((_, stepIndex) => stepIndex !== index))
  }

  function moveExportStep(index: number, direction: -1 | 1) {
    setPreviewResult(null)
    setExpandedStep(index + direction)
    setExportSteps((current) => {
      const nextIndex = index + direction
      if (nextIndex < 0 || nextIndex >= current.length) return current
      const next = [...current]
      ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
      return next
    })
  }

  function buildImportConfig(form: FormData, confirmWrite: boolean) {
    const writeMappings = importWriteMappings.map((mapping) => ({
      dbWriteField: mapping.dbWriteField.trim(),
      writeExcelColumn: mapping.writeExcelColumn.trim(),
    }))
    const firstMapping = writeMappings[0]
    return {
      operation: 'import_update',
      sheetName: formValue(form, 'sheetName').trim() || 'Sheet1',
      tableName: formValue(form, 'tableName').trim(),
      dbMatchField: formValue(form, 'dbMatchField').trim(),
      matchExcelColumn: formValue(form, 'matchExcelColumn').trim(),
      writeMappings,
      dbWriteField: firstMapping?.dbWriteField ?? '',
      writeExcelColumn: firstMapping?.writeExcelColumn ?? '',
      batchSize: Number(formValue(form, 'batchSize') || 1000),
      dryRun: !confirmWrite,
      confirmWrite,
    }
  }

  function updateImportWriteMapping(index: number, key: keyof ExcelImportWriteMappingConfig, value: string) {
    setImportWriteMappings((current) => current.map((mapping, mappingIndex) => mappingIndex === index ? { ...mapping, [key]: value } : mapping))
  }

  function addImportWriteMapping() {
    setImportWriteMappings((current) => current.length >= bojunImportWriteFieldOptions.length
      ? current
      : [...current, { dbWriteField: '', writeExcelColumn: '' }])
  }

  function removeImportWriteMapping(index: number) {
    setImportWriteMappings((current) => current.length === 1 ? current : current.filter((_, mappingIndex) => mappingIndex !== index))
  }

  const fetchSchemes = useCallback(async (operation: 'export_match' | 'import_update') => {
    const response = await client(`/v1/excel-match-jobs/schemes?operation=${operation}`, { method: 'GET', showResult: false, silentLoading: true })
    if (!response.ok) throw new Error(requestErrorMessage(response, '查询 Excel 方案失败'))
    const value = readDataField(response.data, 'schemes')
    return Array.isArray(value) ? (value as ExcelMatchScheme[]) : []
  }, [client])

  const loadExcelModels = useCallback(async () => {
    setExcelModelsLoading(true)
    setExcelModelsError('')
    try {
      const response = await client('/v1/excel-match-jobs/models', { method: 'GET', showResult: false, silentLoading: true })
      if (!response.ok) throw new Error(requestErrorMessage(response, '查询模型与字段目录失败'))
      const value = readDataField(response.data, 'models')
      setExcelModels(Array.isArray(value) ? filterSensitiveExcelModels(value as ExcelMatchModel[]) : [])
    } catch (error) {
      setExcelModelsError(error instanceof Error ? error.message : '查询模型与字段目录失败')
    } finally {
      setExcelModelsLoading(false)
    }
  }, [client])

  const loadSchemes = useCallback(async () => {
    try {
      const [nextExportSchemes, nextImportSchemes] = await Promise.all([
        fetchSchemes('export_match'),
        fetchSchemes('import_update'),
      ])
      setExportSchemes(nextExportSchemes)
      setImportSchemes(nextImportSchemes)
    } catch (error) {
      setResult({ ok: false, status: 0, data: error instanceof Error ? error.message : String(error) })
    }
  }, [fetchSchemes, setResult])

  useEffect(() => {
    if (!token) return
    if (section === 'schemes' || section === 'write') void loadSchemes()
  }, [loadSchemes, refreshVersion, section, token])

  useEffect(() => {
    if (!token || section !== 'schemes') return
    void loadExcelModels()
  }, [loadExcelModels, section, token])

  useEffect(() => {
    setPendingWrite(null)
    setExcelDialog(null)
    setSelectedExportFileName('')
    setSelectedImportFileName('')
    setSelectedClearFileName('')
    setPreviewResult(null)
    resetUploads()
  }, [resetUploads, section])

  function beginSchemeSave(formElement: HTMLFormElement, operation: 'export_match' | 'import_update', mode: 'current' | 'new') {
    const selectedSchemeID = operation === 'export_match' ? selectedExportSchemeID : selectedImportSchemeID
    const schemes = operation === 'export_match' ? exportSchemes : importSchemes
    const selectedScheme = schemes.find((item) => String(item.id) === selectedSchemeID)
    const form = new FormData(formElement)
    const config = operation === 'export_match'
      ? buildExportConfig(form)
      : buildImportConfig(form, false)

    if (mode === 'current' && selectedScheme?.name) {
      void persistScheme(operation, config, selectedScheme.name)
      return
    }
    setSchemeSaveError('')
    setPendingSchemeSave({ operation, config, name: '', overwriteConfirmed: false })
  }

  async function persistScheme(operation: 'export_match' | 'import_update', config: unknown, name: string) {
    if (schemeSaveInFlightRef.current) return false
    schemeSaveInFlightRef.current = true
    setLoading(true)
    try {
      const nextResult = await client('/v1/excel-match-jobs/schemes', {
        method: 'POST',
        body: { name: name.trim(), operation, config },
        showResult: false,
        silentLoading: true,
        retry: false,
      })
      setResult({ ok: nextResult.ok, status: nextResult.status, data: { message: nextResult.ok ? 'Excel 方案已保存。' : requestErrorMessage(nextResult, '保存 Excel 方案失败。') }, error: nextResult.error })
      if (nextResult.ok) {
        const savedScheme = readObject<ExcelMatchScheme>(nextResult, 'scheme')
        if (savedScheme?.id) {
          if (operation === 'export_match') {
            setSelectedExportSchemeID(String(savedScheme.id))
          } else {
            setSelectedImportSchemeID(String(savedScheme.id))
          }
        }
        await loadSchemes()
      }
      return nextResult.ok
    } catch {
      setResult({ ok: false, status: 0, data: { message: '保存 Excel 方案失败，请稍后重试。' } })
      return false
    } finally {
      schemeSaveInFlightRef.current = false
      setLoading(false)
    }
  }

  async function confirmPendingSchemeSave() {
    if (!pendingSchemeSave || loading) return
    const name = pendingSchemeSave.name.trim()
    if (name.length < 1 || name.length > 100) {
      setSchemeSaveError('方案名称应为 1 至 100 个字符。')
      return
    }
    if (pendingSchemeNameConflict && !pendingSchemeSave.overwriteConfirmed) {
      setSchemeSaveError('存在同类型同名方案；请确认覆盖后再保存。')
      return
    }
    const saved = await persistScheme(pendingSchemeSave.operation, pendingSchemeSave.config, name)
    if (saved) {
      setPendingSchemeSave(null)
      setSchemeSaveError('')
    }
  }

  async function deleteScheme(scheme: ExcelMatchScheme) {
    setDeletingSchemeID(scheme.id)
    try {
      const nextResult = await client(excelMatchSchemePath(scheme.id), {
        method: 'DELETE',
        showResult: false,
        silentLoading: true,
        retry: false,
      })
      setResult({ ok: nextResult.ok, status: nextResult.status, data: { message: nextResult.ok ? 'Excel 方案已删除。' : requestErrorMessage(nextResult, '删除 Excel 方案失败。') }, error: nextResult.error })
      if (!nextResult.ok) return

      if (scheme.operation === 'export_match' && selectedExportSchemeID === String(scheme.id)) {
        applyExportScheme('')
      }
      if (scheme.operation === 'import_update' && selectedImportSchemeID === String(scheme.id)) {
        applyImportScheme('')
      }
      await loadSchemes()
      setPendingSchemeDelete(null)
    } catch {
      setResult({ ok: false, status: 0, data: { message: '删除 Excel 方案失败，请稍后重试。' } })
    } finally {
      setDeletingSchemeID(null)
    }
  }

  function applyExportScheme(schemeID: string) {
    setExpandedStep(null)
    setSchemeLibraryOpen(false)
    setSelectedExportSchemeID(schemeID)
    if (!schemeID) {
      setExportDefaults(defaultExcelExportScheme)
      setExportSteps(cloneExcelMatchSteps(defaultExcelExportScheme.steps))
      setExportEmptyCellFills(cloneExcelEmptyCellFills(defaultExcelExportScheme.emptyCellFills))
      setExportFormKey((value) => value + 1)
      setPreviewResult(null)
      setSelectedExportFileName('')
      clearUploadRef('export')
      return
    }
    const scheme = exportSchemes.find((item) => String(item.id) === schemeID)
    if (!scheme) return
    const defaults = exportSchemeDefaults(scheme.config)
    setExportDefaults(defaults)
    setExportSteps(cloneExcelMatchSteps(defaults.steps))
    setExportEmptyCellFills(cloneExcelEmptyCellFills(defaults.emptyCellFills))
    setExportFormKey((value) => value + 1)
    setPreviewResult(null)
    setSelectedExportFileName('')
    clearUploadRef('export')
  }

  function applyImportScheme(schemeID: string) {
    setSelectedImportSchemeID(schemeID)
    if (!schemeID) {
      setImportDefaults(defaultExcelImportScheme)
      setImportWriteMappings(cloneExcelImportWriteMappings(defaultExcelImportScheme.writeMappings))
      setImportFormKey((value) => value + 1)
      setSelectedImportFileName('')
      clearUploadRef('import')
      return
    }
    const scheme = importSchemes.find((item) => String(item.id) === schemeID)
    if (!scheme) return
    const defaults = importSchemeDefaults(scheme.config)
    setImportDefaults(defaults)
    setImportWriteMappings(cloneExcelImportWriteMappings(defaults.writeMappings))
    setImportFormKey((value) => value + 1)
    setSelectedImportFileName('')
    clearUploadRef('import')
  }

  async function createExportJob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!validateExportForm(event.currentTarget, true)) return
    const form = new FormData(event.currentTarget)
    const file = form.get('file')
    if (!(file instanceof File) || file.size === 0) {
      setResult({ ok: false, status: 0, data: '请选择 .xlsx 文件' })
      return
    }

    setLoading(true)
    try {
      const uploadId = await ensureExcelUpload('export', file)
      const payload = buildExcelUploadPayload(uploadId, buildExportConfig(form))
      const nextResult = await client('/v1/excel-match-jobs', {
        method: 'POST',
        body: payload,
        showResult: false,
        silentLoading: true,
        retry: false,
        timeoutMs: 120_000,
      })
      if (nextResult.ok) {
        showCreatedJob(nextResult)
      } else {
        setResult(nextResult)
      }
    } catch {
      setResult({ ok: false, status: 0, data: { message: '创建 Excel 导出任务失败，请稍后重试。' } })
    } finally {
      setLoading(false)
    }
  }

  async function previewExportJob(formElement: HTMLFormElement) {
    if (!validateExportForm(formElement, true)) return
    const form = new FormData(formElement)
    const file = form.get('file')
    if (!(file instanceof File) || file.size === 0) {
      setResult({ ok: false, status: 0, data: '请选择 .xlsx 文件' })
      return
    }

    setLoading(true)
    try {
      const uploadId = await ensureExcelUpload('export', file)
      const payload = buildExcelUploadPayload(uploadId, buildExportConfig(form))
      const nextResult = await client('/v1/excel-match-jobs/preview', {
        method: 'POST',
        body: payload,
        showResult: false,
        silentLoading: true,
        retry: false,
        timeoutMs: 120_000,
      })
      setResult({ ok: nextResult.ok, status: nextResult.status, data: { message: nextResult.ok ? 'Excel 匹配预览已更新。' : requestErrorMessage(nextResult, '预览 Excel 匹配失败。') }, error: nextResult.error })
      if (nextResult.ok) {
        setPreviewResult(readObject<ExcelMatchPreviewResult>(nextResult, 'preview'))
      }
    } catch {
      setResult({ ok: false, status: 0, data: { message: '预览 Excel 匹配失败，请稍后重试。' } })
    } finally {
      setLoading(false)
    }
  }

  async function createExcelWriteJob(slot: 'import' | 'clear', file: File, config: unknown) {
    setLoading(true)
    try {
      const uploadId = await ensureExcelUpload(slot, file)
      const nextResult = await client('/v1/excel-match-jobs', {
        method: 'POST',
        body: buildExcelUploadPayload(uploadId, config),
        showResult: false,
        silentLoading: true,
        retry: false,
        timeoutMs: 120_000,
      })
      if (nextResult.ok) showCreatedWriteJob(nextResult)
      else setResult(nextResult)
    } catch {
      setResult({ ok: false, status: 0, data: { message: '创建 Excel 写入任务失败，请稍后重试。' } })
    } finally {
      setLoading(false)
    }
  }

  async function confirmPendingWrite() {
    if (!pendingWrite || loading) return
    await createExcelWriteJob(pendingWrite.slot, pendingWrite.file, pendingWrite.config)
    setPendingWrite(null)
  }

  async function createImportJob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const file = form.get('file')
    if (!(file instanceof File) || file.size === 0) {
      setResult({ ok: false, status: 0, data: '请选择 .xlsx 文件' })
      return
    }

    const confirmWrite = form.get('confirmWrite') === 'on'
    const confirmMessage = bojunImportWriteConfirmation(importWriteMappings.map((mapping) => mapping.dbWriteField.trim()))
    const config = buildImportConfig(form, confirmWrite)
    if (confirmWrite) {
      setPendingWrite({ slot: 'import', file, config, message: confirmMessage })
      return
    }
    await createExcelWriteJob('import', file, config)
  }

  async function createClearMatchedJob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const file = form.get('file')
    if (!(file instanceof File) || file.size === 0) {
      setResult({ ok: false, status: 0, data: '请选择 .xlsx 文件' })
      return
    }

    const confirmWrite = form.get('confirmWrite') === 'on'
    const config = {
      operation: 'clear_matched_docno',
      sheetName: formValue(form, 'sheetName').trim() || 'Sheet1',
      tableName: formValue(form, 'tableName').trim(),
      dbMatchField: formValue(form, 'dbMatchField').trim(),
      matchExcelColumn: formValue(form, 'matchExcelColumn').trim(),
      dbWriteField: 'matched_docno',
      batchSize: Number(formValue(form, 'batchSize') || 1000),
      dryRun: !confirmWrite,
      confirmWrite,
    }
    if (confirmWrite) {
      setPendingWrite({ slot: 'clear', file, config, message: '确认清空命中行的 matched_docno？该操作会将这些订单退回未匹配状态。' })
      return
    }
    await createExcelWriteJob('clear', file, config)
  }

  function showCreatedJob(result: ClientResponse) {
    jobs.registerCreatedJob(result, true)
    setResult(null)
    closeExcelDialog()
    onNavigateToJobs()
  }

  function showCreatedWriteJob(result: ClientResponse) {
    const createdJob = jobs.registerCreatedJob(result, false)
    if (createdJob) setLatestWriteJob(createdJob)
    setResult(null)
  }

  async function refreshJob() {
    const id = Number(jobs.jobID)
    if (!id) {
      setResult({ ok: false, status: 0, data: '请输入任务 ID' })
      return
    }
    const nextJob = await jobs.refreshJobByID(id)
    if (nextJob) {
      closeExcelDialog()
      jobs.setJobDetailOpen(true)
    }
  }

  function renderPendingSchemeSave() {
    if (!pendingSchemeSave) return null
    return (
      <form className={styles.viewStack} onSubmit={(event) => { event.preventDefault(); void confirmPendingSchemeSave() }}>
        <p>将保存当前表单的配置快照；取消后可继续编辑，已填写内容不会丢失。</p>
        <label>
          方案名称
          <input
            value={pendingSchemeSave.name}
            maxLength={100}
            data-autofocus
            onChange={(event) => {
              const name = event.currentTarget.value
              setPendingSchemeSave((current) => current ? { ...current, name, overwriteConfirmed: false } : current)
              setSchemeSaveError('')
            }}
          />
        </label>
        {pendingSchemeNameConflict && (
          <label className={styles.checkboxLabel}>
            <input
              type="checkbox"
              checked={pendingSchemeSave.overwriteConfirmed}
              onChange={(event) => {
                const overwriteConfirmed = event.currentTarget.checked
                setPendingSchemeSave((current) => current ? { ...current, overwriteConfirmed } : current)
              }}
            />
            覆盖同类型的“{pendingSchemeNameConflict.name}”方案
          </label>
        )}
        {schemeSaveError && <p className={styles.errorBanner} role="alert">{schemeSaveError}</p>}
        <div className={styles.formActions}>
          <button type="button" disabled={loading} onClick={() => { setPendingSchemeSave(null); setSchemeSaveError('') }}>返回编辑</button>
          <button className={styles.primary} type="submit" disabled={loading}>{loading ? '保存中…' : '保存方案'}</button>
        </div>
      </form>
    )
  }

  function validateExportForm(form: HTMLFormElement, forExecution = false) {
    if (forExecution && enabledStepCount === 0) {
      setResult({ ok: false, status: 0, data: '请至少启用一条匹配规则后再预览或导出。' })
      return false
    }
    const invalid = form.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('input:invalid, select:invalid, textarea:invalid')
    if (!invalid) return true
    const fieldset = invalid.closest<HTMLFieldSetElement>('[data-step-index]')
    if (fieldset) setExpandedStep(Number(fieldset.dataset.stepIndex))
    const details = invalid.closest('details')
    if (details) details.open = true
    requestAnimationFrame(() => invalid.reportValidity())
    return false
  }

  function renderExportMatchForm() {
    return (
      <form className={`${styles.uploadForm} ${styles.schemeForm}`} onSubmit={createExportJob} noValidate onChange={() => setPreviewResult(null)} key={exportFormKey}>
        <div className={styles.schemeToolbar}><label className={styles.fileInputLabel}>Excel 文件<input name="file" type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(event) => { setSelectedExportFileName(event.currentTarget.files?.[0]?.name ?? ''); clearUploadRef('export'); setPreviewResult(null) }} /><span>{selectedExportFileName || '请选择 Excel 文件'}</span></label><Field label="Sheet" name="sheetName" defaultValue={exportDefaults.sheetName} /><div className={styles.schemeToolbarActions}><button type="button" onClick={(event) => { const form = event.currentTarget.form; if (form && validateExportForm(form)) beginSchemeSave(form, 'export_match', selectedExportSchemeID ? 'current' : 'new') }} disabled={loading}>保存方案</button><button type="button" onClick={(event) => { const form = event.currentTarget.form; if (form && validateExportForm(form)) void previewExportJob(form) }} disabled={loading}><FileJson aria-hidden="true" />预览匹配</button><button className={styles.primary} type="submit" disabled={loading}><Upload aria-hidden="true" />创建导出任务</button></div></div>
        <div className={styles.stepEditor}>
          <div className={styles.stepEditorTitle}><div><strong>匹配规则 <span className={styles.ruleCount}>{enabledStepCount} / {exportSteps.length} 已启用</span></strong><span>按顺序执行，点击规则展开详情；停用后保留配置。</span></div><button type="button" onClick={addExportStep} disabled={exportSteps.length >= 20}>添加规则</button></div>
          {enabledStepCount === 0 && <p className={styles.modeNote} role="status">当前没有启用的规则。可保存方案，预览和导出前请至少启用一条规则。</p>}
          {usesExcelModels && excelModelsLoading && <p className={styles.modeNote}>正在加载模型与字段目录…</p>}
          {usesExcelModels && excelModelsError && <div className={styles.catalogError} role="alert"><span>模型与字段目录加载失败：{excelModelsError}</span><button type="button" onClick={() => void loadExcelModels()}>重试加载</button></div>}
          {usesExcelModels && !excelModelsLoading && !excelModelsError && excelModels.length === 0 && <p className={styles.modeNote}>当前数据库没有返回可选择的模型；历史配置仍可查看，但新步骤需要先确认数据库连接和模型表。</p>}
          {exportSteps.map((step, index) => (
            <ExcelRuleEditor
              key={`${exportFormKey}-${index}`}
              step={step} index={index} count={exportSteps.length} models={excelModels}
              expanded={expandedStep === index}
              onExpand={() => setExpandedStep((current) => current === index ? null : index)}
              onToggle={() => { setPreviewResult(null); setExportSteps((current) => current.map((item, i) => i === index ? { ...item, enabled: item.enabled === false } : item)) }}
              onMove={(direction) => moveExportStep(index, direction)} onRemove={() => removeExportStep(index)}
              onChange={(key, value) => updateExportStep(index, key, value)} onModelChange={(value) => selectExportStepModel(index, value)}
              onFilterChange={(filterIndex, patch) => updateExportStepFilter(index, filterIndex, patch)}
              onFilterAdd={() => { setPreviewResult(null); addExportStepFilter(index) }} onFilterRemove={(filterIndex) => { setPreviewResult(null); removeExportStepFilter(index, filterIndex) }}
            />
          ))}
          <details className={styles.supplementalSettings}>
            <summary>空值填充 <span>{exportEmptyCellFills.length} 条规则 · 匹配完成后执行</span></summary>
          <div className={styles.stepFilterEditor}>
            <div className={styles.stepFilterHeading}>
              <div><strong>空值填充</strong><span>所有匹配步骤完成后，目标列为空时从同一行来源列填充；可引用原始列或追加输出列，不覆盖已有值。</span></div>
              <button type="button" onClick={() => { setPreviewResult(null); addExportEmptyCellFill() }}>添加填充规则</button>
            </div>
            {exportEmptyCellFills.length === 0 && <p className={styles.stepFilterEmpty}>未设置空值填充规则。</p>}
            {exportEmptyCellFills.map((fill, index) => <div className={styles.stepFilterRow} key={`${exportFormKey}-empty-cell-fill-${index}`}>
              <Field label="目标列（为空时）" name={`empty_cell_fill_target_${index}`} value={fill.targetColumn} onChange={(value) => updateExportEmptyCellFill(index, 'targetColumn', value)} required />
              <Field label="来源列（同一行）" name={`empty_cell_fill_source_${index}`} value={fill.sourceColumn} onChange={(value) => updateExportEmptyCellFill(index, 'sourceColumn', value)} required />
              <button type="button" onClick={() => { setPreviewResult(null); removeExportEmptyCellFill(index) }} aria-label={`删除空值填充规则 ${index + 1}`}>删除规则</button>
            </div>)}
          </div>
          </details>
        </div>
        <details className={styles.schemeAdvanced}><summary>方案与高级设置</summary><div><label>已保存方案<select name="exportSchemeID" value={selectedExportSchemeID} onChange={(event) => applyExportScheme(event.currentTarget.value)}><option value="">选择方案</option>{exportSchemes.map((scheme) => <option value={scheme.id} key={scheme.id}>{scheme.name}</option>)}</select></label><button type="button" disabled={!selectedExportSchemeID || loading} onClick={(event) => { const form = event.currentTarget.form; if (form && validateExportForm(form)) beginSchemeSave(form, 'export_match', 'current') }}>保存到当前方案</button><button type="button" disabled={loading} onClick={(event) => { const form = event.currentTarget.form; if (form && validateExportForm(form)) beginSchemeSave(form, 'export_match', 'new') }}>另存为新方案</button><label>导出列内容格式<textarea name="exportColumnFormats" defaultValue={exportDefaults.exportColumnFormats} rows={4} placeholder={'每行一个：列名=格式\n例如：金额=number\n下单时间=date'} /><small>支持 text、number、integer、bool、date。</small></label><Field label="批量查询大小" name="batchSize" defaultValue={exportDefaults.batchSize} /></div></details>
        {previewResult && <ExcelMatchPreviewPanel preview={previewResult} />}
        {uploadProgress && <p className={styles.modeNote} role="status" aria-live="polite">{uploadProgress}</p>}
      </form>
    )
  }

  function renderImportWriteForm() {
    return <form className={styles.uploadForm} onSubmit={createImportJob} key={importFormKey}>
      <label>已保存方案<select name="importSchemeID" value={selectedImportSchemeID} onChange={(event) => applyImportScheme(event.currentTarget.value)}><option value="">选择方案</option>{importSchemes.map((scheme) => <option value={scheme.id} key={scheme.id}>{scheme.name}</option>)}</select></label>
      <button type="button" disabled={!selectedImportSchemeID || loading} onClick={(event) => { const form = event.currentTarget.form; if (form?.reportValidity()) beginSchemeSave(form, 'import_update', 'current') }}>保存到当前方案</button><button type="button" disabled={loading} onClick={(event) => { const form = event.currentTarget.form; if (form?.reportValidity()) beginSchemeSave(form, 'import_update', 'new') }}>另存为新方案</button>
      <label className={styles.fileInputLabel}>Excel 文件<input name="file" type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(event) => { setSelectedImportFileName(event.currentTarget.files?.[0]?.name ?? ''); clearUploadRef('import') }} /><span>{selectedImportFileName || '请选择需要导入更新的 .xlsx 文件'}</span></label>
      <Field label="Sheet 页名称" name="sheetName" defaultValue={importDefaults.sheetName} /><label>匹配表名<select name="tableName" defaultValue={importDefaults.tableName}><option value="bojun_retail_orders">伯俊零售单 bojun_retail_orders</option></select></label><label>数据库匹配字段<select name="dbMatchField" defaultValue={importDefaults.dbMatchField}>{bojunMatchFieldOptions.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label><Field label="Excel 匹配列名" name="matchExcelColumn" defaultValue={importDefaults.matchExcelColumn} />{renderImportWriteMappings()}<Field label="批量更新大小" name="batchSize" defaultValue={importDefaults.batchSize} />
      <label className={styles.checkboxLabel}><input name="confirmWrite" type="checkbox" />确认写入数据库</label><p className={styles.modeNote}>不勾选时只预检匹配数量，不写库；每个目标列独立判断是否为空，金额字段仅填当前为 0 的记录；Oracle 字段必须用 docno 匹配。</p>{uploadProgress && <p className={styles.modeNote} role="status" aria-live="polite">{uploadProgress}</p>}<div className={styles.formActions}><button className={styles.primary} type="submit" disabled={loading}><Upload aria-hidden="true" />创建预检/导入任务</button></div>
    </form>
  }

  function renderImportWriteMappings() {
    const selectedFields = new Set(importWriteMappings.map((mapping) => mapping.dbWriteField))
    return <div className={styles.stepFilterEditor}>
      <div className={styles.stepFilterHeading}>
        <div><strong>写入列映射</strong><span>一行可同时写入多个符合条件的目标列，每列都不会覆盖已有值。</span></div>
        <button type="button" onClick={addImportWriteMapping} disabled={importWriteMappings.length >= bojunImportWriteFieldOptions.length}>添加写入列</button>
      </div>
      {importWriteMappings.map((mapping, index) => <div className={`${styles.stepFilterRow} ${styles.importWriteRow}`} key={`${importFormKey}-import-write-${index}`}>
        <label>写入字段 {index + 1}<select name={`dbWriteField_${index}`} value={mapping.dbWriteField} onChange={(event) => updateImportWriteMapping(index, 'dbWriteField', event.currentTarget.value)} required><option value="">选择字段</option>{bojunImportWriteFieldOptions.map((option) => <option value={option.value} key={option.value} disabled={option.value !== mapping.dbWriteField && selectedFields.has(option.value)}>{option.label}</option>)}</select></label>
        <Field label="Excel 写入值列名" name={`writeExcelColumn_${index}`} value={mapping.writeExcelColumn} onChange={(value) => updateImportWriteMapping(index, 'writeExcelColumn', value)} required />
        <button type="button" onClick={() => removeImportWriteMapping(index)} disabled={importWriteMappings.length === 1} aria-label={`删除写入列 ${index + 1}`}>删除</button>
      </div>)}
    </div>
  }

  function renderClearWriteForm() {
    return <form className={styles.uploadForm} onSubmit={createClearMatchedJob}>
      <label className={styles.fileInputLabel}>Excel 文件<input name="file" type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(event) => { setSelectedClearFileName(event.currentTarget.files?.[0]?.name ?? ''); clearUploadRef('clear') }} /><span>{selectedClearFileName || '请选择需要退回的 .xlsx 文件'}</span></label>
      <Field label="Sheet 页名称" name="sheetName" defaultValue="Sheet1" /><label>匹配表名<select name="tableName" defaultValue="bojun_retail_orders"><option value="bojun_retail_orders">伯俊零售单 bojun_retail_orders</option></select></label><label>数据库匹配字段<select name="dbMatchField" defaultValue="docno">{bojunMatchFieldOptions.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label><Field label="Excel 匹配列名" name="matchExcelColumn" defaultValue="外部订单编号" /><Field label="批量处理大小" name="batchSize" defaultValue="1000" />
      <label className={styles.checkboxLabel}><input name="confirmWrite" type="checkbox" />确认清空 matched_docno</label><p className={styles.modeNote}>不勾选时只预检会命中的行；勾选后会把命中行的 matched_docno 清空，用于退回未匹配状态。</p>{uploadProgress && <p className={styles.modeNote} role="status" aria-live="polite">{uploadProgress}</p>}<div className={styles.formActions}><button type="submit" disabled={loading}><RefreshCcw aria-hidden="true" />创建预检/退回任务</button></div>
    </form>
  }

  return (
    <PageCanvas className={styles.page} density="compact">
      <PageHeader
        eyebrow="EXCEL WORKSPACE"
        title={section === 'jobs' ? 'Excel 任务' : section === 'schemes' ? 'Excel 多步骤匹配' : 'Excel 写入'}
        description={section === 'jobs' ? '查询任务状态、进度、日志和下载结果。' : section === 'schemes' ? '配置数据库模型、字段和顺序匹配步骤。' : '执行导入更新与退回未匹配操作。'}
      />
      {section === 'jobs' && <>
        <MetricStrip items={[{ key: 'history', label: '历史任务', value: jobs.jobHistoryPagination?.total ?? jobs.jobHistory.length }, { key: 'current', label: '当前任务', value: jobs.job ? `#${jobs.job.id}` : '-' }, { key: 'status', label: '任务状态', value: jobs.job ? excelJobStatusLabel(jobs.job.status) : '-' }, { key: 'processed', label: '已处理行', value: jobs.job ? `${jobs.job.processed_rows.toLocaleString('en-US')} / ${jobs.job.total_rows.toLocaleString('en-US')}` : '-' }, { key: 'tracking', label: '自动跟踪', value: jobs.trackingJobID ? `#${jobs.trackingJobID}` : '-' }]} />
        <form onSubmit={(event) => {
          event.preventDefault()
          jobs.setJobHistoryPage(1)
          jobs.setAppliedJobHistoryFilters({ keyword: jobs.jobQuery, status: jobs.jobStatus === 'all' ? '' : jobs.jobStatus, operation: jobs.jobOperation === 'all' ? '' : jobs.jobOperation })
          jobs.setJobHistoryReloadVersion((version) => version + 1)
        }}>
          <FilterToolbar>
            <label className={styles.search}><span className={styles.visuallyHidden}>任务 ID、文件名或错误</span><span><Search aria-hidden="true" /><input name="excel_job_query" type="search" value={jobs.jobQuery} placeholder="任务 ID / 文件名 / 错误" onChange={(event) => jobs.setJobQuery(event.currentTarget.value)} /></span></label>
            <SelectFilter label="状态" value={jobs.jobStatus} onChange={(value) => { jobs.setJobStatus(value); jobs.setJobHistoryPage(1); jobs.setAppliedJobHistoryFilters({ keyword: jobs.jobQuery, status: value === 'all' ? '' : value, operation: jobs.jobOperation === 'all' ? '' : jobs.jobOperation }) }} options={[{ value: 'pending', label: '等待处理' }, { value: 'running', label: '处理中' }, { value: 'success', label: '成功' }, { value: 'failed', label: '失败' }, { value: 'expired', label: '已过期' }]} />
            <SelectFilter label="操作" value={jobs.jobOperation} onChange={(value) => { jobs.setJobOperation(value); jobs.setJobHistoryPage(1); jobs.setAppliedJobHistoryFilters({ keyword: jobs.jobQuery, status: jobs.jobStatus === 'all' ? '' : jobs.jobStatus, operation: value === 'all' ? '' : value }) }} options={[{ value: 'match', label: '匹配任务' }, { value: 'write', label: '导入任务' }, { value: 'export', label: 'Oracle 导出' }]} />
            <button type="submit" disabled={jobs.jobHistoryLoading}>{jobs.jobHistoryLoading ? '查询中…' : '查询'}</button>
          </FilterToolbar>
        </form>
        {jobs.jobHistoryError && <FeedbackState kind="error" title="任务列表加载提示" description={`${jobs.jobHistoryError}${jobs.jobHistoryPagination && !jobs.jobHistoryError.includes('兼容数据') ? ' 已保留最近一次成功数据。' : ''}`} action={<button type="button" onClick={() => jobs.setJobHistoryReloadVersion((version) => version + 1)} disabled={jobs.jobHistoryLoading}>重试</button>} />}
        <Section title="Excel 任务" description={jobs.jobHistoryLoading && !jobs.jobHistoryPagination ? '正在加载…' : `共 ${jobs.jobHistoryPagination?.total ?? 0} 条`} flush>
              <ExcelJobHistoryTable
                jobs={jobs.jobHistory}
                loading={jobs.jobHistoryLoading}
                downloadingJobID={jobs.downloadingJobID}
                selectedJobID={jobs.job?.id ?? null}
                onDownload={(id) => void jobs.downloadJob(id)}
                onView={(id, trigger) => void jobs.openJobDetail(id, trigger)}
              />
              <PaginationControls page={jobs.jobHistoryPagination?.page ?? jobs.jobHistoryPage} totalPages={jobs.jobHistoryPagination?.totalPages ?? 0} loading={jobs.jobHistoryLoading} onPrevious={() => jobs.setJobHistoryPage((page) => Math.max(1, page - 1))} onNext={() => jobs.setJobHistoryPage((page) => page + 1)} />
        </Section>
        {jobs.jobDetailOpen && jobs.job && <Drawer open size="medium" title={`任务 #${jobs.job.id}`} description="任务进度、有效期和实时日志" returnFocus={jobs.jobDetailTriggerRef.current} onClose={() => jobs.setJobDetailOpen(false)}><ExcelJobDetailContent job={jobs.job} logs={jobs.jobLogs} progress={selectedJobProgress} autoRefreshText={jobs.autoRefreshText} loading={loading} downloading={jobs.downloadingJobID === jobs.job.id} onRefresh={() => void jobs.refreshJobByID(jobs.job!.id)} onDownload={() => void jobs.downloadJob(jobs.job!.id)} /></Drawer>}
      </>}

      {section === 'schemes' && <>
        <section className={styles.matchWorkspace} aria-label="Excel 匹配配置">
          <div className={styles.workspaceHeading}>
            <div><strong>{exportSchemes.find((scheme) => String(scheme.id) === selectedExportSchemeID)?.name || '新建匹配方案'}</strong><span>最多 20 条规则 · 仅启用规则生效</span></div>
            <button type="button" onClick={() => setSchemeLibraryOpen(true)}><ListChecks aria-hidden="true" />方案库 <span>{exportSchemes.length}</span></button>
          </div>
          {renderExportMatchForm()}
        </section>
        <Drawer open={schemeLibraryOpen} size="wide" title="已保存导出方案" description="打开方案继续编辑；启停状态随方案保存。" onClose={() => setSchemeLibraryOpen(false)}>
          <ExcelSchemeList schemes={exportSchemes} deletingSchemeID={deletingSchemeID} onDelete={setPendingSchemeDelete} onOpen={(id) => applyExportScheme(String(id))} />
        </Drawer>
      </>}

      {section === 'write' && <>
        <MetricStrip items={[{ key: 'schemes', label: '导入方案', value: importSchemes.length }, { key: 'default', label: '默认模式', value: '只预检' }, { key: 'write', label: '写入保护', value: '不覆盖' }, { key: 'clear', label: '清空保护', value: '需确认' }]} />
        <section className={styles.masterDetail}>
          <Panel title="数据库回写" icon={<Database />} meta="默认只预检；写入与退回均需二次确认">
            <div className={styles.modeSwitch} role="group" aria-label="数据库回写模式"><button type="button" className={writeMode === 'import' ? styles.active : undefined} onClick={() => setWriteMode('import')}><Database aria-hidden="true" />匹配导入</button><button type="button" className={writeMode === 'clear' ? `${styles.active} ${styles.danger}` : styles.danger} onClick={() => setWriteMode('clear')}><RefreshCcw aria-hidden="true" />退回未匹配</button></div>
            <p className={styles.modeNote}>{writeMode === 'import' ? '先预检，确认后只填充空字段。' : '先预检，确认后清空命中行的 matched_docno。'}</p>
            {writeMode === 'import' ? renderImportWriteForm() : renderClearWriteForm()}
            {latestWriteJob && <section className={styles.writeSummary} aria-labelledby="excel-write-summary-title"><div><strong id="excel-write-summary-title">最近预检/写入任务</strong><span>以下数据来自服务端安全任务摘要；不覆盖与错误计数未返回。</span></div><div className={styles.jobDetail}><Metric label="任务 ID" value={latestWriteJob.id} /><Metric label="状态" value={excelJobStatusLabel(latestWriteJob.status)} /><Metric label="总行数" value={latestWriteJob.total_rows} /><Metric label="已处理" value={latestWriteJob.processed_rows} /><Metric label="预计命中" value={latestWriteJob.matched_rows} /><Metric label="未匹配" value={latestWriteJob.unmatched_rows} /></div><div className={styles.detailActions}><span>不覆盖/错误：服务端未返回独立计数。</span><button type="button" onClick={onNavigateToJobs}>查看任务详情</button></div></section>}
          </Panel>
          <aside className={styles.aside} aria-label="已保存导入方案"><Panel title="已保存导入方案" icon={<ListChecks />} meta={`${importSchemes.length} 个方案`}><ExcelSchemeList schemes={importSchemes} deletingSchemeID={deletingSchemeID} onDelete={setPendingSchemeDelete} onOpen={(id) => { setWriteMode('import'); applyImportScheme(String(id)) }} /></Panel></aside>
        </section>
      </>}

      {pendingSchemeSave && excelDialog === null && <Dialog className={styles.dialog} open title="保存 Excel 匹配方案" closeDisabled={loading || schemeSaveInFlightRef.current} onClose={() => { if (!loading && !schemeSaveInFlightRef.current) { setPendingSchemeSave(null); setSchemeSaveError('') } }}>{renderPendingSchemeSave()}</Dialog>}

      {pendingWrite && excelDialog === null && <Dialog className={styles.dialog} open title={pendingWrite.slot === 'import' ? '确认写入数据库' : '确认退回未匹配'} closeDisabled={loading} onClose={() => { if (!loading) setPendingWrite(null) }}><div className={styles.viewStack}><p>{pendingWrite.message}</p><div className={styles.formActions}><button type="button" disabled={loading} onClick={() => setPendingWrite(null)}>返回修改</button><button className={pendingWrite.slot === 'clear' ? styles.danger : styles.primary} type="button" disabled={loading} onClick={() => void confirmPendingWrite()}>{loading ? '创建任务中…' : pendingWrite.slot === 'clear' ? '确认退回' : '确认写入'}</button></div></div></Dialog>}

      {excelDialog === 'export' && (
        <Dialog className={`${styles.dialog} ${styles.wideDialog}`} open title={pendingSchemeSave ? '保存 Excel 匹配方案' : '匹配导出配置'} closeDisabled={loading || schemeSaveInFlightRef.current} onClose={() => { if (!loading && !schemeSaveInFlightRef.current) closeExcelDialog() }}>
          {renderPendingSchemeSave()}
          <div hidden={pendingSchemeSave !== null}>{renderExportMatchForm()}</div>
        </Dialog>
      )}

      {excelDialog === 'import' && (
        <Dialog className={`${styles.dialog} ${styles.mediumDialog}`} open title={pendingSchemeSave ? '保存 Excel 匹配方案' : pendingWrite?.slot === 'import' ? '确认写入数据库' : '匹配导入参数'} closeDisabled={loading || schemeSaveInFlightRef.current} onClose={() => { if (!loading && !schemeSaveInFlightRef.current) { setPendingWrite(null); closeExcelDialog() } }}>
          {pendingWrite?.slot === 'import' && <div className={styles.viewStack}><p>{pendingWrite.message}</p><div className={styles.formActions}><button type="button" disabled={loading} onClick={() => setPendingWrite(null)}>返回修改</button><button className={styles.primary} type="button" disabled={loading} onClick={() => void confirmPendingWrite()}>{loading ? '创建任务中…' : '确认写入'}</button></div></div>}
          {renderPendingSchemeSave()}
          <form className={styles.uploadForm} onSubmit={createImportJob} key={importFormKey} hidden={pendingWrite?.slot === 'import' || pendingSchemeSave !== null}>
            <label>
              已保存方案
              <select name="importSchemeID" value={selectedImportSchemeID} onChange={(event) => applyImportScheme(event.currentTarget.value)}>
                <option value="">选择方案</option>
                {importSchemes.map((scheme) => <option value={scheme.id} key={scheme.id}>{scheme.name}</option>)}
              </select>
            </label>
            <button
              type="button"
              disabled={!selectedImportSchemeID || loading}
              onClick={(event) => {
                const form = event.currentTarget.form
                if (form?.reportValidity()) beginSchemeSave(form, 'import_update', 'current')
              }}
            >
              保存到当前方案
            </button>
            <button
              type="button"
              disabled={loading}
              onClick={(event) => {
                const form = event.currentTarget.form
                if (form?.reportValidity()) beginSchemeSave(form, 'import_update', 'new')
              }}
            >
              另存为新方案
            </button>
            <label className={styles.fileInputLabel}>
              Excel 文件
              <input
                name="file"
                type="file"
                accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                onChange={(event) => {
                  setSelectedImportFileName(event.currentTarget.files?.[0]?.name ?? '')
                  clearUploadRef('import')
                }}
              />
              <span>{selectedImportFileName || '请选择需要导入更新的 .xlsx 文件'}</span>
            </label>
            <Field label="Sheet 页名称" name="sheetName" defaultValue={importDefaults.sheetName} />
            <label>
              匹配表名
              <select name="tableName" defaultValue={importDefaults.tableName}>
                <option value="bojun_retail_orders">伯俊零售单 bojun_retail_orders</option>
              </select>
            </label>
            <label>
              数据库匹配字段
              <select name="dbMatchField" defaultValue={importDefaults.dbMatchField}>
                {bojunMatchFieldOptions.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
              </select>
            </label>
            <Field label="Excel 匹配列名" name="matchExcelColumn" defaultValue={importDefaults.matchExcelColumn} />
            {renderImportWriteMappings()}
            <Field label="批量更新大小" name="batchSize" defaultValue={importDefaults.batchSize} />
            <label className={styles.checkboxLabel}>
              <input name="confirmWrite" type="checkbox" />
              确认写入数据库
            </label>
            <p className={styles.modeNote}>
              不勾选时只预检匹配数量，不写库；每个目标列独立判断是否为空，金额字段仅填当前为 0 的记录；Oracle 字段必须用 docno 匹配。
            </p>
            {uploadProgress && <p className={styles.modeNote}>{uploadProgress}</p>}
            <div className={styles.formActions}>
              <button className={styles.primary} type="submit" disabled={loading}>
                <Upload aria-hidden="true" />
                创建预检/导入任务
              </button>
            </div>
          </form>
        </Dialog>
      )}

      {excelDialog === 'clear' && (
        <Dialog className={`${styles.dialog} ${styles.mediumDialog}`} open title={pendingWrite?.slot === 'clear' ? '确认退回未匹配' : '退回未匹配参数'} closeDisabled={loading} onClose={() => { if (!loading) { setPendingWrite(null); closeExcelDialog() } }}>
          {pendingWrite?.slot === 'clear' && <div className={styles.viewStack}><p>{pendingWrite.message}</p><div className={styles.formActions}><button type="button" disabled={loading} onClick={() => setPendingWrite(null)}>返回修改</button><button className={styles.danger} type="button" disabled={loading} onClick={() => void confirmPendingWrite()}>{loading ? '创建任务中…' : '确认退回'}</button></div></div>}
          <form className={styles.uploadForm} onSubmit={createClearMatchedJob} hidden={pendingWrite?.slot === 'clear'}>
            <label className={styles.fileInputLabel}>
              Excel 文件
              <input
                name="file"
                type="file"
                accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                onChange={(event) => {
                  setSelectedClearFileName(event.currentTarget.files?.[0]?.name ?? '')
                  clearUploadRef('clear')
                }}
              />
              <span>{selectedClearFileName || '请选择需要退回的 .xlsx 文件'}</span>
            </label>
            <Field label="Sheet 页名称" name="sheetName" defaultValue="Sheet1" />
            <label>
              匹配表名
              <select name="tableName" defaultValue="bojun_retail_orders">
                <option value="bojun_retail_orders">伯俊零售单 bojun_retail_orders</option>
              </select>
            </label>
            <label>
              数据库匹配字段
              <select name="dbMatchField" defaultValue="docno">
                {bojunMatchFieldOptions.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}
              </select>
            </label>
            <Field label="Excel 匹配列名" name="matchExcelColumn" defaultValue="外部订单编号" />
            <Field label="批量处理大小" name="batchSize" defaultValue="1000" />
            <label className={styles.checkboxLabel}>
              <input name="confirmWrite" type="checkbox" />
              确认清空 matched_docno
            </label>
            <p className={styles.modeNote}>
              不勾选时只预检会命中的行；勾选后会把命中行的 matched_docno 清空，用于退回未匹配状态。
            </p>
            {uploadProgress && <p className={styles.modeNote}>{uploadProgress}</p>}
            <div className={styles.formActions}>
              <button type="submit" disabled={loading}>
                <RefreshCcw aria-hidden="true" />
                创建预检/退回任务
              </button>
            </div>
          </form>
        </Dialog>
      )}

      {excelDialog === 'query' && (
        <Dialog className={styles.dialog} open title="任务查询与下载" onClose={closeExcelDialog}>
          <div className={styles.jobActions}>
            <label>
              任务 ID
              <input name="excelJobID" value={jobs.jobID} onChange={(event) => jobs.setJobID(event.target.value)} />
            </label>
            <button type="button" onClick={refreshJob} disabled={loading}>
              <RefreshCcw aria-hidden="true" />
              查询状态
            </button>
            <button type="button" onClick={() => void jobs.downloadJob()} disabled={loading || !jobs.job || !canDownloadExcelJob(jobs.job)}>
              <Download aria-hidden="true" />
              {jobs.downloadingJobID === Number(jobs.jobID || jobs.job?.id) ? '下载中' : '下载结果'}
            </button>
          </div>
        </Dialog>
      )}

      {pendingSchemeDelete && (
        <Dialog className={styles.dialog} open title="删除 Excel 匹配方案" onClose={() => { if (deletingSchemeID === null) setPendingSchemeDelete(null) }}>
          <p>确认删除方案“{pendingSchemeDelete.name}”？删除后不能恢复，已创建的任务不会受影响。</p>
          <div className={styles.formActions}>
            <button type="button" onClick={() => setPendingSchemeDelete(null)} disabled={deletingSchemeID !== null}>取消</button>
            <button type="button" className={styles.danger} onClick={() => void deleteScheme(pendingSchemeDelete)} disabled={deletingSchemeID !== null}>
              {deletingSchemeID === pendingSchemeDelete.id ? '删除中…' : '确认删除'}
            </button>
          </div>
        </Dialog>
      )}
    </PageCanvas>
  )
}
