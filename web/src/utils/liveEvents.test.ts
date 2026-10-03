import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { LIVE_RESYNC, batchTouchesDiscussion, batchTouchesTask, coalesceLiveEvents } from './liveEvents.ts'

describe('coalesceLiveEvents', () => {
  it('keeps one of each kind so a comment is not hidden by a later update', () => {
    const burst = [
      { type: 'task.commented', task_id: 5 },
      { type: 'task.updated', task_id: 5 },
      { type: 'task.updated', task_id: 5 },
      { type: 'task.updated', task_id: 9 },
    ]
    assert.deepEqual(coalesceLiveEvents(burst), [
      { type: 'task.commented', task_id: 5 },
      { type: 'task.updated', task_id: 5 },
      { type: 'task.updated', task_id: 9 },
    ])
  })
  it('returns an empty list for no events', () => {
    assert.deepEqual(coalesceLiveEvents([]), [])
  })
})

describe('batch helpers', () => {
  const burst = [
    { type: 'task.commented', task_id: 5 },
    { type: 'task.updated', task_id: 7 },
  ]
  it('detects comments on the open task even when another event came last', () => {
    assert.equal(batchTouchesDiscussion(burst, 5), true)
    assert.equal(batchTouchesDiscussion(burst, 7), false)
  })
  it('treats a resync as touching everything', () => {
    assert.equal(batchTouchesDiscussion([{ type: LIVE_RESYNC }], 3), true)
    assert.equal(batchTouchesTask([{ type: LIVE_RESYNC }], 3), true)
  })
  it('matches task events by id', () => {
    assert.equal(batchTouchesTask(burst, 7), true)
    assert.equal(batchTouchesTask(burst, 8), false)
  })
})
