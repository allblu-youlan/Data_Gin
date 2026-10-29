import { formatShanghaiDate } from '../dateTime.js'
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import type { ClientResponse } from '../api/client'
import type { LegacyTask } from '../backfillPages/youzanDistributionSupport'
import type { TransformRule } from '../configurationPages/ruleContracts'
import type { DestinationDefinition, SourceDefinition } from '../configurationPages/types'
import { parseDataStatisticsSummary, parseHealthSummary, parseMallWeatherMetricsSummary } from '../monitoring'
import { parseDeliveryLog, parsePipelineRun } from '../monitoringPages/contracts'
import type { DeliveryLog, PipelineRun } from '../monitoringPages/types'
import { parseMonitoringPage } from '../monitoringRecords'
import { pipelineListPath } from '../pipelineRun'
import type { NavKey } from './navigation'
import type { ConsoleSessionState } from './useConsoleSession'
import type { MonitoringSnapshot, PipelineDefinition, WorkspaceApiClient } from './WorkspaceRouter'
import { canViewNavigationItem } from './navigationPermissions'
import { canCommitOverviewResponse, overviewRequestPlan, restrictOverviewData, type OverviewDataState } from './overviewWorkspacePolicy'

type WorkspaceOverviewData = OverviewDataState<
  PipelineRun,
  DeliveryLog,
  MonitoringSnapshot['statistics'],
  MonitoringSnapshot['weather'],
  MonitoringSnapshot['health']
>

function emptyOverviewData(): WorkspaceOverviewData {
  return {
    runs: [],
    deliveryLogs: [],
    overviewTotals: { runs: null, deliveryLogs: null },
    monitoring: { statistics: null, weather: null, health: null },
  }
}

export function useWorkspaceData(
  activeNav: NavKey,
  client: WorkspaceApiClient,
  token: string,
  sessionState: ConsoleSessionState,
  permissions: readonly string[],
  setResult: Dispatch<SetStateAction<ClientResponse | null>>,
) {
  const [refreshing, setRefreshing] = useState(false)
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [workspaceError, setWorkspaceError] = useState('')
  const [overviewData, setOverviewData] = useState<WorkspaceOverviewData>(emptyOverviewData)
  const [stepRunFocusID, setStepRunFocusID] = useState<number | null>(null)
  const requestRef = useRef<AbortController | null>(null)
  const monitoringRequestRef = useRef<AbortController | null>(null)
  const [pipelines, setPipelines] = useState<PipelineDefinition[]>([])
  const [sources, setSources] = useState<SourceDefinition[]>([])
  const [transformRules, setTransformRules] = useState<TransformRule[]>([])
  const [destinations, setDestinations] = useState<DestinationDefinition[]>([])
  const [monitoringStale, setMonitoringStale] = useState(false)
  const [legacyTasks, setLegacyTasks] = useState<LegacyTask[]>([])

  const refresh = useCallback(async (showResult = false) => {
    if (!token || !canViewNavigationItem(activeNav, permissions)) return
    requestRef.current?.abort()
    const controller = new AbortController()
    requestRef.current = controller
    setRefreshing(true)
    setWorkspaceError('')
    try {
      const get = (path: string) => client(path, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
      if (activeNav === 'overview') {
        const plan = overviewRequestPlan(permissions, monitoringDayStartTime())
        const [runResult, logResult] = await Promise.all([
          plan.runs ? get(plan.runs) : null,
          plan.deliveryLogs ? get(plan.deliveryLogs) : null,
        ])
        if (canCommitOverviewResponse(controller.signal)) {
          const runPage = runResult?.ok ? parseMonitoringPage<unknown>(runResult.data, 'runs') : null
          const parsedRuns = runPage?.list.map(parsePipelineRun) ?? []
          if (runPage && parsedRuns.every((run): run is PipelineRun => run !== null)) {
            setOverviewData((current) => ({
              ...current,
              runs: parsedRuns,
              overviewTotals: { ...current.overviewTotals, runs: runPage.pagination.total },
            }))
          } else if (runResult?.ok) {
            setOverviewData((current) => ({
              ...current,
              runs: readList<PipelineRun>(runResult, 'runs'),
              overviewTotals: { ...current.overviewTotals, runs: null },
            }))
          }
          const logPage = logResult?.ok ? parseMonitoringPage<unknown>(logResult.data, 'logs') : null
          const parsedLogs = logPage?.list.map(parseDeliveryLog) ?? []
          if (logPage && parsedLogs.every((log): log is DeliveryLog => log !== null)) {
            setOverviewData((current) => ({
              ...current,
              deliveryLogs: parsedLogs,
              overviewTotals: { ...current.overviewTotals, deliveryLogs: logPage.pagination.total },
            }))
          } else if (logResult?.ok) {
            setOverviewData((current) => ({
              ...current,
              deliveryLogs: readList<DeliveryLog>(logResult, 'logs'),
              overviewTotals: { ...current.overviewTotals, deliveryLogs: null },
            }))
          }
        }
      } else if (activeNav === 'runs') {
        const response = await get(pipelineListPath())
        if (!controller.signal.aborted) {
          if (response.ok) setPipelines(readList<PipelineDefinition>(response, 'pipelines'))
          else setWorkspaceError('可执行流水线加载失败，已保留上一次成功数据。')
        }
      } else if (activeNav === 'rules') {
        const response = await get('/v1/sources')
        if (!controller.signal.aborted) {
          if (response.ok) setSources(readList<SourceDefinition>(response, 'sources'))
          else setWorkspaceError('规则来源加载失败，已保留上一次成功数据。')
        }
      } else if (activeNav === 'tasks') {
        const [sourceResult, destinationResult] = await Promise.all([get('/v1/sources'), get('/v1/destinations')])
        if (!controller.signal.aborted) {
          if (sourceResult.ok) setSources(readList<SourceDefinition>(sourceResult, 'sources'))
          if (destinationResult.ok) setDestinations(readList<DestinationDefinition>(destinationResult, 'destinations'))
          if (!sourceResult.ok || !destinationResult.ok) setWorkspaceError('推送任务关联配置加载不完整，已保留上一次成功数据。')
        }
      } else if (activeNav === 'youzan_distribution') {
        const response = await get('/v1/legacy-tasks')
        if (!controller.signal.aborted && response.ok) setLegacyTasks(readList<LegacyTask>(response, 'tasks'))
      }
      if (!controller.signal.aborted && showResult) {
        setResult({ ok: true, status: 200, data: { refreshed_at: new Date().toISOString() } })
      }
    } finally {
      if (requestRef.current === controller) {
        requestRef.current = null
        setRefreshing(false)
        if (!controller.signal.aborted) setRefreshVersion((version) => version + 1)
      }
    }
  }, [activeNav, client, permissions, setResult, token])

  useLayoutEffect(() => {
    requestRef.current?.abort()
    monitoringRequestRef.current?.abort()
    setOverviewData(emptyOverviewData())
    setStepRunFocusID(null)
    setPipelines([])
    setSources([])
    setTransformRules([])
    setDestinations([])
    setMonitoringStale(false)
    setLegacyTasks([])
    setWorkspaceError('')
  }, [token])

  useLayoutEffect(() => {
    requestRef.current?.abort()
    monitoringRequestRef.current?.abort()
    setOverviewData((current) => restrictOverviewData(current, permissions, activeNav === 'overview'))
    setMonitoringStale(false)
  }, [activeNav, permissions])

  useEffect(() => {
    if (sessionState === 'authenticated') void refresh(false)
    return () => requestRef.current?.abort()
  }, [refresh, sessionState])

  useEffect(() => {
    if (sessionState !== 'authenticated' || activeNav !== 'overview') return
    const controller = new AbortController()
    monitoringRequestRef.current = controller
    const plan = overviewRequestPlan(permissions, monitoringDayStartTime())
    void Promise.all([
      plan.statistics ? client(plan.statistics, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true }) : null,
      plan.weather ? client(plan.weather, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true }) : null,
      client(plan.health, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptBareJSONSuccess: true }),
    ]).then(([statisticsResponse, weatherResponse, healthResponse]) => {
      if (!canCommitOverviewResponse(controller.signal)) return
      const nextStatistics = statisticsResponse?.ok ? parseDataStatisticsSummary(statisticsResponse.data) : null
      const nextWeather = weatherResponse?.ok ? parseMallWeatherMetricsSummary(weatherResponse.data) : null
      const nextHealth = healthResponse.ok ? parseHealthSummary(healthResponse.data) : null
      setOverviewData((current) => ({
        ...current,
        monitoring: {
          statistics: statisticsResponse ? nextStatistics ?? current.monitoring.statistics : null,
          weather: weatherResponse ? nextWeather ?? current.monitoring.weather : null,
          health: nextHealth ?? current.monitoring.health,
        },
      }))
      setMonitoringStale(Boolean(statisticsResponse && !nextStatistics || weatherResponse && !nextWeather || !nextHealth))
    }).finally(() => {
      if (monitoringRequestRef.current === controller) monitoringRequestRef.current = null
    })
    return () => {
      controller.abort()
      if (monitoringRequestRef.current === controller) monitoringRequestRef.current = null
    }
  }, [activeNav, client, permissions, sessionState, token])

  const { deliveryLogs, monitoring, overviewTotals, runs } = overviewData

  return {
    deliveryLogs, destinations, legacyTasks, monitoring, monitoringStale, overviewTotals, pipelines,
    refresh, refreshing, refreshVersion, runs, setStepRunFocusID, setTransformRules, sources,
    stepRunFocusID, transformRules, workspaceError,
  }
}

function readList<T>(result: { data: unknown }, key: string): T[] {
  const value = readDataField(result.data, key)
  return Array.isArray(value) ? (value as T[]) : []
}

function readDataField(data: unknown, key: string) {
  if (!data || typeof data !== 'object') return undefined
  return (data as { data?: Record<string, unknown> }).data?.[key]
}

function monitoringDayStartTime() {
  return `${formatShanghaiDate(new Date())}T00:00`
}
