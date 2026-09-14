import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { kanbanSprintQueryValue, kanbanWorkflowClaimScope } from './kanbanTaskQuery.ts'

describe('kanbanWorkflowClaimScope', () => {
  it('loads every claim state inside a kanban project', () => {
    assert.equal(kanbanWorkflowClaimScope(true), 'all')
  })

  it('keeps the home and classic lists on claimed work only', () => {
    assert.equal(kanbanWorkflowClaimScope(false), 'mine')
  })
})

describe('kanbanSprintQueryValue', () => {
  it('omits sprint filter outside a kanban project', () => {
    assert.equal(kanbanSprintQueryValue(false, 'backlog'), undefined)
    assert.equal(kanbanSprintQueryValue(false, '12'), undefined)
  })

  it('maps backlog to the unassigned sprint filter', () => {
    assert.equal(kanbanSprintQueryValue(true, 'backlog'), 'none')
    assert.equal(kanbanSprintQueryValue(true, ''), 'none')
  })

  it('passes through a sprint id', () => {
    assert.equal(kanbanSprintQueryValue(true, '42'), '42')
  })
})
