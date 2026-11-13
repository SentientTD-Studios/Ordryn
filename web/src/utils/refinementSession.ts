import type { ProjectSprint, RefinementOutcome, RefinementSession, RefinementStartInput } from '@/api/types'

export type QueueItemState = 'current' | RefinementOutcome | 'pending'

/** How a queued task shows in the session list. */
export function queueItemState(session: RefinementSession, taskId: number): QueueItemState {
  if (session.current_task_id === taskId) return 'current'
  return session.outcomes[String(taskId)] ?? 'pending'
}

/** Counts for the session progress bar. */
export function sessionProgress(session: RefinementSession) {
  let refined = 0
  let skipped = 0
  for (const id of session.queue) {
    const o = session.outcomes[String(id)]
    if (o === 'refined') refined++
    else if (o === 'skipped') skipped++
  }
  const total = session.queue.length
  return { total, refined, skipped, remaining: total - refined - skipped, done: session.current >= total }
}

/** Sprint scope choice in the start form: any task, the backlog, or one sprint id. */
export type RefinementScope = 'any' | 'backlog' | number

export function startInputFor(scope: RefinementScope, opts: { title: string; includeUnflagged: boolean }): RefinementStartInput {
  const input: RefinementStartInput = { include_unflagged: opts.includeUnflagged }
  const title = opts.title.trim()
  if (title) input.title = title
  if (scope === 'backlog') input.backlog = true
  else if (typeof scope === 'number') input.sprint_id = scope
  return input
}

/** Scope picker options: open sprints first (active on top), then finished ones. */
export function scopeOptions(sprints: ProjectSprint[]): { value: RefinementScope; label: string }[] {
  const ordered = [...sprints].sort((a, b) => Number(b.is_active) - Number(a.is_active))
  return [
    { value: 'any', label: 'All open tasks' },
    { value: 'backlog', label: 'Backlog only' },
    ...ordered.map((s) => ({ value: s.id as RefinementScope, label: s.is_active ? `${s.name} (active)` : s.name })),
  ]
}

/** Parses "12, #40 7" into unique positive task ids. */
export function parseTaskIdList(raw: string): number[] {
  const out: number[] = []
  for (const part of raw.split(/[\s,]+/)) {
    const n = Number(part.replace(/^#/, ''))
    if (Number.isInteger(n) && n > 0 && !out.includes(n)) out.push(n)
  }
  return out
}
