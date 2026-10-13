import test from 'node:test'
import assert from 'node:assert/strict'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'
import { canRetryMentionJob, confirmationLabel, mentionQuery, parseMentionData, parseMentionPage, safeOriginalURL } from '../.test-dist/officeMention/contracts.js'

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
