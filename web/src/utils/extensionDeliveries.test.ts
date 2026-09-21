import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { rateLimitNotice } from './extensionDeliveries.ts'

describe('rateLimitNotice', () => {
  it('prefers a last_error that mentions rate limiting', () => {
    assert.equal(
      rateLimitNotice('Destination is rate-limited. Next retry in 5 seconds.'),
      'Destination is rate-limited. Next retry in 5 seconds.',
    )
  })

  it('surfaces a 429 delivery even without last_error', () => {
    const notice = rateLimitNotice('', [
      {
        id: 1,
        event: 'task.status_changed',
        status: 'pending',
        http_code: 429,
        attempts: 1,
        created_at: '2026-09-21T16:00:00Z',
        next_attempt_at: '2026-09-21T16:00:07Z',
      },
    ])
    assert.match(notice || '', /rate-limited/)
    assert.match(notice || '', /2026-09-21T16:00:07Z/)
  })

  it('returns null for ordinary delivery errors', () => {
    assert.equal(rateLimitNotice('webhook HTTP 400: bad request'), null)
    assert.equal(
      rateLimitNotice('', [{ id: 2, event: 'task.updated', status: 'sent', attempts: 1, created_at: '2026-09-21T16:00:00Z' }]),
      null,
    )
  })
})
