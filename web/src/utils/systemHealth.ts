import type { WorkerStatusValue } from '@/api/types'

// Display helpers for Admin → System.

const WORKER_LABELS: Record<string, string> = {
  'audit-purge': 'Audit log purge',
  'auto-sprint': 'Auto-sprint creation',
  'due-date-hooks': 'Due-date webhooks',
  'due-reminders': 'Due-date reminders',
  'email-audit-purge': 'Email log purge',
  'notification-email': 'Notification emails',
  'scheduled-backup': 'Scheduled backup',
  'timed-automation': 'Timed automation rules',
  'trash-purge': 'Trash purge',
  'webhook-delivery': 'Webhook delivery',
}

export function workerLabel(name: string): string {
  return WORKER_LABELS[name] ?? name
}

export function workerStatusBadge(status: WorkerStatusValue): { label: string; cls: string } {
  switch (status) {
    case 'ok':
      return { label: 'OK', cls: 'text-bg-success' }
    case 'running':
      return { label: 'Running', cls: 'text-bg-info' }
    case 'pending':
      return { label: 'Waiting for first run', cls: 'text-bg-secondary' }
    case 'error':
      return { label: 'Last run failed', cls: 'text-bg-danger' }
    case 'late':
      return { label: 'Late', cls: 'text-bg-warning' }
    case 'stuck':
      return { label: 'Stuck', cls: 'text-bg-danger' }
  }
}

/** "1d 4h", "3h 12m", "4m 05s", "12s". */
export function formatSeconds(total: number): string {
  const s = Math.max(0, Math.floor(total))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${String(sec).padStart(2, '0')}s`
  return `${sec}s`
}

/** "every 15m", "every 1h", "every 15s". */
export function formatInterval(seconds: number): string {
  if (seconds <= 0) return 'on demand'
  if (seconds % 3600 === 0) return `every ${seconds / 3600}h`
  if (seconds % 60 === 0) return `every ${seconds / 60}m`
  return `every ${seconds}s`
}

/** Milliseconds as "850 ms" or "2.4 s". */
export function formatMs(ms: number): string {
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

/** "12s ago", "4m ago", "3h ago", "2d ago"; "never" for null. */
export function timeAgo(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return 'never'
  const diff = (now - new Date(iso).getTime()) / 1000
  if (diff < 5) return 'just now'
  if (diff < 60) return `${Math.floor(diff)}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

/** Workers that need attention sort first, then by label. */
export function sortWorkers<T extends { name: string; status: WorkerStatusValue }>(list: T[]): T[] {
  const rank: Record<WorkerStatusValue, number> = { stuck: 0, error: 1, late: 2, running: 3, pending: 4, ok: 5 }
  return [...list].sort((a, b) => rank[a.status] - rank[b.status] || workerLabel(a.name).localeCompare(workerLabel(b.name)))
}
