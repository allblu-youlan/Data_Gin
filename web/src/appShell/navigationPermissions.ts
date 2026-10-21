import { overviewSignalAccess } from './overviewWorkspacePolicy'

export function canViewNavigationItem(key: string, permissions: readonly string[]) {
  if (key === 'access_management') return permissions.some((permission) => permission.startsWith('system.'))
  if (key === 'overview') return Object.values(overviewSignalAccess(permissions)).some(Boolean)
  if (key === 'report_catalog') return permissions.includes('report.read') && permissions.includes('report.manage')
  if (key === 'report_configuration') return permissions.includes('report.read') && permissions.includes('report.manage')
  if (key === 'report_query') return permissions.includes('report.read') && permissions.includes('report.execute')
  if (key === 'report_exports') return permissions.includes('report.read') && permissions.includes('report.export')

  const required = navPermission(key)
  return !required || permissions.includes(required) || impliedReadPermission(required, permissions)
}

function impliedReadPermission(required: string, permissions: readonly string[]) {
  const impliedBy: Record<string, string> = {
    'source.read': 'source.manage',
    'pipeline.read': 'pipeline.manage',
    'delivery.read': 'delivery.manage',
  }
  const candidate = impliedBy[required]
  return Boolean(candidate && permissions.includes(candidate))
}

export function resolveAccessibleNavigationItem<T extends string>(
  requested: T,
  orderedItems: readonly T[],
  permissions: readonly string[],
): T | null {
  if (orderedItems.includes(requested) && canViewNavigationItem(requested, permissions)) return requested
  return orderedItems.find((item) => canViewNavigationItem(item, permissions)) ?? null
}

function navPermission(key: string) {
  const permissions: Record<string, string> = {
    business_overview: 'business_overview.read', store_info: 'mall.read', mall_weather: 'weather.read', sources: 'source.read', receive: 'data.read', pull_records: 'data.read',
    backfill: 'data.manage', youzan_distribution: 'data.manage', rules: 'pipeline.read', processed: 'data.read', methods: 'pipeline.read',
    destinations: 'delivery.read', tasks: 'delivery.read', push_policy: 'delivery.read', runs: 'pipeline.read', step_runs: 'pipeline.read',
    delivery_logs: 'delivery.read', excel_jobs: 'excel.read', excel_schemes: 'excel.read', excel_write: 'excel.manage', excel_export: 'excel.read',
    office_messages: 'office_message.read', office_push: 'office_message.read',
    mention_chats: 'office_mention.manage', mention_status: 'office_mention.manage',
    mention_recipients: 'office_mention.read', mention_reports: 'office_mention.read', mention_jobs: 'office_mention.read',
  }
  return permissions[key]
}
