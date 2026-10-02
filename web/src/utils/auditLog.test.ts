import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { auditChanges, auditDetails, auditEventLabel, formatAuditValue, humanizeEventType } from './auditLog.ts'

describe('humanizeEventType', () => {
  it('turns snake_case into a sentence', () => {
    assert.equal(humanizeEventType('status_changed'), 'Status changed')
    assert.equal(humanizeEventType('comment_edit'), 'Comment edit')
  })
})

describe('auditEventLabel', () => {
  it('prefers the server summary', () => {
    assert.equal(auditEventLabel({ source: 'task', event_type: 'title_changed', summary: 'Title · x', metadata: {} }), 'Title · x')
    assert.equal(auditEventLabel({ source: 'admin', event_type: 'user_banned', metadata: {} }), 'User banned')
  })
})

describe('formatAuditValue', () => {
  it('formats empties, arrays and objects', () => {
    assert.equal(formatAuditValue(''), '—')
    assert.equal(formatAuditValue(null), '—')
    assert.equal(formatAuditValue(['a', 'b']), 'a, b')
    assert.equal(formatAuditValue([]), '(none)')
    assert.equal(formatAuditValue(false), 'false')
    assert.equal(formatAuditValue({ a: 1 }), '{"a":1}')
  })
})

describe('auditChanges', () => {
  it('reads top-level from/to', () => {
    assert.deepEqual(auditChanges({ source: 'task', event_type: 'title_changed', metadata: { from: 'A', to: 'B' } }), [
      { field: '', from: 'A', to: 'B' },
    ])
  })

  it('handles one-sided changes', () => {
    assert.deepEqual(auditChanges({ source: 'task', event_type: 'due_date_changed', metadata: { to: '2026-10-01' } }), [
      { field: '', from: '—', to: '2026-10-01' },
    ])
  })

  it('reads site settings changes and masks secrets', () => {
    const changes = auditChanges({
      source: 'admin',
      event_type: 'site_settings_updated',
      metadata: {
        changes: {
          site_name: { from: 'Old', to: 'New' },
          email_smtp_password: { changed: true },
        },
      },
    })
    assert.deepEqual(changes, [
      { field: 'email_smtp_password', from: '•••', to: '••• (updated)' },
      { field: 'site_name', from: 'Old', to: 'New' },
    ])
  })

  it('reads nested field changes', () => {
    const changes = auditChanges({
      source: 'admin',
      event_type: 'extension_updated',
      metadata: { enabled: { from: false, to: true }, templates_updated: true },
    })
    assert.deepEqual(changes, [{ field: 'enabled', from: 'false', to: 'true' }])
  })
})

describe('auditDetails', () => {
  it('lists non-change metadata', () => {
    const details = auditDetails({
      source: 'email',
      event_type: 'password_reset',
      metadata: { status: 'failed', error: '', to_email: 'a@b.c', name: { from: 'x', to: 'y' } },
    })
    assert.deepEqual(details, [
      { key: 'status', value: 'failed' },
      { key: 'to_email', value: 'a@b.c' },
    ])
  })
})
