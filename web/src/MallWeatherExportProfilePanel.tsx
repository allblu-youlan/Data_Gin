import { formatShanghaiDateTime } from './dateTime.js'
import { Plus, RefreshCcw, Save, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { DataTable } from './ui'
import {
  emptyMallWeatherExportProfileForm,
  mallWeatherExportProfileDatasetKinds,
  mallWeatherExportProfileForm,
  mallWeatherExportProfileListPath,
  mallWeatherExportProfileReadOnly,
  mallWeatherExportProfileSaveRequest,
  newMallWeatherExportProfileDataset,
  parseMallWeatherExportProfile,
  parseMallWeatherExportProfilePage,
  type MallWeatherExportProfile,
  type MallWeatherExportProfileDataset,
  type MallWeatherExportProfileForm,
} from './mallWeatherExportProfiles'
import styles from './MallWeatherExportProfilePanel.module.css'

type ProfileApiResult = { ok: boolean; status: number; data: unknown }
type ProfileApiClient = (path: string, options: {
  method: 'GET' | 'POST'
  body?: unknown
  showResult: false
  silentLoading: true
  signal?: AbortSignal
}) => Promise<ProfileApiResult>

export function MallWeatherExportProfilePanel({ client }: { client: ProfileApiClient }) {
  const [enabledFilter, setEnabledFilter] = useState<'' | 'true' | 'false'>('')
  const [profiles, setProfiles] = useState<MallWeatherExportProfile[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [listForbidden, setListForbidden] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [editing, setEditing] = useState<MallWeatherExportProfile | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState<MallWeatherExportProfileForm>(emptyMallWeatherExportProfileForm)
  const requestSequence = useRef(0)
  const controllerRef = useRef<AbortController | null>(null)
  const saveControllerRef = useRef<AbortController | null>(null)

  const loadProfiles = useCallback(async (cursor = '', append = false) => {
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const sequence = ++requestSequence.current
    setLoading(true)
    setError('')
    try {
      const response = await client(mallWeatherExportProfileListPath(enabledFilter, cursor), {
        method: 'GET', showResult: false, silentLoading: true, signal: controller.signal,
      })
      if (controller.signal.aborted || sequence !== requestSequence.current) return
      if (!response.ok) {
        setListForbidden(response.status === 403)
        setError(profileError(response.status, '读取天气导出 Profile 失败', '当前账号缺少 weather.export 权限'))
        return
      }
      const page = parseMallWeatherExportProfilePage(response.data)
      if (!page) {
        setError('天气导出 Profile 响应格式不正确，请联系管理员。')
        return
      }
      setListForbidden(false)
      setProfiles((current) => append ? mergeProfiles(current, page.items) : page.items)
      setNextCursor(page.nextCursor)
    } catch {
      if (!controller.signal.aborted && sequence === requestSequence.current) setError('读取天气导出 Profile 失败，请检查网络后重试。')
    } finally {
      if (!controller.signal.aborted && sequence === requestSequence.current) setLoading(false)
    }
  }, [client, enabledFilter])

  useEffect(() => {
    void loadProfiles()
    return () => {
      controllerRef.current?.abort()
      saveControllerRef.current?.abort()
    }
  }, [loadProfiles])

  function startCreate() {
    setEditing(null)
    setForm(emptyMallWeatherExportProfileForm())
    setShowForm(true)
    setError('')
    setMessage('')
  }

  function startEdit(profile: MallWeatherExportProfile) {
    if (mallWeatherExportProfileReadOnly(profile.code)) return
    setEditing(profile)
    setForm(mallWeatherExportProfileForm(profile))
    setShowForm(true)
    setError('')
    setMessage('')
  }

  function cancelEdit() {
    setEditing(null)
    setForm(emptyMallWeatherExportProfileForm())
    setShowForm(false)
  }

  function update(field: Exclude<keyof MallWeatherExportProfileForm, 'datasets'>, value: string | boolean) {
    setForm((current) => ({ ...current, [field]: value }))
  }

  function updateDataset(index: number, field: keyof MallWeatherExportProfileDataset, value: string | boolean) {
    setForm((current) => ({
      ...current,
      datasets: current.datasets.map((dataset, currentIndex) => currentIndex === index
        ? { ...dataset, [field]: value }
        : dataset),
    }))
  }

  function replaceDatasetKind(index: number, kind: string) {
    const sheetName = datasetLabel(kind)
    setForm((current) => ({
      ...current,
      datasets: current.datasets.map((dataset, currentIndex) => currentIndex === index
        ? { ...newMallWeatherExportProfileDataset(kind, sheetName) }
        : dataset),
    }))
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (saving || listForbidden || (editing && mallWeatherExportProfileReadOnly(editing.code))) return
    let body: ReturnType<typeof mallWeatherExportProfileSaveRequest>
    try {
      body = mallWeatherExportProfileSaveRequest(form, editing?.version)
    } catch {
      setError('请检查 Profile 编码、筛选条件、时间范围和数据集配置。')
      return
    }
    saveControllerRef.current?.abort()
    const controller = new AbortController()
    saveControllerRef.current = controller
    setSaving(true)
    setError('')
    setMessage('')
    try {
      const response = await client('/v1/weather-export-profiles', {
        method: 'POST', body, showResult: false, silentLoading: true, signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (!response.ok) {
        if (response.status === 409) {
          setError('Profile 已被其他管理员更新；已保留当前草稿，请刷新列表后对照最新版本再保存。')
          void loadProfiles()
          return
        }
        setError(profileError(response.status, '保存天气导出 Profile 失败', '当前账号缺少 weather.config.manage 权限'))
        return
      }
      const saved = parseMallWeatherExportProfile(response.data)
      if (!saved) {
        setError('保存结果格式不正确，请刷新列表确认是否已生效。')
        return
      }
      setMessage(response.status === 201 ? '已创建天气导出 Profile。' : '已更新天气导出 Profile。')
      setEditing(saved)
      setForm(mallWeatherExportProfileForm(saved))
      void loadProfiles()
    } catch {
      if (!controller.signal.aborted) setError('保存请求未完成，请刷新列表确认结果后再尝试。')
    } finally {
      if (saveControllerRef.current === controller) {
        saveControllerRef.current = null
        if (!controller.signal.aborted) setSaving(false)
      }
    }
  }

  const formOpen = showForm
  return <section className={[styles['workbench-panel'], styles['mall-weather-export-profiles']].join(' ')} aria-busy={loading || saving}>
    <div className={styles['mall-weather-section-title']}>
      <div><strong>天气导出 Profile</strong><span>维护可复用导出配置；系统固定 Profile 仅供读取，不能在此修改。</span></div>
      <div className={styles['mall-weather-inline-actions']}>
        <button type="button" onClick={() => void loadProfiles()} disabled={loading || saving}><RefreshCcw aria-hidden="true" />刷新</button>
        <button className={styles['primary']} type="button" onClick={startCreate} disabled={listForbidden || saving}><Plus aria-hidden="true" />新建 Profile</button>
      </div>
    </div>

    <form className={styles['mall-weather-profile-filter']} onSubmit={(event) => { event.preventDefault(); void loadProfiles() }}>
      <label><span>启用状态</span><select name="mallWeatherProfileEnabledFilter" value={enabledFilter} onChange={(event) => setEnabledFilter(event.currentTarget.value as '' | 'true' | 'false')} disabled={loading || saving}><option value="">全部</option><option value="true">已启用</option><option value="false">已停用</option></select></label>
      <button type="submit" disabled={loading || saving}>查询</button>
    </form>

    {profiles.length > 0 && <DataTable density="compact" minWidth={760} scrollLabel="天气导出 Profile 列表"><caption>天气导出 Profile 列表</caption><thead><tr><th scope="col">名称</th><th scope="col">编码</th><th scope="col">版本</th><th scope="col">状态</th><th scope="col">数据集</th><th scope="col">更新时间</th><th scope="col">操作</th></tr></thead><tbody>{profiles.map((profile) => <tr key={profile.id}><td>{profile.name}</td><td>{profile.code}</td><td>{profile.version}</td><td>{profile.enabled ? '已启用' : '已停用'}</td><td>{profile.datasets.length}</td><td>{formatDate(profile.updatedAt)}</td><td><button type="button" onClick={() => startEdit(profile)} disabled={saving || mallWeatherExportProfileReadOnly(profile.code)}>{mallWeatherExportProfileReadOnly(profile.code) ? '系统固定' : '编辑'}</button></td></tr>)}</tbody></DataTable>}
    {!loading && !error && profiles.length === 0 && <p className={styles['mall-weather-action-message']} role="status">暂无符合条件的天气导出 Profile。</p>}
    {nextCursor && <button type="button" onClick={() => void loadProfiles(nextCursor, true)} disabled={loading || saving}>加载更多</button>}

    {formOpen && <form className={styles['mall-weather-profile-form']} onSubmit={save}>
      <div className={styles['mall-weather-section-title']}><div><strong>{editing ? `编辑 Profile · v${editing.version}` : '新建 Profile'}</strong><span>保存为完整配置；更新将采用乐观锁，避免覆盖他人修改。</span></div><button type="button" onClick={cancelEdit} disabled={saving} aria-label="关闭 Profile 编辑"><X aria-hidden="true" />关闭</button></div>
      <label><span>Profile 编码 *</span><input value={form.code} onChange={(event) => update('code', event.currentTarget.value)} pattern="[a-z][a-z0-9_-]{2,99}" required disabled={Boolean(editing) || saving} /><small>3–100 个小写字母、数字、下划线或连字符；创建后不可改名。</small></label>
      <label><span>名称 *</span><input value={form.name} onChange={(event) => update('name', event.currentTarget.value)} maxLength={255} required disabled={saving} /></label>
      <label><span>时区 *</span><input value={form.timeZone} onChange={(event) => update('timeZone', event.currentTarget.value)} maxLength={128} required disabled={saving} placeholder="Asia/Shanghai" /></label>
      <label><span>单位制</span><select value={form.unitSystem} onChange={(event) => update('unitSystem', event.currentTarget.value as 'metric' | 'imperial')} disabled={saving}><option value="metric">公制</option><option value="imperial">英制</option></select></label>
      <label><span>日期格式</span><input value={form.dateFormat} onChange={(event) => update('dateFormat', event.currentTarget.value)} maxLength={64} required disabled={saving} /></label>
      <label><span>日期时间格式</span><input value={form.dateTimeFormat} onChange={(event) => update('dateTimeFormat', event.currentTarget.value)} maxLength={64} required disabled={saving} /></label>
      <label><span>文件名模板 *</span><input value={form.fileNameTemplate} onChange={(event) => update('fileNameTemplate', event.currentTarget.value)} maxLength={255} required disabled={saving} /><small>必须以 .xlsx 结尾，不能包含路径字符。</small></label>
      <label className={styles['mall-weather-checkbox']}><input type="checkbox" checked={form.enabled} onChange={(event) => update('enabled', event.currentTarget.checked)} disabled={saving} />启用此 Profile</label>

      <details className={styles['mall-weather-profile-details']}><summary>筛选条件</summary><div className={[styles['mall-weather-profile-form'], styles['mall-weather-profile-nested']].join(' ')}><label><span>商场 ID（逗号分隔）</span><input value={form.mallIds} onChange={(event) => update('mallIds', event.currentTarget.value)} disabled={saving} inputMode="numeric" /></label><label><span>城市（逗号分隔）</span><input value={form.cities} onChange={(event) => update('cities', event.currentTarget.value)} disabled={saving} /></label><label><span>商场状态</span><input value={form.mallStatuses} onChange={(event) => update('mallStatuses', event.currentTarget.value)} disabled={saving} placeholder="draft,active,disabled" /></label><label><span>质量状态</span><input value={form.qualityStatuses} onChange={(event) => update('qualityStatuses', event.currentTarget.value)} disabled={saving} placeholder="valid,warning" /></label><label><span>开始时间（RFC3339）</span><input value={form.start} onChange={(event) => update('start', event.currentTarget.value)} disabled={saving} placeholder="2026-01-02T03:04:05Z" /></label><label><span>结束时间（RFC3339）</span><input value={form.end} onChange={(event) => update('end', event.currentTarget.value)} disabled={saving} placeholder="2026-01-03T03:04:05Z" /></label></div></details>

      <div className={styles['mall-weather-profile-datasets']}><div><strong>数据集 *</strong><span>未指定列时由服务端使用该数据集的默认列。</span></div>{form.datasets.map((dataset, index) => <div className={styles['mall-weather-profile-dataset']} key={`${dataset.kind}-${index}`}><label><span>类型</span><select value={dataset.kind} onChange={(event) => replaceDatasetKind(index, event.currentTarget.value)} disabled={saving}>{mallWeatherExportProfileDatasetKinds.map((kind) => <option value={kind} key={kind}>{datasetLabel(kind)}</option>)}</select></label><label><span>工作表名称 *</span><input value={dataset.sheetName} onChange={(event) => updateDataset(index, 'sheetName', event.currentTarget.value)} maxLength={31} required disabled={saving} /></label><label className={styles['mall-weather-checkbox']}><input type="checkbox" checked={dataset.freezeHeader} onChange={(event) => updateDataset(index, 'freezeHeader', event.currentTarget.checked)} disabled={saving} />冻结表头</label><label className={styles['mall-weather-checkbox']}><input type="checkbox" checked={dataset.autoFilter} onChange={(event) => updateDataset(index, 'autoFilter', event.currentTarget.checked)} disabled={saving} />自动筛选</label><button type="button" onClick={() => setForm((current) => ({ ...current, datasets: current.datasets.filter((_, currentIndex) => currentIndex !== index) }))} disabled={saving || form.datasets.length === 1}>移除</button></div>)}<button type="button" onClick={() => setForm((current) => current.datasets.length >= 8 ? current : ({ ...current, datasets: [...current.datasets, newMallWeatherExportProfileDataset()] }))} disabled={saving || form.datasets.length >= 8}><Plus aria-hidden="true" />添加数据集</button></div>
      <div className={styles['mall-weather-form-actions']}><button className={styles['primary']} type="submit" disabled={saving || listForbidden}><Save aria-hidden="true" />{saving ? '保存中' : editing ? '保存更新' : '创建 Profile'}</button></div>
    </form>}
    {message && <p className={styles['mall-weather-action-message']} role="status">{message}</p>}
    {error && <p className={[styles['mall-weather-action-message'], styles['error']].join(' ')} role="alert">{error}</p>}
  </section>
}

function mergeProfiles(current: MallWeatherExportProfile[], received: MallWeatherExportProfile[]) {
  const merged = new Map(current.map((profile) => [profile.id, profile]))
  for (const profile of received) merged.set(profile.id, profile)
  return [...merged.values()].sort((left, right) => left.code.localeCompare(right.code))
}

function datasetLabel(kind: string) {
  const labels: Record<string, string> = { malls: '商场资料', realtime: '实时天气', minutely: '分钟降水', hourly: '逐小时预报', daily: '逐日预报', alerts: '天气预警', life_indices: '生活指数', fetch_runs: '拉取运行记录' }
  return labels[kind] ?? kind
}

function formatDate(value: string) {
  return formatShanghaiDateTime(value)
}

function profileError(status: number, fallback: string, forbidden: string) {
  if (status === 0) return '无法连接服务，请检查网络后重试。'
  if (status === 403) return forbidden
  if (status === 409) return 'Profile 版本已变化，请刷新后重试。'
  if (status === 422) return 'Profile 配置未通过校验，请检查输入。'
  return `${fallback}（HTTP ${status}）`
}
