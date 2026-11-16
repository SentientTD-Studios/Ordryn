import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  addDays,
  daySegments,
  formatClock,
  formatDuration,
  minutesOnDay,
  snapMinutes,
  tzOffsetMinutes,
  weekStart,
  zonedDay,
  zonedMinutes,
  zonedToInstant,
} from './timeBlocks.ts'

describe('day arithmetic', () => {
  it('adds days across months and years', () => {
    assert.equal(addDays('2026-12-31', 1), '2027-01-01')
    assert.equal(addDays('2026-03-01', -1), '2026-02-28')
  })
  it('starts weeks on Sunday', () => {
    assert.equal(weekStart('2026-11-16'), '2026-11-15') // Monday → Sunday
    assert.equal(weekStart('2026-11-15'), '2026-11-15')
  })
})

describe('time zones', () => {
  it('maps wall clock to instants and back', () => {
    const t = zonedToInstant('2026-11-16', 9 * 60 + 30, 'America/New_York')
    assert.equal(t.toISOString(), '2026-11-16T14:30:00.000Z')
    assert.equal(zonedDay(t, 'America/New_York'), '2026-11-16')
    assert.equal(zonedMinutes(t, 'America/New_York'), 570)
    assert.equal(tzOffsetMinutes(t, 'America/New_York'), -300)
  })
  it('handles half-hour zones and DST days', () => {
    assert.equal(zonedToInstant('2026-06-01', 0, 'Asia/Kolkata').toISOString(), '2026-05-31T18:30:00.000Z')
    // US spring forward 2026-03-08: 1 AM is EST, 3 AM is EDT.
    assert.equal(zonedToInstant('2026-03-08', 60, 'America/New_York').toISOString(), '2026-03-08T06:00:00.000Z')
    assert.equal(zonedToInstant('2026-03-08', 180, 'America/New_York').toISOString(), '2026-03-08T07:00:00.000Z')
  })
})

describe('daySegments', () => {
  const tz = 'UTC'
  const b = (id: number, start: string, end: string) => ({ id, start_at: start, end_at: end })

  it('places blocks by minute and splits overlaps into lanes', () => {
    const segs = daySegments(
      [
        b(1, '2026-11-16T09:00:00Z', '2026-11-16T11:00:00Z'),
        b(2, '2026-11-16T10:00:00Z', '2026-11-16T10:30:00Z'),
        b(3, '2026-11-16T13:00:00Z', '2026-11-16T14:00:00Z'),
      ],
      '2026-11-16',
      tz,
    )
    assert.deepEqual(
      segs.map((s) => [s.block.id, s.startMin, s.endMin, s.lane, s.lanes]),
      [
        [1, 540, 660, 0, 2],
        [2, 600, 630, 1, 2],
        [3, 780, 840, 0, 1],
      ],
    )
  })

  it('splits a block across midnight', () => {
    const blocks = [b(1, '2026-11-16T22:00:00Z', '2026-11-17T02:00:00Z')]
    const first = daySegments(blocks, '2026-11-16', tz)[0]
    const second = daySegments(blocks, '2026-11-17', tz)[0]
    assert.deepEqual([first.startMin, first.endMin, first.continuesAfter], [1320, 1440, true])
    assert.deepEqual([second.startMin, second.endMin, second.continuesBefore], [0, 120, true])
    assert.equal(minutesOnDay(blocks, '2026-11-16', tz), 120)
    assert.equal(daySegments(blocks, '2026-11-18', tz).length, 0)
  })
})

describe('formatting', () => {
  it('formats durations and clock times', () => {
    assert.equal(formatDuration(150), '2h 30m')
    assert.equal(formatDuration(45), '45m')
    assert.equal(formatDuration(0), '0m')
    assert.equal(formatClock(0), '12 AM')
    assert.equal(formatClock(12 * 60), '12 PM')
    assert.equal(formatClock(15 * 60 + 30), '3:30 PM')
  })
  it('snaps to 15 minutes', () => {
    assert.equal(snapMinutes(52), 45)
    assert.equal(snapMinutes(53), 60)
  })
})
