import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { Check, Clipboard, KeyRound, Plus, RefreshCcw, RotateCcw } from 'lucide-react'
import {
  authorizationExpiryISO,
  buildDataAuthorizationGrant,
  buildDataAuthorizationAuditQuery,
  dataAuthorizationAuditActions,
  defaultAuthorizationExpiry,
  newAuthorizationIdempotencyKey,
  parseCreatedAuthorization,
  parseDataAuthorizationAccounts,
  parseDataAuthorizationAudits,
  type CursorPagination,
  type DataAuthorizationAuditAction,
  type DataAuthorizationAccount,
  type DataAuthorizationAudit,
  type DataAuthorizationPermission,
  type DataPermissionCode,
} from './dataAuthorization'
import { DataTable, Dialog, FeedbackState, FilterToolbar, MasterDetail, Section, StatusTag } from './ui'
import styles from './DataAuthorizationPage.module.css'

type ApiResult = { ok: boolean; status: number; data: unknown }
type ApiClient = (path: string, options?: {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  headers?: Record<string, string>
  showResult?: boolean
  silentLoading?: boolean
  signal?: AbortSignal
}) => Promise<ApiResult>

type ActionDialog =
  | { kind: 'grant'; permission: DataPermissionCode }
  | { kind: 'revoke'; permission: DataPermissionCode }
  | { kind: 'reissue' }
  | null

type SubmittedAction = {
  kind: 'grant' | 'revoke' | 'reissue'
  targetID: number
  account: string
  permission?: DataPermissionCode
  expiresAt?: string
  permanent?: boolean
  reason: string
}

const emptyPagination: CursorPagination = { pageSize: 20, nextBeforeId: 0, hasMore: false }
type AuditFilters = { targetUserId: number; action: DataAuthorizationAuditAction | ''; startTime: string; endTime: string }
const emptyAuditFilters: AuditFilters = { targetUserId: 0, action: '', startTime: '', endTime: '' }
const permissionCatalog: Array<{ permission: DataPermissionCode; label: string; description: string }> = [
  { permission: 'weather.read', label: '天气数据查询', description: '可查询全部商场的天气实况、预报、预警和生活指数。' },
  { permission: 'bojun.order.read', label: 'Bojun 订单查询', description: '可按时间、商场和游标分页查询脱敏订单明细。' },
  { permission: 'business_overview.read', label: '营业金额查询', description: '可按日期和门店查询营业金额及支付方式构成。' },
]

export function DataAuthorizationPage({ client }: { client: ApiClient }) {
  const [accounts, setAccounts] = useState<DataAuthorizationAccount[]>([])
  const [pagination, setPagination] = useState(emptyPagination)
  const [selectedID, setSelectedID] = useState<number | null>(null)
  const [keyword, setKeyword] = useState('')
  const [loading, setLoading] = useState(true)
  const [mutating, setMutating] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [audits, setAudits] = useState<DataAuthorizationAudit[]>([])
  const [auditPagination, setAuditPagination] = useState(emptyPagination)
  const [auditFilters, setAuditFilters] = useState<AuditFilters>(emptyAuditFilters)
  const [appliedAuditFilters, setAppliedAuditFilters] = useState<AuditFilters>(emptyAuditFilters)
  const [auditLoading, setAuditLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createPermanent, setCreatePermanent] = useState(false)
  const [actionDialog, setActionDialog] = useState<ActionDialog>(null)
  const [grantPermanent, setGrantPermanent] = useState(false)
  const [pendingAction, setPendingAction] = useState<SubmittedAction | null>(null)
  const [dangerConfirmed, setDangerConfirmed] = useState(false)
  const [oneTimeToken, setOneTimeToken] = useState('')
  const [tokenCopied, setTokenCopied] = useState(false)
  const requestSequence = useRef(0)
  const accountRequestRef = useRef<AbortController | null>(null)
  const auditRequestRef = useRef<AbortController | null>(null)
  const auditRequestSequence = useRef(0)
  const mutationInFlight = useRef(false)

  const selected = accounts.find((account) => account.id === selectedID) ?? accounts[0] ?? null

  const loadAccounts = useCallback(async (options: { append?: boolean; search?: string } = {}) => {
    accountRequestRef.current?.abort()
    const controller = new AbortController()
    accountRequestRef.current = controller
    const sequence = ++requestSequence.current
    setLoading(true)
    setError('')
    const append = options.append === true
    try {
      const response = await client('/v1/data-authorizations/accounts/query', {
        method: 'POST',
        body: { keyword: options.search ?? keyword, beforeId: append ? pagination.nextBeforeId : 0, pageSize: 20 },
        showResult: false,
        silentLoading: true,
        signal: controller.signal,
      })
      if (controller.signal.aborted || sequence !== requestSequence.current) return
      if (!response.ok) {
        setError(response.status === 403 ? '当前登录账号不是可信管理员，无法管理数据授权。' : '开放接口账号加载失败，请稍后重试。')
        return
      }
      const parsed = parseDataAuthorizationAccounts(response.data)
      setAccounts((current) => append ? deduplicateAccounts([...current, ...parsed.accounts]) : parsed.accounts)
      setPagination(parsed.pagination)
      if (!append && parsed.accounts.length > 0) setSelectedID((current) => parsed.accounts.some((account) => account.id === current) ? current : parsed.accounts[0].id)
    } catch {
      if (!controller.signal.aborted && sequence === requestSequence.current) setError('开放接口账号加载失败，请检查网络后重试。')
    } finally {
      if (!controller.signal.aborted && sequence === requestSequence.current) setLoading(false)
    }
  }, [client, keyword, pagination.nextBeforeId])

  const loadAudits = useCallback(async (filters: AuditFilters, options: { append?: boolean; beforeId?: number } = {}) => {
    auditRequestRef.current?.abort()
    const controller = new AbortController()
    auditRequestRef.current = controller
    const sequence = ++auditRequestSequence.current
    const append = options.append === true
    setAuditLoading(true)
    setError('')
    try {
      const response = await client('/v1/data-authorizations/audits/query', {
        method: 'POST',
        body: buildDataAuthorizationAuditQuery({ targetUserId: filters.targetUserId, action: filters.action, startTime: filters.startTime, endTime: filters.endTime, beforeId: append ? options.beforeId : 0, pageSize: 30 }),
        showResult: false,
        silentLoading: true,
        signal: controller.signal,
      })
      if (controller.signal.aborted || sequence !== auditRequestSequence.current) return
      if (!response.ok) {
        setError(response.status === 403 ? '当前登录账号不是可信管理员，无法查看授权审计。' : '授权审计加载失败，请稍后重试。')
        return
      }
      const parsed = parseDataAuthorizationAudits(response.data)
      setAudits((current) => append ? deduplicateAudits([...current, ...parsed.audits]) : parsed.audits)
      setAuditPagination(parsed.pagination)
    } catch {
      if (!controller.signal.aborted && sequence === auditRequestSequence.current) setError('授权审计加载失败，请检查网络后重试。')
    } finally {
      if (!controller.signal.aborted && sequence === auditRequestSequence.current) setAuditLoading(false)
    }
  }, [client])

  useEffect(() => {
    void loadAccounts({ search: '' })
    return () => {
      accountRequestRef.current?.abort()
      requestSequence.current += 1
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const nextFilters = { ...emptyAuditFilters, targetUserId: selected?.id ?? 0 }
    setAuditFilters(nextFilters)
    setAppliedAuditFilters(nextFilters)
    void loadAudits(nextFilters)
  }, [loadAudits, selected?.id])
  useEffect(() => () => auditRequestRef.current?.abort(), [])

  async function refreshAfterMutation(message: string, targetID?: number) {
    setNotice(message)
    if (targetID) setSelectedID(targetID)
    await loadAccounts({ search: keyword })
    const nextFilters = { ...appliedAuditFilters, targetUserId: targetID ?? selected?.id ?? 0 }
    setAuditFilters(nextFilters)
    setAppliedAuditFilters(nextFilters)
    await loadAudits(nextFilters)
  }

  async function createAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!beginMutation()) return
    const form = new FormData(event.currentTarget)
    const permissions = permissionCatalog
      .filter((item) => form.get(item.permission) === 'on')
      .map((item) => buildDataAuthorizationGrant(item.permission, formString(form, 'expiresAt'), createPermanent))
    setError('')
    let response: ApiResult
    try {
      response = await client('/v1/data-authorizations/accounts/create', {
        method: 'POST',
        headers: { 'Idempotency-Key': newAuthorizationIdempotencyKey('account-create') },
        body: { account: formString(form, 'account'), nickname: formString(form, 'nickname'), permissions, reason: formString(form, 'reason') },
        showResult: false,
        silentLoading: true,
      })
    } catch {
      setError('账号开通失败，请检查网络后重试。')
      return
    } finally {
      endMutation()
    }
    if (!response.ok) {
      setError('账号开通失败，请检查字段和授权有效期。')
      return
    }
    const created = parseCreatedAuthorization(response.data)
    setCreateOpen(false)
    if (created.token && created.oneTimeTokenAvailable) setOneTimeToken(created.token)
    else setNotice(created.replayed ? '该开户请求已处理；出于安全原因，Token 不会再次展示。' : '账号已创建。')
    await refreshAfterMutation('开放接口账号已创建。', created.account?.id)
  }

  async function submitAction(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || !actionDialog || mutating) return
    const form = new FormData(event.currentTarget)
    let submittedAction: SubmittedAction
    if (actionDialog.kind === 'grant') {
      submittedAction = {
        kind: 'grant', targetID: selected.id, account: selected.account, permission: actionDialog.permission,
        expiresAt: grantPermanent ? undefined : authorizationExpiryISO(formString(form, 'expiresAt')),
        permanent: grantPermanent,
        reason: formString(form, 'reason'),
      }
    } else if (actionDialog.kind === 'revoke') {
      submittedAction = {
        kind: 'revoke', targetID: selected.id, account: selected.account, permission: actionDialog.permission, reason: formString(form, 'reason'),
      }
    } else {
      submittedAction = { kind: 'reissue', targetID: selected.id, account: selected.account, reason: formString(form, 'reason') }
    }

    if (submittedAction.kind === 'revoke' || submittedAction.kind === 'reissue') {
      setActionDialog(null)
      setPendingAction(submittedAction)
      setDangerConfirmed(false)
      return
    }
    await submitAuthorizedAction(submittedAction)
  }

  async function submitAuthorizedAction(action: SubmittedAction) {
    if (!beginMutation()) return
    let path = ''
    let body: Record<string, unknown> = { reason: action.reason }
    let scope = ''
    if (action.kind === 'grant') {
      path = `/v1/data-authorizations/accounts/${action.targetID}/permissions/grant`
      body = action.permanent
        ? { ...body, permission: action.permission, permanent: true }
        : { ...body, permission: action.permission, expiresAt: action.expiresAt }
      scope = 'permission-grant'
    } else if (action.kind === 'revoke') {
      path = `/v1/data-authorizations/accounts/${action.targetID}/permissions/revoke`
      body = { ...body, permission: action.permission }
      scope = 'permission-revoke'
    } else {
      path = `/v1/data-authorizations/accounts/${action.targetID}/token/reissue`
      scope = 'token-reissue'
    }
    setError('')
    let response: ApiResult
    try {
      response = await client(path, {
        method: 'POST', headers: { 'Idempotency-Key': newAuthorizationIdempotencyKey(scope) }, body, showResult: false, silentLoading: true,
      })
    } catch {
      setError('数据授权操作失败，请检查网络后重试。')
      return
    } finally {
      endMutation()
    }
    if (!response.ok) {
      setError('数据授权操作失败，请刷新后重试。')
      return
    }
    if (action.kind === 'reissue') {
      const data = responseData(response.data)
      const token = typeof data?.token === 'string' ? data.token : ''
      if (token) setOneTimeToken(token)
    }
    const message = action.kind === 'grant' ? '授权已保存。' : action.kind === 'revoke' ? '权限已撤销。' : '访问 Token 已重新签发。'
    setActionDialog(null)
    setPendingAction(null)
    setDangerConfirmed(false)
    await refreshAfterMutation(message, action.targetID)
  }

  function beginMutation() {
    if (mutationInFlight.current) return false
    mutationInFlight.current = true
    setMutating(true)
    return true
  }

  function endMutation() {
    mutationInFlight.current = false
    setMutating(false)
  }

  async function copyToken() {
    try {
      await navigator.clipboard.writeText(oneTimeToken)
      setTokenCopied(true)
    } catch {
      setTokenCopied(false)
    }
  }

  function submitAuditFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (auditFilters.startTime && auditFilters.endTime && new Date(auditFilters.startTime).getTime() > new Date(auditFilters.endTime).getTime()) {
      setError('审计结束时间不能早于开始时间。')
      return
    }
    const nextFilters = { ...auditFilters }
    setAppliedAuditFilters(nextFilters)
    void loadAudits(nextFilters)
  }

  function cancelAuditLoad() {
    auditRequestRef.current?.abort()
    auditRequestRef.current = null
    auditRequestSequence.current += 1
    setAuditLoading(false)
    setNotice('已取消授权审计查询，保留最近一次成功数据。')
  }

  const content = <div className={styles.page} aria-busy={loading || mutating}>
    <div className={styles.embeddedHeader}><div><strong>开放 API 数据授权</strong><span>开放用户不进入内部控制台，访问 Token 仅展示一次。</span></div><div className={styles.actions}><button type="button" onClick={() => void loadAccounts({ search: keyword })} disabled={loading}><RefreshCcw aria-hidden="true" />刷新</button><button className={styles.primary} type="button" onClick={() => { setCreatePermanent(false); setCreateOpen(true) }}><Plus aria-hidden="true" />开通账号</button></div></div>
    {(error || notice) && <div className={error ? styles.errorBanner : styles.banner} role={error ? 'alert' : 'status'} aria-live="polite">{error || notice}</div>}
    <Section title="开放接口账号" description="选择账号后维护天气、Bojun 订单、营业金额查询权限和访问凭证。" flush>
      <MasterDetail className={styles.masterDetail} masterWidth={312} masterLabel="开放接口账号列表" detailLabel="开放接口账号详情" master={<div className={styles.masterPane}><form className={styles.search} onSubmit={(event) => { event.preventDefault(); void loadAccounts({ search: keyword }) }}><label><span>搜索账号</span><input value={keyword} onChange={(event) => setKeyword(event.currentTarget.value)} placeholder="账号、邮箱或名称" /></label><button type="submit" disabled={loading}>查询</button></form>{loading && accounts.length === 0 ? <FeedbackState kind="loading" title="正在加载账号" /> : null}<div className={styles.accountList}>{accounts.map((account) => <button className={selected?.id === account.id ? styles.selectedAccount : undefined} type="button" key={account.id} onClick={() => setSelectedID(account.id)}><span><strong>{account.nickname || account.account}</strong><small>{account.account}</small></span><StatusTag tone={account.credentialStatus === 'ACTIVE' ? 'success' : 'danger'}>{account.credentialStatus === 'ACTIVE' ? '凭证有效' : '已撤销'}</StatusTag></button>)}</div>{!loading && accounts.length === 0 && !error ? <FeedbackState kind="empty" title="暂无开放接口账号" /> : null}{pagination.hasMore ? <button className={styles.loadMore} type="button" onClick={() => void loadAccounts({ append: true })} disabled={loading}>加载更多</button> : null}</div>}
        detail={<div className={styles.detailPane}>{selected ? <><header className={styles.accountHeading}><div><h3>{selected.nickname || selected.account}</h3><p>{selected.account}</p></div><button type="button" onClick={() => setActionDialog({ kind: 'reissue' })}><RotateCcw aria-hidden="true" />重签 Token</button></header><dl className={styles.credential}><div><dt>Token 标识</dt><dd><code>{selected.tokenPrefix || '-'}</code></dd></div><div><dt>签发时间</dt><dd>{formatDateTime(selected.issuedAt)}</dd></div><div><dt>权限范围</dt><dd>仅开放接口，不允许登录控制台</dd></div></dl><div className={styles.permissions}>{permissionCatalog.map((catalog) => { const permission = selected.permissions.find((item) => item.permission === catalog.permission) ?? fallbackPermission(catalog.permission, catalog.label); return <PermissionRow key={catalog.permission} permission={permission} description={catalog.description} onGrant={() => { setGrantPermanent(permission.permanent); setActionDialog({ kind: 'grant', permission: catalog.permission }) }} onRevoke={() => setActionDialog({ kind: 'revoke', permission: catalog.permission })} /> })}</div></> : <FeedbackState kind="empty" title="请选择开放接口账号" description="从左侧账号列表选择后查看授权详情。" />}</div>} />
    </Section>
    <Section title="授权审计" description={appliedAuditFilters.targetUserId ? `当前筛选目标账号 #${appliedAuditFilters.targetUserId}` : '最近授权变更'} flush><details className={styles.auditFilters}><summary>筛选审计记录</summary><FilterToolbar><form className={styles.auditForm} onSubmit={submitAuditFilters} aria-label="授权审计筛选"><label>目标账号<select value={String(auditFilters.targetUserId)} disabled={auditLoading} onChange={(event) => updateAuditFilter('targetUserId', Number(event.currentTarget.value) || 0)}><option value="0">全部已加载账号</option>{accounts.map((account) => <option value={account.id} key={account.id}>{account.account}</option>)}</select></label><label>审计动作<select value={auditFilters.action} disabled={auditLoading} onChange={(event) => updateAuditFilter('action', event.currentTarget.value as DataAuthorizationAuditAction | '')}><option value="">全部动作</option>{dataAuthorizationAuditActions.map((action) => <option value={action} key={action}>{auditActionLabel(action)}</option>)}</select></label><label>开始时间<input type="datetime-local" value={auditFilters.startTime} disabled={auditLoading} onChange={(event) => updateAuditFilter('startTime', event.currentTarget.value)} /></label><label>结束时间<input type="datetime-local" value={auditFilters.endTime} disabled={auditLoading} onChange={(event) => updateAuditFilter('endTime', event.currentTarget.value)} /></label><button type="submit" disabled={auditLoading}>{auditLoading ? '查询中…' : '查询审计'}</button><button type="button" onClick={cancelAuditLoad} disabled={!auditLoading}>取消</button></form></FilterToolbar></details>{audits.length === 0 ? <FeedbackState kind={auditLoading ? 'loading' : 'empty'} title={auditLoading ? '授权审计加载中' : '暂无匹配的授权变更记录'} /> : <DataTable scrollLabel="数据授权审计记录"><thead><tr><th scope="col">时间</th><th scope="col">动作</th><th scope="col">账号</th><th scope="col">权限</th><th scope="col">有效期变更</th><th scope="col">原因</th></tr></thead><tbody>{audits.map((audit) => <tr key={audit.id}><td>{formatDateTime(audit.createdAt)}</td><td>{auditActionLabel(audit.action)}</td><td>{audit.targetAccount || `#${audit.targetUserId}`}</td><td>{permissionLabel(audit.permission)}</td><td>{formatAuditExpiry(audit)}</td><td className={styles.reason}>{audit.reason}</td></tr>)}</tbody></DataTable>}{auditPagination.hasMore ? <div className={styles.more}><button type="button" disabled={auditLoading} onClick={() => void loadAudits(appliedAuditFilters, { append: true, beforeId: auditPagination.nextBeforeId })}>{auditLoading ? '加载中…' : '加载更早记录'}</button></div> : null}</Section>
    <Dialog open={createOpen} title="开通开放 API 账号" closeDisabled={mutating} onClose={() => setCreateOpen(false)}><form className={styles.form} onSubmit={createAccount}><Field label="账号"><input name="account" required minLength={3} maxLength={40} pattern="[a-z0-9][a-z0-9_\-]{2,39}" placeholder="partner_weather_01" autoFocus /></Field><Field label="显示名称"><input name="nickname" required maxLength={64} placeholder="合作方天气账号" /></Field><fieldset><legend>初始数据权限（可暂不授权）</legend>{permissionCatalog.map((item) => <label className={styles.checkbox} key={item.permission}><input type="checkbox" name={item.permission} /><span><strong>{item.label}</strong><small>{item.description}</small></span></label>)}</fieldset><label className={styles.checkbox}><input type="checkbox" name="permanent" checked={createPermanent} onChange={(event) => setCreatePermanent(event.currentTarget.checked)} /><span><strong>永久授权</strong><small>选中后不设置到期时间；账号或 Token 被停用时仍会立即失效。</small></span></label><Field label="统一到期时间"><input name="expiresAt" type="datetime-local" defaultValue={defaultAuthorizationExpiry()} disabled={createPermanent} required={!createPermanent} /></Field><Field label="开户原因"><textarea name="reason" required maxLength={500} rows={3} placeholder="说明申请方、用途和审批依据" /></Field><p className={styles.warning}><KeyRound aria-hidden="true" />账号创建后只展示一次访问 Token，请立即复制并通过安全渠道交付。</p><div className={styles.formActions}><button type="button" onClick={() => setCreateOpen(false)} disabled={mutating}>取消</button><button className={styles.primary} type="submit" disabled={mutating}>{mutating ? '正在开通…' : '确认开通'}</button></div></form></Dialog>
    <Dialog open={Boolean(actionDialog && selected)} title={actionDialog ? actionTitle(actionDialog) : '数据授权操作'} closeDisabled={mutating} onClose={() => setActionDialog(null)}><form className={styles.form} onSubmit={submitAction}>{actionDialog?.kind === 'grant' ? <><label className={styles.checkbox}><input type="checkbox" name="permanent" checked={grantPermanent} onChange={(event) => setGrantPermanent(event.currentTarget.checked)} autoFocus /><span><strong>永久授权</strong><small>选中后该权限不设置到期时间。</small></span></label><Field label="授权到期时间"><input name="expiresAt" type="datetime-local" defaultValue={defaultAuthorizationExpiry()} disabled={grantPermanent} required={!grantPermanent} /></Field></> : null}<Field label="操作原因"><textarea name="reason" required maxLength={500} rows={4} autoFocus={actionDialog?.kind !== 'grant'} placeholder="填写审批依据或撤销原因" /></Field><div className={styles.formActions}><button type="button" onClick={() => setActionDialog(null)} disabled={mutating}>取消</button><button className={actionDialog?.kind === 'revoke' ? styles.danger : styles.primary} type="submit" disabled={mutating}>{mutating ? '正在提交…' : '继续'}</button></div></form></Dialog>
    <Dialog open={Boolean(pendingAction)} role="alertdialog" title={pendingAction ? dangerousActionTitle(pendingAction) : '确认危险操作'} closeDisabled={mutating} onClose={() => { setPendingAction(null); setDangerConfirmed(false) }}><form className={styles.form} onSubmit={(event) => { event.preventDefault(); if (pendingAction && dangerConfirmed) void submitAuthorizedAction(pendingAction) }}><p className={styles.warning}>{pendingAction ? dangerousActionDescription(pendingAction) : ''}</p><label className={styles.checkbox}><input type="checkbox" checked={dangerConfirmed} disabled={mutating} onChange={(event) => setDangerConfirmed(event.currentTarget.checked)} /><span><strong>我已理解此操作的影响</strong><small>确认后将立即执行，且不能撤回。</small></span></label><div className={styles.formActions}><button type="button" onClick={() => { setPendingAction(null); setDangerConfirmed(false) }} disabled={mutating}>取消</button><button className={styles.danger} type="submit" disabled={mutating || !dangerConfirmed}>{mutating ? '正在提交…' : '确认执行'}</button></div></form></Dialog>
    <Dialog open={Boolean(oneTimeToken)} title="访问 Token（仅展示一次）" onClose={() => { setOneTimeToken(''); setTokenCopied(false) }}><div className={styles.token} role="status"><p>关闭后无法再次查看。请现在复制，并通过安全渠道交付给接口调用方。</p><code>{oneTimeToken}</code><button className={styles.primary} type="button" onClick={() => void copyToken()}>{tokenCopied ? <Check aria-hidden="true" /> : <Clipboard aria-hidden="true" />}{tokenCopied ? '已复制' : '复制 Token'}</button></div></Dialog>
  </div>

  function updateAuditFilter<K extends keyof typeof auditFilters>(key: K, value: (typeof auditFilters)[K]) {
    setAuditFilters((current) => ({ ...current, [key]: value }))
  }
  return content
}

function PermissionRow({ permission, description, onGrant, onRevoke }: { permission: DataAuthorizationPermission; description: string; onGrant: () => void; onRevoke: () => void }) {
  const active = permission.status === 'ACTIVE'
  return <article className={styles.permission}><div className={styles.permissionCopy}><div className={styles.permissionTitle}><strong>{permission.label}</strong><StatusTag tone={active ? 'success' : permission.status === 'EXPIRED' ? 'warning' : 'neutral'}>{permissionStatusLabel(permission.status)}</StatusTag></div><p>{description}</p></div><div><span>范围</span><strong>{permission.scope || '-'}</strong></div><div><span>有效期</span><strong>{permission.permanent ? '永久有效' : formatDateTime(permission.expiresAt)}</strong></div><div className={styles.actions}><button type="button" onClick={onGrant}>{active ? '续期' : '授权'}</button>{permission.status !== 'NOT_GRANTED' && <button className={styles.danger} type="button" onClick={onRevoke}>撤销</button>}</div></article>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className={styles.field}><span>{label}</span>{children}</label> }

function responseData(payload: unknown) { return payload && typeof payload === 'object' && !Array.isArray(payload) && (payload as { data?: unknown }).data && typeof (payload as { data?: unknown }).data === 'object' ? (payload as { data: Record<string, unknown> }).data : null }
function formString(form: FormData, key: string) { const value = form.get(key); return typeof value === 'string' ? value.trim() : '' }
function deduplicateAccounts(accounts: DataAuthorizationAccount[]) { return [...new Map(accounts.map((account) => [account.id, account])).values()] }
function deduplicateAudits(audits: DataAuthorizationAudit[]) { return [...new Map(audits.map((audit) => [audit.id, audit])).values()] }
function fallbackPermission(permission: DataPermissionCode, label: string): DataAuthorizationPermission { return { permission, label, scope: '全模块数据', status: 'NOT_GRANTED', permanent: false, expiresAt: null } }
function permissionLabel(permission: string) { return permission === 'weather.read' ? '天气数据查询' : permission === 'bojun.order.read' ? 'Bojun 订单查询' : permission === 'business_overview.read' ? '营业金额查询' : permission === 'open_api.account' ? '开放接口账号' : permission === 'open_api.credential' ? '访问凭证' : permission }
function permissionStatusLabel(status: string) { return status === 'ACTIVE' ? '生效中' : status === 'EXPIRED' ? '已过期' : '未授权' }
function auditActionLabel(action: string) { return ({ ACCOUNT_CREATE: '开通账号', GRANT: '授权', RENEW: '续期', REVOKE: '撤销', TOKEN_REISSUE: '重签 Token' } as Record<string, string>)[action] ?? action }
function actionTitle(action: NonNullable<ActionDialog>) { return action.kind === 'grant' ? `授权 / 续期 · ${permissionLabel(action.permission)}` : action.kind === 'revoke' ? `撤销 · ${permissionLabel(action.permission)}` : '重新签发访问 Token' }
function dangerousActionTitle(action: SubmittedAction) { return action.kind === 'revoke' ? '确认撤销数据权限' : '确认重新签发访问 Token' }
function dangerousActionDescription(action: SubmittedAction) { return action.kind === 'revoke' ? `确认撤销 ${action.account} 的 ${permissionLabel(action.permission ?? '')}？撤销后，该权限的下一次开放接口请求将立即失效。` : `确认重新签发 ${action.account} 的访问 Token？旧 Token 将立即失效。` }
function formatDateTime(value: string | null | undefined) { if (!value) return '-'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short' }).format(date) }
function formatAuditExpiry(audit: DataAuthorizationAudit) { if (!audit.oldExpiresAt && !audit.newExpiresAt) return '-'; return `${formatDateTime(audit.oldExpiresAt)} → ${formatDateTime(audit.newExpiresAt)}` }
