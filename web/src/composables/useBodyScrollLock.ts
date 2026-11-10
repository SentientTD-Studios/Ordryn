import { onScopeDispose, watch, type WatchSource } from 'vue'
import { acquireScrollLock } from '@/utils/bodyScrollLock'

/** Locks page scrolling while `active` is true; releases on unmount. */
export function useBodyScrollLock(active: WatchSource<boolean>) {
  let release: (() => void) | null = null

  function set(locked: boolean) {
    if (locked && !release) {
      release = acquireScrollLock(document.body)
    } else if (!locked && release) {
      release()
      release = null
    }
  }

  watch(active, (locked) => set(!!locked), { immediate: true })
  onScopeDispose(() => set(false))
}
