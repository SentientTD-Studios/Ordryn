<script setup lang="ts">
import { useToast } from '@/composables/useToast'

const { toasts, dismiss } = useToast()

function alertClass(tone: string) {
  if (tone === 'success') return 'alert-success'
  if (tone === 'error') return 'alert-danger'
  return 'alert-info'
}
</script>

<template>
  <div class="position-fixed bottom-0 end-0 p-3" style="z-index: 1080" aria-live="polite">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="alert mb-2 shadow"
      :class="alertClass(t.tone)"
      role="alert"
    >
      <div class="d-flex justify-content-between align-items-start gap-2">
        <span>{{ t.message }}</span>
        <button type="button" class="btn-close" aria-label="Dismiss" @click="dismiss(t.id)" />
      </div>
    </div>
  </div>
</template>
