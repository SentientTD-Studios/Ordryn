// node --test tools/ollama-agent/changes.test.mjs
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { answerSchema, planChanges, tagChange, usableFields } from './changes.mjs'

const projectTags = [
  { id: 1, name: 'bug' },
  { id: 2, name: 'needs-info' },
  { id: 3, name: 'frontend' },
]
const task = {
  title: 'Fix login',
  description: 'old',
  priority: 1,
  status_id: 10,
  tags: [
    { id: 2, name: 'needs-info', protected: false },
    { id: 9, name: 'Archived', protected: true },
  ],
}
const ctx = (fields) => ({ guardrails: { editable_fields: fields }, tags: projectTags })

describe('answerSchema', () => {
  it('only offers fields the guardrails allow and the runner supports', () => {
    const s = answerSchema(['status', 'tags', 'due_date'])
    assert.ok(s.properties.move_to_status_id)
    assert.ok(s.properties.add_tags && s.properties.remove_tags)
    assert.equal(s.properties.new_description, undefined)
    assert.equal(s.properties.new_priority, undefined)
    assert.deepEqual(s.required, Object.keys(s.properties))
  })
  it('due_date and other fields are not offered', () => {
    assert.deepEqual(usableFields(['due_date', 'sprint', 'priority']), ['priority'])
  })
})

describe('tagChange', () => {
  it('removes a tag by name and keeps protected tags out of tag_ids', () => {
    const r = tagChange({ remove_tags: ['Needs-Info'] }, task.tags, projectTags)
    assert.deepEqual(r.tagIds, [])
    assert.deepEqual(r.removed, ['needs-info'])
  })
  it('adds by name, with or without #', () => {
    const r = tagChange({ add_tags: ['#bug', 'frontend'] }, task.tags, projectTags)
    assert.deepEqual(r.tagIds, [2, 1, 3])
    assert.deepEqual(r.added, ['bug', 'frontend'])
  })
  it('reports unknown tags instead of failing silently', () => {
    const r = tagChange({ add_tags: ['urgent'] }, task.tags, projectTags)
    assert.equal(r.tagIds, null)
    assert.match(r.problems[0], /no tag named "urgent"/)
  })
  it('is a no-op when nothing changes', () => {
    assert.equal(tagChange({ add_tags: ['needs-info'], remove_tags: ['bug'] }, task.tags, projectTags).tagIds, null)
  })
})

describe('planChanges', () => {
  it('builds one PATCH per field, status last', () => {
    const answer = { new_title: 'Fix login redirect', new_priority: 3, remove_tags: ['needs-info'], add_tags: ['bug'], move_to_status_id: 11 }
    const { patches, problems } = planChanges(answer, task, ctx(['status', 'title', 'priority', 'tags']))
    assert.deepEqual(problems, [])
    assert.deepEqual(patches.map((p) => p.label), ['title', 'priority', 'tags (+bug -needs-info)', 'status'])
    assert.deepEqual(patches[2].body, { tag_ids: [1] })
  })
  it('ignores fields the guardrails do not allow, even if the model sets them', () => {
    const { patches } = planChanges({ new_title: 'x', remove_tags: ['needs-info'] }, task, ctx(['status']))
    assert.deepEqual(patches, [])
  })
  it('skips unchanged values and out-of-range priorities', () => {
    const { patches, problems } = planChanges({ move_to_status_id: 10, new_priority: 7, new_description: 'old' }, task,
      ctx(['status', 'priority', 'description']))
    assert.deepEqual(patches, [])
    assert.match(problems[0], /priority 7/)
  })
})
