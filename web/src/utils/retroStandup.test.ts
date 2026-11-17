import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { RetroCard } from '../api/types.ts'
import { layoutRetroBoard, nextPhase, prevPhase, rankedCards, votesLeft } from './retroBoard.ts'
import { clockOffsetMs, formatClock, formatTimerSetting, speakerSecondsLeft } from './standupTimer.ts'

function card(over: Partial<RetroCard>): RetroCard {
  return {
    id: 1, column: 'a', body: 'x', author_id: 1, mine: false, hidden: false,
    parent_id: null, votes: 0, voted_by_me: false, ...over,
  }
}

const columns = [{ key: 'a', title: 'A' }, { key: 'b', title: 'B' }]

describe('layoutRetroBoard', () => {
  it('groups children under their top card and counts hidden cards per column', () => {
    const view = layoutRetroBoard(
      [
        card({ id: 1 }),
        card({ id: 2, parent_id: 1 }),
        card({ id: 3, column: 'b', hidden: true, body: undefined }),
        card({ id: 4, column: 'b' }),
      ],
      columns,
    )
    assert.equal(view[0].groups.length, 1)
    assert.deepEqual(view[0].groups[0].children.map((c) => c.id), [2])
    assert.equal(view[1].hiddenCount, 1)
    assert.deepEqual(view[1].groups.map((g) => g.top.id), [4])
  })

  it('shows a card at the top level when its group top is gone', () => {
    const view = layoutRetroBoard([card({ id: 5, parent_id: 99 })], columns)
    assert.deepEqual(view[0].groups.map((g) => g.top.id), [5])
  })
})

describe('rankedCards', () => {
  it('orders top-level cards by votes, keeping board order on ties', () => {
    const ranked = rankedCards([
      card({ id: 1, votes: 1 }),
      card({ id: 2, votes: 3 }),
      card({ id: 3, votes: 1 }),
      card({ id: 4, votes: 9, parent_id: 2 }),
    ])
    assert.deepEqual(ranked.map((c) => c.id), [2, 1, 3])
  })
})

describe('retro phases and votes', () => {
  it('steps through phases and reports votes left', () => {
    assert.equal(nextPhase('collect'), 'group')
    assert.equal(nextPhase('act'), null)
    assert.equal(prevPhase('collect'), null)
    assert.equal(prevPhase('vote'), 'group')
    assert.equal(votesLeft(5, 2), 3)
    assert.equal(votesLeft(2, 5), 0)
    assert.equal(votesLeft(0, 4), null)
  })
})

describe('standup timer', () => {
  it('counts down from the server start using the clock offset', () => {
    const offset = clockOffsetMs('2026-11-13T10:00:10Z', Date.parse('2026-11-13T10:00:00Z'))
    assert.equal(offset, 10_000)
    const left = speakerSecondsLeft('2026-11-13T10:00:00Z', 120, Date.parse('2026-11-13T10:00:20Z'), offset)
    assert.equal(left, 90)
    assert.equal(speakerSecondsLeft('2026-11-13T10:00:00Z', 0, 0, 0), null)
    assert.equal(speakerSecondsLeft('2026-11-13T10:00:00Z', 30, Date.parse('2026-11-13T10:01:00Z'), 0), -30)
  })

  it('formats the clock and the setting', () => {
    assert.equal(formatClock(75), '1:15')
    assert.equal(formatClock(-12), '-0:12')
    assert.equal(formatTimerSetting(0), 'Off')
    assert.equal(formatTimerSetting(90), '1m 30s')
    assert.equal(formatTimerSetting(120), '2m')
    assert.equal(formatTimerSetting(45), '45s')
  })
})
