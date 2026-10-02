/** Minimal shape needed to coalesce live events (see composables/useLiveUpdates). */
export type LiveEventLike = {
  type: string
  task_id?: number
  project_id?: number
}

/** Synthetic event sent after the live stream reconnects; views should refetch. */
export const LIVE_RESYNC = 'live.resync'

function eventKey(e: LiveEventLike): string {
  return `${e.type}|${e.task_id ?? ''}|${e.project_id ?? ''}`
}

/**
 * Collapses a burst of events to one per (type, task, project), keeping the
 * latest copy of each in arrival order, so no kind of event is lost when a
 * burst is debounced.
 */
export function coalesceLiveEvents<T extends LiveEventLike>(events: T[]): T[] {
  const latest = new Map<string, number>()
  events.forEach((e, i) => latest.set(eventKey(e), i))
  return events.filter((e, i) => latest.get(eventKey(e)) === i)
}

/** True when any event in the batch is a comment on taskId, or a resync. */
export function batchTouchesDiscussion(batch: LiveEventLike[], taskId: number): boolean {
  return batch.some(
    (e) => e.type === LIVE_RESYNC || (e.type === 'task.commented' && (!e.task_id || e.task_id === taskId)),
  )
}

/** True when any event in the batch concerns taskId (or is a resync). */
export function batchTouchesTask(batch: LiveEventLike[], taskId: number): boolean {
  return batch.some((e) => e.type === LIVE_RESYNC || !e.task_id || e.task_id === taskId)
}
