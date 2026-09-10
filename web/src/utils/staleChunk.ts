import type { Router } from 'vue-router'

export const STALE_CHUNK_RELOAD_KEY = 'ordryn:stale-chunk-reload'

export function isStaleChunkError(err: unknown): boolean {
  const msg =
    err && typeof err === 'object' && 'message' in err
      ? String((err as { message: unknown }).message)
      : String(err)
  return (
    /Failed to fetch dynamically imported module/i.test(msg) ||
    /error loading dynamically imported module/i.test(msg) ||
    /Importing a module script failed/i.test(msg) ||
    /Unable to preload CSS for/i.test(msg)
  )
}

type ReloadStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function defaultStorage(): ReloadStorage | null {
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

/** Reload once when a Vite/Vue lazy chunk is missing (typical after a deploy). */
export function reloadOnceForStaleChunk(opts?: {
  storage?: ReloadStorage | null
  reload?: () => void
}): boolean {
  const storage = opts && 'storage' in opts ? opts.storage : defaultStorage()
  const reload = opts?.reload ?? (() => window.location.reload())
  try {
    if (storage?.getItem(STALE_CHUNK_RELOAD_KEY) === '1') {
      return false
    }
    storage?.setItem(STALE_CHUNK_RELOAD_KEY, '1')
  } catch {
    // Private mode / blocked storage: still attempt one reload.
  }
  reload()
  return true
}

export function clearStaleChunkReloadGuard(storage?: ReloadStorage | null): void {
  const s = storage === undefined ? defaultStorage() : storage
  try {
    s?.removeItem(STALE_CHUNK_RELOAD_KEY)
  } catch {
    /* ignore */
  }
}

/** Recover from stale hashed chunks after a new SPA deploy. */
export function installStaleChunkReload(router: Router): void {
  window.addEventListener('vite:preloadError', (event) => {
    event.preventDefault()
    reloadOnceForStaleChunk()
  })
  router.onError((err) => {
    if (isStaleChunkError(err)) {
      reloadOnceForStaleChunk()
    }
  })
  router.afterEach(() => {
    clearStaleChunkReloadGuard()
  })
}
