import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { formatInterval, formatMs, formatSeconds, sortWorkers, timeAgo, workerLabel } from './systemHealth.ts'

describe('systemHealth helpers', () => {
  it('formats durations', () => {
    assert.equal(formatSeconds(12), '12s')
    assert.equal(formatSeconds(245), '4m 05s')
    assert.equal(formatSeconds(3 * 3600 + 12 * 60), '3h 12m')
    assert.equal(formatSeconds(86400 + 4 * 3600), '1d 4h')
  })

  it('formats intervals', () => {
    assert.equal(formatInterval(15), 'every 15s')
    assert.equal(formatInterval(900), 'every 15m')
    assert.equal(formatInterval(3600), 'every 1h')
    assert.equal(formatInterval(0), 'on demand')
  })

  it('formats milliseconds', () => {
    assert.equal(formatMs(0.42), '0.4 ms')
    assert.equal(formatMs(85), '85 ms')
    assert.equal(formatMs(2400), '2.4 s')
  })

  it('formats relative times', () => {
    const now = Date.parse('2026-11-14T12:00:00Z')
    assert.equal(timeAgo(null, now), 'never')
    assert.equal(timeAgo('2026-11-14T11:59:58Z', now), 'just now')
    assert.equal(timeAgo('2026-11-14T11:56:00Z', now), '4m ago')
    assert.equal(timeAgo('2026-11-14T09:00:00Z', now), '3h ago')
  })

  it('labels known workers and passes unknown names through', () => {
    assert.equal(workerLabel('trash-purge'), 'Trash purge')
    assert.equal(workerLabel('something-new'), 'something-new')
  })

  it('sorts workers needing attention first', () => {
    const sorted = sortWorkers([
      { name: 'trash-purge', status: 'ok' as const },
      { name: 'auto-sprint', status: 'late' as const },
      { name: 'audit-purge', status: 'ok' as const },
      { name: 'webhook-delivery', status: 'stuck' as const },
    ])
    assert.deepEqual(
      sorted.map((w) => w.name),
      ['webhook-delivery', 'auto-sprint', 'audit-purge', 'trash-purge'],
    )
  })
})
