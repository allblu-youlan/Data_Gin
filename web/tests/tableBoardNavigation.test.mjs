import assert from 'node:assert/strict'
import test from 'node:test'
import { canViewNavigationItem } from '../.test-dist/appShell/navigationPermissions.js'

test('table board requires the complete console permission set', () => {
  const permissions = ['report.read', 'report.manage', 'table_board.use']
  assert.equal(canViewNavigationItem('table_board', permissions), true)
  for (const removed of permissions) assert.equal(canViewNavigationItem('table_board', permissions.filter(p => p !== removed)), false)
})
