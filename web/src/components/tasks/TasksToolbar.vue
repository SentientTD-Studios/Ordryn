<script setup lang="ts">
import { ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { withBase } from '@/base'

const props = defineProps<{
  total: number
  completedCount: number
  incompleteCount: number
  project: Project | null
  showSelectToggle: boolean
  selecting: boolean
  canAddTasks: boolean
  canUndo: boolean
  /** Query-string suffix for the current filters (starts with & or is empty). */
  exportQuery: string
}>()

const emit = defineEmits<{
  'toggle-select': []
  add: []
  undo: []
}>()

const toast = useToast()

const watching = ref<boolean | null>(null)
watch(
  () => props.project?.id,
  async (id) => {
    watching.value = null
    if (!id) return
    try {
      const res = await api.getProjectWatch(id)
      if (props.project?.id === id) watching.value = res.watching
    } catch {
      watching.value = null
    }
  },
  { immediate: true },
)

async function toggleWatch() {
  const id = props.project?.id
  if (!id || watching.value === null) return
  try {
    watching.value = (await api.setProjectWatch(id, !watching.value)).watching
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not update project watch', 'error')
  }
}

async function exportTasks(format: 'json' | 'csv') {
  try {
    const res = await fetch(withBase(`/api/v2/export?format=${format}${props.exportQuery}`), { credentials: 'include' })
    if (!res.ok) throw new Error('Export failed')
    const blob = await res.blob()
    const match = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') || '')
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = match?.[1] || `gotodo-export.${format}`
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    toast.push('Export failed', 'error')
  }
}
</script>

<template>
  <div class="d-flex align-items-center justify-content-between flex-wrap gap-2 mb-2">
    <div class="d-flex align-items-center gap-1 flex-wrap text-muted small ordryn-task-stats">
      <span class="badge rounded-pill bg-primary bg-opacity-10 text-primary border border-primary border-opacity-20 px-2 py-1">
        {{ total }} <span class="d-none d-sm-inline">tasks</span>
      </span>
      <span class="badge rounded-pill bg-success bg-opacity-10 text-success border border-success border-opacity-20 px-2 py-1">
        {{ completedCount }} <span class="d-none d-sm-inline">done</span>
      </span>
      <span class="badge rounded-pill bg-warning bg-opacity-10 text-warning border border-warning border-opacity-20 px-2 py-1">
        {{ incompleteCount }} <span class="d-none d-sm-inline">open</span>
      </span>
      <span
        v-if="project?.role && project.role !== 'owner'"
        class="badge rounded-pill bg-info bg-opacity-10 text-info border border-info border-opacity-20 px-2 py-1"
      >
        {{ project.role_name || project.role }}
      </span>
      <span v-if="project?.archived" class="badge rounded-pill bg-secondary bg-opacity-10 text-secondary border border-secondary border-opacity-20 px-2 py-1">
        Archived
      </span>
      <button
        v-if="project && watching !== null"
        type="button"
        class="btn btn-sm py-0 px-2"
        :class="watching ? 'btn-primary' : 'btn-outline-secondary'"
        :aria-pressed="watching"
        :title="watching ? 'Stop watching every task in this project' : 'Get notified about changes to every task in this project'"
        @click="toggleWatch"
      >
        <i class="bi" :class="watching ? 'bi-eye-fill' : 'bi-eye'" />
        {{ watching ? 'Watching project' : 'Watch project' }}
      </button>
    </div>

    <div class="d-flex align-items-center gap-2">
      <button
        v-if="showSelectToggle"
        type="button"
        class="btn btn-sm btn-outline-secondary rounded-pill px-2 py-1 d-md-none"
        :class="{ active: selecting }"
        :aria-pressed="selecting"
        title="Select tasks"
        @click="emit('toggle-select')"
      >
        <i class="bi bi-check2-square" />
      </button>
      <div class="dropdown">
        <button
          class="btn btn-sm btn-outline-secondary dropdown-toggle rounded-pill px-2 px-sm-3 py-1"
          type="button"
          data-bs-toggle="dropdown"
        >
          <i class="bi bi-arrow-down-up" />
          <span class="d-none d-sm-inline ms-1">Import / Export</span>
        </button>
        <ul class="dropdown-menu dropdown-menu-end shadow-sm border-0">
          <li>
            <RouterLink class="dropdown-item small" to="/import">
              <i class="bi bi-upload me-2" />Import CSV
            </RouterLink>
          </li>
          <li>
            <RouterLink class="dropdown-item small" to="/settings#integrations">
              <i class="bi bi-calendar3 me-2" />Calendar Sync (ICS)
            </RouterLink>
          </li>
          <li><hr class="dropdown-divider" /></li>
          <li>
            <button type="button" class="dropdown-item small" @click="exportTasks('csv')">
              <i class="bi bi-download me-2" />Export Filtered CSV
            </button>
          </li>
          <li>
            <button type="button" class="dropdown-item small" @click="exportTasks('json')">
              <i class="bi bi-download me-2" />Export Filtered JSON
            </button>
          </li>
        </ul>
      </div>

      <slot name="actions" />

      <button
        v-if="canAddTasks"
        type="button"
        class="btn btn-sm btn-success rounded-pill px-3 py-1 shadow-xs d-none d-md-flex align-items-center gap-1"
        @click="emit('add')"
      >
        <i class="bi bi-plus-lg" />
        <span>Add Task</span>
      </button>

      <button
        v-if="canUndo"
        type="button"
        class="btn btn-sm btn-outline-warning rounded-pill px-3 py-1"
        @click="emit('undo')"
      >
        <i class="bi bi-arrow-counterclockwise me-1" />Undo
      </button>
    </div>
  </div>
</template>
