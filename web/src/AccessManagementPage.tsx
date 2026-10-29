import { formatShanghaiDateTime } from './dateTime.js'
import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { Plus, RefreshCcw, ShieldCheck } from 'lucide-react'
import { AccessAccountsPanel } from './AccessAccountsPanel'
import { AccessRoleCreateDrawer, type AccessRoleCreateInput } from './AccessRoleCreateDrawer'
import { DataAuthorizationPage } from './DataAuthorizationPage'
import {
  accessManagementCapabilities,
  type AccessPermission as Permission,
  type AccessRole as Role,
} from './accessManagement'
import { DataTable, Dialog, FeedbackState, FilterToolbar, MasterDetail, PageCanvas, PageHeader, Section, StatusTag } from './ui'
import styles from './AccessManagementPage.module.css'

type ApiResult = { ok: boolean; status: number; data: unknown }
type ApiClient = (path: string, options?: { method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'; body?: unknown; headers?: Record<string, string>; showResult?: boolean; silentLoading?: boolean; signal?: AbortSignal }) => Promise<ApiResult>
type Tab = 'accounts' | 'roles' | 'open-api' | 'audits'
type Audit = { id: number; actorUserId: number; action: string; targetType: string; targetId: number; reason: string; createdAt: string }

const headers = () => ({ 'Idempotency-Key': `access:${globalThis.crypto.randomUUID()}` })
const tabLabels: Record<Tab, string> = { accounts: '控制台账号', roles: '角色管理', 'open-api': '开放 API', audits: '权限审计' }

function envelopeValue(payload: unknown): unknown {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null
  return (payload as Record<string, unknown>).data
}

function list<T>(payload: unknown, key?: string): T[] {
  const data = envelopeValue(payload)
  const result = key && data && typeof data === 'object' && !Array.isArray(data) ? (data as Record<string, unknown>)[key] : data
  return Array.isArray(result) ? result as T[] : []
}

export function AccessManagementPage({ client, permissions }: { client: ApiClient; permissions: string[] }) {
  const { canAccountManage, canAccountRead, canAuditRead, canRoleManage, canRoleRead } = accessManagementCapabilities(permissions)
  const availableTabs = useMemo(() => [
    ...(canAccountRead ? ['accounts' as const] : []),
    ...(canRoleRead ? ['roles' as const] : []),
    ...(canAccountManage ? ['open-api' as const] : []),
    ...(canAuditRead ? ['audits' as const] : []),
  ], [canAccountManage, canAccountRead, canAuditRead, canRoleRead])
  const [tab, setTab] = useState<Tab>(availableTabs[0] ?? 'accounts')

  useEffect(() => {
    if (!availableTabs.includes(tab)) setTab(availableTabs[0] ?? 'accounts')
  }, [availableTabs, tab])

  if (availableTabs.length === 0) {
    return <PageCanvas className={styles.page}>
      <PageHeader eyebrow="ACCESS CONTROL" title="账号与权限" description="在一个工作画布中管理控制台账号、角色权限矩阵、开放 API 与变更审计。" />
      <FeedbackState kind="error" title="当前账号无权查看账号与权限" description="请联系管理员补充系统账号、角色或审计查看权限。" />
    </PageCanvas>
  }

  return <PageCanvas className={styles.page}>
    <PageHeader eyebrow="ACCESS CONTROL" title="账号与权限" description="在一个工作画布中管理控制台账号、角色权限矩阵、开放 API 与变更审计。" />
    <nav className={styles.tabs} aria-label="账号与权限模块">
      {availableTabs.map((item) => <button type="button" key={item} className={tab === item ? styles.activeTab : undefined} aria-current={tab === item ? 'page' : undefined} onClick={() => setTab(item)}>{tabLabels[item]}</button>)}
    </nav>
    {tab === 'accounts' && <AccessAccountsPanel client={client} canManage={canAccountManage} canManageRoles={canRoleManage} canReadRoles={canRoleRead} canManageReportCategories={canAccountManage && permissions.includes('report.manage')} grantablePermissionCodes={permissions} />}
    {tab === 'roles' && <RolePanel client={client} canManage={canRoleManage} grantablePermissionCodes={permissions} />}
    {tab === 'open-api' && <DataAuthorizationPage client={client} />}
    {tab === 'audits' && <AuditPanel client={client} />}
  </PageCanvas>
}

function RolePanel({ client, canManage, grantablePermissionCodes }: { client: ApiClient; canManage: boolean; grantablePermissionCodes: string[] }) {
  const [roles, setRoles] = useState<Role[]>([])
  const [catalog, setCatalog] = useState<Permission[]>([])
  const [selectedID, setSelectedID] = useState(0)
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [deletePending, setDeletePending] = useState(false)
  const selected = roles.find((role) => role.id === selectedID) ?? roles[0]
  const load = useCallback(async () => { setBusy(true); const [rolesResponse, permissionResponse] = await Promise.all([client('/v1/access/roles', { method: 'GET', showResult: false, silentLoading: true }), client('/v1/access/permissions', { method: 'GET', showResult: false, silentLoading: true })]); if (rolesResponse.ok) { const next = list<Role>(rolesResponse.data); setRoles(next); setSelectedID((current) => next.some((role) => role.id === current) ? current : next[0]?.id ?? 0) }; if (permissionResponse.ok) setCatalog(list<Permission>(permissionResponse.data)); setBusy(false) }, [client])
  useEffect(() => { void load() }, [load])
  const groups = useMemo(() => { const grouped: Record<string, Permission[]> = {}; for (const permission of catalog) (grouped[permission.module || '其他'] ??= []).push(permission); return Object.entries(grouped) }, [catalog])
  const grantableCatalog = useMemo(() => { const grantable = new Set(grantablePermissionCodes); return catalog.filter((permission) => grantable.has(permission.code)) }, [catalog, grantablePermissionCodes])
  async function savePermissions(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (!selected) return; const form = new FormData(event.currentTarget); setBusy(true); const response = await client(`/v1/access/roles/${selected.id}/permissions`, { method: 'PUT', headers: headers(), body: { permissions: form.getAll('permissions'), reason: value(form, 'reason') }, showResult: false, silentLoading: true }); setMessage(response.ok ? '角色权限已保存，相关账号旧会话已失效。' : '角色权限保存失败。'); if (response.ok) await load(); setBusy(false) }
  async function roleMutation(path: string, method: 'POST' | 'PUT' | 'DELETE', body: unknown, success: string) { setBusy(true); const response = await client(path, { method, headers: headers(), body, showResult: false, silentLoading: true }); setMessage(response.ok ? success : '角色操作失败，请检查系统角色保护和当前权限。'); if (response.ok) await load(); setBusy(false); return response.ok }
  async function createRole(input: AccessRoleCreateInput) { setCreateError(''); const success = await roleMutation('/v1/access/roles', 'POST', input, '自定义角色已创建。'); if (success) setCreating(false); else setCreateError('角色创建失败，请检查角色代码、权限范围和当前权限。') }

  return <Section title="角色管理" description="维护自定义角色状态和全量权限矩阵；系统角色保持只读。" flush>
    <MasterDetail className={styles.masterDetail} masterWidth={312} masterLabel="角色目录" detailLabel="角色权限详情" master={<div className={styles.masterPane}><div className={styles.masterTitle}><ShieldCheck aria-hidden="true" /><strong>角色目录</strong></div>{canManage && <button className={styles.fullPrimary} type="button" onClick={() => { setCreateError(''); setCreating(true) }}><Plus aria-hidden="true" />创建角色</button>}<div className={styles.list}>{roles.map((role) => <button type="button" className={selected?.id === role.id ? styles.selectedItem : undefined} key={role.id} onClick={() => setSelectedID(role.id)}><span><strong>{role.name}</strong><small>{role.code}</small></span><StatusTag tone={role.isSystem ? 'neutral' : 'success'}>{role.isSystem ? '系统' : '自定义'}</StatusTag></button>)}</div></div>}
      detail={<div className={styles.detailPane}>{message && <p className={styles.message} role="status">{message}</p>}{!selected ? <FeedbackState kind={busy ? 'loading' : 'empty'} title={busy ? '正在加载角色' : '暂无角色'} /> : <><header className={styles.detailHeader}><div><span>角色</span><h3>{selected.name}</h3><p>{selected.description || selected.code}</p></div>{canManage && !selected.isSystem ? <div className={styles.actions}><button type="button" disabled={busy} onClick={() => void roleMutation(`/v1/access/roles/${selected.id}/status`, 'PUT', { status: selected.status === 'ACTIVE' ? 'DISABLED' : 'ACTIVE', reason: '管理员调整角色状态' }, '角色状态已更新。')}>{selected.status === 'ACTIVE' ? '停用' : '启用'}</button><button className={styles.danger} type="button" disabled={busy} onClick={() => setDeletePending(true)}>删除</button></div> : <StatusTag tone="neutral">{selected.status}</StatusTag>}</header><form key={selected.id} className={styles.permissionMatrix} onSubmit={savePermissions}>{groups.map(([module, items]) => <fieldset key={module}><legend>{module}</legend>{items.map((permission) => <label key={permission.code}><input type="checkbox" name="permissions" value={permission.code} defaultChecked={selected.permissions.includes(permission.code)} disabled={!canManage || selected.isSystem} /><span><strong>{permission.name}</strong><small>{permission.code} · {permission.description}</small></span><em>{permission.riskLevel}</em></label>)}</fieldset>)}{canManage && !selected.isSystem && <div className={styles.saveRow}><Field label="权限变更原因"><input name="reason" required /></Field><button className={styles.primary} type="submit" disabled={busy}>保存权限矩阵</button></div>}</form></>}</div>} />
    <AccessRoleCreateDrawer open={creating} busy={busy} catalog={grantableCatalog} error={createError} onClose={() => { setCreateError(''); setCreating(false) }} onSubmit={createRole} />
    <Dialog open={deletePending && Boolean(selected)} role="alertdialog" title="确认删除角色" description={selected ? `确认删除未使用角色“${selected.name}”？删除后无法恢复。` : undefined} closeDisabled={busy} onClose={() => setDeletePending(false)} footer={<><button type="button" disabled={busy} onClick={() => setDeletePending(false)}>取消</button><button className={styles.danger} type="button" disabled={busy} onClick={() => { if (selected) void roleMutation(`/v1/access/roles/${selected.id}`, 'DELETE', { reason: '管理员删除未使用角色' }, '角色已删除。').then((success) => { if (success) setDeletePending(false) }) }}>确认删除</button></>}><p className={styles.dangerNotice}>仅未被账号使用的自定义角色可以删除，系统角色始终受保护。</p></Dialog>
  </Section>
}

function AuditPanel({ client }: { client: ApiClient }) {
  const [audits, setAudits] = useState<Audit[]>([])
  const [action, setAction] = useState('')
  const [targetType, setTargetType] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => { setBusy(true); const query = new URLSearchParams({ pageSize: '100' }); if (action) query.set('action', action); if (targetType) query.set('targetType', targetType); const response = await client(`/v1/access/audits?${query}`, { method: 'GET', showResult: false, silentLoading: true }); if (response.ok) setAudits(list<Audit>(response.data, 'audits')); setBusy(false) }, [action, client, targetType])
  useEffect(() => { void load() }, [load])
  return <Section title="权限审计" description="按动作与对象查询最近 100 条账号和角色变更。" flush><FilterToolbar summary={`${audits.length} 条记录`}><form className={styles.auditFilter} onSubmit={(event) => { event.preventDefault(); void load() }}><label>审计动作<input value={action} onChange={(event) => setAction(event.target.value)} placeholder="例如 ACCOUNT_STATUS" /></label><label>对象类型<select value={targetType} onChange={(event) => setTargetType(event.target.value)}><option value="">全部对象</option><option value="ACCOUNT">账号</option><option value="role">角色</option></select></label><button type="submit" disabled={busy}><RefreshCcw aria-hidden="true" />{busy ? '查询中' : '查询'}</button></form></FilterToolbar>{audits.length === 0 ? <FeedbackState kind={busy ? 'loading' : 'empty'} title={busy ? '正在加载权限审计' : '暂无符合条件的权限审计'} /> : <DataTable scrollLabel="权限审计记录"><thead><tr><th scope="col">时间</th><th scope="col">操作者</th><th scope="col">动作</th><th scope="col">对象</th><th scope="col">原因</th></tr></thead><tbody>{audits.map((audit) => <tr key={audit.id}><td>{formatShanghaiDateTime(audit.createdAt)}</td><td>#{audit.actorUserId}</td><td>{audit.action}</td><td>{audit.targetType} #{audit.targetId}</td><td>{audit.reason}</td></tr>)}</tbody></DataTable>}</Section>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className={styles.field}><span>{label}</span>{children}</label> }
function value(form: FormData, key: string) { const result = form.get(key); return typeof result === 'string' ? result.trim() : '' }
