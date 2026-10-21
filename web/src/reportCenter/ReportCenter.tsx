import type { ReactNode } from 'react'
import { BookOpenText, Download, FileSpreadsheet, Settings2, type LucideIcon } from 'lucide-react'
import { ReportCatalogPage } from './pages/ReportCatalogPage/ReportCatalogPage'
import { ReportConfigurationPage } from './pages/ReportConfigurationPage/ReportConfigurationPage'
import { ReportQueryPage } from './pages/ReportQueryPage/ReportQueryPage'
import { ReportExportsPage } from './pages/ReportExportsPage/ReportExportsPage'
import type { ReportCenterClient } from './api'
import type { ReportCenterSection } from './types'
import { FeedbackState, PageCanvas, PageHeader } from '../ui'
import styles from './ReportCenter.module.css'

const sectionLinks: Record<ReportCenterSection, string> = { catalog: '#report_catalog', configuration: '#report_configuration', query: '#report_query', exports: '#report_exports' }

const managementSections: Array<{ key: ReportCenterSection; label: string; description: string; icon: LucideIcon }> = [
  { key: 'catalog', label: '报表目录', description: '发现与管理', icon: BookOpenText },
  { key: 'configuration', label: '报表工作室', description: '关联与设计', icon: Settings2 },
]

const downloadSections: Array<{ key: ReportCenterSection; label: string; description: string; icon: LucideIcon }> = [
  { key: 'query', label: '报表下载', description: '筛选与生成', icon: Download },
  { key: 'exports', label: '下载记录', description: '文件与留存', icon: FileSpreadsheet },
]

export function ReportCenter({ client, permissions, section, onNavigate }: {
  client: ReportCenterClient
  permissions: string[]
  section: ReportCenterSection
  onNavigate: (section: ReportCenterSection) => void
}) {
  const sections = section === 'query' || section === 'exports' ? downloadSections : managementSections
  const visibleSections = sections.filter((item) => hasSectionPermission(permissions, item.key))
  const allowed = visibleSections.some((item) => item.key === section)
  const navigation = <nav className={styles.tabs} aria-label="报表中心工作流">
    {visibleSections.map((item) => {
      const Icon = item.icon
      const active = item.key === section
      return <a className={active ? styles.active : ''} href={sectionLinks[item.key]} aria-current={active ? 'page' : undefined} onClick={(event) => { if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return; event.preventDefault(); onNavigate(item.key) }} key={item.key}>
        <Icon aria-hidden="true" />
        <span><strong>{item.label}</strong><small>{item.description}</small></span>
      </a>
    })}
  </nav>
  let content: ReactNode
  if (!allowed) content = <PageCanvas density="compact">
    <PageHeader eyebrow="REPORT CENTER" title="报表中心" description="管理业务报表，查询数据并下载文件。" />
    {navigation}
    <FeedbackState kind="error" title="当前账号无权访问此报表模块" description="请从侧栏选择已授权模块，或联系管理员补充报表中心权限。" />
  </PageCanvas>
  else if (section === 'configuration') content = <ReportConfigurationPage client={client} navigation={navigation} />
  else if (section === 'query') content = <ReportQueryPage client={client} navigation={navigation} canExport={permissions.includes('report.export')} />
  else if (section === 'exports') content = <ReportExportsPage client={client} navigation={navigation} />
  else content = <ReportCatalogPage client={client} canManage={hasReportPermission(permissions, 'report.manage')} navigation={navigation} />

  return content
}

function hasReportPermission(permissions: string[], required: string) {
  return permissions.includes(required)
}

function hasSectionPermission(permissions: string[], section: ReportCenterSection) {
  if (!permissions.includes('report.read')) return false
  if (section === 'catalog' || section === 'configuration') return permissions.includes('report.manage')
  if (section === 'query') return permissions.includes('report.execute')
  if (section === 'exports') return permissions.includes('report.export')
  return true
}
