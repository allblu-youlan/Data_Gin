import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { apiURL } from '../apiURL'
import { createApiClient, type ApiRequestOptions, type ClientResponse } from '../api/client'
import { verifySessionResponses, type SessionUser } from '../api/auth'
import { browserLocalStorage } from '../browserStorage'
import {
  clearStoredToken,
  loadStoredSessionUser,
  loadStoredToken,
  saveStoredSessionUser,
  saveStoredToken,
  saveStoredTokenExpiry,
  storedTokenExpiresAt,
  type StoredSessionUser,
} from '../authStorage'
import { parseMallWeatherExportContentStatus, submitMallWeatherExportContentDownload } from '../mallWeatherExport'
import type { WorkspaceApiClient, WorkspaceDownloadClient } from './WorkspaceRouter'

export type ConsoleSessionState = 'checking' | 'authenticated' | 'anonymous'

export function useConsoleSession(baseURL: string) {
  const [token, setToken] = useState(() => loadStoredToken(browserLocalStorage))
  const tokenRef = useRef(token)
  const [sessionState, setSessionState] = useState<ConsoleSessionState>(() => token ? 'checking' : 'anonymous')
  const authenticatedSessionRef = useRef(sessionState === 'authenticated')
  const [sessionUser, setSessionUser] = useState<SessionUser | null>(() => {
    const user = loadStoredSessionUser(browserLocalStorage)
    return user ? { ...user, phone: '', accountType: 'CONSOLE', status: 'ACTIVE', mallScopeMode: 'SELECTED', roles: [], permissions: [], mallIds: [] } : null
  })
  const [sessionExpiresAt, setSessionExpiresAt] = useState(() => storedTokenExpiresAt(browserLocalStorage))
  const [sessionValidationError, setSessionValidationError] = useState('')
  const [sessionValidationAttempt, setSessionValidationAttempt] = useState(0)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ClientResponse | null>(null)
  const actorID = sessionState === 'authenticated' && sessionUser ? String(sessionUser.id) : null

  const clearSession = useCallback(() => {
    clearStoredToken(browserLocalStorage)
    tokenRef.current = ''
    setToken('')
    setSessionUser(null)
    setSessionExpiresAt(null)
    setSessionValidationError('')
    setSessionState('anonymous')
    setResult(null)
  }, [])

  const updateSessionToken = useCallback((nextToken: string) => {
    const expiresAt = saveStoredToken(nextToken, browserLocalStorage)
    tokenRef.current = nextToken
    setToken(nextToken)
    setSessionExpiresAt(expiresAt)
  }, [])

  const apiClient = useMemo(
    () => createApiClient({
      baseURL,
      getToken: () => tokenRef.current,
      onTokenRefreshed: updateSessionToken,
      onUnauthorized: clearSession,
    }),
    [baseURL, clearSession, updateSessionToken],
  )

  const client = useCallback<WorkspaceApiClient>(
    async (path, options = {}) => {
      if (!options.silentLoading) setLoading(true)
      try {
        if (!options.method) {
          const nextResult: ClientResponse = {
            ok: false,
            status: 0,
            data: { message: '请求方法未指定。' },
            error: { kind: 'client', message: '请求方法未指定。' },
          }
          if (options.showResult !== false) setResult(nextResult)
          return nextResult
        }
        const requestOptions: ApiRequestOptions = {
          method: options.method,
          responseType: options.responseType,
          body: options.body,
          headers: options.headers,
          signal: options.signal,
          retry: options.retry,
          timeoutMs: options.timeoutMs,
          acceptBareJSONSuccess: options.acceptBareJSONSuccess,
          acceptSafeErrorMessage: options.acceptSafeErrorMessage,
        }
        const nextResult = await apiClient.request(path, requestOptions)
        if (options.showResult !== false) setResult(nextResult)
        return nextResult
      } finally {
        if (!options.silentLoading) setLoading(false)
      }
    },
    [apiClient],
  )

  const downloadFile = useCallback<WorkspaceDownloadClient>(async (path, fileName, signal) => {
    const validFileName = /^mall_weather_export_[0-9a-f-]{36}\.xlsx$/i.test(fileName)
    if (!validFileName) return { ok: false, status: 422, data: 'invalid download file name' }
    if (signal.aborted) return { ok: false, status: 0, data: 'download request aborted' }
    try {
      const readiness = await client(`${path}/status`, {
        method: 'GET', signal, showResult: false, silentLoading: true,
      })
      if (!readiness.ok) return readiness
      const contentStatus = parseMallWeatherExportContentStatus(readiness.data)
      if (!contentStatus || contentStatus.fileName !== fileName) {
        return { ok: false, status: 502, data: 'invalid XLSX content status' }
      }
      submitMallWeatherExportContentDownload(
        document,
        apiURL(path, baseURL),
        token,
        fileName,
        (callback, delayMilliseconds) => window.setTimeout(callback, delayMilliseconds),
      )
      return { ok: true, status: readiness.status, data: contentStatus }
    } catch (error) {
      return { ok: false, status: 0, data: error instanceof Error ? error.message : String(error) }
    }
  }, [baseURL, client, token])

  useEffect(() => {
    if (!token) return
    if (sessionExpiresAt === null || sessionExpiresAt <= Date.now()) {
      clearSession()
      return
    }

    let timer = 0
    let current = true
    const refreshSession = () => {
      const remaining = sessionExpiresAt - Date.now()
      if (remaining <= 0) {
        clearSession()
        return
      }
      void apiClient.refresh().then((result) => {
        if (!current) return
        if (result.kind === 'refreshed') {
          setSessionValidationError('')
          return
        }
        if (result.kind === 'superseded') return
        if (result.kind === 'unauthorized') {
          clearSession()
          return
        }
        setSessionValidationError('会话续期暂不可用，登录状态已保留。')
        const retryDelay = Math.min(30_000, Math.max(1_000, sessionExpiresAt - Date.now()))
        timer = window.setTimeout(refreshSession, retryDelay)
      })
    }
    const refreshDelay = Math.max(0, sessionExpiresAt - Date.now() - 60_000)
    timer = window.setTimeout(refreshSession, Math.min(refreshDelay, 2_147_000_000))
    return () => {
      current = false
      window.clearTimeout(timer)
    }
  }, [apiClient, clearSession, sessionExpiresAt, token])

  useEffect(() => {
    authenticatedSessionRef.current = sessionState === 'authenticated'
  }, [sessionState])

  useEffect(() => {
    if (!token) return
    let current = true
    let retryTimer = 0
    const controller = new AbortController()
    const preserveAuthenticatedSession = authenticatedSessionRef.current
    if (!preserveAuthenticatedSession) setSessionState('checking')
    void Promise.all([
      apiClient.request('/auth/me', { method: 'GET', signal: controller.signal }),
      apiClient.request('/auth/token/info', { method: 'GET', signal: controller.signal }),
    ]).then(([profileResponse, tokenInfoResponse]) => {
      if (!current) return
      const verification = verifySessionResponses(profileResponse, tokenInfoResponse)
      if (verification.kind !== 'valid') {
        if (verification.kind === 'unauthorized' || verification.kind === 'invalid') {
          clearSession()
          return
        }
        setSessionValidationError(preserveAuthenticatedSession
          ? '会话校验暂不可用，当前数据可能已过期。'
          : '会话校验暂不可用，已保留本地登录状态。')
        if (!preserveAuthenticatedSession && !loadStoredSessionUser(browserLocalStorage)) {
          setSessionState('checking')
          retryTimer = window.setTimeout(() => setSessionValidationAttempt((attempt) => attempt + 1), 30_000)
        } else {
          setSessionState('authenticated')
        }
        return
      }
      const { user, tokenInfo } = verification
      const storedUser: StoredSessionUser = { id: user.id, account: user.account, nickname: user.nickname }
      saveStoredSessionUser(storedUser, browserLocalStorage)
      saveStoredTokenExpiry(tokenInfo.expireTime * 1000, browserLocalStorage)
      setSessionUser(user)
      setSessionExpiresAt(tokenInfo.expireTime * 1000)
      setSessionValidationError('')
      setSessionState('authenticated')
    })
    return () => {
      current = false
      controller.abort()
      window.clearTimeout(retryTimer)
    }
  }, [apiClient, clearSession, sessionValidationAttempt, token])

  const login = useCallback((nextToken: string) => {
    updateSessionToken(nextToken)
    setSessionState('checking')
  }, [updateSessionToken])
  const retrySessionValidation = useCallback(() => setSessionValidationAttempt((attempt) => attempt + 1), [])

  return {
    actorID,
    client,
    downloadFile,
    loading,
    login,
    logout: clearSession,
    result,
    retrySessionValidation,
    sessionState,
    sessionUser,
    sessionValidationError,
    setLoading,
    setResult,
    token,
  }
}
