import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import test from 'node:test'
import { formatShanghaiDate, formatShanghaiDateTime, parseShanghaiDateTime, toShanghaiDateTimeInput } from '../.test-dist/dateTime.js'

test('displays instants in Shanghai with padded fields and seconds', () => {
  assert.equal(formatShanghaiDateTime('2026-10-29T09:03:04Z'), '2026-10-29 17:03:04')
  assert.equal(formatShanghaiDateTime('2026-10-29T02:03:04-07:00'), '2026-10-29 17:03:04')
  assert.equal(formatShanghaiDateTime('2026-10-29T16:00:00Z'), '2026-10-30 00:00:00')
  assert.equal(formatShanghaiDateTime(Date.UTC(2026, 0, 2, 1, 2, 3)), '2026-01-02 09:02:03')
})

test('treats timestamps without offsets as Shanghai wall-clock time', () => {
  for (const value of ['2026-10-29 09:03:04', '2026-10-29T09:03:04']) {
    assert.equal(formatShanghaiDateTime(value), '2026-10-29 09:03:04')
    assert.equal(parseShanghaiDateTime(value).toISOString(), '2026-10-29T01:03:04.000Z')
  }
  assert.equal(formatShanghaiDateTime('2026-10-29T09:03'), '2026-10-29 09:03:00')
  assert.equal(formatShanghaiDate('2026-10-29T16:00:00Z'), '2026-10-30')
  assert.equal(formatShanghaiDate('2026-10-29'), '2026-10-29')
  assert.equal(toShanghaiDateTimeInput('2026-10-29T16:00:00Z'), '2026-10-30T00:00:00')
})

test('uses placeholders for missing or invalid values', () => {
  for (const value of [null, undefined, '', 'invalid', new Date(NaN), NaN]) {
    assert.equal(formatShanghaiDateTime(value), '—')
    assert.equal(formatShanghaiDateTime(value, '-'), '-')
    assert.equal(toShanghaiDateTimeInput(value), '')
  }
})

test('display and input conversion do not depend on the browser host timezone', () => {
  const moduleURL = new URL('../.test-dist/dateTime.js', import.meta.url).href
  const script = `import { formatShanghaiDateTime, parseShanghaiDateTime } from ${JSON.stringify(moduleURL)}; console.log(JSON.stringify([formatShanghaiDateTime('2026-10-29T16:00:00Z'), parseShanghaiDateTime('2026-10-30T00:00:00').toISOString()]));`
  for (const TZ of ['UTC', 'America/Los_Angeles', 'Asia/Shanghai']) {
    const output = execFileSync(process.execPath, ['--input-type=module', '-e', script], { env: { ...process.env, TZ }, encoding: 'utf8' })
    assert.deepEqual(JSON.parse(output), ['2026-10-30 00:00:00', '2026-10-29T16:00:00.000Z'])
  }
})
