<script setup lang="ts">
import { useToast } from '@/composables/useToast'

const { toasts, dismiss } = useToast()
</script>

<template>
  <div class="toasts" aria-live="polite">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="toast"
      :class="t.tone"
      role="status"
    >
      <span>{{ t.message }}</span>
      <button type="button" aria-label="Dismiss" @click="dismiss(t.id)">×</button>
    </div>
  </div>
</template>
