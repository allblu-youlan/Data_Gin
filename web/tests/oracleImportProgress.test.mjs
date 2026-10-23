import assert from 'node:assert/strict'
import test from 'node:test'
import { latestOracleImportProgress, oracleImportProgressView, parseOracleImportProgress } from '../.test-dist/excelPages/oracleImportProgress.js'
import { nextExcelJobPollState } from '../.test-dist/excelPages/excelJobPolling.js'

const progress = { stepIndex: 1, stepName: '导入订单', stage: 'importing', processedRows: 250, totalRows: 1000, totalKnown: true, scannedRows: 1000, fileIndex: 2, fileCount: 3, fileName: '订单.xlsx', sheetName: '销售', rowNumber: 251, identifier: '42' }
const log = (id, value = progress, jobID = 7) => ({ id, job_id: jobID, import_progress: value })

test('uses the highest structured progress log ID for the requested job regardless of array order', () => {
  const latest = { ...progress, processedRows: 500 }
  assert.deepEqual(latestOracleImportProgress([log(20, latest), log(99, { ...progress, stage: 'completed' }, 8), log(10), { id: 30, job_id: 7, message: '导入完成', detail_json: JSON.stringify({ import_progress: { ...progress, stage: 'completed' } }) }], 7), latest)
  assert.equal(latestOracleImportProgress([log(1, progress, 8)], 7), null)
})

test('ignores invalid progress payloads and never replaces malformed latest progress with stale success', () => {
  for (const payload of [null, [], {}, { ...progress, stage: 'unknown' }, { ...progress, processedRows: '100' }, { ...progress, totalRows: -1 }, { ...progress, totalKnown: 'true' }, { ...progress, stepIndex: 0 }, { ...progress, fileName: null }]) assert.equal(parseOracleImportProgress(payload), null)
  assert.equal(latestOracleImportProgress([log(1, { ...progress, stage: 'completed' }), log(2, {})], 7), null)
  assert.equal(latestOracleImportProgress([{ ...log(5), id: '5' }], 7), null)
})

test('counting and unknown totals stay indeterminate while reporting scanned or written rows', () => {
  const counting = oracleImportProgressView({ ...progress, stage: 'counting', totalKnown: false, scannedRows: 230 }, 'running')
  assert.equal(counting.percent, undefined)
  assert.match(counting.headline, /正在统计.*230/)
  const unknown = oracleImportProgressView({ ...progress, totalKnown: false }, 'running')
  assert.equal(unknown.percent, undefined)
  assert.match(unknown.headline, /已写入 250 行/)
  assert.match(unknown.note, /待事务提交/)
})

test('importing shows row progress without claiming commit success, even at one hundred percent', () => {
  const partial = oracleImportProgressView(progress, 'running')
  assert.equal(partial.percent, 25)
  assert.match(partial.headline, /250 \/ 1,000 行（25%）/)
  const full = oracleImportProgressView({ ...progress, processedRows: 1000 }, 'running')
  assert.equal(full.percent, 100)
  assert.equal(full.stage, 'importing')
  assert.doesNotMatch(full.headline, /成功/)
  assert.match(full.note, /待事务提交/)
})

test('committing remains pending until completed, including empty table completion', () => {
  const committing = oracleImportProgressView({ ...progress, stage: 'committing', processedRows: 1000 }, 'running')
  assert.equal(committing.percent, 100)
  assert.match(committing.headline, /正在提交事务/)
  assert.match(committing.note, /尚未确认成功/)
  const completed = oracleImportProgressView({ ...progress, stage: 'completed', processedRows: 1000 }, 'success')
  assert.equal(completed.percent, 100)
  assert.match(completed.headline, /成功导入 1,000 \/ 1,000 行（100%）/)
  const empty = oracleImportProgressView({ ...progress, stage: 'completed', processedRows: 0, totalRows: 0 }, 'success')
  assert.equal(empty.percent, 100)
  assert.match(empty.headline, /成功导入 0 \/ 0/)
})

test('failure reports rollback and zero successful rows for explicit failure and stale uncommitted progress', () => {
  for (const stage of ['importing', 'committing', 'failed']) {
    const failed = oracleImportProgressView({ ...progress, stage }, 'failed')
    assert.equal(failed.stage, 'failed')
    assert.equal(failed.percent, 0)
    assert.match(failed.headline, /已回滚，本次成功导入 0 行/)
    assert.match(failed.note, /回滚前已写入 250 行/)
  }
  assert.equal(oracleImportProgressView({ ...progress, stage: 'failed' }, 'running').stage, 'failed')
  assert.equal(oracleImportProgressView({ ...progress, stage: 'completed' }, 'failed').stage, 'completed')
})

test('successful polling continues beyond sixty requests and resets consecutive failures', () => {
  let failures = 59
  for (let request = 0; request < 180; request++) {
    const next = nextExcelJobPollState(failures, true, 60)
    assert.equal(next.stop, false)
    assert.equal(next.delayMilliseconds, 2000)
    failures = next.consecutiveFailures
  }
  assert.equal(failures, 0)
  assert.equal(nextExcelJobPollState(failures, false, 60).consecutiveFailures, 1)
})

test('polling stops only at the consecutive failure limit and preserves bounded backoff', () => {
  assert.deepEqual(nextExcelJobPollState(0, false, 60), { consecutiveFailures: 1, stop: false, delayMilliseconds: 4000 })
  assert.equal(nextExcelJobPollState(58, false, 60).stop, false)
  assert.deepEqual(nextExcelJobPollState(59, false, 60), { consecutiveFailures: 60, stop: true, delayMilliseconds: 30000 })
  assert.deepEqual(nextExcelJobPollState(59, true, 60), { consecutiveFailures: 0, stop: false, delayMilliseconds: 2000 })
})
