<template>
  <div class="kanban-board">
    <div v-if="loading" class="text-center py-4 text-muted">
      <div class="spinner-border spinner-border-sm me-2" role="status" />
      Loading board…
    </div>

    <div
      v-else-if="!statuses.length"
      class="text-center py-5 rounded-3 border shadow-xs"
      style="background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
    >
      <i class="bi bi-kanban display-4 text-muted opacity-50" />
      <h4 class="mt-3 fw-bold">No status columns yet</h4>
      <p class="text-muted mb-0">Open project Board settings to add statuses.</p>
    </div>

    <div
      v-else
      class="kanban-columns d-flex gap-3 overflow-auto pb-2"
    >
      <div
        v-for="col in statuses"
        :key="col.id"
        class="kanban-column flex-shrink-0"
        role="region"
        :aria-label="columnAriaLabel(col)"
      >
        <div class="kanban-column-header mb-2 px-1">
          <div class="d-flex align-items-center justify-content-between gap-2">
            <div class="d-flex align-items-center gap-1 min-w-0">
              <strong class="kanban-column-title text-truncate">{{ col.name }}</strong>
              <span
                v-if="col.is_default"
                class="ordryn-badge ordryn-badge-status text-nowrap"
                title="Default status for new tasks"
              >
                default
              </span>
              <span
                v-if="col.is_done"
                class="ordryn-badge text-nowrap"
                style="background: var(--badge-status-bg, rgba(25, 135, 84, 0.12)); color: var(--ordryn-accent, #198754);"
                title="Done column"
              >
                done
              </span>
            </div>
            <span class="kanban-column-count">{{ tasksForStatus(col.id).length }}</span>
          </div>
          <p
            v-if="col.description"
            class="kanban-column-description mb-0"
            :title="col.description"
          >
            {{ col.description }}
          </p>
        </div>

        <div class="kanban-column-body-wrap">
          <div
            v-if="!tasksForStatus(col.id).length"
            class="kanban-column-empty"
            aria-hidden="true"
          >
            {{ canDrag ? 'Drop tasks here' : 'No tasks' }}
          </div>
          <div
            :ref="(el) => setColumnEl(col.id, el)"
            class="kanban-column-body"
            :data-status-id="col.id"
          >
            <div
              v-for="task in tasksForStatus(col.id)"
              :key="task.id"
              class="kanban-card ordryn-task-card"
              :class="[
                density === 'dense' ? 'density-dense' : 'density-comfortable',
                { 'kanban-card-readonly': !canDrag },
                { 'kanban-card-selected': isSelected(task.id) },
              ]"
              :data-task-id="task.id"
            >
            <div class="d-flex align-items-start gap-1">
              <span
                v-if="canDrag"
                class="drag-handle text-muted flex-shrink-0 d-inline-flex align-items-center justify-content-center"
                title="Drag to move"
                role="button"
                tabindex="-1"
                aria-label="Drag to move"
              >
                <i class="bi bi-grip-vertical" />
              </span>
              <div
                v-if="canDrag"
                class="hover-reveal flex-shrink-0 align-items-center justify-content-center m-0 p-0"
                :class="selecting || isSelected(task.id) ? 'd-inline-flex is-visible' : 'd-none d-md-inline-flex'"
                title="Select task for bulk actions"
              >
                <input
                  type="checkbox"
                  class="cursor-pointer m-0 p-0"
                  :checked="isSelected(task.id)"
                  style="width: 1.05rem; height: 1.05rem; cursor: pointer; accent-color: var(--ordryn-accent);"
                  :aria-label="`Select task ${task.title}`"
                  @click.stop
                  @change="emit('toggle-select', task.id, ($event.target as HTMLInputElement).checked)"
                />
              </div>

              <div class="flex-grow-1 min-w-0">
                <button
                  type="button"
                  class="btn btn-link text-start text-decoration-none p-0 kanban-card-title task-title fw-semibold"
                  @click="emit('open-task', task.id)"
                >
                  {{ task.title }}
                </button>
                <button
                  v-if="task.parent_id"
                  type="button"
                  class="btn btn-link kanban-subtask-link text-start text-decoration-none p-0 d-block"
                  @click.stop="openParent(task)"
                >
                  Subtask of {{ parentTitleFor(task) }}
                </button>

                <div class="d-flex flex-wrap gap-1 mt-1 align-items-center">
                  <span
                    v-if="!task.parent_id && childCount(task)"
                    class="ordryn-badge text-nowrap"
                    style="background: var(--ordryn-muted-bg); color: var(--ordryn-muted);"
                    :title="`${childCount(task)} subtask${childCount(task) === 1 ? '' : 's'}`"
                  >
                    {{ childCount(task) }} subtask{{ childCount(task) === 1 ? '' : 's' }}
                  </span>
                  <span
                    v-if="priorityLabel(task.priority)"
                    class="ordryn-badge text-nowrap"
                    :class="{
                      'ordryn-badge-priority-low': task.priority === 1,
                      'ordryn-badge-priority-med': task.priority === 2,
                      'ordryn-badge-priority-high': task.priority === 3,
                    }"
                  >
                    {{ priorityLabel(task.priority) }}
                  </span>

                  <span
                    v-if="task.due_date"
                    class="kanban-meta text-nowrap"
                    :class="{ 'is-overdue': isOverdue(task.due_date) }"
                    title="Due date"
                  >
                    <i class="bi bi-calendar-event opacity-75" />
                    {{ formatDueDate(task.due_date) }}
                  </span>

                  <span
                    v-if="task.sprint_name"
                    class="ordryn-badge text-nowrap"
                    style="background: var(--ordryn-muted-bg); color: var(--ordryn-muted);"
                    title="Sprint"
                  >
                    <i class="bi bi-flag me-1" />{{ task.sprint_name }}
                  </span>

                  <span
                    v-if="task.estimate_points != null"
                    class="ordryn-badge text-nowrap"
                    style="background: var(--ordryn-muted-bg); color: var(--ordryn-muted);"
                    title="Estimate"
                  >
                    {{ task.estimate_points }} pts
                  </span>

                  <span
                    v-if="task.claimed_by"
                    class="ordryn-badge text-nowrap text-bg-primary"
                    title="Claimed by"
                  >
                    <i class="bi bi-person me-1" />{{ claimerLabel(task) }}
                  </span>

                  <span
                    v-for="tag in task.tags || []"
                    :key="tag.id"
                    class="tag-chip"
                    :style="{ backgroundColor: tag.color || '#6c757d' }"
                    :title="tag.name"
                  >{{ tag.name }}</span>
                  <TaskFieldBadges :task="task" surface="kanban" />
                </div>

                <div v-if="canDrag" class="kanban-claim-row mt-2">
                  <button
                    v-if="!task.claimed_by || task.claimed_by !== user?.id"
                    type="button"
                    class="btn btn-sm btn-outline-primary py-0 px-2 hover-reveal"
                    :class="{ 'is-visible': claimingId === task.id }"
                    :disabled="claimingId === task.id"
                    @click.stop="claimTask(task)"
                  >
                    {{ task.claimed_by ? 'Take over' : 'Claim' }}
                  </button>
                  <button
                    v-else
                    type="button"
                    class="btn btn-sm btn-outline-secondary py-0 px-2 hover-reveal"
                    :class="{ 'is-visible': claimingId === task.id }"
                    :disabled="claimingId === task.id"
                    @click.stop="unclaimTask(task)"
                  >
                    Release
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Sortable from 'sortablejs'
import { api } from '@/api/client'
import type { ProjectStatus, Task } from '@/api/types'
import TaskFieldBadges from '@/components/TaskFieldBadges.vue'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { pauseLiveReload, resumeLiveReload, useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import type { ViewDensity } from '@/composables/useViewDensity'

const props = withDefaults(
  defineProps<{
    projectId: number
    tasks: Task[]
    role?: 'owner' | 'editor' | 'viewer'
    density?: ViewDensity
    /** Bump to reload column definitions (rename/add/delete status). */
    columnsRev?: number
    /** Board sprint key: `backlog` or a sprint id string. */
    sprintFilter?: string
    selectedIds?: number[]
    selecting?: boolean
  }>(),
  {
    density: 'comfortable',
    columnsRev: 0,
    sprintFilter: '',
    selectedIds: () => [],
    selecting: false,
  },
)

const emit = defineEmits<{
  'open-task': [id: number]
  changed: []
  'task-updated': [task: Task]
  'board-reorder': [payload: { statusId: number; taskIds: number[] }]
  'toggle-select': [id: number, checked: boolean]
}>()

const toast = useToast()
const { user } = useAuth()
const statuses = ref<ProjectStatus[]>([])
const loading = ref(true)
const claimingId = ref<number | null>(null)
const columnEls = new Map<number, HTMLElement>()
const sortables: Sortable[] = []
let dragPaused = false

function beginBoardDrag() {
  if (!dragPaused) {
    pauseLiveReload()
    dragPaused = true
  }
}

function endBoardDrag() {
  if (!dragPaused) return
  resumeLiveReload()
  dragPaused = false
}

const canDrag = computed(() => props.role !== 'viewer')

const parentTitleById = computed(() => {
  const titles = new Map<number, string>()
  for (const t of props.tasks) {
    if (!t.parent_id) titles.set(t.id, t.title)
    if (t.parent_id && t.parent_title) titles.set(t.parent_id, t.parent_title)
    for (const child of t.children || []) {
      if (child.parent_id && child.parent_title) titles.set(child.parent_id, child.parent_title)
    }
  }
  return titles
})

function isSelected(id: number) {
  return props.selectedIds.includes(id)
}

function matchesBoardSprint(task: Task): boolean {
  const key = props.sprintFilter
  if (!key) return true
  const assigned = task.sprint_id && task.sprint_id > 0 ? task.sprint_id : 0
  if (key === 'backlog') return assigned === 0
  return assigned === parseInt(key, 10)
}

const boardTasks = computed(() => {
  const out: Task[] = []
  const seen = new Set<number>()
  const take = (task: Task) => {
    if (seen.has(task.id) || task.project_id !== props.projectId || !matchesBoardSprint(task)) return
    out.push(task)
    seen.add(task.id)
  }
  for (const t of props.tasks) {
    if (t.parent_id) {
      take(t)
      continue
    }
    take(t)
    for (const child of t.children || []) take(child)
  }
  return out
})

function parentTitleFor(task: Task): string {
  if (!task.parent_id) return ''
  return parentTitleById.value.get(task.parent_id) || task.parent_title || `Task #${task.parent_id}`
}

function childCount(task: Task): number {
  return task.child_count ?? task.children?.length ?? 0
}

function openParent(task: Task) {
  if (task.parent_id) emit('open-task', task.parent_id)
}

function claimerLabel(task: Task) {
  if (!task.claimed_by) return 'Unclaimed'
  if (user.value?.id && task.claimed_by === user.value.id) return 'You'
  return task.claimed_by_name || `User #${task.claimed_by}`
}

function priorityLabel(p: number) {
  if (p === 3) return 'High'
  if (p === 2) return 'Med'
  if (p === 1) return 'Low'
  return ''
}

function formatDueDate(dateStr?: string): string {
  if (!dateStr) return ''
  const todayStr = new Date().toISOString().slice(0, 10)
  const currentYear = new Date().getFullYear().toString()
  const isPast = dateStr < todayStr
  const year = dateStr.slice(0, 4)
  if (isPast || year !== currentYear) {
    return dateStr
  }
  const dateObj = new Date(dateStr + 'T00:00:00')
  return dateObj.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function isOverdue(dateStr?: string): boolean {
  if (!dateStr) return false
  return dateStr < new Date().toISOString().slice(0, 10)
}

async function claimTask(task: Task) {
  claimingId.value = task.id
  try {
    const updated = await api.claimTask(task.id)
    emit('task-updated', updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not claim task', 'error')
    emit('changed')
  } finally {
    claimingId.value = null
  }
}

async function unclaimTask(task: Task) {
  claimingId.value = task.id
  try {
    const updated = await api.unclaimTask(task.id)
    emit('task-updated', updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not release task', 'error')
    emit('changed')
  } finally {
    claimingId.value = null
  }
}

function setColumnEl(statusId: number, el: unknown) {
  if (el instanceof HTMLElement) {
    columnEls.set(statusId, el)
  } else {
    columnEls.delete(statusId)
  }
}

const statusIdSet = computed(() => new Set(statuses.value.map((s) => s.id)))

const defaultStatusId = computed(() => {
  const def = statuses.value.find((s) => s.is_default)
  return def?.id ?? statuses.value[0]?.id ?? 0
})

function tasksForStatus(statusId: number): Task[] {
  const known = statusIdSet.value
  return boardTasks.value.filter((t) => {
    // Place tasks with missing/unknown status into the default column so they
    // never disappear from the board after workflow changes.
    if (!t.status_id || !known.has(t.status_id)) {
      return statusId === defaultStatusId.value
    }
    return t.status_id === statusId
  })
}

function columnAriaLabel(col: ProjectStatus): string {
  const count = tasksForStatus(col.id).length
  const desc = (col.description || '').trim()
  if (desc) return `${col.name}: ${desc}, ${count} tasks`
  return `${col.name}, ${count} tasks`
}

async function loadStatuses(quiet = false) {
  if (!quiet) loading.value = true
  try {
    statuses.value = await api.listProjectStatuses(props.projectId)
  } catch (err) {
    if (!quiet) {
      toast.push(err instanceof APIError ? err.message : 'Failed to load board columns', 'error')
      statuses.value = []
    }
  } finally {
    if (!quiet) loading.value = false
    await nextTick()
    initSortables()
  }
}

function destroySortables() {
  endBoardDrag()
  while (sortables.length) {
    sortables.pop()?.destroy()
  }
}

function collectIds(container: HTMLElement): number[] {
  return Array.from(container.querySelectorAll(':scope > .kanban-card'))
    .map((el) => parseInt((el as HTMLElement).dataset.taskId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

async function onCardDrop(evt: Sortable.SortableEvent) {
  const to = evt.to as HTMLElement
  const from = evt.from as HTMLElement
  const statusId = parseInt(to.dataset.statusId || '', 10)
  if (Number.isNaN(statusId)) return

  const taskId = parseInt((evt.item as HTMLElement).dataset.taskId || '', 10)
  const fromStatusId = parseInt(from.dataset.statusId || '', 10)
  const orderedIds = collectIds(to)
  const statusChanged = !Number.isNaN(taskId) && fromStatusId !== statusId

  const rootIds = orderedIds.filter((id) => {
    const t = boardTasks.value.find((task) => task.id === id)
    return !!t && !t.parent_id
  })

  // Optimistic local update before API round-trip
  emit('board-reorder', { statusId, taskIds: orderedIds })

  try {
    const current = !Number.isNaN(taskId) ? boardTasks.value.find((t) => t.id === taskId) : undefined
    if (statusChanged) {
      const col = statuses.value.find((s) => s.id === statusId)
      if (current && col) {
        emit('task-updated', {
          ...current,
          status_id: statusId,
          status_name: col.name,
          completed: col.is_done,
        })
      }
      // Reorder is root-only; persist subtask column moves via PATCH.
      if (current?.parent_id) {
        await api.patchTask(taskId, { status_id: statusId })
      }
    }
    if (rootIds.length) {
      const byId = new Map(boardTasks.value.map((t) => [t.id, t]))
      const favoriteIds = rootIds.filter((id) => byId.get(id)?.favorite)
      const regularIds = rootIds.filter((id) => !byId.get(id)?.favorite)
      const reorderPayload = {
        status_id: statusId,
        project: String(props.projectId),
      }
      if (favoriteIds.length) {
        await api.reorderTasks({
          task_ids: favoriteIds,
          favorite: true,
          ...reorderPayload,
        })
      }
      if (regularIds.length) {
        await api.reorderTasks({
          task_ids: regularIds,
          favorite: false,
          ...reorderPayload,
        })
      }
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update board', 'error')
    emit('changed')
  }
}

function initSortables() {
  destroySortables()
  if (!canDrag.value) return
  const coarse = window.matchMedia('(pointer: coarse)').matches
  for (const el of columnEls.values()) {
    sortables.push(
      Sortable.create(el, {
        group: 'kanban',
        handle: '.drag-handle',
        draggable: '.kanban-card',
        animation: 150,
        delay: coarse ? 200 : 0,
        delayOnTouchOnly: true,
        touchStartThreshold: coarse ? 5 : 1,
        emptyInsertThreshold: 24,
        ghostClass: 'kanban-sortable-ghost',
        chosenClass: 'kanban-sortable-chosen',
        dragClass: 'kanban-sortable-drag',
        onStart() {
          beginBoardDrag()
        },
        onEnd(evt) {
          void onCardDrop(evt).finally(() => {
            endBoardDrag()
          })
        },
      }),
    )
  }
}

watch(
  () => props.projectId,
  () => {
    void loadStatuses()
  },
)

watch(
  () => props.columnsRev,
  (rev, prev) => {
    if (rev && rev !== prev) void loadStatuses(true)
  },
)

useLiveUpdates((event) => {
  if (event.type !== 'project.updated') return
  if (event.project_id && event.project_id !== props.projectId) return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  void loadStatuses(true)
})

watch(
  () => [props.tasks, canDrag.value] as const,
  async () => {
    await nextTick()
    initSortables()
  },
  { deep: true },
)

onMounted(() => {
  void loadStatuses()
})

onBeforeUnmount(destroySortables)
</script>

<style scoped>
.kanban-columns {
  scroll-snap-type: x mandatory;
  -webkit-overflow-scrolling: touch;
}

.kanban-column {
  width: min(280px, 85vw);
  scroll-snap-align: start;
  background: var(--ordryn-muted-bg, var(--bs-tertiary-bg, #f8f9fa));
  border: 1px solid var(--ordryn-card-border, var(--bs-border-color, #dee2e6));
  border-radius: 12px;
  padding: 0.5rem;
  min-height: 12rem;
  display: flex;
  flex-direction: column;
  max-height: min(70vh, 42rem);
}

.kanban-column-header {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--ordryn-muted-bg, var(--bs-tertiary-bg, #f8f9fa));
  padding-top: 0.15rem;
  padding-bottom: 0.15rem;
}

.kanban-column-title {
  font-size: 0.85rem;
  color: var(--ordryn-text, inherit);
  letter-spacing: 0.01em;
}

.kanban-column-description {
  font-size: 0.7rem;
  color: var(--ordryn-muted, #6c757d);
  line-height: 1.3;
  margin-top: 0.2rem;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.kanban-column-count {
  flex-shrink: 0;
  min-width: 1.5rem;
  height: 1.5rem;
  padding: 0 0.45rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--ordryn-muted, #6c757d);
  background: var(--ordryn-card-bg, #fff);
  border: 1px solid var(--ordryn-card-border, #dee2e6);
}

.kanban-column-body-wrap {
  position: relative;
  flex: 1 1 auto;
  min-height: 8rem;
  display: flex;
  flex-direction: column;
}

.kanban-column-body {
  min-height: 8rem;
  flex: 1 1 auto;
  overflow-y: auto;
  position: relative;
  z-index: 1;
}

.kanban-column-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  font-size: 0.8rem;
  color: var(--ordryn-muted, #6c757d);
  border: 1px dashed var(--ordryn-card-border, #dee2e6);
  border-radius: 10px;
  margin: 0.15rem 0;
  min-height: 7rem;
  z-index: 0;
}

/* Slim board cards reuse Ordryn task-card tokens via shared class name */
.kanban-card.ordryn-task-card {
  cursor: default;
  margin-bottom: 0.5rem;
  background-color: var(--ordryn-card-bg);
  border: 1px solid var(--ordryn-card-border);
  border-radius: 12px;
  box-shadow: var(--ordryn-card-shadow);
  transition: transform 0.15s ease, box-shadow 0.15s ease, border-color 0.15s ease;
  position: relative;
  overflow: visible;
}

.kanban-card.density-comfortable {
  padding: 0.65rem 0.7rem;
}

.kanban-card.density-dense {
  padding: 0.4rem 0.55rem;
}

.kanban-card:hover {
  box-shadow: var(--ordryn-card-hover-shadow);
  border-color: var(--ordryn-card-hover-border);
}

.kanban-card-selected {
  border-color: var(--ordryn-accent, #0d6efd) !important;
  box-shadow: 0 0 0 1px var(--ordryn-accent, #0d6efd);
}

.kanban-card .drag-handle {
  cursor: grab;
  opacity: 0.5;
  line-height: 1;
  padding: 0.15rem 0;
  min-width: 1.25rem;
  min-height: 1.5rem;
}

.kanban-card .drag-handle i {
  font-size: 1.15rem;
  line-height: 1;
}

.kanban-card-title {
  white-space: normal;
  word-break: break-word;
  font-size: 0.9rem;
  line-height: 1.3;
  color: var(--ordryn-text) !important;
}

.kanban-subtask-link {
  font-size: 0.75rem;
  line-height: 1.3;
  color: var(--ordryn-muted, #6c757d) !important;
}

.kanban-card.density-dense .kanban-card-title {
  font-size: 0.85rem;
}

.kanban-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  color: var(--ordryn-muted);
}

.kanban-meta.is-overdue {
  color: var(--bs-danger, #dc3545);
  font-weight: 600;
}

.kanban-card-readonly {
  opacity: 0.98;
}

.kanban-claim-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 1.5rem;
}

/* Sortable visual states */
:deep(.kanban-sortable-ghost) {
  opacity: 0.45;
  border-style: dashed !important;
  border-color: var(--ordryn-accent, #0d6efd) !important;
  box-shadow: none !important;
}

:deep(.kanban-sortable-chosen) {
  border-color: var(--ordryn-accent, #0d6efd) !important;
  box-shadow: var(--ordryn-card-hover-shadow);
}

:deep(.kanban-sortable-drag) {
  opacity: 0.95;
}

@media (pointer: coarse) {
  .kanban-card .drag-handle {
    min-width: 1.75rem;
    min-height: 2rem;
    opacity: 0.7;
  }

  .kanban-claim-row .btn {
    min-height: 2rem;
    padding-left: 0.65rem !important;
    padding-right: 0.65rem !important;
  }

  /* Always show claim on touch devices (no hover) */
  .kanban-card .hover-reveal {
    opacity: 1 !important;
    pointer-events: auto !important;
  }
}
</style>
