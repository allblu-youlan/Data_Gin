import assert from 'node:assert/strict'
import test from 'node:test'

import { createApiClient } from '../.test-dist/api/client.js'

function payload(data = {}) {
  return { code: 200, data }
}

function response(status, data = {}, headers = {}) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
}

function clientOptions(overrides = {}) {
  return {
    getToken: () => 'secret-token-that-must-not-leak',
    onTokenRefreshed: () => {},
    onUnauthorized: () => {},
    maxGetRetries: 0,
    wait: async () => {},
    ...overrides,
  }
}

test('write requests use POST action routes and preserve payload, query and headers', async (t) => {
  for (const [method, suffix] of [['PUT', '/update'], ['PATCH', '/patch'], ['DELETE', '/delete'], ['POST', '']]) {
    await t.test(method, async () => {
      let received
      const client = createApiClient(clientOptions({
        baseURL: 'https://example.test',
        fetch: async (input, init) => {
          received = { input, init }
          return response(200, payload({ saved: true }))
        },
      }))
      const body = { kaiyuShopId: 'shop-123', expectedMallVersion: 4 }

      const result = await client.request('/v1/malls/3?expectedMallVersion=4', {
        method,
        body,
        headers: { 'Idempotency-Key': 'save-mall-3' },
      })

      assert.equal(result.ok, true)
      assert.equal(received.input, `https://example.test/api/v1/malls/3${suffix}?expectedMallVersion=4`)
      assert.equal(received.init.method, 'POST')
      assert.deepEqual(JSON.parse(received.init.body), body)
      assert.equal(received.init.headers['Content-Type'], 'application/json')
      assert.equal(received.init.headers['Idempotency-Key'], 'save-mall-3')
      assert.equal(received.init.headers.token, 'secret-token-that-must-not-leak')
      assert.equal(received.init.headers.Authorization, 'Bearer secret-token-that-must-not-leak')
    })
  }
})

test('POST action routes never retry writes or refresh and replay them', async (t) => {
  for (const method of ['PUT', 'PATCH', 'DELETE']) {
    for (const status of [401, 503]) {
      await t.test(`${method} receives ${status}`, async () => {
        let calls = 0
        const client = createApiClient(clientOptions({
          maxGetRetries: 2,
          fetch: async () => {
            calls += 1
            return response(status, { code: status === 401 ? 100401 : 100503 })
          },
        }))

        const result = await client.request('/v1/malls/3', { method, body: {}, retry: true })

        assert.equal(result.ok, false)
        assert.equal(result.status, status)
        assert.equal(calls, 1)
      })
    }
  }
})

test('GET requests discard supplied bodies and content types', async () => {
  let received
  const client = createApiClient(clientOptions({
    fetch: async (_input, init) => {
      received = init
      return response(200, payload({ items: [] }))
    },
  }))
  const form = new FormData()
  form.append('ignored', 'value')

  const result = await client.request('/v1/runs', {
    method: 'GET',
    body: form,
    headers: { 'Content-Type': 'application/json' },
  })

  assert.equal(result.ok, true)
  assert.equal(received.method, 'GET')
  assert.equal(received.body, undefined)
  assert.equal(received.headers['Content-Type'], undefined)
  assert.equal(received.headers.token, 'secret-token-that-must-not-leak')
  assert.equal(received.headers.Authorization, 'Bearer secret-token-that-must-not-leak')
})

test('FormData requests preserve multipart boundaries and authentication headers', async () => {
  let received
  const client = createApiClient(clientOptions({
    fetch: async (_input, init) => {
      received = init
      return response(200, payload({ uploaded: true }))
    },
  }))
  const form = new FormData()
  form.append('file', 'workbook contents')

  const result = await client.request('/v1/uploads', {
    method: 'POST',
    body: form,
    headers: { 'Content-Type': 'application/json', 'X-Upload-Source': 'excel' },
  })

  assert.equal(result.ok, true)
  assert.equal(received.method, 'POST')
  assert.equal(received.body, form)
  assert.equal(received.headers['Content-Type'], undefined)
  assert.equal(received.headers['content-type'], undefined)
  assert.equal(received.headers['X-Upload-Source'], 'excel')
  assert.equal(received.headers.token, 'secret-token-that-must-not-leak')
  assert.equal(received.headers.Authorization, 'Bearer secret-token-that-must-not-leak')
})

test('rejects bare JSON success responses by default', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(200, { status: 'ok', service: 'gin-biz-web-api' }),
  }))

  const result = await client.request('/health', { method: 'GET' })

  assert.equal(result.ok, false)
  assert.equal(result.status, 200)
  assert.equal(result.error?.kind, 'client')
})

test('accepts bare JSON objects only when explicitly enabled', async () => {
  const health = { status: 'ok', service: 'gin-biz-web-api' }
  const client = createApiClient(clientOptions({
    fetch: async () => response(200, health),
  }))

  const result = await client.request('/health', {
    method: 'GET',
    acceptBareJSONSuccess: true,
  })

  assert.equal(result.ok, true)
  assert.equal(result.status, 200)
  assert.deepEqual(result.data, health)
})

test('does not accept HTTP errors when bare JSON success is enabled', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(503, { status: 'unavailable' }),
  }))

  const result = await client.request('/health', {
    method: 'GET',
    retry: false,
    acceptBareJSONSuccess: true,
  })

  assert.equal(result.ok, false)
  assert.equal(result.status, 503)
  assert.equal(result.error?.kind, 'server')
})

test('does not treat error envelopes as bare JSON success', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(200, { code: 500, message: 'internal error' }),
  }))

  const result = await client.request('/health', {
    method: 'GET',
    acceptBareJSONSuccess: true,
  })

  assert.equal(result.ok, false)
  assert.equal(result.status, 200)
  assert.equal(result.error?.kind, 'client')
})

test('concurrent unauthorized GETs use one refresh and replay once', async () => {
  let token = 'expired-token'
  let refreshes = 0
  let protectedRequests = 0
  let releasedRefresh
  const refreshGate = new Promise((resolve) => { releasedRefresh = resolve })
  const client = createApiClient(clientOptions({
    getToken: () => token,
    onTokenRefreshed: (nextToken) => { token = nextToken },
    fetch: async (input) => {
      const path = String(input)
      if (path.endsWith('/auth/token/refresh')) {
        refreshes += 1
        await refreshGate
        return response(200, payload({ token: 'fresh-token' }))
      }
      protectedRequests += 1
      return token === 'expired-token'
        ? response(401, { code: 100401, data: {} })
        : response(200, payload({ token: 'fresh-token', records: [] }))
    },
  }))

  const first = client.request('/v1/runs', { method: 'GET' })
  const second = client.request('/v1/delivery-logs', { method: 'GET' })
  await new Promise((resolve) => setImmediate(resolve))
  releasedRefresh()
  const [firstResult, secondResult] = await Promise.all([first, second])

  assert.equal(refreshes, 1)
  assert.equal(protectedRequests, 4)
  assert.equal(firstResult.ok, true)
  assert.equal(secondResult.ok, true)
})

test('does not restore a token or replay a request after logout during refresh', async () => {
  let token = 'expired-token'
  let refreshed = 0
  let protectedRequests = 0
  let releasedRefresh
  const refreshGate = new Promise((resolve) => { releasedRefresh = resolve })
  const client = createApiClient(clientOptions({
    getToken: () => token,
    onTokenRefreshed: () => { refreshed += 1 },
    fetch: async (input) => {
      if (String(input).endsWith('/auth/token/refresh')) {
        await refreshGate
        return response(200, payload({ token: 'fresh-token' }))
      }
      protectedRequests += 1
      return response(401, { code: 100401, data: {} })
    },
  }))

  const request = client.request('/v1/runs', { method: 'GET' })
  await new Promise((resolve) => setImmediate(resolve))
  token = ''
  releasedRefresh()
  const result = await request

  assert.equal(refreshed, 0)
  assert.equal(protectedRequests, 1)
  assert.equal(result.ok, false)
  assert.equal(result.error?.kind, 'aborted')
})

test('pending refresh results cannot overwrite or reject a newer token', async (t) => {
  for (const refreshStatus of [200, 401]) {
    await t.test(`refresh returns ${refreshStatus}`, async () => {
      let token = 'token-a'
      let refreshed = 0
      let unauthorized = 0
      let releaseRefresh
      const refreshGate = new Promise((resolve) => { releaseRefresh = resolve })
      const client = createApiClient(clientOptions({
        getToken: () => token,
        onTokenRefreshed: () => { refreshed += 1 },
        onUnauthorized: () => { unauthorized += 1 },
        fetch: async () => {
          await refreshGate
          return refreshStatus === 200
            ? response(200, payload({ token: 'stale-refreshed-token' }))
            : response(401, { code: 100401, data: {} })
        },
      }))

      const pending = client.refresh()
      await new Promise((resolve) => setImmediate(resolve))
      token = 'token-b'
      releaseRefresh()
      const result = await pending

      assert.deepEqual(result, { kind: 'superseded' })
      assert.equal(token, 'token-b')
      assert.equal(refreshed, 0)
      assert.equal(unauthorized, 0)
    })
  }
})

test('a stale GET 401 cannot refresh or clear the newer session', async () => {
  let token = 'token-a'
  let refreshes = 0
  let unauthorized = 0
  let releaseRequest
  const requestGate = new Promise((resolve) => { releaseRequest = resolve })
  const client = createApiClient(clientOptions({
    getToken: () => token,
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async (input) => {
      if (String(input).endsWith('/auth/token/refresh')) {
        refreshes += 1
        return response(401, { code: 100401, data: {} })
      }
      await requestGate
      return response(401, { code: 100401, data: {} })
    },
  }))

  const pending = client.request('/v1/runs', { method: 'GET' })
  await new Promise((resolve) => setImmediate(resolve))
  token = 'token-b'
  releaseRequest()
  const result = await pending

  assert.equal(token, 'token-b')
  assert.equal(refreshes, 0)
  assert.equal(unauthorized, 0)
  assert.equal(result.error?.kind, 'aborted')
})

test('a GET whose refresh becomes stale cannot clear a newer session', async (t) => {
  for (const refreshStatus of [200, 401]) {
    await t.test(`refresh returns ${refreshStatus}`, async () => {
      let token = 'token-a'
      let refreshed = 0
      let unauthorized = 0
      let releaseRefresh
      const refreshGate = new Promise((resolve) => { releaseRefresh = resolve })
      const client = createApiClient(clientOptions({
        getToken: () => token,
        onTokenRefreshed: (nextToken) => {
          refreshed += 1
          token = nextToken
        },
        onUnauthorized: () => { unauthorized += 1 },
        fetch: async (input) => {
          if (!String(input).endsWith('/auth/token/refresh')) {
            return response(401, { code: 100401, data: {} })
          }
          await refreshGate
          return refreshStatus === 200
            ? response(200, payload({ token: 'stale-refreshed-token' }))
            : response(401, { code: 100401, data: {} })
        },
      }))

      const pending = client.request('/v1/runs', { method: 'GET' })
      await new Promise((resolve) => setImmediate(resolve))
      token = 'token-b'
      releaseRefresh()
      const result = await pending

      assert.equal(token, 'token-b')
      assert.equal(refreshed, 0)
      assert.equal(unauthorized, 0)
      assert.equal(result.error?.kind, 'aborted')
    })
  }
})

test('failed refresh exits the session and does not loop', async () => {
  let unauthorized = 0
  let requests = 0
  const client = createApiClient(clientOptions({
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async (input) => {
      requests += 1
      return String(input).endsWith('/auth/token/refresh')
        ? response(401, { code: 100401, data: {} })
        : response(401, { code: 100401, data: {} })
    },
  }))

  const result = await client.request('/v1/runs', { method: 'GET' })

  assert.equal(requests, 2)
  assert.equal(unauthorized, 1)
  assert.equal(result.ok, false)
  assert.equal(result.error?.kind, 'unauthorized')
})

test('temporary refresh failures preserve the session and surface the service failure', async () => {
  let unauthorized = 0
  let refreshes = 0
  const client = createApiClient(clientOptions({
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async (input) => {
      if (String(input).endsWith('/auth/token/refresh')) {
        refreshes += 1
        return response(503, { code: 100503, data: {} })
      }
      return response(401, { code: 100401, data: {} })
    },
  }))

  const result = await client.request('/v1/runs', { method: 'GET' })

  assert.equal(refreshes, 1)
  assert.equal(unauthorized, 0)
  assert.equal(result.status, 503)
  assert.equal(result.error?.kind, 'server')
})

test('explicit refresh distinguishes rejected credentials from temporary outages', async () => {
  let nextResponse = response(503, { code: 100503, data: {} })
  let refreshed = 0
  let unauthorized = 0
  const client = createApiClient(clientOptions({
    onTokenRefreshed: () => { refreshed += 1 },
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async () => nextResponse,
  }))

  const unavailable = await client.refresh()
  assert.equal(unavailable.kind, 'transient')
  assert.equal(unavailable.response.status, 503)
  assert.equal(unavailable.response.error?.kind, 'server')
  assert.equal(refreshed, 0)
  assert.equal(unauthorized, 0)

  nextResponse = response(401, { code: 100401, data: {} })
  const rejected = await client.refresh()
  assert.deepEqual(rejected, { kind: 'unauthorized' })
  assert.equal(refreshed, 0)
  assert.equal(unauthorized, 0)
})

test('unauthorized POST is not refreshed or replayed', async () => {
  let calls = 0
  let refreshes = 0
  let unauthorized = 0
  const client = createApiClient(clientOptions({
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async (input) => {
      calls += 1
      if (String(input).endsWith('/auth/token/refresh')) refreshes += 1
      return response(401, { code: 100401, data: {} })
    },
  }))

  const result = await client.request('/v1/sources', { method: 'POST', body: { name: 'source' } })

  assert.equal(calls, 1)
  assert.equal(refreshes, 0)
  assert.equal(unauthorized, 1)
  assert.equal(result.error?.kind, 'unauthorized')
})

test('classifies public failures without exposing credentials', async (t) => {
  const cases = [
    ['forbidden', response(403, { code: 100403, data: { token: 'backend-secret' } })],
    ['rate_limited', response(429, { code: 429, data: { authorization: 'backend-secret' } }, { 'Retry-After': '12' })],
    ['server', response(500, { code: 500, data: { token: 'backend-secret' } })],
  ]

  for (const [expectedKind, nextResponse] of cases) {
    await t.test(String(expectedKind), async () => {
      const client = createApiClient(clientOptions({ fetch: async () => nextResponse }))
      const result = await client.request('/v1/protected', { method: 'GET', retry: false })
      const serialized = JSON.stringify(result)

      assert.equal(result.ok, false)
      assert.equal(result.error?.kind, expectedKind)
      assert.doesNotMatch(serialized, /secret-token-that-must-not-leak|backend-secret/i)
      if (expectedKind === 'rate_limited') assert.equal(result.error?.retryAfterSeconds, 12)
    })
  }
})

test('uses the backend safe validation message for 4xx responses', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(422, { code: 100422, msg: '请选择 Oracle 结果表', data: null }),
  }))

  const result = await client.request('/v1/reports', { method: 'POST', body: {}, acceptSafeErrorMessage: true })

  assert.equal(result.ok, false)
  assert.equal(result.status, 422)
  assert.equal(result.error?.message, '请选择 Oracle 结果表')
})

test('does not trust backend validation messages unless the endpoint opts in', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(422, { code: 100422, msg: 'raw provider detail', data: null }),
  }))

  const result = await client.request('/v1/legacy', { method: 'POST', body: {} })

  assert.equal(result.error?.message, '请求未能完成，请检查输入后重试。')
  assert.doesNotMatch(JSON.stringify(result), /provider detail/)
})

test('does not expose backend messages from 5xx responses', async () => {
  const client = createApiClient(clientOptions({
    fetch: async () => response(500, { code: 100500, msg: 'database password=secret', data: null }),
  }))

  const result = await client.request('/v1/reports', { method: 'POST', body: {} })

  assert.equal(result.error?.message, '服务暂时不可用，请稍后重试。')
  assert.doesNotMatch(JSON.stringify(result), /password|secret/i)
})

test('classifies network and caller cancellation failures', async () => {
  const networkClient = createApiClient(clientOptions({
    fetch: async () => { throw new TypeError('connection reset') },
  }))
  const network = await networkClient.request('/v1/runs', { method: 'GET', retry: false })
  assert.equal(network.error?.kind, 'network')
  assert.doesNotMatch(JSON.stringify(network), /secret-token-that-must-not-leak/i)

  const controller = new AbortController()
  controller.abort()
  const cancelledClient = createApiClient(clientOptions({
    fetch: async () => { throw new DOMException('aborted', 'AbortError') },
  }))
  const cancelled = await cancelledClient.request('/v1/runs', { method: 'GET', signal: controller.signal })
  assert.equal(cancelled.error?.kind, 'aborted')
  assert.doesNotMatch(JSON.stringify(cancelled), /secret-token-that-must-not-leak/i)
})

test('GET retries only network failures and 502, 503, or 504 responses', async (t) => {
  const cases = [
    { name: 'network', first: () => { throw new TypeError('connection reset') }, retries: true },
    { name: 'bad gateway', first: () => response(502, { code: 100502, data: {} }), retries: true },
    { name: 'service unavailable', first: () => response(503, { code: 100503, data: {} }), retries: true },
    { name: 'gateway timeout', first: () => response(504, { code: 100504, data: {} }), retries: true },
    { name: 'internal error', first: () => response(500, { code: 100500, data: {} }), retries: false },
    { name: 'not implemented', first: () => response(501, { code: 501, data: {} }), retries: false },
  ]

  for (const entry of cases) {
    await t.test(entry.name, async () => {
      let calls = 0
      const client = createApiClient(clientOptions({
        maxGetRetries: 1,
        random: () => 0.5,
        fetch: async () => {
          calls += 1
          return calls === 1 ? entry.first() : response(200, payload({ records: [] }))
        },
      }))

      const result = await client.request('/v1/runs', { method: 'GET' })

      assert.equal(calls, entry.retries ? 2 : 1)
      assert.equal(result.ok, entry.retries)
    })
  }
})

test('GET uses Retry-After with deterministic jitter injection', async () => {
  const waits = []
  let calls = 0
  const client = createApiClient(clientOptions({
    maxGetRetries: 1,
    random: () => 0,
    wait: async (milliseconds) => { waits.push(milliseconds) },
    fetch: async () => {
      calls += 1
      return calls === 1
        ? response(503, { code: 100503, data: {} }, { 'Retry-After': '2' })
        : response(200, payload({ records: [] }))
    },
  }))

  const result = await client.request('/v1/runs', { method: 'GET' })

  assert.equal(result.ok, true)
  assert.deepEqual(waits, [2_000])
})

test('GET retries a timeout without refreshing or clearing the session', async () => {
  let calls = 0
  let refreshes = 0
  let unauthorized = 0
  const client = createApiClient(clientOptions({
    maxGetRetries: 1,
    timeoutMs: 5,
    random: () => 0.5,
    onUnauthorized: () => { unauthorized += 1 },
    fetch: async (input, init) => {
      if (String(input).endsWith('/auth/token/refresh')) refreshes += 1
      calls += 1
      if (calls > 1) return response(200, payload({ records: [] }))
      await new Promise((_, reject) => {
        init.signal.addEventListener('abort', () => reject(init.signal.reason), { once: true })
      })
    },
  }))

  const result = await client.request('/v1/runs', { method: 'GET' })

  assert.equal(result.ok, true)
  assert.equal(calls, 2)
  assert.equal(refreshes, 0)
  assert.equal(unauthorized, 0)
})

test('cancels GET retry backoff without issuing a second request', async () => {
  const controller = new AbortController()
  let calls = 0
  let startedBackoff
  const backoffStarted = new Promise((resolve) => { startedBackoff = resolve })
  const client = createApiClient(clientOptions({
    maxGetRetries: 1,
    fetch: async () => {
      calls += 1
      return response(503, { code: 503, data: {} })
    },
    wait: async () => {
      startedBackoff()
      await new Promise(() => {})
    },
  }))

  const pending = client.request('/v1/runs', { method: 'GET', signal: controller.signal })
  await backoffStarted
  controller.abort()
  const result = await pending

  assert.equal(calls, 1)
  assert.equal(result.ok, false)
  assert.equal(result.error?.kind, 'aborted')
})
