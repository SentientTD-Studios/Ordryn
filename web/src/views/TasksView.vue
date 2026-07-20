<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project, SavedView, Tag, Task } from '@/api/types'
import { APIError } from '@/api/types'
import TaskTableRow from '@/components/TaskTableRow.vue'
import { useAuth } from '@/composables/useAuth'
import { useInfiniteScroll } from '@/composables/useInfiniteScroll'
import { useTaskListFilters } from '@/composables/useTaskListFilters'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useTaskSortable } from '@/composables/useTaskSortable'
import { useToast } from '@/composables/useToast'

const { openAdd, openEdit, lastSavedTask } = useTaskSidebar()

const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const savedViews = ref<SavedView[]>([])
const total = ref(0)
const loadedPage = ref(0)
const totalPages = ref(1)
const completedCount = ref(0)
const incompleteCount = ref(0)
const search = ref('')
const loading = ref(true)
const loadingMore = ref(false)
const toast = useToast()
const { user } = useAuth()
const {
  filters,
  hasActiveFilters,
  toApiParams,
  toExportQuery,
  setFilter,
  clearFilters: resetFilters,
  applySavedView: applySavedViewFilters,
} = useTaskListFilters()
const undoToken = ref<string | null>(null)
const selected = ref<number[]>([])
const bulkProjectId = ref('')
const bulkTagId = ref('')
const bulkPriority = ref('0')
const bulkDueDate = ref('')
const favoriteListEl = ref<HTMLElement | null>(null)
const taskListEl = ref<HTMLElement | null>(null)
const loadMoreSentinel = ref<HTMLElement | null>(null)

const favoriteTasks = computed(() => tasks.value.filter((t) => t.favorite))
const regularTasks = computed(() => tasks.value.filter((t) => !t.favorite))
const allSelected = computed(
  () => tasks.value.length > 0 && selected.value.length === tasks.value.length,
)
const isSearching = computed(() => filters.search !== '')
const showTaskTable = computed(
  () => total.value > 0 || favoriteTasks.value.length > 0 || hasActiveFilters.value,
)
const hasMore = computed(() => loadedPage.value < totalPages.value)
const sortableEnabled = computed(() => filters.sort !== 'priority' && !loading.value)
const showFavoriteList = computed(() => favoriteTasks.value.length > 0)

function taskMatchesCurrentFilters(task: Task): boolean {
  if (!taskMatchesStatusFilter(task)) return false
  if (filters.project === '0' && task.project_id != null) return false
  if (filters.project && filters.project !== '0') {
    const pid = parseInt(filters.project, 10)
    if (!Number.isNaN(pid) && task.project_id !== pid) return false
  }
  if (filters.priority && String(task.priority) !== filters.priority) return false
  if (filters.tag) {
    const tagId = parseInt(filters.tag, 10)
    if (!task.tags?.some((t) => t.id === tagId)) return false
  }
  if (filters.search) {
    const q = filters.search.toLowerCase()
    const hay = `${task.title} ${task.description}`.toLowerCase()
    if (!hay.includes(q)) return false
  }
  return true
}

function registerTaskAdded(task: Task) {
  total.value += 1
  if (task.completed) completedCount.value += 1
  else incompleteCount.value += 1
  if (!taskMatchesCurrentFilters(task) || tasks.value.some((t) => t.id === task.id)) return
  if (task.favorite) {
    tasks.value = [task, ...tasks.value]
  } else {
    tasks.value = [...tasks.value, task]
  }
}

function removeTaskLocally(task: Task) {
  if (!tasks.value.some((t) => t.id === task.id)) return
  tasks.value = tasks.value.filter((t) => t.id !== task.id)
  total.value = Math.max(0, total.value - 1)
  if (task.completed) completedCount.value = Math.max(0, completedCount.value - 1)
  else incompleteCount.value = Math.max(0, incompleteCount.value - 1)
  selected.value = selected.value.filter((id) => id !== task.id)
}

function taskMatchesStatusFilter(task: Task) {
  if (filters.status === 'complete') return task.completed
  if (filters.status === 'incomplete') return !task.completed
  return true
}

function adjustCompletionCounts(wasCompleted: boolean, isCompleted: boolean) {
  if (wasCompleted === isCompleted) return
  if (isCompleted) {
    completedCount.value += 1
    incompleteCount.value = Math.max(0, incompleteCount.value - 1)
  } else {
    completedCount.value = Math.max(0, completedCount.value - 1)
    incompleteCount.value += 1
  }
}

function applyTaskUpdate(updated: Task) {
  const idx = tasks.value.findIndex((t) => t.id === updated.id)
  const previous = idx >= 0 ? tasks.value[idx] : null

  if (!taskMatchesStatusFilter(updated)) {
    if (idx >= 0) {
      tasks.value = tasks.value.filter((t) => t.id !== updated.id)
      total.value = Math.max(0, total.value - 1)
    }
    if (previous) adjustCompletionCounts(previous.completed, updated.completed)
    return
  }

  if (idx >= 0) {
    adjustCompletionCounts(tasks.value[idx].completed, updated.completed)
    tasks.value[idx] = updated
    return
  }

  tasks.value = [...tasks.value, updated]
}

function reorderLocalTasks(orderedIds: number[], favorite: boolean) {
  const reordered = orderedIds
    .map((id) => tasks.value.find((t) => t.id === id))
    .filter((t): t is Task => !!t)
  if (favorite) {
    tasks.value = [...reordered, ...tasks.value.filter((t) => !t.favorite)]
  } else {
    tasks.value = [...tasks.value.filter((t) => t.favorite), ...reordered]
  }
}

async function saveReorder(orderedIds: number[], favorite: boolean) {
  reorderLocalTasks(orderedIds, favorite)
  try {
    await api.reorderTasks({
      task_ids: orderedIds,
      favorite,
      project: filters.project || undefined,
    })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save task order', 'error')
    await reloadInitial()
  }
}

const { refresh: refreshSortable } = useTaskSortable(
  favoriteListEl,
  taskListEl,
  sortableEnabled,
  showFavoriteList,
  saveReorder,
)

async function loadMeta() {
  try {
    const [projs, tagList, views] = await Promise.all([
      api.listProjects(),
      api.listTags(),
      api.listSavedViews(),
    ])
    projects.value = projs
    tags.value = tagList
    savedViews.value = views
  } catch {
    /* non-fatal */
  }
}

async function reloadInitial() {
  loading.value = true
  loadedPage.value = 0
  selected.value = []
  try {
    const perPage = user.value?.items_per_page || 50
    const list = await api.listTasks(toApiParams(1, perPage))
    tasks.value = list.tasks
    loadedPage.value = 1
    total.value = list.total
    totalPages.value = list.total_pages
    completedCount.value = list.completed_count
    incompleteCount.value = list.incomplete_count
    search.value = filters.search
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    loading.value = false
    await nextTick()
    refreshSortable()
  }
}

async function loadMore() {
  if (loadingMore.value || loading.value || !hasMore.value) return
  loadingMore.value = true
  try {
    const perPage = user.value?.items_per_page || 50
    const nextPage = loadedPage.value + 1
    const list = await api.listTasks(toApiParams(nextPage, perPage))
    const existingIds = new Set(tasks.value.map((t) => t.id))
    const newTasks = list.tasks.filter((t) => !existingIds.has(t.id))
    tasks.value = [...tasks.value, ...newTasks]
    loadedPage.value = nextPage
    total.value = list.total
    totalPages.value = list.total_pages
    completedCount.value = list.completed_count
    incompleteCount.value = list.incomplete_count
    await nextTick()
    refreshSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    loadingMore.value = false
  }
}

watch(lastSavedTask, async (task) => {
  if (!task) return
  const exists = tasks.value.some((t) => t.id === task.id)
  if (exists) {
    applyTaskUpdate(task)
  } else {
    registerTaskAdded(task)
  }
  lastSavedTask.value = null
  await nextTick()
  refreshSortable()
})

async function toggleComplete(task: Task) {
  try {
    const updated = await api.patchTask(task.id, { completed: !task.completed })
    applyTaskUpdate(updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function toggleFavorite(task: Task) {
  try {
    const updated = await api.patchTask(task.id, { favorite: !task.favorite })
    applyTaskUpdate(updated)
    await nextTick()
    refreshSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeTask(task: Task) {
  if (!confirm(`Delete "${task.title}"?`)) return
  try {
    const res = await api.deleteTask(task.id)
    undoToken.value = res.undo_token || null
    removeTaskLocally(task)
    toast.push(undoToken.value ? 'Task deleted — undo available' : 'Task deleted', 'info')
    await nextTick()
    refreshSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

async function undoDelete() {
  if (!undoToken.value) return
  try {
    await api.undo(undoToken.value)
    undoToken.value = null
    toast.push('Restored', 'success')
    await reloadInitial()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Undo failed', 'error')
  }
}

function toggleSelect(id: number, checked: boolean) {
  if (checked) {
    if (!selected.value.includes(id)) selected.value = [...selected.value, id]
  } else {
    selected.value = selected.value.filter((x) => x !== id)
  }
}

function toggleSelectAll(checked: boolean) {
  selected.value = checked ? tasks.value.map((t) => t.id) : []
}

async function bulk(action: string, extra: Record<string, unknown> = {}) {
  if (!selected.value.length) return
  const affectedIds = [...selected.value]
  try {
    const res = await api.bulkTasks({ action, task_ids: affectedIds, ...extra })
    if (res.undo_token) undoToken.value = res.undo_token
    toast.push(`Bulk ${action}: ${res.affected ?? affectedIds.length}`, 'success')
    if (action === 'delete') {
      for (const id of affectedIds) {
        const task = tasks.value.find((t) => t.id === id)
        if (task) removeTaskLocally(task)
      }
      await nextTick()
      refreshSortable()
    } else if (action === 'complete' || action === 'incomplete') {
      for (const id of affectedIds) {
        const task = tasks.value.find((t) => t.id === id)
        if (!task) continue
        applyTaskUpdate({ ...task, completed: action === 'complete' })
      }
    } else {
      await reloadInitial()
    }
    selected.value = []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Bulk action failed', 'error')
  }
}

function bulkMoveProject() {
  if (!bulkProjectId.value) return
  void bulk('move_project', { project_id: bulkProjectId.value === '0' ? 0 : Number(bulkProjectId.value) })
}

function bulkTagAction(action: 'add_tag' | 'remove_tag') {
  if (!bulkTagId.value) return
  void bulk(action, { tag_id: Number(bulkTagId.value) })
}

function bulkSetPriority() {
  void bulk('set_priority', { priority: Number(bulkPriority.value) })
}

function bulkSetDueDate() {
  if (!bulkDueDate.value) return
  void bulk('set_due_date', { due_date: bulkDueDate.value })
}

function bulkDuePreset(preset: string) {
  const today = new Date()
  let due = ''
  if (preset === 'today') {
    due = today.toISOString().slice(0, 10)
  } else if (preset === 'tomorrow') {
    today.setDate(today.getDate() + 1)
    due = today.toISOString().slice(0, 10)
  } else if (preset === 'week') {
    today.setDate(today.getDate() + 7)
    due = today.toISOString().slice(0, 10)
  } else if (preset === 'clear') {
    void bulk('clear_due_date')
    return
  }
  if (due) void bulk('set_due_date', { due_date: due })
}

function setFilterAndReload(key: Parameters<typeof setFilter>[0], value: string) {
  setFilter(key, value)
  void reloadInitial()
}

function applySearch() {
  setFilter('search', search.value.trim())
  void reloadInitial()
}

function clearFilters() {
  search.value = ''
  resetFilters()
  void reloadInitial()
}

function toggleSort() {
  setFilter('sort', filters.sort === 'priority' ? '' : 'priority')
  void reloadInitial()
}

function cycleStatusColumnFilter() {
  if (!filters.status) setFilterAndReload('status', 'incomplete')
  else if (filters.status === 'incomplete') setFilterAndReload('status', 'complete')
  else setFilterAndReload('status', '')
}

function applySavedView(view: SavedView) {
  applySavedViewFilters(view.filter || {})
  search.value = filters.search
  void reloadInitial()
}

async function exportTasks(format: 'json' | 'csv', filtered: boolean) {
  try {
    const suffix = filtered ? toExportQuery() : ''
    const path = `/api/v1/export?format=${format}${suffix}`
    const res = await fetch(path, { credentials: 'include' })
    if (!res.ok) throw new Error('Export failed')
    const blob = await res.blob()
    const disposition = res.headers.get('Content-Disposition') || ''
    const match = /filename="([^"]+)"/.exec(disposition)
    const filename = match?.[1] || `gotodo-export.${format}`
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    toast.push('Export failed', 'error')
  }
}

useInfiniteScroll(loadMoreSentinel, loadMore, hasMore)

onMounted(async () => {
  await loadMeta()
  await reloadInitial()
})
</script>

<template>
  <div class="container mt-3 rounded p-3">
    <div class="d-flex justify-content-between align-items-start mb-4">
      <div class="d-flex flex-column me-3">
        <form id="search-form" class="d-flex gap-2" @submit.prevent="applySearch">
          <input
            id="search"
            v-model="search"
            type="search"
            class="form-control search-input"
            placeholder="Search tasks..."
            aria-label="Search tasks"
          />
          <button type="submit" class="btn btn-primary">
            <i class="bi bi-search" />
          </button>
        </form>
      </div>
      <div class="d-flex gap-2 align-items-start flex-shrink-0">
        <div class="btn-group">
          <button
            type="button"
            class="btn btn-outline-secondary btn-sm dropdown-toggle"
            data-bs-toggle="dropdown"
            aria-expanded="false"
            aria-label="Import or export tasks"
          >
            <i class="bi bi-arrow-down-up" /> Import / Export
          </button>
          <ul class="dropdown-menu dropdown-menu-end">
            <li>
              <RouterLink class="dropdown-item" to="/import">
                <i class="bi bi-upload me-2" />Import CSV (with preview)
              </RouterLink>
            </li>
            <li>
              <RouterLink class="dropdown-item" to="/settings#calendar-feed">
                <i class="bi bi-calendar3 me-2" />Calendar sync (ICS)
              </RouterLink>
            </li>
            <template v-if="hasActiveFilters">
              <li><hr class="dropdown-divider" /></li>
              <li><h6 class="dropdown-header">Current filters</h6></li>
              <li>
                <button type="button" class="dropdown-item" @click="exportTasks('csv', true)">
                  <i class="bi bi-download me-2" />Export CSV
                </button>
              </li>
              <li>
                <button type="button" class="dropdown-item" @click="exportTasks('json', true)">
                  <i class="bi bi-download me-2" />Export JSON
                </button>
              </li>
              <li><hr class="dropdown-divider" /></li>
            </template>
            <li><h6 class="dropdown-header">All your tasks</h6></li>
            <li>
              <button type="button" class="dropdown-item" @click="exportTasks('csv', false)">
                <i class="bi bi-download me-2" />Export all CSV
              </button>
            </li>
            <li>
              <button type="button" class="dropdown-item" @click="exportTasks('json', false)">
                <i class="bi bi-download me-2" />Export all JSON
              </button>
            </li>
          </ul>
        </div>
        <button type="button" class="btn btn-success" id="openSidebar" @click="() => openAdd()">
          <i class="bi bi-plus-lg" /> Add Task
        </button>
        <button
          v-if="undoToken"
          type="button"
          class="btn btn-outline-secondary btn-sm"
          @click="undoDelete"
        >
          Undo delete
        </button>
      </div>
    </div>
  </div>

  <div class="container mb-3 filter-toolbar-wrapper">
    <div class="d-flex justify-content-between align-items-center flex-wrap gap-2 mb-2">
      <div>
        <select
          v-if="projects.length"
          id="project-filter"
          class="form-select w-auto"
          style="width: 220px"
          :value="filters.project"
          aria-label="Filter by project"
          @change="setFilterAndReload('project', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">All projects</option>
          <option value="0">No project</option>
          <option v-for="p in projects" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
        </select>
      </div>
      <div class="dropdown" id="saved-views-dropdown">
        <button
          type="button"
          class="btn btn-outline-secondary btn-sm dropdown-toggle"
          id="saved-views-btn"
          data-bs-toggle="dropdown"
          aria-expanded="false"
          aria-label="Saved views"
        >
          <i class="bi bi-bookmark" /> Views
        </button>
        <ul class="dropdown-menu dropdown-menu-end" id="saved-views-menu" aria-labelledby="saved-views-btn">
          <li v-if="!savedViews.length">
            <span class="dropdown-item-text text-muted small">No saved views</span>
          </li>
          <li v-for="view in savedViews" :key="view.id">
            <button type="button" class="dropdown-item" @click="applySavedView(view)">
              {{ view.name }}
            </button>
          </li>
          <li><hr class="dropdown-divider" /></li>
          <li>
            <RouterLink class="dropdown-item" to="/views">
              <i class="bi bi-gear me-2" />Manage views
            </RouterLink>
          </li>
        </ul>
      </div>
    </div>
    <div class="d-flex justify-content-end mb-2">
      <button
        v-if="hasActiveFilters"
        type="button"
        class="btn btn-outline-secondary btn-sm"
        id="filter-clear-all"
        @click="clearFilters"
      >
        <i class="bi bi-x-circle" /> Clear all filters
      </button>
    </div>
    <div id="filter-toolbar-panel" class="filter-toolbar-panel">
      <div class="d-flex flex-wrap align-items-center gap-2 filter-toolbar-inner">
        <select
          id="status-filter-select"
          class="form-select form-select-sm w-auto"
          aria-label="Filter by status"
          :value="filters.status"
          @change="setFilterAndReload('status', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">All statuses</option>
          <option value="incomplete">Incomplete</option>
          <option value="complete">Complete</option>
        </select>
        <select
          v-if="tags.length"
          id="tag-filter-toolbar"
          class="form-select form-select-sm w-auto"
          aria-label="Filter by tag"
          :value="filters.tag"
          @change="setFilterAndReload('tag', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">All tags</option>
          <option v-for="tag in tags" :key="tag.id" :value="String(tag.id)">{{ tag.name }}</option>
        </select>
        <div class="btn-group btn-group-sm" role="group" aria-label="Due date filters">
          <button
            v-for="opt in [
              { key: '', label: 'All' },
              { key: 'today', label: 'Today' },
              { key: 'overdue', label: 'Overdue' },
              { key: 'week', label: 'This Week' },
              { key: 'none', label: 'No Date' },
            ]"
            :key="opt.key || 'all'"
            type="button"
            class="btn btn-outline-secondary due-filter-btn"
            :class="{ 'due-filter-active': filters.due === opt.key }"
            :aria-pressed="filters.due === opt.key"
            @click="setFilterAndReload('due', opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
        <select
          id="priority-filter-toolbar"
          class="form-select form-select-sm w-auto"
          aria-label="Filter by priority"
          :value="filters.priority"
          @change="setFilterAndReload('priority', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">All priorities</option>
          <option value="1">Low</option>
          <option value="2">Medium</option>
          <option value="3">High</option>
        </select>
        <button
          type="button"
          class="btn btn-sm"
          :class="filters.sort === 'priority' ? 'btn-primary' : 'btn-outline-primary'"
          id="sort-priority-btn"
          :aria-pressed="filters.sort === 'priority'"
          :title="
            filters.sort === 'priority'
              ? 'Sorted by priority (high first). Click to restore manual drag order.'
              : 'Sorted by manual order. Click to sort by priority (high first).'
          "
          @click="toggleSort"
        >
          {{ filters.sort === 'priority' ? 'Sort: Priority' : 'Sort: Manual' }}
        </button>
      </div>
    </div>
  </div>

  <div
    id="bulk-bar"
    class="bulk-action-bar container mb-2"
    :class="{ 'd-none': !selected.length }"
    :aria-hidden="!selected.length"
    role="region"
    aria-label="Bulk actions"
  >
    <div class="d-flex flex-wrap align-items-center gap-2 py-2 px-3">
      <span id="bulk-count" class="fw-semibold me-2" aria-live="polite">{{ selected.length }} selected</span>
      <button type="button" class="btn btn-sm btn-success" @click="bulk('complete')">Complete</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="bulk('incomplete')">
        Mark incomplete
      </button>
      <button type="button" class="btn btn-sm btn-danger" @click="bulk('delete')">Delete</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="selected = []">Clear</button>
      <div class="dropdown">
        <button
          type="button"
          class="btn btn-sm btn-outline-primary dropdown-toggle"
          data-bs-toggle="dropdown"
          aria-expanded="false"
        >
          More actions
        </button>
        <div class="dropdown-menu dropdown-menu-end p-3 bulk-more-menu">
          <div v-if="projects.length" class="mb-2 d-flex flex-wrap gap-2 align-items-center">
            <select v-model="bulkProjectId" id="bulk-project" class="form-select form-select-sm" aria-label="Move to project">
              <option value="">Move to project…</option>
              <option value="0">No project</option>
              <option v-for="p in projects" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
            </select>
            <button type="button" class="btn btn-sm btn-outline-primary" @click="bulkMoveProject">Move</button>
          </div>
          <div v-if="tags.length" class="mb-2 d-flex flex-wrap gap-2 align-items-center">
            <select v-model="bulkTagId" id="bulk-tag" class="form-select form-select-sm" aria-label="Tag for bulk action">
              <option value="">Select tag…</option>
              <option v-for="tag in tags" :key="tag.id" :value="String(tag.id)">{{ tag.name }}</option>
            </select>
            <button type="button" class="btn btn-sm btn-outline-primary" @click="bulkTagAction('add_tag')">Add tag</button>
            <button type="button" class="btn btn-sm btn-outline-secondary" @click="bulkTagAction('remove_tag')">
              Remove tag
            </button>
          </div>
          <div class="mb-2 d-flex flex-wrap gap-2 align-items-center">
            <select v-model="bulkPriority" id="bulk-priority" class="form-select form-select-sm" aria-label="Set priority">
              <option value="0">Priority: None</option>
              <option value="1">Low</option>
              <option value="2">Medium</option>
              <option value="3">High</option>
            </select>
            <button type="button" class="btn btn-sm btn-outline-primary" @click="bulkSetPriority">Set priority</button>
          </div>
          <div class="d-flex flex-wrap gap-2 align-items-center">
            <div class="btn-group btn-group-sm" role="group" aria-label="Due date presets">
              <button type="button" class="btn btn-outline-secondary" @click="bulkDuePreset('today')">Today</button>
              <button type="button" class="btn btn-outline-secondary" @click="bulkDuePreset('tomorrow')">Tomorrow</button>
              <button type="button" class="btn btn-outline-secondary" @click="bulkDuePreset('week')">+1 week</button>
              <button type="button" class="btn btn-outline-secondary" @click="bulkDuePreset('clear')">Clear</button>
            </div>
            <input v-model="bulkDueDate" type="date" id="bulk-due-date" class="form-control form-control-sm" aria-label="Set due date" />
            <button type="button" class="btn btn-sm btn-outline-primary" @click="bulkSetDueDate">Set due</button>
            <button type="button" class="btn btn-sm btn-outline-secondary" @click="bulk('clear_due_date')">Clear due</button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div id="task-container" class="container" aria-live="polite" aria-atomic="false">
    <p v-if="loading && !tasks.length" class="text-muted">Loading…</p>

    <div v-else-if="showTaskTable" class="row justify-content-center mb-5">
      <div>
        <div class="mb-3">
          <span id="total-tasks-badge" class="badge bg-primary me-2">Tasks: {{ total }}</span>
          <span id="completed-tasks-badge" class="badge bg-success me-2">Completed: {{ completedCount }}</span>
          <span id="incomplete-tasks-badge" class="badge bg-warning text-dark">Incomplete: {{ incompleteCount }}</span>
          <small class="task-count-hint ms-2">Task count excludes starred items</small>

          <table class="table table-striped table-bordered w-100 mb-3 mt-3">
            <thead>
              <tr>
                <th style="width: 32px">
                  <input
                    id="select-all-tasks"
                    type="checkbox"
                    class="form-check-input"
                    :checked="allSelected"
                    aria-label="Select all tasks on this page"
                    title="Select all on page"
                    @change="toggleSelectAll(($event.target as HTMLInputElement).checked)"
                  />
                </th>
                <th style="width: 32px" />
                <th class="description-column">Title</th>
                <th class="tags-column">Tags</th>
                <th class="description-column">Description</th>
                <th class="date-added">Due Date (if set)</th>
                <th class="actions-column">
                  <button
                    type="button"
                    class="status-filter-toggle btn btn-link p-0 text-decoration-none d-inline-flex align-items-center gap-1"
                    @click="cycleStatusColumnFilter"
                  >
                    Status
                    <span class="status-filter-state">
                      ({{
                        filters.status === 'incomplete'
                          ? 'Incomplete'
                          : filters.status === 'complete'
                            ? 'Complete'
                            : 'All'
                      }})
                    </span>
                    <i
                      :class="
                        filters.status === 'incomplete'
                          ? 'bi bi-arrow-right-short'
                          : filters.status === 'complete'
                            ? 'bi bi-x-circle'
                            : 'bi bi-funnel'
                      "
                    />
                  </button>
                </th>
              </tr>
            </thead>
            <tbody v-if="favoriteTasks.length">
              <tr class="starred-section-label">
                <td colspan="7" class="py-1 px-2 border-0">
                  <small><i class="bi bi-star-fill" /> Starred tasks (always visible)</small>
                </td>
              </tr>
            </tbody>
            <tbody v-if="showFavoriteList" id="favorite-task-list" ref="favoriteListEl">
              <TaskTableRow
                v-for="task in favoriteTasks"
                :key="task.id"
                :task="task"
                :selected="selected.includes(task.id)"
                @toggle-select="toggleSelect(task.id, $event)"
                @toggle-complete="toggleComplete(task)"
                @toggle-favorite="toggleFavorite(task)"
                @edit="openEdit(task.id)"
                @remove="removeTask(task)"
              />
            </tbody>
            <tbody id="task-list" ref="taskListEl">
              <TaskTableRow
                v-for="task in regularTasks"
                :key="task.id"
                :task="task"
                :selected="selected.includes(task.id)"
                @toggle-select="toggleSelect(task.id, $event)"
                @toggle-complete="toggleComplete(task)"
                @toggle-favorite="toggleFavorite(task)"
                @edit="openEdit(task.id)"
                @remove="removeTask(task)"
              />
              <tr v-if="!tasks.length && hasActiveFilters">
                <td colspan="7" class="text-center py-4">
                  <div class="empty-state-inline">
                    <p class="text-muted mb-2">No tasks match this filter.</p>
                    <button type="button" class="btn btn-sm btn-outline-primary" @click="clearFilters">
                      <i class="bi bi-x-circle" /> Clear filters
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="hasMore" ref="loadMoreSentinel">
                <td colspan="7" class="text-center py-3 text-muted">
                  <span v-if="loadingMore" class="spinner-border spinner-border-sm me-2" role="status" />
                  {{ loadingMore ? 'Loading more tasks…' : 'Scroll for more tasks' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div v-else-if="isSearching" class="card text-center">
      <div class="card-body">
        <p class="text-muted mb-2">No tasks match your search.</p>
        <div class="d-flex flex-wrap gap-2 justify-content-center">
          <button type="button" class="btn btn-sm btn-outline-primary" @click="clearFilters">
            <i class="bi bi-x-circle" /> Clear search
          </button>
          <button v-if="hasActiveFilters" type="button" class="btn btn-sm btn-outline-secondary" @click="clearFilters">
            Reset filters
          </button>
        </div>
      </div>
    </div>

    <div v-else class="card text-center empty-state-card">
      <div class="card-body">
        <i class="bi bi-clipboard-check empty-state-icon" aria-hidden="true" />
        <h3 class="mt-3">Add your first Todo!</h3>
        <p class="text-muted">Get started by creating a task.</p>
        <button type="button" class="btn btn-success" @click="() => openAdd()">
          <i class="bi bi-plus-lg" /> Add Task
        </button>
      </div>
    </div>
  </div>
</template>
