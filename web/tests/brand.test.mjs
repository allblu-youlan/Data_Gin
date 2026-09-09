import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import test from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { createElement } from 'react'

import { Brand } from '../.test-dist/Brand.js'

test('renders the accessible Allblu wordmark', () => {
  const markup = renderToStaticMarkup(createElement(Brand, { size: 'large' }))

  assert.match(markup, /src="\/allblu-logo-9772212d\.jpg"/)
  assert.match(markup, /alt="Allblu Logo"/)
  assert.equal((markup.match(/Allblu/g) ?? []).length, 1)
  assert.equal(existsSync(new URL('../public/allblu-logo-9772212d.jpg', import.meta.url)), true)
})
