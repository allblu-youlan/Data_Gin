export type MallWeatherMall = {
  kaiyuShopId?: string
  id: number
  mallCode: string
  nameCn: string
  province: string
  city: string
  district: string
  address: string
  longitude?: number
  latitude?: number
  coordinateSystem: string
  geocodeStatus: string
  weatherEnabled: boolean
  detailProfile: string
  coverageRadiusM: number
  timeZone: string
  status: string
  version: number
}

export const mallWeatherMinutelyForecastMinutes = 120
export const mallWeatherHourlyForecastHours = 360
export const mallWeatherDailyForecastDays = 15
export const mallWeatherGeocodePollMaxAttempts = 24

export type MallWeatherMallList = {
  items: MallWeatherMall[]
  nextAfterId: number
}

export type MallWeatherCreateInput = {
  kaiyuShopId?: string
  mallCode: string
  nameCn: string
  province: string
  city: string
  district: string
  address: string
}

export type MallWeatherCreateRequest = {
  kaiyuShopId?: string
  mallCode: string
  nameCn: string
  province: string
  city: string
  district?: string
  address: string
  weather: { detailProfile: 'full'; coverageRadiusM: 1000 }
}

export type MallWeatherCreateResult = {
  id: number
  mallCode: string
  status: string
  geocodeStatus: string
  weatherStatus: string
  version: number
}

export type MallWeatherPatchRequest = {
  kaiyuShopId?: string
  expectedMallVersion: number
  nameCn?: string
  province?: string
  city?: string
  district?: string
  address?: string
}

export type MallWeatherPendingCreate = {
  key: string
  body: MallWeatherCreateRequest
}

export type MallWeatherGeocodeCandidate = {
  id: number
  candidateNo: number
  formattedAddress: string
  province: string
  city: string
  district: string
  longitude: number
  latitude: number
  coordinateSystem: string
  level: string
  confidenceScore: number
  selected: boolean
}

export type MallWeatherGeocodeCandidates = {
  mallId: number
  mallVersion: number
  runId: number
  runStatus: string
  items: MallWeatherGeocodeCandidate[]
}

export type MallWeatherGeocodeConfirmRequest = {
  candidateId?: number
  manualCoordinate?: {
    longitude: number
    latitude: number
    coordinateSystem: 'GCJ02'
    reason: string
  }
  expectedMallVersion: number
  weatherEnabled: true
}

export type MallWeatherGeocodeActionResponse = {
  ok: boolean
  status: number
  data: unknown
}

export type MallWeatherGeocodeTriggerOutcome =
  | { kind: 'accepted'; response: MallWeatherGeocodeActionResponse }
  | { kind: 'rejected'; response: MallWeatherGeocodeActionResponse }
  | { kind: 'latest_mall_unavailable' }
  | { kind: 'conflict'; refreshed: boolean }

export type MallWeatherGeocodeConfirmationOutcome =
  | { kind: 'accepted'; response: MallWeatherGeocodeActionResponse }
  | { kind: 'rejected'; response: MallWeatherGeocodeActionResponse }
  | { kind: 'stale'; refreshed: boolean }
  | { kind: 'conflict'; refreshed: boolean }

export type MallWeatherSheetPushOption = {
  destinationId: number
  name: string
  code: string
  profileId: number
  profileCode: string
  profileVersion: number
}

export type MallWeatherSheetPushRequest = {
  destinationId: number
  profileId: number
  expectedProfileVersion: number
  filters: { mallIds: number[] }
}

export type MallWeatherSheetPushDryRun = {
  destinationId: number
  destinationCode: string
  profileId: number
  profileCode: string
  profileVersion: number
  writeMode: string
  totalEstimatedRows: number
  totalEstimatedCells: number
  canExecute: boolean
  warnings: string[]
  datasets: MallWeatherSheetPushDatasetDryRun[]
}

export type MallWeatherSheetPushDatasetDryRun = {
  datasetKind: string
  estimatedRows: number
  estimatedCells: number
  canExecute: boolean
  warnings: string[]
}

export type MallWeatherSheetPushResult = {
  runId: number
  traceId: string
  status: string
  destinationId: number
  profileId: number
  profileVersion: number
  estimatedRows: number
}

export type MallWeatherSheetPushRun = {
  runId: number
  traceId: string
  status: 'PENDING' | 'RUNNING' | 'SUCCESS' | 'PARTIAL_SUCCESS' | 'FAILED'
  destinationId: number
  profileId: number
  profileVersion: number
  totalCount: number
  successCount: number
  failedCount: number
}

export type MallWeatherSheetPushPollResult =
  | { kind: 'terminal'; run: MallWeatherSheetPushRun }
  | { kind: 'timed_out' }
  | { kind: 'query_error'; status: number }
  | { kind: 'cancelled' }

export type MallWeatherPendingSheetPush = {
  key: string
  body: MallWeatherSheetPushRequest
}

export type MallWeatherWarning = {
  code: string
  path: string
}

export type MallWeatherMeta = {
  provider: string
  apiVersion: string
  representativePoint: string
  longitude: number
  latitude: number
  coordinateSystem: string
  samplingMode: string
  coverageRadiusM: number
  spatialResolution: string
  timeZone: string
  unit: string
  freshnessStatus: string
  dataAgeSeconds?: number
}

export type MallWeatherRealtime = {
  snapshotAtLocal: string
  providerServerTimeLocal: string
  fetchedAtLocal: string
  temperatureC?: number
  apparentTemperatureC?: number
  humidityPct?: number
  pressurePa?: number
  windSpeedKph?: number
  windDirectionDeg?: number
  cloudrateRatio?: number
  dswrfWM2?: number
  localPrecipitationStatus?: string
  localPrecipitationMmH?: number
  localPrecipitationSource?: string
  nearestPrecipitationStatus?: string
  nearestPrecipitationDistanceKm?: number
  nearestPrecipitationMmH?: number
  visibilityKm?: number
  skycon?: string
  pm25UgM3?: number
  pm10UgM3?: number
  o3UgM3?: number
  so2UgM3?: number
  no2UgM3?: number
  coMgM3?: number
  aqiChn?: number
  aqiUsa?: number
  aqiDescriptionChn?: string
  aqiDescriptionUsa?: string
  comfortIndex?: number
  comfortDescription?: string
  ultravioletIndex?: number
  ultravioletDescription?: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherMinutely = {
  forecastMinuteUtc: string
  forecastMinuteLocal: string
  issuedAtUtc: string
  issuedAtLocal: string
  fetchedAtUtc: string
  fetchedAtLocal: string
  minuteOffset: number
  precipitationMmH?: number
  probabilityPct?: number
  datasource?: string
  description?: string
  forecastKeypoint?: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherHourly = {
  forecastTimeUtc: string
  forecastTimeLocal: string
  issuedAtUtc: string
  issuedAtLocal: string
  fetchedAtUtc: string
  fetchedAtLocal: string
  temperatureC?: number
  apparentTemperatureC?: number
  pressurePa?: number
  humidityPct?: number
  precipitationMmH?: number
  precipitationProbabilityPct?: number
  windSpeedKph?: number
  windDirectionDeg?: number
  cloudrateRatio?: number
  dswrfWM2?: number
  visibilityKm?: number
  skycon?: string
  pm25UgM3?: number
  aqiChn?: number
  aqiUsa?: number
  hourlyDescription?: string
  forecastKeypoint?: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherAlert = {
  alertId: string
  status: string
  title: string
  description?: string
  code?: string
  alertTypeCode?: string
  alertLevelCode?: string
  alertTypeName?: string
  alertLevelName?: string
  publishedAtLocal?: string
  source?: string
  province?: string
  city?: string
  county?: string
  location?: string
  regionId?: string
  adcode?: string
  latitude?: number
  longitude?: number
  firstSeenAtLocal?: string
  lastSeenAtLocal?: string
  endedAtLocal?: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherOverview = {
  realtime: MallWeatherRealtime | null
  minutely: MallWeatherMinutely[]
  hourly: MallWeatherHourly[]
  alerts: MallWeatherAlert[]
  meta: MallWeatherMeta
}

export type MallWeatherDaily = {
  forecastDateLocal: string
  issuedAtUtc: string
  issuedAtLocal: string
  fetchedAtUtc: string
  fetchedAtLocal: string
  temperatureMaxC?: number
  temperatureMinC?: number
  temperatureAvgC?: number
  dayTemperatureMaxC?: number
  dayTemperatureMinC?: number
  dayTemperatureAvgC?: number
  nightTemperatureMaxC?: number
  nightTemperatureMinC?: number
  nightTemperatureAvgC?: number
  precipitationMaxMmH?: number
  precipitationMinMmH?: number
  precipitationAvgMmH?: number
  precipitationProbabilityPct?: number
  dayPrecipitationMaxMmH?: number
  dayPrecipitationMinMmH?: number
  dayPrecipitationAvgMmH?: number
  dayPrecipitationProbabilityPct?: number
  nightPrecipitationMaxMmH?: number
  nightPrecipitationMinMmH?: number
  nightPrecipitationAvgMmH?: number
  nightPrecipitationProbabilityPct?: number
  windMaxSpeedKph?: number
  windMaxDirectionDeg?: number
  windMinSpeedKph?: number
  windMinDirectionDeg?: number
  windAvgSpeedKph?: number
  windAvgDirectionDeg?: number
  dayWindMaxSpeedKph?: number
  dayWindMaxDirectionDeg?: number
  dayWindMinSpeedKph?: number
  dayWindMinDirectionDeg?: number
  dayWindAvgSpeedKph?: number
  dayWindAvgDirectionDeg?: number
  nightWindMaxSpeedKph?: number
  nightWindMaxDirectionDeg?: number
  nightWindMinSpeedKph?: number
  nightWindMinDirectionDeg?: number
  nightWindAvgSpeedKph?: number
  nightWindAvgDirectionDeg?: number
  humidityMaxPct?: number
  humidityMinPct?: number
  humidityAvgPct?: number
  cloudrateMaxRatio?: number
  cloudrateMinRatio?: number
  cloudrateAvgRatio?: number
  pressureMaxPa?: number
  pressureMinPa?: number
  pressureAvgPa?: number
  visibilityMaxKm?: number
  visibilityMinKm?: number
  visibilityAvgKm?: number
  dswrfMaxWM2?: number
  dswrfMinWM2?: number
  dswrfAvgWM2?: number
  pm25MaxUgM3?: number
  pm25MinUgM3?: number
  pm25AvgUgM3?: number
  aqiMaxChn?: number
  aqiMinChn?: number
  aqiAvgChn?: number
  aqiMaxUsa?: number
  aqiMinUsa?: number
  aqiAvgUsa?: number
  skycon: string
  daySkycon: string
  nightSkycon: string
  sunriseLocalTime: string
  sunsetLocalTime: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherLifeIndex = {
  sourceApi: 'v26_daily'
  forecastDateLocal: string
  indexType: number
  indexCode: string
  indexName: string
  level?: number
  shortDescription: string
  detail: string
  isUnknownType: boolean
  issuedAtUtc: string
  issuedAtLocal: string
  fetchedAtUtc: string
  fetchedAtLocal: string
  qualityStatus: string
  qualityWarnings: MallWeatherWarning[]
}

export type MallWeatherPageResult<T> = {
  items: T[]
  meta: MallWeatherMeta
  pagination: { pageSize: number; nextCursor: string }
}

export type MallWeatherSeries = 'minutely' | 'hourly' | 'daily' | 'alerts' | 'life-indices'

export type MallWeatherQueryWindow = {
  start: Date
  end: Date
}

export type MallWeatherForecastWindows = {
  minutely: MallWeatherQueryWindow
  hourly: MallWeatherQueryWindow
  daily: MallWeatherQueryWindow
}

export type MallWeatherPageParser<T> = (payload: unknown) => MallWeatherPageResult<T> | null

export type MallWeatherPageRequester = (path: string) => Promise<{
  ok: boolean
  status: number
  data: unknown
}>

export type MallWeatherRefreshKind = 'V26_FULL'

export type MallWeatherRefreshRequest = {
  kinds: MallWeatherRefreshKind[]
  force: false
  reason: string
}

export type MallWeatherPendingRefresh = {
  key: string
  body: MallWeatherRefreshRequest
}

export type MallWeatherRefreshResult = {
  jobId: number
  mallId: number
  force: boolean
  reason: string
  requestedBy: number
  requestedAt: string
  correlationId: string
  kinds: Array<{
    kind: MallWeatherRefreshKind
    status: 'QUEUED' | 'SKIPPED_FRESH'
    outboxJobId?: number
  }>
}

export type MallWeatherRefreshDisposition =
  | { kind: 'accepted'; result: MallWeatherRefreshResult }
  | { kind: 'uncertain' }
  | { kind: 'rejected' }

export type MallWeatherFetchRunTaskKind = 'MANUAL' | 'FULL'

export type MallWeatherFetchRun = {
  runUuid: string
  correlationId: string
  provider: string
  endpointKind: string
  taskKind: string
  requestedHourlySteps: number
  requestedDailySteps: number
  attemptCount: number
  status: string
  durationMs: number
  rowCounts: Record<string, number>
  parseWarnings: MallWeatherWarning[]
  errorCode: string
  errorMessageSafe: string
  createdAtUtc: string
  createdAtLocal: string
  updatedAtUtc: string
  updatedAtLocal: string
  finishedAtUtc?: string
  finishedAtLocal?: string
}

export type MallWeatherFetchRunsResult = {
  items: MallWeatherFetchRun[]
  meta: { timeZone: string }
  pagination: { pageSize: number; nextCursor: string }
}

export type MallWeatherFetchRunPollResult =
  | { kind: 'terminal'; run: MallWeatherFetchRun }
  | { kind: 'timed_out' }
  | { kind: 'cancelled' }
  | { kind: 'query_error'; status: number }

type MallWeatherFetchRunRequester = (
  path: string,
  options: { method: 'GET'; showResult: false; silentLoading: true; signal?: AbortSignal },
) => Promise<{ ok: boolean; status: number; data: unknown }>

type MallWeatherFetchRunPollOptions = {
  maxAttempts?: number
  intervalMs?: number
  signal?: AbortSignal
  now?: () => Date
  wait?: (intervalMs: number, signal?: AbortSignal) => Promise<void>
}

type MallWeatherSheetPushRequester = (
  path: string,
  options: { method: 'GET'; showResult: false; silentLoading: true; signal?: AbortSignal },
) => Promise<{ ok: boolean; status: number; data: unknown }>

type MallWeatherSheetPushPollOptions = {
  maxAttempts?: number
  intervalMs?: number
  signal?: AbortSignal
  isPageVisible?: () => boolean
  wait?: (intervalMs: number, signal?: AbortSignal) => Promise<void>
}

type JsonRecord = Record<string, unknown>
type RefreshStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
type OnboardingStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function positiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function nonNegativeSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function validCoordinateValue(value: unknown, minimum: number, maximum: number): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= minimum && value <= maximum
}

function positiveMallID(mallID: number) {
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  return mallID
}

function mallWeatherOperationKey(scope: string, seed?: string, invalidMessage = 'invalid operation key') {
  const value = seed || globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`
  const key = `${scope}:${value}`
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$/.test(key)) throw new Error(invalidMessage)
  return key
}

function envelopeData(payload: unknown): JsonRecord | null {
  if (!isRecord(payload) || (payload.code !== 0 && payload.code !== 200) || !isRecord(payload.data)) return null
  return payload.data
}

function textValue(record: JsonRecord, key: string) {
  return typeof record[key] === 'string' ? record[key] : ''
}

function aliasedValue(record: JsonRecord, primaryKey: string, legacyKey: string) {
  return record[primaryKey] !== undefined ? record[primaryKey] : record[legacyKey]
}

function aliasedTextValue(record: JsonRecord, primaryKey: string, legacyKey: string) {
  const value = aliasedValue(record, primaryKey, legacyKey)
  return typeof value === 'string' ? value : ''
}

function aliasedNumberValue(record: JsonRecord, primaryKey: string, legacyKey: string) {
  const value = aliasedValue(record, primaryKey, legacyKey)
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function positiveSafeIntegerValue(value: unknown) {
  if (positiveSafeInteger(value)) return value
  if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) return undefined
  const parsed = Number(value)
  return positiveSafeInteger(parsed) ? parsed : undefined
}

function nonNegativeSafeIntegerValue(value: unknown) {
  if (nonNegativeSafeInteger(value)) return value
  if (typeof value !== 'string' || !/^(?:0|[1-9]\d*)$/.test(value)) return undefined
  const parsed = Number(value)
  return nonNegativeSafeInteger(parsed) ? parsed : undefined
}

function numberValue(record: JsonRecord, key: string) {
  const value = record[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function isMallWeatherSheetPushStatus(value: unknown): value is MallWeatherSheetPushRun['status'] {
  return value === 'PENDING' || value === 'RUNNING' || value === 'SUCCESS' || value === 'PARTIAL_SUCCESS' || value === 'FAILED'
}

function mallWeatherMall(value: unknown): MallWeatherMall | null {
  if (!isRecord(value)) return null
  const id = positiveSafeIntegerValue(value.id)
  const version = positiveSafeIntegerValue(value.version)
  const mallCode = aliasedTextValue(value, 'mallCode', 'mall_code').trim()
  const nameCn = aliasedTextValue(value, 'nameCn', 'name_cn').trim()
  if (id === undefined || version === undefined || !mallCode || !nameCn) return null
  const longitude = numberValue(value, 'longitude')
  const latitude = numberValue(value, 'latitude')
  const coordinates = longitude !== undefined && latitude !== undefined &&
    validCoordinateValue(longitude, -180, 180) && validCoordinateValue(latitude, -90, 90)
    ? { longitude, latitude }
    : {}
  const configuredDetailProfile = aliasedTextValue(value, 'detailProfile', 'detail_profile').trim().toLowerCase()
  const detailProfile = ['full', 'standard', 'economy'].includes(configuredDetailProfile)
    ? configuredDetailProfile
    : 'full'
  const configuredCoverageRadiusM = aliasedNumberValue(value, 'coverageRadiusM', 'coverage_radius_m')
  const coverageRadiusM = configuredCoverageRadiusM !== undefined && Number.isSafeInteger(configuredCoverageRadiusM) &&
    configuredCoverageRadiusM >= 100 && configuredCoverageRadiusM <= 10000
    ? configuredCoverageRadiusM
    : 1000
  return {
    id,
    mallCode,
    nameCn,
    ...((typeof value.kaiyuShopId === 'string' || typeof value.kaiyu_shop_id === 'string') ? { kaiyuShopId: aliasedTextValue(value, 'kaiyuShopId', 'kaiyu_shop_id').trim() } : {}),
    province: textValue(value, 'province'),
    city: textValue(value, 'city'),
    district: textValue(value, 'district'),
    address: aliasedTextValue(value, 'address', 'address_raw'),
    ...coordinates,
    coordinateSystem: 'longitude' in coordinates ? aliasedTextValue(value, 'coordinateSystem', 'coordinate_system').trim().toUpperCase() : '',
    geocodeStatus: aliasedTextValue(value, 'geocodeStatus', 'geocode_status').trim().toLowerCase() || 'pending',
    weatherEnabled: aliasedValue(value, 'weatherEnabled', 'weather_enabled') === true,
    detailProfile,
    coverageRadiusM,
    timeZone: aliasedTextValue(value, 'timeZone', 'timezone').trim() || 'Asia/Shanghai',
    status: textValue(value, 'status').trim().toLowerCase() || 'draft',
    version,
  }
}

function warningValues(value: unknown): MallWeatherWarning[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((warning) => {
    if (!isRecord(warning) || typeof warning.code !== 'string' || typeof warning.path !== 'string') return []
    return [{ code: warning.code, path: warning.path }]
  })
}

function mallWeatherMeta(record: JsonRecord): MallWeatherMeta {
  return {
    provider: textValue(record, 'provider'),
    apiVersion: textValue(record, 'apiVersion'),
    representativePoint: textValue(record, 'representativePoint'),
    longitude: numberValue(record, 'longitude') ?? 0,
    latitude: numberValue(record, 'latitude') ?? 0,
    coordinateSystem: textValue(record, 'coordinateSystem'),
    samplingMode: textValue(record, 'samplingMode'),
    coverageRadiusM: numberValue(record, 'coverageRadiusM') ?? 0,
    spatialResolution: textValue(record, 'spatialResolution'),
    timeZone: textValue(record, 'timeZone'),
    unit: textValue(record, 'unit'),
    freshnessStatus: textValue(record, 'freshnessStatus'),
    dataAgeSeconds: numberValue(record, 'dataAgeSeconds'),
  }
}

function strictMallWeatherMeta(value: unknown): MallWeatherMeta | null {
  if (!isRecord(value)) return null
  const requiredText = ['provider', 'apiVersion', 'representativePoint', 'coordinateSystem', 'samplingMode', 'spatialResolution', 'timeZone', 'unit', 'freshnessStatus']
  if (requiredText.some((key) => typeof value[key] !== 'string' || !String(value[key]).trim())) return null
  const longitude = numberValue(value, 'longitude')
  const latitude = numberValue(value, 'latitude')
  const coverageRadiusM = numberValue(value, 'coverageRadiusM')
  const dataAgeSeconds = numberValue(value, 'dataAgeSeconds')
  if (longitude === undefined || longitude < -180 || longitude > 180 || latitude === undefined || latitude < -90 || latitude > 90 ||
    coverageRadiusM === undefined || !Number.isSafeInteger(coverageRadiusM) || coverageRadiusM < 0 ||
    (value.dataAgeSeconds !== undefined && (dataAgeSeconds === undefined || !Number.isSafeInteger(dataAgeSeconds) || dataAgeSeconds < 0))) return null
  return mallWeatherMeta(value)
}

function strictWarningValues(value: unknown): MallWeatherWarning[] | null {
  if (!Array.isArray(value)) return null
  const warnings: MallWeatherWarning[] = []
  for (const warning of value) {
    if (!isRecord(warning) || typeof warning.code !== 'string' || !warning.code.trim() || typeof warning.path !== 'string') return null
    warnings.push({ code: warning.code, path: warning.path })
  }
  return warnings
}

function hasOnlyOptionalFiniteNumbers(record: JsonRecord, keys: string[]) {
  return keys.every((key) => record[key] === undefined || typeof record[key] === 'number' && Number.isFinite(record[key]))
}

function hasOnlyOptionalStrings(record: JsonRecord, keys: string[]) {
  return keys.every((key) => record[key] === undefined || typeof record[key] === 'string')
}

function isRFC3339(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value))
}

function optionalRFC3339(value: unknown): value is string | undefined {
  return value === undefined || isRFC3339(value)
}

function isISODate(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const parsed = new Date(`${value}T00:00:00.000Z`)
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value
}

function mallWeatherRealtime(record: JsonRecord): MallWeatherRealtime {
  return {
    snapshotAtLocal: textValue(record, 'snapshotAtLocal'),
    providerServerTimeLocal: textValue(record, 'providerServerTimeLocal'),
    fetchedAtLocal: textValue(record, 'fetchedAtLocal'),
    temperatureC: numberValue(record, 'temperatureC'),
    apparentTemperatureC: numberValue(record, 'apparentTemperatureC'),
    humidityPct: numberValue(record, 'humidityPct'),
    pressurePa: numberValue(record, 'pressurePa'),
    windSpeedKph: numberValue(record, 'windSpeedKph'),
    windDirectionDeg: numberValue(record, 'windDirectionDeg'),
    cloudrateRatio: numberValue(record, 'cloudrateRatio'),
    dswrfWM2: numberValue(record, 'dswrfWM2'),
    localPrecipitationStatus: textValue(record, 'localPrecipitationStatus'),
    localPrecipitationMmH: numberValue(record, 'localPrecipitationMmH'),
    localPrecipitationSource: textValue(record, 'localPrecipitationSource'),
    nearestPrecipitationStatus: textValue(record, 'nearestPrecipitationStatus'),
    nearestPrecipitationDistanceKm: numberValue(record, 'nearestPrecipitationDistanceKm'),
    nearestPrecipitationMmH: numberValue(record, 'nearestPrecipitationMmH'),
    visibilityKm: numberValue(record, 'visibilityKm'),
    skycon: textValue(record, 'skycon'),
    pm25UgM3: numberValue(record, 'pm25UgM3'),
    pm10UgM3: numberValue(record, 'pm10UgM3'),
    o3UgM3: numberValue(record, 'o3UgM3'),
    so2UgM3: numberValue(record, 'so2UgM3'),
    no2UgM3: numberValue(record, 'no2UgM3'),
    coMgM3: numberValue(record, 'coMgM3'),
    aqiChn: numberValue(record, 'aqiChn'),
    aqiUsa: numberValue(record, 'aqiUsa'),
    aqiDescriptionChn: textValue(record, 'aqiDescriptionChn'),
    aqiDescriptionUsa: textValue(record, 'aqiDescriptionUsa'),
    comfortIndex: numberValue(record, 'comfortIndex'),
    comfortDescription: textValue(record, 'comfortDescription'),
    ultravioletIndex: numberValue(record, 'ultravioletIndex'),
    ultravioletDescription: textValue(record, 'ultravioletDescription'),
    qualityStatus: textValue(record, 'qualityStatus'),
    qualityWarnings: warningValues(record.qualityWarnings),
  }
}

function mallWeatherMinutely(record: JsonRecord, qualityWarnings = warningValues(record.qualityWarnings)): MallWeatherMinutely {
  return {
    forecastMinuteUtc: textValue(record, 'forecastMinuteUtc'),
    forecastMinuteLocal: textValue(record, 'forecastMinuteLocal'),
    issuedAtUtc: textValue(record, 'issuedAtUtc'),
    issuedAtLocal: textValue(record, 'issuedAtLocal'),
    fetchedAtUtc: textValue(record, 'fetchedAtUtc'),
    fetchedAtLocal: textValue(record, 'fetchedAtLocal'),
    minuteOffset: numberValue(record, 'minuteOffset') ?? 0,
    precipitationMmH: numberValue(record, 'precipitationMmH'),
    probabilityPct: numberValue(record, 'probabilityPct'),
    datasource: textValue(record, 'datasource'),
    description: textValue(record, 'description'),
    forecastKeypoint: textValue(record, 'forecastKeypoint'),
    qualityStatus: textValue(record, 'qualityStatus'),
    qualityWarnings,
  }
}

function mallWeatherHourly(record: JsonRecord, qualityWarnings = warningValues(record.qualityWarnings)): MallWeatherHourly {
  return {
    forecastTimeUtc: textValue(record, 'forecastTimeUtc'),
    forecastTimeLocal: textValue(record, 'forecastTimeLocal'),
    issuedAtUtc: textValue(record, 'issuedAtUtc'),
    issuedAtLocal: textValue(record, 'issuedAtLocal'),
    fetchedAtUtc: textValue(record, 'fetchedAtUtc'),
    fetchedAtLocal: textValue(record, 'fetchedAtLocal'),
    temperatureC: numberValue(record, 'temperatureC'),
    apparentTemperatureC: numberValue(record, 'apparentTemperatureC'),
    pressurePa: numberValue(record, 'pressurePa'),
    humidityPct: numberValue(record, 'humidityPct'),
    precipitationMmH: numberValue(record, 'precipitationMmH'),
    precipitationProbabilityPct: numberValue(record, 'precipitationProbabilityPct'),
    windSpeedKph: numberValue(record, 'windSpeedKph'),
    windDirectionDeg: numberValue(record, 'windDirectionDeg'),
    cloudrateRatio: numberValue(record, 'cloudrateRatio'),
    dswrfWM2: numberValue(record, 'dswrfWM2'),
    visibilityKm: numberValue(record, 'visibilityKm'),
    skycon: textValue(record, 'skycon'),
    pm25UgM3: numberValue(record, 'pm25UgM3'),
    aqiChn: numberValue(record, 'aqiChn'),
    aqiUsa: numberValue(record, 'aqiUsa'),
    hourlyDescription: textValue(record, 'hourlyDescription'),
    forecastKeypoint: textValue(record, 'forecastKeypoint'),
    qualityStatus: textValue(record, 'qualityStatus'),
    qualityWarnings,
  }
}

function mallWeatherAlert(record: JsonRecord): MallWeatherAlert {
  return {
    alertId: textValue(record, 'alertId'),
    status: textValue(record, 'status'),
    title: textValue(record, 'title'),
    description: textValue(record, 'description'),
    code: textValue(record, 'code'),
    alertTypeCode: textValue(record, 'alertTypeCode'),
    alertLevelCode: textValue(record, 'alertLevelCode'),
    alertTypeName: textValue(record, 'alertTypeName'),
    alertLevelName: textValue(record, 'alertLevelName'),
    publishedAtLocal: textValue(record, 'publishedAtLocal'),
    source: textValue(record, 'source'),
    province: textValue(record, 'province'),
    city: textValue(record, 'city'),
    county: textValue(record, 'county'),
    location: textValue(record, 'location'),
    regionId: textValue(record, 'regionId'),
    adcode: textValue(record, 'adcode'),
    latitude: numberValue(record, 'latitude'),
    longitude: numberValue(record, 'longitude'),
    firstSeenAtLocal: textValue(record, 'firstSeenAtLocal'),
    lastSeenAtLocal: textValue(record, 'lastSeenAtLocal'),
    endedAtLocal: textValue(record, 'endedAtLocal'),
    qualityStatus: textValue(record, 'qualityStatus'),
    qualityWarnings: warningValues(record.qualityWarnings),
  }
}

export function parseMallWeatherMallList(payload: unknown): MallWeatherMallList | null {
  const data = envelopeData(payload)
  if (!data) return null
  const rawItems = Array.isArray(data.items) ? data.items : Array.isArray(data.result) ? data.result : null
  if (!rawItems) return null

  const items: MallWeatherMall[] = []
  for (const item of rawItems) {
    const mall = mallWeatherMall(item)
    if (mall) items.push(mall)
  }
  if (rawItems.length > 0 && items.length === 0) return null
  const rawNextAfterId = aliasedValue(data, 'nextAfterId', 'next_after_id')
  const nextAfterId = rawNextAfterId === undefined ? 0 : nonNegativeSafeIntegerValue(rawNextAfterId)
  if (nextAfterId === undefined || !Number.isSafeInteger(nextAfterId) || nextAfterId < 0) return null
  return { items, nextAfterId }
}

export function parseMallWeatherMall(payload: unknown): MallWeatherMall | null {
  const data = envelopeData(payload)
  return data ? mallWeatherMall(data) : null
}

export function parseMallWeatherCreateResult(payload: unknown): MallWeatherCreateResult | null {
  const data = envelopeData(payload)
  if (!data || !positiveSafeInteger(data.id) || !positiveSafeInteger(data.version) || typeof data.mallCode !== 'string') return null
  return {
    id: Number(data.id),
    mallCode: data.mallCode,
    status: textValue(data, 'status'),
    geocodeStatus: textValue(data, 'geocodeStatus'),
    weatherStatus: textValue(data, 'weatherStatus'),
    version: Number(data.version),
  }
}

export function parseMallWeatherGeocodeCandidates(payload: unknown): MallWeatherGeocodeCandidates | null {
  const data = envelopeData(payload)
  if (!data || !positiveSafeInteger(data.mallId) || !positiveSafeInteger(data.mallVersion) || !Array.isArray(data.items)) return null
  const items: MallWeatherGeocodeCandidate[] = []
  for (const item of data.items) {
    if (!isRecord(item) || !positiveSafeInteger(item.id) || !Number.isSafeInteger(item.candidateNo) || Number(item.candidateNo) < 1 ||
      typeof item.formattedAddress !== 'string' || !validCoordinateValue(item.longitude, -180, 180) ||
      !validCoordinateValue(item.latitude, -90, 90) || typeof item.coordinateSystem !== 'string' ||
      item.coordinateSystem.trim().toUpperCase() !== 'GCJ02' || typeof item.confidenceScore !== 'number' ||
      !Number.isFinite(item.confidenceScore) || item.confidenceScore < 0 || item.confidenceScore > 100) return null
    items.push({
      id: Number(item.id),
      candidateNo: Number(item.candidateNo),
      formattedAddress: item.formattedAddress,
      province: textValue(item, 'province'),
      city: textValue(item, 'city'),
      district: textValue(item, 'district'),
      longitude: Number(item.longitude),
      latitude: Number(item.latitude),
      coordinateSystem: 'GCJ02',
      level: textValue(item, 'level'),
      confidenceScore: item.confidenceScore,
      selected: item.selected === true,
    })
  }
  const runId = data.runId === undefined ? 0 : numberValue(data, 'runId')
  if (runId === undefined || !Number.isSafeInteger(runId) || runId < 0) return null
  return {
    mallId: Number(data.mallId),
    mallVersion: Number(data.mallVersion),
    runId,
    runStatus: textValue(data, 'runStatus'),
    items,
  }
}

export function mallWeatherCreateRequest(input: MallWeatherCreateInput): MallWeatherCreateRequest {
  const mallCode = input.mallCode.trim().toUpperCase()
  const nameCn = input.nameCn.trim()
  const province = input.province.trim()
  const city = input.city.trim()
  const district = input.district.trim()
  const address = input.address.trim()
  const kaiyuShopId = input.kaiyuShopId?.trim()
  if (kaiyuShopId && kaiyuShopId.length > 64) throw new Error('invalid kaiyu shop id')
  if (!/^[A-Z0-9][A-Z0-9_-]{1,63}$/.test(mallCode) || !nameCn || !province || !city || !address ||
    nameCn.length > 255 || province.length > 128 || city.length > 128 || district.length > 128 || address.length > 1000) {
    throw new Error('invalid mall create request')
  }
  return {
    mallCode,
    nameCn,
    province,
    city,
    ...(district ? { district } : {}),
    address,
    ...(kaiyuShopId ? { kaiyuShopId } : {}),
    weather: { detailProfile: 'full', coverageRadiusM: 1000 },
  }
}

export function mallWeatherCreateKey(seed?: string) {
  return mallWeatherOperationKey('mall-create', seed)
}

export function mallWeatherMallPatchRequest(
  mall: MallWeatherMall,
  input: Omit<MallWeatherCreateInput, 'mallCode'>,
): MallWeatherPatchRequest | null {
  if (!positiveSafeInteger(mall.id) || !positiveSafeInteger(mall.version)) throw new Error('invalid mall patch request')
  const normalized = mallWeatherCreateRequest({ mallCode: mall.mallCode, ...input })
  const request: MallWeatherPatchRequest = { expectedMallVersion: mall.version }
  if (input.kaiyuShopId !== undefined && (normalized.kaiyuShopId || '') !== (mall.kaiyuShopId || '')) request.kaiyuShopId = normalized.kaiyuShopId || ''
  if (normalized.nameCn !== mall.nameCn.trim()) request.nameCn = normalized.nameCn
  if (normalized.province !== mall.province.trim()) request.province = normalized.province
  if (normalized.city !== mall.city.trim()) request.city = normalized.city
  const district = normalized.district ?? ''
  if (district !== mall.district.trim()) request.district = district
  if (normalized.address !== mall.address.trim()) request.address = normalized.address
  return Object.keys(request).length > 1 ? request : null
}

export function mallWeatherMallPath(mallID: number) {
  return `/v1/malls/${positiveMallID(mallID)}`
}

export function mallWeatherMallDeletePath(mallID: number, expectedMallVersion: number) {
  if (!positiveSafeInteger(expectedMallVersion)) throw new Error('invalid mall delete version')
  return `${mallWeatherMallPath(mallID)}?${new URLSearchParams({ expectedMallVersion: String(expectedMallVersion) }).toString()}`
}

export function mallWeatherMallReady(mall: MallWeatherMall) {
  return mall.status.toLowerCase() === 'active' && mall.geocodeStatus.toLowerCase() === 'confirmed' && mall.weatherEnabled &&
    mall.longitude !== undefined && mall.latitude !== undefined && mall.coordinateSystem.trim().toUpperCase() === 'GCJ02'
}

export function mergeMallWeatherMalls(current: MallWeatherMall[], incoming: MallWeatherMall[]) {
  const byID = new Map(current.map((mall) => [mall.id, mall]))
  for (const mall of incoming) {
    const existing = byID.get(mall.id)
    if (!existing || mall.version >= existing.version) byID.set(mall.id, mall)
  }
  return Array.from(byID.values())
}

export function loadMallWeatherPendingCreate(actorID: string, storage: OnboardingStorage): MallWeatherPendingCreate | null {
  const storageKey = mallWeatherPendingCreateStorageKey(actorID)
  const raw = storage.getItem(storageKey)
  if (!raw) return null
  try {
    const snapshot: unknown = JSON.parse(raw)
    if (!isRecord(snapshot) || typeof snapshot.key !== 'string' || !isRecord(snapshot.body) || !validMallWeatherCreateKey(snapshot.key)) return null
    const body = mallWeatherCreateRequest({
      mallCode: textValue(snapshot.body, 'mallCode'),
      nameCn: textValue(snapshot.body, 'nameCn'),
      province: textValue(snapshot.body, 'province'),
      city: textValue(snapshot.body, 'city'),
      district: textValue(snapshot.body, 'district'),
      address: textValue(snapshot.body, 'address'),
    })
    return { key: snapshot.key, body }
  } catch {
    return null
  }
}

export function saveMallWeatherPendingCreate(actorID: string, pending: MallWeatherPendingCreate, storage: OnboardingStorage) {
  if (!validMallWeatherCreateKey(pending.key)) throw new Error('invalid create key')
  const body = mallWeatherCreateRequest({
    mallCode: pending.body.mallCode,
    nameCn: pending.body.nameCn,
    province: pending.body.province,
    city: pending.body.city,
    district: pending.body.district || '',
    address: pending.body.address,
  })
  storage.setItem(mallWeatherPendingCreateStorageKey(actorID), JSON.stringify({ key: pending.key, body }))
}

export function clearMallWeatherPendingCreate(actorID: string, storage: OnboardingStorage) {
  storage.removeItem(mallWeatherPendingCreateStorageKey(actorID))
}

export function mallWeatherGeocodeCandidatesPath(mallID: number) {
  return `/v1/malls/${positiveMallID(mallID)}/geocode-candidates`
}

export function mallWeatherGeocodeTriggerPath(mallID: number) {
  return `/v1/malls/${positiveMallID(mallID)}/geocode`
}

export function mallWeatherGeocodeConfirmPath(mallID: number) {
  return `/v1/malls/${positiveMallID(mallID)}/geocode-confirm`
}

export function mallWeatherGeocodeRunTerminal(status: string) {
  return ['SUCCEEDED', 'FAILED', 'STALE', 'NO_CANDIDATES', 'AUTO_CONFIRMED', 'REVIEW_REQUIRED'].includes(status.toUpperCase())
}

export function mallWeatherShouldPollGeocode(
  mallGeocodeStatus: string,
  candidates: MallWeatherGeocodeCandidates | null,
  candidateLoading = false,
) {
  if (candidateLoading) return false
  if (mallGeocodeStatus.trim().toUpperCase() === 'PENDING') return true
  return Boolean(candidates && candidates.items.length === 0 && !mallWeatherGeocodeRunTerminal(candidates.runStatus))
}

export function mallWeatherGeocodePollDelayMilliseconds(failureCount: number, isPageVisible: boolean) {
  if (!Number.isSafeInteger(failureCount) || failureCount < 0) throw new Error('invalid geocode poll failure count')
  const retryMultiplier = 2 ** Math.min(failureCount, 3)
  const visibilityMultiplier = isPageVisible ? 1 : 6
  return Math.min(60_000, 5_000 * retryMultiplier * visibilityMultiplier)
}

type MallWeatherGeocodeActionRequester = (
  path: string,
  options: { method: 'POST'; body: unknown; showResult: false; silentLoading: true },
) => Promise<MallWeatherGeocodeActionResponse>

type MallWeatherMallReloader = () => Promise<MallWeatherMall | null>
type MallWeatherCandidatesReloader = () => Promise<boolean>

async function refreshMallWeatherGeocodeState(
  reloadMall: MallWeatherMallReloader,
  reloadCandidates: MallWeatherCandidatesReloader,
) {
  const candidatesLoaded = await reloadCandidates()
  const mall = await reloadMall()
  return Boolean(mall && candidatesLoaded)
}

export async function submitMallWeatherGeocodeTrigger(
  request: MallWeatherGeocodeActionRequester,
  mallID: number,
  reloadMall: MallWeatherMallReloader,
  reloadCandidates: MallWeatherCandidatesReloader,
): Promise<MallWeatherGeocodeTriggerOutcome> {
  const latestMall = await reloadMall()
  if (!latestMall) return { kind: 'latest_mall_unavailable' }
  const response = await request(mallWeatherGeocodeTriggerPath(mallID), {
    method: 'POST',
    body: { expectedMallVersion: latestMall.version },
    showResult: false,
    silentLoading: true,
  })
  if (response.status === 409) {
    return { kind: 'conflict', refreshed: await refreshMallWeatherGeocodeState(reloadMall, reloadCandidates) }
  }
  return response.ok && response.status === 202 ? { kind: 'accepted', response } : { kind: 'rejected', response }
}

export async function submitMallWeatherGeocodeConfirmation(
  request: MallWeatherGeocodeActionRequester,
  mallID: number,
  mallVersion: number,
  candidateVersion: number,
  body: MallWeatherGeocodeConfirmRequest,
  reloadMall: MallWeatherMallReloader,
  reloadCandidates: MallWeatherCandidatesReloader,
): Promise<MallWeatherGeocodeConfirmationOutcome> {
  if (mallVersion !== candidateVersion || body.expectedMallVersion !== candidateVersion) {
    return { kind: 'stale', refreshed: await refreshMallWeatherGeocodeState(reloadMall, reloadCandidates) }
  }
  const response = await request(mallWeatherGeocodeConfirmPath(mallID), {
    method: 'POST', body, showResult: false, silentLoading: true,
  })
  if (response.status === 409) {
    return { kind: 'conflict', refreshed: await refreshMallWeatherGeocodeState(reloadMall, reloadCandidates) }
  }
  return response.ok && response.status === 200 ? { kind: 'accepted', response } : { kind: 'rejected', response }
}

export function mallWeatherCandidateConfirmationRequest(
  candidate: MallWeatherGeocodeCandidate,
  longitudeInput: string,
  latitudeInput: string,
  reasonInput: string,
  expectedMallVersion: number,
): MallWeatherGeocodeConfirmRequest {
  if (!positiveSafeInteger(candidate.id) || candidate.coordinateSystem.trim().toUpperCase() !== 'GCJ02' ||
    !positiveSafeInteger(expectedMallVersion)) throw new Error('invalid geocode candidate')
  const coordinate = mallWeatherCoordinateInput(longitudeInput, latitudeInput, reasonInput)
  if (coordinate.longitude === candidate.longitude && coordinate.latitude === candidate.latitude) {
    return { candidateId: candidate.id, expectedMallVersion, weatherEnabled: true }
  }
  return { manualCoordinate: coordinate, expectedMallVersion, weatherEnabled: true }
}

export function mallWeatherCoordinateAdjustmentRequest(
  mall: MallWeatherMall,
  longitudeInput: string,
  latitudeInput: string,
  reasonInput: string,
): MallWeatherGeocodeConfirmRequest {
  if (!positiveSafeInteger(mall.version) || mall.longitude === undefined || mall.latitude === undefined ||
    mall.coordinateSystem.trim().toUpperCase() !== 'GCJ02') throw new Error('invalid mall coordinate')
  return {
    manualCoordinate: mallWeatherCoordinateInput(longitudeInput, latitudeInput, reasonInput),
    expectedMallVersion: mall.version,
    weatherEnabled: true,
  }
}

export function mallWeatherManualCoordinateConfirmationRequest(
  expectedMallVersion: number,
  longitudeInput: string,
  latitudeInput: string,
  reasonInput: string,
): MallWeatherGeocodeConfirmRequest {
  if (!positiveSafeInteger(expectedMallVersion)) throw new Error('invalid mall coordinate version')
  return {
    manualCoordinate: mallWeatherCoordinateInput(longitudeInput, latitudeInput, reasonInput),
    expectedMallVersion,
    weatherEnabled: true,
  }
}

function mallWeatherCoordinateInput(longitudeInput: string, latitudeInput: string, reasonInput: string) {
  const longitude = Number(longitudeInput.trim())
  const latitude = Number(latitudeInput.trim())
  const reason = reasonInput.trim()
  if (!longitudeInput.trim() || !latitudeInput.trim() || !validCoordinateValue(longitude, -180, 180) ||
    !validCoordinateValue(latitude, -90, 90) || !reason || reason.length > 500 || reason.includes('\n') || reason.includes('\r')) {
    throw new Error('invalid coordinate adjustment')
  }
  return { longitude, latitude, coordinateSystem: 'GCJ02' as const, reason }
}

export function parseMallWeatherSheetPushOptions(payload: unknown): MallWeatherSheetPushOption[] | null {
  const data = envelopeData(payload)
  if (!data || !Array.isArray(data.items)) return null
  const items: MallWeatherSheetPushOption[] = []
  for (const item of data.items) {
    if (!isRecord(item) || !positiveSafeInteger(item.destinationId) || !positiveSafeInteger(item.profileId) ||
      !positiveSafeInteger(item.profileVersion) || typeof item.name !== 'string' || !item.name.trim() ||
      typeof item.code !== 'string' || !item.code.trim() || typeof item.profileCode !== 'string' || !item.profileCode.trim()) return null
    items.push({
      destinationId: item.destinationId,
      name: item.name,
      code: item.code,
      profileId: item.profileId,
      profileCode: item.profileCode,
      profileVersion: item.profileVersion,
    })
  }
  return items
}

export function mallWeatherSheetPushRequest(option: MallWeatherSheetPushOption, mallID: number): MallWeatherSheetPushRequest {
  if (!positiveSafeInteger(option.destinationId) || !positiveSafeInteger(option.profileId) || !positiveSafeInteger(option.profileVersion)) {
    throw new Error('invalid weather sheet push option')
  }
  return {
    destinationId: option.destinationId,
    profileId: option.profileId,
    expectedProfileVersion: option.profileVersion,
    filters: { mallIds: [positiveMallID(mallID)] },
  }
}

export function mallWeatherSheetPushRequestMatchesOption(request: MallWeatherSheetPushRequest, option: MallWeatherSheetPushOption, mallID: number) {
  return request.destinationId === option.destinationId && request.profileId === option.profileId &&
    request.expectedProfileVersion === option.profileVersion && request.filters.mallIds.length === 1 &&
    request.filters.mallIds[0] === mallID
}

export function mallWeatherSheetPushResultMatchesRequest(result: MallWeatherSheetPushResult, request: MallWeatherSheetPushRequest) {
  return result.destinationId === request.destinationId && result.profileId === request.profileId &&
    result.profileVersion === request.expectedProfileVersion
}

export function parseMallWeatherSheetPushDryRun(payload: unknown): MallWeatherSheetPushDryRun | null {
  const data = envelopeData(payload)
  if (!data || !positiveSafeInteger(data.destinationId) || !positiveSafeInteger(data.profileId) || !positiveSafeInteger(data.profileVersion) ||
    typeof data.destinationCode !== 'string' || typeof data.profileCode !== 'string' || typeof data.writeMode !== 'string' ||
    !Number.isSafeInteger(data.totalEstimatedRows) || Number(data.totalEstimatedRows) < 0 ||
    !Number.isSafeInteger(data.totalEstimatedCells) || Number(data.totalEstimatedCells) < 0 || typeof data.canExecute !== 'boolean' ||
    !Array.isArray(data.warnings) || !data.warnings.every((warning) => typeof warning === 'string') || !Array.isArray(data.datasets)) return null
  const datasets: MallWeatherSheetPushDatasetDryRun[] = []
  for (const dataset of data.datasets) {
    if (!isRecord(dataset) || typeof dataset.datasetKind !== 'string' || !dataset.datasetKind.trim() ||
      !Number.isSafeInteger(dataset.estimatedRows) || Number(dataset.estimatedRows) < 0 ||
      !Number.isSafeInteger(dataset.estimatedCells) || Number(dataset.estimatedCells) < 0 || typeof dataset.canExecute !== 'boolean' ||
      !Array.isArray(dataset.warnings) || !dataset.warnings.every((warning) => typeof warning === 'string')) return null
    datasets.push({
      datasetKind: dataset.datasetKind,
      estimatedRows: Number(dataset.estimatedRows),
      estimatedCells: Number(dataset.estimatedCells),
      canExecute: dataset.canExecute,
      warnings: dataset.warnings.map((warning) => String(warning)),
    })
  }
  return {
    destinationId: data.destinationId,
    destinationCode: data.destinationCode,
    profileId: data.profileId,
    profileCode: data.profileCode,
    profileVersion: data.profileVersion,
    writeMode: data.writeMode,
    totalEstimatedRows: Number(data.totalEstimatedRows),
    totalEstimatedCells: Number(data.totalEstimatedCells),
    canExecute: data.canExecute,
    warnings: data.warnings.map((warning) => String(warning)),
    datasets,
  }
}

export function parseMallWeatherSheetPushResult(payload: unknown): MallWeatherSheetPushResult | null {
  const data = envelopeData(payload)
  if (!data || !positiveSafeInteger(data.runId) || !positiveSafeInteger(data.destinationId) || !positiveSafeInteger(data.profileId) ||
    !positiveSafeInteger(data.profileVersion) || typeof data.traceId !== 'string' || !data.traceId.trim() || typeof data.status !== 'string' ||
    !Number.isSafeInteger(data.estimatedRows) || Number(data.estimatedRows) < 0) return null
  return {
    runId: data.runId,
    traceId: data.traceId,
    status: data.status,
    destinationId: data.destinationId,
    profileId: data.profileId,
    profileVersion: data.profileVersion,
    estimatedRows: Number(data.estimatedRows),
  }
}

export function parseMallWeatherSheetPushRun(payload: unknown): MallWeatherSheetPushRun | null {
  const data = envelopeData(payload)
  if (!data || !positiveSafeInteger(data.runId) || !positiveSafeInteger(data.destinationId) || !positiveSafeInteger(data.profileId) ||
    !positiveSafeInteger(data.profileVersion) || typeof data.traceId !== 'string' || !data.traceId.trim() || !isMallWeatherSheetPushStatus(data.status) ||
    !nonNegativeSafeInteger(data.totalCount) || !nonNegativeSafeInteger(data.successCount) || !nonNegativeSafeInteger(data.failedCount) ||
    Number(data.successCount) + Number(data.failedCount) > Number(data.totalCount)) return null
  return {
    runId: data.runId,
    traceId: data.traceId,
    status: data.status,
    destinationId: data.destinationId,
    profileId: data.profileId,
    profileVersion: data.profileVersion,
    totalCount: data.totalCount,
    successCount: data.successCount,
    failedCount: data.failedCount,
  }
}

export function mallWeatherSheetPushRunTerminal(status: string) {
  return ['SUCCESS', 'PARTIAL_SUCCESS', 'FAILED'].includes(status.trim().toUpperCase())
}

export function mallWeatherSheetPushRunMatchesResult(run: MallWeatherSheetPushRun, result: MallWeatherSheetPushResult) {
  return run.runId === result.runId && run.destinationId === result.destinationId && run.profileId === result.profileId &&
    run.profileVersion === result.profileVersion && run.traceId === result.traceId
}

export async function pollMallWeatherSheetPushRun(
  request: MallWeatherSheetPushRequester,
  runID: number,
  options: MallWeatherSheetPushPollOptions = {},
): Promise<MallWeatherSheetPushPollResult> {
  const maxAttempts = options.maxAttempts ?? 30
  const intervalMs = options.intervalMs ?? 2_000
  const wait = options.wait ?? waitForMallWeatherPoll
  if (!positiveSafeInteger(runID) || !Number.isSafeInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 120 ||
    !Number.isFinite(intervalMs) || intervalMs < 0 || intervalMs > 60_000) throw new Error('invalid weather sheet push poll')

  let failures = 0
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (options.signal?.aborted) return { kind: 'cancelled' }
    const response = await request(`/v1/weather-sheet-pushes/${runID}`, {
      method: 'GET', showResult: false, silentLoading: true, signal: options.signal,
    })
    if (options.signal?.aborted) return { kind: 'cancelled' }
    if (!response.ok) {
      const retryable = response.status === 0 || response.status === 408 || response.status === 425 || response.status === 429 || response.status >= 500
      if (!retryable || attempt + 1 >= maxAttempts) return { kind: 'query_error', status: response.status }
      failures++
      await wait(sheetPushPollDelay(intervalMs, failures, options.isPageVisible), options.signal)
      continue
    }
    const run = parseMallWeatherSheetPushRun(response.data)
    if (!run) return { kind: 'query_error', status: response.status }
    if (mallWeatherSheetPushRunTerminal(run.status)) return { kind: 'terminal', run }
    failures = 0
    if (attempt + 1 < maxAttempts) await wait(sheetPushPollDelay(intervalMs, failures, options.isPageVisible), options.signal)
  }
  return options.signal?.aborted ? { kind: 'cancelled' } : { kind: 'timed_out' }
}

export function mallWeatherSheetPushKey(seed?: string) {
  return mallWeatherOperationKey('weather-sheet-push', seed)
}

export function loadMallWeatherPendingSheetPush(actorID: string, mallID: number, storage: OnboardingStorage): MallWeatherPendingSheetPush | null {
  const storageKey = mallWeatherPendingSheetPushStorageKey(actorID, mallID)
  const raw = storage.getItem(storageKey)
  if (!raw) return null
  try {
    const snapshot: unknown = JSON.parse(raw)
    if (!isRecord(snapshot) || typeof snapshot.key !== 'string' || !validMallWeatherSheetPushKey(snapshot.key) || !isRecord(snapshot.body) ||
      !positiveSafeInteger(snapshot.body.destinationId) || !positiveSafeInteger(snapshot.body.profileId) ||
      !positiveSafeInteger(snapshot.body.expectedProfileVersion) || !isRecord(snapshot.body.filters) ||
      !Array.isArray(snapshot.body.filters.mallIds) || snapshot.body.filters.mallIds.length !== 1 || snapshot.body.filters.mallIds[0] !== mallID) return null
    return {
      key: snapshot.key,
      body: {
        destinationId: snapshot.body.destinationId,
        profileId: snapshot.body.profileId,
        expectedProfileVersion: snapshot.body.expectedProfileVersion,
        filters: { mallIds: [mallID] },
      },
    }
  } catch {
    return null
  }
}

export function saveMallWeatherPendingSheetPush(actorID: string, mallID: number, pending: MallWeatherPendingSheetPush, storage: OnboardingStorage) {
  if (!validMallWeatherSheetPushKey(pending.key) || !positiveSafeInteger(pending.body.destinationId) ||
    !positiveSafeInteger(pending.body.profileId) || !positiveSafeInteger(pending.body.expectedProfileVersion) ||
    pending.body.filters.mallIds.length !== 1 || pending.body.filters.mallIds[0] !== mallID) {
    throw new Error('invalid weather sheet push')
  }
  storage.setItem(mallWeatherPendingSheetPushStorageKey(actorID, mallID), JSON.stringify(pending))
}

export function clearMallWeatherPendingSheetPush(actorID: string, mallID: number, storage: OnboardingStorage) {
  storage.removeItem(mallWeatherPendingSheetPushStorageKey(actorID, mallID))
}

export function parseMallWeatherOverview(payload: unknown): MallWeatherOverview | null {
  const data = envelopeData(payload)
  if (!data || !isRecord(data.meta) || !Array.isArray(data.minutely) || !Array.isArray(data.hourly) || !Array.isArray(data.alerts)) return null

  return {
    realtime: isRecord(data.realtime) ? mallWeatherRealtime(data.realtime) : null,
    minutely: data.minutely.filter(isRecord).map((item) => mallWeatherMinutely(item)),
    hourly: data.hourly.filter(isRecord).map((item) => mallWeatherHourly(item)),
    alerts: data.alerts.filter(isRecord).map(mallWeatherAlert),
    meta: mallWeatherMeta(data.meta),
  }
}

export type MallWeatherRealtimePage = {
  items: MallWeatherRealtime[]
  meta: MallWeatherMeta
}

export function parseMallWeatherRealtimePage(payload: unknown): MallWeatherRealtimePage | null {
  const data = envelopeData(payload)
  if (!data || !isRecord(data.meta) || !Array.isArray(data.items)) return null
  const items: MallWeatherRealtime[] = []
  for (const item of data.items) {
    if (!isRecord(item)) return null
    items.push(mallWeatherRealtime(item))
  }
  return { items, meta: mallWeatherMeta(data.meta) }
}

export function mallWeatherOverviewHasHourlyTemperature(
  overview: Pick<MallWeatherOverview, 'hourly'>,
) {
  return overview.hourly.some((item) =>
    typeof item.temperatureC === 'number' && Number.isFinite(item.temperatureC))
}

export type MallWeatherOverviewReadiness = 'ready' | 'waiting-empty' | 'waiting-hourly-temperature'

export function mallWeatherOverviewHasBusinessData(overview: MallWeatherOverview) {
  return overview.realtime !== null || overview.minutely.length > 0 || overview.hourly.length > 0 ||
    overview.alerts.length > 0
}

export function mallWeatherOverviewReadiness(
  overview: MallWeatherOverview,
): MallWeatherOverviewReadiness {
  if (mallWeatherOverviewHasHourlyTemperature(overview)) return 'ready'
  if (overview.meta.freshnessStatus.trim().toUpperCase() === 'UNAVAILABLE' &&
    overview.realtime === null && overview.minutely.length === 0 && overview.hourly.length === 0 &&
    overview.alerts.length === 0) return 'waiting-empty'
  return 'waiting-hourly-temperature'
}

function mallWeatherPagination(value: unknown): MallWeatherPageResult<never>['pagination'] | null {
  if (!isRecord(value) || !Number.isSafeInteger(value.pageSize) || Number(value.pageSize) < 1 || Number(value.pageSize) > 200 ||
    (value.nextCursor !== undefined && typeof value.nextCursor !== 'string')) return null
  return { pageSize: Number(value.pageSize), nextCursor: typeof value.nextCursor === 'string' ? value.nextCursor : '' }
}

function mallWeatherPageData(payload: unknown): { data: JsonRecord; pagination: MallWeatherPageResult<never>['pagination'] } | null {
  const data = envelopeData(payload)
  if (!data || !Array.isArray(data.items) || !isRecord(data.meta)) return null
  const pagination = mallWeatherPagination(data.pagination)
  return pagination ? { data, pagination } : null
}

export function parseMallWeatherHourlyPage(payload: unknown): MallWeatherPageResult<MallWeatherHourly> | null {
  const page = mallWeatherPageData(payload)
  if (!page) return null
  const meta = strictMallWeatherMeta(page.data.meta)
  if (!meta) return null
  const items: MallWeatherHourly[] = []
  for (const item of page.data.items as unknown[]) {
    if (!isRecord(item) || !isRFC3339(item.forecastTimeUtc) || !isRFC3339(item.forecastTimeLocal) || !isRFC3339(item.issuedAtUtc) ||
      !isRFC3339(item.issuedAtLocal) || !isRFC3339(item.fetchedAtUtc) || !isRFC3339(item.fetchedAtLocal) ||
      typeof item.qualityStatus !== 'string' || !item.qualityStatus.trim() || !hasOnlyOptionalFiniteNumbers(item, [
        'temperatureC', 'apparentTemperatureC', 'pressurePa', 'humidityPct', 'precipitationMmH',
        'precipitationProbabilityPct', 'windSpeedKph', 'windDirectionDeg', 'cloudrateRatio', 'dswrfWM2',
        'visibilityKm', 'pm25UgM3', 'aqiChn', 'aqiUsa',
      ]) || !hasOnlyOptionalStrings(item, ['skycon', 'hourlyDescription', 'forecastKeypoint'])) return null
    const warnings = strictWarningValues(item.qualityWarnings)
    if (!warnings) return null
    items.push(mallWeatherHourly(item, warnings))
  }
  return { items, meta, pagination: page.pagination }
}

export function parseMallWeatherMinutelyPage(payload: unknown): MallWeatherPageResult<MallWeatherMinutely> | null {
  const page = mallWeatherPageData(payload)
  if (!page) return null
  const meta = strictMallWeatherMeta(page.data.meta)
  if (!meta) return null
  const items: MallWeatherMinutely[] = []
  for (const item of page.data.items as unknown[]) {
    if (!isRecord(item) || !isRFC3339(item.forecastMinuteUtc) || !isRFC3339(item.forecastMinuteLocal) ||
      !isRFC3339(item.issuedAtUtc) || !isRFC3339(item.issuedAtLocal) || !isRFC3339(item.fetchedAtUtc) ||
      !isRFC3339(item.fetchedAtLocal) || !Number.isSafeInteger(item.minuteOffset) || Number(item.minuteOffset) < 0 ||
      typeof item.qualityStatus !== 'string' || !item.qualityStatus.trim() ||
      !hasOnlyOptionalFiniteNumbers(item, ['precipitationMmH', 'probabilityPct']) ||
      !hasOnlyOptionalStrings(item, ['datasource', 'description', 'forecastKeypoint'])) return null
    const warnings = strictWarningValues(item.qualityWarnings)
    if (!warnings) return null
    items.push(mallWeatherMinutely(item, warnings))
  }
  return { items, meta, pagination: page.pagination }
}

export function parseMallWeatherDailyPage(payload: unknown): MallWeatherPageResult<MallWeatherDaily> | null {
  const page = mallWeatherPageData(payload)
  if (!page) return null
  const meta = strictMallWeatherMeta(page.data.meta)
  if (!meta) return null
  const items: MallWeatherDaily[] = []
  for (const item of page.data.items as unknown[]) {
    if (!isRecord(item) || !isISODate(item.forecastDateLocal) || !isRFC3339(item.issuedAtUtc) || !isRFC3339(item.issuedAtLocal) ||
      !isRFC3339(item.fetchedAtUtc) || !isRFC3339(item.fetchedAtLocal) || typeof item.qualityStatus !== 'string' || !item.qualityStatus.trim() ||
      !hasOnlyOptionalFiniteNumbers(item, [
        'temperatureMaxC', 'temperatureMinC', 'temperatureAvgC', 'dayTemperatureMaxC', 'dayTemperatureMinC', 'dayTemperatureAvgC',
        'nightTemperatureMaxC', 'nightTemperatureMinC', 'nightTemperatureAvgC', 'precipitationMaxMmH', 'precipitationMinMmH',
        'precipitationAvgMmH', 'precipitationProbabilityPct', 'dayPrecipitationMaxMmH', 'dayPrecipitationMinMmH',
        'dayPrecipitationAvgMmH', 'dayPrecipitationProbabilityPct', 'nightPrecipitationMaxMmH', 'nightPrecipitationMinMmH',
        'nightPrecipitationAvgMmH', 'nightPrecipitationProbabilityPct', 'windMaxSpeedKph', 'windMaxDirectionDeg',
        'windMinSpeedKph', 'windMinDirectionDeg', 'windAvgSpeedKph', 'windAvgDirectionDeg', 'dayWindMaxSpeedKph',
        'dayWindMaxDirectionDeg', 'dayWindMinSpeedKph', 'dayWindMinDirectionDeg', 'dayWindAvgSpeedKph', 'dayWindAvgDirectionDeg',
        'nightWindMaxSpeedKph', 'nightWindMaxDirectionDeg', 'nightWindMinSpeedKph', 'nightWindMinDirectionDeg',
        'nightWindAvgSpeedKph', 'nightWindAvgDirectionDeg', 'humidityMaxPct', 'humidityMinPct', 'humidityAvgPct',
        'cloudrateMaxRatio', 'cloudrateMinRatio', 'cloudrateAvgRatio', 'pressureMaxPa', 'pressureMinPa', 'pressureAvgPa',
        'visibilityMaxKm', 'visibilityMinKm', 'visibilityAvgKm', 'dswrfMaxWM2', 'dswrfMinWM2', 'dswrfAvgWM2',
        'pm25MaxUgM3', 'pm25MinUgM3', 'pm25AvgUgM3', 'aqiMaxChn', 'aqiMinChn', 'aqiAvgChn', 'aqiMaxUsa', 'aqiMinUsa', 'aqiAvgUsa',
      ]) || !hasOnlyOptionalStrings(item, ['skycon', 'daySkycon', 'nightSkycon', 'sunriseLocalTime', 'sunsetLocalTime'])) return null
    const warnings = strictWarningValues(item.qualityWarnings)
    if (!warnings) return null
    items.push({
      forecastDateLocal: item.forecastDateLocal,
      issuedAtUtc: item.issuedAtUtc,
      issuedAtLocal: item.issuedAtLocal,
      fetchedAtUtc: item.fetchedAtUtc,
      fetchedAtLocal: item.fetchedAtLocal,
      temperatureMaxC: numberValue(item, 'temperatureMaxC'),
      temperatureMinC: numberValue(item, 'temperatureMinC'),
      temperatureAvgC: numberValue(item, 'temperatureAvgC'),
      dayTemperatureMaxC: numberValue(item, 'dayTemperatureMaxC'),
      dayTemperatureMinC: numberValue(item, 'dayTemperatureMinC'),
      dayTemperatureAvgC: numberValue(item, 'dayTemperatureAvgC'),
      nightTemperatureMaxC: numberValue(item, 'nightTemperatureMaxC'),
      nightTemperatureMinC: numberValue(item, 'nightTemperatureMinC'),
      nightTemperatureAvgC: numberValue(item, 'nightTemperatureAvgC'),
      precipitationMaxMmH: numberValue(item, 'precipitationMaxMmH'),
      precipitationMinMmH: numberValue(item, 'precipitationMinMmH'),
      precipitationAvgMmH: numberValue(item, 'precipitationAvgMmH'),
      precipitationProbabilityPct: numberValue(item, 'precipitationProbabilityPct'),
      dayPrecipitationMaxMmH: numberValue(item, 'dayPrecipitationMaxMmH'),
      dayPrecipitationMinMmH: numberValue(item, 'dayPrecipitationMinMmH'),
      dayPrecipitationAvgMmH: numberValue(item, 'dayPrecipitationAvgMmH'),
      dayPrecipitationProbabilityPct: numberValue(item, 'dayPrecipitationProbabilityPct'),
      nightPrecipitationMaxMmH: numberValue(item, 'nightPrecipitationMaxMmH'),
      nightPrecipitationMinMmH: numberValue(item, 'nightPrecipitationMinMmH'),
      nightPrecipitationAvgMmH: numberValue(item, 'nightPrecipitationAvgMmH'),
      nightPrecipitationProbabilityPct: numberValue(item, 'nightPrecipitationProbabilityPct'),
      windMaxSpeedKph: numberValue(item, 'windMaxSpeedKph'),
      windMaxDirectionDeg: numberValue(item, 'windMaxDirectionDeg'),
      windMinSpeedKph: numberValue(item, 'windMinSpeedKph'),
      windMinDirectionDeg: numberValue(item, 'windMinDirectionDeg'),
      windAvgSpeedKph: numberValue(item, 'windAvgSpeedKph'),
      windAvgDirectionDeg: numberValue(item, 'windAvgDirectionDeg'),
      dayWindMaxSpeedKph: numberValue(item, 'dayWindMaxSpeedKph'),
      dayWindMaxDirectionDeg: numberValue(item, 'dayWindMaxDirectionDeg'),
      dayWindMinSpeedKph: numberValue(item, 'dayWindMinSpeedKph'),
      dayWindMinDirectionDeg: numberValue(item, 'dayWindMinDirectionDeg'),
      dayWindAvgSpeedKph: numberValue(item, 'dayWindAvgSpeedKph'),
      dayWindAvgDirectionDeg: numberValue(item, 'dayWindAvgDirectionDeg'),
      nightWindMaxSpeedKph: numberValue(item, 'nightWindMaxSpeedKph'),
      nightWindMaxDirectionDeg: numberValue(item, 'nightWindMaxDirectionDeg'),
      nightWindMinSpeedKph: numberValue(item, 'nightWindMinSpeedKph'),
      nightWindMinDirectionDeg: numberValue(item, 'nightWindMinDirectionDeg'),
      nightWindAvgSpeedKph: numberValue(item, 'nightWindAvgSpeedKph'),
      nightWindAvgDirectionDeg: numberValue(item, 'nightWindAvgDirectionDeg'),
      humidityMaxPct: numberValue(item, 'humidityMaxPct'),
      humidityMinPct: numberValue(item, 'humidityMinPct'),
      humidityAvgPct: numberValue(item, 'humidityAvgPct'),
      cloudrateMaxRatio: numberValue(item, 'cloudrateMaxRatio'),
      cloudrateMinRatio: numberValue(item, 'cloudrateMinRatio'),
      cloudrateAvgRatio: numberValue(item, 'cloudrateAvgRatio'),
      pressureMaxPa: numberValue(item, 'pressureMaxPa'),
      pressureMinPa: numberValue(item, 'pressureMinPa'),
      pressureAvgPa: numberValue(item, 'pressureAvgPa'),
      visibilityMaxKm: numberValue(item, 'visibilityMaxKm'),
      visibilityMinKm: numberValue(item, 'visibilityMinKm'),
      visibilityAvgKm: numberValue(item, 'visibilityAvgKm'),
      dswrfMaxWM2: numberValue(item, 'dswrfMaxWM2'),
      dswrfMinWM2: numberValue(item, 'dswrfMinWM2'),
      dswrfAvgWM2: numberValue(item, 'dswrfAvgWM2'),
      pm25MaxUgM3: numberValue(item, 'pm25MaxUgM3'),
      pm25MinUgM3: numberValue(item, 'pm25MinUgM3'),
      pm25AvgUgM3: numberValue(item, 'pm25AvgUgM3'),
      aqiMaxChn: numberValue(item, 'aqiMaxChn'),
      aqiMinChn: numberValue(item, 'aqiMinChn'),
      aqiAvgChn: numberValue(item, 'aqiAvgChn'),
      aqiMaxUsa: numberValue(item, 'aqiMaxUsa'),
      aqiMinUsa: numberValue(item, 'aqiMinUsa'),
      aqiAvgUsa: numberValue(item, 'aqiAvgUsa'),
      skycon: textValue(item, 'skycon'),
      daySkycon: textValue(item, 'daySkycon'),
      nightSkycon: textValue(item, 'nightSkycon'),
      sunriseLocalTime: textValue(item, 'sunriseLocalTime'),
      sunsetLocalTime: textValue(item, 'sunsetLocalTime'),
      qualityStatus: item.qualityStatus,
      qualityWarnings: warnings,
    })
  }
  return { items, meta, pagination: page.pagination }
}

export function parseMallWeatherLifeIndexPage(payload: unknown): MallWeatherPageResult<MallWeatherLifeIndex> | null {
  const page = mallWeatherPageData(payload)
  if (!page) return null
  const meta = strictMallWeatherMeta(page.data.meta)
  if (!meta) return null
  const items: MallWeatherLifeIndex[] = []
  for (const item of page.data.items as unknown[]) {
    if (!isRecord(item) || item.sourceApi !== 'v26_daily' || !isISODate(item.forecastDateLocal) ||
      !Number.isSafeInteger(item.indexType) || Number(item.indexType) < 0 || typeof item.indexCode !== 'string' || !item.indexCode.trim() ||
      !isRFC3339(item.issuedAtUtc) || !isRFC3339(item.issuedAtLocal) || !isRFC3339(item.fetchedAtUtc) || !isRFC3339(item.fetchedAtLocal) ||
      typeof item.qualityStatus !== 'string' || !item.qualityStatus.trim() || typeof item.isUnknownType !== 'boolean') return null
    const level = numberValue(item, 'level')
    if (level !== undefined && !Number.isSafeInteger(level)) return null
    const warnings = strictWarningValues(item.qualityWarnings)
    if (!warnings) return null
    items.push({
      sourceApi: item.sourceApi,
      forecastDateLocal: item.forecastDateLocal,
      indexType: Number(item.indexType),
      indexCode: item.indexCode,
      indexName: textValue(item, 'indexName'),
      level,
      shortDescription: textValue(item, 'shortDescription'),
      detail: textValue(item, 'detail'),
      isUnknownType: item.isUnknownType,
      issuedAtUtc: item.issuedAtUtc,
      issuedAtLocal: item.issuedAtLocal,
      fetchedAtUtc: item.fetchedAtUtc,
      fetchedAtLocal: item.fetchedAtLocal,
      qualityStatus: item.qualityStatus,
      qualityWarnings: warnings,
    })
  }
  return { items, meta, pagination: page.pagination }
}

export function parseMallWeatherAlertPage(payload: unknown): MallWeatherPageResult<MallWeatherAlert> | null {
  const page = mallWeatherPageData(payload)
  if (!page) return null
  const meta = strictMallWeatherMeta(page.data.meta)
  if (!meta) return null
  const items: MallWeatherAlert[] = []
  for (const item of page.data.items as unknown[]) {
    if (!isRecord(item) || typeof item.alertId !== 'string' || !item.alertId.trim() ||
      typeof item.status !== 'string' || !item.status.trim() || typeof item.title !== 'string' || !item.title.trim() ||
      typeof item.qualityStatus !== 'string' || !item.qualityStatus.trim() || !hasOnlyOptionalFiniteNumbers(item, [
        'latitude', 'longitude',
      ]) || !hasOnlyOptionalStrings(item, [
        'description', 'code', 'alertTypeCode', 'alertLevelCode', 'alertTypeName', 'alertLevelName',
        'publishedAtLocal', 'source', 'province', 'city', 'county', 'location', 'regionId', 'adcode',
        'firstSeenAtLocal', 'lastSeenAtLocal', 'endedAtLocal',
      ])) return null
    const warnings = strictWarningValues(item.qualityWarnings)
    if (!warnings) return null
    items.push({ ...mallWeatherAlert(item), qualityWarnings: warnings })
  }
  return { items, meta, pagination: page.pagination }
}

export function parseMallWeatherRefreshResult(payload: unknown): MallWeatherRefreshResult | null {
  const data = envelopeData(payload)
  if (!data || !Number.isSafeInteger(data.jobId) || Number(data.jobId) <= 0 || !Number.isSafeInteger(data.mallId) || Number(data.mallId) <= 0 ||
    typeof data.force !== 'boolean' || typeof data.reason !== 'string' || !Number.isSafeInteger(data.requestedBy) || Number(data.requestedBy) <= 0 ||
    !isRFC3339(data.requestedAt) || typeof data.correlationId !== 'string' || !validMallWeatherCorrelationID(data.correlationId) ||
    !Array.isArray(data.kinds) || data.kinds.length !== 1) return null
  const kinds: MallWeatherRefreshResult['kinds'] = []
  const seenKinds = new Set<MallWeatherRefreshKind>()
  for (const item of data.kinds) {
    if (!isRecord(item) || item.kind !== 'V26_FULL' ||
      (item.status !== 'QUEUED' && item.status !== 'SKIPPED_FRESH')) return null
    if (seenKinds.has(item.kind)) return null
    seenKinds.add(item.kind)
    const outboxJobId = numberValue(item, 'outboxJobId')
    if (item.status === 'QUEUED' && (outboxJobId === undefined || !Number.isSafeInteger(outboxJobId) || outboxJobId <= 0)) return null
    if (item.status === 'SKIPPED_FRESH' && outboxJobId !== undefined) return null
    kinds.push({ kind: item.kind, status: item.status, ...(outboxJobId === undefined ? {} : { outboxJobId }) })
  }
  return {
    jobId: Number(data.jobId),
    mallId: Number(data.mallId),
    force: data.force,
    reason: data.reason,
    requestedBy: Number(data.requestedBy),
    requestedAt: data.requestedAt,
    correlationId: data.correlationId,
    kinds,
  }
}

export function parseMallWeatherFetchRuns(payload: unknown): MallWeatherFetchRunsResult | null {
  const data = envelopeData(payload)
  if (!data || !Array.isArray(data.items) || !isRecord(data.meta) || typeof data.meta.timeZone !== 'string' || !data.meta.timeZone.trim()) return null
  const pagination = mallWeatherPagination(data.pagination)
  if (!pagination) return null
  const items: MallWeatherFetchRun[] = []
  for (const item of data.items) {
    if (!isRecord(item) || typeof item.runUuid !== 'string' || !item.runUuid.trim() ||
      typeof item.correlationId !== 'string' || !validMallWeatherCorrelationID(item.correlationId) || typeof item.provider !== 'string' ||
      typeof item.endpointKind !== 'string' || !item.endpointKind.trim() || typeof item.taskKind !== 'string' || !item.taskKind.trim() ||
      typeof item.status !== 'string' || !item.status.trim() || !nonNegativeSafeInteger(item.requestedHourlySteps) ||
      !nonNegativeSafeInteger(item.requestedDailySteps) || !nonNegativeSafeInteger(item.attemptCount) ||
      !nonNegativeSafeInteger(item.durationMs) || !isRecord(item.rowCounts) || !Array.isArray(item.parseWarnings) ||
      !isRFC3339(item.createdAtUtc) || !isRFC3339(item.createdAtLocal) || !isRFC3339(item.updatedAtUtc) || !isRFC3339(item.updatedAtLocal) ||
      !optionalRFC3339(item.finishedAtUtc) || !optionalRFC3339(item.finishedAtLocal) ||
      !hasOnlyOptionalStrings(item, ['errorCode', 'errorMessageSafe'])) return null
    const rowCounts: Record<string, number> = {}
    const entries = Object.entries(item.rowCounts)
    if (entries.length > 64) return null
    for (const [key, value] of entries) {
      if (!key.trim() || key.length > 128 || !nonNegativeSafeInteger(value)) return null
      rowCounts[key] = value
    }
    const parseWarnings = strictWarningValues(item.parseWarnings)
    if (!parseWarnings) return null
    items.push({
      runUuid: item.runUuid,
      correlationId: item.correlationId,
      provider: item.provider,
      endpointKind: item.endpointKind,
      taskKind: item.taskKind.toUpperCase(),
      requestedHourlySteps: item.requestedHourlySteps,
      requestedDailySteps: item.requestedDailySteps,
      attemptCount: item.attemptCount,
      status: item.status.toUpperCase(),
      durationMs: item.durationMs,
      rowCounts,
      parseWarnings,
      errorCode: textValue(item, 'errorCode'),
      errorMessageSafe: textValue(item, 'errorMessageSafe'),
      createdAtUtc: item.createdAtUtc,
      createdAtLocal: item.createdAtLocal,
      updatedAtUtc: item.updatedAtUtc,
      updatedAtLocal: item.updatedAtLocal,
      ...(typeof item.finishedAtUtc === 'string' ? { finishedAtUtc: item.finishedAtUtc } : {}),
      ...(typeof item.finishedAtLocal === 'string' ? { finishedAtLocal: item.finishedAtLocal } : {}),
    })
  }
  return { items, meta: { timeZone: data.meta.timeZone }, pagination }
}

export function mallWeatherFetchRunTerminal(status: string) {
  return ['SUCCESS', 'PARTIAL_SUCCESS', 'FAILED', 'CANCELLED'].includes(status.trim().toUpperCase())
}

export async function pollMallWeatherFetchRun(
  request: MallWeatherFetchRunRequester,
  mallID: number,
  requestedAt: string,
  taskKind: MallWeatherFetchRunTaskKind,
  correlationID: string,
  options: MallWeatherFetchRunPollOptions = {},
): Promise<MallWeatherFetchRunPollResult> {
  const requestedAtMS = Date.parse(requestedAt)
  const maxAttempts = options.maxAttempts ?? 30
  const intervalMs = options.intervalMs ?? 2_000
  const now = options.now ?? (() => new Date())
  const wait = options.wait ?? waitForMallWeatherPoll
  if (!Number.isFinite(requestedAtMS) || !Number.isSafeInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 120 ||
    !Number.isFinite(intervalMs) || intervalMs < 0 || intervalMs > 60_000) {
    throw new Error('invalid weather fetch run poll')
  }
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (options.signal?.aborted) return { kind: 'cancelled' }
    const currentTime = now()
    const endMS = Math.max(currentTime.getTime() + 5 * 60 * 1000, requestedAtMS + 60 * 1000)
    const startMS = Math.max(requestedAtMS, endMS - 24 * 60 * 60 * 1000)
    const response = await request(mallWeatherFetchRunsPath(mallID, new Date(startMS), new Date(endMS), taskKind, correlationID), {
      method: 'GET',
      showResult: false,
      silentLoading: true,
      signal: options.signal,
    })
    if (options.signal?.aborted) return { kind: 'cancelled' }
    if (!response.ok) {
      const retryable = response.status === 0 || response.status === 408 || response.status === 425 ||
        response.status === 429 || response.status >= 500
      if (!retryable) return { kind: 'query_error', status: response.status }
      if (attempt + 1 >= maxAttempts) return { kind: 'query_error', status: response.status }
      await wait(intervalMs, options.signal)
      continue
    }
    const parsed = parseMallWeatherFetchRuns(response.data)
    if (!parsed) return { kind: 'query_error', status: response.status }
    const run = parsed.items
      .filter((item) => item.taskKind === taskKind && item.correlationId === correlationID && Date.parse(item.createdAtUtc) >= requestedAtMS)
      .sort((left, right) => Date.parse(right.createdAtUtc) - Date.parse(left.createdAtUtc))[0]
    if (run && mallWeatherFetchRunTerminal(run.status)) return { kind: 'terminal', run }
    if (attempt + 1 < maxAttempts) await wait(intervalMs, options.signal)
  }
  return options.signal?.aborted ? { kind: 'cancelled' } : { kind: 'timed_out' }
}

function waitForMallWeatherPoll(intervalMs: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (signal?.aborted || intervalMs === 0) {
      resolve()
      return
    }
    const finish = () => {
      globalThis.clearTimeout(timer)
      signal?.removeEventListener('abort', finish)
      resolve()
    }
    const timer = globalThis.setTimeout(finish, intervalMs)
    signal?.addEventListener('abort', finish, { once: true })
  })
}

function sheetPushPollDelay(intervalMs: number, failures: number, isPageVisible: (() => boolean) | undefined) {
  const backoff = 2 ** Math.min(failures, 3)
  const hiddenFactor = isPageVisible?.() === false ? 5 : 1
  return Math.min(60_000, intervalMs * backoff * hiddenFactor)
}

export function mallWeatherRefreshDisposition(
  response: { ok: boolean; status: number; data: unknown },
  actorID: string,
  mallID: number,
  request: MallWeatherRefreshRequest,
): MallWeatherRefreshDisposition {
  if (!response.ok) {
    const uncertain = response.status === 0 || response.status === 408 || response.status === 409 || response.status >= 500 ||
      (response.status >= 200 && response.status < 300)
    return { kind: uncertain ? 'uncertain' : 'rejected' }
  }
  if (response.status !== 202) return { kind: 'uncertain' }
  const result = parseMallWeatherRefreshResult(response.data)
  if (!result || result.mallId !== mallID || String(result.requestedBy) !== actorID || result.force !== request.force ||
    result.reason !== request.reason || result.kinds.length !== request.kinds.length) return { kind: 'uncertain' }
  const returnedKinds = new Set(result.kinds.map((item) => item.kind))
  if (request.kinds.some((kind) => !returnedKinds.has(kind))) return { kind: 'uncertain' }
  return { kind: 'accepted', result }
}

export function mallWeatherRefreshResultMessage(result: MallWeatherRefreshResult) {
  const queued = result.kinds.filter((item) => item.status === 'QUEUED').length
  const skipped = result.kinds.filter((item) => item.status === 'SKIPPED_FRESH').length
  if (queued > 0 && skipped > 0) return `${queued} 个采集任务已入队，${skipped} 项数据仍新鲜并已跳过。`
  if (queued > 0) return `${queued} 个采集任务已进入异步队列；稍后重新加载天气即可查看结果。`
  return `${skipped} 项数据仍新鲜，本次未重复入队。`
}

export function mallWeatherOverviewPath(mallID: number, timeZone = '') {
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  const query = new URLSearchParams()
  if (timeZone.trim()) query.set('timeZone', timeZone)
  const suffix = query.toString()
  return `/v1/malls/${mallID}/weather/overview${suffix ? `?${suffix}` : ''}`
}

// mallWeatherRealtimePath is used only as a bounded management-side fallback
// when the overview response has no realtime record. Realtime requires a
// concrete RFC3339 range, unlike the overview endpoint.
export function mallWeatherRealtimePath(mallID: number, start: Date, end: Date, timeZone: string) {
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || start >= end || end.getTime() - start.getTime() > 31 * 24 * 60 * 60 * 1000) {
    throw new Error('invalid weather range')
  }
  if (!timeZone.trim()) throw new Error('invalid weather time zone')
  const query = new URLSearchParams({
    start: start.toISOString(),
    end: end.toISOString(),
    timeZone: timeZone.trim(),
    latest: 'true',
    pageSize: '200',
  })
  return `/v1/malls/${mallID}/weather/realtime?${query.toString()}`
}

export function mallWeatherSeriesPath(mallID: number, series: MallWeatherSeries, start: Date, end: Date, cursor = '', timeZone = 'Asia/Shanghai', asOf?: Date) {
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  if (!['minutely', 'hourly', 'daily', 'alerts', 'life-indices'].includes(series)) throw new Error('invalid weather series')
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || start >= end || end.getTime() - start.getTime() > 31 * 24 * 60 * 60 * 1000) {
    throw new Error('invalid weather range')
  }
  if (asOf !== undefined && !Number.isFinite(asOf.getTime())) throw new Error('invalid weather snapshot time')
  if (!timeZone.trim()) throw new Error('invalid weather time zone')
  const query = new URLSearchParams({
    start: start.toISOString(),
    end: end.toISOString(),
    timeZone,
    latest: 'true',
    pageSize: '200',
  })
  if (asOf !== undefined) query.set('asOf', asOf.toISOString())
  if (cursor) query.set('cursor', cursor)
  return `/v1/malls/${mallID}/weather/${series}?${query.toString()}`
}

export function mallWeatherForecastQueryWindows(now = new Date(), timeZone = 'Asia/Shanghai'): MallWeatherForecastWindows {
  if (!Number.isFinite(now.getTime())) throw new Error('invalid weather query time')
  const minuteMilliseconds = 60 * 1000
  const hourMilliseconds = 60 * 60 * 1000
  const minutelyStart = new Date(Math.floor(now.getTime() / minuteMilliseconds) * minuteMilliseconds)
  const hourlyStart = new Date((Math.floor(now.getTime() / hourMilliseconds) + 1) * hourMilliseconds)
  const localDate = datePartsInTimeZone(now, timeZone)
  const dailyStart = localMidnight(localDate.year, localDate.month, localDate.day, timeZone)
  const normalizedEndDate = new Date(Date.UTC(
    localDate.year,
    localDate.month - 1,
    localDate.day + mallWeatherDailyForecastDays,
  ))
  const dailyEnd = localMidnight(normalizedEndDate.getUTCFullYear(), normalizedEndDate.getUTCMonth() + 1, normalizedEndDate.getUTCDate(), timeZone)
  return {
    minutely: {
      start: minutelyStart,
      end: new Date(minutelyStart.getTime() + mallWeatherMinutelyForecastMinutes * minuteMilliseconds),
    },
    hourly: {
      start: hourlyStart,
      end: new Date(hourlyStart.getTime() + mallWeatherHourlyForecastHours * hourMilliseconds),
    },
    daily: { start: dailyStart, end: dailyEnd },
  }
}

export async function loadAllMallWeatherPages<T>(
  request: MallWeatherPageRequester,
  mallID: number,
  series: MallWeatherSeries,
  window: MallWeatherQueryWindow,
  timeZone: string,
  asOf: Date | undefined,
  parser: MallWeatherPageParser<T>,
): Promise<{ items: T[]; meta: MallWeatherMeta | null }> {
  const items: T[] = []
  const fixedWindow = { start: new Date(window.start.getTime()), end: new Date(window.end.getTime()) }
  const fixedAsOf = asOf === undefined ? undefined : new Date(asOf.getTime())
  let cursor = ''
  let meta: MallWeatherMeta | null = null
  const seenCursors = new Set<string>()
  const seenLogicalKeys = new Set<string>()
  for (let pageNumber = 0; pageNumber < 10; pageNumber++) {
    const response = await request(mallWeatherSeriesPath(mallID, series, fixedWindow.start, fixedWindow.end, cursor, timeZone, fixedAsOf))
    if (!response.ok) throw new Error(mallWeatherQueryError(response.status))
    const page = parser(response.data)
    if (!page) throw new Error('响应格式不正确，请联系管理员')
    for (const item of page.items) {
      const logicalKey = mallWeatherLogicalKey(series, item)
      if (!logicalKey || seenLogicalKeys.has(logicalKey)) throw new Error('分页数据重复或缺少业务键，请联系管理员')
      seenLogicalKeys.add(logicalKey)
    }
    items.push(...page.items)
    meta = page.meta
    const nextCursor = page.pagination.nextCursor
    if (!nextCursor) {
      if (series === 'hourly') validateAvailableMallWeatherHourlySeries(items, fixedWindow)
      return { items, meta }
    }
    if (seenCursors.has(nextCursor)) throw new Error('分页游标重复，请联系管理员')
    seenCursors.add(nextCursor)
    cursor = nextCursor
  }
  throw new Error('分页数量超过安全上限，请联系管理员')
}

function validateAvailableMallWeatherHourlySeries(items: unknown[], window: MallWeatherQueryWindow) {
  const hourMilliseconds = 60 * 60 * 1000
  const startMilliseconds = window.start.getTime()
  const durationMilliseconds = window.end.getTime() - startMilliseconds
  const expectedCount = durationMilliseconds / hourMilliseconds
  if (!Number.isSafeInteger(expectedCount) || expectedCount !== mallWeatherHourlyForecastHours) {
    throw new Error('逐小时预报查询窗口无效，请联系管理员')
  }
  if (items.length > expectedCount) {
    throw new Error(`逐小时预报数量超过窗口：最多 ${expectedCount} 条，实际 ${items.length} 条`)
  }
  for (let index = 0; index < items.length; index++) {
    const item = items[index]
    if (!isRecord(item)) throw new Error('逐小时预报响应格式不正确，请联系管理员')
    const utcMilliseconds = Date.parse(String(item.forecastTimeUtc))
    const localMilliseconds = Date.parse(String(item.forecastTimeLocal))
    const expectedMilliseconds = startMilliseconds + index * hourMilliseconds
    if (utcMilliseconds !== expectedMilliseconds) {
      throw new Error(`逐小时预报时间不连续：第 ${index + 1} 条不在预期小时`)
    }
    if (localMilliseconds !== utcMilliseconds) {
      throw new Error(`逐小时预报本地时间与 UTC 时间不一致：第 ${index + 1} 条`)
    }
  }
}

export function loadMallWeatherForecastDatasets(
  request: MallWeatherPageRequester,
  mallID: number,
  timeZone: string,
  requestedAt = new Date(),
) {
  const windows = mallWeatherForecastQueryWindows(requestedAt, timeZone)
  return {
    minutely: loadAllMallWeatherPages(
      request, mallID, 'minutely', windows.minutely, timeZone, requestedAt, parseMallWeatherMinutelyPage,
    ),
    hourly: loadAllMallWeatherPages(
      request, mallID, 'hourly', windows.hourly, timeZone, requestedAt, parseMallWeatherHourlyPage,
    ),
    daily: loadAllMallWeatherPages(
      request, mallID, 'daily', windows.daily, timeZone, requestedAt, parseMallWeatherDailyPage,
    ),
    life: loadAllMallWeatherPages(
      request, mallID, 'life-indices', windows.daily, timeZone, requestedAt, parseMallWeatherLifeIndexPage,
    ),
  }
}

export function loadAllMallWeatherAlerts(
  request: MallWeatherPageRequester,
  mallID: number,
  timeZone: string,
  requestedAt = new Date(),
) {
  if (!Number.isFinite(requestedAt.getTime())) throw new Error('invalid weather alert query time')
  const dayMilliseconds = 24 * 60 * 60 * 1_000
  return loadAllMallWeatherPages(
    request,
    mallID,
    'alerts',
    {
      start: new Date(requestedAt.getTime() - 30 * dayMilliseconds),
      end: new Date(requestedAt.getTime() + dayMilliseconds),
    },
    timeZone,
    requestedAt,
    parseMallWeatherAlertPage,
  )
}

function mallWeatherLogicalKey(series: MallWeatherSeries, value: unknown) {
  if (!isRecord(value)) return ''
  if (series === 'minutely') return typeof value.forecastMinuteUtc === 'string' ? value.forecastMinuteUtc : ''
  if (series === 'hourly') return typeof value.forecastTimeUtc === 'string' ? value.forecastTimeUtc : ''
  if (series === 'daily') return typeof value.forecastDateLocal === 'string' ? value.forecastDateLocal : ''
  if (series === 'alerts') return typeof value.alertId === 'string' ? value.alertId : ''
  return typeof value.forecastDateLocal === 'string' && typeof value.sourceApi === 'string' && Number.isSafeInteger(value.indexType)
    ? `${value.forecastDateLocal}\u0000${value.sourceApi}\u0000${value.indexType}`
    : ''
}

function mallWeatherQueryError(status: number) {
  if (status === 0) return '无法连接服务，请检查网络后重试'
  if (status === 403) return '当前账号缺少 weather.read 权限'
  if (status === 404) return '商场或天气数据不存在'
  if (status === 422) return '查询窗口、时区或商场坐标无效'
  return `完整天气查询失败（HTTP ${status}）`
}

function datePartsInTimeZone(value: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(value)
  const part = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((item) => item.type === type)?.value)
  const year = part('year')
  const month = part('month')
  const day = part('day')
  if (!Number.isSafeInteger(year) || !Number.isSafeInteger(month) || !Number.isSafeInteger(day)) throw new Error('invalid weather time zone')
  return { year, month, day }
}

function localMidnight(year: number, month: number, day: number, timeZone: string) {
  const targetTimestamp = Date.UTC(year, month - 1, day)
  const normalized = new Date(targetTimestamp)
  if (normalized.getUTCFullYear() !== year || normalized.getUTCMonth() + 1 !== month || normalized.getUTCDate() !== day) {
    throw new Error('invalid local weather date')
  }
  const searchRadius = 36 * 60 * 60 * 1000
  let before = targetTimestamp - searchRadius
  let atOrAfter = targetTimestamp + searchRadius
  while (atOrAfter - before > 1) {
    const candidate = before + Math.floor((atOrAfter - before) / 2)
    const local = datePartsInTimeZone(new Date(candidate), timeZone)
    const representedDate = Date.UTC(local.year, local.month - 1, local.day)
    if (representedDate >= targetTimestamp) atOrAfter = candidate
    else before = candidate
  }
  const result = new Date(atOrAfter)
  const local = datePartsInTimeZone(result, timeZone)
  if (Date.UTC(local.year, local.month - 1, local.day) !== targetTimestamp) {
    throw new Error('local weather date does not exist')
  }
  return result
}

export function mallWeatherRefreshPath(mallID: number) {
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  return `/v1/malls/${mallID}/weather-refresh`
}

export function mallWeatherFetchRunsPath(
  mallID: number,
  start: Date,
  end: Date,
  taskKind: MallWeatherFetchRunTaskKind,
  correlationID: string,
) {
  positiveMallID(mallID)
  const duration = end.getTime() - start.getTime()
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || duration <= 0 || duration > 24 * 60 * 60 * 1000) {
    throw new Error('invalid weather fetch run range')
  }
  if (taskKind !== 'MANUAL' && taskKind !== 'FULL') throw new Error('invalid weather fetch run task kind')
  if (!validMallWeatherCorrelationID(correlationID)) throw new Error('invalid weather fetch run correlation id')
  const query = new URLSearchParams({
    start: start.toISOString(),
    end: end.toISOString(),
    taskKind,
    endpointKind: 'v26_weather',
    correlationId: correlationID,
    pageSize: '10',
  })
  return `/v1/malls/${mallID}/weather/fetch-runs?${query.toString()}`
}

export function mallWeatherRefreshRequest(kinds: MallWeatherRefreshKind[], reason: string): MallWeatherRefreshRequest {
	const normalizedReason = reason.trim()
	if (kinds.length !== 1 || kinds[0] !== 'V26_FULL') throw new Error('invalid refresh kinds')
  if (!normalizedReason || Array.from(normalizedReason).length > 500 || /[\0\r\n]/.test(normalizedReason)) {
    throw new Error('invalid refresh reason')
  }
  return { kinds: ['V26_FULL'], force: false, reason: normalizedReason }
}

export function mallWeatherRefreshKey(seed?: string) {
  return mallWeatherOperationKey('weather-refresh', seed, 'invalid refresh key')
}

export function loadMallWeatherPendingRefresh(actorID: string, mallID: number, storage: RefreshStorage): MallWeatherPendingRefresh | null {
  const storageKey = mallWeatherPendingRefreshStorageKey(actorID, mallID)
  const raw = storage.getItem(storageKey)
  if (!raw) return null
  try {
    const snapshot: unknown = JSON.parse(raw)
    if (!isRecord(snapshot) || typeof snapshot.key !== 'string' || !isRecord(snapshot.body) || snapshot.body.force !== false ||
      !Array.isArray(snapshot.body.kinds) || snapshot.body.kinds.length !== 1 || snapshot.body.kinds[0] !== 'V26_FULL' ||
      typeof snapshot.body.reason !== 'string' || !validMallWeatherRefreshKey(snapshot.key)) return null
    const body = mallWeatherRefreshRequest(snapshot.body.kinds as MallWeatherRefreshKind[], snapshot.body.reason)
    return { key: snapshot.key, body }
  } catch {
    return null
  }
}

export function saveMallWeatherPendingRefresh(actorID: string, mallID: number, pending: MallWeatherPendingRefresh, storage: RefreshStorage) {
  if (!validMallWeatherRefreshKey(pending.key)) throw new Error('invalid refresh key')
  const body = mallWeatherRefreshRequest(pending.body.kinds, pending.body.reason)
  storage.setItem(mallWeatherPendingRefreshStorageKey(actorID, mallID), JSON.stringify({ key: pending.key, body }))
}

export function clearMallWeatherPendingRefresh(actorID: string, mallID: number, storage: RefreshStorage) {
  storage.removeItem(mallWeatherPendingRefreshStorageKey(actorID, mallID))
}

function mallWeatherPendingRefreshStorageKey(actorID: string, mallID: number) {
  const numericActorID = Number(actorID)
  if (!/^[1-9]\d*$/.test(actorID) || !Number.isSafeInteger(numericActorID)) throw new Error('invalid actor id')
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  return `mall-weather-pending-refresh:${actorID}:${mallID}`
}

function mallWeatherPendingCreateStorageKey(actorID: string) {
  const numericActorID = Number(actorID)
  if (!/^[1-9]\d*$/.test(actorID) || !Number.isSafeInteger(numericActorID)) throw new Error('invalid actor id')
  return `mall-weather-pending-create:${actorID}`
}

function validMallWeatherCreateKey(key: string) {
  return key.startsWith('mall-create:') && /^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$/.test(key)
}

function mallWeatherPendingSheetPushStorageKey(actorID: string, mallID: number) {
  const numericActorID = Number(actorID)
  if (!/^[1-9]\d*$/.test(actorID) || !Number.isSafeInteger(numericActorID)) throw new Error('invalid actor id')
  if (!Number.isSafeInteger(mallID) || mallID <= 0) throw new Error('invalid mall id')
  return `mall-weather-pending-sheet-push:${actorID}:${mallID}`
}

function validMallWeatherSheetPushKey(key: string) {
  return key.startsWith('weather-sheet-push:') && /^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$/.test(key)
}

function validMallWeatherRefreshKey(key: string) {
  return /^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$/.test(key)
}

function validMallWeatherCorrelationID(value: string) {
  return /^[A-Za-z0-9][A-Za-z0-9:_-]{7,254}$/.test(value)
}

export function mallWeatherFreshnessLabel(status: string) {
  const labels: Record<string, string> = {
    FRESH: '数据新鲜',
    WARNING: '即将过期',
    STALE: '数据已过期',
    CRITICAL: '数据严重过期',
    UNAVAILABLE: '暂无数据',
  }
  return labels[status.toUpperCase()] ?? (status || '未知状态')
}

export function mallWeatherSkyconLabel(skycon: string | undefined) {
  if (!skycon) return '暂无天气现象'
  const labels: Record<string, string> = {
    CLEAR_DAY: '晴（白天）',
    CLEAR_NIGHT: '晴（夜间）',
    PARTLY_CLOUDY_DAY: '多云（白天）',
    PARTLY_CLOUDY_NIGHT: '多云（夜间）',
    CLOUDY: '阴',
    LIGHT_HAZE: '轻度雾霾',
    MODERATE_HAZE: '中度雾霾',
    HEAVY_HAZE: '重度雾霾',
    LIGHT_RAIN: '小雨',
    MODERATE_RAIN: '中雨',
    HEAVY_RAIN: '大雨',
    STORM_RAIN: '暴雨',
    FOG: '雾',
  }
  return labels[skycon] ?? skycon
}

export function mallWeatherMetric(value: number | undefined, unit: string, fractionDigits = 1) {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  return `${value.toFixed(fractionDigits)}${unit}`
}

export type MallWeatherChartPoint = {
  index: number
  x: number
  y: number
}

export type MallWeatherChartScale = {
  minimum: number
  maximum: number
  ticks: number[]
}

export function mallWeatherChartTimeDomain(
  series: ReadonlyArray<ReadonlyArray<{ time: string }>>,
) {
  return [...new Set(series.flatMap((items) => items.map((item) => item.time).filter(Boolean)))].sort()
}

export function mallWeatherChartValuesByTime(
  data: ReadonlyArray<{ time: string; value?: number }>,
  timeDomain: readonly string[],
) {
  const valuesByTime = new Map(data.map((point) => [point.time, point.value]))
  return timeDomain.map((time) => valuesByTime.get(time))
}

export function mallWeatherClampedChartIndex(index: number | null, itemCount: number) {
  if (index === null || !Number.isSafeInteger(itemCount) || itemCount <= 0) return null
  return Math.min(Math.max(0, index), itemCount - 1)
}

export function mallWeatherChartScale(
  values: Array<number | undefined>,
  options: { floorZero?: boolean; tickCount?: number } = {},
): MallWeatherChartScale | undefined {
  const finiteValues = values.filter((value): value is number => typeof value === 'number' && Number.isFinite(value))
  if (finiteValues.length === 0) return undefined

  const tickCount = Math.max(2, Math.min(8, options.tickCount ?? 5))
  const dataMinimum = Math.min(...finiteValues)
  const dataMaximum = Math.max(...finiteValues)
  const dataRange = dataMaximum - dataMinimum
  const padding = dataRange > 0
    ? dataRange * 0.1
    : Math.max(Math.abs(dataMinimum) * 0.05, 1)
  let lower = dataMinimum - padding
  const upper = dataMaximum + padding
  if (options.floorZero && dataMinimum >= 0) lower = 0

  const rawStep = Math.max((upper - lower) / (tickCount - 1), Number.EPSILON)
  const magnitude = 10 ** Math.floor(Math.log10(rawStep))
  const normalized = rawStep / magnitude
  const factor = [1, 2, 2.5, 5, 10].find((candidate) => candidate >= normalized) ?? 10
  const step = factor * magnitude
  let minimum = Math.floor(lower / step) * step
  let maximum = Math.ceil(upper / step) * step
  if (options.floorZero && dataMinimum >= 0) minimum = Math.max(0, minimum)
  if (minimum === maximum) maximum = minimum + step

  const precision = Math.max(0, -Math.floor(Math.log10(step)) + 2)
  const ticks: number[] = []
  for (let value = minimum; value <= maximum + step / 2; value += step) {
    ticks.push(Number(value.toFixed(precision)))
  }
  return { minimum, maximum, ticks }
}

export function mallWeatherChartPoints(
  values: Array<number | undefined>,
  width: number,
  height: number,
  scale?: Pick<MallWeatherChartScale, 'minimum' | 'maximum'>,
) {
  const chartScale = scale ?? mallWeatherChartScale(values)
  if (!chartScale || width <= 0 || height <= 0) return []
  const range = chartScale.maximum - chartScale.minimum || 1
  const denominator = Math.max(values.length - 1, 1)

  return values.flatMap((value, index): MallWeatherChartPoint[] => {
    if (typeof value !== 'number' || !Number.isFinite(value)) return []
    return [{
      index,
      x: index / denominator * width,
      y: height - (value - chartScale.minimum) / range * height,
    }]
  })
}

export function mallWeatherNearestChartPoint(points: MallWeatherChartPoint[], targetX: number) {
  if (points.length === 0 || !Number.isFinite(targetX)) return undefined
  return points.reduce((nearest, point) =>
    Math.abs(point.x - targetX) < Math.abs(nearest.x - targetX) ? point : nearest)
}

export function mallWeatherChartSegments(
  values: Array<number | undefined>,
  width: number,
  height: number,
  scale?: Pick<MallWeatherChartScale, 'minimum' | 'maximum'>,
) {
  const points = mallWeatherChartPoints(values, width, height, scale)
  if (points.length === 0) return []
  const pointsByIndex = new Map(points.map((point) => [point.index, point]))

  const segments: string[] = []
  let current: string[] = []
  values.forEach((_value, index) => {
    const point = pointsByIndex.get(index)
    if (!point) {
      if (current.length > 0) segments.push(current.join(' '))
      current = []
      return
    }
    current.push(`${point.x.toFixed(1)},${point.y.toFixed(1)}`)
  })
  if (current.length > 0) segments.push(current.join(' '))
  return segments
}
