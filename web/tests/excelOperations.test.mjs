import assert from 'node:assert/strict'
import test from 'node:test'
import { defaultWorkflowSteps, emptyImportConfig, workflowIssues, workflowNeedsUploads } from '../.test-dist/excelPages/excelOperations.js'

test('four operation workflow can pass matched workbook to Oracle import', () => {
 const steps = defaultWorkflowSteps().map((step, index) => ({ ...step, schemeId: index + 1 }))
 assert.deepEqual(steps.map(step => step.operation), ['export_match', 'oracle_import', 'oracle_procedure', 'oracle_export'])
 assert.equal(steps[1].source, 'previous')
 assert.deepEqual(workflowIssues(steps), [])
})

test('moving Oracle import before its matched workbook rejects the plan', () => {
 const steps = defaultWorkflowSteps().map((step, index) => ({ ...step, schemeId: index + 1 }))
 ;[steps[0], steps[1]] = [steps[1], steps[0]]
 assert.match(workflowIssues(steps).join(' '), /第 1 步之前需要有 Excel 匹配步骤/)
})

test('procedure-only runs need no Excel input while missing methods remain invalid', () => {
 assert.deepEqual(workflowIssues([{ operation: 'oracle_procedure', schemeId: 7 }]), [])
 assert.match(workflowIssues([{ operation: 'oracle_procedure', schemeId: 0 }]).join(' '), /请选择已保存的方法/)
 assert.equal(workflowIssues([]).length, 1)
})

test('workflow requests uploads only for steps using original files', () => {
 assert.equal(workflowNeedsUploads([{ operation: 'oracle_procedure', schemeId: 7 }, { operation: 'oracle_export', schemeId: 8 }]), false)
 assert.equal(workflowNeedsUploads([{ operation: 'oracle_import', schemeId: 7, source: 'previous' }]), false)
 assert.equal(workflowNeedsUploads([{ operation: 'oracle_import', schemeId: 7, source: 'upload' }]), true)
 assert.equal(workflowNeedsUploads([{ operation: 'export_match', schemeId: 7 }]), true)
})

test('new Oracle import methods retain specified-sheet behavior until all sheets is enabled', () => {
 const config = emptyImportConfig()
 assert.equal(config.sheetName, 'Sheet1')
 assert.equal(config.allSheets, false)
 assert.deepEqual(config.fixedIdentifier, { enabled: false, field: '', value: '' })
})
