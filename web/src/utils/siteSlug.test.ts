import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { siteKeyEnvVar, siteSlug } from './siteSlug.ts'

describe('siteSlug', () => {
  it('slugs the site name', () => {
    assert.equal(siteSlug('Acme Tasks'), 'acme-tasks')
    assert.equal(siteSlug('  Ordryn!  '), 'ordryn')
    assert.equal(siteSlug('a'.repeat(30)), 'a'.repeat(24))
  })
  it('falls back to ordryn', () => {
    assert.equal(siteSlug(''), 'ordryn')
    assert.equal(siteSlug(null), 'ordryn')
    assert.equal(siteSlug('日本'), 'ordryn')
  })
  it('builds the key environment variable', () => {
    assert.equal(siteKeyEnvVar('Acme Tasks'), 'ACME_TASKS_API_KEY')
    assert.equal(siteKeyEnvVar(), 'ORDRYN_API_KEY')
  })
})
