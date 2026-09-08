import type { ReactNode } from 'react'
import {
  Activity,
  ArrowDownToLine,
  ArrowUpFromLine,
  BookOpen,
  Building2,
  ChartColumn,
  CheckCircle2,
  CloudSun,
  Database,
  Download,
  FileSpreadsheet,
  Inbox,
  ListChecks,
  Search,
  ScrollText,
  Send,
  Settings2,
  Upload,
  Users,
  Wrench,
} from 'lucide-react'
import type { ReportCenterSection } from '../reportCenter/types'
import { canViewNavigationItem, resolveAccessibleNavigationItem } from './navigationPermissions'

export type NavKey =
  | 'business_overview'
  | 'overview'
  | 'runs'
  | 'delivery_logs'
  | 'step_runs'
  | 'store_info'
  | 'mall_weather'
  | 'access_management'
  | 'sources'
  | 'receive'
  | 'pull_records'
  | 'backfill'
  | 'youzan_distribution'
  | 'rules'
  | 'processed'
  | 'methods'
  | 'destinations'
  | 'tasks'
  | 'push_policy'
  | 'excel_jobs'
  | 'excel_schemes'
  | 'excel_write'
  | 'report_catalog'
  | 'report_configuration'
  | 'report_query'
  | 'report_exports'
  | 'office_messages'
  | 'office_push'

export type NavItem = { key: NavKey; label: string; description: string; icon: ReactNode }
export type NavGroup = { label: string; items: NavItem[] }

export const navGroups: NavGroup[] = [
  {
    label: '基础信息',
    items: [
      { key: 'business_overview', label: '营业概况', description: '销售日结与门店对账', icon: <ChartColumn aria-hidden="true" /> },
      { key: 'store_info', label: '店铺信息', description: '店铺资料与坐标维护', icon: <Building2 aria-hidden="true" /> },
    ],
  },
  {
    label: '运行监控',
    items: [
      { key: 'overview', label: '运行总览', description: '运行与推送健康度', icon: <Activity aria-hidden="true" /> },
      { key: 'runs', label: '流水线运行', description: '按状态与 Trace 查询', icon: <ListChecks aria-hidden="true" /> },
      { key: 'delivery_logs', label: '推送日志', description: '按门店与业务键查询', icon: <Send aria-hidden="true" /> },
      { key: 'step_runs', label: '步骤运行', description: '选择运行查看步骤', icon: <BookOpen aria-hidden="true" /> },
    ],
  },
  {
    label: '数据接入',
    items: [
      { key: 'sources', label: '数据源', description: '接入配置与启用状态', icon: <Database aria-hidden="true" /> },
      { key: 'receive', label: '接口接收', description: '外部推送入库记录', icon: <Inbox aria-hidden="true" /> },
      { key: 'pull_records', label: '拉取记录', description: '主动拉取原始数据', icon: <ArrowDownToLine aria-hidden="true" /> },
      { key: 'backfill', label: '伯俊补拉', description: '预览并确认补写订单', icon: <Download aria-hidden="true" /> },
      { key: 'youzan_distribution', label: '有赞分销', description: '每日拉取与手动补拉', icon: <Download aria-hidden="true" /> },
    ],
  },
  { label: '数据服务', items: [{ key: 'mall_weather', label: '商场天气', description: '实况、趋势与预警', icon: <CloudSun aria-hidden="true" /> }] },
  {
    label: '报表中心',
    items: [
      { key: 'report_query', label: '报表中心', description: '选择已授权报表并下载', icon: <Search aria-hidden="true" /> },
      { key: 'report_exports', label: '下载记录', description: 'Excel 生成与下载状态', icon: <Download aria-hidden="true" /> },
    ],
  },
  {
    label: '报表管理',
    items: [
      { key: 'report_catalog', label: '报表目录', description: '报表定义与发布状态', icon: <FileSpreadsheet aria-hidden="true" /> },
      { key: 'report_configuration', label: '报表配置', description: '过程、形参与字段契约', icon: <Settings2 aria-hidden="true" /> },
    ],
  },
  {
    label: '办公消息',
    items: [
      { key: 'office_messages', label: '消息管理', description: '文本与 Oracle Excel 来源', icon: <FileSpreadsheet aria-hidden="true" /> },
      { key: 'office_push', label: '推送管理', description: '飞书与 WebDAV 推送记录', icon: <Send aria-hidden="true" /> },
    ],
  },
  { label: '账号与权限', items: [{ key: 'access_management', label: '账号与权限', description: '控制台账号、角色与审计', icon: <Users aria-hidden="true" /> }] },
  {
    label: '数据处理',
    items: [
      { key: 'rules', label: '清洗规则', description: '规则类型与执行顺序', icon: <ListChecks aria-hidden="true" /> },
      { key: 'processed', label: '处理结果', description: '质量与业务数据查询', icon: <CheckCircle2 aria-hidden="true" /> },
      { key: 'methods', label: '方法目录', description: '配置与系统方法', icon: <Wrench aria-hidden="true" /> },
    ],
  },
  {
    label: '数据交付',
    items: [
      { key: 'destinations', label: '推送目标', description: '目标接口配置', icon: <Send aria-hidden="true" /> },
      { key: 'tasks', label: '推送任务', description: '任务与目标关系', icon: <ArrowUpFromLine aria-hidden="true" /> },
      { key: 'push_policy', label: '推送策略', description: '订单少推送设置', icon: <ListChecks aria-hidden="true" /> },
    ],
  },
  {
    label: '数据工具',
    items: [
      { key: 'excel_jobs', label: 'Excel 任务', description: '状态与结果下载', icon: <ScrollText aria-hidden="true" /> },
      { key: 'excel_schemes', label: 'Excel 匹配', description: '自定义多步骤方案', icon: <Upload aria-hidden="true" /> },
      { key: 'excel_write', label: 'Excel 写入', description: '导入与退回未匹配', icon: <Database aria-hidden="true" /> },
    ],
  },
]

const navItems = navGroups.flatMap((group) => group.items)
const navKeys = navItems.map((item) => item.key)
const compactWorkspaceKeys = new Set<NavKey>([
  'access_management', 'sources', 'receive', 'pull_records', 'backfill', 'youzan_distribution', 'rules', 'processed',
  'methods', 'destinations', 'tasks', 'push_policy', 'overview', 'runs', 'delivery_logs', 'step_runs', 'business_overview', 'store_info',
  'mall_weather', 'report_catalog', 'report_configuration', 'report_query', 'report_exports', 'excel_jobs',
  'excel_schemes', 'excel_write',
  'office_messages', 'office_push',
])

export function navGroupFor(key: NavKey) {
  return navGroups.find((group) => group.items.some((item) => item.key === key))
}

export function navFromHash(hash = window.location.hash): NavKey {
  const value = hash.replace(/^#\/?/, '') as NavKey
  return navItems.some((item) => item.key === value) ? value : 'overview'
}

export function accessibleNavFromHash(permissions: readonly string[], hash = window.location.hash): NavKey | null {
  return resolveAccessibleNavigationItem(navFromHash(hash), navKeys, permissions)
}

export function visibleNavigationGroups(permissions: readonly string[]) {
  return navGroups.map((group) => ({
    ...group,
    items: group.items.filter((item) => canViewNavigationItem(item.key, permissions)),
  })).filter((group) => group.items.length > 0)
}

export function usesCompactWorkspace(key: NavKey) {
  return compactWorkspaceKeys.has(key)
}

export function reportCenterSection(key: NavKey): ReportCenterSection | null {
  if (key === 'report_catalog') return 'catalog'
  if (key === 'report_configuration') return 'configuration'
  if (key === 'report_query') return 'query'
  if (key === 'report_exports') return 'exports'
  return null
}

export function reportCenterNavKey(section: ReportCenterSection): NavKey {
  if (section === 'configuration') return 'report_configuration'
  if (section === 'query') return 'report_query'
  if (section === 'exports') return 'report_exports'
  return 'report_catalog'
}
