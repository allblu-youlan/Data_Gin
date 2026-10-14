import test from 'node:test'
import assert from 'node:assert/strict'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'
import { canResendMentionReport, canRetryMentionJob, confirmationLabel, mentionDigestSettingsRequest, mentionQuery, mentionResendReportRequest, parseMentionData, parseMentionPage, reportKindLabel, safeOriginalURL } from '../.test-dist/officeMention/contracts.js'

test('read and manage navigation are independent and old office permission grants neither', () => {
  for (const key of ['mention_chats', 'mention_recipients', 'mention_reports', 'mention_jobs', 'mention_status']) assert.equal(canViewNavigationItem(key, ['office_message.read']), false)
  assert.equal(canViewNavigationItem('mention_recipients', ['office_mention.manage']), false)
  assert.equal(canViewNavigationItem('mention_chats', ['office_mention.read']), false)
  assert.equal(canViewNavigationItem('mention_status', ['office_mention.manage']), true)
  assert.equal(canViewNavigationItem('mention_reports', ['office_mention.read']), true)
})
test('retry needs read, retry, failed state and server eligibility', () => {
  assert.equal(canRetryMentionJob({ status: 'failed' }, ['office_mention.retry']), false)
  assert.equal(canRetryMentionJob({ status: 'failed' }, ['office_mention.read']), false)
  assert.equal(canRetryMentionJob({ status: 'failed', retryable: true }, ['office_mention.read', 'office_mention.retry']), true)
  assert.equal(canRetryMentionJob({ status: 'failed', retryable: false }, ['office_mention.read', 'office_mention.retry']), false)
  for (const status of ['sent', 'cancelled', 'sending', 'pending', 'retry_wait']) assert.equal(canRetryMentionJob({ status }, ['office_mention.read', 'office_mention.retry']), false)
})
test('view status remains independent of feature switches and delivery', () => {
  assert.equal(confirmationLabel({ viewStatus: 'not_required', confirmationEnabled: false }), '无需确认')
  assert.equal(confirmationLabel({ viewStatus: 'pending', confirmationEnabled: false }), '待确认查看（确认功能已关闭）')
  assert.equal(confirmationLabel({ viewStatus: 'confirmed', confirmationEnabled: false }), '已确认查看')
})
test('empty and out of range pages retain requested page and result array', () => {
  const data = { result: [], pagination: { page: 99, per_page: 20, total_count: 0, total_page: 0 } }
  assert.deepEqual(parseMentionPage({ code: 0, data }), data)
  assert.throws(() => parseMentionPage({ result: null, pagination: data.pagination }))
  assert.throws(() => parseMentionData({ code: 100429, data: {} }))
})
test('original links cannot execute scripts and time filters use Shanghai offset', () => {
  assert.equal(safeOriginalURL('javascript:alert(1)'), null)
  assert.equal(safeOriginalURL('data:text/html,script'), null)
  assert.equal(safeOriginalURL('https://example.com/message'), 'https://example.com/message')
  const q = new URLSearchParams(mentionQuery({ receivedFrom: '2026-10-13T10:00', receivedTo: '2026-10-13T18:00', chatId: ' oc_A ' }, 2))
  assert.equal(q.get('receivedFrom'), '2026-10-13T10:00:00+08:00')
  assert.equal(q.get('chatId'), 'oc_A')
  assert.equal(q.get('page'), '2')
})
test('digest settings request only updates the strict Shanghai HH:mm clock', () => {
  for (const digestTime of ['00:00', '08:05', '18:30', '23:59']) {
    assert.deepEqual(mentionDigestSettingsRequest(digestTime), { path: '/digest-settings', options: { method: 'PUT', body: { digestTime } } })
  }
  for (const invalid of ['', '8:00', '24:00', '18:60', '18:00:00', ' 18:00', '18:00 ', '18:00\n']) {
    assert.throws(() => mentionDigestSettingsRequest(invalid), /HH:mm/)
  }
})
test('recount allows daily source states but requires finished manual reports and both permissions', () => {
  const permissions = ['office_mention.read', 'office_mention.retry']
  for (const reportKind of [undefined, 'daily', 'manual']) {
    for (const status of ['ready', 'sent', 'failed']) {
      assert.equal(canResendMentionReport({ status, reportKind }, permissions), reportKind !== 'manual' || status !== 'ready')
      assert.equal(canResendMentionReport({ status, reportKind }, ['office_mention.read']), false)
      assert.equal(canResendMentionReport({ status, reportKind }, ['office_mention.retry']), false)
      assert.equal(canResendMentionReport({ status, reportKind }, ['office_mention.manage']), false)
    }
    for (const status of ['pending', 'sending', 'retry_wait', 'cancelled']) assert.equal(canResendMentionReport({ status, reportKind }, permissions), false)
  }
  assert.deepEqual(mentionResendReportRequest(27), { path: '/reports/27/resend', options: { method: 'POST', body: {} } })
  for (const id of [0, -1, 1.5, NaN, Number.MAX_SAFE_INTEGER + 1]) assert.throws(() => mentionResendReportRequest(id))
})
test('manual report labels are distinct while older reports remain daily', () => {
  assert.equal(reportKindLabel({ reportKind: 'manual' }), '手动统计')
  assert.equal(reportKindLabel({ reportKind: 'daily' }), '定时日报')
  assert.equal(reportKindLabel({}), '定时日报')
})
