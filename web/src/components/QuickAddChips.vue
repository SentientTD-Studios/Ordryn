<script setup lang="ts">
import type { QuickAddChip } from '@/utils/quickAdd'

defineProps<{ chips: QuickAddChip[] }>()

const ICONS: Record<QuickAddChip['kind'], string> = {
  due: 'bi-calendar-event',
  priority: 'bi-flag',
  tag: 'bi-tag',
  project: 'bi-folder2',
  claim: 'bi-person-check',
}
</script>

<template>
  <div v-if="chips.length" class="quick-add-chips d-flex flex-wrap gap-1" aria-live="polite">
    <span
      v-for="chip in chips"
      :key="`${chip.kind}:${chip.label}`"
      class="badge rounded-pill quick-add-chip"
      :class="`quick-add-chip-${chip.kind}`"
    >
      <i class="bi me-1" :class="ICONS[chip.kind]" aria-hidden="true" />{{ chip.label }}
    </span>
  </div>
</template>

<style scoped>
.quick-add-chip {
  font-weight: 500;
  background: var(--ordryn-card-bg, #f8f9fa);
  color: var(--bs-body-color);
  border: 1px solid var(--ordryn-card-border, #dee2e6);
}
.quick-add-chip-due {
  border-color: var(--bs-primary);
}
.quick-add-chip-priority {
  border-color: var(--bs-warning);
}
.quick-add-chip-claim {
  border-color: var(--bs-success);
}
</style>
