import assert from 'node:assert/strict'
import test from 'node:test'
import { defaultWorkflowSteps, workflowIssues } from '../.test-dist/excelPages/excelOperations.js'

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
