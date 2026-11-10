import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { projectRoute, taskPageRoute } from './projectRoute.ts'

describe('projectRoute', () => {
  it('prefers the slug, falling back to the id', () => {
    assert.deepEqual(projectRoute({ id: 7, slug: 'web-app' }), { name: 'project', params: { ref: 'web-app' } })
    assert.deepEqual(projectRoute({ id: 7 }), { name: 'project', params: { ref: '7' } })
    assert.deepEqual(projectRoute(7), { name: 'project', params: { ref: '7' } })
  })

  it('maps home and "no project" to the home route', () => {
    assert.deepEqual(projectRoute(''), { name: 'tasks' })
    assert.deepEqual(projectRoute('0'), { name: 'tasks', query: { project: '0' } })
  })
})

describe('taskPageRoute', () => {
  it('puts the task inside its project', () => {
    assert.deepEqual(taskPageRoute(42, { id: 7, slug: 'web' }), { name: 'project-task', params: { ref: 'web', id: '42' } })
    assert.deepEqual(taskPageRoute(42, { id: 7 }), { name: 'project-task', params: { ref: '7', id: '42' } })
  })

  it('uses the short link without a project', () => {
    assert.deepEqual(taskPageRoute(42, null), { name: 'task', params: { id: '42' } })
  })
})
