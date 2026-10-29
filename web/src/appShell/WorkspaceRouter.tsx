import type { Dispatch, SetStateAction } from 'react'
import { AccessManagementPage } from '../AccessManagementPage'
import { BojunBackfillPage } from '../backfillPages/BojunBackfillPage'
import { YouzanDistributionPage } from '../backfillPages/YouzanDistributionPage'
import type { LegacyTask } from '../backfillPages/youzanDistributionSupport'
import { BusinessOverviewPage } from '../businessOverviewPages/BusinessOverviewPage'
import { DeliveryTasksPage } from '../configurationPages/DeliveryTasksPage/DeliveryTasksPage'
import { DestinationsPage } from '../configurationPages/DestinationsPage/DestinationsPage'
import { MethodsPage } from '../configurationPages/MethodsPage/MethodsPage'
import { PushPolicyPage } from '../configurationPages/PushPolicyPage/PushPolicyPage'
import { RulesPage } from '../configurationPages/RulesPage/RulesPage'
import type { TransformRule } from '../configurationPages/ruleContracts'
import { SourcesPage } from '../configurationPages/SourcesPage/SourcesPage'
import type { DestinationDefinition, SourceDefinition } from '../configurationPages/types'
import { ProcessedRecordsPage } from '../dataPages/ProcessedRecordsPage'
import { RawRecordsPage } from '../dataPages/RawRecordsPage'
import { ExcelMatchPage } from '../excelPages/ExcelMatchPage'
import { ExcelWorkflowPage } from '../excelPages/ExcelWorkflowPage'
import { OracleMethodPage } from '../excelPages/OracleMethodPage'
import { OracleExportPage } from '../excelPages/OracleExportPage'
import { MallWeatherPage, StoreInfoPage } from '../MallWeatherPage'
import type { DataStatisticsSummary, HealthSummary, MallWeatherMetricsSummary } from '../monitoring'
import { DeliveryLogsPage } from '../monitoringPages/DeliveryLogsPage/DeliveryLogsPage'
import { PipelineRunsPage } from '../monitoringPages/PipelineRunsPage/PipelineRunsPage'
import { RunOverviewPage } from '../monitoringPages/RunOverviewPage/RunOverviewPage'
import { StepRunsPage } from '../monitoringPages/StepRunsPage/StepRunsPage'
import { OfficeMentionPage } from '../officeMention/OfficeMentionPage'
import type { MentionSection } from '../officeMention/types'
import { MessageManagementPage } from '../officeMessage/MessageManagementPage'
import { PushManagementPage } from '../officeMessage/PushManagementPage'
import { TableEmailNotificationsPage } from '../tableEmailNotifications/TableEmailNotificationsPage'
import type { DeliveryLog, PipelineRun } from '../monitoringPages/types'
import { ReportCenter } from '../reportCenter/ReportCenter'
import type { ApiRequestOptions, ClientResponse, HTTPMethod } from '../api/client'
import { FeedbackState, PageCanvas } from '../ui'
import { reportCenterNavKey, reportCenterSection, type NavKey } from './navigation'
import { canViewNavigationItem } from './navigationPermissions'

export type WorkspaceApiClientOptions = Omit<ApiRequestOptions, 'method'> & {
  method?: HTTPMethod
  showResult?: boolean
  silentLoading?: boolean
}

export type WorkspaceApiClient = (path: string, options?: WorkspaceApiClientOptions) => Promise<ClientResponse>
export type WorkspaceDownloadClient = (path: string, fileName: string, signal: AbortSignal) => Promise<ClientResponse>
export type MonitoringSnapshot = {
  statistics: DataStatisticsSummary | null
  weather: MallWeatherMetricsSummary | null
  health: HealthSummary | null
}
export type PipelineDefinition = { id: number; name: string; code: string; enabled: boolean }

type WorkspaceRouterProps = {
  activeNav: NavKey
  actorID: string | null
  client: WorkspaceApiClient
  deliveryLogs: DeliveryLog[]
  destinations: DestinationDefinition[]
  downloadFile: WorkspaceDownloadClient
  legacyTasks: LegacyTask[]
  loading: boolean
  monitoring: MonitoringSnapshot
  monitoringStale: boolean
  overviewTotals: { runs: number | null; deliveryLogs: number | null }
  permissions: string[]
  pipelines: PipelineDefinition[]
  refreshing: boolean
  refreshVersion: number
  runs: PipelineRun[]
  sources: SourceDefinition[]
  stepRunFocusID: number | null
  token: string
  transformRules: TransformRule[]
  navigate: (key: NavKey) => void
  onFetchSource: (sourceID: number) => Promise<ClientResponse>
  onLoadSteps: (runID: number) => void
  onRefresh: () => Promise<void>
  onRetryDeliveryLog: (logID: number) => Promise<void>
  onTestSource: (sourceID: number) => Promise<ClientResponse>
  setLoading: Dispatch<SetStateAction<boolean>>
  setResult: Dispatch<SetStateAction<ClientResponse | null>>
  setTransformRules: Dispatch<SetStateAction<TransformRule[]>>
}

export function WorkspaceRouter(props: WorkspaceRouterProps) {
  const {
    activeNav, actorID, client, deliveryLogs, destinations, downloadFile, legacyTasks, loading,
    monitoring, monitoringStale, navigate, onFetchSource, onLoadSteps, onRefresh,
    onRetryDeliveryLog, onTestSource, overviewTotals, permissions, pipelines, refreshing,
    refreshVersion, runs, setLoading, setResult, setTransformRules, sources, stepRunFocusID,
    token, transformRules,
  } = props
  const reportSection = reportCenterSection(activeNav)
  const canManageDelivery = permissions.includes('delivery.manage')

  if (!canViewNavigationItem(activeNav, permissions)) {
    return <PageCanvas><FeedbackState kind="error" title="当前账号无可访问页面" description="请联系管理员为账号分配至少一项页面查看权限。" /></PageCanvas>
  }

  if (activeNav === 'business_overview') return <BusinessOverviewPage client={client} />
  if (activeNav === 'overview') return <RunOverviewPage runs={runs} deliveryLogs={deliveryLogs} monitoring={monitoring} stale={monitoringStale} overviewTotals={overviewTotals} onLoadSteps={onLoadSteps} />
  if (activeNav === 'runs') return <PipelineRunsPage client={client} pipelines={pipelines} onLoadSteps={onLoadSteps} onPipelineRunCompleted={() => void onRefresh()} refreshVersion={refreshVersion} />
  if (activeNav === 'delivery_logs') return <DeliveryLogsPage client={client} onRetryLog={onRetryDeliveryLog} />
  if (activeNav === 'step_runs') return <StepRunsPage client={client} focusRunID={stepRunFocusID} />
  if (activeNav === 'store_info') return <StoreInfoPage actorID={actorID} client={client} downloadFile={downloadFile} />
  if (activeNav === 'mall_weather') return <MallWeatherPage actorID={actorID} client={client} downloadFile={downloadFile} />
  if (activeNav === 'office_messages') return <MessageManagementPage client={client} permissions={permissions} />
  if (activeNav === 'office_push') return <PushManagementPage client={client} permissions={permissions} />
  if (activeNav === 'table_email_notifications') return <TableEmailNotificationsPage client={client} permissions={permissions} />
  const mentionSections: Partial<Record<NavKey, MentionSection>> = { mention_chats: 'chats', mention_recipients: 'recipients', mention_reports: 'reports', mention_jobs: 'jobs', mention_status: 'status' }
  const mentionSection = mentionSections[activeNav]
  if (mentionSection) return <OfficeMentionPage key={mentionSection} client={client} permissions={permissions} section={mentionSection} />
  if (reportSection) return <ReportCenter client={client} permissions={permissions} section={reportSection} onNavigate={(section) => navigate(reportCenterNavKey(section))} />
  if (activeNav === 'access_management') return <AccessManagementPage client={client} permissions={permissions} />
  if (activeNav === 'sources') return <SourcesPage client={client} onFetchSource={onFetchSource} onTestSource={onTestSource} refreshVersion={refreshVersion} />
  if (activeNav === 'methods') return <MethodsPage client={client} permissions={permissions} refreshVersion={refreshVersion} />
  if (activeNav === 'receive') return <RawRecordsPage title="接口接收记录" origin="receive" client={client} onFetchSource={onFetchSource} />
  if (activeNav === 'pull_records') return <RawRecordsPage title="数据拉取记录" origin="pull" client={client} onFetchSource={onFetchSource} />
  if (activeNav === 'backfill') return <BojunBackfillPage client={client} loading={loading || refreshing} onCompletedRefresh={onRefresh} />
  if (activeNav === 'youzan_distribution') return <YouzanDistributionPage client={client} task={legacyTasks.find((item) => item.code === 'youzan_distribution_order_fetch')} loading={loading || refreshing} onCompletedRefresh={onRefresh} />
  if (activeNav === 'rules') return <RulesPage client={client} rules={transformRules} sources={sources} onRulesChange={setTransformRules} refreshVersion={refreshVersion} />
  if (activeNav === 'processed') return <ProcessedRecordsPage client={client} />
  if (activeNav === 'destinations') return <DestinationsPage client={client} refreshVersion={refreshVersion} />
  if (activeNav === 'tasks') return <DeliveryTasksPage client={client} canManage={canManageDelivery} sources={sources} destinations={destinations} onRefresh={onRefresh} refreshVersion={refreshVersion} />
  if (activeNav === 'push_policy') return <PushPolicyPage client={client} canManage={canManageDelivery} refreshVersion={refreshVersion} />
  if (activeNav === 'excel_workflow') return <ExcelWorkflowPage client={client} token={token} permissions={permissions} refreshVersion={refreshVersion} setLoading={setLoading} setResult={setResult} onNavigateToJobs={() => navigate('excel_jobs')} />
  if (activeNav === 'excel_oracle_import' || activeNav === 'excel_procedure') return <OracleMethodPage key={activeNav} operation={activeNav === 'excel_oracle_import' ? 'oracle_import' : 'oracle_procedure'} client={client} token={token} permissions={permissions} refreshVersion={refreshVersion} setLoading={setLoading} setResult={setResult} onNavigateToJobs={() => navigate('excel_jobs')} />
  if (activeNav === 'excel_export') return <OracleExportPage client={client} token={token} loading={loading} refreshVersion={refreshVersion} canExecute={permissions.includes('excel.execute')} canManage={permissions.includes('excel.manage')} setLoading={setLoading} setResult={setResult} onNavigateToJobs={() => navigate('excel_jobs')} />
  if (activeNav === 'excel_jobs' || activeNav === 'excel_schemes' || activeNav === 'excel_write') {
    return <ExcelMatchPage section={activeNav === 'excel_jobs' ? 'jobs' : activeNav === 'excel_schemes' ? 'schemes' : 'write'} client={client} token={token} loading={loading} refreshVersion={refreshVersion} setLoading={setLoading} setResult={setResult} onNavigateToJobs={() => navigate('excel_jobs')} />
  }
  return null
}
