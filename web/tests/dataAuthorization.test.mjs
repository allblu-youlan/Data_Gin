import test from 'node:test'
import assert from 'node:assert/strict'
import * as dataAuthorization from '../.test-dist/dataAuthorization.js'
import {
  authorizationExpiryISO,
  buildDataAuthorizationGrant,
  buildDataAuthorizationAuditQuery,
  dataAuthorizationMessage,
  defaultAuthorizationExpiry,
  parseCreatedAuthorization,
  parseDataAuthorizationAccounts,
  parseDataAuthorizationAudits,
  parseDataPermissionGroups,
} from '../.test-dist/dataAuthorization.js'

const monitoringGroup = {
  code: 'monitoring.api.read', name: '监控数据', description: '查询授权店铺监控数据',
  children: [
    { code: 'monitoring.hourly.read', name: '小时监控查询', description: '查询多日小时数据' },
    { code: 'monitoring.daily.read', name: '每日监控汇总', description: '按日汇总小时计数' },
    { code: 'monitoring.area.read', name: '区域监控查询', description: '查询单日区域数据' },
  ],
}

test('loads the dynamic monitoring catalog with an explicit GET for the console client', async () => {
  const signal = new AbortController().signal
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    if (options?.method !== 'GET') return { ok: false, status: 400, data: { msg: '请求方法未指定' } }
    return { ok: true, status: 200, data: { data: { groups: [monitoringGroup] } } }
  }
  assert.deepEqual(await dataAuthorization.loadDataPermissionGroups(client, signal), [monitoringGroup])
  assert.deepEqual(requests, [{ path: '/v1/data-authorizations/permissions', options: {
    method: 'GET', showResult: false, silentLoading: true, signal,
  } }])
})

test('reports catalog failures instead of treating them as an empty permission catalog', async () => {
  for (const response of [{ ok: false, data: { msg: '禁止访问' } }, { ok: true, data: { data: { groups: [] } } }]) {
    await assert.rejects(() => dataAuthorization.loadDataPermissionGroups(async () => response), /权限目录加载失败/)
  }
})

test('uses monitoring parent and child catalog names in permission and audit labels', () => {
  for (const [code, expected] of [
    ['monitoring.api.read', '监控数据'], ['monitoring.hourly.read', '小时监控查询'],
    ['monitoring.daily.read', '每日监控汇总'], ['monitoring.area.read', '区域监控查询'],
    ['open_api.account', '开放接口账号'], ['open_api.credential', '访问凭证'], ['unknown.read', 'unknown.read'],
  ]) assert.equal(dataAuthorization.dataPermissionLabel(code, [monitoringGroup]), expected)
})

test('preserves backend monitoring inheritance and independent child status', () => {
  const parsed = parseDataAuthorizationAccounts({ data: { accounts: [{
    id: 14, account: 'monitoring-partner', permissions: [
      { permission: 'monitoring.api.read', status: 'ACTIVE', permanent: true, level: 1 },
      { permission: 'monitoring.hourly.read', status: 'ACTIVE', permanent: true, level: 2, parentCode: 'monitoring.api.read', inheritedFrom: 'monitoring.api.read' },
      { permission: 'monitoring.daily.read', status: 'ACTIVE', permanent: true, level: 2, parentCode: 'monitoring.api.read', inheritedFrom: 'monitoring.api.read' },
      { permission: 'monitoring.area.read', status: 'ACTIVE', permanent: true, level: 2, parentCode: 'monitoring.api.read', inheritedFrom: 'monitoring.api.read' },
    ],
  }] } })
  assert.deepEqual(parsed.accounts[0].permissions.map(grant => grant.inheritedFrom), [undefined, 'monitoring.api.read', 'monitoring.api.read', 'monitoring.api.read'])
  const independent = parseDataAuthorizationAccounts({ data: { accounts: [{
    id: 14, account: 'monitoring-partner', permissions: [
      { permission: 'monitoring.api.read', status: 'EXPIRED', permanent: false, expiresAt: '2026-11-01T00:00:00+08:00' },
      { permission: 'monitoring.hourly.read', status: 'ACTIVE', permanent: true, parentCode: 'monitoring.api.read' },
      { permission: 'monitoring.area.read', status: 'NOT_GRANTED', permanent: false, parentCode: 'monitoring.api.read' },
    ],
  }] } })
  assert.deepEqual(independent.accounts[0].permissions.map(grant => [grant.status, grant.inheritedFrom]), [
    ['EXPIRED', undefined], ['ACTIVE', undefined], ['NOT_GRANTED', undefined],
  ])
  assert.deepEqual(buildDataAuthorizationGrant('monitoring.api.read', '', true), { permission: 'monitoring.api.read', permanent: true })
})

test('parses safe account envelope and rejects malformed rows', () => {
  const parsed = parseDataAuthorizationAccounts({ data: {
    accounts: [
      { id: 7, account: 'partner', email: 'owner@example.com', nickname: '合作方', credentialStatus: 'ACTIVE', tokenPrefix: 'dg_open_abcd', issuedAt: '2026-07-28T08:00:00Z', createdAt: '2026-07-28T08:00:00Z', permissions: [{ permission: 'weather.read', label: '天气数据查询', scope: '全模块数据', status: 'ACTIVE', permanent: false, expiresAt: '2026-08-28T08:00:00Z' }, { permission: 'business_overview.read', label: '营业金额查询', scope: '全模块数据', status: 'ACTIVE', permanent: true }] },
      { id: 0, account: 'invalid' },
      { id: 8, account: 'bad-permission', permissions: [{ permission: '', status: 'ACTIVE' }] },
    ],
    pagination: { pageSize: 20, nextBeforeId: 7, hasMore: true },
  } })
  assert.equal(parsed.accounts.length, 2)
  assert.equal(parsed.accounts[0].permissions[0].permission, 'weather.read')
  assert.equal(parsed.accounts[0].permissions[1].permission, 'business_overview.read')
  assert.equal(parsed.accounts[0].permissions[1].permanent, true)
  assert.deepEqual(parsed.accounts[1].permissions, [])
  assert.equal(parsed.pagination.nextBeforeId, 7)
})

test('accepts backend permission groups and inherited child grants without a frontend allowlist', () => {
  const groups = [{ code: 'future.read', name: '新平台', description: '全部查询', children: [{ code: 'future.orders.read', name: '订单', description: '查询订单' }] }]
  assert.deepEqual(parseDataPermissionGroups({ data: { groups } }), groups)
  const parsed = parseDataAuthorizationAccounts({ data: { accounts: [{ id: 1, account: 'partner', permissions: [{ permission: 'future.orders.read', status: 'ACTIVE', inheritedFrom: 'future.read', parentCode: 'future.read', level: 2 }] }] } })
  assert.equal(parsed.accounts[0].permissions[0].inheritedFrom, 'future.read')
  assert.equal(parsed.accounts[0].permissions[0].permission, 'future.orders.read')
  assert.deepEqual(buildDataAuthorizationGrant('future.read', '', true), { permission: 'future.read', permanent: true })
})

test('parses one-time token only from the expected envelope', () => {
  const parsed = parseCreatedAuthorization({ data: { account: { id: 9, account: 'partner' }, token: 'dg_open_secret', oneTimeTokenAvailable: true } })
  assert.equal(parsed.account?.id, 9)
  assert.equal(parsed.token, 'dg_open_secret')
  assert.equal(parsed.oneTimeTokenAvailable, true)
  assert.equal(parseCreatedAuthorization({ token: 'outside' }).token, '')
})

test('parses audit pagination and safe messages', () => {
  const parsed = parseDataAuthorizationAudits({ data: { audits: [{ id: 3, targetUserId: 9, targetAccount: 'partner', permission: 'weather.read', action: 'GRANT', actorUserId: 1, reason: '接入', createdAt: '2026-07-28T08:00:00Z' }], pagination: { hasMore: false } } })
  assert.equal(parsed.audits[0].action, 'GRANT')
  assert.equal(dataAuthorizationMessage({ msg: '禁止访问' }, 'fallback'), '禁止访问')
  assert.equal(dataAuthorizationMessage({ msg: 1 }, 'fallback'), 'fallback')
})

test('builds the exact bounded audit cursor request contract with an RFC3339 time range', () => {
  assert.deepEqual(buildDataAuthorizationAuditQuery({ targetUserId: 9, action: 'REVOKE', startTime: '2026-07-30T09:00:00Z', endTime: '2026-07-30T19:00:00Z', beforeId: 41, pageSize: 30 }), {
    targetUserId: 9,
    permission: '',
    action: 'REVOKE',
    startTime: '2026-07-30T09:00:00.000Z',
    endTime: '2026-07-30T19:00:00.000Z',
    beforeId: 41,
    pageSize: 30,
  })
  assert.deepEqual(buildDataAuthorizationAuditQuery({ targetUserId: -1, action: 'DELETE', beforeId: 0, pageSize: 1000 }), {
    targetUserId: 0,
    permission: '',
    action: '',
    startTime: '',
    endTime: '',
    beforeId: 0,
    pageSize: 100,
  })
})

test('normalizes default and explicit expiry', () => {
  const now = new Date('2026-07-28T00:00:00Z')
  assert.equal(defaultAuthorizationExpiry(now).length, 16)
  assert.match(authorizationExpiryISO('2026-08-27T12:30'), /^2026-08-/)
  assert.equal(authorizationExpiryISO('invalid'), '')
  assert.deepEqual(buildDataAuthorizationGrant('bojun.order.read', '2026-08-27T12:30', false), {
    permission: 'bojun.order.read',
    expiresAt: authorizationExpiryISO('2026-08-27T12:30'),
  })
  assert.deepEqual(buildDataAuthorizationGrant('business_overview.read', '2026-08-27T12:30', true), {
    permission: 'business_overview.read',
    permanent: true,
  })
})

test('preserves independent store and stock grants including revoked and expired states', () => {
  for (const permission of ['bojun.store.read', 'bojun.stock.read']) {
    for (const status of ['ACTIVE', 'EXPIRED', 'NOT_GRANTED']) {
      const parsed = parseDataAuthorizationAccounts({ data: { accounts: [{
        id: 9, account: 'stock-partner', permissions: [{ permission, status, permanent: status === 'ACTIVE' }],
      }] } })
      assert.equal(parsed.accounts[0].permissions[0].permission, permission)
      assert.equal(parsed.accounts[0].permissions[0].status, status)
    }
    assert.deepEqual(buildDataAuthorizationGrant(permission, '', true), { permission, permanent: true })
  }
})

test('preserves each tmall grant and its expiry state independently', () => {
  const permissions = ['tmall.sycm.read', 'tmall.qztg.read', 'tmall.tot.read', 'tmall.orders.read']
  for (const status of ['ACTIVE', 'EXPIRED', 'NOT_GRANTED']) {
    const parsed = parseDataAuthorizationAccounts({ data: { accounts: [{
      id: 12, account: 'tmall-partner', permissions: permissions.map((permission) => ({
        permission, status, permanent: status === 'ACTIVE',
      })),
    }] } })
    assert.deepEqual(parsed.accounts[0].permissions.map((grant) => grant.permission), permissions)
    assert.deepEqual(parsed.accounts[0].permissions.map((grant) => grant.status), [status, status, status, status])
  }
})
