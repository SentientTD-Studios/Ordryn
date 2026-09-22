import assert from 'node:assert/strict'
import { afterEach, describe, it } from 'node:test'
import { DEFAULT_TASK_LIST_STATUS, useTaskListFilters } from './useTaskListFilters.ts'

const {
  filters,
  hasActiveFilters,
  toApiParams,
  toExportQuery,
  setFilter,
  clearFilters,
  applySavedView,
} = useTaskListFilters()

afterEach(() => {
  applySavedView({})
})

describe('clearFilters', () => {
  it('keeps the current project and restores other list filters to defaults', () => {
    setFilter('project', '42')
    setFilter('status', 'complete')
    setFilter('due', 'today')
    setFilter('priority', '3')
    setFilter('tag', 'urgent')
    setFilter('sort', 'priority')
    setFilter('search', 'deploy')
    setFilter('completed', '1')

    clearFilters()

    assert.equal(filters.project, '42')
    assert.equal(filters.status, DEFAULT_TASK_LIST_STATUS)
    assert.equal(filters.due, '')
    assert.equal(filters.priority, '')
    assert.equal(filters.tag, '')
    assert.equal(filters.sort, '')
    assert.equal(filters.search, '')
    assert.equal(filters.completed, '')
  })

  it('keeps the No Project inbox (project "0")', () => {
    setFilter('project', '0')
    setFilter('status', 'complete')
    setFilter('search', 'inbox')

    clearFilters()

    assert.equal(filters.project, '0')
    assert.equal(filters.status, DEFAULT_TASK_LIST_STATUS)
    assert.equal(filters.search, '')
  })

  it('leaves an empty project empty when clearing from the home list', () => {
    setFilter('status', 'complete')
    setFilter('tag', 'bug')

    clearFilters()

    assert.equal(filters.project, '')
    assert.equal(filters.status, DEFAULT_TASK_LIST_STATUS)
    assert.equal(filters.tag, '')
  })

  it('still sends the current project on the next list request', () => {
    setFilter('project', '7')
    setFilter('priority', '2')
    setFilter('search', 'api')

    clearFilters()

    assert.deepEqual(toApiParams(1, 50), {
      page: 1,
      per_page: 50,
      status: DEFAULT_TASK_LIST_STATUS,
      project: '7',
    })
    assert.equal(toExportQuery(), `&status=${DEFAULT_TASK_LIST_STATUS}&project=7`)
  })
})

describe('hasActiveFilters', () => {
  it('ignores the current project so being in a project is not an active list filter', () => {
    setFilter('project', '42')
    assert.equal(hasActiveFilters.value, false)
  })

  it('is true when a list filter differs from its default', () => {
    setFilter('project', '42')
    setFilter('due', 'overdue')
    assert.equal(hasActiveFilters.value, true)

    clearFilters()
    assert.equal(hasActiveFilters.value, false)
    assert.equal(filters.project, '42')
  })
})

describe('applySavedView', () => {
  it('replaces the current project with the saved view project', () => {
    setFilter('project', '42')
    setFilter('status', 'complete')

    applySavedView({ project: '9', due: 'today' })

    assert.equal(filters.project, '9')
    assert.equal(filters.due, 'today')
    assert.equal(filters.status, DEFAULT_TASK_LIST_STATUS)
  })

  it('leaves the project when the saved view has no project of its own', () => {
    setFilter('project', '42')
    setFilter('tag', 'ops')

    applySavedView({ status: 'complete' })

    assert.equal(filters.project, '')
    assert.equal(filters.status, 'complete')
    assert.equal(filters.tag, '')
  })
})
