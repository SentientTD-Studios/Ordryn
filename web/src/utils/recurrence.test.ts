import assert from 'node:assert/strict'
import test from 'node:test'
import {
  draftFromRecurrence,
  emptyRecurrenceDraft,
  recurrencePayload,
  recurrenceSummary,
  sameRecurrenceDraft,
} from './recurrence.ts'

test('disabled draft has no payload or summary', () => {
  const d = emptyRecurrenceDraft()
  assert.equal(recurrencePayload(d), null)
  assert.equal(recurrenceSummary(d), '')
})

test('payload includes only fields relevant to the frequency and end mode', () => {
  const d = { ...emptyRecurrenceDraft(), enabled: true, frequency: 'weekly' as const, interval: 2, weekdays: [4, 1], monthDay: 12 }
  assert.deepEqual(recurrencePayload(d), { frequency: 'weekly', interval: 2, basis: 'due', weekdays: [1, 4] })
  const m = { ...d, frequency: 'monthly' as const, endMode: 'after' as const, endAfter: 3 }
  assert.deepEqual(recurrencePayload(m), { frequency: 'monthly', interval: 2, basis: 'due', month_day: 12, end_after: 3 })
  const e = { ...d, endMode: 'on' as const, endsOn: '2027-01-01', basis: 'completion' as const }
  assert.deepEqual(recurrencePayload(e), { frequency: 'weekly', interval: 2, basis: 'completion', weekdays: [1, 4], ends_on: '2027-01-01' })
})

test('round-trips a server rule', () => {
  const d = draftFromRecurrence({
    frequency: 'monthly', interval: 1, weekdays: [], month_day: 31, basis: 'due',
    ends_on: null, end_after: 4, occurrence: 2, series_id: 9, summary: 'Monthly on the last day, 4 times',
  })
  assert.equal(d.enabled, true)
  assert.equal(d.endMode, 'after')
  assert.equal(recurrenceSummary(d), 'Monthly on the last day, 4 times')
  assert.ok(sameRecurrenceDraft(d, { ...d, weekdays: [3] }), 'hidden weekday field does not make monthly dirty')
  assert.ok(!sameRecurrenceDraft(d, { ...d, interval: 2 }))
})

test('summaries match the server wording', () => {
  const base = { ...emptyRecurrenceDraft(), enabled: true }
  assert.equal(recurrenceSummary({ ...base, frequency: 'daily' }), 'Daily')
  assert.equal(recurrenceSummary({ ...base, frequency: 'daily', interval: 3, basis: 'completion' }), 'Every 3 days after completion')
  assert.equal(recurrenceSummary({ ...base, frequency: 'weekly', interval: 2, weekdays: [4, 1] }), 'Every 2 weeks on Mon, Thu')
  assert.equal(recurrenceSummary({ ...base, frequency: 'monthly', monthDay: 22 }), 'Monthly on the 22nd')
  assert.equal(recurrenceSummary({ ...base, frequency: 'monthly', monthDay: 13 }), 'Monthly on the 13th')
  assert.equal(recurrenceSummary({ ...base, frequency: 'yearly', endMode: 'on', endsOn: '2030-01-01' }), 'Yearly until 2030-01-01')
})
