import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { ProjectSprint, RefinementSession } from '../api/types.ts'
import { parseTaskIdList, queueItemState, scopeOptions, sessionProgress, startInputFor } from './refinementSession.ts'

function session(over: Partial<RefinementSession> = {}): RefinementSession {
  return {
    id: 1, project_id: 1, kind: 'refinement', title: 'Grooming', status: 'active', facilitator_id: 1,
    sprint_id: null, revision: 3, created_at: '', updated_at: '', ended_at: null,
    queue: [10, 11, 12, 13], current: 2, current_task_id: 12,
    outcomes: { '10': 'refined', '11': 'skipped' }, tasks: [], can_facilitate: true, is_facilitator: true,
    ...over,
  }
}

describe('queueItemState', () => {
  it('marks current, outcomes, and pending tasks', () => {
    const s = session()
    assert.equal(queueItemState(s, 12), 'current')
    assert.equal(queueItemState(s, 10), 'refined')
    assert.equal(queueItemState(s, 11), 'skipped')
    assert.equal(queueItemState(s, 13), 'pending')
  })
})

describe('sessionProgress', () => {
  it('counts outcomes and detects the end of the queue', () => {
    assert.deepEqual(sessionProgress(session()), { total: 4, refined: 1, skipped: 1, remaining: 2, done: false })
    assert.equal(sessionProgress(session({ current: 4, current_task_id: null })).done, true)
  })
})

describe('startInputFor', () => {
  it('maps the scope to backlog or sprint_id and trims the title', () => {
    assert.deepEqual(startInputFor('any', { title: '  ', includeUnflagged: false }), { include_unflagged: false })
    assert.deepEqual(startInputFor('backlog', { title: ' Q4 ', includeUnflagged: true }), {
      include_unflagged: true, title: 'Q4', backlog: true,
    })
    assert.deepEqual(startInputFor(7, { title: '', includeUnflagged: false }), { include_unflagged: false, sprint_id: 7 })
  })
})

describe('scopeOptions', () => {
  it('lists any/backlog then sprints with the active one first', () => {
    const sprints = [
      { id: 1, name: 'S1', is_active: false },
      { id: 2, name: 'S2', is_active: true },
    ] as ProjectSprint[]
    assert.deepEqual(scopeOptions(sprints).map((o) => o.label), ['All open tasks', 'Backlog only', 'S2 (active)', 'S1'])
  })
})

describe('parseTaskIdList', () => {
  it('accepts commas, spaces and # prefixes, dropping junk and repeats', () => {
    assert.deepEqual(parseTaskIdList('12, #40 7 12 abc -3 0'), [12, 40, 7])
    assert.deepEqual(parseTaskIdList(''), [])
  })
})
