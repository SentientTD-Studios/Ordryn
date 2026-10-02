// Pure helpers for the admin audit log view (kept framework-free so node:test can run them).

export type AuditChange = { field: string; from: string; to: string }

type AuditLike = {
  source: string
  event_type: string
  summary?: string
  metadata: Record<string, unknown>
}

export const auditSourceLabels: Record<string, string> = {
  task: 'Task',
  project: 'Project',
  comment: 'Comment',
  email: 'Email',
  admin: 'Admin',
}

/** "status_changed" → "Status changed"; comment_edit → "Comment edit". */
export function humanizeEventType(eventType: string): string {
  const words = eventType.replace(/[_.]+/g, ' ').trim()
  if (!words) return eventType
  return words.charAt(0).toUpperCase() + words.slice(1)
}

export function auditEventLabel(entry: AuditLike): string {
  return entry.summary || humanizeEventType(entry.event_type)
}

export function formatAuditValue(v: unknown): string {
  if (v === undefined || v === null || v === '') return '—'
  if (Array.isArray(v)) return v.length ? v.map((x) => formatAuditValue(x)).join(', ') : '(none)'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

function isChangeObject(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v) && ('from' in v || 'to' in v || 'changed' in v)
}

function changeFrom(field: string, v: Record<string, unknown>): AuditChange {
  if ('changed' in v && !('from' in v) && !('to' in v)) {
    return { field, from: '•••', to: '••• (updated)' }
  }
  return { field, from: formatAuditValue(v.from), to: formatAuditValue(v.to) }
}

/**
 * Before → after pairs for a row. Reads top-level metadata.from/to, nested
 * {field: {from, to}} entries, and site-settings metadata.changes.
 */
export function auditChanges(entry: AuditLike): AuditChange[] {
  const meta = entry.metadata || {}
  const out: AuditChange[] = []
  if ('from' in meta || 'to' in meta) {
    out.push({ field: '', from: formatAuditValue(meta.from), to: formatAuditValue(meta.to) })
  }
  const changes = meta.changes
  if (changes && typeof changes === 'object' && !Array.isArray(changes)) {
    for (const key of Object.keys(changes).sort()) {
      const v = (changes as Record<string, unknown>)[key]
      if (isChangeObject(v)) out.push(changeFrom(key, v))
    }
  }
  for (const key of Object.keys(meta).sort()) {
    if (key === 'from' || key === 'to' || key === 'changes') continue
    const v = meta[key]
    if (isChangeObject(v)) out.push(changeFrom(key, v))
  }
  return out
}

/** Remaining metadata worth showing as key: value detail (excluding change pairs). */
export function auditDetails(entry: AuditLike): { key: string; value: string }[] {
  const meta = entry.metadata || {}
  const out: { key: string; value: string }[] = []
  for (const key of Object.keys(meta).sort()) {
    if (key === 'from' || key === 'to' || key === 'changes') continue
    const v = meta[key]
    if (isChangeObject(v)) continue
    if (v === undefined || v === null || v === '') continue
    out.push({ key, value: formatAuditValue(v) })
  }
  return out
}
