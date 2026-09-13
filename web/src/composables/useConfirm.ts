import { reactive } from 'vue'

export type ConfirmOptions = {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  warning?: boolean
}

export type UnsavedChoice = 'save' | 'discard' | 'stay'

export type UnsavedOptions = {
  title?: string
  message: string
  saveLabel?: string
  discardLabel?: string
  stayLabel?: string
}

type ConfirmState = {
  open: boolean
  mode: 'confirm' | 'unsaved'
  title: string
  message: string
  confirmLabel: string
  cancelLabel: string
  discardLabel: string
  danger: boolean
  warning: boolean
  resolve: ((value: boolean) => void) | null
  resolveUnsaved: ((value: UnsavedChoice) => void) | null
}

const state = reactive<ConfirmState>({
  open: false,
  mode: 'confirm',
  title: 'Confirm',
  message: '',
  confirmLabel: 'Confirm',
  cancelLabel: 'Cancel',
  discardLabel: 'Discard',
  danger: false,
  warning: false,
  resolve: null,
  resolveUnsaved: null,
})

function settleExisting() {
  if (state.resolve) {
    state.resolve(false)
    state.resolve = null
  }
  if (state.resolveUnsaved) {
    state.resolveUnsaved('stay')
    state.resolveUnsaved = null
  }
}

export function useConfirm() {
  function askConfirm(options: ConfirmOptions): Promise<boolean> {
    settleExisting()
    state.mode = 'confirm'
    state.title = options.title || 'Confirm'
    state.message = options.message
    state.confirmLabel = options.confirmLabel || 'Confirm'
    state.cancelLabel = options.cancelLabel || 'Cancel'
    state.discardLabel = 'Discard'
    state.danger = !!options.danger
    state.warning = !!options.warning
    state.open = true
    return new Promise((resolve) => {
      state.resolve = resolve
    })
  }

  function askUnsaved(options: UnsavedOptions): Promise<UnsavedChoice> {
    settleExisting()
    state.mode = 'unsaved'
    state.title = options.title || 'Unsaved changes'
    state.message = options.message
    state.confirmLabel = options.saveLabel || 'Save'
    state.cancelLabel = options.stayLabel || 'Stay'
    state.discardLabel = options.discardLabel || 'Discard'
    state.danger = false
    state.warning = false
    state.open = true
    return new Promise((resolve) => {
      state.resolveUnsaved = resolve
    })
  }

  function accept() {
    state.open = false
    if (state.mode === 'unsaved') {
      state.resolveUnsaved?.('save')
    } else {
      state.resolve?.(true)
    }
    state.resolve = null
    state.resolveUnsaved = null
  }

  function cancel() {
    state.open = false
    if (state.mode === 'unsaved') {
      state.resolveUnsaved?.('stay')
    } else {
      state.resolve?.(false)
    }
    state.resolve = null
    state.resolveUnsaved = null
  }

  function discard() {
    state.open = false
    state.resolveUnsaved?.('discard')
    state.resolve = null
    state.resolveUnsaved = null
  }

  return { state, askConfirm, askUnsaved, accept, cancel, discard }
}
