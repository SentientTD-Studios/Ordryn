<script setup lang="ts">
import { ref } from 'vue'
import type { ChangelogEntry } from '@/api/types'

defineProps<{
  entries: ChangelogEntry[]
}>()

const expanded = ref<Record<number, boolean>>({})

function toggleEntry(idx: number) {
  expanded.value = { ...expanded.value, [idx]: !expanded.value[idx] }
}
</script>

<template>
  <div class="changelog-list">
    <div v-for="(entry, idx) in entries" :key="`${entry.version}-${idx}`" class="card mb-3">
      <div class="card-body">
        <button
          type="button"
          class="btn btn-link text-start w-100 p-0 d-flex align-items-center"
          style="text-decoration: none"
          @click="toggleEntry(idx)"
        >
          <span class="chev me-2">{{ expanded[idx] ? '▼' : '►' }}</span>
          <div class="flex-grow-1 text-start">
            <strong>{{ entry.title }}</strong>
            <span
              class="badge releasetag ms-3"
              :class="entry.prerelease ? 'bg-warning text-dark' : 'bg-success'"
            >
              {{ entry.prerelease ? 'Prerelease' : 'Release' }} • {{ entry.date }}
            </span>
          </div>
        </button>
        <div v-show="expanded[idx]" class="mt-2">
          <div v-if="entry.html" class="changelog-entry-body" v-html="entry.html" />
          <ul v-else-if="entry.notes?.length">
            <li v-for="(note, nIdx) in entry.notes" :key="nIdx">{{ note }}</li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
