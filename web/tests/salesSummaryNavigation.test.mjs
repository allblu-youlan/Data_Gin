import assert from 'node:assert/strict'
import test from 'node:test'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'

test('sales summary follows the existing business overview read permission', () => {
  assert.equal(canViewNavigationItem('sales_summary', []), false)
  assert.equal(canViewNavigationItem('sales_summary', ['mall.read']), false)
  assert.equal(canViewNavigationItem('sales_summary', ['business_overview.read']), true)
})
