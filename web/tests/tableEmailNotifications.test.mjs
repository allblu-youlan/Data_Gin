import test from 'node:test'
import assert from 'node:assert/strict'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'
import { buildNotificationPayload, parseNotificationColumns, parseNotificationPreview, parseNotificationRules, parseSampleRow, validateTemplateFields } from '../.test-dist/tableEmailNotifications/contracts.js'

test('Oracle table email navigation requires both message read and report management access', () => {
  assert.equal(canViewNavigationItem('table_email_notifications', []), false)
  assert.equal(canViewNavigationItem('table_email_notifications', ['office_message.read']), false)
  assert.equal(canViewNavigationItem('table_email_notifications', ['report.manage']), false)
  assert.equal(canViewNavigationItem('table_email_notifications', ['office_message.read', 'report.manage']), true)
})

const fields = [
  { name: 'ID', dataType: 'NUMBER', comment: '流水号', position: 1, nullable: false },
  { name: 'EMAIL', dataType: 'VARCHAR2', comment: '负责人邮箱', position: 2, nullable: true },
  { name: 'ORDER_NO', dataType: 'VARCHAR2', comment: '订单号', position: 3, nullable: false },
]
const draft = {
  id: null, name: '  新订单通知 ', datasourceId: 0, owner: ' ERP ', tableName: 'ORDERS', cursorColumn: 'ID',
  recipientsTemplate: 'ops@example.com, {{EMAIL}}', subjectTemplate: '新订单 {{order_no}}', bodyTemplate: '订单编号：{{ORDER_NO}}\n请及时处理。',
  enabled: true, pollIntervalSeconds: 60, lockVersion: 0,
}

test('saving preserves mixed recipient templates, line breaks and the selected Oracle source', () => {
  assert.deepEqual(buildNotificationPayload(draft, fields), {
    name: '新订单通知', datasourceId: 0, owner: 'ERP', tableName: 'ORDERS', cursorColumn: 'ID',
    recipientsTemplate: 'ops@example.com, {{EMAIL}}', subjectTemplate: '新订单 {{order_no}}', bodyTemplate: '订单编号：{{ORDER_NO}}\n请及时处理。',
    enabled: true, pollIntervalSeconds: 60,
  })
  assert.equal(buildNotificationPayload({ ...draft, id: 7, lockVersion: 3 }, fields).lockVersion, 3)
})

test('unknown template fields are caught before saving while field names are case insensitive', () => {
  assert.doesNotThrow(() => validateTemplateFields('{{email}} / {{ ORDER_NO }}', fields))
  assert.throws(() => buildNotificationPayload({ ...draft, recipientsTemplate: '{{PHONE}}' }, fields), /PHONE/)
  assert.throws(() => buildNotificationPayload({ ...draft, subjectTemplate: '{{ORDER_NO}' }, fields), /模板/)
})

test('required configuration and cursor constraints prevent invalid listener requests', () => {
  assert.throws(() => buildNotificationPayload({ ...draft, recipientsTemplate: '' }, fields), /收件人/)
  assert.throws(() => buildNotificationPayload({ ...draft, cursorColumn: 'EMAIL' }, fields), /游标/)
  assert.throws(() => buildNotificationPayload({ ...draft, pollIntervalSeconds: 10 }, fields), /60/)
  assert.throws(() => buildNotificationPayload({ ...draft, datasourceId: -1 }, fields), /数据源/)
})

test('preview sample must be a JSON object and preserves numeric and null field values', () => {
  assert.deepEqual(parseSampleRow('{"ID":42,"EMAIL":"team@example.com","ORDER_NO":null}'), { ID: 42, EMAIL: 'team@example.com', ORDER_NO: null })
  assert.throws(() => parseSampleRow('[]'), /对象/)
  assert.throws(() => parseSampleRow('{bad'), /JSON/)
})

test('API parsers unwrap envelopes and retain cursor state and preview output', () => {
  assert.deepEqual(parseNotificationColumns({ code: 0, data: { items: fields } }), fields)
  const rule = { ...draft, id: 7, lockVersion: 3, cursorValue: '100', lastCheckedAt: null, lastError: '', createdAt: '2026-10-28', updatedAt: '2026-10-28' }
  assert.equal(parseNotificationRules({ data: { items: [rule] } })[0].cursorValue, '100')
  assert.deepEqual(parseNotificationPreview({ data: { recipients: ['ops@example.com'], subject: '新订单 42', body: '订单编号：42\n请及时处理。' } }), { recipients: ['ops@example.com'], subject: '新订单 42', body: '订单编号：42\n请及时处理。' })
  assert.throws(() => parseNotificationRules({ data: { items: null } }))
})
