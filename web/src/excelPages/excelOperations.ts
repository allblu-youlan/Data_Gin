export type ExcelOperation = 'export_match' | 'oracle_import' | 'oracle_procedure' | 'oracle_export'
export type OracleMethodOperation = 'oracle_import' | 'oracle_procedure'
export type OracleImportConfig = { operation: 'oracle_import'; owner: string; tableName: string; sheetName: string; headerRow: number; mappings: Array<{ excelColumn: string; field: string }> }
export type ProcedureParameter = { name: string; direction: 'IN' | 'OUT' | 'IN OUT'; dataType: string; value: string }
export type ProcedureConfig = { operation: 'oracle_procedure'; owner: string; packageName: string; procedureName: string; parameters: ProcedureParameter[]; success: { parameter: string; operator: 'eq' | 'contains'; value: string } }
export type OracleMethodConfig = OracleImportConfig | ProcedureConfig
export type OperationScheme<T = unknown> = { id: number; name: string; operation: string; config: T }
export type WorkflowStep = { operation: ExcelOperation; schemeId: number; source?: 'upload' | 'previous' }
export type WorkflowConfig = { operation: 'excel_workflow'; steps: WorkflowStep[] }

export const operationLabels: Record<ExcelOperation, string> = { export_match: 'Excel 匹配', oracle_import: '导入 Excel 数据', oracle_procedure: '存储过程执行', oracle_export: 'Oracle Excel 导出' }
export const operationPaths: Record<ExcelOperation, string> = {
 export_match: '/v1/excel-match-jobs/schemes?operation=export_match',
 oracle_import: '/v1/excel-match-jobs/operation-schemes/oracle_import',
 oracle_procedure: '/v1/excel-match-jobs/operation-schemes/oracle_procedure',
 oracle_export: '/v1/excel-match-jobs/oracle-export-schemes',
}
export const emptyImportConfig = (): OracleImportConfig => ({ operation: 'oracle_import', owner: '', tableName: '', sheetName: 'Sheet1', headerRow: 1, mappings: [{ excelColumn: '', field: '' }] })
export const emptyProcedureConfig = (): ProcedureConfig => ({ operation: 'oracle_procedure', owner: '', packageName: '', procedureName: '', parameters: [], success: { parameter: '', operator: 'eq', value: '' } })
export const defaultWorkflowSteps = (): WorkflowStep[] => [
 { operation: 'export_match', schemeId: 0, source: 'upload' },
 { operation: 'oracle_import', schemeId: 0, source: 'previous' },
 { operation: 'oracle_procedure', schemeId: 0 },
 { operation: 'oracle_export', schemeId: 0 },
]

export function workflowIssues(steps: WorkflowStep[]): string[] {
 const issues: string[] = []
 let previous = false
 if (!steps.length || steps.length > 20) issues.push('请配置 1 至 20 个步骤。')
 steps.forEach((step, i) => {
  if (!step.schemeId) issues.push(`第 ${i + 1} 步请选择已保存的方法。`)
  if (step.source === 'previous' && !previous) issues.push(`第 ${i + 1} 步之前需要有 Excel 匹配步骤。`)
  if (step.operation === 'export_match') previous = true
 })
 return issues
}
