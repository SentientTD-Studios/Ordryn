import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  applyFeedbackVote,
  countFeedbackByStatus,
  feedbackStatusBadge,
  feedbackStatusLabel,
  filterFeedbackByStatus,
} from './feedbackStatus.ts'

describe('feedback status helpers', () => {
  it('labels known statuses and falls back to under review', () => {
    assert.equal(feedbackStatusLabel('planned'), 'Planned')
    assert.equal(feedbackStatusLabel('done'), 'Done')
    assert.equal(feedbackStatusLabel('mystery'), 'Under review')
    assert.equal(feedbackStatusBadge('mystery'), feedbackStatusBadge('under_review'))
  })

  it('filters and counts by status', () => {
    const posts = [
      { id: 1, status: 'planned' },
      { id: 2, status: 'done' },
      { id: 3, status: 'planned' },
      { id: 4, status: 'weird' },
    ]
    assert.deepEqual(filterFeedbackByStatus(posts, '').map((p) => p.id), [1, 2, 3, 4])
    assert.deepEqual(filterFeedbackByStatus(posts, 'planned').map((p) => p.id), [1, 3])
    assert.deepEqual(countFeedbackByStatus(posts), { under_review: 1, planned: 2, in_progress: 0, done: 1 })
  })

  it('applies a vote result to one post only', () => {
    const posts = [
      { id: 1, vote_count: 2, voted: false },
      { id: 2, vote_count: 5, voted: true },
    ]
    const next = applyFeedbackVote(posts, 1, { vote_count: 3, voted: true })
    assert.deepEqual(next[0], { id: 1, vote_count: 3, voted: true })
    assert.equal(next[1], posts[1])
    assert.equal(posts[0].vote_count, 2, 'input is not mutated')
  })
})
