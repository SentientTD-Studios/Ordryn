import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { Project, ProjectStatus } from '@/api/types'
import {
  PROJECT_PERMS,
  canMoveTaskStatus,
  canManageProject,
  discussionAuthorLabel,
  hasAnyProjectWrite,
  hasProjectPerm,
  slugifyRoleName,
} from './projectPerms.ts'

function project(partial: Partial<Project>): Project {
  return {
    id: 1,
    name: 'Board',
    ...partial,
  }
}

function status(partial: Partial<ProjectStatus> & { id: number; name: string }): ProjectStatus {
  return {
    project_id: 1,
    position: 0,
    is_done: false,
    is_default: false,
    created_at: '',
    ...partial,
  }
}

describe('hasProjectPerm', () => {
  it('treats missing project and owner as allowed', () => {
    assert.equal(hasProjectPerm(null, PROJECT_PERMS.TASKS_CREATE), true)
    assert.equal(hasProjectPerm(project({ role: 'owner' }), PROJECT_PERMS.PROJECT_MANAGE), true)
    assert.equal(hasProjectPerm(project({}), PROJECT_PERMS.TASKS_DELETE), true)
  })

  it('checks the permission list for other roles', () => {
    const qa = project({ role: 'qa', permissions: [PROJECT_PERMS.TASKS_STATUS, PROJECT_PERMS.TASKS_CLAIM] })
    assert.equal(hasProjectPerm(qa, PROJECT_PERMS.TASKS_STATUS), true)
    assert.equal(hasProjectPerm(qa, PROJECT_PERMS.TASKS_CREATE), false)
    assert.equal(canManageProject(qa), false)
    assert.equal(hasAnyProjectWrite(qa), true)
  })

  it('treats viewers with no write perms as read-only', () => {
    const viewer = project({ role: 'viewer', permissions: [] })
    assert.equal(hasAnyProjectWrite(viewer), false)
    assert.equal(hasProjectPerm(viewer, PROJECT_PERMS.TASKS_EDIT), false)
  })
})

describe('canMoveTaskStatus', () => {
  const ready = status({ id: 1, name: 'Ready for QA' })
  const inQA = status({
    id: 2,
    name: 'In QA',
    enter_role_slugs: ['qa'],
    leave_role_slugs: ['qa'],
  })
  const qa = project({ role: 'qa', permissions: [PROJECT_PERMS.TASKS_STATUS] })
  const dev = project({ role: 'developer', permissions: [PROJECT_PERMS.TASKS_STATUS] })
  const owner = project({ role: 'owner' })

  it('lets QA enter and leave a gated In QA column', () => {
    assert.equal(canMoveTaskStatus(qa, ready, inQA), true)
    assert.equal(canMoveTaskStatus(qa, inQA, ready), true)
  })

  it('blocks developers from gated In QA moves', () => {
    assert.equal(canMoveTaskStatus(dev, ready, inQA), false)
    assert.equal(canMoveTaskStatus(dev, inQA, ready), false)
  })

  it('lets owners and site admins bypass gates', () => {
    assert.equal(canMoveTaskStatus(owner, ready, inQA), true)
    assert.equal(canMoveTaskStatus(dev, ready, inQA, { permissions: ['admin'] }), true)
  })

  it('keeps unrestricted columns open to any status-capable role', () => {
    const done = status({ id: 3, name: 'Done' })
    assert.equal(canMoveTaskStatus(dev, ready, done), true)
  })
})

describe('discussionAuthorLabel', () => {
  it('formats username with the project role name', () => {
    assert.equal(discussionAuthorLabel('Ryan', 'Owner'), 'Ryan - Owner')
    assert.equal(discussionAuthorLabel('Tester', 'QA', false), 'Tester - QA')
    assert.equal(discussionAuthorLabel('Dev', 'Developer II', true), 'You - Developer II')
    assert.equal(discussionAuthorLabel('Alex'), 'Alex')
  })
})

describe('slugifyRoleName', () => {
  it('builds a valid role slug from a display name', () => {
    assert.equal(slugifyRoleName('Developer II'), 'developer-ii')
    assert.equal(slugifyRoleName('  QA Lead  '), 'qa-lead')
  })
})
