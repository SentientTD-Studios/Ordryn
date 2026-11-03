import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { personLabel } from './personLabel.ts'

describe('personLabel', () => {
  it('prefers the full name, then username, then email', () => {
    assert.equal(personLabel({ full_name: 'Emma Price', user_name: 'emma', email: 'e@x.com' }), 'Emma Price')
    assert.equal(personLabel({ full_name: '  ', user_name: 'emma', email: 'e@x.com' }), 'emma')
    assert.equal(personLabel({ user_name: '', email: 'e@x.com' }), 'e@x.com')
    assert.equal(personLabel({}), '')
  })
})
