<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ChangelogEntry } from '@/api/types'

const MAX_MODAL = 5

const loading = ref(false)
const error = ref('')
const entries = ref<ChangelogEntry[]>([])
const expanded = ref<Record<number, boolean>>({})
const showAll = ref(false)

const visibleEntries = computed(() =>
  showAll.value ? entries.value : entries.value.slice(0, MAX_MODAL),
)

const hasMore = computed(() => !showAll.value && entries.value.length > MAX_MODAL)

async function loadChangelog() {
  loading.value = true
  error.value = ''
  showAll.value = false
  expanded.value = {}
  try {
    entries.value = await api.changelog()
  } catch {
    entries.value = []
    error.value = 'Unable to load changelog.'
  } finally {
    loading.value = false
  }
}

function toggleEntry(idx: number) {
  expanded.value = { ...expanded.value, [idx]: !expanded.value[idx] }
}

function onShow() {
  void loadChangelog()
}

let modalEl: HTMLElement | null = null

onMounted(() => {
  modalEl = document.getElementById('changelogModal')
  modalEl?.addEventListener('show.bs.modal', onShow)
})

onUnmounted(() => {
  modalEl?.removeEventListener('show.bs.modal', onShow)
})
</script>

<template>
  <div
    id="changelogModal"
    class="modal fade"
    tabindex="-1"
    aria-labelledby="changelogModalLabel"
    aria-hidden="true"
  >
    <div class="modal-dialog modal-lg modal-dialog-scrollable">
      <div class="modal-content">
        <div class="modal-header">
          <h5 id="changelogModalLabel" class="modal-title">Change Log</h5>
          <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close" />
        </div>
        <div id="changelog-body" class="modal-body">
          <div v-if="loading" class="text-center text-muted">Loading...</div>
          <div v-else-if="error" class="text-danger">{{ error }}</div>
          <div v-else-if="entries.length === 0" class="text-center text-muted">
            No changelog entries available.
          </div>
          <div v-else class="changelog-list">
            <div v-for="(entry, idx) in visibleEntries" :key="`${entry.version}-${idx}`" class="card mb-3">
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
                  <div
                    v-if="entry.html"
                    class="changelog-entry-body"
                    v-html="entry.html"
                  />
                  <ul v-else-if="entry.notes?.length">
                    <li v-for="(note, nIdx) in entry.notes" :key="nIdx">{{ note }}</li>
                  </ul>
                </div>
              </div>
            </div>
            <div v-if="hasMore" class="text-center mt-3">
              <button type="button" class="btn btn-link" @click="showAll = true">
                View full changelog
              </button>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>
