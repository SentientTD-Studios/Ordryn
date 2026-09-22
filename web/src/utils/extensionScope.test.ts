import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { settingHasScope, settingScopes } from './extensionScope.ts'

describe('settingScopes', () => {
  it('defaults missing scope to site', () => {
    assert.deepEqual(settingScopes({}), ['site'])
    assert.deepEqual(settingScopes({ scope: '' }), ['site'])
  })

  it('accepts a string or array and aliases member to user', () => {
    assert.deepEqual(settingScopes({ scope: 'project' }), ['project'])
    assert.deepEqual(settingScopes({ scope: ['project', 'kanban'] }), ['project', 'kanban'])
    assert.deepEqual(settingScopes({ scope: 'member' }), ['user'])
  })
})

describe('settingHasScope', () => {
  it('matches user and member interchangeably', () => {
    assert.equal(settingHasScope({ scope: 'user' }, 'member'), true)
    assert.equal(settingHasScope({ scope: 'member' }, 'user'), true)
    assert.equal(settingHasScope({ scope: ['project', 'kanban'] }, 'kanban'), true)
    assert.equal(settingHasScope({ scope: 'project' }, 'kanban'), false)
  })
})
