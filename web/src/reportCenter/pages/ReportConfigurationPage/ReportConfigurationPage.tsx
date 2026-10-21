import { useMemo, useState, type ReactNode } from 'react'
import { BookOpenText, ChevronDown, Database, History, Plus } from 'lucide-react'
import { Button, FeedbackState, PageCanvas, PageHeader, Section, StatusTag } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { BojunDictionaryPanel } from '../../components/BojunDictionaryPanel/BojunDictionaryPanel'
import { ReportConfigDrawer } from '../../components/ReportConfigDrawer/ReportConfigDrawer'
import { ReportValidationResultDrawer } from '../../components/ReportValidationResultDrawer/ReportValidationResultDrawer'
import { ReportVersionDrawer } from '../../components/ReportVersionDrawer/ReportVersionDrawer'
import { ReportAuditDrawer } from '../../components/ReportAuditDrawer/ReportAuditDrawer'
import { ReportDatasourcePanel } from '../../components/ReportDatasourcePanel/ReportDatasourcePanel'
import { ReportInputQueryDefinitionPanel } from '../../components/ReportInputQueryDefinitionPanel/ReportInputQueryDefinitionPanel'
import { ReportLibrary } from '../../components/ReportLibrary/ReportLibrary'
import type { ReportPublication, ReportSummary } from '../../types'
import { useReportCatalog } from '../../useReportCatalog'
import { useReportDatasources } from '../../useReportDatasources'
import design from '../../reportDesign.module.css'
import styles from './ReportConfigurationPage.module.css'

export function ReportConfigurationPage({ client, navigation }: { client: ReportCenterClient; navigation?: ReactNode }) {
  const [search, setSearch] = useState('')
  const query = useMemo(() => ({ search, limit: 50 }), [search])
  const { items, loading, loadingMore, error, hasMore, reload, loadMore } = useReportCatalog(client, query)
  const ownedItems = items.filter((item) => item.isOwner)
  const datasources = useReportDatasources(client)
  const systemDatasource = datasources.items.find((source) => source.code === 'system_environment_oracle' && source.enabled)
  const [selected, setSelected] = useState<ReportSummary | null>(null)
  const [creating, setCreating] = useState(false)
  const [auditsOpen, setAuditsOpen] = useState(false)
  const [publication, setPublication] = useState<ReportPublication | null>(null)
  const [versionReport, setVersionReport] = useState<ReportSummary | null>(null)
  const createAction = <Button variant="primary" onClick={() => setCreating(true)}><Plus aria-hidden="true" />新建报表</Button>

  return <PageCanvas density="compact" className={`${design.surface} ${design.page}`}>
    {navigation}
    <PageHeader eyebrow="REPORT STUDIO" title="报表工作室" description="把业务表连接起来，让每一份报表都有清晰的数据来路。" actions={<><Button onClick={() => setAuditsOpen(true)}><History aria-hidden="true" />操作记录</Button>{createAction}</>} context={<div className={design.caption}><strong>{ownedItems.length} 份自有报表{hasMore ? '已加载' : ''}</strong><span>·</span><span>{ownedItems.filter((item) => item.status === 'ACTIVE').length} 份已发布</span><span>·</span><span>{ownedItems.filter((item) => item.status === 'DRAFT').length} 份草稿</span></div>} />
    <div className={styles.connectionBar}>
      <div className={styles.connection}><span className={styles.connectionIcon}><Database aria-hidden="true" /></span><div><strong>{systemDatasource ? '系统环境 Oracle' : datasources.loading ? '正在读取系统连接…' : 'Oracle 数据连接'}</strong><span>{systemDatasource ? '沿用现有连接，直接开始设计' : '在下方字典与连接设置中查看数据源'}</span></div></div>
      <StatusTag tone={systemDatasource ? 'success' : 'neutral'}>{systemDatasource ? '已自动选用' : '统一管理连接'}</StatusTag>
      <div className={styles.dictionaryNote}><BookOpenText aria-hidden="true" /><span>主键、显示字段与关联定义<br /><strong>直接继承伯俊字典</strong></span></div>
    </div>
    {datasources.error ? <FeedbackState kind="error" title="数据连接读取失败" description={datasources.error} action={<Button onClick={datasources.reload}>重新读取连接</Button>} /> : null}
    <ReportLibrary reports={ownedItems} loading={loading} loadingMore={loadingMore} hasMore={hasMore} error={error} search={search} onSearch={setSearch} onReload={reload} onLoadMore={() => void loadMore()} onOpen={setSelected} onVersions={setVersionReport} emptyAction={createAction} />
    <details className={styles.advanced}>
      <summary><span><BookOpenText aria-hidden="true" /><strong>字典与连接设置</strong><small>查看字段定义、输入查询和数据连接</small></span><ChevronDown aria-hidden="true" /></summary>
      <Section title="伯俊表与字段字典" description="主键、默认显示字段与关联定义自动读取，无需逐表设置。"><BojunDictionaryPanel client={client} datasources={datasources.items} /></Section>
      <Section title="筛选项输入查询" description="维护筛选下拉框使用的命名查询。连接继续使用系统环境配置。"><ReportInputQueryDefinitionPanel client={client} /></Section>
      <Section title="其他 Oracle 数据源" description="需要使用其他已登记连接时，在这里维护。" flush><ReportDatasourcePanel client={client} datasources={datasources.items.filter((source) => source.code !== 'system_environment_oracle')} loading={datasources.loading} error={datasources.error} onReload={datasources.reload} /></Section>
    </details>
    {creating || selected ? <ReportConfigDrawer client={client} report={selected} datasources={datasources.items} datasourcesLoading={datasources.loading} datasourcesError={datasources.error} onSaved={reload} onClose={() => { setCreating(false); setSelected(null) }} /> : null}
    <ReportValidationResultDrawer publication={publication} onClose={() => setPublication(null)} />
    <ReportVersionDrawer client={client} report={versionReport} onPublished={setPublication} onChanged={reload} onClose={() => setVersionReport(null)} />
    <ReportAuditDrawer client={client} open={auditsOpen} reports={ownedItems} onClose={() => setAuditsOpen(false)} />
  </PageCanvas>
}
