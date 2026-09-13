<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import { useConfirm } from '@/composables/useConfirm'

const { state, accept, cancel, discard } = useConfirm()

function onKeydown(e: KeyboardEvent) {
  if (!state.open) return
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopImmediatePropagation()
    cancel()
  }
}

function syncBodyModalState(open: boolean) {
  const taskOpen = !!document.getElementById('taskModal')
  if (open || taskOpen) {
    document.body.classList.add('modal-open')
    document.body.style.overflow = 'hidden'
    return
  }
  document.body.classList.remove('modal-open')
  document.body.style.overflow = ''
}

watch(
  () => state.open,
  (open) => {
    syncBodyModalState(open)
  },
)

onMounted(() => {
  // Clear any leftover Bootstrap modal body state from prior broken attempts.
  document.body.classList.remove('modal-open')
  document.body.style.overflow = ''
  document.body.style.paddingRight = ''
  window.addEventListener('keydown', onKeydown, true)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown, true)
  document.body.classList.remove('modal-open')
  document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="state.open"
      id="siteConfirmModal"
      class="modal fade show d-block site-confirm-modal"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      aria-labelledby="siteConfirmModalLabel"
    >
      <div class="modal-dialog modal-dialog-centered" @click.stop>
        <div class="modal-content">
          <div class="modal-header">
            <h5 id="siteConfirmModalLabel" class="modal-title">{{ state.title }}</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="cancel" />
          </div>
          <div class="modal-body">
            <p class="mb-0" style="white-space: pre-wrap">{{ state.message }}</p>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="cancel">
              {{ state.cancelLabel }}
            </button>
            <button
              v-if="state.mode === 'unsaved'"
              type="button"
              class="btn btn-warning"
              @click="discard"
            >
              {{ state.discardLabel }}
            </button>
            <button
              type="button"
              class="btn"
              :class="state.danger ? 'btn-danger' : state.warning ? 'btn-warning' : 'btn-primary'"
              @click="accept"
            >
              {{ state.confirmLabel }}
            </button>
          </div>
        </div>
      </div>
    </div>
    <div v-if="state.open" class="modal-backdrop fade show site-confirm-backdrop" />
  </Teleport>
</template>

<style>
.site-confirm-modal {
  z-index: 2100;
}
.site-confirm-backdrop {
  z-index: 2090;
}
</style>
