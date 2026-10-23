import assert from 'node:assert/strict'
import test from 'node:test'
import { defaultWorkflowSteps, emptyImportConfig, newWorkflowStep, operationLabels, operationPaths, prepareWorkflowSteps, workflowIssues, workflowNeedsUploads, workflowSourceIssue, workflowUploadIssues } from '../.test-dist/excelPages/excelOperations.js'

test('four operation workflow can pass matched workbook to Oracle import', () => {
 const steps = defaultWorkflowSteps().map((step, index) => ({ ...step, schemeId: index + 1 }))
 assert.deepEqual(steps.map(step => step.operation), ['export_match', 'oracle_import', 'oracle_procedure', 'oracle_export'])
 assert.equal(steps[1].source, 'previous')
 assert.deepEqual(workflowIssues(steps), [])
})

test('moving Oracle import before its matched workbook rejects the plan', () => {
 const steps = defaultWorkflowSteps().map((step, index) => ({ ...step, schemeId: index + 1 }))
 ;[steps[0], steps[1]] = [steps[1], steps[0]]
 assert.match(workflowIssues(steps).join(' '), /第 1 步的数据来源必须是此前的 Excel 匹配步骤/)
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

test('Excel write methods use their existing scheme endpoint and independent upload source', () => {
 assert.equal(operationLabels.import_update, 'Excel 写入（回写数据）')
 assert.equal(operationLabels.oracle_import, '导入 Oracle')
 assert.equal(operationPaths.import_update, '/v1/excel-match-jobs/schemes?operation=import_update')
 const first = newWorkflowStep('import_update')
 const second = newWorkflowStep('import_update')
 assert.ok(first.id)
 assert.notEqual(first.id, second.id)
 assert.equal(first.source, 'upload')
 assert.equal(workflowNeedsUploads([first]), true)
})

test('loading a legacy workflow assigns stable IDs and binds each previous source to the closest earlier match', () => {
 const legacy = [
  { operation: 'export_match', schemeId: 1, source: 'upload' },
  { operation: 'oracle_import', schemeId: 2, source: 'previous' },
  { operation: 'export_match', schemeId: 3, source: 'upload' },
  { operation: 'import_update', schemeId: 4, source: 'previous' },
 ]
 const prepared = prepareWorkflowSteps(legacy)
 assert.equal(new Set(prepared.map(step => step.id)).size, 4)
 assert.equal(prepared[1].sourceStepId, prepared[0].id)
 assert.equal(prepared[3].sourceStepId, prepared[2].id)
 assert.deepEqual(prepareWorkflowSteps(prepared), prepared)
 assert.deepEqual(workflowIssues(prepared), [])
 assert.equal(legacy[0].id, undefined)
 assert.equal(legacy[1].sourceStepId, undefined)
})

test('deleted or reordered result sources remain invalid instead of retargeting another match', () => {
 const steps = prepareWorkflowSteps([
  { id: 'first', operation: 'export_match', schemeId: 1, source: 'upload' },
  { id: 'second', operation: 'export_match', schemeId: 2, source: 'upload' },
  { id: 'write', operation: 'import_update', schemeId: 3, source: 'previous', sourceStepId: 'first' },
 ])
 const deleted = steps.filter(step => step.id !== 'first')
 assert.equal(deleted[1].sourceStepId, 'first')
 assert.match(workflowSourceIssue(deleted[1], 1, deleted), /已删除/)
 const reordered = [steps[1], steps[2], steps[0]]
 assert.equal(reordered[1].sourceStepId, 'first')
 assert.match(workflowSourceIssue(reordered[1], 1, reordered), /此前的 Excel 匹配步骤/)
 const reloaded = prepareWorkflowSteps(deleted)
 assert.equal(reloaded[1].sourceStepId, 'first')
 assert.match(workflowIssues(reloaded).join(' '), /已删除/)
})

test('legacy previous sources without an earlier match require an explicit replacement', () => {
 const steps = prepareWorkflowSteps([{ operation: 'oracle_import', schemeId: 2, source: 'previous' }])
 assert.equal(steps[0].sourceStepId, undefined)
 assert.match(workflowIssues(steps).join(' '), /尚未关联匹配结果/)
})

test('each upload step needs its own files and keeps ownership after reordering', () => {
 const steps = [
  { id: 'match', operation: 'export_match', schemeId: 1, source: 'upload' },
  { id: 'write', operation: 'import_update', schemeId: 2, source: 'upload' },
  { id: 'import', operation: 'oracle_import', schemeId: 3, source: 'previous', sourceStepId: 'match' },
 ]
 const files = { match: [{ size: 25 }] }
 assert.match(workflowUploadIssues(steps, files).join(' '), /第 2 步.*Excel 写入/)
 const reordered = [steps[1], steps[0], steps[2]]
 assert.match(workflowUploadIssues(reordered, files).join(' '), /第 1 步.*Excel 写入/)
 assert.deepEqual(workflowUploadIssues(reordered, { ...files, write: [{ size: 20 }, { size: 30 }] }), [])
 assert.match(workflowUploadIssues(steps, { ...files, write: [{ size: 0 }] }).join(' '), /第 2 步/)
})
