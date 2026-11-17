import { ref } from 'vue'

// One palette for the whole app; Ctrl/⌘+K toggles it (see AppShell).
const open = ref(false)

export function useCommandPalette() {
  return {
    open,
    show: () => {
      open.value = true
    },
    close: () => {
      open.value = false
    },
    toggle: () => {
      open.value = !open.value
    },
  }
}
