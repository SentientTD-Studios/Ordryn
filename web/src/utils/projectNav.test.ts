import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { Project } from '@/api/types'
import {
  homeProjectOrder,
  pinIdsFromProjects,
  projectInitials,
  projectTileHue,
  pushRecentId,
  searchProjects,
  sidebarProjectSections,
  togglePinId,
} from './projectNav.ts'

function proj(id: number, name: string, extra: Partial<Project> = {}): Project {
  return { id, name, ...extra }
}

const list: Project[] = [
  proj(1, 'Alpha'),
  proj(2, 'Beta'),
  proj(3, 'Gamma'),
  proj(4, 'Delta', { archived: true }),
  proj(5, 'Epsilon', { role: 'editor' }),
  proj(6, 'Zeta', { role: 'viewer' }),
  proj(7, 'Eta', { role: 'viewer' }),
]

describe('pinIdsFromProjects', () => {
  it('orders pinned projects by pin_position', () => {
    const ids = pinIdsFromProjects([
      proj(1, 'a', { pinned: true, pin_position: 2 }),
      proj(2, 'b'),
      proj(3, 'c', { pinned: true, pin_position: 0 }),
    ])
    assert.deepEqual(ids, [3, 1])
  })
})

describe('sidebarProjectSections', () => {
  it('auto-fills the top active projects when nothing is pinned', () => {
    const s = sidebarProjectSections(list, [], [], { autoFill: 3 })
    assert.deepEqual(s.pinned, [])
    assert.deepEqual(s.suggested.map((p) => p.id), [1, 2, 3])
  })

  it('shows pins in pin order and stops auto-filling', () => {
    const s = sidebarProjectSections(list, [6, 1], [])
    assert.deepEqual(s.pinned.map((p) => p.id), [6, 1])
    assert.deepEqual(s.suggested, [])
  })

  it('lists recents not already shown, skipping archived and unknown ids', () => {
    const s = sidebarProjectSections(list, [1], [1, 4, 99, 7, 5], { recentLimit: 5 })
    assert.deepEqual(s.recent.map((p) => p.id), [7, 5])
  })

  it('does not repeat auto-filled projects in recent', () => {
    const s = sidebarProjectSections(list, [], [2, 7], { autoFill: 3 })
    assert.deepEqual(s.recent.map((p) => p.id), [7])
  })

  it('hides archived pins', () => {
    const s = sidebarProjectSections(list, [4], [])
    assert.deepEqual(s.pinned, [])
  })
})

describe('pushRecentId / togglePinId', () => {
  it('moves an id to the front and caps the list', () => {
    assert.deepEqual(pushRecentId([1, 2, 3], 3, 3), [3, 1, 2])
    assert.deepEqual(pushRecentId([1, 2, 3], 4, 3), [4, 1, 2])
  })

  it('toggles pins, appending new ones', () => {
    assert.deepEqual(togglePinId([1, 2], 3), [1, 2, 3])
    assert.deepEqual(togglePinId([1, 2, 3], 2), [1, 3])
  })
})

describe('searchProjects', () => {
  const items = [
    proj(1, 'Website redesign'),
    proj(2, 'Mobile app', { organization_name: 'Acme' }),
    proj(3, 'Old web stuff', { archived: true }),
    proj(4, 'Design system'),
    proj(5, 'Webhooks'),
  ]

  it('returns everything for an empty query', () => {
    assert.equal(searchProjects(items, '  ').length, 5)
  })

  it('ranks name prefix, then word prefix, then substring', () => {
    assert.deepEqual(searchProjects(items, 'web').map((p) => p.id), [1, 5, 3])
    assert.deepEqual(searchProjects(items, 'design').map((p) => p.id), [4, 1])
  })

  it('matches organization names', () => {
    assert.deepEqual(searchProjects(items, 'acme').map((p) => p.id), [2])
  })
})

describe('homeProjectOrder', () => {
  const items = [
    proj(1, 'One'),
    proj(2, 'Two'),
    proj(3, 'Three', { archived: true }),
    proj(4, 'Four', { role: 'viewer' }),
    proj(5, 'Five'),
  ]

  it('puts pinned, then recent, then the rest, without archived', () => {
    assert.deepEqual(homeProjectOrder(items, [4], [5, 4, 3]).map((p) => p.id), [4, 5, 1, 2])
  })

  it('includes archived matches when filtering', () => {
    assert.deepEqual(homeProjectOrder(items, [], [], 'thr').map((p) => p.id), [3])
  })
})

describe('projectInitials / projectTileHue', () => {
  it('builds initials from one or two words', () => {
    assert.equal(projectInitials('Website redesign'), 'WR')
    assert.equal(projectInitials('api'), 'AP')
    assert.equal(projectInitials('  '), '?')
  })

  it('gives a stable hue in range', () => {
    assert.equal(projectTileHue(7), projectTileHue(7))
    for (const id of [1, 2, 99, 123456]) {
      const h = projectTileHue(id)
      assert.ok(h >= 0 && h < 360)
    }
  })
})
