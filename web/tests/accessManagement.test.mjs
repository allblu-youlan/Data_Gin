import assert from 'node:assert/strict'
import test from 'node:test'
import * as accessManagement from '../.test-dist/accessManagement.js'

import {
  accountRoleIDs,
  accessMallCatalogPath,
  accessMallScopeRequest,
  accessAccountReportCategoriesPath,
  accessManagementCapabilities,
  canReplaceAccountRoles,
  mergeAccessMalls,
  parseAccessMallCatalog,
  parseAccessAccounts,
  parseAccessAccountReportCategories,
  parseCreatedAccessRole,
  updateRoleSelection,
} from '../.test-dist/accessManagement.js'

test('describes monitoring role hierarchy and saved inheritance from the dynamic catalog', () => {
  const parent = { code: 'monitoring.api.read', name: '监控数据', module: 'monitoring', description: '全部监控查询', riskLevel: 'LOW', level: 1 }
  const children = ['hourly', 'daily', 'area'].map(kind => ({
    code: `monitoring.${kind}.read`, name: kind, module: 'monitoring', description: '监控查询', riskLevel: 'LOW', level: 2, parentCode: parent.code,
  }))
  const catalog = [parent, ...children]
  assert.equal(accessManagement.accessPermissionHierarchyLabel(parent, catalog), '一级权限 · 自动覆盖下属全部二级接口')
  for (const child of children) {
    assert.equal(accessManagement.accessPermissionHierarchyLabel(child, catalog, [parent.code]), '二级权限 · 所属监控数据 · 当前由一级权限覆盖')
    assert.equal(accessManagement.accessPermissionHierarchyLabel(child, catalog, [child.code]), '二级权限 · 所属监控数据')
  }
  assert.equal(accessManagement.accessPermissionHierarchyLabel(children[0], catalog, [parent.code, children[0].code]), '二级权限 · 所属监控数据 · 当前由一级权限覆盖，同时独立授权')
  assert.equal(accessManagement.accessPermissionHierarchyLabel({ ...children[0], parentCode: undefined, level: undefined }, catalog, [parent.code]), '')
})

test('keeps role permission hierarchy dynamic for future API groups', () => {
  const parent = { code: 'future.api.read', name: '新平台', module: 'future', description: '', riskLevel: 'LOW', level: 1 }
  const child = { code: 'future.query.read', name: '查询', module: 'future', description: '', riskLevel: 'LOW', parentCode: parent.code, level: 2 }
  assert.equal(accessManagement.accessPermissionHierarchyLabel(child, [parent, child], [parent.code]), '二级权限 · 所属新平台 · 当前由一级权限覆盖')
})

test('parses account report category permissions', () => {
  assert.equal(accessAccountReportCategoriesPath(7), '/v1/access/accounts/7/report-categories')
  assert.throws(() => accessAccountReportCategoriesPath(0), /invalid access account id/)
  assert.deepEqual(parseAccessAccountReportCategories({ data: { items: [{
    category: ' 财务 ', reportCount: 3, configured: true, lockVersion: 2,
    directActions: ['QUERY'], inheritedActions: ['QUERY', 'EXPORT'], effectiveActions: ['QUERY', 'EXPORT'],
  }] } }), [{
    category: '财务', reportCount: 3, configured: true, lockVersion: 2,
    directActions: ['QUERY'], inheritedActions: ['QUERY', 'EXPORT'], effectiveActions: ['QUERY', 'EXPORT'],
  }])
  assert.equal(parseAccessAccountReportCategories({ data: { items: [{ category: '财务', reportCount: 1, configured: false, lockVersion: 2, directActions: [], inheritedActions: [], effectiveActions: [] }] } }), null)
  assert.equal(parseAccessAccountReportCategories({ data: { items: [{ category: '财务', reportCount: 1, configured: true, lockVersion: 1, directActions: ['DELETE'], inheritedActions: [], effectiveActions: [] }] } }), null)
})

test('builds a valid account mall scope request', () => {
  assert.deepEqual(accessMallScopeRequest('ALL', [7, 8]), { mallScopeMode: 'ALL', mallIds: [] })
  assert.deepEqual(accessMallScopeRequest('SELECTED', [9, 2, 9]), { mallScopeMode: 'SELECTED', mallIds: [9, 2] })
  assert.equal(accessMallScopeRequest('SELECTED', []), null)
  assert.equal(accessMallScopeRequest('SELECTED', [0, Number.NaN]), null)
})

test('parses and merges the grantable mall catalog', () => {
  assert.equal(accessMallCatalogPath(), '/v1/access/malls?limit=200')
  assert.equal(accessMallCatalogPath(8, 25), '/v1/access/malls?limit=25&afterId=8')
  assert.throws(() => accessMallCatalogPath(0, 201), /invalid access mall pagination/)
  const page = parseAccessMallCatalog({ data: { items: [
    { id: 8, mallCode: 'SH-008', nameCn: ' 浦东商场 ' },
    { id: 3, mallCode: 'SH-003', nameCn: '徐汇商场' },
  ], nextAfterId: 8 } })
  assert.deepEqual(page, { items: [
    { id: 8, mallCode: 'SH-008', nameCn: '浦东商场' },
    { id: 3, mallCode: 'SH-003', nameCn: '徐汇商场' },
  ], nextAfterId: 8 })
  assert.deepEqual(mergeAccessMalls([{ id: 3, mallCode: 'OLD', nameCn: '旧名称' }], page.items), [
    { id: 3, mallCode: 'SH-003', nameCn: '徐汇商场' },
    { id: 8, mallCode: 'SH-008', nameCn: '浦东商场' },
  ])
  assert.equal(parseAccessMallCatalog({ data: { items: [{ id: 0 }], nextAfterId: 0 } }), null)
})

test('normalizes null account roles and mall ids to empty arrays', () => {
  const accounts = parseAccessAccounts({ data: { accounts: [{
    id: 1,
    account: 'admin',
    phone: '',
    nickname: '管理员',
    status: 'ACTIVE',
    mallScopeMode: 'ALL',
    roles: null,
    mallIds: null,
  }] } })

  assert.equal(accounts.length, 1)
  assert.deepEqual(accounts[0].roles, [])
  assert.deepEqual(accounts[0].mallIds, [])
})

test('keeps valid account roles and positive mall ids only', () => {
  const accounts = parseAccessAccounts({ data: { accounts: [{
    id: 7,
    account: 'operator',
    phone: '138****8000',
    nickname: '运营人员',
    status: 'ACTIVE',
    mallScopeMode: 'SELECTED',
    roles: [{ code: 'operator', name: '操作员' }, null],
    mallIds: [9, 0, '2'],
  }, null] } })

  assert.deepEqual(accounts, [{
    id: 7,
    account: 'operator',
    phone: '138****8000',
    nickname: '运营人员',
    status: 'ACTIVE',
    mallScopeMode: 'SELECTED',
    roles: [{ code: 'operator', name: '操作员' }],
    mallIds: [9],
  }])
})

test('parses a created custom role from the mutation envelope', () => {
  assert.deepEqual(parseCreatedAccessRole({ data: { role: {
    id: 12,
    code: 'offline_sales_viewer',
    name: '线下销售查看',
    description: '查看线下销售数据',
    status: 'ACTIVE',
    isSystem: false,
    isSuper: false,
    permissions: ['business_overview.read', 'report.read'],
  } } }), {
    id: 12,
    code: 'offline_sales_viewer',
    name: '线下销售查看',
    description: '查看线下销售数据',
    status: 'ACTIVE',
    isSystem: false,
    isSuper: false,
    permissions: ['business_overview.read', 'report.read'],
  })
  assert.equal(parseCreatedAccessRole({ data: { role: { id: 0 } } }), null)
  assert.equal(parseCreatedAccessRole({ data: { role: { id: 12, code: 'custom', name: '自定义', description: '', status: 'ACTIVE', isSystem: false, isSuper: false, permissions: ['report.read', null] } } }), null)
})

test('maps assigned account role codes to current role ids', () => {
  const account = parseAccessAccounts({ data: { accounts: [{
    id: 7,
    account: 'offline_sales',
    roles: [{ code: 'offline_sales_viewer', name: '线下销售查看' }],
  }] } })[0]
  assert.deepEqual(accountRoleIDs(account, [
    { id: 4, code: 'viewer', name: '只读', description: '', status: 'ACTIVE', isSystem: true, isSuper: false, permissions: [] },
    { id: 12, code: 'offline_sales_viewer', name: '线下销售查看', description: '', status: 'ACTIVE', isSystem: false, isSuper: false, permissions: ['business_overview.read'] },
  ]), [12])
})

test('checks account and role capabilities without implicit manage-to-read expansion', () => {
  assert.deepEqual(accessManagementCapabilities(['system.role.manage']), {
    canAccountRead: false,
    canAccountManage: false,
    canRoleRead: false,
    canRoleManage: true,
    canAuditRead: false,
  })
})

test('only allows replacing roles after the role catalog is ready', () => {
  assert.equal(canReplaceAccountRoles('idle'), false)
  assert.equal(canReplaceAccountRoles('loading'), false)
  assert.equal(canReplaceAccountRoles('error'), false)
  assert.equal(canReplaceAccountRoles('ready'), true)
})

test('updates role selection without dropping existing choices or adding duplicates', () => {
  assert.deepEqual(updateRoleSelection([4, 7], 12, true), [4, 7, 12])
  assert.deepEqual(updateRoleSelection([4, 7, 12], 12, true), [4, 7, 12])
  assert.deepEqual(updateRoleSelection([4, 7, 12], 7, false), [4, 12])
})
