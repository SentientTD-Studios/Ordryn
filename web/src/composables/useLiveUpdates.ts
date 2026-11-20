import { onUnmounted } from 'vue'
import { withBase } from '@/base'
import { LIVE_CEREMONY_UPDATED, LIVE_RESYNC, coalesceLiveEvents } from '@/utils/liveEvents'
import { hasRecentOwnMutation } from '@/utils/ownMutations'

export { LIVE_CEREMONY_UPDATED, LIVE_RESYNC } from '@/utils/liveEvents'

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

/**
 * The server pings every 25s. A stream that has said nothing for longer than
 * this is dead even if the browser still thinks it is open: after a computer
 * sleeps, the proxy drops the connection but a receive-only EventSource never
 * notices, so the page would keep showing pre-sleep data indefinitely.
 */
const STALE_AFTER_MS = 65_000
/** On returning to the tab, reconnect sooner than the watchdog would. */
const STALE_ON_RETURN_MS = 35_000
/**
 * Waking from sleep needs no special case: the last message is then minutes
 * old, so the first tick after wake reconnects. Hidden tabs may only tick once
 * a minute (browser throttling), which is fine for the same reason.
 */
const WATCHDOG_MS = 10_000

const listeners = new Set<RawListener>()
let source: EventSource | null = null
let pauseCount = 0
let pendingWhilePaused: LiveEvent[] = []
let lostConnection = false
let lastMessageAt = 0
let watchdog: ReturnType<typeof setInterval> | null = null
let pageHooksInstalled = false
let wanted = false

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

function markAlive(): void {
  lastMessageAt = Date.now()
}

function openSource(): void {
  const es = new EventSource(withBase('/api/v2/events'))
  source = es
  markAlive()
  es.addEventListener('task-update', (raw) => {
    if (source !== es) return
    markAlive()
    const msg = raw as MessageEvent<string>
    let payload: LiveEvent | null = null
    try {
      payload = JSON.parse(msg.data || '{}') as LiveEvent
    } catch {
      payload = { type: 'task.updated' }
    }
    dispatch(payload)
  })
  es.addEventListener('ping', () => {
    if (source === es) markAlive()
  })
  // The browser reconnects on its own, but anything sent while the stream was
  // down is gone. After a reconnect, tell views to refetch what they show.
  es.addEventListener('error', () => {
    if (source === es) lostConnection = true
  })
  es.addEventListener('ready', () => {
    if (source !== es) return
    markAlive()
    if (!lostConnection) return
    lostConnection = false
    dispatch({ type: LIVE_RESYNC })
  })
}

/** Drop the current stream and open a fresh one; views refetch once it is up. */
function reconnect(): void {
  if (!wanted) return
  source?.close()
  source = null
  lostConnection = true
  openSource()
}

function isStale(limitMs: number, now = Date.now()): boolean {
  return !source || source.readyState === EventSource.CLOSED || now - lastMessageAt > limitMs
}

function tick(): void {
  if (isStale(STALE_AFTER_MS)) reconnect()
}

function onVisible(): void {
  if (document.visibilityState === 'visible' && isStale(STALE_ON_RETURN_MS)) reconnect()
}

function onPageShow(e: PageTransitionEvent): void {
  // Restored from the back/forward cache: the old stream is gone.
  if (e.persisted) reconnect()
}

function installPageHooks(): void {
  if (pageHooksInstalled || typeof window === 'undefined') return
  pageHooksInstalled = true
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('online', reconnect)
  window.addEventListener('pageshow', onPageShow)
}

function removePageHooks(): void {
  if (!pageHooksInstalled) return
  pageHooksInstalled = false
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('online', reconnect)
  window.removeEventListener('pageshow', onPageShow)
}

export function startLiveUpdates(): void {
  if (source || typeof EventSource === 'undefined') return
  wanted = true
  openSource()
  installPageHooks()
  if (!watchdog) watchdog = setInterval(tick, WATCHDOG_MS)
}

export function stopLiveUpdates(): void {
  wanted = false
  source?.close()
  source = null
  lostConnection = false
  if (watchdog) clearInterval(watchdog)
  watchdog = null
  removePageHooks()
}

/**
 * Bring every view up to date, e.g. after the server refused an action made
 * from out-of-date data. Reconnects first when the stream looks dead (views
 * refetch once it is back); otherwise tells views to refetch now.
 */
export function refreshLiveData(): void {
  if (wanted && isStale(STALE_ON_RETURN_MS)) {
    reconnect()
    return
  }
  dispatch({ type: LIVE_RESYNC })
}

export function subscribeLiveUpdates(handler: RawListener): () => void {
  listeners.add(handler)
  return () => {
    listeners.delete(handler)
  }
}

/**
 * True when this tab caused the event and has already applied the change.
 * The same user acting from another tab or device is not "own": that view
 * still needs to refresh.
 */
export function isOwnTabLiveEvent(event: LiveEvent, userId?: number | null): boolean {
  if (!userId || !event.actor_id || event.actor_id !== userId) return false
  return hasRecentOwnMutation()
}

/** True when every event in a debounced batch came from this tab's own writes. */
export function isOwnTabBatch(batch: LiveEvent[], userId?: number | null): boolean {
  return batch.length > 0 && batch.every((e) => isOwnTabLiveEvent(e, userId))
}

/**
 * Subscribe while the caller is mounted; debounce bursts of events.
 * Ceremony session events fire on every facilitator step and change no task lists,
 * so they only reach handlers that pass `{ ceremonies: true }`.
 */
export function useLiveUpdates(
  handler: LiveHandler,
  debounceMs = 200,
  opts: { ceremonies?: boolean } = {},
): void {
  let timer: ReturnType<typeof setTimeout> | null = null
  const queued: LiveEvent[] = []

  function flush(): void {
    timer = null
    if (queued.length === 0) return
    if (isLiveReloadPaused()) {
      // A drag started after these arrived: keep them and try again rather
      // than dropping them.
      timer = setTimeout(flush, debounceMs)
      return
    }
    const batch = coalesceLiveEvents(queued.splice(0))
    void handler(batch[batch.length - 1], batch)
  }

  const unsub = subscribeLiveUpdates((event) => {
    if (event.type === LIVE_CEREMONY_UPDATED && !opts.ceremonies) return
    queued.push(event)
    if (timer) return
    timer = setTimeout(flush, debounceMs)
  })

  onUnmounted(() => {
    unsub()
    if (timer) clearTimeout(timer)
  })
}
