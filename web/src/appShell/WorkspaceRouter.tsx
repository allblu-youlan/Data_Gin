import { lazy, Suspense, type Dispatch, type SetStateAction } from 'react'
import type { LegacyTask } from '../backfillPages/youzanDistributionSupport'
import type { TransformRule } from '../configurationPages/ruleContracts'
import type { DestinationDefinition, SourceDefinition } from '../configurationPages/types'
import type { DataStatisticsSummary, HealthSummary, MallWeatherMetricsSummary } from '../monitoring'
import type { MentionSection } from '../officeMention/types'
import type { DeliveryLog, PipelineRun } from '../monitoringPages/types'
import type { ApiRequestOptions, ClientResponse, HTTPMethod } from '../api/client'
import { FeedbackState, PageCanvas } from '../ui'
import { reportCenterNavKey, reportCenterSection, type NavKey } from './navigation'
import { canViewNavigationItem } from './navigationPermissions'

const AccessManagementPage = lazy(() => import('../AccessManagementPage').then((module) => ({ default: module.AccessManagementPage })))
const BojunBackfillPage = lazy(() => import('../backfillPages/BojunBackfillPage').then((module) => ({ default: module.BojunBackfillPage })))
const YouzanDistributionPage = lazy(() => import('../backfillPages/YouzanDistributionPage').then((module) => ({ default: module.YouzanDistributionPage })))
const BusinessOverviewPage = lazy(() => import('../businessOverviewPages/BusinessOverviewPage').then((module) => ({ default: module.BusinessOverviewPage })))
const PassengerFlowPage = lazy(() => import('../passengerFlow/PassengerFlowPage').then((module) => ({ default: module.PassengerFlowPage })))
const SalesSummaryPage = lazy(() => import('../salesSummary/SalesSummaryPage').then((module) => ({ default: module.SalesSummaryPage })))
const DeliveryTasksPage = lazy(() => import('../configurationPages/DeliveryTasksPage/DeliveryTasksPage').then((module) => ({ default: module.DeliveryTasksPage })))
const DestinationsPage = lazy(() => import('../configurationPages/DestinationsPage/DestinationsPage').then((module) => ({ default: module.DestinationsPage })))
const MethodsPage = lazy(() => import('../configurationPages/MethodsPage/MethodsPage').then((module) => ({ default: module.MethodsPage })))
const PushPolicyPage = lazy(() => import('../configurationPages/PushPolicyPage/PushPolicyPage').then((module) => ({ default: module.PushPolicyPage })))
const RulesPage = lazy(() => import('../configurationPages/RulesPage/RulesPage').then((module) => ({ default: module.RulesPage })))
const SourcesPage = lazy(() => import('../configurationPages/SourcesPage/SourcesPage').then((module) => ({ default: module.SourcesPage })))
const ProcessedRecordsPage = lazy(() => import('../dataPages/ProcessedRecordsPage').then((module) => ({ default: module.ProcessedRecordsPage })))
const RawRecordsPage = lazy(() => import('../dataPages/RawRecordsPage').then((module) => ({ default: module.RawRecordsPage })))
const ExcelMatchPage = lazy(() => import('../excelPages/ExcelMatchPage').then((module) => ({ default: module.ExcelMatchPage })))
const ExcelWorkflowPage = lazy(() => import('../excelPages/ExcelWorkflowPage').then((module) => ({ default: module.ExcelWorkflowPage })))
const OracleMethodPage = lazy(() => import('../excelPages/OracleMethodPage').then((module) => ({ default: module.OracleMethodPage })))
const OracleExportPage = lazy(() => import('../excelPages/OracleExportPage').then((module) => ({ default: module.OracleExportPage })))
const MallWeatherPage = lazy(() => import('../MallWeatherPage').then((module) => ({ default: module.MallWeatherPage })))
const StoreInfoPage = lazy(() => import('../MallWeatherPage').then((module) => ({ default: module.StoreInfoPage })))
const DeliveryLogsPage = lazy(() => import('../monitoringPages/DeliveryLogsPage/DeliveryLogsPage').then((module) => ({ default: module.DeliveryLogsPage })))
const PipelineRunsPage = lazy(() => import('../monitoringPages/PipelineRunsPage/PipelineRunsPage').then((module) => ({ default: module.PipelineRunsPage })))
const RunOverviewPage = lazy(() => import('../monitoringPages/RunOverviewPage/RunOverviewPage').then((module) => ({ default: module.RunOverviewPage })))
const StepRunsPage = lazy(() => import('../monitoringPages/StepRunsPage/StepRunsPage').then((module) => ({ default: module.StepRunsPage })))
const OfficeMentionPage = lazy(() => import('../officeMention/OfficeMentionPage').then((module) => ({ default: module.OfficeMentionPage })))
const MessageManagementPage = lazy(() => import('../officeMessage/MessageManagementPage').then((module) => ({ default: module.MessageManagementPage })))
const PushManagementPage = lazy(() => import('../officeMessage/PushManagementPage').then((module) => ({ default: module.PushManagementPage })))
const TableEmailNotificationsPage = lazy(() => import('../tableEmailNotifications/TableEmailNotificationsPage').then((module) => ({ default: module.TableEmailNotificationsPage })))
const ReportCenter = lazy(() => import('../reportCenter/ReportCenter').then((module) => ({ default: module.ReportCenter })))

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
  username: string
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
  onLogout: () => void
  onOpenNavigation: () => void
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
  return <Suspense fallback={<PageCanvas><FeedbackState kind="loading" title="正在加载页面" /></PageCanvas>}>
    <WorkspacePage {...props} />
  </Suspense>
}

function WorkspacePage(props: WorkspaceRouterProps) {
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
  if (activeNav === 'sales_summary') return <SalesSummaryPage
    client={client}
    username={props.username}
    onLogout={props.onLogout}
    onOpenNavigation={props.onOpenNavigation}
    onNavigate={(target) => navigate(target === 'business-overview' ? 'business_overview' : target === 'store-info' ? 'store_info' : 'report_query')}
  />
  if (activeNav === 'overview') return <RunOverviewPage runs={runs} deliveryLogs={deliveryLogs} monitoring={monitoring} stale={monitoringStale} overviewTotals={overviewTotals} onLoadSteps={onLoadSteps} />
  if (activeNav === 'runs') return <PipelineRunsPage client={client} pipelines={pipelines} onLoadSteps={onLoadSteps} onPipelineRunCompleted={() => void onRefresh()} refreshVersion={refreshVersion} />
  if (activeNav === 'delivery_logs') return <DeliveryLogsPage client={client} onRetryLog={onRetryDeliveryLog} />
  if (activeNav === 'step_runs') return <StepRunsPage client={client} focusRunID={stepRunFocusID} />
  if (activeNav === 'store_info') return <StoreInfoPage actorID={actorID} client={client} downloadFile={downloadFile} />
  if (activeNav === 'mall_weather') return <MallWeatherPage actorID={actorID} client={client} downloadFile={downloadFile} />
  if (activeNav === 'passenger_flow') return <PassengerFlowPage client={client} canManage={permissions.includes('data.manage')} />
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
