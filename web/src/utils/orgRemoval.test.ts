import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { orgRemovalModeLabel, splitOrgRemovalProjects } from './orgRemoval.ts'

describe('splitOrgRemovalProjects', () => {
  it('separates automatic projects and preselects copied ones', () => {
    const { automatic, optional, preselected } = splitOrgRemovalProjects([
      { id: 1, name: 'Locked', mode: 'lock', automatic: true },
      { id: 2, name: 'Copied', mode: 'copy', automatic: false },
      { id: 3, name: 'Picked', mode: 'select', automatic: false },
      { id: 4, name: 'Shared', mode: 'personal', automatic: false },
    ])
    assert.deepEqual(automatic.map((p) => p.id), [1])
    assert.deepEqual(optional.map((p) => p.id), [2, 3, 4])
    assert.deepEqual(preselected, [2])
  })

  it('labels every mode', () => {
    for (const mode of ['lock', 'copy', 'select', 'share', 'personal'] as const) {
      assert.ok(orgRemovalModeLabel(mode).length > 0)
    }
  })
})
