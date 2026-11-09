<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { SavedView } from '@/api/types'
import { audienceIcon, audienceLabel } from '@/utils/queryDefinition'

/** Count tiles for the queries the user pinned to their dashboard. */
const props = defineProps<{ savedViews: SavedView[]; refreshKey?: number }>()

const pinned = computed(() => props.savedViews.filter((v) => v.on_dashboard))
const counts = ref<Record<number, number | null>>({})

async function loadCounts() {
  const next: Record<number, number | null> = {}
  await Promise.all(
    pinned.value.map(async (v) => {
      try {
        next[v.id] = (await api.savedViewResults(v.id, 1, -1)).total
      } catch {
        next[v.id] = null
      }
    }),
  )
  counts.value = next
}

watch(
  () => [pinned.value.map((v) => v.id).join(','), props.refreshKey],
  () => void loadCounts(),
  { immediate: true },
)
</script>

<template>
  <section v-if="pinned.length" class="mb-4" aria-labelledby="pinned-queries-heading">
    <div class="d-flex align-items-center justify-content-between mb-2">
      <h2 id="pinned-queries-heading" class="h6 fw-bold mb-0">Pinned queries</h2>
      <RouterLink to="/views" class="small">Manage</RouterLink>
    </div>
    <div class="row g-3">
      <div v-for="v in pinned" :key="v.id" class="col-6 col-md-4 col-xl-3">
        <RouterLink :to="`/views/${v.id}`" class="card border-0 shadow-xs h-100 text-decoration-none text-reset">
          <div class="card-body py-3">
            <div class="small text-muted fw-medium text-truncate" :title="v.name">
              <i :class="v.shared ? `${audienceIcon(v)} me-1` : 'bi bi-funnel me-1'" aria-hidden="true" />{{ v.name }}
            </div>
            <div class="fs-3 fw-bold">
              <span v-if="counts[v.id] === undefined" class="placeholder col-3" aria-label="Loading" />
              <span v-else-if="counts[v.id] === null" class="text-muted small" title="Could not run this query">—</span>
              <template v-else>{{ counts[v.id] }}</template>
            </div>
            <div v-if="v.shared" class="small text-muted text-truncate">{{ audienceLabel(v) }}</div>
          </div>
        </RouterLink>
      </div>
    </div>
  </section>
</template>
