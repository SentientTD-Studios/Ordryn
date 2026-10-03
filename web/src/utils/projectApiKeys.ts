export type ProjectAPIKeyScope = 'tasks:read' | 'tasks:write' | 'comments:write'

export const PROJECT_API_KEY_SCOPES: { id: ProjectAPIKeyScope; label: string; help: string }[] = [
  { id: 'tasks:read', label: 'Read tasks', help: 'List and view tasks and comments, statuses, sprints and custom fields' },
  { id: 'tasks:write', label: 'Write tasks', help: 'Create tasks and update them (title, status, completion, fields…)' },
  { id: 'comments:write', label: 'Post comments', help: 'Add comments to tasks' },
]

export type ExpiryPreset = 'never' | '30' | '90' | '365'

export const EXPIRY_PRESETS: { id: ExpiryPreset; label: string }[] = [
  { id: '30', label: '30 days' },
  { id: '90', label: '90 days' },
  { id: '365', label: '1 year' },
  { id: 'never', label: 'Never' },
]

/** ISO timestamp for a preset, or null when the key should not expire. */
export function expiresAtFromPreset(preset: ExpiryPreset, now: Date = new Date()): string | null {
  if (preset === 'never') return null
  const days = Number(preset)
  return new Date(now.getTime() + days * 24 * 60 * 60 * 1000).toISOString()
}

/** Short human status for a key's expiry. */
export function keyExpiryLabel(expiresAt: string | null | undefined, now: Date = new Date()): string {
  if (!expiresAt) return 'Never'
  const at = new Date(expiresAt)
  if (Number.isNaN(at.getTime())) return expiresAt
  const ms = at.getTime() - now.getTime()
  if (ms <= 0) return 'Expired'
  const days = Math.ceil(ms / (24 * 60 * 60 * 1000))
  if (days <= 1) return 'Expires today'
  return `In ${days} days`
}

export function scopeLabel(scope: string): string {
  return PROJECT_API_KEY_SCOPES.find((s) => s.id === scope)?.label ?? scope
}
