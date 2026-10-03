import { onUnmounted } from 'vue'
import { withBase } from '@/base'
import { LIVE_RESYNC, coalesceLiveEvents } from '@/utils/liveEvents'

export { LIVE_RESYNC } from '@/utils/liveEvents'

export type LiveEvent = {
  type: string
  task_id?: number
  project_id?: number
  actor_id?: number
  origin?: string
  timestamp?: string
  extension_id?: string
  key?: string
}

/**
 * Receives the last event of a debounced burst, plus every distinct event in
 * that burst. Handlers that only need "something changed" can ignore `batch`;
 * handlers that must not miss a kind of event (e.g. a comment followed by an
 * update) should inspect it.
 */
type LiveHandler = (event: LiveEvent, batch: LiveEvent[]) => void | Promise<void>
type RawListener = (event: LiveEvent) => void

const listeners = new Set<RawListener>()
let source: EventSource | null = null
let pauseCount = 0
let pendingWhilePaused: LiveEvent[] = []
let lostConnection = false

export function pauseLiveReload(): void {
  pauseCount += 1
}

export function resumeLiveReload(): void {
  pauseCount = Math.max(0, pauseCount - 1)
  if (pauseCount === 0 && pendingWhilePaused.length) {
    const held = coalesceLiveEvents(pendingWhilePaused)
    pendingWhilePaused = []
    for (const ev of held) dispatch(ev)
  }
}

export function isLiveReloadPaused(): boolean {
  return pauseCount > 0
}

function dispatch(event: LiveEvent): void {
  if (pauseCount > 0) {
    pendingWhilePaused.push(event)
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
  // The browser reconnects on its own, but anything sent while the stream was
  // down is gone. After a reconnect, tell views to refetch what they show.
  source.addEventListener('error', () => {
    lostConnection = true
  })
  source.addEventListener('ready', () => {
    if (!lostConnection) return
    lostConnection = false
    dispatch({ type: LIVE_RESYNC })
  })
}

export function stopLiveUpdates(): void {
  source?.close()
  source = null
  lostConnection = false
}

export function subscribeLiveUpdates(handler: RawListener): () => void {
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
    const batch = coalesceLiveEvents(queued.splice(0))
    void handler(batch[batch.length - 1], batch)
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
