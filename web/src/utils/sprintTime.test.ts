import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { sprintTimeSummary } from './sprintTime.ts'

describe('sprintTimeSummary', () => {
  it('combines logged time and estimates', () => {
    assert.equal(sprintTimeSummary({ time_spent_minutes: 390, estimate_hours: 12 }), '6h 30m logged of 12h estimated')
    assert.equal(sprintTimeSummary({ time_spent_minutes: 120, estimate_hours: 0 }), '2h logged')
    assert.equal(sprintTimeSummary({ estimate_hours: 1.5 }), '1.5h estimated')
    assert.equal(sprintTimeSummary({}), '')
  })
})
