import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { matchScore, rankItems, taskNumber } from './commandPalette.ts'

describe('matchScore', () => {
  it('prefers prefixes, then word starts, then substrings, then letters in order', () => {
    const prefix = matchScore('New task', 'new')!
    const word = matchScore('Create new task', 'new')!
    const sub = matchScore('Renew', 'new')!
    const fuzzy = matchScore('New task', 'nwtsk')!
    assert.ok(prefix < word && word < sub && sub < fuzzy)
    assert.equal(matchScore('Calendar', 'xyz'), null)
  })
})

describe('rankItems', () => {
  const items = [
    { id: 'a', label: 'Go to calendar', group: 'Pages' },
    { id: 'b', label: 'New task', keywords: 'create add', group: 'Actions' },
    { id: 'c', label: 'Toggle theme', keywords: 'dark light', group: 'Actions' },
  ]
  it('keeps order for an empty query', () => {
    assert.deepEqual(rankItems(items, '').map((i) => i.id), ['a', 'b', 'c'])
  })
  it('matches keywords', () => {
    assert.deepEqual(rankItems(items, 'dark').map((i) => i.id), ['c'])
    assert.equal(rankItems(items, 'create')[0]!.id, 'b')
  })
})

describe('taskNumber', () => {
  it('reads #123 and 123', () => {
    assert.equal(taskNumber('#123'), 123)
    assert.equal(taskNumber(' 42 '), 42)
    assert.equal(taskNumber('12a'), null)
  })
})
