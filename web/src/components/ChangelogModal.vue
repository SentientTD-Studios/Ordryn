<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ChangelogEntry } from '@/api/types'
import ChangelogEntryList from '@/components/ChangelogEntryList.vue'

const MAX_MODAL = 5

const loading = ref(false)
const error = ref('')
const entries = ref<ChangelogEntry[]>([])
const showAll = ref(false)

const visibleEntries = computed(() =>
  showAll.value ? entries.value : entries.value.slice(0, MAX_MODAL),
)
const hasMore = computed(() => !showAll.value && entries.value.length > MAX_MODAL)

async function loadChangelog() {
  loading.value = true
  error.value = ''
  showAll.value = false
  try {
    entries.value = await api.changelog()
  } catch {
    entries.value = []
    error.value = 'Unable to load changelog.'
  } finally {
    loading.value = false
  }
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
          <div v-else>
            <ChangelogEntryList :entries="visibleEntries" />
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
