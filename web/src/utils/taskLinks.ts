import type { TaskLink, TaskLinkType } from '@/api/types'

export const LINK_TYPE_ORDER: TaskLinkType[] = ['blocked_by', 'blocks', 'relates', 'duplicates', 'duplicated_by']

export const LINK_TYPE_LABELS: Record<TaskLinkType, string> = {
  blocked_by: 'Blocked by',
  blocks: 'Blocks',
  relates: 'Relates to',
  duplicates: 'Duplicates',
  duplicated_by: 'Duplicated by',
}

/** Group links by type, open tasks first, then by id. */
export function groupTaskLinks(links: TaskLink[]): Record<TaskLinkType, TaskLink[]> {
  const out = Object.fromEntries(LINK_TYPE_ORDER.map((t) => [t, [] as TaskLink[]])) as Record<TaskLinkType, TaskLink[]>
  for (const l of links) {
    if (out[l.type]) out[l.type].push(l)
  }
  for (const t of LINK_TYPE_ORDER) {
    out[t].sort((a, b) => Number(a.completed) - Number(b.completed) || a.task_id - b.task_id)
  }
  return out
}

/** Parse "#123", "123", or a task URL ending in /tasks/123 into a task id. */
export function parseTaskRef(raw: string): number | null {
  const s = raw.trim()
  const m = s.match(/^#?(\d+)$/) || s.match(/\/tasks?\/(\d+)(?:[/?#].*)?$/)
  if (!m) return null
  const id = Number(m[1])
  return Number.isSafeInteger(id) && id > 0 ? id : null
}
