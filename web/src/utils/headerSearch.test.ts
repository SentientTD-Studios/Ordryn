import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { Project, Task } from '@/api/types'
import { headerSearchItems, wantsLiveResults } from './headerSearch.ts'

const task = (id: number) => ({ id, title: `Task ${id}` }) as Task
const project = (id: number) => ({ id, name: `Project ${id}` }) as Project

describe('wantsLiveResults', () => {
  it('needs two characters or an id', () => {
    assert.equal(wantsLiveResults(' a '), false)
    assert.equal(wantsLiveResults('ab'), true)
    assert.equal(wantsLiveResults('7'), true)
    assert.equal(wantsLiveResults('#7'), true)
  })
})

describe('headerSearchItems', () => {
  it('is empty for a blank query', () => {
    assert.deepEqual(headerSearchItems('  ', [task(1)], []), [])
  })
  it('lists tasks, then projects, then the full search', () => {
    const kinds = headerSearchItems('task', [task(1), task(2)], [project(3)]).map((i) => i.kind)
    assert.deepEqual(kinds, ['task', 'task', 'project', 'all'])
  })
  it('offers to open an id the results lack', () => {
    assert.deepEqual(headerSearchItems('#42', [], []).map((i) => i.key), ['open-42', 'all'])
    assert.deepEqual(headerSearchItems('42', [task(42)], []).map((i) => i.key), ['task-42', 'all'])
  })
  it('caps tasks and projects', () => {
    const items = headerSearchItems('x', Array.from({ length: 10 }, (_, i) => task(i + 1)), Array.from({ length: 5 }, (_, i) => project(i + 1)))
    assert.equal(items.filter((i) => i.kind === 'task').length, 6)
    assert.equal(items.filter((i) => i.kind === 'project').length, 3)
  })
})
