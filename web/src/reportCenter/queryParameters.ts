import type { ReportExport, ReportExportStatus, ReportParameter, ReportResultPage, ReportResultQuery, ReportRun, ReportRunStatus } from './types'

export const terminalReportRunStatuses = new Set<ReportRunStatus>(['SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPORTED', 'RESULT_PURGED', 'SUPERSEDED'])
export const terminalReportExportStatuses = new Set<ReportExportStatus>(['READY', 'FAILED', 'CANCELLED', 'EXPIRED'])
const reportExportRunStatuses = new Set<ReportRunStatus>(['SUCCEEDED', 'EXPORTING', 'EXPORTED', 'RESULT_PURGING', 'RESULT_PURGED'])

export type NewReportRunState = {
  values: Record<string, unknown>
  run: ReportRun | null
  result: ReportResultPage | null
  reportExport: ReportExport | null
  resultQuery: ReportResultQuery
  appliedQuery: ReportResultQuery
  cursorHistory: string[]
  cursorIndex: number
  filtersOpen: boolean
  parametersOpen: boolean
  operation: { busy: boolean; error: string }
}

export function visibleReportParameters(parameters: ReportParameter[]) {
  return parameters.filter((parameter) => !parameter.systemInjected)
}

export function initialReportParameterValues(parameters: ReportParameter[]) {
  return visibleReportParameters(parameters).reduce<Record<string, unknown>>((result, parameter) => {
    result[parameter.code] = editableDefaultValue(parameter)
    return result
  }, {})
}

export function canStartNewReportRun(runStatus: ReportRunStatus, reportExport: Pick<ReportExport, 'status' | 'purgedAt'> | null, busy: boolean, purgeAfterExport = true) {
  const runFinished = terminalReportRunStatuses.has(runStatus)
  const exportFinished = isReportExportSettled(reportExport, purgeAfterExport)
  return !busy && runFinished && exportFinished
}

export function canBindReportExport(run: Pick<ReportRun, 'status' | 'resultAvailable'>) {
  return run.resultAvailable || reportExportRunStatuses.has(run.status)
}

export function canRetryReportExportBinding(run: Pick<ReportRun, 'status' | 'resultAvailable'> | null, reportExport: ReportExport | null) {
  return run !== null && reportExport === null && canBindReportExport(run)
}

export function isReportExportSettled(reportExport: Pick<ReportExport, 'status' | 'purgedAt'> | null, purgeAfterExport = true) {
  if (reportExport === null) return true
  if (!terminalReportExportStatuses.has(reportExport.status)) return false
  return reportExport.status !== 'READY' || !purgeAfterExport || reportExport.purgedAt !== null
}

export function buildNewReportRunState(parameters: ReportParameter[]): NewReportRunState {
  return {
    values: initialReportParameterValues(parameters),
    run: null,
    result: null,
    reportExport: null,
    resultQuery: { filters: [], sort: [] },
    appliedQuery: { filters: [], sort: [] },
    cursorHistory: [''],
    cursorIndex: 0,
    filtersOpen: false,
    parametersOpen: true,
    operation: { busy: false, error: '' },
  }
}

function editableDefaultValue(parameter: ReportParameter): unknown {
  const value = parameter.defaultValue
  if (value === null || value === undefined) return parameter.logicalType === 'multi_enum' ? [] : ''

  switch (parameter.logicalType) {
    case 'integer':
    case 'decimal':
      return String(value)
    case 'json':
      return JSON.stringify(value)
    case 'multi_enum':
      return Array.isArray(value) ? value.map(String) : []
    case 'boolean':
      return typeof value === 'boolean' ? value : ''
    default:
      return String(value)
  }
}
