export type AccessAccountRole = { code: string; name: string }

export type AccessRole = {
  id: number
  code: string
  name: string
  description: string
  status: string
  isSystem: boolean
  isSuper: boolean
  permissions: string[]
}

export type AccessPermission = {
  code: string
  name: string
  module: string
  description: string
  riskLevel: string
  parentCode?: string
  level?: number
}

export function accessPermissionHierarchyLabel(permission: AccessPermission, catalog: readonly AccessPermission[], assignedCodes: readonly string[] = []): string {
  if (!permission.parentCode) return permission.level === 1 ? '一级权限 · 自动覆盖下属全部二级接口' : ''
  const parentName = catalog.find(item => item.code === permission.parentCode)?.name ?? permission.parentCode
  const hierarchy = `二级权限 · 所属${parentName}`
  if (!assignedCodes.includes(permission.parentCode)) return hierarchy
  return `${hierarchy} · 当前由一级权限覆盖${assignedCodes.includes(permission.code) ? '，同时独立授权' : ''}`
}

export type RoleCatalogStatus = 'idle' | 'loading' | 'ready' | 'error'

export function accessManagementCapabilities(permissions: readonly string[]) {
  return {
    canAccountRead: permissions.includes('system.account.read'),
    canAccountManage: permissions.includes('system.account.manage'),
    canRoleRead: permissions.includes('system.role.read'),
    canRoleManage: permissions.includes('system.role.manage'),
    canAuditRead: permissions.includes('system.audit.read'),
  }
}

export function canReplaceAccountRoles(status: RoleCatalogStatus) {
  return status === 'ready'
}

export function updateRoleSelection(current: readonly number[], roleID: number, checked: boolean): number[] {
  return checked ? [...new Set([...current, roleID])] : current.filter((id) => id !== roleID)
}

export type AccessMallScopeRequest = {
  mallScopeMode: 'ALL' | 'SELECTED'
  mallIds: number[]
}

export type AccessMall = {
  id: number
  mallCode: string
  nameCn: string
}

export type AccessMallCatalog = {
  items: AccessMall[]
  nextAfterId: number
}

export type AccessReportAction = 'QUERY' | 'EXPORT'

export type AccessAccountReportCategory = {
  category: string
  reportCount: number
  configured: boolean
  lockVersion: number
  directActions: AccessReportAction[]
  inheritedActions: AccessReportAction[]
  effectiveActions: AccessReportAction[]
}

export function accessAccountReportCategoriesPath(accountID: number) {
  if (!Number.isSafeInteger(accountID) || accountID <= 0) throw new Error('invalid access account id')
  return `/v1/access/accounts/${accountID}/report-categories`
}

export function parseAccessAccountReportCategories(payload: unknown): AccessAccountReportCategory[] | null {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null
  const data = (payload as Record<string, unknown>).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  const items = (data as Record<string, unknown>).items
  if (!Array.isArray(items)) return null
  const result: AccessAccountReportCategory[] = []
  for (const value of items) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    const item = value as Record<string, unknown>
    const directActions = parseAccessReportActions(item.directActions)
    const inheritedActions = parseAccessReportActions(item.inheritedActions)
    const effectiveActions = parseAccessReportActions(item.effectiveActions)
    if (typeof item.category !== 'string' || !item.category.trim() || !Number.isSafeInteger(item.reportCount) || Number(item.reportCount) < 0 ||
      typeof item.configured !== 'boolean' || !Number.isSafeInteger(item.lockVersion) || Number(item.lockVersion) < 0 ||
      item.configured !== (Number(item.lockVersion) > 0) || !directActions || !inheritedActions || !effectiveActions) return null
    result.push({
      category: item.category.trim(), reportCount: Number(item.reportCount), configured: item.configured,
      lockVersion: Number(item.lockVersion), directActions, inheritedActions, effectiveActions,
    })
  }
  return result
}

function parseAccessReportActions(value: unknown): AccessReportAction[] | null {
  if (!Array.isArray(value)) return null
  const result: AccessReportAction[] = []
  for (const action of value) {
    if (action !== 'QUERY' && action !== 'EXPORT') return null
    if (!result.includes(action)) result.push(action)
  }
  return result
}

export function accessMallCatalogPath(afterID = 0, limit = 200) {
  if (!Number.isSafeInteger(afterID) || afterID < 0 || !Number.isSafeInteger(limit) || limit < 1 || limit > 200) {
    throw new Error('invalid access mall pagination')
  }
  const query = new URLSearchParams({ limit: String(limit) })
  if (afterID > 0) query.set('afterId', String(afterID))
  return `/v1/access/malls?${query.toString()}`
}

export function parseAccessMallCatalog(payload: unknown): AccessMallCatalog | null {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null
  const data = (payload as Record<string, unknown>).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  const record = data as Record<string, unknown>
  if (!Array.isArray(record.items) || !Number.isSafeInteger(record.nextAfterId) || Number(record.nextAfterId) < 0) return null
  const items: AccessMall[] = []
  for (const value of record.items) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    const mall = value as Record<string, unknown>
    if (!Number.isSafeInteger(mall.id) || Number(mall.id) < 1 || typeof mall.mallCode !== 'string' || !mall.mallCode.trim() ||
      typeof mall.nameCn !== 'string' || !mall.nameCn.trim()) return null
    items.push({ id: Number(mall.id), mallCode: mall.mallCode.trim(), nameCn: mall.nameCn.trim() })
  }
  return { items, nextAfterId: Number(record.nextAfterId) }
}

export function mergeAccessMalls(current: readonly AccessMall[], incoming: readonly AccessMall[]): AccessMall[] {
  const byID = new Map(current.map((mall) => [mall.id, mall]))
  for (const mall of incoming) byID.set(mall.id, mall)
  return [...byID.values()].sort((left, right) => left.id - right.id)
}

export function accessMallScopeRequest(mode: string, rawMallIDs: readonly number[]): AccessMallScopeRequest | null {
  if (mode === 'ALL') return { mallScopeMode: 'ALL', mallIds: [] }
  if (mode !== 'SELECTED') return null
  const mallIds = [...new Set(rawMallIDs.filter((id) => Number.isSafeInteger(id) && id > 0))]
  return mallIds.length > 0 ? { mallScopeMode: 'SELECTED', mallIds } : null
}

export type AccessAccount = {
  id: number
  account: string
  phone: string
  nickname: string
  status: string
  mallScopeMode: string
  roles: AccessAccountRole[]
  mallIds: number[]
}

export function parseAccessAccounts(payload: unknown): AccessAccount[] {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return []
  const data = (payload as Record<string, unknown>).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return []
  const accounts = (data as Record<string, unknown>).accounts
  if (!Array.isArray(accounts)) return []
  return accounts.flatMap((value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return []
    const account = value as Record<string, unknown>
    if (!Number.isSafeInteger(account.id) || Number(account.id) <= 0 || typeof account.account !== 'string') return []
    const roles = Array.isArray(account.roles) ? account.roles.flatMap((role) => {
      if (!role || typeof role !== 'object' || Array.isArray(role)) return []
      const item = role as Record<string, unknown>
      return typeof item.code === 'string' && typeof item.name === 'string' ? [{ code: item.code, name: item.name }] : []
    }) : []
    const mallIds = Array.isArray(account.mallIds)
      ? account.mallIds.filter((id): id is number => Number.isSafeInteger(id) && Number(id) > 0)
      : []
    return [{
      id: Number(account.id),
      account: account.account,
      phone: typeof account.phone === 'string' ? account.phone : '',
      nickname: typeof account.nickname === 'string' ? account.nickname : account.account,
      status: typeof account.status === 'string' ? account.status : '',
      mallScopeMode: typeof account.mallScopeMode === 'string' ? account.mallScopeMode : '',
      roles,
      mallIds,
    }]
  })
}

export function parseCreatedAccessRole(payload: unknown): AccessRole | null {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null
  const data = (payload as Record<string, unknown>).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  const role = (data as Record<string, unknown>).role
  if (!role || typeof role !== 'object' || Array.isArray(role)) return null
  const item = role as Record<string, unknown>
  if (!Number.isSafeInteger(item.id) || Number(item.id) <= 0 || typeof item.code !== 'string' || typeof item.name !== 'string' ||
    typeof item.description !== 'string' || typeof item.status !== 'string' || typeof item.isSystem !== 'boolean' || typeof item.isSuper !== 'boolean' ||
    !Array.isArray(item.permissions) || !item.permissions.every((permission) => typeof permission === 'string')) return null
  return {
    id: Number(item.id),
    code: item.code,
    name: item.name,
    description: item.description,
    status: item.status,
    isSystem: item.isSystem,
    isSuper: item.isSuper,
    permissions: item.permissions as string[],
  }
}

export function accountRoleIDs(account: AccessAccount | undefined, roles: readonly AccessRole[]): number[] {
  if (!account) return []
  const assignedCodes = new Set(account.roles.map((role) => role.code))
  return roles.filter((role) => assignedCodes.has(role.code)).map((role) => role.id)
}
