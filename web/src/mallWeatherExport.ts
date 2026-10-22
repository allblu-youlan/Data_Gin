export const mallWeatherExportPollIntervalMilliseconds = 2_000
export const mallWeatherExportMaximumPollAttempts = 150
export const mallWeatherExportRequestTimeoutMilliseconds = 30_000
export const mallWeatherExportMaximumTransientPollRetries = 5
export const mallWeatherExportMaximumPollRetryDelayMilliseconds = 30_000

export type MallWeatherExportFixedFilters =
  | {
      mallIds: number[]
      start?: never
      end?: never
    }
  | {
      mallIds: number[]
      start: string
      end: string
    }

export type MallWeatherExportLegacyFilters = {
  mallIds: number[]
  start: string
  end: string
}

export type MallWeatherExportFixedCreateRequest = {
  filters: MallWeatherExportFixedFilters
  profileId?: never
  expectedProfileVersion?: never
}

export type MallWeatherExportLegacyCreateRequest = {
  profileId: number
  expectedProfileVersion: number
  filters: MallWeatherExportLegacyFilters
}

export type MallWeatherExportCreateRequest =
  | MallWeatherExportFixedCreateRequest
  | MallWeatherExportLegacyCreateRequest

export type MallWeatherExportPendingCreate = {
  key: string
  body: MallWeatherExportCreateRequest
}

export type MallWeatherExportSession = {
  pending: MallWeatherExportPendingCreate | null
  jobId: string
}

export type MallWeatherExportCreateResult = {
  jobId: string
  status: 'PENDING'
  profileId: number
  profileVersion: number
  estimatedRows: number
  createdBy: number
  createdAt: string
}

export type MallWeatherExportCreateDisposition =
  | { kind: 'accepted'; result: MallWeatherExportCreateResult }
  | { kind: 'uncertain' }

export type MallWeatherExportJobStatus = 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELLED' | 'EXPIRED'

export type MallWeatherExportJob = {
  jobId: string
  profileId: number
  profileVersion: number
  status: MallWeatherExportJobStatus
  totalRows: number
  processedRows: number
  currentSheet: string
  cancelRequested: boolean
  fileSizeBytes: number
  errorMessageSafe: string
  expiresAt?: string
}

export type MallWeatherExportDownloadReadiness = 'ready' | 'not-ready' | 'expired'
export type MallWeatherExportPollFailureAction = 'retry' | 'forget' | 'pause'

export const mallWeatherExportDownloadPreflightTimeoutMilliseconds = 65 * 1000
export const mallWeatherExportDownloadFrameLifetimeMilliseconds = 20 * 60 * 1000

export class MallWeatherExportRequestTimeoutError extends Error {
  constructor() {
    super('mall weather export request timed out')
    this.name = 'MallWeatherExportRequestTimeoutError'
  }
}

type JsonRecord = Record<string, unknown>
type ExportStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

const exportJobIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
const exportKeyPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$/
const exportJobStatuses = new Set<MallWeatherExportJobStatus>([
  'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED',
])
let exportDownloadFrameSequence = 0

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function hasOwn(value: object, key: string) {
  return Object.prototype.hasOwnProperty.call(value, key)
}

function envelopeData(payload: unknown): JsonRecord | null {
  if (!isRecord(payload) || payload.code !== 0 || !isRecord(payload.data)) return null
  return payload.data
}

function positiveInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0
}

function nonNegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0
}

function nonEmptyString(value: unknown, maximumLength = 1_000): value is string {
  return typeof value === 'string' && value.trim() === value && value.length > 0 && Array.from(value).length <= maximumLength
}

function isRFC3339(value: unknown): value is string {
  if (typeof value !== 'string' || value.length < 20 || value.length > 64) return false
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) && /(?:Z|[+-]\d{2}:\d{2})$/.test(value)
}

export function mallWeatherExportCreateRequest(
  mallID: number,
): MallWeatherExportCreateRequest {
  if (!positiveInteger(mallID)) {
    throw new Error('invalid mall weather export request')
  }
  return {
    filters: { mallIds: [mallID] },
  }
}

export function mallWeatherExportKey(seed?: string) {
  const suffix = (seed ?? globalThis.crypto?.randomUUID?.() ?? '').trim()
  const key = `weather-export:${suffix}`
  if (!exportKeyPattern.test(key)) throw new Error('invalid mall weather export key')
  return key
}

export function mallWeatherExportRequestMatches(
  request: MallWeatherExportCreateRequest,
  mallID: number,
) {
  return storedExportRequestIsValid(request, mallID)
}

export function mallWeatherExportCreateResultMatchesRequest(
  result: MallWeatherExportCreateResult,
  request: MallWeatherExportCreateRequest,
) {
  if (!requestHasLegacyProfile(request)) return true
  return result.profileId === request.profileId && result.profileVersion === request.expectedProfileVersion
}

export function mallWeatherExportCreateDisposition(
  response: { ok: boolean; status: number; data: unknown },
  request: MallWeatherExportCreateRequest,
): MallWeatherExportCreateDisposition {
  if (!response.ok || response.status !== 202) return { kind: 'uncertain' }
  const result = parseMallWeatherExportCreateResult(response.data)
  if (!result || !mallWeatherExportCreateResultMatchesRequest(result, request)) return { kind: 'uncertain' }
  return { kind: 'accepted', result }
}

export function resolveMallWeatherExportStorage(resolve: () => ExportStorage): ExportStorage | null {
  try {
    return resolve()
  } catch {
    return null
  }
}

export function loadMallWeatherExportSession(
  actorID: string,
  mallID: number,
  storage: ExportStorage | null,
): MallWeatherExportSession | null {
  const storageKey = mallWeatherExportSessionStorageKey(actorID, mallID)
  if (!storage) return null
  let raw: string | null
  try {
    raw = storage.getItem(storageKey)
  } catch {
    return null
  }
  if (!raw) return null
  try {
    const snapshot: unknown = JSON.parse(raw)
    if (!isRecord(snapshot)) return null
    const pending = snapshot.pending === null || snapshot.pending === undefined
      ? null
      : parseStoredPendingCreate(snapshot.pending, mallID)
    const jobId = snapshot.jobId === undefined ? '' : snapshot.jobId
    if (pending === null && snapshot.pending !== null && snapshot.pending !== undefined ||
      typeof jobId !== 'string' || jobId !== '' && !exportJobIDPattern.test(jobId) ||
      !pending && !jobId) return null
    return { pending, jobId }
  } catch {
    return null
  }
}

export function saveMallWeatherExportSession(
  actorID: string,
  mallID: number,
  session: MallWeatherExportSession,
  storage: ExportStorage | null,
): boolean {
  const storageKey = mallWeatherExportSessionStorageKey(actorID, mallID)
  if (!session.pending && !session.jobId) {
    if (!storage) return false
    try {
      storage.removeItem(storageKey)
      return true
    } catch {
      return false
    }
  }
  const pending = session.pending
  if (pending && (!exportKeyPattern.test(pending.key) ||
    !storedExportRequestIsValid(pending.body, mallID)) ||
    session.jobId && !exportJobIDPattern.test(session.jobId)) {
    throw new Error('invalid mall weather export session')
  }
  const serialized = JSON.stringify(session)
  if (!storage) return false
  try {
    storage.setItem(storageKey, serialized)
    return true
  } catch {
    return false
  }
}

export function clearMallWeatherExportSession(
  actorID: string,
  mallID: number,
  storage: ExportStorage | null,
): boolean {
  const storageKey = mallWeatherExportSessionStorageKey(actorID, mallID)
  if (!storage) return false
  try {
    storage.removeItem(storageKey)
    return true
  } catch {
    return false
  }
}

function parseStoredPendingCreate(value: unknown, mallID: number): MallWeatherExportPendingCreate | null {
  if (!isRecord(value) || typeof value.key !== 'string' || !exportKeyPattern.test(value.key) ||
    !isRecord(value.body)) return null
  const body = value.body
  if (!isRecord(body.filters) || !Array.isArray(body.filters.mallIds) ||
    body.filters.mallIds.length !== 1 || body.filters.mallIds[0] !== mallID) return null
  const hasProfileID = hasOwn(body, 'profileId')
  const hasProfileVersion = hasOwn(body, 'expectedProfileVersion')
  const hasStart = hasOwn(body.filters, 'start')
  const hasEnd = hasOwn(body.filters, 'end')
  if (hasProfileID !== hasProfileVersion || hasStart !== hasEnd) return null
  if (hasProfileID && (!positiveInteger(body.profileId) || !positiveInteger(body.expectedProfileVersion))) return null
  if (hasStart && (!isRFC3339(body.filters.start) || !isRFC3339(body.filters.end))) return null
  if (hasProfileID && !hasStart) return null
  const request: MallWeatherExportCreateRequest = hasProfileID
    ? {
        profileId: body.profileId as number,
        expectedProfileVersion: body.expectedProfileVersion as number,
        filters: {
          mallIds: [mallID],
          start: body.filters.start as string,
          end: body.filters.end as string,
        },
      }
    : {
        filters: hasStart
          ? {
              mallIds: [mallID],
              start: body.filters.start as string,
              end: body.filters.end as string,
            }
          : { mallIds: [mallID] },
      }
  return storedExportRequestIsValid(request, mallID) ? { key: value.key, body: request } : null
}

function storedExportRequestIsValid(request: MallWeatherExportCreateRequest, mallID: number) {
  if (!isRecord(request) || !isRecord(request.filters) || !Array.isArray(request.filters.mallIds) ||
    request.filters.mallIds.length !== 1 || request.filters.mallIds[0] !== mallID) return false
  const hasProfileID = hasOwn(request, 'profileId')
  const hasProfileVersion = hasOwn(request, 'expectedProfileVersion')
  if (hasProfileID !== hasProfileVersion) return false
  if (hasProfileID && (!positiveInteger(request.profileId) || !positiveInteger(request.expectedProfileVersion))) return false
  const hasStart = hasOwn(request.filters, 'start')
  const hasEnd = hasOwn(request.filters, 'end')
  if (hasStart !== hasEnd || hasProfileID && !hasStart) return false
  if (!hasStart) return true
  if (!isRFC3339(request.filters.start) || !isRFC3339(request.filters.end)) return false
  const start = Date.parse(request.filters.start)
  const end = Date.parse(request.filters.end)
  return end > start && end - start <= 366 * 24 * 60 * 60 * 1_000
}

function requestHasLegacyProfile(
  request: MallWeatherExportCreateRequest,
): request is MallWeatherExportLegacyCreateRequest {
  return hasOwn(request, 'profileId') && hasOwn(request, 'expectedProfileVersion')
}

function mallWeatherExportSessionStorageKey(actorID: string, mallID: number) {
  const numericActorID = Number(actorID)
  if (!/^[1-9]\d*$/.test(actorID) || !Number.isSafeInteger(numericActorID)) throw new Error('invalid actor id')
  if (!positiveInteger(mallID)) throw new Error('invalid mall id')
  return `mall-weather-export:${actorID}:${mallID}`
}

export function parseMallWeatherExportCreateResult(payload: unknown): MallWeatherExportCreateResult | null {
  const data = envelopeData(payload)
  if (!data || typeof data.jobId !== 'string' || !exportJobIDPattern.test(data.jobId) || data.status !== 'PENDING' ||
    !positiveInteger(data.profileId) || !positiveInteger(data.profileVersion) || !nonNegativeInteger(data.estimatedRows) ||
    !positiveInteger(data.createdBy) || !isRFC3339(data.createdAt)) return null
  return {
    jobId: data.jobId,
    status: 'PENDING',
    profileId: data.profileId,
    profileVersion: data.profileVersion,
    estimatedRows: data.estimatedRows,
    createdBy: data.createdBy,
    createdAt: data.createdAt,
  }
}

export function parseMallWeatherExportJob(payload: unknown): MallWeatherExportJob | null {
  const data = envelopeData(payload)
  if (!data || typeof data.jobId !== 'string' || !exportJobIDPattern.test(data.jobId) || !positiveInteger(data.profileId) ||
    !positiveInteger(data.profileVersion) || typeof data.status !== 'string' || !exportJobStatuses.has(data.status as MallWeatherExportJobStatus) ||
    !nonNegativeInteger(data.totalRows) || !nonNegativeInteger(data.processedRows) ||
    typeof data.cancelRequested !== 'boolean' || !nonNegativeInteger(data.fileSizeBytes) ||
    (data.currentSheet !== undefined && (typeof data.currentSheet !== 'string' || Array.from(data.currentSheet).length > 255)) ||
    (data.errorMessageSafe !== undefined && (typeof data.errorMessageSafe !== 'string' || Array.from(data.errorMessageSafe).length > 2_000)) ||
    (data.expiresAt !== undefined && !isRFC3339(data.expiresAt))) return null
  return {
    jobId: data.jobId,
    profileId: data.profileId,
    profileVersion: data.profileVersion,
    status: data.status as MallWeatherExportJobStatus,
    totalRows: Math.max(data.totalRows, data.processedRows),
    processedRows: data.processedRows,
    currentSheet: typeof data.currentSheet === 'string' ? data.currentSheet : '',
    cancelRequested: data.cancelRequested,
    fileSizeBytes: data.fileSizeBytes,
    errorMessageSafe: typeof data.errorMessageSafe === 'string' ? data.errorMessageSafe : '',
    ...(typeof data.expiresAt === 'string' ? { expiresAt: data.expiresAt } : {}),
  }
}

export function parseMallWeatherExportContentStatus(payload: unknown) {
  const data = envelopeData(payload)
  if (!data || !nonEmptyString(data.fileName, 255) ||
    !Number.isSafeInteger(data.fileSizeBytes) || Number(data.fileSizeBytes) < 4 ||
    data.contentType !== 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet') return null
  return {
    fileName: data.fileName,
    fileSizeBytes: Number(data.fileSizeBytes),
    contentType: data.contentType,
  }
}

export function submitMallWeatherExportContentDownload(
  documentRef: Document,
  action: string,
  token: string,
  fileName: string,
  scheduleCleanup: (callback: () => void, delayMilliseconds: number) => unknown,
): void {
  if (!documentRef?.body || !nonEmptyString(action, 8_192) || !nonEmptyString(token, 16_384) ||
    !nonEmptyString(fileName, 255) || typeof scheduleCleanup !== 'function') {
    throw new Error('invalid mall weather export browser download')
  }
  let parsedAction: URL
  try {
    parsedAction = new URL(action, 'https://mall-weather-download.invalid')
  } catch {
    throw new Error('invalid mall weather export browser download')
  }
  const contentPath = parsedAction.pathname.match(
    /^\/api\/v1\/weather-exports\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\/content$/i,
  )
  if (!contentPath || !['http:', 'https:'].includes(parsedAction.protocol) || parsedAction.username ||
    parsedAction.password || parsedAction.search || parsedAction.hash) {
    throw new Error('invalid mall weather export browser download')
  }

  exportDownloadFrameSequence++
  const frameName = `mall-weather-export-download-${contentPath[1]}-${exportDownloadFrameSequence}`
  const frame = documentRef.createElement('iframe')
  frame.hidden = true
  frame.title = `下载 ${fileName}`
  frame.name = frameName
  frame.referrerPolicy = 'no-referrer'
  frame.setAttribute('aria-hidden', 'true')
  frame.setAttribute('sandbox', 'allow-downloads')
  const form = documentRef.createElement('form')
  form.hidden = true
  form.method = 'POST'
  form.action = action
  form.target = frameName
  form.enctype = 'application/x-www-form-urlencoded'
  form.acceptCharset = 'UTF-8'
  const tokenInput = documentRef.createElement('input')
  tokenInput.type = 'hidden'
  tokenInput.name = 'token'
  tokenInput.value = token
  form.append(tokenInput)

  try {
    documentRef.body.append(frame)
    documentRef.body.append(form)
    form.submit()
    form.remove()
    scheduleCleanup(() => frame.remove(), mallWeatherExportDownloadFrameLifetimeMilliseconds)
  } catch (error) {
    form.remove()
    frame.remove()
    throw error
  }
}

export function mallWeatherExportJobTerminal(status: MallWeatherExportJobStatus) {
  return status === 'SUCCEEDED' || status === 'FAILED' || status === 'CANCELLED' || status === 'EXPIRED'
}

export function mallWeatherExportDownloadReadiness(
  job: Pick<MallWeatherExportJob, 'status' | 'expiresAt'>,
  now = new Date(),
): MallWeatherExportDownloadReadiness {
  if (job.status === 'EXPIRED') return 'expired'
  if (job.status !== 'SUCCEEDED') return 'not-ready'
  const expiresAt = job.expiresAt ? Date.parse(job.expiresAt) : Number.NaN
  if (!Number.isFinite(now.getTime()) || !Number.isFinite(expiresAt) || expiresAt - now.getTime() < 60_000) return 'expired'
  return 'ready'
}

export function mallWeatherExportProgress(job: Pick<MallWeatherExportJob, 'processedRows' | 'totalRows'>) {
  const totalRows = Math.max(job.totalRows, job.processedRows)
  if (totalRows <= 0) return 0
  return Math.min(100, Math.max(0, Math.round(job.processedRows * 100 / totalRows)))
}

export function mallWeatherExportPollStatusRetryable(status: number) {
  return status === 0 || status === 408 || status === 429 || (status >= 500 && status <= 599)
}

export function mallWeatherExportPollFailureAction(status: number): MallWeatherExportPollFailureAction {
  if (mallWeatherExportPollStatusRetryable(status)) return 'retry'
  if (status === 404 || status === 422) return 'forget'
  return 'pause'
}

export function mallWeatherExportPollingActive(
  status: MallWeatherExportJobStatus | undefined,
  pollingPaused: boolean,
) {
  return Boolean(status && !mallWeatherExportJobTerminal(status) && !pollingPaused)
}

export function mallWeatherExportPollRetryDelayMilliseconds(transientFailureCount: number) {
  if (!Number.isSafeInteger(transientFailureCount) || transientFailureCount < 1) {
    throw new Error('invalid mall weather export transient failure count')
  }
  return Math.min(
    mallWeatherExportMaximumPollRetryDelayMilliseconds,
    mallWeatherExportPollIntervalMilliseconds * (2 ** (transientFailureCount - 1)),
  )
}

export async function waitForMallWeatherExportRequest<T>(
  request: Promise<T>,
  controller: AbortController,
  timeoutMilliseconds = mallWeatherExportRequestTimeoutMilliseconds,
): Promise<T> {
  return waitForMallWeatherExportAbortableRequest(
    request,
    controller,
    timeoutMilliseconds,
    () => new MallWeatherExportRequestTimeoutError(),
  )
}

async function waitForMallWeatherExportAbortableRequest<T>(
  request: Promise<T>,
  controller: AbortController,
  timeoutMilliseconds: number,
  timeoutError: () => Error,
): Promise<T> {
  if (!Number.isSafeInteger(timeoutMilliseconds) || timeoutMilliseconds < 1 || controller.signal.aborted) {
    throw new Error('invalid mall weather export request timeout')
  }
  let timer: ReturnType<typeof globalThis.setTimeout> | undefined
  let timedOut = false
  let cancelRequest: (() => void) | null = null
  try {
    return await new Promise<T>((resolve, reject) => {
      cancelRequest = () => {
        if (!timedOut) reject(new DOMException('mall weather export request aborted', 'AbortError'))
      }
      controller.signal.addEventListener('abort', cancelRequest, { once: true })
      timer = globalThis.setTimeout(() => {
        timedOut = true
        controller.abort()
        reject(timeoutError())
      }, timeoutMilliseconds)
      request.then(resolve, reject)
    })
  } finally {
    globalThis.clearTimeout(timer)
    if (cancelRequest) controller.signal.removeEventListener('abort', cancelRequest)
  }
}

export function parseMallWeatherExportSafeErrorMessage(payload: unknown): string | null {
  if (!isRecord(payload) || typeof payload.code !== 'number' || payload.code === 0 ||
    !nonEmptyString(payload.msg, 200)) return null
  return payload.msg
}

export function mallWeatherExportJobPath(jobID: string) {
  if (!exportJobIDPattern.test(jobID)) throw new Error('invalid mall weather export job id')
  return `/v1/weather-exports/${jobID}`
}

export function mallWeatherExportDownloadPath(jobID: string) {
  return `${mallWeatherExportJobPath(jobID)}/download`
}

export function mallWeatherExportContentPath(jobID: string) {
  return `${mallWeatherExportJobPath(jobID)}/content`
}
