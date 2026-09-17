import { onUnmounted } from 'vue'
import { withBase } from '@/base'

export type LiveEvent = {
  type: string
  task_id?: number
  project_id?: number
  actor_id?: number
  origin?: string
  timestamp?: string
}

type LiveHandler = (event: LiveEvent) => void

const listeners = new Set<LiveHandler>()
let source: EventSource | null = null
let pauseCount = 0
let pendingWhilePaused: LiveEvent | null = null

export function pauseLiveReload(): void {
  pauseCount += 1
}

export function resumeLiveReload(): void {
  pauseCount = Math.max(0, pauseCount - 1)
  if (pauseCount === 0 && pendingWhilePaused) {
    const ev = pendingWhilePaused
    pendingWhilePaused = null
    dispatch(ev)
  }
}

export function isLiveReloadPaused(): boolean {
  return pauseCount > 0
}

function dispatch(event: LiveEvent): void {
  if (pauseCount > 0) {
    pendingWhilePaused = event
    return
  }
  for (const fn of listeners) {
    try {
      fn(event)
    } catch {
      /* ignore subscriber errors */
    }
  }
}

export function startLiveUpdates(): void {
  if (source || typeof EventSource === 'undefined') return
  const url = withBase('/api/v2/events')
  source = new EventSource(url)
  source.addEventListener('task-update', (raw) => {
    const msg = raw as MessageEvent<string>
    let payload: LiveEvent | null = null
    try {
      payload = JSON.parse(msg.data || '{}') as LiveEvent
    } catch {
      payload = { type: 'task.updated' }
    }
    dispatch(payload)
  })
}

export function stopLiveUpdates(): void {
  source?.close()
  source = null
}

export function subscribeLiveUpdates(handler: LiveHandler): () => void {
  listeners.add(handler)
  return () => {
    listeners.delete(handler)
  }
}

/** True when this focused tab already applied the actor's own mutation. */
export function isOwnFocusedLiveEvent(event: LiveEvent, userId?: number | null): boolean {
  if (!userId || !event.actor_id || event.actor_id !== userId) return false
  if (typeof document === 'undefined') return false
  return document.hasFocus()
}

/** Subscribe while the caller is mounted; debounce bursts of events. */
export function useLiveUpdates(handler: LiveHandler, debounceMs = 200): void {
  let timer: ReturnType<typeof setTimeout> | null = null
  const queued: LiveEvent[] = []

  function flush(): void {
    timer = null
    if (isLiveReloadPaused() || queued.length === 0) {
      queued.length = 0
      return
    }
    const last = queued[queued.length - 1]
    queued.length = 0
    handler(last)
  }

  const unsub = subscribeLiveUpdates((event) => {
    queued.push(event)
    if (timer) return
    timer = setTimeout(flush, debounceMs)
  })

  onUnmounted(() => {
    unsub()
    if (timer) clearTimeout(timer)
  })
}
