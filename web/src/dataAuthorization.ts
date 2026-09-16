export type DataPermissionCode = 'weather.read' | 'bojun.order.read' | 'business_overview.read'
export type DataPermissionStatus = 'ACTIVE' | 'EXPIRED' | 'NOT_GRANTED'

export type DataAuthorizationPermission = {
  permission: DataPermissionCode
  label: string
  scope: string
  status: DataPermissionStatus
  permanent: boolean
  expiresAt: string | null
}

export type DataAuthorizationGrantPayload =
  | { permission: DataPermissionCode; permanent: true }
  | { permission: DataPermissionCode; expiresAt: string }

export type DataAuthorizationAccount = {
  id: number
  account: string
  nickname: string
  credentialStatus: 'ACTIVE' | 'REVOKED'
  tokenPrefix: string
  issuedAt: string | null
  permissions: DataAuthorizationPermission[]
  createdAt: string
}

export type DataAuthorizationAudit = {
  id: number
  targetUserId: number
  targetAccount: string
  permission: string
  action: string
  oldExpiresAt: string | null
  newExpiresAt: string | null
  actorUserId: number
  reason: string
  createdAt: string
}

export type CursorPagination = {
  pageSize: number
  nextBeforeId: number
  hasMore: boolean
}

export type DataAuthorizationAuditAction =
  | 'ACCOUNT_CREATE'
  | 'GRANT'
  | 'RENEW'
  | 'REVOKE'
  | 'TOKEN_REISSUE'

export type DataAuthorizationAuditQuery = {
  targetUserId: number
  permission: string
  action: DataAuthorizationAuditAction | ''
  startTime: string
  endTime: string
  beforeId: number
  pageSize: number
}

export const dataAuthorizationAuditActions: readonly DataAuthorizationAuditAction[] = [
  'ACCOUNT_CREATE',
  'GRANT',
  'RENEW',
  'REVOKE',
  'TOKEN_REISSUE',
]

type JsonRecord = Record<string, unknown>

function record(value: unknown): JsonRecord | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as JsonRecord : null
}

function envelopeData(payload: unknown): JsonRecord | null {
  return record(record(payload)?.data)
}

function stringValue(value: unknown, fallback = '') {
  return typeof value === 'string' ? value : fallback
}

function positiveInteger(value: unknown) {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : 0
}

function nullableString(value: unknown) {
  return typeof value === 'string' && value ? value : null
}

function parsePermission(value: unknown): DataAuthorizationPermission | null {
  const item = record(value)
  if (!item || (item.permission !== 'weather.read' && item.permission !== 'bojun.order.read' && item.permission !== 'business_overview.read')) return null
  if (item.status !== 'ACTIVE' && item.status !== 'EXPIRED' && item.status !== 'NOT_GRANTED') return null
  return {
    permission: item.permission,
    label: stringValue(item.label, item.permission),
    scope: stringValue(item.scope, '全模块数据'),
    status: item.status,
    permanent: item.permanent === true,
    expiresAt: nullableString(item.expiresAt),
  }
}

export function parseDataAuthorizationAccount(value: unknown): DataAuthorizationAccount | null {
  const item = record(value)
  const id = positiveInteger(item?.id)
  if (!item || !id || typeof item.account !== 'string') return null
  const credentialStatus = item.credentialStatus === 'ACTIVE' ? 'ACTIVE' : 'REVOKED'
  const permissions = Array.isArray(item.permissions) ? item.permissions.map(parsePermission).filter((entry): entry is DataAuthorizationPermission => Boolean(entry)) : []
  return {
    id,
    account: item.account,
    nickname: stringValue(item.nickname),
    credentialStatus,
    tokenPrefix: stringValue(item.tokenPrefix),
    issuedAt: nullableString(item.issuedAt),
    permissions,
    createdAt: stringValue(item.createdAt),
  }
}

function parsePagination(value: unknown): CursorPagination {
  const item = record(value)
  return {
    pageSize: positiveInteger(item?.pageSize) || 20,
    nextBeforeId: positiveInteger(item?.nextBeforeId),
    hasMore: item?.hasMore === true,
  }
}

export function parseDataAuthorizationAccounts(payload: unknown) {
  const data = envelopeData(payload)
  const accounts = Array.isArray(data?.accounts)
    ? data.accounts.map(parseDataAuthorizationAccount).filter((entry): entry is DataAuthorizationAccount => Boolean(entry))
    : []
  return { accounts, pagination: parsePagination(data?.pagination) }
}

export function parseDataAuthorizationAudits(payload: unknown) {
  const data = envelopeData(payload)
  const audits: DataAuthorizationAudit[] = []
  if (Array.isArray(data?.audits)) {
    for (const value of data.audits) {
      const item = record(value)
      const id = positiveInteger(item?.id)
      const targetUserId = positiveInteger(item?.targetUserId)
      if (!item || !id || !targetUserId) continue
      audits.push({
        id,
        targetUserId,
        targetAccount: stringValue(item.targetAccount),
        permission: stringValue(item.permission),
        action: stringValue(item.action),
        oldExpiresAt: nullableString(item.oldExpiresAt),
        newExpiresAt: nullableString(item.newExpiresAt),
        actorUserId: positiveInteger(item.actorUserId),
        reason: stringValue(item.reason),
        createdAt: stringValue(item.createdAt),
      })
    }
  }
  return { audits, pagination: parsePagination(data?.pagination) }
}

/** Builds the exact POST body accepted by the cursor-based audit query endpoint. */
export function buildDataAuthorizationAuditQuery(input: Partial<DataAuthorizationAuditQuery>): DataAuthorizationAuditQuery {
  const action = dataAuthorizationAuditActions.includes(input.action as DataAuthorizationAuditAction)
    ? input.action as DataAuthorizationAuditAction
    : ''
  const positive = (value: unknown) => typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : 0
  return {
    targetUserId: positive(input.targetUserId),
    permission: typeof input.permission === 'string' ? input.permission : '',
    action,
    startTime: authorizationExpiryISO(input.startTime ?? ''),
    endTime: authorizationExpiryISO(input.endTime ?? ''),
    beforeId: positive(input.beforeId),
    pageSize: Math.min(100, positive(input.pageSize) || 20),
  }
}

export function parseCreatedAuthorization(payload: unknown) {
  const data = envelopeData(payload)
  return {
    account: parseDataAuthorizationAccount(data?.account),
    token: stringValue(data?.token),
    oneTimeTokenAvailable: data?.oneTimeTokenAvailable === true,
    replayed: data?.replayed === true,
  }
}

export function dataAuthorizationMessage(payload: unknown, fallback: string) {
  const message = record(payload)?.msg
  return typeof message === 'string' && message.trim() ? message : fallback
}

export function defaultAuthorizationExpiry(now = new Date()) {
  const expiry = new Date(now.getTime() + 30 * 24 * 60 * 60 * 1000)
  const local = new Date(expiry.getTime() - expiry.getTimezoneOffset() * 60 * 1000)
  return local.toISOString().slice(0, 16)
}

export function authorizationExpiryISO(localDateTime: string) {
  const value = new Date(localDateTime)
  return Number.isNaN(value.getTime()) ? '' : value.toISOString()
}

export function buildDataAuthorizationGrant(
  permission: DataPermissionCode,
  localDateTime: string,
  permanent: boolean,
): DataAuthorizationGrantPayload {
  if (permanent) return { permission, permanent: true }
  return { permission, expiresAt: authorizationExpiryISO(localDateTime) }
}

export function newAuthorizationIdempotencyKey(scope: string) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `${scope}:${random}`
}
