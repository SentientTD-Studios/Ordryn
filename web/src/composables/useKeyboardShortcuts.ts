import { ref } from 'vue'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useConfirm } from '@/composables/useConfirm'

export type TaskShortcutHandlers = {
  newTask: () => void
  focusSearch: () => void
  getFocusableTaskIds: () => number[]
  editTask: (id: number) => void
  deleteTask: (id: number) => void
  toggleComplete: (id: number) => void
}

const focusedTaskId = ref<number | null>(null)
let taskHandlers: TaskShortcutHandlers | null = null
let listenerAttached = false

const BOOTSTRAP_MODAL_IDS = ['shortcutsModal', 'changelogModal']

type BootstrapModal = {
  show: () => void
  hide: () => void
}

type BootstrapNS = {
  Modal: {
    getOrCreateInstance: (el: HTMLElement) => BootstrapModal
    getInstance: (el: HTMLElement) => BootstrapModal | null
  }
}

function getBootstrap(): BootstrapNS | undefined {
  return (window as unknown as { bootstrap?: BootstrapNS }).bootstrap
}

function isTypingTarget(el: EventTarget | null): boolean {
  if (!el || !(el instanceof Element)) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (el instanceof HTMLElement && el.isContentEditable) return true
  return !!el.closest("[contenteditable='true']")
}

function isHelpKey(e: KeyboardEvent): boolean {
  return e.key === '?' || (e.code === 'Slash' && e.shiftKey)
}

function isSearchKey(e: KeyboardEvent): boolean {
  return e.code === 'Slash' && !e.shiftKey
}

function openShortcutsModal() {
  const active = document.activeElement
  if (
    active instanceof HTMLElement &&
    (active.id === 'task-search' || active.closest('.ordryn-filter-bar'))
  ) {
    active.blur()
  }
  const el = document.getElementById('shortcutsModal')
  const bs = getBootstrap()
  if (!el || !bs?.Modal) return
  bs.Modal.getOrCreateInstance(el).show()
}

function closeOpenModals() {
  const bs = getBootstrap()
  if (!bs?.Modal) return
  for (const id of BOOTSTRAP_MODAL_IDS) {
    const el = document.getElementById(id)
    if (!el) continue
    const inst = bs.Modal.getInstance(el)
    if (inst) inst.hide()
  }
}

function focusTaskCardElement(id: number) {
  const el = document.getElementById(`task-card-${id}`)
  if (!(el instanceof HTMLElement)) return
  if (!el.hasAttribute('tabindex')) el.tabIndex = -1
  el.focus({ preventScroll: true })
  el.scrollIntoView({ block: 'nearest' })
}

function setFocusedTaskId(id: number | null) {
  focusedTaskId.value = id
  if (id != null) focusTaskCardElement(id)
}

function moveFocus(delta: 1 | -1) {
  const ids = taskHandlers?.getFocusableTaskIds() ?? []
  if (ids.length === 0) {
    setFocusedTaskId(null)
    return
  }
  const current = focusedTaskId.value
  let idx = current == null ? -1 : ids.indexOf(current)
  if (idx < 0) {
    setFocusedTaskId(delta > 0 ? ids[0]! : ids[ids.length - 1]!)
    return
  }
  idx = (idx + delta + ids.length) % ids.length
  setFocusedTaskId(ids[idx]!)
}

function onKeydown(e: KeyboardEvent) {
  if (e.ctrlKey || e.metaKey || e.altKey) return

  const typing = isTypingTarget(document.activeElement)

  if (e.code === 'Escape') {
    if (useConfirm().state.open) return
    e.preventDefault()
    e.stopPropagation()
    closeOpenModals()
    void useTaskSidebar().requestClose()
    const active = document.activeElement
    if (active instanceof HTMLElement && isTypingTarget(active)) {
      active.blur()
    }
    return
  }

  // Help should work from most UI focus targets; only skip real text entry.
  if (isHelpKey(e)) {
    if (typing) {
      const active = document.activeElement
      const inTextarea =
        active instanceof HTMLTextAreaElement ||
        (active instanceof HTMLElement && active.isContentEditable)
      if (inTextarea) return
      // Allow ? from empty single-line inputs; keep typing ? when the field has content.
      if (active instanceof HTMLInputElement && active.value.trim() !== '') return
    }
    e.preventDefault()
    openShortcutsModal()
    return
  }

  if (typing || e.defaultPrevented) return

  if (!taskHandlers) return

  if (isSearchKey(e)) {
    e.preventDefault()
    taskHandlers.focusSearch()
    return
  }

  if (e.code === 'KeyN') {
    e.preventDefault()
    taskHandlers.newTask()
    return
  }

  const ids = taskHandlers.getFocusableTaskIds()
  if (ids.length === 0) return

  if (e.code === 'KeyJ') {
    e.preventDefault()
    moveFocus(1)
    return
  }

  if (e.code === 'KeyK') {
    e.preventDefault()
    moveFocus(-1)
    return
  }

  const focused = focusedTaskId.value
  if (focused == null || !ids.includes(focused)) return

  if (e.code === 'Enter' || e.code === 'KeyE') {
    e.preventDefault()
    taskHandlers.editTask(focused)
    return
  }

  if (e.code === 'KeyD') {
    e.preventDefault()
    taskHandlers.deleteTask(focused)
    return
  }

  if (e.code === 'KeyX') {
    e.preventDefault()
    taskHandlers.toggleComplete(focused)
  }
}

export function useKeyboardShortcuts() {
  function initKeyboardShortcuts() {
    if (listenerAttached) return
    window.addEventListener('keydown', onKeydown, true)
    listenerAttached = true
  }

  function destroyKeyboardShortcuts() {
    if (!listenerAttached) return
    window.removeEventListener('keydown', onKeydown, true)
    listenerAttached = false
  }

  function registerTaskShortcuts(handlers: TaskShortcutHandlers) {
    taskHandlers = handlers
  }

  function unregisterTaskShortcuts() {
    taskHandlers = null
    focusedTaskId.value = null
  }

  return {
    focusedTaskId,
    openShortcutsModal,
    closeOpenModals,
    initKeyboardShortcuts,
    destroyKeyboardShortcuts,
    registerTaskShortcuts,
    unregisterTaskShortcuts,
  }
}
