import assert from 'node:assert/strict'
import test from 'node:test'
import { parseQuickAdd } from './quickAdd.ts'

// Wednesday, 14 October 2026
const today = new Date(2026, 9, 14)
const tags = [
  { id: 1, name: 'bug' },
  { id: 2, name: 'Frontend' },
]
const projects = [
  { id: 10, name: 'Website Redesign' },
  { id: 11, name: 'Mobile' },
  { id: 12, name: 'Mobile Backend' },
]

test('plain text is just a title', () => {
  const r = parseQuickAdd('  Write the   report ', { today })
  assert.equal(r.title, 'Write the report')
  assert.equal(r.dueDate, undefined)
  assert.equal(r.priority, undefined)
  assert.deepEqual(r.tagIds, [])
  assert.deepEqual(r.chips, [])
})

test('tags, priority and a date come out of the title', () => {
  const r = parseQuickAdd('Fix login #bug !high tomorrow', { today, tags })
  assert.equal(r.title, 'Fix login')
  assert.equal(r.dueDate, '2026-10-15')
  assert.equal(r.priority, 3)
  assert.deepEqual(r.tagIds, [1])
  assert.deepEqual(
    r.chips.map((c) => c.kind),
    ['due', 'tag', 'priority'],
  )
})

test('tag names match case-insensitively and unknown tags stay in the title', () => {
  const r = parseQuickAdd('Style #frontend #nope', { today, tags })
  assert.equal(r.title, 'Style #nope')
  assert.deepEqual(r.tagIds, [2])
})

test('priority spellings', () => {
  assert.equal(parseQuickAdd('a !low', { today }).priority, 1)
  assert.equal(parseQuickAdd('a !m', { today }).priority, 2)
  assert.equal(parseQuickAdd('a !!!', { today }).priority, 3)
  assert.equal(parseQuickAdd('a !2', { today }).priority, 2)
  const r = parseQuickAdd('a !urgent', { today })
  assert.equal(r.priority, undefined)
  assert.equal(r.title, 'a !urgent')
})

test('relative dates', () => {
  const due = (s: string) => parseQuickAdd(`Task ${s}`, { today }).dueDate
  assert.equal(due('today'), '2026-10-14')
  assert.equal(due('friday'), '2026-10-16')
  assert.equal(due('wednesday'), '2026-10-14')
  assert.equal(due('next week'), '2026-10-19')
  assert.equal(due('next friday'), '2026-10-23')
  assert.equal(due('next month'), '2026-11-01')
  assert.equal(due('in 3 days'), '2026-10-17')
  assert.equal(due('in 2 weeks'), '2026-10-28')
  assert.equal(due('in a month'), '2026-11-14')
})

test('absolute dates roll into next year once passed', () => {
  const due = (s: string) => parseQuickAdd(`Task ${s}`, { today }).dueDate
  assert.equal(due('2026-12-01'), '2026-12-01')
  assert.equal(due('oct 20'), '2026-10-20')
  assert.equal(due('20th October'), '2026-10-20')
  assert.equal(due('mar 3'), '2027-03-03')
  assert.equal(due('mar 3 2028'), '2028-03-03')
  assert.equal(due('feb 30'), undefined)
})

test('trigger words are eaten with the date, and short weekdays need one', () => {
  const r = parseQuickAdd('Call Sam on fri', { today })
  assert.equal(r.title, 'Call Sam')
  assert.equal(r.dueDate, '2026-10-16')
  const sun = parseQuickAdd('Replace sun icon', { today })
  assert.equal(sun.title, 'Replace sun icon')
  assert.equal(sun.dueDate, undefined)
  const due = parseQuickAdd('Report due monday.', { today })
  assert.equal(due.title, 'Report')
  assert.equal(due.dueDate, '2026-10-19')
})

test('only the first date is used', () => {
  const r = parseQuickAdd('Move meeting from today to friday', { today })
  assert.equal(r.dueDate, '2026-10-14')
  assert.equal(r.title, 'Move meeting from to friday')
})

test('projects by exact or unique prefix match', () => {
  assert.equal(parseQuickAdd('a +website', { today, projects }).projectId, 10)
  assert.equal(parseQuickAdd('a +website-redesign', { today, projects }).projectId, 10)
  assert.equal(parseQuickAdd('a +mobile', { today, projects }).projectId, 11)
  assert.equal(parseQuickAdd('a +mobileb', { today, projects }).projectId, 12)
  const r = parseQuickAdd('a +mob', { today, projects })
  assert.equal(r.projectId, undefined)
  assert.equal(r.title, 'a +mob')
  assert.equal(parseQuickAdd('a +website', { today }).projectId, undefined)
})

test('@me only when claiming is allowed', () => {
  assert.equal(parseQuickAdd('a @me', { today, allowClaim: true }).claim, true)
  const r = parseQuickAdd('a @me', { today })
  assert.equal(r.claim, false)
  assert.equal(r.title, 'a @me')
})

test('a title made only of keywords is kept literally', () => {
  const r = parseQuickAdd('Today', { today })
  assert.equal(r.title, 'Today')
  assert.equal(r.dueDate, undefined)
  assert.deepEqual(r.chips, [])
})
