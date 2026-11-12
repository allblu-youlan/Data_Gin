import { FormEvent, useMemo } from 'react'
import { accessPermissionHierarchyLabel, type AccessPermission } from './accessManagement'
import { Drawer } from './ui'
import styles from './AccessManagementPage.module.css'

export type AccessRoleCreateInput = {
  code: string
  name: string
  description: string
  permissions: string[]
  reason: string
}

type Props = {
  busy: boolean
  catalog: AccessPermission[]
  error?: string
  open: boolean
  onClose: () => void
  onSubmit: (input: AccessRoleCreateInput) => void | Promise<void>
}

export function AccessRoleCreateDrawer({ busy, catalog, error, onClose, onSubmit, open }: Props) {
  const groups = useMemo(() => {
    const grouped = new Map<string, AccessPermission[]>()
    for (const permission of catalog) {
      const module = permission.module || '其他'
      grouped.set(module, [...(grouped.get(module) ?? []), permission])
    }
    return [...grouped.entries()]
  }, [catalog])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    void onSubmit({
      code: value(form, 'code'),
      name: value(form, 'name'),
      description: value(form, 'description'),
      permissions: form.getAll('permissions').filter((permission): permission is string => typeof permission === 'string'),
      reason: value(form, 'reason'),
    })
  }

  return <Drawer open={open} title="创建自定义角色" description="创建后可立即用于账号授权；一级权限自动覆盖下属二级接口，也可仅勾选所需二级权限。目录仅包含当前操作者可授予的权限。" size="wide" closeDisabled={busy} onClose={onClose}>
    <form className={styles.drawerForm} onSubmit={submit}>
      {error && <p className={styles.message} role="alert">{error}</p>}
      <Field label="角色代码"><input name="code" required pattern="[a-z][a-z0-9_]{2,63}" autoComplete="off" placeholder="例如 offline_sales_viewer" /></Field>
      <Field label="角色名称"><input name="name" required maxLength={128} autoComplete="off" placeholder="例如 线下销售查看" /></Field>
      <Field label="角色说明"><textarea name="description" maxLength={500} rows={3} /></Field>
      <div className={styles.permissionMatrix}>
        {groups.map(([module, permissions]) => <fieldset key={module}><legend>{module}</legend>{permissions.map((permission) => <label key={permission.code}><input type="checkbox" name="permissions" value={permission.code} /><span><strong>{permission.name}</strong><small>{permission.code} · {permission.description}</small>{accessPermissionHierarchyLabel(permission, catalog) && <small>{accessPermissionHierarchyLabel(permission, catalog)}</small>}</span><em>{permission.riskLevel}</em></label>)}</fieldset>)}
      </div>
      <Field label="创建原因"><textarea name="reason" required maxLength={500} rows={3} /></Field>
      <button className={styles.primary} type="submit" disabled={busy}>{busy ? '创建中' : '创建角色并加入账号'}</button>
    </form>
  </Drawer>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className={styles.field}><span>{label}</span>{children}</label>
}

function value(form: FormData, key: string) {
  const result = form.get(key)
  return typeof result === 'string' ? result.trim() : ''
}
