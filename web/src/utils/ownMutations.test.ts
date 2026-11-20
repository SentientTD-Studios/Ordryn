import { describe, it, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import {
  OWN_MUTATION_WINDOW_MS,
  beginOwnMutation,
  hasRecentOwnMutation,
  isMutatingMethod,
  resetOwnMutations,
} from './ownMutations.ts'

describe('ownMutations', () => {
  beforeEach(() => resetOwnMutations())

  it('treats only writes as mutations', () => {
    assert.equal(isMutatingMethod(undefined), false)
    assert.equal(isMutatingMethod('get'), false)
    assert.equal(isMutatingMethod('HEAD'), false)
    assert.equal(isMutatingMethod('POST'), true)
    assert.equal(isMutatingMethod('patch'), true)
    assert.equal(isMutatingMethod('DELETE'), true)
  })

  it('is not recent when this tab has written nothing', () => {
    assert.equal(hasRecentOwnMutation(), false)
  })

  it('counts a write in flight, then for a short window after it settles', () => {
    const settle = beginOwnMutation()
    assert.equal(hasRecentOwnMutation(Date.now() + 60_000), true, 'in flight')
    settle()
    const settledAt = Date.now()
    assert.equal(hasRecentOwnMutation(settledAt + 100), true)
    assert.equal(hasRecentOwnMutation(settledAt + OWN_MUTATION_WINDOW_MS + 100), false)
  })

  it('ignores a second settle call', () => {
    const a = beginOwnMutation()
    const b = beginOwnMutation()
    a()
    a()
    assert.equal(hasRecentOwnMutation(Date.now() + 60_000), true, 'b still in flight')
    b()
    assert.equal(hasRecentOwnMutation(Date.now() + 60_000), false)
  })
})
