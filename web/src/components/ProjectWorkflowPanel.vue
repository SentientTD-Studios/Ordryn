<template>
  <div class="workflow-panel">
    <h4 class="h6 mb-2">Board mode</h4>
    <p class="small text-muted mb-3">
      Kanban organizes tasks into status columns on a board. Turning board mode off is only allowed
      when the project has no tasks.
    </p>

    <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
      <div class="form-check form-switch m-0">
        <input
          :id="`kanban-toggle-${project.id}`"
          class="form-check-input"
          type="checkbox"
          :checked="isKanban"
          :disabled="savingMode || !isOwner"
          @change="onToggleMode(($event.target as HTMLInputElement).checked)"
        />
        <label class="form-check-label" :for="`kanban-toggle-${project.id}`">
          Enable kanban board
        </label>
      </div>
      <span v-if="isKanban" class="badge text-bg-primary">Kanban</span>
      <span v-else class="badge text-bg-secondary">Classic</span>
    </div>

    <template v-if="isKanban">
      <h4 class="h6">Statuses</h4>
      <p class="small text-muted mb-2">
        Up to {{ maxStatuses }} columns. Drag to set board column order. One must be default (new tasks)
        and at least one must be done (completed).
      </p>

      <ul ref="statusListEl" class="list-unstyled mb-3 status-reorder-list">
        <li
          v-for="(s, index) in statuses"
          :key="s.id"
          class="status-reorder-item d-flex flex-wrap align-items-center gap-2 mb-2 pb-2 border-bottom"
          :data-status-id="s.id"
        >
          <span
            v-if="isOwner"
            class="status-drag-handle text-muted"
            title="Drag to reorder"
            aria-label="Drag to reorder status"
          >
            <i class="bi bi-grip-vertical" />
          </span>

          <template v-if="renameId === s.id">
            <form class="d-flex flex-column gap-1 flex-grow-1" @submit.prevent="saveRename(s)">
              <div class="d-flex align-items-center gap-1 flex-wrap">
                <input
                  v-model="renameValue"
                  type="text"
                  class="form-control form-control-sm"
                  maxlength="40"
                  required
                  aria-label="Status name"
                />
                <button class="btn btn-sm btn-primary" type="submit" aria-label="Save status">
                  <i class="bi bi-check" />
                </button>
                <button
                  class="btn btn-sm btn-secondary"
                  type="button"
                  aria-label="Cancel edit"
                  @click="renameId = null"
                >
                  <i class="bi bi-x" />
                </button>
              </div>
              <input
                v-model="renameDescription"
                type="text"
                class="form-control form-control-sm"
                :maxlength="maxStatusDescription"
                placeholder="Short description (optional)"
                aria-label="Status description"
              />
              <div class="d-flex justify-content-between">
                <small class="form-hint">Max {{ maxStatusDescription }} characters</small>
                <small class="text-muted">{{ renameDescription.length }}/{{ maxStatusDescription }}</small>
              </div>
            </form>
          </template>
          <template v-else>
            <div class="min-w-0 flex-grow-1">
              <div class="d-flex align-items-center gap-1 flex-wrap">
                <strong class="me-1">{{ s.name }}</strong>
                <span v-if="s.is_default" class="badge text-bg-info">default</span>
                <span v-if="s.is_done" class="badge text-bg-success">done</span>
                <button
                  v-if="isOwner"
                  class="btn btn-sm btn-link p-0"
                  type="button"
                  aria-label="Edit status"
                  @click="beginRename(s)"
                >
                  <i class="bi bi-pencil" />
                </button>
              </div>
              <div v-if="s.description" class="small text-muted text-break">{{ s.description }}</div>
            </div>
          </template>

          <div v-if="isOwner" class="d-flex flex-wrap align-items-center gap-2 ms-auto">
            <div class="btn-group btn-group-sm" role="group" aria-label="Move status">
              <button
                type="button"
                class="btn btn-outline-secondary"
                :disabled="index === 0 || reordering"
                title="Move up"
                aria-label="Move status up"
                @click="moveStatus(index, -1)"
              >
                <i class="bi bi-arrow-up" />
              </button>
              <button
                type="button"
                class="btn btn-outline-secondary"
                :disabled="index === statuses.length - 1 || reordering"
                title="Move down"
                aria-label="Move status down"
                @click="moveStatus(index, 1)"
              >
                <i class="bi bi-arrow-down" />
              </button>
            </div>
            <div class="form-check form-check-inline m-0">
              <input
                :id="`default-${s.id}`"
                class="form-check-input"
                type="checkbox"
                :checked="s.is_default"
                :disabled="s.is_default"
                @change="setDefault(s)"
              />
              <label class="form-check-label small" :for="`default-${s.id}`">Default</label>
            </div>
            <div class="form-check form-check-inline m-0">
              <input
                :id="`done-${s.id}`"
                class="form-check-input"
                type="checkbox"
                :checked="s.is_done"
                @change="toggleDone(s, ($event.target as HTMLInputElement).checked)"
              />
              <label class="form-check-label small" :for="`done-${s.id}`">Done</label>
            </div>
            <button
              class="btn btn-sm btn-outline-danger"
              type="button"
              :disabled="statuses.length <= 1 || s.is_default"
              title="Delete status"
              @click="askDelete(s)"
            >
              <i class="bi bi-trash" />
            </button>
          </div>
        </li>
      </ul>

      <form v-if="isOwner && statuses.length < maxStatuses" class="row g-2 align-items-end mb-3" @submit.prevent="addStatus">
        <div class="col-sm-6">
          <label class="form-label small mb-0" :for="`new-status-${project.id}`">Add status</label>
          <input
            :id="`new-status-${project.id}`"
            v-model="newStatusName"
            type="text"
            class="form-control form-control-sm"
            maxlength="40"
            required
            placeholder="e.g. In review"
          />
        </div>
        <div class="col-sm-6">
          <label class="form-label small mb-0" :for="`new-status-desc-${project.id}`">Description</label>
          <input
            :id="`new-status-desc-${project.id}`"
            v-model="newStatusDescription"
            type="text"
            class="form-control form-control-sm"
            :maxlength="maxStatusDescription"
            placeholder="What this column is for"
          />
          <div class="d-flex justify-content-between">
            <small class="form-hint">Max {{ maxStatusDescription }} characters</small>
            <small class="text-muted">{{ newStatusDescription.length }}/{{ maxStatusDescription }}</small>
          </div>
        </div>
        <div class="col-sm-6">
          <div class="form-check">
            <input
              :id="`new-status-done-${project.id}`"
              v-model="newStatusDone"
              class="form-check-input"
              type="checkbox"
            />
            <label class="form-check-label small" :for="`new-status-done-${project.id}`">Done column</label>
          </div>
        </div>
        <div class="col-sm-6">
          <button class="btn btn-sm btn-primary w-100" type="submit" :disabled="adding">Add</button>
        </div>
      </form>
      <p v-else-if="isOwner" class="small text-muted mb-3">Maximum of {{ maxStatuses }} statuses reached.</p>

      <div v-if="deleteTarget" class="border rounded p-2 bg-body mb-2">
        <p class="small mb-2">
          Delete <strong>{{ deleteTarget.name }}</strong>?
          <span v-if="moveOptions.length">
            Tasks in this column (if any) will move to the status you select.
          </span>
        </p>
        <div v-if="moveOptions.length" class="mb-2">
          <label class="form-label small mb-0" for="move-to-status">Move tasks to</label>
          <select id="move-to-status" v-model.number="moveToStatusId" class="form-select form-select-sm">
            <option :value="0" disabled>Select status…</option>
            <option v-for="opt in moveOptions" :key="opt.id" :value="opt.id">{{ opt.name }}</option>
          </select>
        </div>
        <div class="d-flex gap-2">
          <button class="btn btn-sm btn-danger" type="button" :disabled="deleting" @click="confirmDelete">
            Delete
          </button>
          <button class="btn btn-sm btn-outline-secondary" type="button" @click="cancelDelete">Cancel</button>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Sortable from 'sortablejs'
import { api } from '@/api/client'
import type { Project, ProjectStatus } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import { useAuth } from '@/composables/useAuth'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: []; 'columns-changed': [] }>()

const maxStatuses = 8
const maxStatusDescription = 50
const statuses = ref<ProjectStatus[]>([])
const savingMode = ref(false)
const adding = ref(false)
const deleting = ref(false)
const reordering = ref(false)
const newStatusName = ref('')
const newStatusDescription = ref('')
const newStatusDone = ref(false)
const renameId = ref<number | null>(null)
const renameValue = ref('')
const renameDescription = ref('')
const deleteTarget = ref<ProjectStatus | null>(null)
const moveToStatusId = ref(0)
const statusListEl = ref<HTMLElement | null>(null)
const toast = useToast()
const { user } = useAuth()
let sortable: Sortable | null = null

const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const isOwner = computed(() => (props.project.role || 'owner') === 'owner')
const moveOptions = computed(() =>
  statuses.value.filter((s) => s.id !== deleteTarget.value?.id),
)

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

function collectOrderedIds(el: HTMLElement): number[] {
  return Array.from(el.querySelectorAll(':scope > .status-reorder-item'))
    .map((node) => parseInt((node as HTMLElement).dataset.statusId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

async function persistOrder(orderedIds: number[]) {
  if (reordering.value) return
  const current = statuses.value.map((s) => s.id)
  if (
    orderedIds.length !== current.length ||
    orderedIds.every((id, i) => id === current[i])
  ) {
    return
  }
  const previous = [...statuses.value]
  const byId = new Map(statuses.value.map((s) => [s.id, s]))
  statuses.value = orderedIds.map((id) => byId.get(id)!).filter(Boolean)
  reordering.value = true
  try {
    await api.reorderProjectStatuses(props.project.id, orderedIds)
    emit('columns-changed')
  } catch (err) {
    statuses.value = previous
    toast.push(err instanceof APIError ? err.message : 'Could not reorder statuses', 'error')
    await nextTick()
    initSortable()
  } finally {
    reordering.value = false
  }
}

function initSortable() {
  destroySortable()
  if (!statusListEl.value || !isKanban.value || !isOwner.value || statuses.value.length < 2) return
  sortable = Sortable.create(statusListEl.value, {
    handle: '.status-drag-handle',
    draggable: '.status-reorder-item',
    animation: 150,
    onEnd(evt) {
      const el = evt.to as HTMLElement
      void persistOrder(collectOrderedIds(el))
    },
  })
}

async function moveStatus(index: number, delta: number) {
  const next = index + delta
  if (next < 0 || next >= statuses.value.length) return
  const ordered = statuses.value.map((s) => s.id)
  const [id] = ordered.splice(index, 1)
  ordered.splice(next, 0, id)
  await persistOrder(ordered)
  await nextTick()
  initSortable()
}

async function loadStatuses() {
  if (!isKanban.value) {
    statuses.value = []
    destroySortable()
    return
  }
  try {
    statuses.value = await api.listProjectStatuses(props.project.id)
    await nextTick()
    initSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load statuses', 'error')
  }
}

async function onToggleMode(enabled: boolean) {
  savingMode.value = true
  try {
    await api.updateProject(props.project.id, {
      workflow_mode: enabled ? 'kanban' : 'classic',
    })
    toast.push(enabled ? 'Kanban board enabled' : 'Board mode disabled', 'success')
    emit('changed')
    if (enabled) await loadStatuses()
    else statuses.value = []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update workflow mode', 'error')
  } finally {
    savingMode.value = false
  }
}

function beginRename(s: ProjectStatus) {
  renameId.value = s.id
  renameValue.value = s.name
  renameDescription.value = s.description || ''
  deleteTarget.value = null
}

async function saveRename(s: ProjectStatus) {
  const name = renameValue.value.trim()
  if (!name) return
  try {
    const updated = await api.updateProjectStatus(props.project.id, s.id, {
      name,
      description: renameDescription.value.trim(),
    })
    statuses.value = statuses.value.map((row) => (row.id === updated.id ? updated : row))
    renameId.value = null
    toast.push('Status updated', 'success')
    emit('columns-changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function setDefault(s: ProjectStatus) {
  if (s.is_default) return
  try {
    const updated = await api.updateProjectStatus(props.project.id, s.id, { is_default: true })
    statuses.value = statuses.value.map((row) => ({
      ...row,
      is_default: row.id === updated.id,
    }))
    toast.push('Default status updated', 'success')
    emit('columns-changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await loadStatuses()
  }
}

async function toggleDone(s: ProjectStatus, isDone: boolean) {
  try {
    const updated = await api.updateProjectStatus(props.project.id, s.id, { is_done: isDone })
    statuses.value = statuses.value.map((row) => (row.id === updated.id ? updated : row))
    toast.push('Status updated', 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await loadStatuses()
  }
}

async function addStatus() {
  const name = newStatusName.value.trim()
  if (!name) return
  adding.value = true
  try {
    const created = await api.createProjectStatus(props.project.id, {
      name,
      description: newStatusDescription.value.trim(),
      is_done: newStatusDone.value,
    })
    statuses.value = [...statuses.value, created]
    newStatusName.value = ''
    newStatusDescription.value = ''
    newStatusDone.value = false
    toast.push('Status added', 'success')
    emit('columns-changed')
    await nextTick()
    initSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not add status', 'error')
  } finally {
    adding.value = false
  }
}

function askDelete(s: ProjectStatus) {
  deleteTarget.value = s
  renameId.value = null
  const first = statuses.value.find((x) => x.id !== s.id)
  moveToStatusId.value = first?.id ?? 0
}

function cancelDelete() {
  deleteTarget.value = null
  moveToStatusId.value = 0
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    const moveTo = moveToStatusId.value > 0 ? moveToStatusId.value : undefined
    await api.deleteProjectStatus(props.project.id, deleteTarget.value.id, moveTo)
    toast.push('Status deleted', 'info')
    cancelDelete()
    await loadStatuses()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  } finally {
    deleting.value = false
  }
}

watch(
  () => [props.project.id, props.project.workflow_mode] as const,
  () => {
    cancelDelete()
    renameId.value = null
    renameDescription.value = ''
    void loadStatuses()
  },
)

onMounted(loadStatuses)

useLiveUpdates((event) => {
  if (event.type !== 'project.updated' && event.type !== 'project.created' && event.type !== 'project.deleted') return
  if (event.project_id && event.project_id !== props.project.id) return
  if (renameId.value != null || deleteTarget.value) return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  void loadStatuses()
})

onBeforeUnmount(() => {
  destroySortable()
})
</script>

<style scoped>
.status-drag-handle {
  cursor: grab;
  display: inline-flex;
  align-items: center;
  padding: 0.15rem 0.2rem;
  opacity: 0.65;
}
.status-drag-handle:active {
  cursor: grabbing;
}
.status-reorder-item {
  user-select: none;
}
</style>
