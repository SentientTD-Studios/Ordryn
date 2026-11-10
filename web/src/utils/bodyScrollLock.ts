// Reference-counted page scroll lock shared by every hand-rolled modal.
//
// Each modal used to toggle `body.style.overflow` on its own, so one modal
// closing could unlock the page under another, or (worse) a modal could
// leave the page locked after the modal that "owned" the lock had already
// closed, hiding every scrollbar until a refresh. Holding a counted lock per
// open modal means the page unlocks exactly when the last one closes.

export type ScrollLockTarget = {
  classList: { add(token: string): void; remove(token: string): void }
  style: { overflow: string }
}

let holders = 0

function apply(target: ScrollLockTarget) {
  if (holders > 0) {
    target.classList.add('modal-open')
    target.style.overflow = 'hidden'
  } else {
    target.classList.remove('modal-open')
    target.style.overflow = ''
  }
}

/** Returns a release function; calling it more than once is a no-op. */
export function acquireScrollLock(target: ScrollLockTarget): () => void {
  holders += 1
  apply(target)
  let released = false
  return () => {
    if (released) return
    released = true
    holders = Math.max(0, holders - 1)
    apply(target)
  }
}

export function scrollLockHolders(): number {
  return holders
}
