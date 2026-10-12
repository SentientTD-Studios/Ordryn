<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Organization, Project, SavedView, SavedViewFilter, Tag, Task } from '@/api/types'
import { APIError } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import ModernTaskFilterBar from '@/components/modern/ModernTaskFilterBar.vue'
import ModernTaskCard from '@/components/modern/ModernTaskCard.vue'
import KanbanBoard from '@/components/KanbanBoard.vue'
import QuickAddBar from '@/components/QuickAddBar.vue'
import ExtensionSurfaceFrame from '@/components/ExtensionSurfaceFrame.vue'
import DeleteTaskDialog from '@/components/DeleteTaskDialog.vue'
import AppFooter from '@/components/AppFooter.vue'
import ProjectSettingsModal from '@/components/ProjectSettingsModal.vue'
import TasksToolbar from '@/components/tasks/TasksToolbar.vue'
import BoardSprintBar from '@/components/tasks/BoardSprintBar.vue'
import TaskBulkBar from '@/components/tasks/TaskBulkBar.vue'
import SaveViewModal from '@/components/tasks/SaveViewModal.vue'
import AddProjectModal from '@/components/tasks/AddProjectModal.vue'
import TaskTemplatesMenu from '@/components/tasks/TaskTemplatesMenu.vue'
import { useAuth } from '@/composables/useAuth'
import { useBoardSprints } from '@/composables/useBoardSprints'
import { useInfiniteScroll } from '@/composables/useInfiniteScroll'
import { useKanbanSurfaces } from '@/composables/useKanbanSurfaces'
import { useTaskListFilters } from '@/composables/useTaskListFilters'
import { useTaskListState } from '@/composables/useTaskListState'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useTaskSortable } from '@/composables/useTaskSortable'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useViewDensity } from '@/composables/useViewDensity'
import { useSidebarState } from '@/composables/useSidebarState'
import { useKeyboardShortcuts } from '@/composables/useKeyboardShortcuts'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'
import { isArchivedProject, isProjectOwner } from '@/utils/projectLabel'
import { hasAnyProjectWrite, hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'
import { kanbanSprintQueryValue, kanbanWorkflowClaimScope } from '@/utils/kanbanTaskQuery'
import { uniqueTagsByName, isArchivedTask } from '@/utils/tags'
import { filterHas, parseFilterList } from '@/utils/filterValues'

const route = useRoute()
const router = useRouter()
const { openAdd, openEdit, openView, lastSavedTask, lastDeletedTask } = useTaskSidebar()
const { density } = useViewDensity()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()
const { focusedTaskId, registerTaskShortcuts, unregisterTaskShortcuts } = useKeyboardShortcuts()
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

const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const savedViews = ref<SavedView[]>([])
const organizations = ref<Organization[]>([])
const activeViewId = ref<string | null>(null)
const deleteDialogOpen = ref(false)
const deleteDialogTask = ref<Task | null>(null)
/** Parent ids with expanded subtasks. Collapsed by default. */
const expandedParentIds = ref<Set<number>>(new Set())

const loadedPage = ref(0)
const totalPages = ref(1)
const search = ref('')
const loading = ref(true)
const loadingMore = ref(false)
/** Project filter key last applied to `tasks` ('' = all projects). */
const loadedProjectKey = ref<string | null>(null)
/** Monotonic counter so out-of-order reloadInitial responses are ignored. */
let reloadGeneration = 0

const showSaveViewModal = ref(false)
const showAddProjectModal = ref(false)
const showEditProjectModal = ref(false)
const editingProject = ref<Project | null>(null)

const undoToken = ref<string | null>(null)
const kanbanColumnsRev = ref(0)
const selectMode = ref(false)
const taskListEl = ref<HTMLElement | null>(null)
const loadMoreSentinel = ref<HTMLElement | null>(null)

const activeProjectObj = computed(() => {
  if (!filters.project || filters.project === '0') return null
  const pid = parseInt(filters.project, 10)
  if (Number.isNaN(pid)) return null
  return projects.value.find((p) => p.id === pid) ?? null
})

const isReadOnlyProjectView = computed(() => !!activeProjectObj.value && !hasAnyProjectWrite(activeProjectObj.value))
const isArchivedProjectView = computed(() => !!activeProjectObj.value?.archived)
const canAddTasks = computed(
  () => hasProjectPerm(activeProjectObj.value, PROJECT_PERMS.TASKS_CREATE) && !isArchivedProjectView.value,
)

const tagFilterByName = computed(() => !filters.project)
const displayTags = computed(() => (tagFilterByName.value ? uniqueTagsByName(tags.value) : tags.value))

const VIEW_MODE_KEY = 'gotodo.viewMode'

function readStoredViewMode(): string {
  try {
    return localStorage.getItem(VIEW_MODE_KEY) === 'board' ? 'board' : 'list'
  } catch {
    return 'list'
  }
}

/** 'list', 'board', or an extension tab key (`ext:<extension>:<surface>`). */
const viewMode = ref<string>(readStoredViewMode())

const isKanbanProjectView = computed(() => {
  const p = activeProjectObj.value
  if (!p) return false
  if (p.workflow_mode === 'kanban') return true
  // Fallback: any loaded task for this project reports kanban workflow
  return tasks.value.some((t) => t.project_id === p.id && t.project_workflow === 'kanban')
})

const board = useBoardSprints(activeProjectObj, isKanbanProjectView)
const surfaces = useKanbanSurfaces(activeProjectObj, isKanbanProjectView, viewMode)

const showBoardView = computed(() => isKanbanProjectView.value && viewMode.value === 'board')
const showExtensionSurface = computed(() => isKanbanProjectView.value && viewMode.value.startsWith('ext:'))

function taskMatchesStatusFilter(task: Task) {
  // Board keeps done-column cards even when the list default is incomplete.
  if (showBoardView.value) return filters.status === 'complete' ? task.completed : true
  if (filters.status === 'complete') return task.completed
  if (filters.status === 'incomplete') return !task.completed
  return true
}

function taskMatchesProjectScope(task: Task): boolean {
  if (!board.taskInSprint(task)) return false
  if (filters.project === '0' && task.project_id != null) return false
  if (filters.project && filters.project !== '0') {
    const pid = parseInt(filters.project, 10)
    if (!Number.isNaN(pid) && task.project_id !== pid) return false
  }
  return true
}

function taskMatchesCurrentFilters(task: Task): boolean {
  if (!taskMatchesStatusFilter(task)) return false
  if (!taskMatchesProjectScope(task)) return false
  if (filters.priority && !filterHas(filters.priority, String(task.priority ?? 0))) return false
  if (filters.due === 'range' && (filters.due_from || filters.due_to)) {
    const due = task.due_date || ''
    if (!due || (filters.due_from && due < filters.due_from) || (filters.due_to && due > filters.due_to)) return false
  }
  if (filters.claimed && isKanbanProjectView.value) {
    const claimedBy = task.claimed_by ?? null
    if (filters.claimed === 'me' && claimedBy !== user.value?.id) return false
    if (filters.claimed === 'none' && claimedBy !== null) return false
  }
  if (filters.tag) {
    const wanted = parseFilterList(filters.tag).map((v) => v.toLowerCase())
    if (!task.tags?.some((t) => wanted.includes(String(t.id)) || wanted.includes(t.name.toLowerCase()))) {
      return false
    }
  }
  if (filters.search) {
    const q = filters.search.toLowerCase()
    if (!`${task.title} ${task.description}`.toLowerCase().includes(q)) return false
  }
  return true
}

const list = useTaskListState({
  inProjectScope: taskMatchesProjectScope,
  matchesFilters: taskMatchesCurrentFilters,
})
const { tasks, total, completedCount, incompleteCount, selected, findTaskInTree, applyTaskUpdate } = list

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
  () => tasks.value.length > 0 || total.value > 0 || hasActiveFilters.value || isKanbanProjectView.value,
)
const hasMore = computed(() => loadedPage.value < totalPages.value)
const sortableEnabled = computed(() => filters.sort !== 'priority' && !loading.value)

// The claim filter belongs to kanban boards; drop it when leaving one so it
// doesn't linger as an invisible active filter on Home or classic projects.
watch(isKanbanProjectView, (now, before) => {
  if (before && !now && filters.claimed) setFilter('claimed', '')
})

const extensionSprintId = computed(() => board.currentSprintId() || 0)
const extensionBridgeContext = computed(() => ({
  project_id: activeProjectObj.value?.id || 0,
  sprint_id: extensionSprintId.value,
  role: activeProjectObj.value?.role || '',
  user_id: user.value?.id || 0,
  user_name: user.value?.user_name || '',
  can_write: hasAnyProjectWrite(activeProjectObj.value),
}))

function setViewMode(mode: string) {
  if (viewMode.value === mode) return
  viewMode.value = mode
  if (mode === 'list' || mode === 'board') {
    try {
      localStorage.setItem(VIEW_MODE_KEY, mode)
    } catch {
      /* ignore */
    }
  }
  if (!mode.startsWith('ext:')) void reloadInitial()
}

/** Prefer board when entering a kanban project so unclaimed backlog is visible. */
function ensureKanbanBoardDefault() {
  if (!isKanbanProjectView.value || viewMode.value === 'board') return
  viewMode.value = 'board'
  try {
    localStorage.setItem(VIEW_MODE_KEY, 'board')
  } catch {
    /* ignore */
  }
}

function onBoardSprintChange() {
  board.persist()
  void reloadInitial()
}

function openAddFromView() {
  openAdd(undefined, filters.project, null, board.currentSprintId())
}

/** Board has no infinite-scroll sentinel — fetch remaining pages after the first.
 *  Kanban list view also loads the full sprint so select-all covers unclaimed work. */
async function loadAllForBoard() {
  while (isKanbanProjectView.value && hasMore.value && !loading.value) {
    const before = loadedPage.value
    await loadMore()
    if (loadedPage.value === before) break
  }
}

function listApiParams(page: number, perPage: number) {
  // Home/classic lists stay on claimed work. A kanban project view (board or
  // list) includes unclaimed tasks so mass-edit can cover the whole sprint.
  const inKanbanProject = isKanbanProjectView.value
  const params: Record<string, string | number> = {
    ...toApiParams(page, perPage),
    workflow_claim_scope: kanbanWorkflowClaimScope(inKanbanProject),
  }
  // List defaults to incomplete; board still needs done-column cards.
  if (inKanbanProject && viewMode.value === 'board' && params.status === 'incomplete') delete params.status
  // Claims only exist on kanban tasks; elsewhere the filter would hide personal work.
  if (!inKanbanProject) delete params.claimed
  const sprintId = kanbanSprintQueryValue(inKanbanProject, board.sprintKey.value)
  if (sprintId !== undefined) params.sprint_id = sprintId
  return params
}

function projectForTask(task: Task): Project | null | undefined {
  if (!task.project_id) return null
  return projects.value.find((pr) => pr.id === task.project_id) ?? activeProjectObj.value
}

function canWriteTask(task: Task): boolean {
  return hasAnyProjectWrite(projectForTask(task))
}

function canMoveTaskProject(task: Task): boolean {
  if (!task.project_id) return true
  const p = projects.value.find((pr) => pr.id === task.project_id)
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
  if ((found && !canWriteTask(found.task)) || (!found && isReadOnlyProjectView.value)) {
    openView(id)
    return
  }
  openEdit(id)
}

async function onBoardTaskCreated(task: Task) {
  list.registerTaskAdded(task)
  if (!taskMatchesCurrentFilters(task)) {
    toast.push(`Added "${task.title}" (hidden by current filters)`, 'info')
  }
  await nextTick()
  refreshSortable()
}

async function onTemplateTaskCreated(task: Task) {
  await onBoardTaskCreated(task)
  openTaskDetails(task.id)
}

async function saveReorder(orderedIds: number[], parentId?: number | null) {
  list.reorderLocalTasks(orderedIds, parentId)
  try {
    await api.reorderTasks(
      parentId ? { task_ids: orderedIds, parent_id: parentId } : { task_ids: orderedIds, project: filters.project || undefined },
    )
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save task order', 'error')
    await reloadInitial()
  }
}

const { refresh: refreshSortable } = useTaskSortable(taskListEl, sortableEnabled, saveReorder)

async function loadMeta() {
  try {
    const [projs, views, orgs] = await Promise.all([
      api.listProjects(),
      api.listSavedViews(),
      api.listOrganizations().catch(() => [] as Organization[]),
    ])
    projects.value = projs
    savedViews.value = views
    organizations.value = orgs
    await loadTags()
    await surfaces.load()
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
      // Across projects tags filter by name, inside one project by id; translate
      // each selected tag and drop any this scope doesn't have.
      const next = parseFilterList(filters.tag).flatMap((v) => {
        const tag =
          tags.value.find((t) => String(t.id) === v) ?? tags.value.find((t) => t.name.toLowerCase() === v.toLowerCase())
        if (!tag) return []
        return [tagFilterByName.value ? tag.name : String(tag.id)]
      })
      const joined = parseFilterList(next.join(',')).join(',')
      if (joined !== filters.tag) setFilter('tag', joined)
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
    await board.ensureLoaded()
    if (generation !== reloadGeneration) return
    const perPage = user.value?.items_per_page || 50
    const page = await api.listTasks(listApiParams(1, perPage))
    if (generation !== reloadGeneration) return
    tasks.value = page.tasks
    loadedPage.value = 1
    total.value = page.total
    totalPages.value = page.total_pages
    completedCount.value = page.completed_count
    incompleteCount.value = page.incomplete_count
    search.value = filters.search
    loadedProjectKey.value = projectKey
  } catch (err) {
    if (generation !== reloadGeneration) return
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    if (generation === reloadGeneration) {
      loading.value = false
      await nextTick()
      refreshSortable()
      if (isKanbanProjectView.value) void loadAllForBoard()
    }
  }
}

async function loadMore() {
  if (loadingMore.value || loading.value || !hasMore.value) return
  loadingMore.value = true
  try {
    const perPage = user.value?.items_per_page || 50
    const nextPage = loadedPage.value + 1
    const page = await api.listTasks(listApiParams(nextPage, perPage))
    const existingIds = new Set(tasks.value.map((t) => t.id))
    tasks.value = [...tasks.value, ...page.tasks.filter((t) => !existingIds.has(t.id))]
    loadedPage.value = nextPage
    total.value = page.total
    totalPages.value = page.total_pages
    completedCount.value = page.completed_count
    incompleteCount.value = page.incomplete_count
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
    void surfaces.load()
  },
)

watch(surfaces.tabs, (tabs) => {
  if (viewMode.value.startsWith('ext:') && !tabs.some((t) => t.viewKey === viewMode.value)) setViewMode('board')
})

function isArchivedTagFilter(): boolean {
  return parseFilterList(filters.tag).some((v) => {
    const name = (tags.value.find((t) => String(t.id) === v)?.name ?? v).toLowerCase()
    return name === 'archived' || name === 'removed'
  })
}

watch(lastSavedTask, async (task) => {
  if (!task) return
  lastSavedTask.value = null
  if (isArchivedTask(task) !== isArchivedTagFilter()) {
    await reloadInitial()
    return
  }
  if (task.parent_id) expandParent(task.parent_id)
  if (findTaskInTree(task.id)) applyTaskUpdate(task)
  else list.registerTaskAdded(task)
  await nextTick()
  refreshSortable()
})

watch(lastDeletedTask, async (payload) => {
  if (!payload) return
  lastDeletedTask.value = null
  const found = findTaskInTree(payload.id)
  if (payload.mode === 'reparent') {
    if (found) list.removeTaskLocally({ ...found.task, children: [], child_count: 0 })
    await reloadInitial()
  } else if (found) {
    list.removeTaskLocally(found.task)
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
    if (next.task) void consumeTaskQuery()
  },
)

async function toggleComplete(task: Task) {
  if (!hasProjectPerm(projectForTask(task), PROJECT_PERMS.TASKS_COMPLETE)) return
  try {
    applyTaskUpdate(await api.patchTask(task.id, { completed: !task.completed }))
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function handleInlineTaskPatch(payload: { id: number; title?: string; description?: string }) {
  const found = findTaskInTree(payload.id)
  if (found && !hasProjectPerm(projectForTask(found.task), PROJECT_PERMS.TASKS_EDIT)) return
  try {
    applyTaskUpdate(await api.patchTask(payload.id, { title: payload.title, description: payload.description }))
    toast.push('Task updated', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeTask(task: Task) {
  if (!hasProjectPerm(projectForTask(task), PROJECT_PERMS.TASKS_DELETE)) return
  if ((task.child_count ?? task.children?.length ?? 0) > 0) {
    deleteDialogTask.value = task
    deleteDialogOpen.value = true
    return
  }
  const ok = await askConfirm({
    title: 'Delete task?',
    message: `Move “${task.title}” to the trash? You can restore it from Trash.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (ok) await performDelete(task, { mode: 'cascade' })
}

async function performDelete(task: Task, opts: { mode: 'cascade' | 'reparent'; new_parent_id?: number | null }) {
  try {
    const res = await api.deleteTask(task.id, opts)
    undoToken.value = res.undo_token || null
    if (opts.mode === 'reparent') {
      // Children moved; drop parent only and refresh to show new homes.
      list.removeTaskLocally({ ...task, children: [], child_count: 0 })
      await reloadInitial()
    } else {
      list.removeTaskLocally(task)
    }
    toast.push(undoToken.value ? 'Moved to trash — undo available' : 'Moved to trash', 'info')
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
  expandedParentIds.value = new Set([...expandedParentIds.value, taskId])
}

function openAddSubtask(task: Task) {
  if (!hasProjectPerm(projectForTask(task), PROJECT_PERMS.TASKS_CREATE)) return
  const p = task.project_id ? projects.value.find((pr) => pr.id === task.project_id) : null
  if (p && isArchivedProject(p)) {
    toast.push('Cannot add tasks to an archived project', 'error')
    return
  }
  expandParent(task.id)
  openAdd(undefined, task.project_id ?? null, { id: task.id, title: task.title })
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

async function bulk(action: string, extra: Record<string, unknown> = {}) {
  if (!selected.value.length || isReadOnlyProjectView.value) return
  if (action === 'move_project' && !canBulkMoveProject.value) return
  const count = selected.value.length
  if (action === 'delete') {
    const nestedCount = selected.value.reduce((n, id) => {
      const t = findTaskInTree(id)?.task
      return n + (t?.child_count ?? t?.children?.length ?? 0)
    }, 0)
    const base = `Delete ${count} selected task${count === 1 ? '' : 's'}?`
    const ok = await askConfirm({
      title: 'Delete tasks?',
      message:
        nestedCount > 0
          ? `${base} This also deletes ${nestedCount} nested subtask${nestedCount === 1 ? '' : 's'}.`
          : base,
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
        if (task) list.removeTaskLocally(task)
      }
      await nextTick()
      refreshSortable()
    } else if (action === 'complete' || action === 'incomplete') {
      for (const id of affectedIds) {
        const task = tasks.value.find((t) => t.id === id)
        if (task) applyTaskUpdate({ ...task, completed: action === 'complete' })
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

// Choosing a preset other than "Custom range" drops any range dates.
function onDuePreset(value: string) {
  activeViewId.value = null
  if (value !== 'range') {
    setFilter('due_from', '')
    setFilter('due_to', '')
  }
  setFilter('due', value)
  void reloadInitial()
}

function onDueRange(from: string, to: string) {
  activeViewId.value = null
  setFilter('due_from', from)
  setFilter('due_to', to)
  void reloadInitial()
}

function clearFilters() {
  activeViewId.value = null
  search.value = ''
  // Keep the current project: Clear is for list filters, not leaving the project.
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
  if (!view) return
  activeViewId.value = String(view.id)
  applySavedViewFilters(view.filter || {})
  search.value = filters.search
  void reloadInitial()
}

const currentViewFilter = computed<SavedViewFilter>(() => ({
  status: filters.status || undefined,
  due: filters.due || undefined,
  due_from: (filters.due === 'range' && filters.due_from) || undefined,
  due_to: (filters.due === 'range' && filters.due_to) || undefined,
  project: filters.project || undefined,
  priority: filters.priority || undefined,
  tag: filters.tag || undefined,
  sort: filters.sort || undefined,
  search: filters.search || undefined,
  claimed: (isKanbanProjectView.value && filters.claimed) || undefined,
}))

const activeView = computed(() => savedViews.value.find((v) => String(v.id) === activeViewId.value) ?? null)

/** The open saved view filters on a project that was deleted or that the user left. */
const viewProjectMissing = computed(() => {
  const raw = activeView.value?.filter?.project
  const pid = raw ? parseInt(raw, 10) : NaN
  return Number.isFinite(pid) && pid > 0 && !projects.value.some((p) => p.id === pid)
})

async function dropMissingProjectFromView() {
  const view = activeView.value
  if (!view) return
  const filter = { ...(view.filter || {}) }
  delete filter.project
  try {
    await api.updateSavedView(view.id, { filter })
    await loadMeta()
    selectSavedViewFilter(String(view.id))
    toast.push(`“${view.name}” now covers all your projects`, 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update the view', 'error')
  }
}

async function deleteActiveView() {
  const view = activeView.value
  if (!view) return
  const ok = await askConfirm({
    title: 'Delete view?',
    message: `Delete the saved view “${view.name}”?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteSavedView(view.id)
    activeViewId.value = null
    setFilter('project', '')
    await loadMeta()
    void reloadInitial()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete the view', 'error')
  }
}

async function onViewSaved(view: SavedView) {
  activeViewId.value = String(view.id)
  showSaveViewModal.value = false
  await loadMeta()
}

async function onProjectCreated() {
  showAddProjectModal.value = false
  await loadMeta()
}

function openEditProject(proj: Project) {
  editingProject.value = proj
  showEditProjectModal.value = true
}

function closeEditProject() {
  showEditProjectModal.value = false
  editingProject.value = null
}

function refreshEditingProject() {
  if (!editingProject.value) return
  const updated = projects.value.find((p) => p.id === editingProject.value!.id)
  if (updated) editingProject.value = updated
}

async function onProjectSettingsSaved() {
  await loadMeta()
  await reloadInitial()
  // Keep modal open but refresh project reference from list
  refreshEditingProject()
}

async function onProjectSettingsChanged() {
  board.invalidate()
  await loadMeta()
  await reloadInitial()
  kanbanColumnsRev.value++
  refreshEditingProject()
}

async function onReorderProjects(projectIds: number[]) {
  const previous = [...projects.value]
  const ownedActive = projects.value.filter((p) => (!p.role || p.role === 'owner') && !isArchivedProject(p))
  const rest = projects.value.filter((p) => !ownedActive.some((o) => o.id === p.id))
  const byId = new Map(ownedActive.map((p) => [p.id, p]))
  projects.value = [...projectIds.map((id) => byId.get(id)!).filter(Boolean), ...rest]
  try {
    await api.reorderProjects(projectIds)
  } catch (err) {
    projects.value = previous
    toast.push(err instanceof APIError ? err.message : 'Could not reorder projects', 'error')
  }
}

function selectHome() {
  activeViewId.value = null
  if (filters.project) setFilterAndReload('project', '')
}

useInfiniteScroll(loadMoreSentinel, loadMore, hasMore)

function getFocusableTaskIds(): number[] {
  const ids: number[] = []
  for (const task of tasks.value) {
    ids.push(task.id)
    if (task.children?.length && isParentExpanded(task.id)) {
      for (const child of task.children) ids.push(child.id)
    }
  }
  return ids
}

function focusSearchInput() {
  const el = document.getElementById('task-search') as HTMLInputElement | null
  if (!el) return
  el.focus()
  el.select()
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
      if (canAddTasks.value) openAddFromView()
    },
    focusSearch: focusSearchInput,
    getFocusableTaskIds,
    editTask: openTaskDetails,
    deleteTask: (id) => {
      const found = findTaskInTree(id)
      if (found) void removeTask(found.task)
    },
    toggleComplete: (id) => {
      const found = findTaskInTree(id)
      if (found) void toggleComplete(found.task)
    },
  })
  showShortcutsHintOnce()
  await loadMeta()
  syncFiltersFromRoute()
  await reloadInitial()
  await consumeTaskQuery()
})

useLiveUpdates((event, batch) => {
  // Comments alone don't change this view; skip only when nothing else arrived.
  if (batch.every((e) => e.type === 'task.commented')) return
  if (event.type === 'extension.store') return
  if (event.type === 'notification.created') return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  if (event.type === 'extensions.reloaded') {
    clearCustomFieldDefsCache()
    void surfaces.load()
    void reloadInitial()
    return
  }
  if (event.type === 'project.updated' || event.type === 'project.created' || event.type === 'project.deleted') {
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

    <div class="ordryn-main-pane flex-grow-1 p-2 p-sm-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div>
        <TasksToolbar
          :total="total"
          :completed-count="completedCount"
          :incomplete-count="incompleteCount"
          :project="activeProjectObj"
          :show-select-toggle="!isReadOnlyProjectView && !isArchivedProjectView && showTaskTable"
          :selecting="isSelecting"
          :can-add-tasks="canAddTasks"
          :can-undo="!!undoToken"
          :export-query="toExportQuery()"
          @toggle-select="toggleSelectMode"
          @add="openAddFromView"
          @undo="undoDelete"
        >
          <template #actions>
            <TaskTemplatesMenu
              v-if="canAddTasks"
              :project-id="activeProjectObj?.id ?? null"
              :sprint-id="board.currentSprintId() ?? null"
              @created="onTemplateTaskCreated"
            />
          </template>
        </TasksToolbar>

        <ModernTaskFilterBar
          :status="filters.status"
          :tag="filters.tag"
          :priority="filters.priority"
          :claimed="filters.claimed"
          :show-claim-filter="isKanbanProjectView"
          :due-date-preset="filters.due"
          :due-from="filters.due_from"
          :due-to="filters.due_to"
          :sort="filters.sort"
          :search="search"
          :density="density"
          :tags="displayTags"
          :tag-by-name="tagFilterByName"
          :show-view-mode="isKanbanProjectView"
          :view-mode="viewMode"
          :surface-tabs="surfaces.tabs.value"
          :hide-task-filters="showExtensionSurface"
          @update:status="setFilterAndReload('status', $event)"
          @update:tag="setFilterAndReload('tag', $event)"
          @update:priority="setFilterAndReload('priority', $event)"
          @update:claimed="setFilterAndReload('claimed', $event)"
          @update:due-date-preset="onDuePreset"
          @update:due-range="onDueRange"
          @update:sort="setFilterAndReload('sort', $event)"
          @update:search="search = $event; setFilter('search', $event); reloadInitial()"
          @update:density="density = $event"
          @update:view-mode="setViewMode($event)"
          @clear-filters="clearFilters"
        />

        <div
          v-if="viewProjectMissing && activeView"
          class="alert alert-warning d-flex flex-wrap align-items-center gap-2 py-2 px-3 mb-2"
          role="status"
        >
          <i class="bi bi-exclamation-triangle" aria-hidden="true" />
          <span class="small flex-grow-1">
            The view <strong>{{ activeView.name }}</strong> is limited to a project you can no longer see. It was
            deleted, or you're no longer a member.
          </span>
          <button type="button" class="btn btn-sm btn-outline-dark" @click="dropMissingProjectFromView">
            Use all projects instead
          </button>
          <button type="button" class="btn btn-sm btn-outline-danger" @click="deleteActiveView">Delete view</button>
        </div>

        <BoardSprintBar
          v-if="isKanbanProjectView"
          v-model="board.sprintKey.value"
          :project="activeProjectObj"
          :sprints="board.sprints.value"
          :selected-sprint="board.selectedSprint.value"
          :show-select-all="!isReadOnlyProjectView && showBoardView"
          :all-selected="allSelected"
          :select-all-disabled="!flatSelectableIds.length"
          @change="onBoardSprintChange"
          @toggle-select-all="toggleSelectAll"
        />

        <TaskBulkBar
          v-if="selected.length && !isReadOnlyProjectView"
          :count="selected.length"
          :project="activeProjectObj"
          :projects="projects"
          :tags="displayTags"
          :sprints="board.sprints.value"
          :statuses="board.statuses.value"
          :is-kanban="isKanbanProjectView"
          :can-move-project="canBulkMoveProject"
          @bulk="bulk"
          @deselect="selected = []"
        />

        <div v-if="showBoardView" class="mb-3">
          <div v-if="loading && !tasks.length" class="text-center py-4 text-muted">
            <div class="spinner-border spinner-border-sm me-2" role="status" />Loading tasks…
          </div>
          <KanbanBoard
            v-else-if="activeProjectObj"
            :key="activeProjectObj.id"
            :project-id="activeProjectObj.id"
            :tasks="tasks"
            :project="activeProjectObj"
            :role="activeProjectObj.role"
            :density="density"
            :columns-rev="kanbanColumnsRev"
            :sprint-filter="board.sprintKey.value"
            :selected-ids="selected"
            :selecting="isSelecting"
            @open-task="openTaskDetails"
            @changed="reloadInitial"
            @task-updated="applyTaskUpdate"
            @board-reorder="list.applyBoardReorder"
            @toggle-select="toggleSelect"
            @task-created="onBoardTaskCreated"
          />
          <div v-if="loadingMore" class="text-center py-2 text-muted small">
            <span class="spinner-border spinner-border-sm me-2" />Loading more tasks…
          </div>
        </div>

        <div v-if="showExtensionSurface && surfaces.active.value && activeProjectObj" class="mb-3">
          <ExtensionSurfaceFrame
            :key="`${surfaces.active.value.extensionId}:${surfaces.active.value.surfaceId}`"
            :project-id="activeProjectObj.id"
            :extension-id="surfaces.active.value.extensionId"
            :surface-id="surfaces.active.value.surfaceId"
            :sprint-id="extensionSprintId"
            :context="extensionBridgeContext"
          />
        </div>

        <div v-if="!showBoardView && !showExtensionSurface" id="task-container" aria-live="polite">
          <QuickAddBar
            v-if="canAddTasks"
            :projects="projects"
            :default-project-id="activeProjectObj?.id ?? null"
            :sprint-id="board.currentSprintId() ?? null"
            @created="onBoardTaskCreated"
          />
          <div v-if="loading && !tasks.length" class="text-center py-5 text-muted">
            <div class="spinner-border spinner-border-sm me-2" role="status" />Loading tasks…
          </div>

          <div v-else-if="showTaskTable">
            <div class="d-flex align-items-center justify-content-between mb-2 px-1">
              <div class="d-flex align-items-center gap-3">
                <div
                  v-if="!isReadOnlyProjectView"
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

            <div id="task-list" ref="taskListEl">
              <div v-for="task in tasks" :key="task.id" class="task-tree-root" :data-task-id="task.id">
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
                    @toggle-complete="toggleComplete(child)"
                    @patch-task="handleInlineTaskPatch"
                    @edit="openTaskDetails(child.id)"
                  />
                </div>
              </div>

              <div
                v-if="!tasks.length && (hasActiveFilters || isKanbanProjectView)"
                class="text-center py-5 rounded-3 border"
                style="background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
              >
                <template v-if="isKanbanProjectView">
                  <p class="text-muted mb-2">No tasks in this sprint match your current filters.</p>
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

              <div v-if="hasMore" ref="loadMoreSentinel" class="text-center py-3 text-muted small">
                <span v-if="loadingMore" class="spinner-border spinner-border-sm me-2" />
                {{ loadingMore ? 'Loading more tasks…' : 'Scroll for more tasks' }}
              </div>
            </div>
          </div>

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

      <SaveViewModal
        :open="showSaveViewModal"
        :filter="currentViewFilter"
        @close="showSaveViewModal = false"
        @saved="onViewSaved"
      />

      <AddProjectModal
        :open="showAddProjectModal"
        :organizations="organizations"
        @close="showAddProjectModal = false"
        @created="onProjectCreated"
      />

      <ProjectSettingsModal
        :open="showEditProjectModal"
        :project="editingProject"
        @close="closeEditProject"
        @saved="onProjectSettingsSaved"
        @changed="onProjectSettingsChanged"
        @columns-changed="kanbanColumnsRev++"
      />

      <DeleteTaskDialog
        :open="deleteDialogOpen"
        :task="deleteDialogTask"
        :root-tasks="tasks"
        @cancel="deleteDialogOpen = false; deleteDialogTask = null"
        @cascade="deleteDialogTask && performDelete(deleteDialogTask, { mode: 'cascade' })"
        @reparent="deleteDialogTask && performDelete(deleteDialogTask, { mode: 'reparent', new_parent_id: $event })"
      />

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
