import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildOfficeMessagePayload,
  emptyOfficeMessageDraft,
  mappingsFromColumns,
  parseOfficeFeishuBots,
  parseOfficeMessages,
  parseOfficeRuns,
  parseOfficeSchedules,
  parseOfficeTargets,
} from '../.test-dist/officeMessage/contracts.js'
import {
  DEFAULT_WEBDAV_URL,
  buildPushTargetPayload,
  emptyPushTarget,
  targetDraftFrom,
} from '../.test-dist/officeMessage/pushTargetDraft.js'

test('office Feishu bot contract projects public App ID metadata', () => {
  const [bot] = parseOfficeFeishuBots({ data: { items: [{ id: 'cli_office', name: '办公消息机器人', source: 'ENVIRONMENT' }] } })
  assert.deepEqual(bot, { id: 'cli_office', name: '办公消息机器人', source: 'ENVIRONMENT' })

  const [target] = parseOfficeTargets({ data: { items: [{
    id: 3, name: '销售日报推送', messageId: 8, channel: 'FEISHU', botAppId: 'cli_office',
    receiveIdType: 'chat_id', receiveId: 'oc_sales', enabled: true, lockVersion: 1, updatedAt: '2026-09-01T08:00:00Z',
  }] } })
  assert.equal(target.botAppId, 'cli_office')
})

test('legacy office target without channel remains Feishu', () => {
  const [target] = parseOfficeTargets({ data: { items: [{
    id: 4, name: '历史推送', messageId: 8, botAppId: 'cli_office', receiveIdType: 'chat_id', receiveId: 'oc_sales',
    enabled: true, lockVersion: 1, updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.equal(target.channel, 'FEISHU')
})

test('office WebDAV target contract accepts empty Feishu fields without exposing credentials', () => {
  const [target] = parseOfficeTargets({ data: { items: [{
    id: 5, name: '坚果云日报', messageId: 8, channel: 'WEBDAV', botAppId: '', receiveIdType: '', receiveId: '',
    webdavUrl: DEFAULT_WEBDAV_URL, webdavUsername: 'report@example.com', webdavPath: '/reports', hasWebdavPassword: true,
    enabled: true, lockVersion: 2, updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.deepEqual({
    channel: target.channel,
    receiveIdType: target.receiveIdType,
    webdavUrl: target.webdavUrl,
    webdavUsername: target.webdavUsername,
    webdavPath: target.webdavPath,
    hasWebdavPassword: target.hasWebdavPassword,
  }, {
    channel: 'WEBDAV', receiveIdType: '', webdavUrl: DEFAULT_WEBDAV_URL,
    webdavUsername: 'report@example.com', webdavPath: '/reports', hasWebdavPassword: true,
  })
  assert.throws(() => parseOfficeTargets({ data: { items: [{
    id: 5, name: '泄漏凭据', messageId: 8, channel: 'WEBDAV', webdavPasswordCiphertext: 'ciphertext',
    enabled: true, lockVersion: 2, updatedAt: '2026-09-01T08:00:00Z',
  }] } }), /WebDAV 凭据/)
})

test('WebDAV target draft defaults to Jianguoyun and requires an Excel message and new password', () => {
  const messages = [{ id: 7, name: '文本通知', sourceType: 'EDITED', enabled: true }, { id: 8, name: '销售日报', sourceType: 'ORACLE_QUERY', enabled: true }]
  const draft = emptyPushTarget(messages, [])
  assert.equal(draft.channel, 'WEBDAV')
  assert.equal(draft.messageId, 8)
  assert.equal(draft.webdavUrl, DEFAULT_WEBDAV_URL)

  const configured = { ...draft, name: '坚果云日报', webdavUsername: 'report@example.com', webdavPath: '/reports' }
  assert.throws(() => buildPushTargetPayload(configured, messages), /应用密码/)
  assert.throws(() => buildPushTargetPayload({ ...configured, messageId: 7, webdavPassword: 'secret' }, messages), /Excel/)
  assert.equal(buildPushTargetPayload({ ...configured, webdavPassword: 'secret' }, messages).webdavPassword, 'secret')
})

test('editing a WebDAV target leaves its stored password unchanged when the password input is empty', () => {
  const messages = [{ id: 8, name: '销售日报', sourceType: 'ORACLE_PROCEDURE', enabled: true }]
  const draft = targetDraftFrom({
    id: 5, name: '坚果云日报', messageId: 8, channel: 'WEBDAV', botAppId: '', receiveIdType: '', receiveId: '',
    webdavUrl: DEFAULT_WEBDAV_URL, webdavUsername: 'report@example.com', webdavPath: '/reports', hasWebdavPassword: true,
    enabled: true, lockVersion: 3, updatedAt: '2026-09-01T08:00:00Z',
  })

  assert.equal(draft.webdavPassword, '')
  assert.equal(buildPushTargetPayload(draft, messages).webdavPassword, '')
})

test('office push schedule contract keeps Cron, time zone and scheduled date parameters', () => {
  const [schedule] = parseOfficeSchedules({ data: { items: [{
    id: 12, name: '每日销售日报', targetId: 3, cronExpr: '0 9 * * *', timeZone: 'Asia/Shanghai',
    parameters: {
      bill_date: { mode: 'SCHEDULED_DATE', offsetDays: -1 },
      store_code: { mode: 'LITERAL', value: 'SH001' },
    },
    enabled: true, nextRunAt: '2026-09-02T01:00:00Z', lastScheduledAt: '2026-09-01T01:00:00Z',
    lastError: '', lockVersion: 2, updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.equal(schedule.cronExpr, '0 9 * * *')
  assert.equal(schedule.timeZone, 'Asia/Shanghai')
  assert.deepEqual(schedule.parameters.bill_date, { mode: 'SCHEDULED_DATE', value: '', offsetDays: -1 })
  assert.deepEqual(schedule.parameters.store_code, { mode: 'LITERAL', value: 'SH001', offsetDays: 0 })
})

test('office push run contract distinguishes manual and scheduled triggers', () => {
  const runs = parseOfficeRuns({ data: { items: [{
    id: 21, runUuid: 'schedule-run', targetId: 3, messageId: 8, status: 'SUCCEEDED', attemptCount: 1, rowCount: 14,
    errorMessage: '', triggerType: 'SCHEDULE', scheduleId: 12, scheduledFor: '2026-09-01T01:00:00Z',
    createdAt: '2026-09-01T01:00:01Z', finishedAt: '2026-09-01T01:00:02Z',
  }, {
    id: 22, runUuid: 'manual-run', targetId: 3, messageId: 8, status: 'QUEUED', attemptCount: 0, rowCount: 0,
    errorMessage: '', triggerType: 'MANUAL', createdAt: '2026-09-01T02:00:00Z', finishedAt: '',
  }] } })

  assert.deepEqual(runs.map(({ triggerType, scheduleId, scheduledFor }) => ({ triggerType, scheduleId, scheduledFor })), [
    { triggerType: 'SCHEDULE', scheduleId: 12, scheduledFor: '2026-09-01T01:00:00Z' },
    { triggerType: 'MANUAL', scheduleId: 0, scheduledFor: '' },
  ])
})

test('office message contract keeps yyyyMMdd query parameters and column mappings', () => {
  const [message] = parseOfficeMessages({ data: { items: [{
    id: 8,
    name: '销售日报',
    sourceType: 'ORACLE_QUERY',
    content: '',
    procedureOwner: '',
    packageName: '',
    procedureName: '',
    procedureOverload: '',
    resultTableOwner: '',
    resultTableName: '',
    selectSql: 'SELECT ORDER_NO FROM SALES WHERE BILL_DATE = :bill_date',
    fileNameTemplate: '销售日报_{{date:yyyyMMdd}}.xlsx',
    parameters: [{ code: 'bill_date', label: '业务日期', valueType: 'date', format: 'yyyyMMdd', required: true }],
    columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
    enabled: true,
    lockVersion: 2,
    updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.equal(message.parameters[0].format, 'yyyyMMdd')
  assert.equal(message.columnMapping[0].header, '单号')
  assert.equal(message.fileNameTemplate, '销售日报_{{date:yyyyMMdd}}.xlsx')
})

test('legacy Oracle messages keep the message-name workbook fallback', () => {
  const [message] = parseOfficeMessages({ data: { items: [{
    id: 9, name: '销售/日报', sourceType: 'ORACLE_PROCEDURE', content: '',
    procedureOwner: 'REPORT', packageName: '', procedureName: 'BUILD_REPORT', procedureOverload: '',
    resultTableOwner: 'REPORT', resultTableName: 'DAILY_RESULT', selectSql: '',
    parameters: [], columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
    enabled: true, lockVersion: 1, updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.equal(message.fileNameTemplate, '销售-日报.xlsx')
})

test('legacy edited messages preserve an empty workbook template for source switching', () => {
  const [message] = parseOfficeMessages({ data: { items: [{
    id: 10, name: '待切换消息', sourceType: 'EDITED', content: '消息正文',
    procedureOwner: '', packageName: '', procedureName: '', procedureOverload: '',
    resultTableOwner: '', resultTableName: '', selectSql: '',
    parameters: [], columnMapping: [], enabled: true, lockVersion: 1, updatedAt: '2026-09-01T08:00:00Z',
  }] } })

  assert.equal(message.fileNameTemplate, '')
})

test('query payload requires configured names to match SELECT binds', () => {
  const draft = {
    ...emptyOfficeMessageDraft(),
    name: '销售日报',
    sourceType: 'ORACLE_QUERY',
    selectSql: 'SELECT ORDER_NO FROM SALES WHERE BILL_DATE = :bill_date',
    parameters: [{ code: 'wrong_date', label: '业务日期', valueType: 'date', format: 'yyyyMMdd', required: true }],
    columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
  }
  assert.throws(() => buildOfficeMessagePayload(draft), /命名绑定/)
})

test('query payload ignores colons inside Oracle string literals', () => {
  const draft = {
    ...emptyOfficeMessageDraft(),
    name: '销售日报',
    sourceType: 'ORACLE_QUERY',
    selectSql: "SELECT ':display_text' AS LABEL, ORDER_NO FROM SALES WHERE BILL_DATE = :bill_date",
    parameters: [{ code: 'bill_date', label: '业务日期', valueType: 'date', format: 'yyyyMMdd', required: true }],
    columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
  }
  assert.equal(buildOfficeMessagePayload(draft).parameters.length, 1)
})

test('Oracle column metadata creates ordered Excel mappings', () => {
  assert.deepEqual(mappingsFromColumns([
    { name: 'AMOUNT', dataType: 'NUMBER' },
    { name: 'CREATED_AT', dataType: 'TIMESTAMP' },
  ]).map(({ sourceColumn, valueType, order }) => ({ sourceColumn, valueType, order })), [
    { sourceColumn: 'AMOUNT', valueType: 'decimal', order: 0 },
    { sourceColumn: 'CREATED_AT', valueType: 'datetime', order: 1 },
  ])
})

test('Oracle message payload includes a daily Excel file name template', () => {
  const draft = {
    ...emptyOfficeMessageDraft(),
    name: '销售日报',
    sourceType: 'ORACLE_QUERY',
    selectSql: 'SELECT ORDER_NO FROM SALES',
    fileNameTemplate: '销售日报_{{date:yyyyMMdd}}.xlsx',
    columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
  }
  assert.equal(buildOfficeMessagePayload(draft).fileNameTemplate, '销售日报_{{date:yyyyMMdd}}.xlsx')
})

test('Oracle message payload rejects unsafe or unknown file name templates', () => {
  const draft = {
    ...emptyOfficeMessageDraft(),
    name: '销售日报',
    sourceType: 'ORACLE_QUERY',
    selectSql: 'SELECT ORDER_NO FROM SALES',
    columnMapping: [{ sourceColumn: 'ORDER_NO', header: '单号', valueType: 'string', order: 0, width: 18 }],
  }
  assert.throws(() => buildOfficeMessagePayload({ ...draft, fileNameTemplate: '../sales.xlsx' }), /Excel 文件名模板/)
  assert.throws(() => buildOfficeMessagePayload({ ...draft, fileNameTemplate: 'sales_{{unknown}}.xlsx' }), /Excel 文件名模板/)
  assert.throws(() => buildOfficeMessagePayload({ ...draft, fileNameTemplate: 'sales\treport.xlsx' }), /Excel 文件名模板/)
  assert.throws(() => buildOfficeMessagePayload({ ...draft, fileNameTemplate: `${'销'.repeat(84)}.xlsx` }), /Excel 文件名模板/)
})
