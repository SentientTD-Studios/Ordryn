import assert from 'node:assert/strict'
import test from 'node:test'
import type { TaskLink } from '../api/types.ts'
import { groupTaskLinks, parseTaskRef } from './taskLinks.ts'

test('parseTaskRef accepts numbers, #refs, and task URLs', () => {
  assert.equal(parseTaskRef('42'), 42)
  assert.equal(parseTaskRef(' #7 '), 7)
  assert.equal(parseTaskRef('https://ordryn.example/tasks/913'), 913)
  assert.equal(parseTaskRef('https://ordryn.example/task/15?x=1'), 15)
  assert.equal(parseTaskRef('abc'), null)
  assert.equal(parseTaskRef('#0'), null)
  assert.equal(parseTaskRef(''), null)
})

test('groupTaskLinks buckets by type with open tasks first', () => {
  const mk = (task_id: number, type: TaskLink['type'], completed = false): TaskLink => ({
    link_id: task_id, type, task_id, title: `T${task_id}`, completed, project_id: null,
  })
  const g = groupTaskLinks([mk(5, 'blocked_by', true), mk(3, 'blocked_by'), mk(9, 'relates'), mk(1, 'blocks')])
  assert.deepEqual(g.blocked_by.map((l) => l.task_id), [3, 5])
  assert.deepEqual(g.relates.map((l) => l.task_id), [9])
  assert.deepEqual(g.blocks.map((l) => l.task_id), [1])
  assert.deepEqual(g.duplicates, [])
})
