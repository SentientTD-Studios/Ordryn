import { test } from 'node:test'
import assert from 'node:assert/strict'
import { addDays, dayLabel, nextMonday, snoozeOptions } from './planner.ts'

test('addDays crosses month and year ends', () => {
  assert.equal(addDays('2026-01-31', 1), '2026-02-01')
  assert.equal(addDays('2026-12-31', 1), '2027-01-01')
  assert.equal(addDays('2026-03-01', -1), '2026-02-28')
})

test('nextMonday is always in the future', () => {
  assert.equal(nextMonday('2026-11-11'), '2026-11-16') // Wednesday
  assert.equal(nextMonday('2026-11-16'), '2026-11-23') // Monday -> next week
  assert.equal(nextMonday('2026-11-15'), '2026-11-16') // Sunday
})

test('snoozeOptions are all after today', () => {
  for (const o of snoozeOptions('2026-11-11')) assert.ok(o.until > '2026-11-11', o.label)
})

test('dayLabel names today and tomorrow', () => {
  assert.equal(dayLabel('2026-11-11', '2026-11-11'), 'Today')
  assert.equal(dayLabel('2026-11-12', '2026-11-11'), 'Tomorrow')
  assert.match(dayLabel('2026-11-14', '2026-11-11'), /14/)
})
