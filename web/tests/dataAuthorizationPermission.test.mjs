import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createServer } from 'vite'

let server
let ChildPermissionRow

before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  ;({ ChildPermissionRow } = await server.ssrLoadModule('/src/DataAuthorizationPage.tsx'))
})

after(async () => { await server?.close() })

const permission = {
  permission: 'youzan.orders.read', label: '有赞订单查询', scope: '全模块数据',
  status: 'NOT_GRANTED', permanent: false, expiresAt: null,
  directGranted: false, directPermanent: false, directExpiresAt: null,
}

function renderPermission(overrides = {}) {
  return renderToStaticMarkup(createElement(ChildPermissionRow, {
    permission: { ...permission, ...overrides }, description: '查询有赞订单主表数据',
    inheritedLabel: '有赞', onGrant: () => {}, onRevoke: () => {},
  }))
}

test('ungranted leaf offers independent grant and its own API documentation', () => {
  const markup = renderPermission()
  assert.match(markup, /<button[^>]*>授权<\/button>/)
  assert.match(markup, /href="[^" ]*youzan\.orders\.read[^" ]*"/)
  assert.doesNotMatch(markup, /撤销独立授权/)
})

test('active independent leaf offers renewal and revocation', () => {
  const markup = renderPermission({
    status: 'ACTIVE', expiresAt: '2099-12-01T00:00:00+08:00',
    directGranted: true, directExpiresAt: '2099-12-01T00:00:00+08:00',
  })
  assert.match(markup, /<button[^>]*>续期<\/button>/)
  assert.match(markup, /撤销独立授权/)
  assert.match(markup, /2099/)
})

test('expired independent leaf can be granted again', () => {
  const markup = renderPermission({
    status: 'EXPIRED', expiresAt: '2000-11-01T00:00:00+08:00',
    directGranted: true, directExpiresAt: '2000-11-01T00:00:00+08:00',
  })
  assert.match(markup, /<button[^>]*>授权<\/button>/)
  assert.match(markup, /已过期/)
  assert.match(markup, /撤销独立授权/)
})

test('inherited leaf offers a separate grant without pretending it can revoke its parent', () => {
  const markup = renderPermission({ status: 'ACTIVE', inheritedFrom: 'youzan.read', permanent: true })
  assert.match(markup, /继承自 有赞/)
  assert.match(markup, /<button[^>]*>独立授权<\/button>/)
  assert.doesNotMatch(markup, /撤销独立授权/)
})

test('inherited leaf keeps its active independent grant visible and renewable', () => {
  const markup = renderPermission({
    status: 'ACTIVE', inheritedFrom: 'youzan.read', permanent: true,
    directGranted: true, directExpiresAt: '2099-12-01T01:30:00Z',
  })
  assert.match(markup, /继承自 有赞/)
  assert.match(markup, /独立授权有效期至 2099-12-01 09:30:00/)
  assert.match(markup, /有效期：永久有效/)
  assert.match(markup, /<button[^>]*>续期<\/button>/)
  assert.match(markup, /撤销独立授权/)
})

test('inherited leaf can revoke or replace its expired independent grant', () => {
  const markup = renderPermission({
    status: 'ACTIVE', inheritedFrom: 'youzan.read', permanent: true,
    directGranted: true, directExpiresAt: '2000-11-01T01:30:00Z',
  })
  assert.match(markup, /继承自 有赞/)
  assert.match(markup, /独立授权有效期至 2000-11-01 09:30:00/)
  assert.match(markup, /<button[^>]*>授权<\/button>/)
  assert.match(markup, /撤销独立授权/)
})

test('permanent independent leaf shows its own duration and can be renewed or revoked', () => {
  const markup = renderPermission({
    status: 'ACTIVE', permanent: true, directGranted: true, directPermanent: true,
  })
  assert.match(markup, /独立授权永久有效/)
  assert.match(markup, /<button[^>]*>续期<\/button>/)
  assert.match(markup, /撤销独立授权/)
})
