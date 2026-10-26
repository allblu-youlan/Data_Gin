export type ExcelOperation = 'export_match' | 'import_update' | 'oracle_import' | 'oracle_procedure' | 'oracle_export'
export type OracleMethodOperation = 'oracle_import' | 'oracle_procedure'
export type OracleImportConfig = { operation: 'oracle_import'; owner: string; tableName: string; sheetName: string; allSheets?: boolean; rawCellValues?: boolean; headerRow: number; mappings: Array<{ excelColumn: string; field: string }>; fixedIdentifier?: { enabled: boolean; field: string; value: string } }
export type ProcedureParameter = { name: string; direction: 'IN' | 'OUT' | 'IN OUT'; dataType: string; value: string }
export type ProcedureConfig = { operation: 'oracle_procedure'; owner: string; packageName: string; procedureName: string; parameters: ProcedureParameter[]; success: { parameter: string; operator: 'eq' | 'contains'; value: string } }
export type OracleMethodConfig = OracleImportConfig | ProcedureConfig
export type OperationScheme<T = unknown> = { id: number; name: string; operation: string; config: T }
export type WorkflowStep = { id?: string; operation: ExcelOperation; schemeId: number; source?: 'upload' | 'previous'; sourceStepId?: string }
export type WorkflowEditorStep = WorkflowStep & { id: string }
export type WorkflowConfig = { operation: 'excel_workflow'; steps: WorkflowStep[] }

export const operationLabels: Record<ExcelOperation, string> = { export_match: 'Excel 匹配', import_update: 'Excel 写入（回写数据）', oracle_import: '导入 Oracle', oracle_procedure: '存储过程执行', oracle_export: 'Oracle Excel 导出' }
export const operationPaths: Record<ExcelOperation, string> = {
 export_match: '/v1/excel-match-jobs/schemes?operation=export_match',
 import_update: '/v1/excel-match-jobs/schemes?operation=import_update',
 oracle_import: '/v1/excel-match-jobs/operation-schemes/oracle_import',
 oracle_procedure: '/v1/excel-match-jobs/operation-schemes/oracle_procedure',
 oracle_export: '/v1/excel-match-jobs/oracle-export-schemes',
}
export const emptyImportConfig = (): OracleImportConfig => ({ operation: 'oracle_import', owner: '', tableName: '', sheetName: 'Sheet1', allSheets: false, rawCellValues: true, headerRow: 1, mappings: [{ excelColumn: '', field: '' }], fixedIdentifier: { enabled: false, field: '', value: '' } })
export function normalizeOracleImportConfig(config: OracleImportConfig) {
 return { ...config, rawCellValues: config.rawCellValues ?? false, fixedIdentifier: { enabled: config.fixedIdentifier?.enabled ?? false, field: config.fixedIdentifier?.field ?? '', value: '' } }
}
export const emptyProcedureConfig = (): ProcedureConfig => ({ operation: 'oracle_procedure', owner: '', packageName: '', procedureName: '', parameters: [], success: { parameter: '', operator: 'eq', value: '' } })
export const defaultWorkflowSteps = (): WorkflowEditorStep[] => prepareWorkflowSteps([
 { operation: 'export_match', schemeId: 0, source: 'upload' },
 { operation: 'oracle_import', schemeId: 0, source: 'previous' },
 { operation: 'oracle_procedure', schemeId: 0 },
 { operation: 'oracle_export', schemeId: 0 },
])

export function operationNeedsExcel(operation: ExcelOperation): boolean {
 return ['export_match', 'import_update', 'oracle_import'].includes(operation)
}

function createWorkflowStepID(): string {
 return Array.from(crypto.getRandomValues(new Uint32Array(4)), value => value.toString(16)).join('-')
}

export function newWorkflowStep(operation: ExcelOperation): WorkflowEditorStep {
 return { id: createWorkflowStepID(), operation, schemeId: 0, ...(operationNeedsExcel(operation) ? { source: 'upload' as const } : {}) }
}

export function prepareWorkflowSteps(steps: readonly WorkflowStep[]): WorkflowEditorStep[] {
 let previousMatchID: string | undefined
 return steps.map(step => {
  const prepared: WorkflowEditorStep = { ...step, id: step.id || createWorkflowStepID() }
  if (prepared.source === 'previous' && !prepared.sourceStepId && previousMatchID) prepared.sourceStepId = previousMatchID
  if (prepared.operation === 'export_match') previousMatchID = prepared.id
  return prepared
 })
}

export function workflowSourceIssue(step: WorkflowStep, index: number, steps: readonly WorkflowStep[]): string | undefined {
 if (!operationNeedsExcel(step.operation) || step.source !== 'previous') return undefined
 if (!step.sourceStepId) return `第 ${index + 1} 步尚未关联匹配结果，请明确选择此前的 Excel 匹配步骤。`
 const sourceIndex = steps.findIndex(source => source.id === step.sourceStepId)
 if (sourceIndex < 0) return `第 ${index + 1} 步引用的 Excel 匹配步骤已删除，请重新选择数据来源。`
 if (sourceIndex >= index || steps[sourceIndex].operation !== 'export_match') return `第 ${index + 1} 步的数据来源必须是此前的 Excel 匹配步骤，请重新选择。`
 return undefined
}

export function workflowUploadIssues(steps: readonly WorkflowStep[], filesByStep: Readonly<Record<string, readonly Pick<File, 'size'>[]>>): string[] {
 return steps.flatMap((step, index) => {
  if (!operationNeedsExcel(step.operation) || step.source === 'previous') return []
  const files = filesByStep[step.id || ''] ?? []
  return !files.length || files.some(file => file.size === 0) ? [`请为第 ${index + 1} 步“${operationLabels[step.operation]}”独立选择有效的 .xlsx 文件。`] : []
 })
}

export function workflowNeedsUploads(steps: WorkflowStep[]): boolean {
 return steps.some(step => operationNeedsExcel(step.operation) && step.source !== 'previous')
}

export function workflowIssues(steps: WorkflowStep[]): string[] {
 const issues: string[] = []
 if (!steps.length || steps.length > 20) issues.push('请配置 1 至 20 个步骤。')
 steps.forEach((step, i) => {
  if (!step.schemeId) issues.push(`第 ${i + 1} 步请选择已保存的方法。`)
  const sourceIssue = workflowSourceIssue(step, i, steps)
  if (sourceIssue) issues.push(sourceIssue)
 })
 return issues
}
