import { test } from 'node:test'
import assert from 'node:assert/strict'
import { dayNumber, isoFromDay, monthBands, shiftDates, taskSpan, timelineWindow, weekday } from './timeline.ts'

test('dayNumber round-trips and rejects bad dates', () => {
  const d = dayNumber('2026-03-01')!
  assert.equal(isoFromDay(d), '2026-03-01')
  assert.equal(dayNumber('2026-02-28')! + 1, d)
  assert.equal(dayNumber('2026-02-30'), null)
  assert.equal(dayNumber(''), null)
  assert.equal(dayNumber('nope'), null)
})

test('taskSpan handles one or both dates', () => {
  assert.deepEqual(taskSpan(null, null), null)
  const due = dayNumber('2026-05-10')!
  assert.deepEqual(taskSpan(null, '2026-05-10'), { start: due, end: due, milestone: true })
  assert.deepEqual(taskSpan('2026-05-10', null), { start: due, end: due, milestone: false })
  assert.deepEqual(taskSpan('2026-05-08', '2026-05-10'), { start: due - 2, end: due, milestone: false })
})

test('timelineWindow pads, includes today, and has a minimum length', () => {
  const today = dayNumber('2026-05-01')!
  const w = timelineWindow([{ start: today + 10, end: today + 12, milestone: false }], today)
  assert.equal(w.start, today - 3)
  assert.ok(w.end >= today + 19)
  assert.equal(w.end - w.start + 1, 42)
  const long = timelineWindow([{ start: today - 100, end: today + 100, milestone: false }], today)
  assert.equal(long.start, today - 103)
  assert.equal(long.end, today + 107)
})

test('shiftDates moves set dates and keeps unset ones null', () => {
  assert.deepEqual(shiftDates('2026-05-30', '2026-06-02', 3), { start_date: '2026-06-02', due_date: '2026-06-05' })
  assert.deepEqual(shiftDates(null, '2026-01-01', -1), { start_date: null, due_date: '2025-12-31' })
})

test('monthBands split at month boundaries', () => {
  const bands = monthBands(dayNumber('2026-01-30')!, dayNumber('2026-03-02')!)
  assert.deepEqual(
    bands.map((b) => b.days),
    [2, 28, 2],
  )
})

test('weekday', () => {
  assert.equal(weekday(dayNumber('2026-11-11')!), 3)
})
