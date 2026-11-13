import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { projectSettingsGroups, resolveProjectSettingsSection } from './projectSettingsSections.ts'

const ids = (groups: ReturnType<typeof projectSettingsGroups>) => groups.flatMap((g) => g.items.map((i) => i.id))

describe('projectSettingsGroups', () => {
  it('shows every section to a kanban manager outside company mode', () => {
    const groups = projectSettingsGroups({ isKanban: true, canManage: true, isCompanyMode: false })
    assert.deepEqual(groups.map((g) => g.id), ['general', 'workflow', 'intake', 'ceremonies', 'integrations'])
    assert.deepEqual(ids(groups), [
      'details', 'sharing', 'roles',
      'board', 'sprints', 'estimates', 'tags', 'rules',
      'forms', 'feedback',
      'standup', 'refinement', 'retro',
      'github', 'extensions', 'api-keys', 'agents',
    ])
  })

  it('hides manager-only sections and drops empty groups for non-managers', () => {
    const groups = projectSettingsGroups({ isKanban: false, canManage: false, isCompanyMode: false })
    assert.deepEqual(groups.map((g) => g.id), ['general', 'workflow', 'integrations'])
    assert.deepEqual(ids(groups), ['details', 'sharing', 'board', 'tags', 'github', 'extensions'])
  })

  it('hides roles in company mode and sprints on classic boards', () => {
    const list = ids(projectSettingsGroups({ isKanban: false, canManage: true, isCompanyMode: true }))
    assert.ok(!list.includes('roles'))
    assert.ok(!list.includes('sprints'))
    assert.ok(!list.includes('estimates'))
    assert.ok(!list.includes('refinement'))
    assert.ok(!list.includes('retro'))
    assert.ok(!list.includes('standup'))
  })
})

describe('resolveProjectSettingsSection', () => {
  it('falls back to details when the section is hidden', () => {
    const groups = projectSettingsGroups({ isKanban: false, canManage: true, isCompanyMode: false })
    assert.equal(resolveProjectSettingsSection('sprints', groups), 'details')
    assert.equal(resolveProjectSettingsSection('rules', groups), 'rules')
  })
})
