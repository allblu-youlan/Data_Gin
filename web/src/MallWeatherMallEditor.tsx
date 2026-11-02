import { type FormEvent, useEffect, useRef, useState } from 'react'
import { Pencil, Trash2 } from 'lucide-react'
import {
  mallWeatherGeocodeConfirmPath,
  mallWeatherMallDeletePath,
  mallWeatherMallPatchRequest,
  mallWeatherMallPath,
  mallWeatherManualCoordinateConfirmationRequest,
  parseMallWeatherMall,
  type MallWeatherCreateInput,
  type MallWeatherMall,
} from './mallWeather'
import { runSingleFlight } from './singleFlight'
import { Dialog } from './ui'
import styles from './MallWeatherMallEditor.module.css'

type MallWeatherMutationClient = (
  path: string,
  options?: {
    method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
    body?: unknown
    headers?: Record<string, string>
    showResult?: boolean
    silentLoading?: boolean
    signal?: AbortSignal
  },
) => Promise<{ ok: boolean; status: number; data: unknown }>

export function MallDetailsFields({ form, onChange, disabled, mallCodeReadOnly = false }: {
  form: MallWeatherCreateInput
  onChange: (field: keyof MallWeatherCreateInput, value: string) => void
  disabled: boolean
  mallCodeReadOnly?: boolean
}) {
  return <>
    <label><span>商场编码 *</span><input name="mallCode" value={form.mallCode} onChange={(event) => onChange('mallCode', event.currentTarget.value)} placeholder="例如 SH-PD-001" maxLength={64} disabled={disabled || mallCodeReadOnly} /></label>
    <label><span>商场名称 *</span><input name="nameCn" value={form.nameCn} onChange={(event) => onChange('nameCn', event.currentTarget.value)} placeholder="中文名称" maxLength={255} disabled={disabled} /></label>
    <label><span>开域店铺 ID</span><input name="kaiyuShopId" value={form.kaiyuShopId || ''} onChange={(event) => onChange('kaiyuShopId', event.currentTarget.value)} placeholder="填写监控平台 shopid，留空不采集" maxLength={64} disabled={disabled} /></label>
    <label><span>省份 *</span><input name="province" value={form.province} onChange={(event) => onChange('province', event.currentTarget.value)} placeholder="上海市" maxLength={128} disabled={disabled} /></label>
    <label><span>城市 *</span><input name="city" value={form.city} onChange={(event) => onChange('city', event.currentTarget.value)} placeholder="上海市" maxLength={128} disabled={disabled} /></label>
    <label><span>区县</span><input name="district" value={form.district} onChange={(event) => onChange('district', event.currentTarget.value)} placeholder="浦东新区" maxLength={128} disabled={disabled} /></label>
    <label className={styles['mall-weather-form-wide']}><span>详细地址 *</span><input name="address" value={form.address} onChange={(event) => onChange('address', event.currentTarget.value)} placeholder="道路、门牌号及建筑名称" maxLength={1000} disabled={disabled} /></label>
  </>
}

function mallEditInput(mall: MallWeatherMall): MallWeatherCreateInput {
  return {
    mallCode: mall.mallCode,
    nameCn: mall.nameCn,
    kaiyuShopId: mall.kaiyuShopId || '',
    province: mall.province,
    city: mall.city,
    district: mall.district,
    address: mall.address,
  }
}

export function MallWeatherMallEditor({ mall, client, onMallUpdated, onMallDeleted }: {
  mall: MallWeatherMall
  client: MallWeatherMutationClient
  onMallUpdated: (mall: MallWeatherMall) => void
  onMallDeleted: (mallID: number) => void
}) {
  const failedGeocode = mall.geocodeStatus.toLowerCase() === 'failed'
  const [editing, setEditing] = useState(failedGeocode)
  const [form, setForm] = useState<MallWeatherCreateInput>(() => mallEditInput(mall))
  const [workingMall, setWorkingMall] = useState(mall)
  const [longitude, setLongitude] = useState(mall.longitude === undefined ? '' : String(mall.longitude))
  const [latitude, setLatitude] = useState(mall.latitude === undefined ? '' : String(mall.latitude))
  const [reason, setReason] = useState('人工确认商场地址与高德坐标')
  const [submitting, setSubmitting] = useState(false)
  const [coordinateConfirming, setCoordinateConfirming] = useState(false)
  const [deleteConfirming, setDeleteConfirming] = useState(false)
  const [deleteName, setDeleteName] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const coordinateConfirmationInFlight = useRef({ current: false })

  useEffect(() => {
    setWorkingMall(mall)
    setForm(mallEditInput(mall))
    setLongitude(mall.longitude === undefined ? '' : String(mall.longitude))
    setLatitude(mall.latitude === undefined ? '' : String(mall.latitude))
  }, [mall])

  function change(field: keyof MallWeatherCreateInput, value: string) {
    setForm((current) => ({ ...current, [field]: value }))
    setError('')
    setMessage('')
  }

  async function save(confirmCoordinate: boolean) {
    let patch: ReturnType<typeof mallWeatherMallPatchRequest>
    try {
      patch = mallWeatherMallPatchRequest(workingMall, form)
    } catch {
      setError('请完整填写商场名称、省市和详细地址，并检查字段长度。')
      return
    }
    if (!patch && !confirmCoordinate) {
      setError('请至少修改一项商场信息，或选择确认高德坐标。')
      return
    }

    setSubmitting(true)
    setError('')
    setMessage('')
    let latestMall = workingMall
    if (patch) {
      const response = await client(mallWeatherMallPath(workingMall.id), {
        method: 'PATCH', body: patch, showResult: false, silentLoading: true,
      })
      if (!response.ok) {
        setSubmitting(false)
        setError(mallEditorActionError(response.status, '商场信息修改失败', '当前账号缺少 mall.write 权限'))
        return
      }
      const updated = parseMallWeatherMall(response.data)
      if (!updated) {
        setSubmitting(false)
        setError('商场信息已提交，但响应格式不正确；请刷新列表确认。')
        return
      }
      latestMall = updated
      setWorkingMall(updated)
      setForm(mallEditInput(updated))
      onMallUpdated(updated)
    }

    if (confirmCoordinate) {
      let body: unknown
      try {
        body = mallWeatherManualCoordinateConfirmationRequest(latestMall.version, longitude, latitude, reason)
      } catch {
        setSubmitting(false)
        setError('请填写有效的高德 GCJ-02 经纬度和 500 字以内的单行确认原因。')
        return
      }
      const coordinateResponse = await client(mallWeatherGeocodeConfirmPath(latestMall.id), {
        method: 'POST', body, showResult: false, silentLoading: true,
      })
      if (!coordinateResponse.ok) {
        setSubmitting(false)
        setError(patch
          ? `地址已保存，但${mallEditorActionError(coordinateResponse.status, '高德坐标确认失败', '当前账号缺少 mall.geocode.confirm 权限')}`
          : mallEditorActionError(coordinateResponse.status, '高德坐标确认失败', '当前账号缺少 mall.geocode.confirm 权限'))
        return
      }
      const confirmed = parseMallWeatherMall(coordinateResponse.data)
      if (!confirmed) {
        setSubmitting(false)
        setError('高德坐标已提交，但响应格式不正确；请刷新列表确认。')
        return
      }
      latestMall = confirmed
    }

    setSubmitting(false)
    setWorkingMall(latestMall)
    onMallUpdated(latestMall)
    setEditing(false)
    setMessage(confirmCoordinate ? '商场信息和高德坐标已确认，天气已启用。' : '商场信息已保存，地址解析已重新提交。')
  }

  function requestCoordinateConfirmation() {
    try {
      mallWeatherMallPatchRequest(workingMall, form)
    } catch {
      setError('请完整填写商场名称、省市和详细地址，并检查字段长度。')
      return
    }
    try {
      mallWeatherManualCoordinateConfirmationRequest(workingMall.version, longitude, latitude, reason)
    } catch {
      setError('请填写有效的高德 GCJ-02 经纬度和 500 字以内的单行确认原因。')
      return
    }
    setError('')
    setCoordinateConfirming(true)
  }

  async function deleteMall() {
    if (deleteName !== workingMall.nameCn) return
    setSubmitting(true)
    setError('')
    setMessage('')
    const response = await client(mallWeatherMallDeletePath(workingMall.id, workingMall.version), {
      method: 'DELETE', showResult: false, silentLoading: true,
    })
    setSubmitting(false)
    if (!response.ok && response.status !== 204) {
      setError(mallEditorActionError(response.status, '商场删除失败', '当前账号缺少 mall.write 权限'))
      return
    }
    onMallDeleted(workingMall.id)
  }

  return (
    <section className={[styles['workbench-panel'], styles['mall-weather-editor-panel']].join(' ')}>
      <div className={styles['mall-weather-section-title']}>
        <div><strong>商场信息</strong><span>{workingMall.mallCode} · {mallEditorLifecycleLabel(workingMall)}</span></div>
        <div className={styles['mall-weather-form-actions']}>
          <button type="button" onClick={() => { setEditing((current) => !current); setError(''); setMessage('') }} disabled={submitting} aria-expanded={editing}>
            <Pencil aria-hidden="true" />{editing ? '收起编辑' : '编辑商场'}
          </button>
          <button className={styles['danger']} type="button" onClick={() => { setDeleteConfirming((current) => !current); setDeleteName(''); setError(''); setMessage('') }} disabled={submitting} aria-expanded={deleteConfirming}>
            <Trash2 aria-hidden="true" />删除商场
          </button>
        </div>
      </div>
      {failedGeocode && !editing && <p className={[styles['mall-weather-action-message'], styles['error']].join(' ')} role="alert">地址解析失败，请编辑地址，或直接填写高德经纬度后确认。</p>}
      {editing && <form className={styles['mall-weather-create-form']} onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); void save(false) }} aria-busy={submitting}>
        <MallDetailsFields form={form} onChange={change} disabled={submitting} mallCodeReadOnly />
        <div className={[styles['mall-weather-manual-coordinate'], styles['mall-weather-form-wide']].join(' ')}>
          <div className={[styles['mall-weather-section-title'], styles['mall-weather-form-wide']].join(' ')}><div><strong>手动确认高德坐标</strong><span>地址解析失败或候选不准确时填写；坐标系固定为 GCJ-02</span></div><span>可选</span></div>
          <label><span>高德经度</span><input name="editLongitude" inputMode="decimal" value={longitude} onChange={(event) => setLongitude(event.currentTarget.value)} disabled={submitting} placeholder="例如 121.4737" /></label>
          <label><span>高德纬度</span><input name="editLatitude" inputMode="decimal" value={latitude} onChange={(event) => setLatitude(event.currentTarget.value)} disabled={submitting} placeholder="例如 31.2304" /></label>
          <label className={styles['mall-weather-form-wide']}><span>确认原因</span><input name="editCoordinateReason" value={reason} onChange={(event) => setReason(event.currentTarget.value)} maxLength={500} disabled={submitting} /></label>
        </div>
        <div className={[styles['mall-weather-form-actions'], styles['mall-weather-form-wide']].join(' ')}>
          <button type="submit" disabled={submitting}>{submitting ? '保存中' : '保存店铺信息'}</button>
          <button className={styles['primary']} type="button" onClick={requestCoordinateConfirmation} disabled={submitting}>{submitting ? '确认中' : '保存并确认高德坐标'}</button>
        </div>
      </form>}
      {coordinateConfirming && <Dialog open role="alertdialog" title="确认高德坐标并启用天气" closeDisabled={submitting} onClose={() => setCoordinateConfirming(false)}><div className={styles.dialogBody}>
        <p>将使用当前 GCJ-02 坐标启用天气服务，并创建首次采集任务。</p>
        <div className={styles['mall-weather-form-actions']}><button className={styles.primary} type="button" disabled={submitting} onClick={() => { setCoordinateConfirming(false); void runSingleFlight(coordinateConfirmationInFlight.current, () => save(true)) }}>{submitting ? '确认中' : '确认启用天气'}</button><button type="button" disabled={submitting} onClick={() => setCoordinateConfirming(false)}>返回修改</button></div>
      </div></Dialog>}
      {deleteConfirming && <Dialog open role="alertdialog" title="删除商场及天气配置" closeDisabled={submitting} onClose={() => { setDeleteConfirming(false); setDeleteName('') }}><div className={styles.dialogBody}>
        <label><span>请输入商场名称“{workingMall.nameCn}”确认</span><input value={deleteName} onChange={(event) => setDeleteName(event.currentTarget.value)} disabled={submitting} autoComplete="off" /></label>
        <div className={styles['mall-weather-form-actions']}><button className={styles.danger} type="button" onClick={() => void deleteMall()} disabled={submitting || deleteName !== workingMall.nameCn}>{submitting ? '删除中' : '确认删除'}</button><button type="button" onClick={() => { setDeleteConfirming(false); setDeleteName('') }} disabled={submitting}>取消</button></div>
      </div></Dialog>}
      {message && <p className={styles['mall-weather-action-message']} role="status">{message}</p>}
      {error && <p className={[styles['mall-weather-action-message'], styles['error']].join(' ')} role="alert">{error}</p>}
    </section>
  )
}

function mallEditorActionError(status: number, fallback: string, forbidden: string) {
  if (status === 0) return '无法连接服务，请检查网络后重试'
  if (status === 403) return forbidden
  if (status === 404) return '商场不存在，请刷新列表'
  if (status === 409) return '商场状态已变化，请刷新后重试'
  if (status === 422) return '提交内容校验失败，请检查输入后重试'
  return `${fallback}（HTTP ${status}）`
}

function mallEditorLifecycleLabel(mall: MallWeatherMall) {
  if (mall.geocodeStatus.toLowerCase() === 'failed') return '坐标解析失败'
  if (mall.geocodeStatus.toLowerCase() !== 'confirmed') return '等待确认坐标'
  if (!mall.weatherEnabled) return '天气未启用'
  if (mall.status.toLowerCase() !== 'active') return '商场未启用'
  return '可查询'
}
