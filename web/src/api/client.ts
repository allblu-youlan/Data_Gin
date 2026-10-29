import { effectiveApiStatus } from '../apiResponse'
import { apiURL } from '../apiURL'
import { isSuccessfulPayload, readEnvelopeToken } from './auth'

export type HTTPMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type ClientFailureKind = 'aborted' | 'offline' | 'timeout' | 'rate_limited' | 'unauthorized' | 'forbidden' | 'client' | 'server' | 'network'

export type ClientResponse = {
  ok: boolean
  status: number
  data: unknown
  error?: {
    kind: ClientFailureKind
    message: string
    retryAfterSeconds?: number
  }
}

export type TokenRefreshResult =
  | { kind: 'refreshed' }
  | { kind: 'unauthorized' }
  | { kind: 'superseded' }
  | { kind: 'transient'; response: ClientResponse }

export type ApiRequestOptions = {
  method: HTTPMethod
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
  retry?: boolean
  timeoutMs?: number
  acceptBareJSONSuccess?: boolean
  acceptSafeErrorMessage?: boolean
}

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

export type ApiClientOptions = {
  baseURL?: string
  getToken: () => string
  onTokenRefreshed: (token: string) => void
  onUnauthorized: () => void
  fetch?: FetchLike
  timeoutMs?: number
  maxGetRetries?: number
  random?: () => number
  wait?: (milliseconds: number, signal?: AbortSignal) => Promise<void>
}

type RequestSignal = {
  signal: AbortSignal
  cleanup: () => void
  wasTimedOut: () => boolean
}

function createRequestSignal(external: AbortSignal | undefined, timeoutMs: number): RequestSignal {
  const controller = new AbortController()
  let timedOut = false
  const abortExternal = () => controller.abort(external?.reason)
  external?.addEventListener('abort', abortExternal, { once: true })
  const timer = globalThis.setTimeout(() => {
    timedOut = true
    controller.abort(new DOMException('请求超时', 'TimeoutError'))
  }, timeoutMs)
  return {
    signal: controller.signal,
    cleanup: () => {
      globalThis.clearTimeout(timer)
      external?.removeEventListener('abort', abortExternal)
    },
    wasTimedOut: () => timedOut,
  }
}

function publicFailure(status: number, retryAfterSeconds?: number): NonNullable<ClientResponse['error']> {
  if (status === 401) return { kind: 'unauthorized', message: '登录状态已失效，请重新登录。' }
  if (status === 403) return { kind: 'forbidden', message: '当前账号没有执行此操作的权限。' }
  if (status === 429) return { kind: 'rate_limited', message: retryAfterSeconds ? `请求过于频繁，请在 ${retryAfterSeconds} 秒后重试。` : '请求过于频繁，请稍后重试。', retryAfterSeconds }
  if (status >= 500) return { kind: 'server', message: '服务暂时不可用，请稍后重试。' }
  return { kind: 'client', message: '请求未能完成，请检查输入后重试。' }
}

function publicResponseMessage(payload: unknown, status: number) {
  if (status < 400 || status >= 500 || payload === null || typeof payload !== 'object' || Array.isArray(payload)) return ''
  const message = (payload as Record<string, unknown>).msg
  if (typeof message !== 'string') return ''
  const normalized = message.trim()
  const hasControlCharacter = Array.from(normalized).some((character) => {
    const code = character.charCodeAt(0)
    return code <= 31 || code === 127
  })
  return normalized && normalized.length <= 300 && !hasControlCharacter ? normalized : ''
}

function retryAfterSeconds(headers: Headers) {
  const value = headers.get('Retry-After')
  if (!value) return undefined
  const seconds = Number(value)
  return Number.isFinite(seconds) && seconds >= 0 ? Math.ceil(seconds) : undefined
}

function getRetryDelay(attempt: number, random: () => number, retryAfter?: number) {
  const exponentialDelay = Math.min(2_000, 250 * 2 ** attempt)
  const randomValue = Math.min(1, Math.max(0, random()))
  const jitteredDelay = Math.round(exponentialDelay * (0.8 + randomValue * 0.4))
  return retryAfter === undefined ? jitteredDelay : Math.max(jitteredDelay, retryAfter * 1_000)
}

function isRetryableStatus(status: number) {
  return status === 502 || status === 503 || status === 504
}

function defaultWait(milliseconds: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (milliseconds <= 0 || signal?.aborted) {
      resolve()
      return
    }
    const finish = () => {
      globalThis.clearTimeout(timer)
      signal?.removeEventListener('abort', finish)
      resolve()
    }
    const timer = globalThis.setTimeout(finish, milliseconds)
    signal?.addEventListener('abort', finish, { once: true })
    if (signal?.aborted) finish()
  })
}

function abortedResponse(): ClientResponse {
  return { ok: false, status: 0, data: { message: '请求已取消。' }, error: { kind: 'aborted', message: '请求已取消。' } }
}

function isFormData(body: unknown): body is FormData {
  return typeof FormData !== 'undefined' && body instanceof FormData
}

function isBareJSONObject(payload: unknown) {
  return payload !== null
    && typeof payload === 'object'
    && !Array.isArray(payload)
    && !Object.prototype.hasOwnProperty.call(payload, 'code')
}

function withoutContentType(headers: Record<string, string> | undefined) {
  if (!headers) return undefined
  return Object.fromEntries(Object.entries(headers).filter(([name]) => name.toLowerCase() !== 'content-type'))
}

function authorizedHeaders(token: string, extra: Record<string, string> | undefined, hasJSONBody: boolean, omitContentType = false) {
  return {
    ...(hasJSONBody ? { 'Content-Type': 'application/json' } : {}),
    ...(omitContentType ? withoutContentType(extra) : extra),
    ...(token ? { token, Authorization: `Bearer ${token}` } : {}),
  }
}

function writeTransport(path: string, method: HTTPMethod) {
  if (method === 'GET' || method === 'POST') return { path, method }
  // Match the backend POST aliases because the public gateway rejects other write methods.
  const action = { PUT: 'update', PATCH: 'patch', DELETE: 'delete' }[method]
  const suffixIndex = path.search(/[?#]/)
  const pathname = suffixIndex < 0 ? path : path.slice(0, suffixIndex)
  const query = suffixIndex < 0 ? '' : path.slice(suffixIndex)
  return { path: `${pathname.replace(/\/+$/, '')}/${action}${query}`, method: 'POST' as const }
}

export function createApiClient(options: ApiClientOptions) {
  const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis)
  const timeoutMs = options.timeoutMs ?? 15_000
  const maxGetRetries = options.maxGetRetries ?? 2
  const random = options.random ?? Math.random
  const wait = options.wait ?? defaultWait
  const refreshPromises = new Map<string, Promise<TokenRefreshResult>>()

  async function waitForRetry(milliseconds: number, signal: AbortSignal | undefined) {
    if (signal?.aborted) return false
    let removeAbortListener: (() => void) | undefined
    const aborted = signal
      ? new Promise<void>((resolve) => {
        const finish = () => resolve()
        signal.addEventListener('abort', finish, { once: true })
        removeAbortListener = () => signal.removeEventListener('abort', finish)
        if (signal.aborted) finish()
      })
      : undefined
    try {
      await (aborted ? Promise.race([wait(milliseconds, signal), aborted]) : wait(milliseconds, signal))
    } finally {
      removeAbortListener?.()
    }
    return !signal?.aborted
  }

  async function refreshToken(expectedToken = options.getToken()) {
    if (!expectedToken) return { kind: 'unauthorized' } as const
    if (options.getToken() !== expectedToken) return { kind: 'superseded' } as const
    const existingRefresh = refreshPromises.get(expectedToken)
    if (existingRefresh) return existingRefresh
    const refreshPromise: Promise<TokenRefreshResult> = (async () => {
      const request = createRequestSignal(undefined, timeoutMs)
      try {
        const response = await fetchImpl(apiURL('/auth/token/refresh', options.baseURL), {
          method: 'POST',
          signal: request.signal,
          headers: authorizedHeaders(expectedToken, undefined, true),
          body: JSON.stringify({ token_type: 'refreshable' }),
        })
        const payload: unknown = await response.json().catch(() => null)
        if (options.getToken() !== expectedToken) return { kind: 'superseded' }
        const status = effectiveApiStatus(response.status, payload)
        if (status === 401) return { kind: 'unauthorized' }
        const token = response.ok && status < 400 && isSuccessfulPayload(payload) ? readEnvelopeToken(payload) : ''
        if (!token) {
          const retryAfter = retryAfterSeconds(response.headers)
          const error = publicFailure(status, retryAfter)
          return {
            kind: 'transient',
            response: { ok: false, status, data: { message: error.message }, error },
          }
        }
        if (options.getToken() !== expectedToken) return { kind: 'superseded' }
        options.onTokenRefreshed(token)
        return { kind: 'refreshed' }
      } catch {
        if (options.getToken() !== expectedToken) return { kind: 'superseded' }
        if (request.wasTimedOut()) {
          const message = '请求超时，请稍后重试。'
          return {
            kind: 'transient',
            response: { ok: false, status: 0, data: { message }, error: { kind: 'timeout', message } },
          }
        }
        const offline = typeof navigator !== 'undefined' && navigator.onLine === false
        const message = offline ? '当前处于离线状态，已保留最近一次数据。' : '网络连接异常，请检查网络后重试。'
        return {
          kind: 'transient',
          response: { ok: false, status: 0, data: { message }, error: { kind: offline ? 'offline' : 'network', message } },
        }
      } finally {
        request.cleanup()
      }
    })()
    refreshPromises.set(expectedToken, refreshPromise)
    void refreshPromise.finally(() => {
      if (refreshPromises.get(expectedToken) === refreshPromise) refreshPromises.delete(expectedToken)
    })
    return refreshPromise
  }

  async function request(path: string, requestOptions: ApiRequestOptions): Promise<ClientResponse> {
    const { method, body, headers, signal, retry = true, acceptBareJSONSuccess = false, acceptSafeErrorMessage = false } = requestOptions
    const transport = writeTransport(path, method)
    const retryable = method === 'GET' && retry
    let attempt = 0
    let replayedAfterRefresh = false

    while (true) {
      const requestSignal = createRequestSignal(signal, requestOptions.timeoutMs ?? timeoutMs)
      try {
        const canSendBody = method !== 'GET' && body !== undefined
        const formDataBody = canSendBody && isFormData(body)
        const requestToken = options.getToken()
        const response = await fetchImpl(apiURL(transport.path, options.baseURL), {
          method: transport.method,
          signal: requestSignal.signal,
          headers: authorizedHeaders(requestToken, headers, canSendBody && !formDataBody, formDataBody || method === 'GET'),
          body: !canSendBody ? undefined : formDataBody ? body : JSON.stringify(body),
        })
        const payload: unknown = await response.json().catch(() => null)
        const status = effectiveApiStatus(response.status, payload)
        const successfulPayload = isSuccessfulPayload(payload) || (acceptBareJSONSuccess && isBareJSONObject(payload))
        if (response.ok && status < 400 && successfulPayload) return { ok: true, status, data: payload }

        if (status === 401) {
          if (options.getToken() !== requestToken) return abortedResponse()
          if (retryable && !replayedAfterRefresh) {
            replayedAfterRefresh = true
            const refreshResult = await refreshToken(requestToken)
            if (refreshResult.kind === 'refreshed') continue
            if (refreshResult.kind === 'superseded') return abortedResponse()
            if (refreshResult.kind === 'transient') return refreshResult.response
          }
          if (options.getToken() !== requestToken) return abortedResponse()
          options.onUnauthorized()
        }

        const retryAfter = retryAfterSeconds(response.headers)
        if (retryable && !replayedAfterRefresh && attempt < maxGetRetries && isRetryableStatus(status)) {
          if (!await waitForRetry(getRetryDelay(attempt++, random, retryAfter), signal)) return abortedResponse()
          continue
        }
        const error = publicFailure(status, retryAfter)
        const responseMessage = acceptSafeErrorMessage ? publicResponseMessage(payload, status) : ''
        if (responseMessage && error.kind === 'client') error.message = responseMessage
        return { ok: false, status, data: { message: error.message }, error }
      } catch {
        if (signal?.aborted) return abortedResponse()
        if (requestSignal.wasTimedOut()) {
          if (retryable && !replayedAfterRefresh && attempt < maxGetRetries) {
            if (!await waitForRetry(getRetryDelay(attempt++, random), signal)) return abortedResponse()
            continue
          }
          return { ok: false, status: 0, data: { message: '请求超时，请稍后重试。' }, error: { kind: 'timeout', message: '请求超时，请稍后重试。' } }
        }
        const offline = typeof navigator !== 'undefined' && navigator.onLine === false
        if (retryable && !replayedAfterRefresh && attempt < maxGetRetries) {
          if (!await waitForRetry(getRetryDelay(attempt++, random), signal)) return abortedResponse()
          continue
        }
        const message = offline ? '当前处于离线状态，已保留最近一次数据。' : '网络连接异常，请检查网络后重试。'
        return { ok: false, status: 0, data: { message }, error: { kind: offline ? 'offline' : 'network', message } }
      } finally {
        requestSignal.cleanup()
      }
    }
  }

  return { request, refresh: refreshToken }
}
