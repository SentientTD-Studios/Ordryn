<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, ProjectSprint, SavedView, Tag, Task } from '@/api/types'
import { APIError } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import ModernTaskFilterBar from '@/components/modern/ModernTaskFilterBar.vue'
import ModernTaskCard from '@/components/modern/ModernTaskCard.vue'
import KanbanBoard from '@/components/KanbanBoard.vue'
import DeleteTaskDialog from '@/components/DeleteTaskDialog.vue'
import AppFooter from '@/components/AppFooter.vue'
import ProjectSettingsModal from '@/components/ProjectSettingsModal.vue'
import { useAuth } from '@/composables/useAuth'
import { useInfiniteScroll } from '@/composables/useInfiniteScroll'
import { useTaskListFilters } from '@/composables/useTaskListFilters'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useTaskSortable } from '@/composables/useTaskSortable'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useViewDensity } from '@/composables/useViewDensity'
import { useSidebarState } from '@/composables/useSidebarState'
import { useKeyboardShortcuts } from '@/composables/useKeyboardShortcuts'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import { projectOptionLabel, activeProjects, isArchivedProject, isProjectOwner } from '@/utils/projectLabel'
import { sprintOptionLabel } from '@/utils/sprintLabel'
import { uniqueTagsByName, isArchivedTask } from '@/utils/tags'

const route = useRoute()
const router = useRouter()
const { openAdd, openEdit, openView, lastSavedTask, lastDeletedTask } = useTaskSidebar()
const { density } = useViewDensity()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()
const {
  focusedTaskId,
  registerTaskShortcuts,
  unregisterTaskShortcuts,
} = useKeyboardShortcuts()

const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const savedViews = ref<SavedView[]>([])
const activeViewId = ref<string | null>(null)
const deleteDialogOpen = ref(false)
const deleteDialogTask = ref<Task | null>(null)
/** Parent ids with expanded subtasks. Collapsed by default. */
const expandedParentIds = ref<Set<number>>(new Set())

const total = ref(0)
const loadedPage = ref(0)
const totalPages = ref(1)
const completedCount = ref(0)
const incompleteCount = ref(0)
const search = ref('')
const loading = ref(true)
const loadingMore = ref(false)
/** Project filter key last applied to `tasks` ('' = all projects). */
const loadedProjectKey = ref<string | null>(null)
/** Monotonic counter so out-of-order reloadInitial responses are ignored. */
let reloadGeneration = 0

// Save View Modal state
const showSaveViewModal = ref(false)
const newViewName = ref('')

// Add Project Modal state
const showAddProjectModal = ref(false)
const newProjectName = ref('')
const newProjectDescription = ref('')

// Edit Project Modal state
const showEditProjectModal = ref(false)
const editingProject = ref<Project | null>(null)

// Bulk Control Panel State
const bulkProject = ref('')
const bulkTag = ref('')
const bulkPriority = ref('')
const bulkDate = ref('')

const toast = useToast()
const { askConfirm } = useConfirm()
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
const kanbanColumnsRev = ref(0)
const boardSprints = ref<ProjectSprint[]>([])
const boardSprintKey = ref('backlog')
let sprintsLoadedForProject: number | null = null
const selected = ref<number[]>([])
const selectMode = ref(false)
const taskListEl = ref<HTMLElement | null>(null)
const loadMoreSentinel = ref<HTMLElement | null>(null)

const rootTasks = computed(() => tasks.value)
const flatSelectableIds = computed(() => {
  const ids: number[] = []
  for (const t of tasks.value) {
    ids.push(t.id)
    for (const c of t.children || []) ids.push(c.id)
  }
  return ids
})
const allSelected = computed(
  () => flatSelectableIds.value.length > 0 && selected.value.length === flatSelectableIds.value.length,
)
const isSelecting = computed(() => selectMode.value || selected.value.length > 0)
const isSearching = computed(() => filters.search !== '')
const showTaskTable = computed(
  () =>
    tasks.value.length > 0 ||
    total.value > 0 ||
    hasActiveFilters.value ||
    isKanbanProjectView.value,
)

const activeProjectObj = computed(() => {
  if (!filters.project || filters.project === '0') return null
  const pid = parseInt(filters.project, 10)
  if (Number.isNaN(pid)) return null
  return projects.value.find((p) => p.id === pid) ?? null
})

const isViewerProjectView = computed(
  () => activeProjectObj.value?.role === 'viewer',
)

const isArchivedProjectView = computed(() => !!activeProjectObj.value?.archived)

const canAddTasks = computed(() => !isViewerProjectView.value && !isArchivedProjectView.value)

const tagFilterByName = computed(() => !filters.project)
const displayTags = computed(() => (tagFilterByName.value ? uniqueTagsByName(tags.value) : tags.value))

const VIEW_MODE_KEY = 'gotodo.viewMode'
type TaskViewMode = 'list' | 'board'

function readStoredViewMode(): TaskViewMode {
  try {
    const v = localStorage.getItem(VIEW_MODE_KEY)
    return v === 'board' ? 'board' : 'list'
  } catch {
    return 'list'
  }
}

const viewMode = ref<TaskViewMode>(readStoredViewMode())

const isKanbanProjectView = computed(() => {
  const p = activeProjectObj.value
  if (!p) return false
  if (p.workflow_mode === 'kanban') return true
  // Fallback: any loaded task for this project reports kanban workflow
  return tasks.value.some(
    (t) => t.project_id === p.id && t.project_workflow === 'kanban',
  )
})

const showBoardView = computed(
  () => isKanbanProjectView.value && viewMode.value === 'board',
)

function setViewMode(mode: TaskViewMode) {
  if (viewMode.value === mode) return
  viewMode.value = mode
  try {
    localStorage.setItem(VIEW_MODE_KEY, mode)
  } catch {
    /* ignore */
  }
  void reloadInitial()
}

/** Keep column order + status_id in sync without a full reload after drag. */
function applyBoardReorder(payload: { statusId: number; taskIds: number[] }) {
  const { statusId, taskIds } = payload
  const idSet = new Set(taskIds)
  const byId = new Map(tasks.value.map((t) => [t.id, t]))
  const columnRootIds = taskIds.filter((id) => byId.has(id))
  const columnSet = new Set(columnRootIds)

  const updatedRoots: Task[] = tasks.value.map((t) => {
    const next: Task = idSet.has(t.id) ? { ...t, status_id: statusId } : { ...t }
    const kids = (next.children || []).map((c) =>
      idSet.has(c.id) ? { ...c, status_id: statusId } : c,
    )
    return { ...next, children: kids }
  })

  const orderedColumn = columnRootIds
    .map((id) => updatedRoots.find((t) => t.id === id))
    .filter((t): t is Task => t != null)

  const result: Task[] = []
  let inserted = false
  for (const t of updatedRoots) {
    if (columnSet.has(t.id)) {
      if (!inserted) {
        result.push(...orderedColumn)
        inserted = true
      }
      continue
    }
    result.push(t)
  }
  if (!inserted) {
    result.push(...orderedColumn)
  }
  tasks.value = result
}

/** Board has no infinite-scroll sentinel — fetch remaining pages after the first. */
async function loadAllForBoard() {
  if (!showBoardView.value) return
  while (showBoardView.value && hasMore.value && !loading.value) {
    const before = loadedPage.value
    await loadMore()
    if (loadedPage.value === before) break
  }
}

function listApiParams(page: number, perPage: number) {
  // Board needs the full backlog (including unclaimed). List/home only show
  // kanban tasks the current user has claimed.
  const onKanbanBoard = isKanbanProjectView.value && viewMode.value === 'board'
  const params: Record<string, string | number> = {
    ...toApiParams(page, perPage),
    workflow_claim_scope: onKanbanBoard ? 'all' : 'mine',
  }
  // List defaults to incomplete; board still needs done-column cards.
  if (onKanbanBoard && params.status === 'incomplete') {
    delete params.status
  }
  if (onKanbanBoard) {
    params.sprint_id = boardSprintKey.value === 'backlog' ? 'none' : boardSprintKey.value
  }
  return params
}

function boardSprintStorageKey(projectId: number) {
  return `gotodo.boardSprint.${projectId}`
}

function pickDefaultSprintKey(projectId: number, sprints: ProjectSprint[]): string {
  try {
    const stored = localStorage.getItem(boardSprintStorageKey(projectId))
    if (stored === 'backlog') return 'backlog'
    if (stored && sprints.some((s) => String(s.id) === stored)) return stored
  } catch {
    /* ignore */
  }
  const active = sprints.find((s) => s.is_active)
  if (active) return String(active.id)
  return 'backlog'
}

async function ensureBoardSprints() {
  const p = activeProjectObj.value
  if (!p || !showBoardView.value) {
    boardSprints.value = []
    sprintsLoadedForProject = null
    return
  }
  if (sprintsLoadedForProject === p.id) return
  try {
    boardSprints.value = await api.listProjectSprints(p.id)
  } catch {
    boardSprints.value = []
  }
  boardSprintKey.value = pickDefaultSprintKey(p.id, boardSprints.value)
  sprintsLoadedForProject = p.id
}

function persistBoardSprint(projectId: number, key: string) {
  try {
    localStorage.setItem(boardSprintStorageKey(projectId), key)
  } catch {
    /* ignore */
  }
}

function onBoardSprintChange() {
  const p = activeProjectObj.value
  if (p) persistBoardSprint(p.id, boardSprintKey.value)
  void reloadInitial()
}

const selectedBoardSprint = computed(() => {
  if (boardSprintKey.value === 'backlog') return null
  const id = parseInt(boardSprintKey.value, 10)
  return boardSprints.value.find((s) => s.id === id) ?? null
})

function currentBoardSprintId(): number | null | undefined {
  if (!showBoardView.value) return undefined
  if (boardSprintKey.value === 'backlog') return null
  const n = parseInt(boardSprintKey.value, 10)
  return Number.isNaN(n) ? undefined : n
}

function openAddFromView() {
  openAdd(undefined, filters.project, null, currentBoardSprintId())
}

/** Prefer board when entering a kanban project so unclaimed backlog is visible. */
function ensureKanbanBoardDefault() {
  if (!isKanbanProjectView.value) return false
  if (viewMode.value === 'board') return false
  viewMode.value = 'board'
  try {
    localStorage.setItem(VIEW_MODE_KEY, 'board')
  } catch {
    /* ignore */
  }
  return true
}

function canWriteTask(task: Task): boolean {
  if (isViewerProjectView.value) return false
  if (task.project_id) {
    const p = projects.value.find((pr) => pr.id === task.project_id)
    if (p && p.role === 'viewer') return false
  }
  return true
}

function canMoveTaskProject(task: Task): boolean {
  const pid = task.project_id
  if (!pid) return true
  const p = projects.value.find((pr) => pr.id === pid)
  return !!p && isProjectOwner(p)
}

const canBulkMoveProject = computed(() =>
  selected.value.every((id) => {
    const found = findTaskInTree(id)
    return !!found && canMoveTaskProject(found.task)
  }),
)

/** Open edit for writers, view-only for project viewers. */
function openTaskDetails(id: number) {
  const found = findTaskInTree(id)
  if (found && !canWriteTask(found.task)) {
    openView(id)
    return
  }
  if (!found && isViewerProjectView.value) {
    openView(id)
    return
  }
  openEdit(id)
}

const hasMore = computed(() => loadedPage.value < totalPages.value)
const sortableEnabled = computed(() => filters.sort !== 'priority' && !loading.value)

function getTodayStr() {
  return new Date().toISOString().slice(0, 10)
}

function getTomorrowStr() {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return d.toISOString().slice(0, 10)
}

function getNextWeekStr() {
  const d = new Date()
  d.setDate(d.getDate() + 7)
  return d.toISOString().slice(0, 10)
}

function taskMatchesBoardSprint(task: Task): boolean {
  if (!isKanbanProjectView.value || viewMode.value !== 'board') return true
  const assigned = task.sprint_id && task.sprint_id > 0 ? task.sprint_id : 0
  if (boardSprintKey.value === 'backlog') return assigned === 0
  return assigned === parseInt(boardSprintKey.value, 10)
}

function taskMatchesCurrentFilters(task: Task): boolean {
  if (!taskMatchesStatusFilter(task)) return false
  if (!taskMatchesBoardSprint(task)) return false
  if (filters.project === '0' && task.project_id != null) return false
  if (filters.project && filters.project !== '0') {
    const pid = parseInt(filters.project, 10)
    if (!Number.isNaN(pid) && task.project_id !== pid) return false
  }
  if (filters.priority && String(task.priority) !== filters.priority) return false
  if (filters.tag) {
    const q = filters.tag.toLowerCase()
    const byId = String(parseInt(filters.tag, 10)) === filters.tag
    if (!task.tags?.some((t) => (byId && String(t.id) === filters.tag) || t.name.toLowerCase() === q)) {
      return false
    }
  }
  if (filters.search) {
    const q = filters.search.toLowerCase()
    const hay = `${task.title} ${task.description}`.toLowerCase()
    if (!hay.includes(q)) return false
  }
  return true
}

function findTaskInTree(id: number): { parent: Task | null; task: Task } | null {
  for (const t of tasks.value) {
    if (t.id === id) return { parent: null, task: t }
    const child = t.children?.find((c) => c.id === id)
    if (child) return { parent: t, task: child }
  }
  return null
}

function refreshParentCounts(parent: Task) {
  const kids = parent.children || []
  parent.child_count = kids.length
  parent.children_completed = kids.filter((c) => c.completed).length
}

function registerTaskAdded(task: Task) {
  if (task.parent_id) {
    const parent = tasks.value.find((t) => t.id === task.parent_id)
    if (parent) {
      if (!parent.children) parent.children = []
      if (!parent.children.some((c) => c.id === task.id)) {
        parent.children = [...parent.children, task]
        refreshParentCounts(parent)
      }
      if (task.completed) completedCount.value += 1
      else incompleteCount.value += 1
      return
    }
    // Parent not on the current page — don't insert a root row for a subtask.
    if (task.completed) completedCount.value += 1
    else incompleteCount.value += 1
    return
  }
  total.value += 1
  if (task.completed) completedCount.value += 1
  else incompleteCount.value += 1
  if (!taskMatchesCurrentFilters(task) || tasks.value.some((t) => t.id === task.id)) return
  const withChildren = { ...task, children: task.children || [] }
  tasks.value = [...tasks.value, withChildren]
}

function removeTaskLocally(task: Task) {
  const found = findTaskInTree(task.id)
  if (!found) return
  if (found.parent) {
    found.parent.children = (found.parent.children || []).filter((c) => c.id !== task.id)
    refreshParentCounts(found.parent)
    if (task.completed) completedCount.value = Math.max(0, completedCount.value - 1)
    else incompleteCount.value = Math.max(0, incompleteCount.value - 1)
    selected.value = selected.value.filter((id) => id !== task.id)
    return
  }
  const childIds = (task.children || []).map((c) => c.id)
  tasks.value = tasks.value.filter((t) => t.id !== task.id)
  total.value = Math.max(0, total.value - 1)
  if (task.completed) completedCount.value = Math.max(0, completedCount.value - 1)
  else incompleteCount.value = Math.max(0, incompleteCount.value - 1)
  for (const c of task.children || []) {
    if (c.completed) completedCount.value = Math.max(0, completedCount.value - 1)
    else incompleteCount.value = Math.max(0, incompleteCount.value - 1)
  }
  selected.value = selected.value.filter((id) => id !== task.id && !childIds.includes(id))
}

function taskMatchesStatusFilter(task: Task) {
  // Board keeps done-column cards even when the list default is incomplete.
  if (showBoardView.value) {
    if (filters.status === 'complete') return task.completed
    return true
  }
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

/** Nested children spread onto the existing row; omitted JSON nulls must not keep old values. */
function mergeNestedChild(existing: Task, updated: Task): Task {
  return {
    ...existing,
    ...updated,
    claimed_by: updated.claimed_by ?? null,
    claimed_by_name: updated.claimed_by_name || '',
    children: undefined,
  }
}

function applyTaskUpdate(updated: Task) {
  const found = findTaskInTree(updated.id)
  const previous = found?.task ?? null

  if (updated.parent_id) {
    if (found?.parent) {
      adjustCompletionCounts(previous!.completed, updated.completed)
      found.parent.children = (found.parent.children || []).map((c) =>
        c.id === updated.id ? mergeNestedChild(c, updated) : c,
      )
      refreshParentCounts(found.parent)
      return
    }
    const parent = tasks.value.find((t) => t.id === updated.parent_id)
    if (parent) {
      // Newly nested or moved under a parent already in the list
      if (found && !found.parent) {
        tasks.value = tasks.value.filter((t) => t.id !== updated.id)
        total.value = Math.max(0, total.value - 1)
      }
      if (!parent.children) parent.children = []
      parent.children = [...parent.children.filter((c) => c.id !== updated.id), { ...updated, children: undefined }]
      refreshParentCounts(parent)
      if (previous) adjustCompletionCounts(previous.completed, updated.completed)
      return
    }
  }

  if (!taskMatchesCurrentFilters(updated)) {
    if (found) {
      removeTaskLocally(found.task)
    }
    if (previous) adjustCompletionCounts(previous.completed, updated.completed)
    return
  }

  if (found && !found.parent) {
    adjustCompletionCounts(found.task.completed, updated.completed)
    const idx = tasks.value.findIndex((t) => t.id === updated.id)
    const existingChildren = found.task.children || updated.children || []
    tasks.value[idx] = {
      ...updated,
      children: existingChildren,
      child_count: existingChildren.length,
      children_completed: existingChildren.filter((c) => c.completed).length,
    }
    return
  }

  if (found?.parent && !updated.parent_id) {
    // Promoted to root
    found.parent.children = (found.parent.children || []).filter((c) => c.id !== updated.id)
    refreshParentCounts(found.parent)
    tasks.value = [...tasks.value, { ...updated, children: [] }]
    total.value += 1
    return
  }

  tasks.value = [...tasks.value, { ...updated, children: updated.children || [] }]
}

function reorderLocalTasks(orderedIds: number[], parentId?: number | null) {
  if (parentId) {
    const parent = tasks.value.find((t) => t.id === parentId)
    if (!parent?.children) return
    parent.children = orderedIds
      .map((id) => parent.children!.find((c) => c.id === id))
      .filter((t): t is Task => !!t)
    return
  }
  const reordered = orderedIds
    .map((id) => tasks.value.find((t) => t.id === id))
    .filter((t): t is Task => !!t)
  const leftover = tasks.value.filter((t) => !orderedIds.includes(t.id))
  tasks.value = [...reordered, ...leftover]
}

async function saveReorder(orderedIds: number[], parentId?: number | null) {
  reorderLocalTasks(orderedIds, parentId)
  try {
    if (parentId) {
      await api.reorderTasks({
        task_ids: orderedIds,
        favorite: false,
        parent_id: parentId,
      })
      return
    }
    const byId = new Map(tasks.value.map((t) => [t.id, t]))
    const favoriteIds = orderedIds.filter((id) => byId.get(id)?.favorite)
    const regularIds = orderedIds.filter((id) => !byId.get(id)?.favorite)
    const project = filters.project || undefined
    if (favoriteIds.length) {
      await api.reorderTasks({
        task_ids: favoriteIds,
        favorite: true,
        project,
      })
    }
    if (regularIds.length) {
      await api.reorderTasks({
        task_ids: regularIds,
        favorite: false,
        project,
      })
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save task order', 'error')
    await reloadInitial()
  }
}

const { refresh: refreshSortable } = useTaskSortable(
  taskListEl,
  sortableEnabled,
  saveReorder,
)

async function loadMeta() {
  try {
    const [projs, views] = await Promise.all([
      api.listProjects(),
      api.listSavedViews(),
    ])
    projects.value = projs
    savedViews.value = views
    await loadTags()
  } catch {
    /* non-fatal */
  }
}

async function loadTags() {
  try {
    if (filters.project === '0') {
      tags.value = await api.listTags({ project_id: 0 })
    } else if (filters.project) {
      const pid = parseInt(filters.project, 10)
      tags.value = Number.isNaN(pid) ? await api.listTags() : await api.listTags({ project_id: pid })
    } else {
      tags.value = await api.listTags()
    }
    if (filters.tag) {
      if (tagFilterByName.value) {
        const byId = tags.value.find((t) => String(t.id) === filters.tag)
        if (byId) {
          setFilter('tag', byId.name)
        } else if (!displayTags.value.some((t) => t.name.toLowerCase() === filters.tag.toLowerCase())) {
          setFilter('tag', '')
        }
      } else {
        const byName = tags.value.find((t) => t.name.toLowerCase() === filters.tag.toLowerCase())
        if (byName) {
          setFilter('tag', String(byName.id))
        } else if (!tags.value.some((t) => String(t.id) === filters.tag)) {
          setFilter('tag', '')
        }
      }
    }
  } catch {
    tags.value = []
  }
}

function syncFiltersFromRoute() {
  const qView = typeof route.query.view === 'string' ? route.query.view : null
  const qProject = typeof route.query.project === 'string' ? route.query.project : null

  if (qView) {
    const view = savedViews.value.find((v) => String(v.id) === qView)
    if (view) {
      activeViewId.value = String(view.id)
      applySavedViewFilters(view.filter || {})
      search.value = filters.search
      ensureKanbanBoardDefault()
      return
    }
  }

  if (qProject !== null) {
    activeViewId.value = null
    setFilter('project', qProject)
    ensureKanbanBoardDefault()
    return
  }
}

async function consumeTaskQuery() {
  const raw = Array.isArray(route.query.task) ? route.query.task[0] : route.query.task
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) return
  const nextQuery = { ...route.query }
  delete nextQuery.task
  await router.replace({ query: nextQuery })
  openTaskDetails(id)
}

async function reloadInitial() {
  const generation = ++reloadGeneration
  const projectKey = filters.project || ''
  loading.value = true
  loadedPage.value = 0
  selected.value = []
  // Drop stale cards immediately when switching projects so the previous
  // project's tasks never flash under the new project context.
  if (loadedProjectKey.value !== null && loadedProjectKey.value !== projectKey) {
    tasks.value = []
    total.value = 0
    completedCount.value = 0
    incompleteCount.value = 0
  }
  try {
    await ensureBoardSprints()
    if (generation !== reloadGeneration) return
    const perPage = user.value?.items_per_page || 50
    const list = await api.listTasks(listApiParams(1, perPage))
    if (generation !== reloadGeneration) return
    tasks.value = list.tasks
    loadedPage.value = 1
    total.value = list.total
    totalPages.value = list.total_pages
    completedCount.value = list.completed_count
    incompleteCount.value = list.incomplete_count
    search.value = filters.search
    loadedProjectKey.value = projectKey
  } catch (err) {
    if (generation !== reloadGeneration) return
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    if (generation !== reloadGeneration) return
    loading.value = false
    await nextTick()
    refreshSortable()
    if (showBoardView.value) {
      void loadAllForBoard()
    }
  }
}

async function loadMore() {
  if (loadingMore.value || loading.value || !hasMore.value) return
  loadingMore.value = true
  try {
    const perPage = user.value?.items_per_page || 50
    const nextPage = loadedPage.value + 1
    const list = await api.listTasks(listApiParams(nextPage, perPage))
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

watch(
  () => filters.project,
  () => {
    void loadTags()
  },
)

function isRemovedTagFilter(): boolean {
  const q = (filters.tag || '').trim().toLowerCase()
  if (q === 'removed') return true
  const tag = tags.value.find((t) => String(t.id) === filters.tag)
  return !!tag && tag.name.toLowerCase() === 'removed'
}

watch(lastSavedTask, async (task) => {
  if (!task) return
  lastSavedTask.value = null
  if (isArchivedTask(task) !== isRemovedTagFilter()) {
    await reloadInitial()
    return
  }
  if (task.parent_id) expandParent(task.parent_id)
  const exists = !!findTaskInTree(task.id)
  if (exists) {
    applyTaskUpdate(task)
  } else {
    registerTaskAdded(task)
  }
  await nextTick()
  refreshSortable()
})

watch(lastDeletedTask, async (payload) => {
  if (!payload) return
  lastDeletedTask.value = null
  const found = findTaskInTree(payload.id)
  if (payload.mode === 'reparent') {
    if (found) {
      removeTaskLocally({ ...found.task, children: [], child_count: 0 })
    }
    await reloadInitial()
  } else if (found) {
    removeTaskLocally(found.task)
  }
  await nextTick()
  refreshSortable()
})

watch(
  () => route.query,
  (next, prev) => {
    const nextRest = { ...next }
    const prevRest = { ...(prev || {}) }
    delete nextRest.task
    delete prevRest.task
    if (JSON.stringify(nextRest) !== JSON.stringify(prevRest)) {
      syncFiltersFromRoute()
      void reloadInitial()
    }
    if (next.task) {
      void consumeTaskQuery()
    }
  },
)

async function toggleComplete(task: Task) {
  if (!canWriteTask(task)) return
  try {
    const updated = await api.patchTask(task.id, { completed: !task.completed })
    applyTaskUpdate(updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function handleInlineTaskPatch(payload: { id: number; title?: string; description?: string }) {
  try {
    const updated = await api.patchTask(payload.id, {
      title: payload.title,
      description: payload.description,
    })
    applyTaskUpdate(updated)
    toast.push('Task updated', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeTask(task: Task) {
  if (!canWriteTask(task)) return
  const childCount = task.child_count ?? task.children?.length ?? 0
  if (childCount > 0) {
    deleteDialogTask.value = task
    deleteDialogOpen.value = true
    return
  }
  const ok = await askConfirm({
    title: 'Delete task?',
    message: `Permanently delete “${task.title}”? This cannot be undone.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  await performDelete(task, { mode: 'cascade' })
}

async function performDelete(
  task: Task,
  opts: { mode: 'cascade' | 'reparent'; new_parent_id?: number | null },
) {
  try {
    const res = await api.deleteTask(task.id, opts)
    undoToken.value = res.undo_token || null
    if (opts.mode === 'reparent') {
      // Children moved; drop parent only and refresh to show new homes.
      const withoutChildren = { ...task, children: [], child_count: 0 }
      removeTaskLocally(withoutChildren)
      await reloadInitial()
    } else {
      removeTaskLocally(task)
    }
    toast.push(undoToken.value ? 'Task deleted — undo available' : 'Task deleted', 'info')
    await nextTick()
    refreshSortable()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  } finally {
    deleteDialogOpen.value = false
    deleteDialogTask.value = null
  }
}

function isParentExpanded(taskId: number) {
  return expandedParentIds.value.has(taskId)
}

async function toggleParentExpanded(taskId: number) {
  const next = new Set(expandedParentIds.value)
  if (next.has(taskId)) next.delete(taskId)
  else next.add(taskId)
  expandedParentIds.value = next
  await nextTick()
  refreshSortable()
}

function expandParent(taskId: number) {
  if (expandedParentIds.value.has(taskId)) return
  const next = new Set(expandedParentIds.value)
  next.add(taskId)
  expandedParentIds.value = next
}

function openAddSubtask(task: Task) {
  if (task.project_id) {
    const p = projects.value.find((pr) => pr.id === task.project_id)
    if (p && isArchivedProject(p)) {
      toast.push('Cannot add tasks to an archived project', 'error')
      return
    }
  }
  expandParent(task.id)
  openAdd(undefined, task.project_id ?? null, { id: task.id, title: task.title })
}

async function onDeleteCascade() {
  if (!deleteDialogTask.value) return
  await performDelete(deleteDialogTask.value, { mode: 'cascade' })
}

async function onDeleteReparent(newParentId: number | null) {
  if (!deleteDialogTask.value) return
  await performDelete(deleteDialogTask.value, { mode: 'reparent', new_parent_id: newParentId })
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

function toggleSelectMode() {
  selectMode.value = !selectMode.value
  if (!selectMode.value) selected.value = []
}

function toggleSelectAll(checked: boolean) {
  selected.value = checked ? [...flatSelectableIds.value] : []
}

async function toggleCompleteChild(child: Task) {
  if (!canWriteTask(child)) return
  try {
    const updated = await api.patchTask(child.id, { completed: !child.completed })
    applyTaskUpdate(updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function bulk(action: string, extra: Record<string, unknown> = {}) {
  if (!selected.value.length || isViewerProjectView.value) return
  if (action === 'move_project' && !canBulkMoveProject.value) return
  if (action === 'delete') {
    const nestedCount = selected.value.reduce((n, id) => {
      const t = findTaskInTree(id)?.task
      return n + (t?.child_count ?? t?.children?.length ?? 0)
    }, 0)
    const ok = await askConfirm({
      title: 'Delete tasks?',
      message:
        nestedCount > 0
          ? `Delete ${selected.value.length} selected task${selected.value.length === 1 ? '' : 's'}? This also deletes ${nestedCount} nested subtask${nestedCount === 1 ? '' : 's'}.`
          : `Delete ${selected.value.length} selected task${selected.value.length === 1 ? '' : 's'}?`,
      confirmLabel: 'Delete',
      danger: true,
    })
    if (!ok) return
  }
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

function setFilterAndReload(key: Parameters<typeof setFilter>[0], value: string) {
  activeViewId.value = null
  setFilter(key, value)
  void reloadInitial()
}

function clearFilters() {
  activeViewId.value = null
  search.value = ''
  resetFilters()
  void reloadInitial()
}

function selectProjectFilter(id: string) {
  activeViewId.value = null
  if (filters.project === id) {
    setFilterAndReload('project', '')
    return
  }
  setFilter('project', id)
  // Switch to board before reload so claim-scope=all is used for kanban projects.
  ensureKanbanBoardDefault()
  void reloadInitial()
}

function selectSavedViewFilter(id: string) {
  const view = savedViews.value.find((v) => String(v.id) === id)
  if (view) {
    activeViewId.value = String(view.id)
    applySavedViewFilters(view.filter || {})
    search.value = filters.search
    void reloadInitial()
  }
}

async function saveCurrentView() {
  if (!newViewName.value.trim()) return
  try {
    const created = await api.createSavedView({
      name: newViewName.value.trim(),
      filter: {
        status: filters.status || undefined,
        due: filters.due || undefined,
        project: filters.project || undefined,
        priority: filters.priority || undefined,
        tag: filters.tag || undefined,
        sort: filters.sort || undefined,
        search: filters.search || undefined,
      },
    })
    toast.push('View saved successfully!', 'success')
    activeViewId.value = String(created.id)
    newViewName.value = ''
    showSaveViewModal.value = false
    await loadMeta()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to save view', 'error')
  }
}

async function createProject() {
  if (!newProjectName.value.trim()) return
  try {
    await api.createProject(newProjectName.value.trim(), newProjectDescription.value.trim())
    toast.push('Project created!', 'success')
    newProjectName.value = ''
    newProjectDescription.value = ''
    showAddProjectModal.value = false
    await loadMeta()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to create project', 'error')
  }
}

function openEditProject(proj: Project) {
  editingProject.value = proj
  showEditProjectModal.value = true
}

function closeEditProject() {
  showEditProjectModal.value = false
  editingProject.value = null
}

async function onProjectSettingsSaved() {
  await loadMeta()
  await reloadInitial()
  // Keep modal open but refresh project reference from list
  if (editingProject.value) {
    const updated = projects.value.find((p) => p.id === editingProject.value!.id)
    if (updated) editingProject.value = updated
  }
}

async function onProjectSettingsChanged() {
  sprintsLoadedForProject = null
  await loadMeta()
  await reloadInitial()
  kanbanColumnsRev.value++
  if (editingProject.value) {
    const updated = projects.value.find((p) => p.id === editingProject.value!.id)
    if (updated) editingProject.value = updated
  }
}

async function onProjectColumnsChanged() {
  kanbanColumnsRev.value++
}

async function onReorderProjects(projectIds: number[]) {
  const previous = [...projects.value]
  const ownedActive = projects.value.filter((p) => (!p.role || p.role === 'owner') && !isArchivedProject(p))
  const rest = projects.value.filter((p) => !ownedActive.some((o) => o.id === p.id))
  const byId = new Map(ownedActive.map((p) => [p.id, p]))
  const nextOwned = projectIds.map((id) => byId.get(id)!).filter(Boolean)
  projects.value = [...nextOwned, ...rest]
  try {
    await api.reorderProjects(projectIds)
  } catch (err) {
    projects.value = previous
    toast.push(err instanceof APIError ? err.message : 'Could not reorder projects', 'error')
  }
}

function selectHome() {
  activeViewId.value = null
  if (filters.project) {
    setFilterAndReload('project', '')
  }
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

function getFocusableTaskIds(): number[] {
  const ids: number[] = []
  const pushVisible = (list: Task[]) => {
    for (const task of list) {
      ids.push(task.id)
      if (task.children?.length && isParentExpanded(task.id)) {
        for (const child of task.children) ids.push(child.id)
      }
    }
  }
  pushVisible(rootTasks.value)
  return ids
}

function focusSearchInput() {
  const el = document.getElementById('task-search') as HTMLInputElement | null
  if (!el) return
  el.focus()
  el.select()
}

function shortcutEditTask(id: number) {
  openTaskDetails(id)
}

function shortcutDeleteTask(id: number) {
  const found = findTaskInTree(id)
  if (!found) return
  void removeTask(found.task)
}

function shortcutToggleComplete(id: number) {
  const found = findTaskInTree(id)
  if (!found) return
  if (found.parent) void toggleCompleteChild(found.task)
  else void toggleComplete(found.task)
}

function showShortcutsHintOnce() {
  try {
    if (localStorage.getItem('shortcuts-hint-dismissed') === '1') return
    localStorage.setItem('shortcuts-hint-dismissed', '1')
  } catch {
    return
  }
  window.setTimeout(() => {
    toast.push('Tip: Press ? for keyboard shortcuts.', 'info', 6000)
  }, 1500)
}

onMounted(async () => {
  registerTaskShortcuts({
    newTask: () => {
      if (!canAddTasks.value) return
      openAddFromView()
    },
    focusSearch: focusSearchInput,
    getFocusableTaskIds,
    editTask: shortcutEditTask,
    deleteTask: shortcutDeleteTask,
    toggleComplete: shortcutToggleComplete,
  })
  showShortcutsHintOnce()
  await loadMeta()
  syncFiltersFromRoute()
  await reloadInitial()
  await consumeTaskQuery()
})

useLiveUpdates((event) => {
  if (event.type === 'task.commented') return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  if (event.type === 'project.updated') {
    void loadMeta()
    kanbanColumnsRev.value++
  }
  void reloadInitial()
}, 500)

onUnmounted(() => {
  unregisterTaskShortcuts()
})
</script>

<template>
  <div class="ordryn-main-layout">
    <!-- Collapsible & Responsive Warm Sidebar -->
    <ModernSidebar
      :collapsed="sidebarCollapsed"
      :projects="projects"
      :saved-views="savedViews"
      :active-project="filters.project"
      :active-view="activeViewId || undefined"
      @toggle-collapse="toggleSidebar"
      @select-home="selectHome"
      @select-project="selectProjectFilter"
      @select-view="selectSavedViewFilter"
      @add-project="showAddProjectModal = true"
      @edit-project="openEditProject"
      @reorder-projects="onReorderProjects"
      @add-view="showSaveViewModal = true"
    />

    <!-- Main Content Area -->
    <div class="ordryn-main-pane flex-grow-1 p-2 p-sm-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div>
        <!-- Single Compact Header Toolbar: Stats Pills, Import/Export, Add Task -->
        <div class="d-flex align-items-center justify-content-between flex-wrap gap-2 mb-2">
          <!-- Compact Inline Task Counts -->
          <div class="d-flex align-items-center gap-1 flex-wrap text-muted small oryryn-task-stats">
            <span class="badge rounded-pill bg-primary bg-opacity-10 text-primary border border-primary border-opacity-20 px-2 py-1">
              {{ total }} <span class="d-none d-sm-inline">tasks</span>
            </span>
            <span class="badge rounded-pill bg-success bg-opacity-10 text-success border border-success border-opacity-20 px-2 py-1">
              {{ completedCount }} <span class="d-none d-sm-inline">done</span>
            </span>
            <span class="badge rounded-pill bg-warning bg-opacity-10 text-warning border border-warning border-opacity-20 px-2 py-1">
              {{ incompleteCount }} <span class="d-none d-sm-inline">open</span>
            </span>
            <span v-if="isViewerProjectView" class="badge rounded-pill bg-info bg-opacity-10 text-info border border-info border-opacity-20 px-2 py-1">
              Viewer
            </span>
            <span v-if="isArchivedProjectView" class="badge rounded-pill bg-secondary bg-opacity-10 text-secondary border border-secondary border-opacity-20 px-2 py-1">
              Archived
            </span>
          </div>

          <!-- Actions Group: Import/Export & Add Task -->
          <div class="d-flex align-items-center gap-2">
            <button
              v-if="!isViewerProjectView && !isArchivedProjectView && showTaskTable"
              type="button"
              class="btn btn-sm btn-outline-secondary rounded-pill px-2 py-1 d-md-none"
              :class="{ active: isSelecting }"
              :aria-pressed="isSelecting"
              title="Select tasks"
              @click="toggleSelectMode"
            >
              <i class="bi bi-check2-square" />
            </button>
            <!-- Import/Export Dropdown -->
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
                  <button type="button" class="dropdown-item small" @click="exportTasks('csv', true)">
                    <i class="bi bi-download me-2" />Export Filtered CSV
                  </button>
                </li>
                <li>
                  <button type="button" class="dropdown-item small" @click="exportTasks('json', true)">
                    <i class="bi bi-download me-2" />Export Filtered JSON
                  </button>
                </li>
              </ul>
            </div>

            <!-- Add Task Button (Hidden for read-only viewer role) -->
            <button
              v-if="canAddTasks"
              type="button"
              class="btn btn-sm btn-success rounded-pill px-3 py-1 shadow-xs d-none d-md-flex align-items-center gap-1"
              @click="openAddFromView"
            >
              <i class="bi bi-plus-lg" />
              <span>Add Task</span>
            </button>

            <!-- Undo Delete Button -->
            <button
              v-if="undoToken"
              type="button"
              class="btn btn-sm btn-outline-warning rounded-pill px-3 py-1"
              @click="undoDelete"
            >
              <i class="bi bi-arrow-counterclockwise me-1" />Undo
            </button>
          </div>
        </div>

        <!-- Modern Filter Bar (Search, Folded Filters, Density + List/Board) -->
        <ModernTaskFilterBar
          :status="filters.status"
          :tag="filters.tag"
          :priority="filters.priority"
          :due-date-preset="filters.due"
          :sort="filters.sort"
          :search="search"
          :density="density"
          :tags="displayTags"
          :tag-by-name="tagFilterByName"
          :show-view-mode="isKanbanProjectView"
          :view-mode="viewMode"
          @update:status="setFilterAndReload('status', $event)"
          @update:tag="setFilterAndReload('tag', $event)"
          @update:priority="setFilterAndReload('priority', $event)"
          @update:due-date-preset="setFilterAndReload('due', $event)"
          @update:sort="setFilterAndReload('sort', $event)"
          @update:search="search = $event; setFilter('search', $event); reloadInitial()"
          @update:density="density = $event"
          @update:view-mode="setViewMode($event)"
          @clear-filters="clearFilters"
        />

        <!-- Kanban board -->
        <div v-if="showBoardView" class="mb-3">
          <div
            class="d-flex flex-wrap align-items-center gap-2 mb-3 p-2 rounded-3 border shadow-xs"
            style="background: var(--ordryn-card-bg); border-color: var(--ordryn-card-border) !important;"
          >
            <label class="small fw-bold mb-0" for="board-sprint">Sprint</label>
            <select
              id="board-sprint"
              v-model="boardSprintKey"
              class="form-select form-select-sm"
              style="max-width: 28rem;"
              @change="onBoardSprintChange"
            >
              <option value="backlog">{{ activeProjectObj?.backlog_name || 'Backlog' }}</option>
              <option v-for="s in boardSprints" :key="s.id" :value="String(s.id)">
                {{ sprintOptionLabel(s, { activeSuffix: true, lockedSuffix: true }) }}
              </option>
            </select>
            <span v-if="selectedBoardSprint" class="small text-muted">
              <template v-if="selectedBoardSprint.description">{{ selectedBoardSprint.description }} · </template>
              {{ selectedBoardSprint.start_date }} – {{ selectedBoardSprint.end_date }}
              <template v-if="selectedBoardSprint.lock_date">
                · {{ selectedBoardSprint.is_locked ? 'locked' : 'locks' }} {{ selectedBoardSprint.lock_date }}
              </template>
              · {{ selectedBoardSprint.task_count }} task{{ selectedBoardSprint.task_count === 1 ? '' : 's' }}
            </span>
            <span v-else class="small text-muted">Tasks not assigned to a sprint</span>
          </div>
          <div v-if="loading && !tasks.length" class="text-center py-4 text-muted">
            <div class="spinner-border spinner-border-sm me-2" role="status" />Loading tasks…
          </div>
          <KanbanBoard
            v-else-if="activeProjectObj"
            :key="activeProjectObj.id"
            :project-id="activeProjectObj.id"
            :tasks="tasks"
            :role="activeProjectObj.role"
            :density="density"
            :columns-rev="kanbanColumnsRev"
            :sprint-filter="boardSprintKey"
            @open-task="openTaskDetails"
            @changed="reloadInitial"
            @task-updated="applyTaskUpdate"
            @board-reorder="applyBoardReorder"
          />
          <div v-if="loadingMore" class="text-center py-2 text-muted small">
            <span class="spinner-border spinner-border-sm me-2" />Loading more tasks…
          </div>
        </div>

        <!-- Sleek Bulk Actions Bar -->
        <div
          v-if="!showBoardView && selected.length && !isViewerProjectView"
          class="bulk-action-bar alert alert-info py-1.5 px-3 rounded-3 shadow-sm d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2"
        >
          <span class="fw-semibold small">{{ selected.length }} task{{ selected.length === 1 ? '' : 's' }} selected</span>
          <div class="d-flex align-items-center gap-2">
            <button type="button" class="btn btn-xs btn-success rounded-pill" @click="bulk('complete')">Complete</button>
            <button type="button" class="btn btn-xs btn-outline-secondary rounded-pill" @click="bulk('incomplete')">Incomplete</button>

            <!-- Compact Feature-Rich "More Actions" Popover Panel -->
            <div class="dropdown d-inline-block">
              <button class="btn btn-xs btn-outline-primary dropdown-toggle rounded-pill" type="button" data-bs-toggle="dropdown" data-bs-auto-close="outside">
                More Actions
              </button>
              <div
                class="dropdown-menu shadow-lg border p-3 rounded-3 mt-1"
                style="width: 280px; max-width: 90vw; background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
              >
                <!-- Move to Project -->
                <div class="mb-3">
                  <label class="form-label text-muted small fw-bold text-uppercase mb-1" style="font-size: 0.7rem;">Move to project...</label>
                  <select v-model="bulkProject" class="form-select form-select-sm mb-2" :disabled="!canBulkMoveProject">
                    <option value="">Select project...</option>
                    <option value="0">No Project</option>
                    <option v-for="p in activeProjects(projects)" :key="p.id" :value="String(p.id)">{{ projectOptionLabel(p) }}</option>
                  </select>
                  <button
                    type="button"
                    class="btn btn-xs btn-outline-primary rounded-pill w-100"
                    :disabled="!canBulkMoveProject || bulkProject === ''"
                    @click="bulk('move_project', { project_id: bulkProject })"
                  >
                    Move
                  </button>
                  <small v-if="!canBulkMoveProject" class="form-hint d-block mt-1">
                    Only the project owner can move this task to another project.
                  </small>
                </div>

                <!-- Select Tag -->
                <div class="mb-3 border-top pt-2">
                  <label class="form-label text-muted small fw-bold text-uppercase mb-1" style="font-size: 0.7rem;">Select tag...</label>
                  <select v-model="bulkTag" class="form-select form-select-sm mb-2">
                    <option value="">Select tag...</option>
                    <option v-for="t in displayTags" :key="t.id" :value="String(t.id)">{{ t.name }}</option>
                  </select>
                  <div class="d-flex gap-2">
                    <button
                      type="button"
                      class="btn btn-xs btn-outline-primary rounded-pill flex-grow-1"
                      :disabled="!bulkTag"
                      @click="bulk('add_tag', { tag_id: parseInt(bulkTag, 10) })"
                    >
                      Add Tag
                    </button>
                    <button
                      type="button"
                      class="btn btn-xs btn-outline-secondary rounded-pill flex-grow-1"
                      :disabled="!bulkTag"
                      @click="bulk('remove_tag', { tag_id: parseInt(bulkTag, 10) })"
                    >
                      Remove Tag
                    </button>
                  </div>
                </div>

                <!-- Priority -->
                <div class="mb-3 border-top pt-2">
                  <label class="form-label text-muted small fw-bold text-uppercase mb-1" style="font-size: 0.7rem;">Priority</label>
                  <select v-model="bulkPriority" class="form-select form-select-sm mb-2">
                    <option value="">Select priority...</option>
                    <option value="3">High Priority</option>
                    <option value="2">Medium Priority</option>
                    <option value="1">Low Priority</option>
                    <option value="0">No Priority</option>
                  </select>
                  <button
                    type="button"
                    class="btn btn-xs btn-outline-primary rounded-pill w-100"
                    :disabled="bulkPriority === ''"
                    @click="bulk('set_priority', { priority: parseInt(bulkPriority, 10) })"
                  >
                    Set Priority
                  </button>
                </div>

                <!-- Due Date -->
                <div class="border-top pt-2">
                  <label class="form-label text-muted small fw-bold text-uppercase mb-1" style="font-size: 0.7rem;">Due Date</label>
                  <div class="btn-group btn-group-sm w-100 mb-2">
                    <button type="button" class="btn btn-xs btn-outline-secondary" @click="bulk('set_due_date', { due_date: getTodayStr() })">Today</button>
                    <button type="button" class="btn btn-xs btn-outline-secondary" @click="bulk('set_due_date', { due_date: getTomorrowStr() })">Tomorrow</button>
                    <button type="button" class="btn btn-xs btn-outline-secondary" @click="bulk('set_due_date', { due_date: getNextWeekStr() })">+1 Wk</button>
                    <button type="button" class="btn btn-xs btn-outline-secondary" @click="bulk('set_due_date', { due_date: '' })">Clear</button>
                  </div>
                  <input v-model="bulkDate" type="date" class="form-control form-control-sm mb-2" />
                  <div class="d-flex gap-2">
                    <button
                      type="button"
                      class="btn btn-xs btn-outline-primary rounded-pill flex-grow-1"
                      :disabled="!bulkDate"
                      @click="bulk('set_due_date', { due_date: bulkDate })"
                    >
                      Set Due
                    </button>
                    <button
                      type="button"
                      class="btn btn-xs btn-outline-secondary rounded-pill flex-grow-1"
                      @click="bulk('set_due_date', { due_date: '' })"
                    >
                      Clear Due
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <button type="button" class="btn btn-xs btn-danger rounded-pill" @click="bulk('delete')">Delete</button>
            <button type="button" class="btn btn-xs btn-link text-muted" @click="selected = []">Deselect</button>
          </div>
        </div>

        <!-- Task Lists Container -->
        <div v-if="!showBoardView" id="task-container" aria-live="polite">
          <div v-if="loading && !tasks.length" class="text-center py-5 text-muted">
            <div class="spinner-border spinner-border-sm me-2" role="status" />Loading tasks…
          </div>

          <div v-else-if="showTaskTable">
            <!-- Merged Header Row: Select All Checkbox -->
            <div class="d-flex align-items-center justify-content-between mb-2 px-1">
              <div class="d-flex align-items-center gap-3">
                <div
                  v-if="!isViewerProjectView"
                  class="form-check align-items-center m-0 p-0"
                  :class="isSelecting ? 'd-flex' : 'd-none d-md-flex'"
                >
                  <input
                    id="select-all-tasks"
                    type="checkbox"
                    class="form-check-input m-0 cursor-pointer"
                    :checked="allSelected"
                    style="width: 0.95rem; height: 0.95rem;"
                    @change="toggleSelectAll(($event.target as HTMLInputElement).checked)"
                  />
                  <label for="select-all-tasks" class="form-check-label small text-muted cursor-pointer ms-1.5">
                    Select all
                  </label>
                </div>
              </div>
              <button
                v-if="hasActiveFilters"
                type="button"
                class="btn btn-link btn-sm text-decoration-none p-0 text-muted small"
                @click="clearFilters"
              >
                <i class="bi bi-x-circle me-1" />Clear active filters
              </button>
            </div>

            <!-- Task List -->
            <div id="task-list" ref="taskListEl">
              <div
                v-for="task in rootTasks"
                :key="task.id"
                class="task-tree-root"
                :data-task-id="task.id"
              >
                <ModernTaskCard
                  :task="task"
                  :selected="selected.includes(task.id)"
                  :selecting="isSelecting"
                  :focused="focusedTaskId === task.id"
                  :density="density"
                  :show-project-pill="!filters.project"
                  :can-write="canWriteTask(task)"
                  :expanded="isParentExpanded(task.id)"
                  @toggle-select="toggleSelect(task.id, $event)"
                  @toggle-complete="toggleComplete(task)"
                  @toggle-expand="toggleParentExpanded(task.id)"
                  @patch-task="handleInlineTaskPatch"
                  @add-subtask="openAddSubtask(task)"
                  @edit="openTaskDetails(task.id)"
                />
                <div
                  v-if="task.children?.length && isParentExpanded(task.id)"
                  class="task-children"
                  :data-parent-id="task.id"
                >
                  <ModernTaskCard
                    v-for="child in task.children"
                    :key="child.id"
                    :task="child"
                    :depth="1"
                    :selected="selected.includes(child.id)"
                    :selecting="isSelecting"
                    :focused="focusedTaskId === child.id"
                    :density="density"
                    :show-project-pill="false"
                    :can-write="canWriteTask(child)"
                    @toggle-select="toggleSelect(child.id, $event)"
                    @toggle-complete="toggleCompleteChild(child)"
                    @patch-task="handleInlineTaskPatch"
                    @edit="openTaskDetails(child.id)"
                  />
                </div>
              </div>

              <!-- Empty Search Results / unclaimed kanban backlog -->
              <div
                v-if="!tasks.length && (hasActiveFilters || isKanbanProjectView)"
                class="text-center py-5 rounded-3 border"
                style="background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
              >
                <template v-if="isKanbanProjectView">
                  <p class="text-muted mb-2">
                    No claimed tasks in your list. Unclaimed work lives on the board — claim a task to see it here.
                  </p>
                  <button type="button" class="btn btn-sm btn-primary rounded-pill" @click="setViewMode('board')">
                    <i class="bi bi-kanban me-1" />Open board
                  </button>
                </template>
                <template v-else>
                  <p class="text-muted mb-2">No tasks match your active filters.</p>
                  <button type="button" class="btn btn-sm btn-outline-primary rounded-pill" @click="clearFilters">
                    <i class="bi bi-x-circle me-1" />Clear filters
                  </button>
                </template>
              </div>

              <!-- Infinite Scroll Sentinel -->
              <div v-if="hasMore" ref="loadMoreSentinel" class="text-center py-3 text-muted small">
                <span v-if="loadingMore" class="spinner-border spinner-border-sm me-2" />
                {{ loadingMore ? 'Loading more tasks…' : 'Scroll for more tasks' }}
              </div>
            </div>
          </div>

          <!-- Zero State (No tasks match search) -->
          <div
            v-else-if="isSearching"
            class="text-center py-5 rounded-3 border"
            style="background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
          >
            <p class="text-muted mb-2">No tasks match your search query.</p>
            <button type="button" class="btn btn-sm btn-outline-primary rounded-pill" @click="clearFilters">
              Clear Search
            </button>
          </div>

          <!-- Empty State (No tasks at all) -->
          <div
            v-else
            class="text-center py-5 rounded-3 border shadow-xs"
            style="background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
          >
            <i class="bi bi-clipboard-check display-4 text-muted opacity-50" />
            <h4 class="mt-3 fw-bold">No tasks yet</h4>
            <p class="text-muted">Get started by creating your first task.</p>
            <button v-if="canAddTasks" type="button" class="btn btn-success rounded-pill px-4" @click="openAddFromView">
              <i class="bi bi-plus-lg me-1" /> Add Task
            </button>
            <p v-else-if="isArchivedProjectView" class="text-muted small mb-0">This project is archived, so new tasks cannot be added.</p>
          </div>
        </div>
      </div>

      <!-- Save Current View Modal -->
      <div v-if="showSaveViewModal" class="modal fade show d-block" style="background: rgba(0,0,0,0.5);" tabindex="-1">
        <div class="modal-dialog modal-dialog-centered">
          <div
            class="modal-content border-0 shadow"
            style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
          >
            <div class="modal-header border-0 pb-0">
              <h5 class="modal-title fw-bold">Save Current View</h5>
              <button type="button" class="btn-close" @click="showSaveViewModal = false" />
            </div>
            <div class="modal-body py-3">
              <p class="text-muted small mb-3">Save your current active filters into a custom named View in the sidebar.</p>
              <div class="mb-3">
                <label for="new-view-name" class="form-label small fw-bold">View Name</label>
                <input
                  id="new-view-name"
                  v-model="newViewName"
                  type="text"
                  class="form-control"
                  placeholder="e.g., High Priority Work"
                  @keyup.enter="saveCurrentView"
                />
              </div>
            </div>
            <div class="modal-footer border-0 pt-0 justify-content-end gap-2">
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="showSaveViewModal = false">Cancel</button>
              <button type="button" class="btn btn-sm btn-primary px-3" :disabled="!newViewName.trim()" @click="saveCurrentView">Save View</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Add Project Modal -->
      <div v-if="showAddProjectModal" class="modal fade show d-block" style="background: rgba(0,0,0,0.5);" tabindex="-1">
        <div class="modal-dialog modal-dialog-centered">
          <div
            class="modal-content border-0 shadow"
            style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
          >
            <div class="modal-header border-0 pb-0">
              <h5 class="modal-title fw-bold">Create New Project</h5>
              <button type="button" class="btn-close" @click="showAddProjectModal = false" />
            </div>
            <div class="modal-body py-3">
              <div class="mb-3">
                <label for="new-project-name" class="form-label small fw-bold">Project Name</label>
                <input
                  id="new-project-name"
                  v-model="newProjectName"
                  type="text"
                  class="form-control"
                  maxlength="50"
                  placeholder="e.g., Marketing Campaign"
                  @keyup.enter="createProject"
                />
              </div>
              <div class="mb-3">
                <label for="new-project-description" class="form-label small fw-bold">Description</label>
                <textarea
                  id="new-project-description"
                  v-model="newProjectDescription"
                  class="form-control"
                  rows="3"
                  maxlength="1000"
                  placeholder="Optional details about this project"
                />
              </div>
            </div>
            <div class="modal-footer border-0 pt-0 justify-content-end gap-2">
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="showAddProjectModal = false">Cancel</button>
              <button type="button" class="btn btn-sm btn-success px-3" :disabled="!newProjectName.trim()" @click="createProject">Create Project</button>
            </div>
          </div>
        </div>
      </div>

      <ProjectSettingsModal
        :open="showEditProjectModal"
        :project="editingProject"
        @close="closeEditProject"
        @saved="onProjectSettingsSaved"
        @changed="onProjectSettingsChanged"
        @columns-changed="onProjectColumnsChanged"
      />

      <DeleteTaskDialog
        :open="deleteDialogOpen"
        :task="deleteDialogTask"
        :root-tasks="tasks"
        @cancel="deleteDialogOpen = false; deleteDialogTask = null"
        @cascade="onDeleteCascade"
        @reparent="onDeleteReparent"
      />

      <!-- Clean Reusable Footer inside right content area -->
      <AppFooter />

      <button
        v-if="canAddTasks"
        type="button"
        class="ordryn-mobile-fab d-md-none"
        aria-label="Add task"
        @click="openAddFromView"
      >
        <i class="bi bi-plus-lg" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.btn-xs {
  padding: 0.15rem 0.5rem;
  font-size: 0.75rem;
}
</style>
