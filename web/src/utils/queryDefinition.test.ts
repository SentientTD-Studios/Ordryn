import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { QueryField, QueryNode } from '../api/types.ts'
import {
  countClauses,
  definitionProblems,
  describeClause,
  emptyDefinition,
  fixClause,
  groupDepth,
  isValidDateValue,
  newClause,
  parseNumberList,
  pruneEmptyGroups,
} from './queryDefinition.ts'

const fields: QueryField[] = [
  { id: 'title', label: 'Title', type: 'text', operators: ['contains', 'not_contains', '=', '<>', 'is_empty', 'is_not_empty'], sortable: true },
  { id: 'workflow_status', label: 'Status', type: 'status', operators: ['=', '<>', 'in', 'not_in', 'is_empty', 'is_not_empty'], sortable: true },
  { id: 'due_date', label: 'Due date', type: 'date', operators: ['between', '=', '<>', '<', '<=', '>', '>=', 'is_empty', 'is_not_empty'], sortable: true },
  { id: 'claimed_by', label: 'Claimed by', type: 'user', operators: ['=', '<>', 'in', 'not_in', 'is_empty', 'is_not_empty'], sortable: true },
  { id: 'completed', label: 'Completed', type: 'boolean', operators: ['=', '<>'], sortable: true },
]

describe('queryDefinition', () => {
  it('starts with an empty AND group and default columns', () => {
    const d = emptyDefinition()
    assert.equal(d.where.op, 'and')
    assert.deepEqual(d.where.clauses, [])
    assert.ok(d.columns.includes('title'))
    assert.equal(d.scope.include_personal, true)
  })

  it('picks a natural operator and value for a new clause', () => {
    assert.deepEqual(newClause(fields[1]), { field: 'workflow_status', operator: 'in', value: [] })
    assert.deepEqual(newClause(fields[2]), { field: 'due_date', operator: 'between', value: ['@start_of_week', '@end_of_week'] })
    assert.deepEqual(newClause(fields[3]), { field: 'claimed_by', operator: '=', value: '@me' })
    assert.deepEqual(newClause(fields[4]), { field: 'completed', operator: '=', value: true })
  })

  it('keeps a clause valid when its field or operator changes', () => {
    // Text operator on a boolean field: falls back to the boolean's operator.
    assert.deepEqual(fixClause({ field: 'completed', operator: 'contains', value: 'x' }, fields[4]), {
      field: 'completed',
      operator: '=',
      value: 'x',
    })
    // Scalar to list keeps the value as the first item.
    assert.deepEqual(fixClause({ field: 'workflow_status', operator: 'in', value: 'Doing' }, fields[1]), {
      field: 'workflow_status',
      operator: 'in',
      value: ['Doing'],
    })
    // List to scalar keeps the first item.
    assert.deepEqual(fixClause({ field: 'workflow_status', operator: '=', value: ['Doing', 'Review'] }, fields[1]), {
      field: 'workflow_status',
      operator: '=',
      value: 'Doing',
    })
    // is_empty drops the value.
    assert.deepEqual(fixClause({ field: 'due_date', operator: 'is_empty', value: '@today' }, fields[2]), {
      field: 'due_date',
      operator: 'is_empty',
      value: null,
    })
  })

  it('keeps a date as the start of a range and back', () => {
    assert.deepEqual(fixClause({ field: 'due_date', operator: 'between', value: '2026-11-01' }, fields[2]), {
      field: 'due_date',
      operator: 'between',
      value: ['2026-11-01', '@end_of_week'],
    })
    assert.deepEqual(fixClause({ field: 'due_date', operator: '<=', value: ['@today', '@today+7'] }, fields[2]), {
      field: 'due_date',
      operator: '<=',
      value: '@today',
    })
  })

  it('flags a range with a bad or missing end', () => {
    const d = emptyDefinition()
    d.where.clauses = [
      { field: 'due_date', operator: 'between', value: ['@start_of_week', ''] },
      { field: 'due_date', operator: 'between', value: ['@today', 'soon'] },
      { field: 'due_date', operator: 'between', value: ['@start_of_week', '@end_of_week'] },
    ]
    assert.equal(definitionProblems(d).length, 2)
  })

  it('counts clauses and depth', () => {
    const tree: QueryNode = {
      op: 'and',
      clauses: [{ field: 'title', operator: 'contains', value: 'x' }, { op: 'or', clauses: [{ field: 'completed', operator: '=', value: true }, { op: 'and', clauses: [] }] }],
    }
    assert.equal(countClauses(tree), 2)
    assert.equal(groupDepth(tree), 3)
    assert.deepEqual(pruneEmptyGroups(tree), {
      op: 'and',
      clauses: [{ field: 'title', operator: 'contains', value: 'x' }, { op: 'or', clauses: [{ field: 'completed', operator: '=', value: true }] }],
    })
  })

  it('reports clauses missing values', () => {
    const d = emptyDefinition()
    d.where.clauses = [
      { field: 'workflow_status', operator: 'in', value: [] },
      { field: 'title', operator: 'contains', value: '' },
      { field: 'due_date', operator: 'is_empty', value: null },
    ]
    assert.equal(definitionProblems(d).length, 2)
  })

  it('describes clauses with names, never raw ids', () => {
    const opts = { projects: [], people: [{ id: 5, name: 'Ada' }], sprints: [] }
    assert.equal(describeClause({ field: 'claimed_by', operator: 'in', value: ['@me', 5, 9] }, fields, opts), 'Claimed by is any of Me, Ada, Unavailable person')
    assert.equal(describeClause({ field: 'due_date', operator: '<', value: '@today' }, fields, opts), 'Due date is before @today')
    assert.equal(describeClause({ field: 'title', operator: 'is_empty' }, fields, opts), 'Title is empty')
    assert.equal(
      describeClause({ field: 'due_date', operator: 'between', value: ['@start_of_week', '@end_of_week'] }, fields, opts),
      'Due date is between @start_of_week and @end_of_week',
    )
  })

  it('validates date inputs', () => {
    for (const ok of ['@today', '@today-7', '@START_OF_WEEK+6', '@end_of_week', '2024-02-29']) assert.ok(isValidDateValue(ok), ok)
    for (const bad of ['@yesterday', '2023-02-29', '2024-13-01', 'today']) assert.ok(!isValidDateValue(bad), bad)
  })

  it('parses number lists', () => {
    assert.deepEqual(parseNumberList('1, 2,,x,3'), [1, 2, 3])
  })
})
