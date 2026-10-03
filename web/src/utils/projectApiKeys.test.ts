import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { expiresAtFromPreset, keyExpiryLabel, scopeLabel } from './projectApiKeys.ts'

const now = new Date('2026-10-01T12:00:00.000Z')

describe('expiresAtFromPreset', () => {
  it('returns null for never', () => {
    assert.equal(expiresAtFromPreset('never', now), null)
  })
  it('adds whole days', () => {
    assert.equal(expiresAtFromPreset('30', now), '2026-10-31T12:00:00.000Z')
    assert.equal(expiresAtFromPreset('365', now), '2027-10-01T12:00:00.000Z')
  })
})

describe('keyExpiryLabel', () => {
  it('describes missing, past and upcoming expiry', () => {
    assert.equal(keyExpiryLabel(null, now), 'Never')
    assert.equal(keyExpiryLabel('2026-09-30T12:00:00Z', now), 'Expired')
    assert.equal(keyExpiryLabel('2026-10-01T18:00:00Z', now), 'Expires today')
    assert.equal(keyExpiryLabel('2026-10-11T12:00:00Z', now), 'In 10 days')
  })
})

describe('scopeLabel', () => {
  it('labels known scopes and passes others through', () => {
    assert.equal(scopeLabel('tasks:read'), 'Read tasks')
    assert.equal(scopeLabel('other:thing'), 'other:thing')
  })
})
