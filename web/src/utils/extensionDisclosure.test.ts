import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { ProjectExtension } from '@/api/types'
import {
  destinationSite,
  enabledTeamExtensions,
  isTeamExtensionEnabled,
  triggerLabels,
} from './extensionDisclosure.ts'

function ext(partial: Partial<ProjectExtension> & { id: string }): ProjectExtension {
  return {
    name: partial.name || partial.id,
    version: '1',
    host_api: 2,
    site_enabled: true,
    manifest: { id: partial.id, name: partial.name || partial.id, version: '1', host_api: 2 },
    settings: { enabled: false, triggers: [], templates: {}, status_only: false },
    secrets: {},
    ...partial,
  }
}

describe('isTeamExtensionEnabled', () => {
  it('requires both site and project toggles', () => {
    assert.equal(isTeamExtensionEnabled(ext({ id: 'a', settings: { enabled: true, triggers: [], templates: {}, status_only: false } })), true)
    assert.equal(
      isTeamExtensionEnabled(ext({ id: 'b', site_enabled: false, settings: { enabled: true, triggers: [], templates: {}, status_only: false } })),
      false,
    )
    assert.equal(isTeamExtensionEnabled(ext({ id: 'c' })), false)
  })
})

describe('enabledTeamExtensions', () => {
  it('returns only turned-on extensions', () => {
    const on = ext({ id: 'discord', settings: { enabled: true, triggers: ['*'], templates: {}, status_only: false } })
    const off = ext({ id: 'slack' })
    assert.deepEqual(enabledTeamExtensions([off, on]).map((e) => e.id), ['discord'])
  })
})

describe('triggerLabels', () => {
  const discord = ext({
    id: 'discord',
    manifest: {
      id: 'discord',
      name: 'Discord',
      version: '1',
      host_api: 2,
      hooks: [
        { on: 'task.created', label: 'Task created' },
        { on: 'task.updated', label: 'Task updated' },
      ],
    },
    settings: { enabled: true, triggers: ['task.created'], templates: {}, status_only: false },
  })

  it('uses hook labels and expands *', () => {
    assert.deepEqual(triggerLabels(discord), ['Task created'])
    assert.deepEqual(triggerLabels({ ...discord, settings: { ...discord.settings, triggers: ['*'] } }), [
      'Task created',
      'Task updated',
    ])
  })
})

describe('destinationSite', () => {
  it('shows the host without implying a secret', () => {
    assert.equal(
      destinationSite(
        ext({
          id: 'discord',
          destination_host: 'discord.com',
          manifest: { id: 'discord', name: 'Discord', version: '1', host_api: 2, delivery: { type: 'discord.webhook' } },
        }),
      ),
      'Discord · discord.com',
    )
  })

  it('labels in-app and unconfigured destinations', () => {
    assert.equal(destinationSite(ext({ id: 'standup' })), 'In-app only')
    assert.equal(
      destinationSite(
        ext({
          id: 'http',
          manifest: { id: 'http', name: 'HTTP', version: '1', host_api: 2, delivery: { type: 'http.webhook' } },
        }),
      ),
      'Webhook · not configured',
    )
  })
})
