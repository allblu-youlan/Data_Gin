import { formatShanghaiDateTime } from './dateTime.js'
import { AlertTriangle, Clock3, CloudRain, CloudSun, Download, MapPin, RefreshCcw, Thermometer } from 'lucide-react'
import { useState } from 'react'
import { MallWeatherChart, type MallWeatherChartSeries } from './MallWeatherChart'
import {
  createMallWeatherChartCsv,
  createMallWeatherDatasetCsv,
  downloadMallWeatherBytes,
  mallWeatherChartCsvFileName,
  mallWeatherCsvFileName,
} from './mallWeatherCsv'
import {
  mallWeatherFreshnessLabel,
  mallWeatherMetric,
  mallWeatherSkyconLabel,
  type MallWeatherAlert,
  type MallWeatherMall,
  type MallWeatherOverview,
} from './mallWeather'
import styles from './MallWeatherOverviewPanel.module.css'
import { weatherChartPalette } from './weatherChartPalette'

export type MallWeatherAlertSnapshot = {
  mallID: number
  items: MallWeatherAlert[]
  loading: boolean
  ready: boolean
  error: string
}

export function WeatherRealtime({ mall, overview }: { mall: MallWeatherMall; overview: MallWeatherOverview }) {
  const { realtime } = overview
  const [downloadError, setDownloadError] = useState('')

  function downloadCsv() {
    setDownloadError('')
    try {
      downloadMallWeatherBytes(
        createMallWeatherDatasetCsv('realtime', realtime ? [realtime] : [], { mallCode: mall.mallCode, mallName: mall.nameCn }),
        mallWeatherCsvFileName('realtime', mall.mallCode),
      )
    } catch {
      setDownloadError('实况 CSV 文件生成失败，请重新加载后重试。')
    }
  }

  return (
    <article className={[styles['panel'], styles['realtime']].join(' ')} aria-label="当前实况天气">
      <header className={styles['realtimeHeading']}>
        <div><strong>当前实况</strong><span>{formatShanghaiDateTime(realtime?.snapshotAtLocal, '暂无快照时间')}</span></div>
        <button type="button" onClick={downloadCsv} disabled={!realtime} aria-label={`下载数据：${mall.nameCn}当前实况 CSV`}><Download aria-hidden="true" />下载数据</button>
      </header>
      {downloadError && <p className={[styles['message'], styles['error']].join(' ')} role="alert">{downloadError}</p>}
      {realtime ? (
        <>
          <div className={styles['realtimeBody']}>
            <div className={styles['skySummary']}>
              <CloudSun aria-hidden="true" />
              <div className={styles['temperature']}><strong>{mallWeatherMetric(realtime.temperatureC, '°C')}</strong><span>{mallWeatherSkyconLabel(realtime.skycon).replace(/（.*）/, '')}</span></div>
            </div>
            <dl className={styles['currentMetrics']}>
              <div><dt>体感</dt><dd>{mallWeatherMetric(realtime.apparentTemperatureC, '°C')}</dd></div>
              <div><dt>湿度</dt><dd>{mallWeatherMetric(realtime.humidityPct, '%', 0)}</dd></div>
              <div><dt>风速</dt><dd>{mallWeatherMetric(realtime.windSpeedKph, ' km/h')}</dd></div>
              <div><dt>能见度</dt><dd>{mallWeatherMetric(realtime.visibilityKm, ' km')}</dd></div>
              <div><dt>本地降水</dt><dd>{mallWeatherMetric(realtime.localPrecipitationMmH, ' mm/h')}</dd></div>
              <div><dt>质量</dt><dd className={realtime.qualityWarnings.length === 0 ? styles.success : styles.warning}>{realtime.qualityWarnings.length === 0 ? '正常' : `${realtime.qualityWarnings.length} 项告警`}</dd></div>
            </dl>
          </div>
        </>
      ) : <EmptyState title="暂无实况" detail="最近一次采集尚未产生可用实况。" />}
    </article>
  )
}

export function WeatherOverviewDetails({ mall, overview, alerts, refreshing, onRefresh, onAlertsRetry }: {
  mall: MallWeatherMall
  overview: MallWeatherOverview
  alerts: MallWeatherAlertSnapshot
  refreshing: boolean
  onRefresh: () => void
  onAlertsRetry: () => void
}) {
  const { realtime, meta } = overview
  const [alertDownloadError, setAlertDownloadError] = useState('')
  const [chartDownloadError, setChartDownloadError] = useState('')
  const representativePoint = ['MALL_CENTER', 'CENTER', 'center'].includes(meta.representativePoint) ? '商场中心点' : meta.representativePoint || '口径缺失'
  const coverageRadius = meta.coverageRadiusM > 0 ? `业务半径 ${meta.coverageRadiusM} m` : '业务半径口径缺失'

  function downloadAlertsCsv() {
    setAlertDownloadError('')
    try {
      downloadMallWeatherBytes(
        createMallWeatherDatasetCsv('alerts', alerts.items, { mallCode: mall.mallCode, mallName: mall.nameCn }),
        mallWeatherCsvFileName('alerts', mall.mallCode),
      )
    } catch {
      setAlertDownloadError('气象预警 CSV 文件生成失败，请重新查询后重试。')
    }
  }

  function downloadChartCsv(chartID: string, unit: string, series: MallWeatherChartSeries[]) {
    setChartDownloadError('')
    try {
      downloadMallWeatherBytes(
        createMallWeatherChartCsv(series.map((item) => ({ ...item, unit })), { mallCode: mall.mallCode, mallName: mall.nameCn }),
        mallWeatherChartCsvFileName(chartID, mall.mallCode),
      )
    } catch {
      setChartDownloadError('曲线数据 CSV 文件生成失败，请重新加载后重试。')
    }
  }

  const primaryAlert = alerts.items[0]

  return (
    <div className={styles['overviewDetails']}>
      <section className={[styles['panel'], styles['airQuality']].join(' ')} aria-label="空气质量">
        <strong className={styles['cardTitle']}>空气质量</strong>
        <div className={styles['aqi']}><strong>{mallWeatherMetric(realtime?.aqiChn, '', 0)}</strong><span>AQI</span></div>
        <b className={styles['aqiGrade']}>{realtime?.aqiDescriptionChn || '暂无评级'}</b>
        <dl className={styles['airMetrics']}>
          <div><dt>PM2.5</dt><dd>{mallWeatherMetric(realtime?.pm25UgM3, '')}</dd></div>
          <div><dt>PM10</dt><dd>{mallWeatherMetric(realtime?.pm10UgM3, '')}</dd></div>
          <div><dt>O₃</dt><dd>{mallWeatherMetric(realtime?.o3UgM3, '')}</dd></div>
        </dl>
      </section>

      <section className={[styles['panel'], styles['alert']].join(' ')} id="mall-weather-alerts" tabIndex={-1} aria-busy={alerts.loading}>
        <header className={styles['cardHeading']}>
          <strong className={styles['cardTitle']}>气象预警</strong>
          <button type="button" onClick={downloadAlertsCsv} disabled={alerts.loading || Boolean(alerts.error)} aria-label={`下载数据：${mall.nameCn}气象预警 CSV`}><Download aria-hidden="true" />下载数据</button>
        </header>
        {alerts.loading && <LoadingState label="正在加载全部气象预警" />}
        {alerts.error && <RequestError message={alerts.error} onRetry={onAlertsRetry} />}
        {alerts.ready && (primaryAlert
          ? <article>
            <AlertTriangle aria-hidden="true" />
            <strong>{primaryAlert.title}</strong>
            <span>{primaryAlert.source || '预警发布机构'}</span>
            <time>{formatShanghaiDateTime(primaryAlert.publishedAtLocal, '发布时间未知')}</time>
            {primaryAlert.description && <p>{primaryAlert.description}</p>}
          </article>
          : <EmptyState title="当前无有效预警" detail="当前查询窗口没有返回有效气象预警。" />)}
        {alertDownloadError && <p className={[styles['message'], styles['error']].join(' ')} role="alert">{alertDownloadError}</p>}
      </section>

      <section className={[styles['contentGrid'], styles['two'], styles['charts']].join(' ')}>
        <MallWeatherChart
          title="未来 120 分钟降水"
          detail="1 km 级"
          unit="mm/h"
          icon={<CloudRain aria-hidden="true" />}
          series={[{ id: 'precipitationMmH', name: '降水强度', color: weatherChartPalette.precipitation, data: overview.minutely.map((item) => ({ time: item.forecastMinuteLocal, value: item.precipitationMmH })) }]}
          floorZero
          onDownload={(series) => downloadChartCsv('overview_minutely_precipitation', 'mm/h', series)}
        />
        <MallWeatherChart
          title="未来 24 小时温度"
          detail="9～13 km 预报网格"
          unit="°C"
          icon={<Thermometer aria-hidden="true" />}
          series={[
            { id: 'temperatureC', name: '温度', color: weatherChartPalette.temperature, data: overview.hourly.map((item) => ({ time: item.forecastTimeLocal, value: item.temperatureC })) },
            { id: 'apparentTemperatureC', name: '体感温度', color: weatherChartPalette.apparentTemperature, dash: '6 4', data: overview.hourly.map((item) => ({ time: item.forecastTimeLocal, value: item.apparentTemperatureC })) },
          ]}
          onDownload={(series) => downloadChartCsv('overview_hourly_temperature', '°C', series)}
        />
      </section>
      {chartDownloadError && <p className={[styles['message'], styles['error'], styles['chartDownloadError']].join(' ')} role="alert">{chartDownloadError}</p>}

      <section className={[styles['panel'], styles['summary']].join(' ')}>
        <div className={styles['meta']} aria-label="天气数据口径">
          <div className={styles['sourceItem']}><CloudSun aria-hidden="true" /><span>供应商<strong>{`${meta.provider || '彩云天气'} ${meta.apiVersion}`.trim()}</strong></span></div>
          <div className={styles['sourceItem']}><Clock3 aria-hidden="true" /><span>新鲜度<strong>{mallWeatherFreshnessLabel(meta.freshnessStatus)}</strong></span></div>
          <div className={styles['sourceItem']}><MapPin aria-hidden="true" /><span>坐标<strong>{meta.longitude === 0 && meta.latitude === 0 ? '坐标未提供' : `${meta.longitude.toFixed(4)}, ${meta.latitude.toFixed(4)} ${meta.coordinateSystem}`}</strong></span></div>
        </div>
        <details className={styles['resolution']}><summary>查看天气数据口径</summary><p>代表点：{representativePoint} · {coverageRadius}。实况与未来两小时降水为商场中心点 1 km 级数据；常规小时预报为商场中心点所在 9～13 km 预报网格，预警按行政区域发布。</p><button type="button" onClick={onRefresh} disabled={refreshing}><RefreshCcw aria-hidden="true" />{refreshing ? '加载中' : '重新加载'}</button></details>
      </section>
    </div>
  )
}


function RequestError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return <div className={[styles.requestState, styles.error].join(' ')} role="alert"><strong>加载失败</strong><span>{message}</span><button type="button" onClick={onRetry}>重试</button></div>
}

function LoadingState({ label }: { label: string }) {
  return <div className={styles.requestState} role="status" aria-busy="true"><RefreshCcw aria-hidden="true" /><span>{label}</span></div>
}

function EmptyState({ title, detail }: { title: string; detail: string }) {
  return <div className={styles.emptyState} role="status"><strong>{title}</strong><span>{detail}</span></div>
}
