/**
 * Tracks writes this browser tab sends, so a live event caused by this tab
 * can be told apart from one caused by the same user somewhere else (another
 * tab, device, or the chat integration). Focus alone can't tell them apart:
 * a second computer left on the board is usually the focused window there.
 */

/** How long after a write finishes its live event still counts as this tab's. */
export const OWN_MUTATION_WINDOW_MS = 4000

let inFlight = 0
let lastSettledAt = 0

export function isMutatingMethod(method?: string): boolean {
  const m = (method || 'GET').toUpperCase()
  return m !== 'GET' && m !== 'HEAD' && m !== 'OPTIONS'
}

/** Call when a write starts; call the returned function when it settles. */
export function beginOwnMutation(): () => void {
  inFlight += 1
  let done = false
  return () => {
    if (done) return
    done = true
    inFlight = Math.max(0, inFlight - 1)
    lastSettledAt = Date.now()
  }
}

/** True while this tab has a write in flight or finished one moments ago. */
export function hasRecentOwnMutation(now = Date.now()): boolean {
  return inFlight > 0 || now - lastSettledAt < OWN_MUTATION_WINDOW_MS
}

/** Test hook. */
export function resetOwnMutations(): void {
  inFlight = 0
  lastSettledAt = 0
}
