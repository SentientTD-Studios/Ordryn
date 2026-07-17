<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, SavedView, Tag, Task } from '@/api/types'
import { APIError } from '@/api/types'
import TaskTableRow from '@/components/TaskTableRow.vue'
import { useAuth } from '@/composables/useAuth'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useTaskSortable } from '@/composables/useTaskSortable'
import { useToast } from '@/composables/useToast'

const { openAdd, openEdit, lastSavedTask } = useTaskSidebar()

const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const savedViews = ref<SavedView[]>([])
const total = ref(0)
const page = ref(1)
const totalPages = ref(1)
const completedCount = ref(0)
const incompleteCount = ref(0)
const search = ref('')
const loading = ref(true)
const toast = useToast()
const { user } = useAuth()
const undoToken = ref<string | null>(null)
const selected = ref<number[]>([])
const bulkProjectId = ref('')
const bulkTagId = ref('')
const bulkPriority = ref('0')
const bulkDueDate = ref('')
const route = useRoute()
const router = useRouter()
const favoriteListEl = ref<HTMLElement | null>(null)
const taskListEl = ref<HTMLElement | null>(null)

const filterKeys = ['status', 'due', 'completed', 'priority', 'tag', 'sort', 'project', 'search'] as const

const filterParams = computed(() => {
  const q = route.query
  const params: Record<string, string | number | undefined> = {
    per_page: user.value?.items_per_page || 50,
  }
  for (const key of filterKeys) {
    const v = q[key]
    if (typeof v === 'string' && v) params[key] = v
  }
  const pageParam = q.page
  if (typeof pageParam === 'string' && pageParam) {
    const p = parseInt(pageParam, 10)
    if (p > 0) params.page = p
  }
  return params
})

const currentStatus = computed(() =>
  typeof route.query.status === 'string' ? route.query.status : '',
)
const currentDue = computed(() => (typeof route.query.due === 'string' ? route.query.due : ''))
const currentSort = computed(() => (typeof route.query.sort === 'string' ? route.query.sort : ''))
const currentProject = computed(() =>
  typeof route.query.project === 'string' ? route.query.project : '',
)
const currentPriority = computed(() =>
  typeof route.query.priority === 'string' ? route.query.priority : '',
)
const currentTag = computed(() => (typeof route.query.tag === 'string' ? route.query.tag : ''))

const favoriteTasks = computed(() => tasks.value.filter((t) => t.favorite))
const regularTasks = computed(() => tasks.value.filter((t) => !t.favorite))
const allSelected = computed(
  () => tasks.value.length > 0 && selected.value.length === tasks.value.length,
)
const hasActiveFilters = computed(() =>
  filterKeys.some((k) => {
    const v = route.query[k]
    return typeof v === 'string' && v !== ''
  }),
)
const isSearching = computed(
  () => typeof route.query.search === 'string' && route.query.search !== '',
)
const showTaskTable = computed(
  () => total.value > 0 || favoriteTasks.value.length > 0 || hasActiveFilters.value,
)

const exportFilterQuery = computed(() => {
  const params = new URLSearchParams()
  for (const key of filterKeys) {
    const v = route.query[key]
    if (typeof v === 'string' && v) params.set(key, v)
  }
  const s = params.toString()
  return s ? `&${s}` : ''
})

const paginationWindow = computed(() => {
  const current = page.value
  const last = totalPages.value
  const windowSize = 4
  const start = current >= windowSize ? current : 1
  const end = Math.min(start + windowSize - 1, last)
  const pages: number[] = []
  for (let i = start; i <= end; i++) pages.push(i)
  return { pages, hasRightEllipsis: end < last }
})

const sortableEnabled = computed(() => currentSort.value !== 'priority' && !loading.value)
const showFavoriteList = computed(() => page.value === 1 && favoriteTasks.value.length > 0)

function taskMatchesStatusFilter(task: Task) {
  if (currentStatus.value === 'complete') return task.completed
  if (currentStatus.value === 'incomplete') return !task.completed
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
      project: currentProject.value || undefined,
    })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save task order', 'error')
    await load({ silent: true })
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

async function load(opts?: { silent?: boolean }) {
  if (!opts?.silent) {
    loading.value = true
    selected.value = []
  }
  try {
    const list = await api.listTasks(filterParams.value)
    tasks.value = list.tasks
    total.value = list.total
    page.value = list.page
    totalPages.value = list.total_pages
    completedCount.value = list.completed_count
    incompleteCount.value = list.incomplete_count
    search.value = typeof route.query.search === 'string' ? route.query.search : ''
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    loading.value = false
    await nextTick()
    refreshSortable()
  }
}

watch(lastSavedTask, async (task) => {
  if (!task) return
  const exists = tasks.value.some((t) => t.id === task.id)
  if (exists) {
    applyTaskUpdate(task)
    await nextTick()
    refreshSortable()
  } else {
    await load({ silent: true })
  }
  lastSavedTask.value = null
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
    toast.push(undoToken.value ? 'Task deleted — undo available' : 'Task deleted', 'info')
    await load()
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
    await load()
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
  try {
    const res = await api.bulkTasks({ action, task_ids: selected.value, ...extra })
    if (res.undo_token) undoToken.value = res.undo_token
    toast.push(`Bulk ${action}: ${res.affected ?? selected.value.length}`, 'success')
    await load()
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

function pushQuery(query: Record<string, string | undefined>) {
  void router.push({ name: 'tasks', query })
}

function applySearch() {
  const query: Record<string, string | undefined> = {}
  for (const key of [...filterKeys, 'page'] as const) {
    const v = route.query[key]
    if (typeof v === 'string' && v) query[key] = v
  }
  if (search.value.trim()) query.search = search.value.trim()
  else delete query.search
  delete query.page
  pushQuery(query)
}

function setFilter(key: string, value: string) {
  const query: Record<string, string | undefined> = {}
  for (const k of [...filterKeys, 'page'] as const) {
    const v = route.query[k]
    if (typeof v === 'string' && v) query[k] = v
  }
  if (value) query[key] = value
  else delete query[key]
  delete query.page
  pushQuery(query)
}

function setPage(p: number) {
  const query: Record<string, string | undefined> = {}
  for (const k of filterKeys) {
    const v = route.query[k]
    if (typeof v === 'string' && v) query[k] = v
  }
  if (p > 1) query.page = String(p)
  pushQuery(query)
}

function clearFilters() {
  pushQuery({})
}

function toggleSort() {
  setFilter('sort', currentSort.value === 'priority' ? '' : 'priority')
}

function cycleStatusColumnFilter() {
  if (!currentStatus.value) setFilter('status', 'incomplete')
  else if (currentStatus.value === 'incomplete') setFilter('status', 'complete')
  else setFilter('status', '')
}

function applySavedView(view: SavedView) {
  const f = view.filter || {}
  const query: Record<string, string | undefined> = {}
  for (const key of filterKeys) {
    const v = f[key as keyof typeof f]
    if (typeof v === 'string' && v) query[key] = v
  }
  pushQuery(query)
}

async function exportTasks(format: 'json' | 'csv', filtered: boolean) {
  try {
    const suffix = filtered ? exportFilterQuery.value : ''
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

watch(
  () => route.query,
  () => {
    void load()
  },
)

onMounted(async () => {
  await loadMeta()
  await load()
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
        <button type="button" class="btn btn-success" id="openSidebar" @click="openAdd">
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
          :value="currentProject"
          aria-label="Filter by project"
          @change="setFilter('project', ($event.target as HTMLSelectElement).value)"
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
          :value="currentStatus"
          @change="setFilter('status', ($event.target as HTMLSelectElement).value)"
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
          :value="currentTag"
          @change="setFilter('tag', ($event.target as HTMLSelectElement).value)"
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
            :class="{ 'due-filter-active': currentDue === opt.key }"
            :aria-pressed="currentDue === opt.key"
            @click="setFilter('due', opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
        <select
          id="priority-filter-toolbar"
          class="form-select form-select-sm w-auto"
          aria-label="Filter by priority"
          :value="currentPriority"
          @change="setFilter('priority', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">All priorities</option>
          <option value="1">Low</option>
          <option value="2">Medium</option>
          <option value="3">High</option>
        </select>
        <button
          type="button"
          class="btn btn-sm"
          :class="currentSort === 'priority' ? 'btn-primary' : 'btn-outline-primary'"
          id="sort-priority-btn"
          :aria-pressed="currentSort === 'priority'"
          :title="
            currentSort === 'priority'
              ? 'Sorted by priority (high first). Click to restore manual drag order.'
              : 'Sorted by manual order. Click to sort by priority (high first).'
          "
          @click="toggleSort"
        >
          {{ currentSort === 'priority' ? 'Sort: Priority' : 'Sort: Manual' }}
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
                        currentStatus === 'incomplete'
                          ? 'Incomplete'
                          : currentStatus === 'complete'
                            ? 'Complete'
                            : 'All'
                      }})
                    </span>
                    <i
                      :class="
                        currentStatus === 'incomplete'
                          ? 'bi bi-arrow-right-short'
                          : currentStatus === 'complete'
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
            </tbody>
          </table>
        </div>

        <div v-if="total > 0" class="d-flex justify-content-center align-items-center">
          <div class="d-flex align-items-center gap-2">
            <button
              class="btn btn-outline-primary btn-sm"
              type="button"
              title="Go to first page"
              aria-label="Go to first page"
              :disabled="page <= 1"
              @click="setPage(1)"
            >
              &laquo;
            </button>
            <button
              v-for="p in paginationWindow.pages"
              :key="p"
              class="btn btn-sm"
              :class="p === page ? 'btn-primary' : 'btn-outline-primary'"
              type="button"
              :aria-current="p === page ? 'page' : undefined"
              :disabled="p === page"
              @click="setPage(p)"
            >
              {{ p }}
            </button>
            <span v-if="paginationWindow.hasRightEllipsis" class="text-muted">&hellip;</span>
            <button
              v-if="paginationWindow.hasRightEllipsis"
              class="btn btn-outline-primary btn-sm"
              type="button"
              title="Go to last page"
              aria-label="Go to last page"
              @click="setPage(totalPages)"
            >
              {{ totalPages }}
            </button>
            <button
              class="btn btn-outline-primary btn-sm"
              type="button"
              title="Go to last page"
              aria-label="Go to last page"
              :disabled="page >= totalPages"
              @click="setPage(totalPages)"
            >
              &raquo;
            </button>
          </div>
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
        <button type="button" class="btn btn-success" @click="openAdd">
          <i class="bi bi-plus-lg" /> Add Task
        </button>
      </div>
    </div>
  </div>
</template>
