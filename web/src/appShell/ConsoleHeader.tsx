import type { RefObject } from 'react'
import { CalendarDays, LogOut, RefreshCcw } from 'lucide-react'
import type { SessionUser } from '../api/auth'
import { WorkspaceHeader } from '../ui'
import type { NavKey } from './navigation'
import styles from './ConsoleHeader.module.css'

interface ConsoleHeaderProps {
  activeNav: NavKey
  compact: boolean
  loading: boolean
  mobileNavTriggerRef: RefObject<HTMLButtonElement>
  refreshing: boolean
  sessionUser: SessionUser | null
  onLogout: () => void
  onOpenNavigation: () => void
  onRefresh: () => void
}

const titles: Record<NavKey, { title: string; subtitle: string }> = {
  business_overview: { title: '营业概况', subtitle: '查看销售日结、收款构成与门店对账记录。' },
  overview: { title: '运行总览', subtitle: '只看当前运行与交付健康度，快速定位失败。' },
  runs: { title: '流水线运行', subtitle: '按状态、运行类型和 Trace ID 查询执行记录。' },
  delivery_logs: { title: '推送日志', subtitle: '按成功状态、门店和业务键查询外部交付结果。' },
  step_runs: { title: '步骤运行', subtitle: '选择一次流水线运行并查看每个步骤的输入输出。' },
  store_info: { title: '店铺信息', subtitle: '统一维护店铺资料、地址与天气服务坐标。' },
  mall_weather: { title: '商场天气', subtitle: '查看商场中心点实况、未来降水、小时趋势和气象预警。' },
  report_catalog: { title: '报表目录', subtitle: '查看报表定义、发布状态与当前版本。' },
  report_configuration: { title: '报表配置', subtitle: '维护 MySQL 中的过程、形参、字段和权限契约。' },
  report_query: { title: '报表中心', subtitle: '选择已上线且已授权的报表，生成并下载 Excel。' },
  report_exports: { title: '导出中心', subtitle: '查看 Excel 生成、下载和结果清理状态。' },
  office_messages: { title: '消息管理', subtitle: '维护文本消息和 Oracle Excel 消息来源。' },
  office_push: { title: '推送管理', subtitle: '维护飞书与 WebDAV 推送目标并查看运行记录。' },
  access_management: { title: '账号与权限', subtitle: '管理控制台账号、角色权限矩阵、开放 API 和变更审计。' },
  sources: { title: '数据源', subtitle: '查询数据接入配置、类型和启用状态。' },
  receive: { title: '接口接收', subtitle: '查询外部系统主动推送进来的原始数据。' },
  pull_records: { title: '拉取记录', subtitle: '查询系统主动从外部接口拉取的数据。' },
  backfill: { title: '伯俊补拉', subtitle: '先预览、再确认写入指定时间范围的伯俊订单。' },
  youzan_distribution: { title: '有赞分销订单', subtitle: '查看每日自动任务，并按时间范围提交异步补拉。' },
  rules: { title: '清洗规则', subtitle: '查询规则类型、来源、顺序和启用状态。' },
  processed: { title: '处理结果', subtitle: '按业务键、类型和质量状态查询处理后数据。' },
  methods: { title: '方法目录', subtitle: '查询已配置方法和系统内置能力。' },
  destinations: { title: '推送目标', subtitle: '查询目标系统和接口配置。' },
  tasks: { title: '推送任务', subtitle: '查询交付任务、触发方式和目标关系。' },
  push_policy: { title: '推送策略', subtitle: '配置各具体推送目标的订单跳过周期。' },
  excel_jobs: { title: 'Excel 任务', subtitle: '查询任务状态、进度、日志和下载结果。' },
  excel_schemes: { title: 'Excel 多步骤匹配', subtitle: '配置数据库表、字段和顺序匹配步骤。' },
  excel_write: { title: 'Excel 写入', subtitle: '执行导入更新与退回未匹配操作。' },
}

export function ConsoleHeader({
  activeNav,
  compact,
  loading,
  mobileNavTriggerRef,
  refreshing,
  sessionUser,
  onLogout,
  onOpenNavigation,
  onRefresh,
}: ConsoleHeaderProps) {
  return (
    <WorkspaceHeader
      title={compact ? undefined : titles[activeNav].title}
      description={compact ? undefined : titles[activeNav].subtitle}
      context={compact ? headerContext(activeNav) : undefined}
      menuButtonRef={mobileNavTriggerRef}
      onOpenNavigation={onOpenNavigation}
      actions={(
        <div className={styles.session}>
          {activeNav !== 'store_info' ? (
            <span className={styles.date}>
              <CalendarDays aria-hidden="true" />
              {new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', timeZone: 'Asia/Shanghai' }).format(new Date())}
            </span>
          ) : null}
          {sessionUser ? <span className={styles.user}>{sessionUser.nickname || sessionUser.account}</span> : null}
          <button className={styles.refresh} type="button" onClick={onRefresh} disabled={refreshing}>
            <RefreshCcw aria-hidden="true" />{refreshing ? '刷新中' : '刷新数据'}
          </button>
          <button className={styles.logout} type="button" onClick={onLogout}>
            <LogOut aria-hidden="true" />退出登录
          </button>
          <span className={`${styles.health} ${loading ? styles.loading : ''}`}>
            {loading ? '数据加载中' : '系统正常'}
          </span>
        </div>
      )}
    />
  )
}

function headerContext(activeNav: NavKey) {
  if (activeNav === 'business_overview') return 'STORE OPERATIONS'
  if (activeNav === 'access_management') return 'ACCESS CONTROL'
  if (['sources', 'rules', 'destinations'].includes(activeNav)) return 'DATA CONFIGURATION'
  if (['overview', 'runs', 'delivery_logs', 'step_runs'].includes(activeNav)) return 'OPERATIONS'
  if (['office_messages', 'office_push'].includes(activeNav)) return 'OFFICE MESSAGING'
  return 'REPORT CENTER'
}
