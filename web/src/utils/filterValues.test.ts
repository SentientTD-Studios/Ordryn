import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { dueRangeLabel, filterHas, filterSummary, parseFilterList, priorityFilterLabel, toggleFilterValue } from './filterValues.ts'

describe('filter values', () => {
  it('parses lists, dropping blanks and repeats', () => {
    assert.deepEqual(parseFilterList(' work, ,Work,urgent '), ['work', 'urgent'])
    assert.deepEqual(parseFilterList(''), [])
  })

  it('toggles entries case-insensitively', () => {
    assert.equal(toggleFilterValue('', '3'), '3')
    assert.equal(toggleFilterValue('3', '2'), '3,2')
    assert.equal(toggleFilterValue('work,Urgent', 'urgent'), 'work')
    assert.ok(filterHas('work,urgent', 'URGENT'))
    assert.ok(!filterHas('work', 'wor'))
  })

  it('labels selections', () => {
    assert.equal(filterSummary([]), 'ALL')
    assert.equal(filterSummary(['bug']), 'bug')
    assert.equal(filterSummary(['bug', 'ui']), '2 selected')
    assert.equal(priorityFilterLabel('2,3'), 'HIGH + MED')
    assert.equal(priorityFilterLabel(''), 'ALL')
  })

  it('labels due ranges', () => {
    assert.equal(dueRangeLabel('', ''), 'CUSTOM')
    assert.match(dueRangeLabel('2026-10-01', ''), /^FROM /)
    assert.match(dueRangeLabel('', '2026-10-14'), /^UNTIL /)
    assert.match(dueRangeLabel('2026-10-01', '2026-10-14'), / – /)
  })
})
