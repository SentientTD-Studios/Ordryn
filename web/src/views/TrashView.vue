<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { TrashEntry } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useLiveUpdates } from '@/composables/useLiveUpdates'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const { askConfirm } = useConfirm()
const entries = ref<TrashEntry[]>([])
const loading = ref(true)
const busyId = ref<number | null>(null)
const emptying = ref(false)

const isEmpty = computed(() => !loading.value && entries.value.length === 0)

async function load() {
  try {
    entries.value = (await api.listTrash()).tasks
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load the trash', 'error')
  } finally {
    loading.value = false
  }
}

function formatWhen(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function daysLeft(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (Number.isNaN(ms)) return ''
  const days = Math.ceil(ms / 86_400_000)
  if (days <= 0) return 'Deleted soon'
  return days === 1 ? '1 day left' : `${days} days left`
}

function subtaskNote(e: TrashEntry): string {
  if (e.subtask_count <= 0) return ''
  return e.subtask_count === 1 ? 'with 1 subtask' : `with ${e.subtask_count} subtasks`
}

async function restore(e: TrashEntry) {
  busyId.value = e.id
  try {
    await api.restoreTrashedTask(e.id)
    entries.value = entries.value.filter((x) => x.id !== e.id)
    toast.push(`Restored “${e.title}”`, 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Restore failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function purge(e: TrashEntry) {
  const ok = await askConfirm({
    title: 'Delete forever?',
    message: `Permanently delete “${e.title}”${e.subtask_count > 0 ? ` and its ${subtaskNote(e).replace('with ', '')}` : ''}? This can't be undone.`,
    confirmLabel: 'Delete forever',
    danger: true,
  })
  if (!ok) return
  busyId.value = e.id
  try {
    await api.purgeTrashedTask(e.id)
    entries.value = entries.value.filter((x) => x.id !== e.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function emptyAll() {
  const ok = await askConfirm({
    title: 'Empty the trash?',
    message: `Permanently delete all ${entries.value.length} item${entries.value.length === 1 ? '' : 's'} in your trash? This can't be undone.`,
    confirmLabel: 'Empty trash',
    danger: true,
  })
  if (!ok) return
  emptying.value = true
  try {
    const res = await api.emptyTrash()
    toast.push(`Deleted ${res.deleted} item${res.deleted === 1 ? '' : 's'} for good`, 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Empty failed', 'error')
  } finally {
    emptying.value = false
  }
}

// Deletes and restores elsewhere (other tabs, teammates) change this list.
useLiveUpdates((event) => {
  if (event.type === 'task.deleted' || event.type === 'task.restored') void load()
}, 500)

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="container mt-3 mb-5">
    <div class="d-flex flex-wrap justify-content-between align-items-start gap-2 mb-4">
      <div>
        <h1 class="h3 mb-1">
          <i class="bi bi-trash3 me-2" aria-hidden="true" />
          Trash
        </h1>
        <p class="text-muted mb-0">
          Deleted tasks stay here until they're permanently deleted. Restoring brings back everything: comments,
          links, fields, history, and subtasks.
        </p>
      </div>
      <div class="d-flex gap-2">
        <button
          v-if="entries.length > 0"
          type="button"
          class="btn btn-outline-danger btn-sm"
          :disabled="emptying"
          @click="emptyAll"
        >
          <i class="bi bi-trash3 me-1" />{{ emptying ? 'Emptying…' : 'Empty trash' }}
        </button>
        <RouterLink to="/" class="btn btn-outline-secondary btn-sm">
          <i class="bi bi-arrow-left" /> Back to tasks
        </RouterLink>
      </div>
    </div>

    <div v-if="loading" class="text-muted">Loading…</div>

    <div v-else-if="isEmpty" class="text-center text-muted py-5 border rounded-3">
      <i class="bi bi-trash3 display-6 opacity-50" aria-hidden="true" />
      <p class="mt-3 mb-0">The trash is empty.</p>
    </div>

    <div v-else class="card">
      <ul class="list-group list-group-flush">
        <li v-for="e in entries" :key="e.id" class="list-group-item d-flex flex-wrap align-items-center gap-3 py-3">
          <div class="flex-grow-1 min-w-0">
            <div class="fw-semibold text-break">{{ e.title }}</div>
            <div class="small text-muted">
              <span v-if="e.project_name"><i class="bi bi-folder me-1" />{{ e.project_name }}</span>
              <span v-else><i class="bi bi-inbox me-1" />No project</span>
              <span v-if="e.parent_title"> · Subtask of {{ e.parent_title }}</span>
              <span v-if="subtaskNote(e)"> · {{ subtaskNote(e) }}</span>
            </div>
            <div class="small text-muted">
              Deleted {{ formatWhen(e.deleted_at) }}<span v-if="e.deleted_by"> by {{ e.deleted_by }}</span>
              · <span :title="`Permanently deleted ${formatWhen(e.purge_at)}`">{{ daysLeft(e.purge_at) }}</span>
            </div>
          </div>
          <div class="d-flex gap-2">
            <button
              type="button"
              class="btn btn-sm btn-primary"
              :disabled="busyId === e.id"
              @click="restore(e)"
            >
              <i class="bi bi-arrow-counterclockwise me-1" />Restore
            </button>
            <button
              type="button"
              class="btn btn-sm btn-outline-danger"
              :disabled="busyId === e.id"
              :aria-label="`Delete ${e.title} forever`"
              @click="purge(e)"
            >
              Delete forever
            </button>
          </div>
        </li>
      </ul>
    </div>
  </div>
</template>
