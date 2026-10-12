import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { Task } from '@/api/types'
import { useTaskListState } from './useTaskListState.ts'

function task(id: number, extra: Partial<Task> = {}): Task {
  return { id, title: `Task ${id}`, description: '', completed: false, priority: 0, project_id: 1, ...extra } as Task
}

/** A list showing only open tasks in project 1. */
function openTasksList() {
  const state = useTaskListState({
    inProjectScope: (t) => t.project_id === 1,
    matchesFilters: (t) => t.project_id === 1 && !t.completed,
  })
  state.tasks.value = [task(1), task(2)]
  state.total.value = 2
  state.incompleteCount.value = 2
  state.completedCount.value = 5
  return state
}

describe('useTaskListState', () => {
  it('moves a task completed out of an open-only list from open to done once', () => {
    const s = openTasksList()
    s.applyTaskUpdate(task(1, { completed: true }))
    assert.deepEqual(s.tasks.value.map((t) => t.id), [2])
    assert.equal(s.total.value, 1)
    assert.equal(s.incompleteCount.value, 1)
    assert.equal(s.completedCount.value, 6)
  })

  it('drops a task moved to another project from the list and the counts', () => {
    const s = openTasksList()
    s.applyTaskUpdate(task(2, { project_id: 9 }))
    assert.equal(s.total.value, 1)
    assert.equal(s.incompleteCount.value, 1)
    assert.equal(s.completedCount.value, 5)
  })

  it('counts but does not show an added task the filters hide', () => {
    const s = openTasksList()
    s.registerTaskAdded(task(3, { completed: true }))
    assert.equal(s.tasks.value.length, 2)
    assert.equal(s.total.value, 2)
    assert.equal(s.completedCount.value, 6)
  })

  it('ignores an added task from outside the project scope', () => {
    const s = openTasksList()
    s.registerTaskAdded(task(4, { project_id: 9 }))
    assert.equal(s.total.value, 2)
    assert.equal(s.incompleteCount.value, 2)
  })

  it('nests an added subtask under its loaded parent', () => {
    const s = openTasksList()
    s.registerTaskAdded(task(5, { parent_id: 1 }))
    assert.equal(s.tasks.value.length, 2)
    assert.deepEqual(s.tasks.value[0].children?.map((c) => c.id), [5])
    assert.equal(s.tasks.value[0].child_count, 1)
    assert.equal(s.incompleteCount.value, 3)
  })

  it('removes a parent with its children and clears their selection', () => {
    const s = openTasksList()
    s.registerTaskAdded(task(5, { parent_id: 1 }))
    s.selected.value = [1, 5, 2]
    s.removeTaskLocally(s.tasks.value[0])
    assert.deepEqual(s.tasks.value.map((t) => t.id), [2])
    assert.deepEqual(s.selected.value, [2])
    assert.equal(s.incompleteCount.value, 1)
  })

  it('reorders a board column and stamps the new status', () => {
    const s = openTasksList()
    s.tasks.value = [task(1), task(2), task(3)]
    s.applyBoardReorder({ statusId: 7, taskIds: [3, 1] })
    assert.deepEqual(s.tasks.value.map((t) => t.id), [3, 1, 2])
    assert.equal(s.tasks.value[0].status_id, 7)
    assert.notEqual(s.tasks.value[2].status_id, 7)
  })
})
