import { formatShanghaiDate, formatShanghaiDateTime } from './dateTime.js'
import type {
  MallWeatherAlert,
  MallWeatherDaily,
  MallWeatherHourly,
  MallWeatherLifeIndex,
  MallWeatherMinutely,
  MallWeatherRealtime,
  MallWeatherWarning,
} from './mallWeather'

export const mallWeatherCsvKinds = [
  'realtime',
  'minutely',
  'hourly',
  'daily',
  'alerts',
  'life_indices',
] as const

export type MallWeatherCsvKind = (typeof mallWeatherCsvKinds)[number]

export type MallWeatherCsvRowByKind = {
  realtime: MallWeatherRealtime
  minutely: MallWeatherMinutely
  hourly: MallWeatherHourly
  daily: MallWeatherDaily
  alerts: MallWeatherAlert
  life_indices: MallWeatherLifeIndex
}

export type MallWeatherCsvDatasetMap = {
  [Kind in MallWeatherCsvKind]: readonly MallWeatherCsvRowByKind[Kind][]
}

export type MallWeatherCsvZipData = Partial<MallWeatherCsvDatasetMap>

export type MallWeatherCsvMallContext = {
  mallCode: string
  mallName: string
}

export type MallWeatherChartCsvSeries = {
  id: string
  name: string
  unit: string
  data: ReadonlyArray<{ time: string; value?: number }>
}

export const mallWeatherCsvEntryNames: Readonly<Record<MallWeatherCsvKind, string>> = {
  realtime: 'realtime.csv',
  minutely: 'minutely.csv',
  hourly: 'hourly.csv',
  daily: 'daily.csv',
  alerts: 'alerts.csv',
  life_indices: 'life_indices.csv',
}

const UTF8_BOM = new Uint8Array([0xef, 0xbb, 0xbf])
const UTF8_FLAG = 0x0800
const ZIP_VERSION = 20
const ZIP_STORED = 0
const ZIP_DOS_TIME = 0
const ZIP_DOS_DATE = 0x0021
const MAX_UINT16 = 0xffff
const MAX_UINT32 = 0xffffffff
const MAX_CHART_CSV_CELLS = 250_000
const textEncoder = new TextEncoder()

type CsvHeaderMap<Kind extends MallWeatherCsvKind> = Record<keyof MallWeatherCsvRowByKind[Kind], string>

const realtimeHeaders = {
  snapshotAtLocal: '实况时间',
  providerServerTimeLocal: '供应商时间',
  fetchedAtLocal: '采集时间',
  temperatureC: '温度（℃）',
  apparentTemperatureC: '体感温度（℃）',
  humidityPct: '湿度（%）',
  pressurePa: '气压（Pa）',
  windSpeedKph: '风速（km/h）',
  windDirectionDeg: '风向（度）',
  cloudrateRatio: '云量比例',
  dswrfWM2: '短波辐射（W/m²）',
  localPrecipitationStatus: '本地降水状态',
  localPrecipitationMmH: '本地降水强度（mm/h）',
  localPrecipitationSource: '本地降水数据源',
  nearestPrecipitationStatus: '最近降水状态',
  nearestPrecipitationDistanceKm: '最近降水距离（km）',
  nearestPrecipitationMmH: '最近降水强度（mm/h）',
  visibilityKm: '能见度（km）',
  skycon: '天气现象',
  pm25UgM3: '细颗粒物PM2.5（μg/m³）',
  pm10UgM3: '可吸入颗粒物PM10（μg/m³）',
  o3UgM3: '臭氧（μg/m³）',
  so2UgM3: '二氧化硫（μg/m³）',
  no2UgM3: '二氧化氮（μg/m³）',
  coMgM3: '一氧化碳（mg/m³）',
  aqiChn: '中国空气质量指数',
  aqiUsa: '美国空气质量指数',
  aqiDescriptionChn: '中国空气质量描述',
  aqiDescriptionUsa: '美国空气质量描述',
  comfortIndex: '舒适度指数',
  comfortDescription: '舒适度描述',
  ultravioletIndex: '紫外线指数',
  ultravioletDescription: '紫外线描述',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'realtime'>

const minutelyHeaders = {
  forecastMinuteUtc: '预报分钟（UTC）',
  forecastMinuteLocal: '预报分钟（当地时间）',
  issuedAtUtc: '发布时间（UTC）',
  issuedAtLocal: '发布时间（当地时间）',
  fetchedAtUtc: '采集时间（UTC）',
  fetchedAtLocal: '采集时间（当地时间）',
  minuteOffset: '分钟偏移',
  precipitationMmH: '降水强度（mm/h）',
  probabilityPct: '降水概率（%）',
  datasource: '数据源',
  description: '描述',
  forecastKeypoint: '预报关键点',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'minutely'>

const hourlyHeaders = {
  forecastTimeUtc: '预报时间（UTC）',
  forecastTimeLocal: '预报时间（当地时间）',
  issuedAtUtc: '发布时间（UTC）',
  issuedAtLocal: '发布时间（当地时间）',
  fetchedAtUtc: '采集时间（UTC）',
  fetchedAtLocal: '采集时间（当地时间）',
  temperatureC: '温度（℃）',
  apparentTemperatureC: '体感温度（℃）',
  pressurePa: '气压（Pa）',
  humidityPct: '湿度（%）',
  precipitationMmH: '降水强度（mm/h）',
  precipitationProbabilityPct: '降水概率（%）',
  windSpeedKph: '风速（km/h）',
  windDirectionDeg: '风向（度）',
  cloudrateRatio: '云量比例',
  dswrfWM2: '短波辐射（W/m²）',
  visibilityKm: '能见度（km）',
  skycon: '天气现象',
  pm25UgM3: '细颗粒物PM2.5（μg/m³）',
  aqiChn: '中国空气质量指数',
  aqiUsa: '美国空气质量指数',
  hourlyDescription: '逐小时描述',
  forecastKeypoint: '预报关键点',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'hourly'>

const dailyHeaders = {
  forecastDateLocal: '预报日期',
  issuedAtUtc: '发布时间（UTC）',
  issuedAtLocal: '发布时间（当地时间）',
  fetchedAtUtc: '采集时间（UTC）',
  fetchedAtLocal: '采集时间（当地时间）',
  temperatureMaxC: '最高温度（℃）',
  temperatureMinC: '最低温度（℃）',
  temperatureAvgC: '平均温度（℃）',
  dayTemperatureMaxC: '白天最高温度（℃）',
  dayTemperatureMinC: '白天最低温度（℃）',
  dayTemperatureAvgC: '白天平均温度（℃）',
  nightTemperatureMaxC: '夜间最高温度（℃）',
  nightTemperatureMinC: '夜间最低温度（℃）',
  nightTemperatureAvgC: '夜间平均温度（℃）',
  precipitationMaxMmH: '最高降水强度（mm/h）',
  precipitationMinMmH: '最低降水强度（mm/h）',
  precipitationAvgMmH: '平均降水强度（mm/h）',
  precipitationProbabilityPct: '降水概率（%）',
  dayPrecipitationMaxMmH: '白天最高降水强度（mm/h）',
  dayPrecipitationMinMmH: '白天最低降水强度（mm/h）',
  dayPrecipitationAvgMmH: '白天平均降水强度（mm/h）',
  dayPrecipitationProbabilityPct: '白天降水概率（%）',
  nightPrecipitationMaxMmH: '夜间最高降水强度（mm/h）',
  nightPrecipitationMinMmH: '夜间最低降水强度（mm/h）',
  nightPrecipitationAvgMmH: '夜间平均降水强度（mm/h）',
  nightPrecipitationProbabilityPct: '夜间降水概率（%）',
  windMaxSpeedKph: '最大风速（km/h）',
  windMaxDirectionDeg: '最大风速对应风向（度）',
  windMinSpeedKph: '最小风速（km/h）',
  windMinDirectionDeg: '最小风速对应风向（度）',
  windAvgSpeedKph: '平均风速（km/h）',
  windAvgDirectionDeg: '平均风向（度）',
  dayWindMaxSpeedKph: '白天最大风速（km/h）',
  dayWindMaxDirectionDeg: '白天最大风速对应风向（度）',
  dayWindMinSpeedKph: '白天最小风速（km/h）',
  dayWindMinDirectionDeg: '白天最小风速对应风向（度）',
  dayWindAvgSpeedKph: '白天平均风速（km/h）',
  dayWindAvgDirectionDeg: '白天平均风向（度）',
  nightWindMaxSpeedKph: '夜间最大风速（km/h）',
  nightWindMaxDirectionDeg: '夜间最大风速对应风向（度）',
  nightWindMinSpeedKph: '夜间最小风速（km/h）',
  nightWindMinDirectionDeg: '夜间最小风速对应风向（度）',
  nightWindAvgSpeedKph: '夜间平均风速（km/h）',
  nightWindAvgDirectionDeg: '夜间平均风向（度）',
  humidityMaxPct: '最高湿度（%）',
  humidityMinPct: '最低湿度（%）',
  humidityAvgPct: '平均湿度（%）',
  cloudrateMaxRatio: '最高云量比例',
  cloudrateMinRatio: '最低云量比例',
  cloudrateAvgRatio: '平均云量比例',
  pressureMaxPa: '最高气压（Pa）',
  pressureMinPa: '最低气压（Pa）',
  pressureAvgPa: '平均气压（Pa）',
  visibilityMaxKm: '最高能见度（km）',
  visibilityMinKm: '最低能见度（km）',
  visibilityAvgKm: '平均能见度（km）',
  dswrfMaxWM2: '最高短波辐射（W/m²）',
  dswrfMinWM2: '最低短波辐射（W/m²）',
  dswrfAvgWM2: '平均短波辐射（W/m²）',
  pm25MaxUgM3: 'PM2.5最高值（μg/m³）',
  pm25MinUgM3: 'PM2.5最低值（μg/m³）',
  pm25AvgUgM3: 'PM2.5平均值（μg/m³）',
  aqiMaxChn: '中国空气质量指数最高值',
  aqiMinChn: '中国空气质量指数最低值',
  aqiAvgChn: '中国空气质量指数平均值',
  aqiMaxUsa: '美国空气质量指数最高值',
  aqiMinUsa: '美国空气质量指数最低值',
  aqiAvgUsa: '美国空气质量指数平均值',
  skycon: '全天天气现象',
  daySkycon: '白天天气现象',
  nightSkycon: '夜间天气现象',
  sunriseLocalTime: '日出时间',
  sunsetLocalTime: '日落时间',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'daily'>

const alertHeaders = {
  alertId: '预警编号',
  status: '状态',
  title: '标题',
  description: '描述',
  code: '代码',
  alertTypeCode: '预警类型代码',
  alertLevelCode: '预警级别代码',
  alertTypeName: '预警类型',
  alertLevelName: '预警级别',
  publishedAtLocal: '发布时间',
  source: '发布来源',
  province: '省份',
  city: '城市',
  county: '区县',
  location: '预警区域',
  regionId: '区域编号',
  adcode: '行政区划代码',
  latitude: '预警纬度',
  longitude: '预警经度',
  firstSeenAtLocal: '首次发现时间',
  lastSeenAtLocal: '最近发现时间',
  endedAtLocal: '结束时间',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'alerts'>

const lifeIndexHeaders = {
  sourceApi: '来源接口',
  forecastDateLocal: '预报日期',
  indexType: '指数类型',
  indexCode: '指数代码',
  indexName: '指数名称',
  level: '等级',
  shortDescription: '简要说明',
  detail: '详细说明',
  isUnknownType: '是否未知类型',
  issuedAtUtc: '发布时间（UTC）',
  issuedAtLocal: '发布时间（当地时间）',
  fetchedAtUtc: '采集时间（UTC）',
  fetchedAtLocal: '采集时间（当地时间）',
  qualityStatus: '质量状态',
  qualityWarnings: '质量告警',
} satisfies CsvHeaderMap<'life_indices'>

const headersByKind = {
  realtime: realtimeHeaders,
  minutely: minutelyHeaders,
  hourly: hourlyHeaders,
  daily: dailyHeaders,
  alerts: alertHeaders,
  life_indices: lifeIndexHeaders,
} satisfies { [Kind in MallWeatherCsvKind]: CsvHeaderMap<Kind> }

export function createMallWeatherDatasetCsv<Kind extends MallWeatherCsvKind>(
  kind: Kind,
  rows: readonly MallWeatherCsvRowByKind[Kind][],
  mall: MallWeatherCsvMallContext,
): Uint8Array {
  const normalizedMall = normalizeMallContext(mall)
  if (!Array.isArray(rows)) throw new Error('invalid mall weather CSV rows')

  const headers = headersByKind[kind] as CsvHeaderMap<Kind>
  if (!headers) throw new Error('invalid mall weather CSV kind')
  const fields = objectKeys(headers)
  const records: string[][] = [
    ['商场编码', '商场名称', ...fields.map((field) => headers[field].replace('（UTC）', '（上海时间，原UTC）').replace('（当地时间）', '（上海时间）'))],
  ]
  for (const row of rows) {
    if (!row || typeof row !== 'object' || Array.isArray(row)) {
      throw new Error('invalid mall weather CSV row')
    }
    records.push([
      protectSpreadsheetText(normalizedMall.mallCode),
      protectSpreadsheetText(normalizedMall.mallName),
      ...fields.map((field) => formatWeatherCsvValue(String(field), row[field])),
    ])
  }

  const csvText = `${records.map((record) => record.map(escapeCsvCell).join(',')).join('\r\n')}\r\n`
  return concatenateBytes([UTF8_BOM, textEncoder.encode(csvText)])
}

export function createMallWeatherChartCsv(
  series: readonly MallWeatherChartCsvSeries[],
  mall: MallWeatherCsvMallContext,
): Uint8Array {
  const normalizedMall = normalizeMallContext(mall)
  if (!Array.isArray(series) || series.length === 0) throw new Error('invalid mall weather chart CSV series')
  let pointCount = 0
  const normalizedSeries = series.map((item) => {
    const id = typeof item.id === 'string' ? item.id.trim() : ''
    const name = typeof item.name === 'string' ? item.name.trim() : ''
    const unit = typeof item.unit === 'string' ? item.unit.trim() : ''
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(id) || !name || Array.from(name).length > 64 || Array.from(unit).length > 24 || !Array.isArray(item.data)) {
      throw new Error('invalid mall weather chart CSV series')
    }
    const values = new Map<string, number | undefined>()
    for (const point of item.data) {
      pointCount += 1
      if (pointCount > MAX_CHART_CSV_CELLS) throw new Error('mall weather chart CSV is too large')
      const time = typeof point?.time === 'string' ? point.time.trim() : ''
      if (!time || Array.from(time).length > 64 || containsDisallowedControl(time, false) ||
        (point.value !== undefined && (typeof point.value !== 'number' || !Number.isFinite(point.value)))) {
        throw new Error('invalid mall weather chart CSV point')
      }
      values.set(time, point.value)
    }
    return { id, name, unit, values }
  })
  const times = [...new Set(normalizedSeries.flatMap((item) => [...item.values.keys()]))].sort()
  const cellCount = (times.length + 1) * (normalizedSeries.length + 3)
  if (cellCount > MAX_CHART_CSV_CELLS) throw new Error('mall weather chart CSV is too large')
  const records: string[][] = [
    ['商场编码', '商场名称', '时间', ...normalizedSeries.map((item) => protectSpreadsheetText(`${item.name}${item.unit ? `（${item.unit}）` : ''}`))],
    ...times.map((time) => [
      protectSpreadsheetText(normalizedMall.mallCode),
      protectSpreadsheetText(normalizedMall.mallName),
      protectSpreadsheetText(/^\d{4}-\d{2}-\d{2}$/.test(time) ? formatShanghaiDate(time) : formatShanghaiDateTime(time, time)),
      ...normalizedSeries.map((item) => formatCsvValue(item.values.get(time))),
    ]),
  ]
  const csvText = `${records.map((record) => record.map(escapeCsvCell).join(',')).join('\r\n')}\r\n`
  return concatenateBytes([UTF8_BOM, textEncoder.encode(csvText)])
}

export function createMallWeatherCsvZip(
  data: MallWeatherCsvZipData,
  mall: MallWeatherCsvMallContext,
): Uint8Array {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    throw new Error('invalid mall weather CSV ZIP data')
  }
  const entries = mallWeatherCsvKinds.map((kind) => ({
    name: mallWeatherCsvEntryNames[kind],
    data: createMallWeatherDatasetCsvForZip(kind, data, mall),
  }))
  return createStoredZip(entries)
}

export function mallWeatherCsvFileName(kind: MallWeatherCsvKind, mallCode: string): string {
  if (!mallWeatherCsvKinds.includes(kind)) throw new Error('invalid mall weather CSV kind')
  return `${safeMallCodeFileStem(mallCode)}_${mallWeatherCsvEntryNames[kind]}`
}

export function mallWeatherChartCsvFileName(chartID: string, mallCode: string): string {
  const safeChartID = typeof chartID === 'string' ? chartID.trim() : ''
  if (!/^[a-z0-9_]{1,64}$/.test(safeChartID)) throw new Error('invalid mall weather chart id')
  return `${safeMallCodeFileStem(mallCode)}_${safeChartID}.csv`
}

export function mallWeatherCsvZipFileName(mallCode: string): string {
  return `${safeMallCodeFileStem(mallCode)}_weather_csv.zip`
}

export function downloadMallWeatherBytes(bytes: Uint8Array, fileName: string): void {
  if (!(bytes instanceof Uint8Array) || bytes.byteLength === 0) {
    throw new Error('invalid mall weather download bytes')
  }
  if (!isSafeDownloadFileName(fileName)) {
    throw new Error('invalid mall weather download file name')
  }
  const contentType = fileName.endsWith('.zip') ? 'application/zip' : 'text/csv;charset=utf-8'
  const url = URL.createObjectURL(new Blob([bytes], { type: contentType }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName
  anchor.rel = 'noopener'
  anchor.hidden = true
  document.body.append(anchor)
  try {
    anchor.click()
  } finally {
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}

function createMallWeatherDatasetCsvForZip<Kind extends MallWeatherCsvKind>(
  kind: Kind,
  data: MallWeatherCsvZipData,
  mall: MallWeatherCsvMallContext,
): Uint8Array {
  const rows = (data[kind] ?? []) as readonly MallWeatherCsvRowByKind[Kind][]
  return createMallWeatherDatasetCsv(kind, rows, mall)
}

function normalizeMallContext(mall: MallWeatherCsvMallContext): MallWeatherCsvMallContext {
  if (!mall || typeof mall !== 'object') throw new Error('invalid mall weather CSV mall context')
  const mallCode = typeof mall.mallCode === 'string' ? mall.mallCode.trim() : ''
  const mallName = typeof mall.mallName === 'string' ? mall.mallName.trim() : ''
  if (!mallCode || !mallName || mallCode.length > 64 || Array.from(mallName).length > 255 ||
    containsDisallowedControl(mallCode, false) || containsDisallowedControl(mallName, true)) {
    throw new Error('invalid mall weather CSV mall context')
  }
  return { mallCode, mallName }
}

function containsDisallowedControl(value: string, allowCsvWhitespace: boolean): boolean {
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0
    if (codePoint === 0x7f) return true
    if (codePoint < 0x20 && (!allowCsvWhitespace || ![0x09, 0x0a, 0x0d].includes(codePoint))) return true
  }
  return false
}

function objectKeys<Value extends object>(value: Value): Array<keyof Value> {
  return Object.keys(value) as Array<keyof Value>
}

function formatWeatherCsvValue(field: string, value: unknown): string {
  if (typeof value === 'string' && /(?:At|Time|Minute)(?:Utc|Local)$/.test(field)) {
    return protectSpreadsheetText(formatShanghaiDateTime(value, value))
  }
  return formatCsvValue(value)
}

function formatCsvValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return protectSpreadsheetText(value)
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error('invalid mall weather CSV number')
    return Object.is(value, -0) ? '0' : String(value)
  }
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (value instanceof Date) {
    if (Number.isNaN(value.getTime())) throw new Error('invalid mall weather CSV date')
    return formatShanghaiDateTime(value)
  }
  if (Array.isArray(value)) return formatWarnings(value)
  throw new Error('unsupported mall weather CSV value')
}

function formatWarnings(value: readonly unknown[]): string {
  const warnings = value.map((item) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      throw new Error('invalid mall weather CSV warning')
    }
    const warning = item as Partial<MallWeatherWarning>
    if (typeof warning.code !== 'string' || typeof warning.path !== 'string') {
      throw new Error('invalid mall weather CSV warning')
    }
    return { code: warning.code, path: warning.path }
  })
  return JSON.stringify(warnings)
}

function protectSpreadsheetText(value: string): string {
  return /^(?:[\t\r\n]|[ \t\r\n]*[=+\-@])/.test(value) ? `'${value}` : value
}

function escapeCsvCell(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value
}

function safeMallCodeFileStem(value: string): string {
  const normalized = typeof value === 'string' ? value.normalize('NFKC').trim() : ''
  const safe = normalized
    .replace(/[^A-Za-z0-9_-]+/g, '_')
    .replace(/_+/g, '_')
    .replace(/^[_-]+|[_-]+$/g, '')
    .slice(0, 64)
  if (!safe) return 'mall'
  return /^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(safe) ? `_${safe}` : safe
}

function isSafeDownloadFileName(value: string): boolean {
  return typeof value === 'string' && value.length > 4 && value.length <= 128 &&
    /^(?![.])[A-Za-z0-9_-]+\.(?:csv|zip)$/.test(value)
}

type StoredZipInput = {
  name: string
  data: Uint8Array
}

type StoredZipEntry = StoredZipInput & {
  nameBytes: Uint8Array
  crc32: number
  offset: number
}

function createStoredZip(inputs: readonly StoredZipInput[]): Uint8Array {
  if (inputs.length === 0 || inputs.length > MAX_UINT16) throw new Error('invalid ZIP entry count')
  const entries: StoredZipEntry[] = []
  const localParts: Uint8Array[] = []
  let offset = 0
  for (const input of inputs) {
    if (!/^[a-z0-9_]+\.csv$/.test(input.name) || !(input.data instanceof Uint8Array)) {
      throw new Error('invalid ZIP entry')
    }
    const nameBytes = textEncoder.encode(input.name)
    assertUint16(nameBytes.byteLength, 'ZIP entry name')
    assertUint32(input.data.byteLength, 'ZIP entry data')
    const entry: StoredZipEntry = {
      ...input,
      nameBytes,
      crc32: crc32(input.data),
      offset,
    }
    const header = createLocalHeader(entry)
    localParts.push(header, input.data)
    offset = checkedZipSize(offset + header.byteLength + input.data.byteLength)
    entries.push(entry)
  }

  const centralOffset = offset
  const centralParts = entries.map(createCentralHeader)
  const centralSize = centralParts.reduce((total, part) => checkedZipSize(total + part.byteLength), 0)
  const end = createEndOfCentralDirectory(entries.length, centralSize, centralOffset)
  return concatenateBytes([...localParts, ...centralParts, end])
}

function createLocalHeader(entry: StoredZipEntry): Uint8Array {
  const header = new Uint8Array(30 + entry.nameBytes.byteLength)
  const view = new DataView(header.buffer)
  view.setUint32(0, 0x04034b50, true)
  view.setUint16(4, ZIP_VERSION, true)
  view.setUint16(6, UTF8_FLAG, true)
  view.setUint16(8, ZIP_STORED, true)
  view.setUint16(10, ZIP_DOS_TIME, true)
  view.setUint16(12, ZIP_DOS_DATE, true)
  view.setUint32(14, entry.crc32, true)
  view.setUint32(18, entry.data.byteLength, true)
  view.setUint32(22, entry.data.byteLength, true)
  view.setUint16(26, entry.nameBytes.byteLength, true)
  view.setUint16(28, 0, true)
  header.set(entry.nameBytes, 30)
  return header
}

function createCentralHeader(entry: StoredZipEntry): Uint8Array {
  const header = new Uint8Array(46 + entry.nameBytes.byteLength)
  const view = new DataView(header.buffer)
  view.setUint32(0, 0x02014b50, true)
  view.setUint16(4, ZIP_VERSION, true)
  view.setUint16(6, ZIP_VERSION, true)
  view.setUint16(8, UTF8_FLAG, true)
  view.setUint16(10, ZIP_STORED, true)
  view.setUint16(12, ZIP_DOS_TIME, true)
  view.setUint16(14, ZIP_DOS_DATE, true)
  view.setUint32(16, entry.crc32, true)
  view.setUint32(20, entry.data.byteLength, true)
  view.setUint32(24, entry.data.byteLength, true)
  view.setUint16(28, entry.nameBytes.byteLength, true)
  view.setUint16(30, 0, true)
  view.setUint16(32, 0, true)
  view.setUint16(34, 0, true)
  view.setUint16(36, 0, true)
  view.setUint32(38, 0, true)
  view.setUint32(42, entry.offset, true)
  header.set(entry.nameBytes, 46)
  return header
}

function createEndOfCentralDirectory(entryCount: number, centralSize: number, centralOffset: number): Uint8Array {
  assertUint16(entryCount, 'ZIP entry count')
  assertUint32(centralSize, 'ZIP central directory')
  assertUint32(centralOffset, 'ZIP central offset')
  const end = new Uint8Array(22)
  const view = new DataView(end.buffer)
  view.setUint32(0, 0x06054b50, true)
  view.setUint16(4, 0, true)
  view.setUint16(6, 0, true)
  view.setUint16(8, entryCount, true)
  view.setUint16(10, entryCount, true)
  view.setUint32(12, centralSize, true)
  view.setUint32(16, centralOffset, true)
  view.setUint16(20, 0, true)
  return end
}

const crc32Table = createCrc32Table()

function createCrc32Table(): Uint32Array {
  const table = new Uint32Array(256)
  for (let index = 0; index < table.length; index++) {
    let value = index
    for (let bit = 0; bit < 8; bit++) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1
    }
    table[index] = value >>> 0
  }
  return table
}

function crc32(data: Uint8Array): number {
  let value = 0xffffffff
  for (const byte of data) value = crc32Table[(value ^ byte) & 0xff] ^ (value >>> 8)
  return (value ^ 0xffffffff) >>> 0
}

function concatenateBytes(parts: readonly Uint8Array[]): Uint8Array {
  const total = parts.reduce((size, part) => checkedZipSize(size + part.byteLength), 0)
  const output = new Uint8Array(total)
  let offset = 0
  for (const part of parts) {
    output.set(part, offset)
    offset += part.byteLength
  }
  return output
}

function checkedZipSize(value: number): number {
  if (!Number.isSafeInteger(value) || value < 0 || value > MAX_UINT32) {
    throw new Error('mall weather CSV ZIP exceeds ZIP32 limits')
  }
  return value
}

function assertUint16(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0 || value > MAX_UINT16) throw new Error(`${label} exceeds ZIP limits`)
}

function assertUint32(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0 || value > MAX_UINT32) throw new Error(`${label} exceeds ZIP limits`)
}
