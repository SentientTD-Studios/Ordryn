<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useTaskTextLimits } from '@/utils/taskText'
import type { Project, ProjectSprint, ProjectStatus, RefinementState, Tag, Task, TaskRecurrenceDetail } from '@/api/types'
import { APIError } from '@/api/types'
import ParentTaskCombobox from '@/components/ParentTaskCombobox.vue'
import UserSearchCombobox from '@/components/UserSearchCombobox.vue'
import DeleteTaskDialog from '@/components/DeleteTaskDialog.vue'
import TaskDiscussion from '@/components/TaskDiscussion.vue'
import TaskSidebarFields from '@/components/TaskSidebarFields.vue'
import TaskRecurrenceEditor from '@/components/TaskRecurrenceEditor.vue'
import TaskWatchButton from '@/components/TaskWatchButton.vue'
import TaskLinksPanel from '@/components/TaskLinksPanel.vue'
import TaskAgentsPanel from '@/components/TaskAgentsPanel.vue'
import TaskTimePanel from '@/components/task/TaskTimePanel.vue'
import TaskGitHubPanel from '@/components/task/TaskGitHubPanel.vue'
import TaskActivityLog from '@/components/task/TaskActivityLog.vue'
import SaveTemplateModal from '@/components/task/SaveTemplateModal.vue'
import TaskAttachmentsPanel from '@/components/task/TaskAttachmentsPanel.vue'
import { draftFromRecurrence, emptyRecurrenceDraft, recurrencePayload, sameRecurrenceDraft, type RecurrenceDraft } from '@/utils/recurrence'
import WysiwygEditor from '@/components/WysiwygEditor.vue'
import RichBody from '@/components/RichBody.vue'
import { useImageUpload } from '@/composables/useImageUpload'
import { insertMarkdownAtCursor } from '@/utils/taskCommentBody'
import { dropHasFiles, imageFileFromClipboard, imageFileFromDrop, shouldIgnoreTaskOverlayClose } from '@/utils/imageUpload'
import { useAuth } from '@/composables/useAuth'
import { useStaleConflict, staleTaskOf, versionGuard } from '@/composables/useStaleConflict'
import { useSite } from '@/composables/useSite'
import { allowTaskRouteNavigation, useTaskSidebar } from '@/composables/useTaskSidebar'
import { taskPageRoute } from '@/utils/projectRoute'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { canMoveTaskStatus, hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'
import { isArchivedProject, isProjectOwner, projectOptionLabel } from '@/utils/projectLabel'
import { sprintLockedForUser, sprintOptionLabel } from '@/utils/sprintLabel'
import { useLiveUpdates, isOwnTabLiveEvent, LIVE_RESYNC, type LiveEvent } from '@/composables/useLiveUpdates'
import { batchTouchesDiscussion } from '@/utils/liveEvents'
import { assignableTags, archiveConfirmMessage, isArchivedTask, isProtectedTag } from '@/utils/tags'
import { ESTIMATE_STEP, MAX_ESTIMATE_HOURS, parseEstimateInput } from '@/utils/estimates'
import { useProjectCeremonies } from '@/composables/useProjectCeremonies'
import { formatDuration } from '@/utils/timeBlocks'

const {
  open,
  mode,
  taskId,
  defaultDueDate,
  defaultProjectId,
  defaultParentId,
  defaultParentTitle,
  defaultSprintId,
  close,
  requestClose,
  setCloseGuard,
  notifySaved,
  notifyDeleted,
  openEdit,
  openView,
} = useTaskSidebar()
useBodyScrollLock(open)
const toast = useToast()
const { askConfirm, askUnsaved, state: confirmState } = useConfirm()
const { user } = useAuth()
const { claimTask: claimWithConfirm, releaseTask } = useStaleConflict()
const { enabled: imageEnabled, uploadImageFile } = useImageUpload()
const router = useRouter()
const showTaskNumber = computed(() => (mode.value === 'edit' || mode.value === 'view') && !!taskId.value)

// The task's link: its project's board with the task open (/p/{slug}/tasks/{id}).
const taskPageHref = computed(() =>
  taskId.value ? router.resolve(taskPageRoute(taskId.value, savedTaskProject.value)).href : '',
)

/**
 * The #id link: go to the task's project board with the task still open (the
 * same modal, so unsaved edits stay). Modified clicks open a new tab as usual.
 */
function goToTaskInProject(e: MouseEvent) {
  if (!taskId.value || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  e.preventDefault()
  const target = router.resolve(taskPageRoute(taskId.value, savedTaskProject.value))
  if (target.fullPath === router.currentRoute.value.fullPath) return
  // Task URLs otherwise just open the modal in place; this one should navigate.
  allowTaskRouteNavigation(taskId.value)
  void router.push(target)
}

/** The task's project (as saved, not the unsaved form value), for its page URL. */
const savedTaskProject = computed(() => {
  const pid = currentTask.value?.project_id
  if (pid == null) return null
  return projects.value.find((p) => p.id === pid) ?? { id: pid }
})

async function copyPermalink() {
  if (!taskId.value) return
  const href = router.resolve(taskPageRoute(taskId.value, savedTaskProject.value)).href
  const url = new URL(href, window.location.origin).href
  try {
    await navigator.clipboard.writeText(url)
    toast.push('Copied', 'success')
  } catch {
    toast.push(url, 'info')
  }
}

const loading = ref(false)
const saving = ref(false)
const deleteDialogOpen = ref(false)
const projects = ref<Project[]>([])
const allTags = ref<Tag[]>([])
const rootTasks = ref<Task[]>([])
const currentTask = ref<Task | null>(null)
const descriptionError = ref('')
const statuses = ref<ProjectStatus[]>([])
const sprints = ref<ProjectSprint[]>([])
const taskWorkflow = ref('')

const titleInput = ref<HTMLInputElement | null>(null)
const descriptionEditor = ref<InstanceType<typeof WysiwygEditor> | null>(null)
const descriptionInput = computed(() => descriptionEditor.value?.textarea ?? null)
const title = ref('')
const description = ref('')
const projectId = ref<number | ''>('')
const parentId = ref<number | ''>('')
const parentTitle = ref('')
const priority = ref(0)
const dueDate = ref('')
const startDate = ref('')
const selectedTagIds = ref<number[]>([])
const taskTags = ref<Tag[]>([])
const newTags = ref('')
const completed = ref(false)
const statusId = ref<number | ''>('')
const sprintId = ref<number | ''>('')
const estimateHours = ref<number | ''>('')
const refinement = ref<RefinementState>('')
const claimedBy = ref<number | null>(null)
const claimedByName = ref('')
const claiming = ref(false)
const reviewerId = ref<number | null>(null)
const reviewerName = ref('')
const reviewerDraft = ref('')
const reviewerBusy = ref(false)
const activityLog = ref<InstanceType<typeof TaskActivityLog> | null>(null)
const saveTemplateOpen = ref(false)
const customFieldValues = ref<Record<string, unknown>>({})
const recurrenceDraft = ref<RecurrenceDraft>(emptyRecurrenceDraft())
const recurrenceDetail = ref<TaskRecurrenceDetail | null>(null)
const linksPanel = ref<InstanceType<typeof TaskLinksPanel> | null>(null)
const isSubtask = computed(() => parentId.value !== '' && Number(parentId.value) > 0)
const selectedProject = computed(() => {
  if (projectId.value === '') return null
  return projects.value.find((p) => p.id === Number(projectId.value)) ?? null
})
const assignableProjects = computed(() =>
  projects.value.filter((p) => !isArchivedProject(p) || p.id === Number(projectId.value)),
)
// Company mode: every task belongs to a project, so there's no "No project" choice.
const { isCompanyMode } = useSite()
const firstWritableProjectId = computed<number | ''>(
  () => projects.value.find((p) => !isArchivedProject(p) && hasProjectPerm(p, PROJECT_PERMS.TASKS_CREATE))?.id ?? '',
)
const canEditDetails = computed(() => {
  if (mode.value === 'view') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_EDIT)
})
const canChangeStatus = computed(() => {
  if (mode.value === 'view') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_STATUS)
})
const canChangeSprint = computed(() => {
  if (mode.value === 'view') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_SPRINT)
})
const canClaimTask = computed(() => {
  if (mode.value !== 'edit') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_CLAIM)
})
const canArchiveTask = computed(() => {
  if (mode.value !== 'edit') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_ARCHIVE)
})
const canRestoreTask = computed(() => {
  if (mode.value !== 'edit') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_RESTORE)
})
const canDeleteTask = computed(() => {
  if (mode.value !== 'edit') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_DELETE)
})
const canLogTime = computed(() => {
  if (mode.value === 'view') return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.TIME_WRITE)
})
const canCreateTask = computed(() => hasProjectPerm(selectedProject.value, PROJECT_PERMS.TASKS_CREATE))
const canSaveTask = computed(() => {
  if (mode.value === 'add') return canCreateTask.value && canEditDetails.value
  if (mode.value === 'edit') return canEditDetails.value || canChangeStatus.value || canChangeSprint.value
  return false
})
/** Members without tasks:edit may open via edit entry points; treat details as read-only. */
const readOnly = computed(() => !canEditDetails.value)
const canManageTags = computed(() => {
  if (readOnly.value) return false
  return hasProjectPerm(selectedProject.value, PROJECT_PERMS.PROJECT_TAGS)
})
const currentStatus = computed(() => {
  const id = Number(statusId.value || currentTask.value?.status_id || 0)
  return statuses.value.find((s) => s.id === id) ?? null
})
function statusOptionDisabled(s: ProjectStatus): boolean {
  if (!canChangeStatus.value) return true
  if (statusId.value === s.id || currentTask.value?.status_id === s.id) return false
  return !canMoveTaskStatus(selectedProject.value, currentStatus.value, s, user.value)
}
/** Editors on a shared board cannot move the task; lock from the loaded project, not the v-model. */
const projectLockedToOwner = computed(() => {
  if (mode.value !== 'edit') return false
  const pid = currentTask.value?.project_id
  if (!pid) return false
  const p = projects.value.find((pr) => pr.id === pid)
  return !!p && !isProjectOwner(p)
})
const pickerTags = computed(() => assignableTags(allTags.value))
const taskIsArchived = computed(() => isArchivedTask(currentTask.value))
const parentOptions = computed(() =>
  rootTasks.value.filter((r) => r.id !== taskId.value && taskInSelectedProject(r)),
)
const parentPickerDisabled = computed(
  () =>
    readOnly.value ||
    (mode.value === 'edit' && (currentTask.value?.child_count || currentTask.value?.children?.length || 0) > 0),
)
const relatedParent = computed(() => {
  if (!isSubtask.value) return null
  const id = Number(parentId.value)
  return { id, title: parentTitle.value || rootTasks.value.find((t) => t.id === id)?.title || `Task #${id}` }
})
const relatedChildren = computed(() => currentTask.value?.children ?? [])
/** The form as it stands, for "Save as template". */
const templateDraft = computed(() => {
  const byId = new Map([...taskTags.value, ...allTags.value].map((t) => [t.id, t.name]))
  return {
    title: title.value.trim(),
    description: description.value,
    priority: priority.value,
    tag_names: selectedTagIds.value.map((id) => byId.get(id)).filter((n): n is string => !!n),
    estimate_hours: estimateHours.value === '' ? null : Number(estimateHours.value),
    subtasks: relatedChildren.value.map((c) => c.title),
  }
})
const isKanbanTask = computed(() => {
  if (taskWorkflow.value === 'kanban') return true
  return (selectedProject.value?.workflow_mode || 'classic') === 'kanban'
})
const { refinementActive } = useProjectCeremonies(() =>
  isKanbanTask.value && projectId.value !== '' ? Number(projectId.value) : null,
)
/** Shown while refinement is on, or read-only when a flag lingers after it was switched off. */
const showRefinementField = computed(
  () => isKanbanTask.value && (refinementActive.value || (currentTask.value?.refinement ?? '') !== ''),
)
const estimateInvalid = computed(() => parseEstimateInput(estimateHours.value) === undefined)
const sidebarTitle = computed(() => {
  if (mode.value === 'view') return 'View Task'
  if (mode.value === 'edit' && !canEditDetails.value) return 'Task'
  if (mode.value === 'edit') return 'Edit Task'
  return 'Add Task'
})
const kanbanHeaderTitle = computed(() => (mode.value === 'add' ? 'Add Task' : 'Task'))
const submitText = computed(() => (mode.value === 'edit' ? 'Save Task' : 'Add Task'))
const { maxDescription: MAX_DESCRIPTION } = useTaskTextLimits()
const charCount = computed(() => description.value.length)
const editingDescription = ref(false)
const showDescriptionEditor = computed(() => {
  if (readOnly.value) return false
  if (editingDescription.value) return true
  if (mode.value === 'add' && !description.value.trim()) return true
  return false
})
const showDiscussion = computed(
  () => (mode.value === 'edit' || mode.value === 'view') && !!currentTask.value?.project_id,
)
const discussionCanModerate = computed(() =>
  hasProjectPerm(selectedProject.value, PROJECT_PERMS.COMMENTS_MODERATE),
)
type TaskDiscussionExpose = {
  reload: () => Promise<void>
  isDirty: () => boolean
  flushUnsaved: () => Promise<boolean>
}
const discussionRef = ref<TaskDiscussionExpose | null>(null)
const claimerLabel = computed(() => {
  if (!claimedBy.value) return 'Unclaimed'
  if (user.value?.id && claimedBy.value === user.value.id) return 'You'
  return claimedByName.value || `User #${claimedBy.value}`
})
// Reviewer applies to saved project tasks (classic or kanban).
const reviewerProjectId = computed(() => currentTask.value?.project_id ?? 0)
const showReviewer = computed(
  () => (mode.value === 'edit' || mode.value === 'view') && !!taskId.value && reviewerProjectId.value > 0,
)
const isMyReview = computed(() => !!user.value?.id && reviewerId.value === user.value.id)
const reviewerLabel = computed(() => {
  if (!reviewerId.value) return 'No reviewer'
  if (isMyReview.value) return 'You'
  return reviewerName.value || `User #${reviewerId.value}`
})

// Kanban header: secondary actions live in the "more" menu.
const canSaveAsTemplate = computed(() => showTaskNumber.value && !isSubtask.value && !loading.value)
const canToggleArchive = computed(
  () =>
    !loading.value &&
    mode.value === 'edit' &&
    (taskIsArchived.value ? canRestoreTask.value : canArchiveTask.value),
)
const showHeaderMenu = computed(
  () =>
    showTaskNumber.value ||
    canSaveAsTemplate.value ||
    canToggleArchive.value ||
    (canDeleteTask.value && !loading.value),
)

// Kanban tool tabs. Each panel reports whether it renders anything and a count.
type ToolTabKey = 'discussion' | 'related' | 'agents' | 'github' | 'files' | 'time' | 'activity'
type PanelSummary = { visible: boolean; count: number }
const panelSummary = reactive<Partial<Record<'discussion' | 'links' | 'agents' | 'github' | 'files' | 'time', PanelSummary>>>({})
const preferredToolTab = ref<ToolTabKey>('discussion')
const activityTabOpened = ref(false)

const toolTabs = computed(() => {
  if (!isKanbanTask.value || !taskId.value || (mode.value !== 'edit' && mode.value !== 'view')) return []
  const s = panelSummary
  const tabs: { key: ToolTabKey; label: string; icon: string; badge: string }[] = []
  const badge = (n: number | undefined) => (n ? String(n) : '')
  if (showDiscussion.value) {
    tabs.push({ key: 'discussion', label: 'Discussion', icon: 'bi-chat-left-text', badge: badge(s.discussion?.count) })
  }
  const relatedCount = (relatedParent.value ? 1 : 0) + relatedChildren.value.length
  if (relatedCount || s.links?.visible) {
    tabs.push({ key: 'related', label: 'Related', icon: 'bi-diagram-3', badge: badge(relatedCount + (s.links?.count ?? 0)) })
  }
  if (s.files?.visible) tabs.push({ key: 'files', label: 'Files', icon: 'bi-paperclip', badge: badge(s.files.count) })
  tabs.push({ key: 'time', label: 'Time', icon: 'bi-clock', badge: s.time?.count ? formatDuration(s.time.count) : '' })
  if (s.github?.visible) tabs.push({ key: 'github', label: 'GitHub', icon: 'bi-github', badge: '' })
  if (currentTask.value?.project_id && s.agents?.visible) {
    tabs.push({ key: 'agents', label: 'Agents', icon: 'bi-robot', badge: badge(s.agents.count) })
  }
  tabs.push({ key: 'activity', label: 'Activity', icon: 'bi-list-ul', badge: '' })
  return tabs
})

/** The chosen tab, or the first one while the chosen tab has nothing to show. */
const activeToolTab = computed<ToolTabKey | null>(() => {
  const tabs = toolTabs.value
  if (tabs.some((t) => t.key === preferredToolTab.value)) return preferredToolTab.value
  return tabs[0]?.key ?? null
})

watch(activeToolTab, (key) => {
  if (key === 'activity') activityTabOpened.value = true
})
watch(taskId, () => {
  preferredToolTab.value = 'discussion'
})

function selectToolTab(key: ToolTabKey) {
  preferredToolTab.value = key
}

function onToolTabKeydown(e: KeyboardEvent, index: number) {
  const tabs = toolTabs.value
  let next = -1
  if (e.key === 'ArrowRight') next = (index + 1) % tabs.length
  else if (e.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = tabs.length - 1
  if (next < 0) return
  e.preventDefault()
  selectToolTab(tabs[next].key)
  void nextTick(() => document.getElementById(`task-tab-${tabs[next].key}`)?.focus())
}

/** Outside kanban tabs every pane shows, stacked. */
function toolPaneShown(key: ToolTabKey) {
  return !toolTabs.value.length || activeToolTab.value === key
}

function toolPaneAttrs(key: ToolTabKey) {
  if (!toolTabs.value.length) return {}
  return {
    id: `task-pane-${key}`,
    role: 'tabpanel',
    'aria-labelledby': `task-tab-${key}`,
    class: 'kanban-tab-pane',
  }
}

function taskInSelectedProject(t: Task): boolean {
  if (projectId.value === '') return t.project_id == null || t.project_id === 0
  return t.project_id === Number(projectId.value)
}

function formSprintId(value: number | string | '' | null | undefined): number | '' {
  if (value === '' || value == null) return ''
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n) || n <= 0) return ''
  return n
}

function canAssignSprint(sprint: ProjectSprint | undefined): boolean {
  if (!sprint) return false
  return !sprintLockedForUser(sprint, selectedProject.value?.role)
}

function sprintSelectDisabled(sprint: ProjectSprint): boolean {
  if (canAssignSprint(sprint)) return false
  return formSprintId(sprintId.value) !== sprint.id
}

function sprintPayloadId(value: number | string | '' | null | undefined): number {
  const id = formSprintId(value)
  return id === '' ? 0 : id
}

function onSprintChange(event: Event) {
  const target = event.target as HTMLSelectElement | null
  sprintId.value = formSprintId(target?.value)
}

function stubParentTask(id: number, titleText: string, pid: number | ''): Task {
  return {
    id,
    title: titleText,
    description: '',
    completed: false,
    due_date: '',
    project_id: pid === '' ? null : Number(pid),
    priority: 0,
    position: 0,
    tags: [],
    created_at: '',
    modified_at: '',
  }
}

const DESCRIPTION_MIN_HEIGHT = 80
const KANBAN_DESCRIPTION_MIN_HEIGHT = 160

function autosizeDescription() {
  const el = descriptionInput.value
  if (!el) return
  const min = isKanbanTask.value ? KANBAN_DESCRIPTION_MIN_HEIGHT : DESCRIPTION_MIN_HEIGHT
  el.style.height = 'auto'
  el.style.height = `${Math.max(el.scrollHeight, min)}px`
}

function insertDescriptionImage(markdown: string) {
  const el = descriptionInput.value
  const start = el?.selectionStart ?? description.value.length
  const end = el?.selectionEnd ?? start
  const next = insertMarkdownAtCursor(description.value, markdown, start, end)
  if (next.body.length > MAX_DESCRIPTION.value) {
    toast.push(`Description would exceed ${MAX_DESCRIPTION.value} characters`, 'error')
    return
  }
  description.value = next.body
  editingDescription.value = false
}

function startEditDescription() {
  if (readOnly.value) return
  editingDescription.value = true
  void nextTick(() => {
    const el = descriptionInput.value
    el?.focus()
    autosizeDescription()
  })
}

function stopEditDescription() {
  editingDescription.value = false
}

async function ingestDescriptionImage(file: File) {
  const markdown = await uploadImageFile(file)
  if (markdown) insertDescriptionImage(markdown)
}

function onDescriptionPaste(e: ClipboardEvent) {
  const file = imageFileFromClipboard(e)
  if (!file || !imageEnabled.value || readOnly.value) return
  e.preventDefault()
  void ingestDescriptionImage(file)
}

function onDescriptionDragOver(e: DragEvent) {
  if (readOnly.value || !imageEnabled.value || !dropHasFiles(e)) return
  e.preventDefault()
}

function onDescriptionDrop(e: DragEvent) {
  const file = imageFileFromDrop(e)
  if (!file || !imageEnabled.value || readOnly.value) return
  e.preventDefault()
  void ingestDescriptionImage(file)
}

function onTaskOverlayClick() {
  if (shouldIgnoreTaskOverlayClose()) return
  void requestClose()
}

function resetForm() {
  title.value = ''
  description.value = ''
  projectId.value = ''
  parentId.value = ''
  parentTitle.value = ''
  currentTask.value = null
  priority.value = 0
  dueDate.value = ''
  startDate.value = ''
  recurrenceDraft.value = emptyRecurrenceDraft()
  recurrenceDetail.value = null
  selectedTagIds.value = []
  taskTags.value = []
  newTags.value = ''
  completed.value = false
  descriptionError.value = ''
  statusId.value = ''
  sprintId.value = ''
  estimateHours.value = ''
  refinement.value = ''
  claimedBy.value = null
  claimedByName.value = ''
  reviewerId.value = null
  reviewerName.value = ''
  reviewerDraft.value = ''
  statuses.value = []
  sprints.value = []
  taskWorkflow.value = ''
  customFieldValues.value = {}
  editingDescription.value = true
}

async function loadMeta() {
  projects.value = await api.listProjects()
  await loadTagsForProject(projectId.value)
}

async function loadTagsForProject(pid: number | '') {
  const project_id = pid === '' || pid == null ? 0 : Number(pid)
  try {
    allTags.value = await api.listTags({ project_id })
  } catch {
    allTags.value = []
  }
}

async function loadParentCandidates(pid: number | '', keepParentId: number | null = null) {
  const list = await api.listTasks({
    page: 1,
    per_page: 100,
    workflow_claim_scope: 'all',
    project: pid === '' || pid == null ? 0 : pid,
  })
  let loaded = list.tasks.filter((t) => !t.parent_id)
  if (keepParentId && !loaded.some((t) => t.id === keepParentId)) {
    const cached = rootTasks.value.find((t) => t.id === keepParentId && !t.parent_id)
    if (cached) {
      loaded = [cached, ...loaded]
    } else {
      try {
        const parent = await api.getTask(keepParentId)
        if (!parent.parent_id) {
          loaded = [parent, ...loaded]
        }
      } catch {
        if (parentTitle.value) {
          loaded = [stubParentTask(keepParentId, parentTitle.value, pid), ...loaded]
        }
      }
    }
  }
  rootTasks.value = loaded
}

async function loadSprintsForProject(pid: number | '') {
  if (pid === '' || pid == null) {
    sprints.value = []
    return
  }
  const proj = projects.value.find((p) => p.id === Number(pid))
  const kanban =
    taskWorkflow.value === 'kanban' || (proj?.workflow_mode || 'classic') === 'kanban'
  if (!kanban) {
    sprints.value = []
    return
  }
  try {
    sprints.value = await api.listProjectSprints(Number(pid))
    if (mode.value === 'add' && sprintId.value === '' && defaultSprintId.value != null) {
      if (defaultSprintId.value > 0) {
        const target = sprints.value.find((s) => s.id === defaultSprintId.value)
        if (canAssignSprint(target)) {
          sprintId.value = defaultSprintId.value
        }
      }
    }
  } catch {
    sprints.value = []
  }
}

async function loadStatusesForProject(pid: number | '') {
  if (pid === '' || pid == null) {
    statuses.value = []
    return
  }
  const proj = projects.value.find((p) => p.id === Number(pid))
  const kanban =
    taskWorkflow.value === 'kanban' || (proj?.workflow_mode || 'classic') === 'kanban'
  if (!kanban) {
    statuses.value = []
    return
  }
  try {
    statuses.value = await api.listProjectStatuses(Number(pid))
    if (statusId.value === '' && mode.value === 'add') {
      const def = statuses.value.find((s) => s.is_default)
      if (def) statusId.value = def.id
    }
  } catch {
    statuses.value = []
  }
}

async function resolveParentTitle(parentTaskId: number) {
  const cached = rootTasks.value.find((t) => t.id === parentTaskId)
  if (cached) {
    parentTitle.value = cached.title
    return
  }
  try {
    const parent = await api.getTask(parentTaskId)
    parentTitle.value = parent.title
    if (!parent.parent_id && !rootTasks.value.some((t) => t.id === parent.id)) {
      rootTasks.value = [parent, ...rootTasks.value]
    }
  } catch {
    parentTitle.value = `Task #${parentTaskId}`
  }
}

async function loadTask(id: number) {
  const task = await api.getTask(id)
  currentTask.value = task
  title.value = task.title
  description.value = task.description || ''
  editingDescription.value = false
  projectId.value = task.project_id ?? ''
  parentId.value = task.parent_id ?? ''
  parentTitle.value = ''
  await loadParentCandidates(projectId.value, task.parent_id ?? null)
  if (task.parent_id) {
    await resolveParentTitle(task.parent_id)
  }
  priority.value = task.priority
  dueDate.value = task.due_date || ''
  startDate.value = task.start_date || ''
  taskTags.value = task.tags ? [...task.tags] : []
  selectedTagIds.value = taskTags.value.map((t) => t.id)
  newTags.value = ''
  completed.value = task.completed
  descriptionError.value = ''
  statusId.value = task.status_id ?? ''
  sprintId.value = formSprintId(task.sprint_id)
  estimateHours.value = task.estimate_hours ?? ''
  refinement.value = task.refinement ?? ''
  claimedBy.value = task.claimed_by ?? null
  claimedByName.value = task.claimed_by_name || ''
  applyReviewer(task)
  reviewerDraft.value = ''
  taskWorkflow.value = task.project_workflow || ''
  customFieldValues.value = { ...(task.fields || {}) }
  recurrenceDraft.value = draftFromRecurrence(task.recurrence)
  recurrenceDetail.value = null
  if (!task.parent_id) void loadRecurrenceDetail(id)
  await loadStatusesForProject(projectId.value)
  await loadSprintsForProject(projectId.value)
  await loadTagsForProject(projectId.value)
}

async function loadRecurrenceDetail(id: number) {
  try {
    const detail = await api.getTaskRecurrence(id)
    if (taskId.value === id) recurrenceDetail.value = detail
  } catch {
    recurrenceDetail.value = null
  }
}

function recurrenceChanged(): boolean {
  return !sameRecurrenceDraft(recurrenceDraft.value, draftFromRecurrence(currentTask.value?.recurrence))
}

function sameIdSet(a: number[], b: number[]) {
  if (a.length !== b.length) return false
  const seen = new Set(a)
  return b.every((id) => seen.has(id))
}

function fieldPatchValue(value: unknown): unknown {
  if (value === '' || value === undefined) return null
  if (value && typeof value === 'object' && 'id' in (value as object)) {
    const id = Number((value as { id?: number }).id)
    return Number.isFinite(id) && id > 0 ? id : null
  }
  return value
}

function fieldsPayload(from: Record<string, unknown> = customFieldValues.value): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(from)) {
    out[key] = fieldPatchValue(value)
  }
  return out
}

function sameFields(a: Record<string, unknown>, b: Record<string, unknown>) {
  return JSON.stringify(fieldsPayload(a)) === JSON.stringify(fieldsPayload(b))
}

type FormSnapshot = {
  title: string
  description: string
  projectId: number | ''
  parentId: number | ''
  priority: number
  dueDate: string
  startDate: string
  selectedTagIds: number[]
  newTags: string
  statusId: number | ''
  sprintId: number | ''
  estimateHours: number | ''
  refinement: RefinementState
  customFields: Record<string, unknown>
  recurrence: RecurrenceDraft
}

const addFormBaseline = ref<FormSnapshot | null>(null)

function captureFormSnapshot(): FormSnapshot {
  return {
    title: title.value,
    description: description.value,
    projectId: projectId.value,
    parentId: parentId.value,
    priority: priority.value,
    dueDate: dueDate.value,
    startDate: startDate.value,
    selectedTagIds: [...selectedTagIds.value],
    newTags: newTags.value,
    statusId: statusId.value,
    sprintId: formSprintId(sprintId.value),
    estimateHours: estimateHours.value,
    refinement: refinement.value,
    customFields: { ...customFieldValues.value },
    recurrence: { ...recurrenceDraft.value },
  }
}

/** The form as it would look freshly loaded from `t`. */
function snapshotFromTask(t: Task): FormSnapshot {
  return {
    title: t.title,
    description: t.description || '',
    projectId: t.project_id ?? '',
    parentId: t.parent_id ?? '',
    priority: t.priority,
    dueDate: t.due_date || '',
    startDate: t.start_date || '',
    selectedTagIds: (t.tags || []).map((x) => x.id),
    newTags: '',
    statusId: t.status_id ?? '',
    sprintId: formSprintId(t.sprint_id),
    estimateHours: t.estimate_hours ?? '',
    refinement: t.refinement ?? '',
    customFields: { ...(t.fields || {}) },
    recurrence: draftFromRecurrence(t.recurrence),
  }
}

type FormKey = keyof FormSnapshot

function sameFormValue(key: FormKey, a: FormSnapshot, b: FormSnapshot): boolean {
  switch (key) {
    case 'selectedTagIds':
      return sameIdSet(a.selectedTagIds, b.selectedTagIds)
    case 'customFields':
      return sameFields(a.customFields, b.customFields)
    case 'recurrence':
      return sameRecurrenceDraft(a.recurrence, b.recurrence)
    case 'newTags':
      return a.newTags.trim() === b.newTags.trim()
    case 'sprintId':
      return formSprintId(a.sprintId) === formSprintId(b.sprintId)
    default:
      return a[key] === b[key]
  }
}

function changedFormKeys(from: FormSnapshot, to: FormSnapshot): FormKey[] {
  return (Object.keys(from) as FormKey[]).filter((k) => !sameFormValue(k, from, to))
}

/** Custom field keys whose values differ between two field maps. */
function changedFieldKeys(from: Record<string, unknown>, to: Record<string, unknown>): string[] {
  const a = fieldsPayload(from)
  const b = fieldsPayload(to)
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  return [...keys].filter((k) => JSON.stringify(a[k] ?? null) !== JSON.stringify(b[k] ?? null))
}

const FORM_KEY_LABELS: Record<FormKey, string> = {
  title: 'title',
  description: 'description',
  projectId: 'project',
  parentId: 'parent task',
  priority: 'priority',
  dueDate: 'due date',
  startDate: 'start date',
  selectedTagIds: 'tags',
  newTags: 'tags',
  statusId: 'status',
  sprintId: 'sprint',
  estimateHours: 'estimate',
  refinement: 'refinement',
  customFields: 'custom fields',
  recurrence: 'repeat rule',
}

/**
 * The task changed on the server after this form loaded it. Move the form
 * onto the latest copy: fields this person did not touch take the new values,
 * fields they edited keep their edit. Returns the labels of fields both sides
 * changed, which the person should look over before saving again.
 */
function rebaseFormOnto(latest: Task): string[] {
  const base = currentTask.value ? snapshotFromTask(currentTask.value) : snapshotFromTask(latest)
  const theirs = snapshotFromTask(latest)
  const mine = captureFormSnapshot()
  const myKeys = new Set(changedFormKeys(base, mine))
  const theirKeys = new Set(changedFormKeys(base, theirs))

  if (!myKeys.has('title')) title.value = theirs.title
  if (!myKeys.has('description')) description.value = theirs.description
  if (!myKeys.has('projectId')) projectId.value = theirs.projectId
  if (!myKeys.has('parentId')) parentId.value = theirs.parentId
  if (!myKeys.has('priority')) priority.value = theirs.priority
  if (!myKeys.has('dueDate')) dueDate.value = theirs.dueDate
  if (!myKeys.has('startDate')) startDate.value = theirs.startDate
  if (!myKeys.has('selectedTagIds') && !myKeys.has('newTags')) {
    taskTags.value = latest.tags ? [...latest.tags] : []
    selectedTagIds.value = theirs.selectedTagIds
  }
  if (!myKeys.has('statusId')) statusId.value = theirs.statusId
  if (!myKeys.has('sprintId')) sprintId.value = theirs.sprintId
  if (!myKeys.has('estimateHours')) estimateHours.value = theirs.estimateHours
  if (!myKeys.has('refinement')) refinement.value = theirs.refinement
  if (!myKeys.has('recurrence')) recurrenceDraft.value = theirs.recurrence

  // Custom fields merge per key, not as one blob.
  const myFieldKeys = new Set(changedFieldKeys(base.customFields, mine.customFields))
  const theirFieldKeys = changedFieldKeys(base.customFields, theirs.customFields)
  const mergedFields: Record<string, unknown> = { ...theirs.customFields }
  for (const k of myFieldKeys) mergedFields[k] = mine.customFields[k]
  customFieldValues.value = mergedFields

  completed.value = latest.completed
  claimedBy.value = latest.claimed_by ?? null
  claimedByName.value = latest.claimed_by_name || ''
  currentTask.value = latest

  const both = new Set<string>()
  for (const k of myKeys) {
    if (k === 'customFields') continue
    if (theirKeys.has(k)) both.add(FORM_KEY_LABELS[k])
  }
  if (theirFieldKeys.some((k) => myFieldKeys.has(k))) both.add(FORM_KEY_LABELS.customFields)
  return [...both]
}

/**
 * A save was refused because the task changed elsewhere. Ask what to do;
 * never overwrite silently.
 */
async function resolveStaleSave(err: unknown): Promise<void> {
  const stale = staleTaskOf(err)
  if (!stale || !taskId.value) return
  const id = taskId.value
  let latest = stale.current
  if (!latest) {
    try {
      latest = await api.getTask(id)
    } catch {
      toast.push('This task is no longer available', 'info')
      close()
      return
    }
  }
  const choice = await askUnsaved({
    title: 'Task changed elsewhere',
    message:
      'Someone changed this task (maybe you, on another device) since you opened it. Nothing was saved. ' +
      'Keep your edits on top of the latest version, or discard them?',
    saveLabel: 'Keep my edits',
    discardLabel: 'Discard my edits',
    stayLabel: 'Cancel',
  })
  if (choice === 'stay' || taskId.value !== id) return
  if (choice === 'discard') {
    await loadTask(id)
    toast.push('Loaded the latest version', 'info')
    return
  }
  const overlap = rebaseFormOnto(latest)
  await activityLog.value?.refresh()
  toast.push(
    overlap.length
      ? `Updated to the latest version. You both changed: ${overlap.join(', ')}. Review, then save.`
      : 'Updated to the latest version with your edits kept. Review, then save.',
    'info',
    8000,
  )
}

function isFormDirty(): boolean {
  if (mode.value !== 'edit' || !currentTask.value) return false
  const t = currentTask.value
  const project = t.project_id ?? ''
  const parent = t.parent_id ?? ''
  const due = t.due_date || ''
  const status = t.status_id ?? ''
  const sprint = formSprintId(t.sprint_id)
  const estimate = t.estimate_hours ?? ''
  const tagIds = (t.tags || []).map((x) => x.id)
  return (
    title.value !== t.title ||
    (description.value || '') !== (t.description || '') ||
    projectId.value !== project ||
    parentId.value !== parent ||
    priority.value !== t.priority ||
    dueDate.value !== due ||
    startDate.value !== (t.start_date || '') ||
    completed.value !== t.completed ||
    statusId.value !== status ||
    formSprintId(sprintId.value) !== sprint ||
    estimateHours.value !== estimate ||
    refinement.value !== (t.refinement ?? '') ||
    newTags.value.trim() !== '' ||
    !sameIdSet(selectedTagIds.value, tagIds) ||
    !sameFields(customFieldValues.value, t.fields || {}) ||
    (!isSubtask.value && recurrenceChanged())
  )
}

function isAddFormDirty(): boolean {
  if (mode.value !== 'add' || !addFormBaseline.value) return false
  const b = addFormBaseline.value
  return (
    title.value !== b.title ||
    description.value !== b.description ||
    projectId.value !== b.projectId ||
    parentId.value !== b.parentId ||
    priority.value !== b.priority ||
    dueDate.value !== b.dueDate ||
    startDate.value !== b.startDate ||
    !sameIdSet(selectedTagIds.value, b.selectedTagIds) ||
    newTags.value.trim() !== b.newTags.trim() ||
    statusId.value !== b.statusId ||
    formSprintId(sprintId.value) !== b.sprintId ||
    estimateHours.value !== b.estimateHours ||
    refinement.value !== b.refinement ||
    !sameFields(customFieldValues.value, b.customFields) ||
    !sameRecurrenceDraft(recurrenceDraft.value, b.recurrence)
  )
}

function isDiscussionDirty() {
  return discussionRef.value?.isDirty() ?? false
}

function isLeaveDirty() {
  if (mode.value === 'add') return isAddFormDirty()
  if (mode.value === 'view') return isDiscussionDirty()
  return isFormDirty() || isDiscussionDirty()
}

async function flushDiscussion(): Promise<boolean> {
  if (!discussionRef.value) return true
  return discussionRef.value.flushUnsaved()
}

useLiveUpdates(async (_last: LiveEvent, batch: LiveEvent[]) => {
  if (!open.value || !taskId.value) return
  const id = taskId.value
  const relevant = batch.filter(
    (e) => e.type !== 'notification.created' && !isOwnTabLiveEvent(e, user.value?.id),
  )
  if (!relevant.length) return

  // The discussion is independent of the edit form, so new comments always
  // show up, even while the task form has unsaved changes.
  if (batchTouchesDiscussion(relevant, id)) {
    await discussionRef.value?.reload()
  }

  const projectChanged = relevant.some(
    (e) =>
      (e.type === 'project.updated' || e.type === 'project.created' || e.type === 'project.deleted') &&
      !!currentTask.value?.project_id &&
      e.project_id === currentTask.value.project_id,
  )
  if (projectChanged) {
    await loadMeta()
    await loadTagsForProject(projectId.value)
    await loadStatusesForProject(projectId.value)
    await loadSprintsForProject(projectId.value)
  }

  const taskEvents = relevant.filter(
    (e) =>
      e.type === LIVE_RESYNC ||
      (e.type !== 'task.commented' && !e.type.startsWith('project.') && (!e.task_id || e.task_id === id)),
  )
  if (!taskEvents.length) return
  if (taskEvents.some((e) => e.type === 'task.deleted')) {
    toast.push('This task was deleted in another session', 'info')
    close()
    return
  }
  if (mode.value === 'add') return
  if (isFormDirty()) {
    toast.push('This task was changed somewhere else. Your edits are kept; saving will let you combine them with the latest version.', 'info', 6000)
    return
  }
  try {
    await loadTask(id)
    await linksPanel.value?.reload()
    await activityLog.value?.refresh()
  } catch {
    toast.push('This task is no longer available', 'info')
    close()
  }
})

async function resolveTagIds(): Promise<number[]> {
  const ids = new Set(
    selectedTagIds.value.filter((id) => {
      const tag = allTags.value.find((t) => t.id === id)
      return tag && !isProtectedTag(tag)
    }),
  )
  const parts = newTags.value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  for (const name of parts) {
    if (name.toLowerCase() === 'removed' || name.toLowerCase() === 'archived') continue
    const existing = allTags.value.find((t) => t.name.toLowerCase() === name.toLowerCase())
    if (existing) {
      if (!isProtectedTag(existing)) ids.add(existing.id)
      continue
    }
    const created = await api.createTag(name, projectId.value === '' ? null : Number(projectId.value))
    allTags.value = [...allTags.value, created]
    ids.add(created.id)
  }
  const out = Array.from(ids)
  if (out.length > 5) {
    throw new Error('Maximum 5 tags per task')
  }
  return out
}

function validateDescription() {
  if (description.value.length > MAX_DESCRIPTION.value) {
    descriptionError.value = `Description must be ${MAX_DESCRIPTION.value} characters or fewer.`
    return false
  }
  descriptionError.value = ''
  return true
}

async function saveStatusOnly(): Promise<boolean> {
  if (!taskId.value || !currentTask.value) return false
  const payload: Parameters<typeof api.patchTask>[1] = {}
  const t = currentTask.value
  if (isKanbanTask.value && canChangeStatus.value && statusId.value !== '') {
    const next = Number(statusId.value)
    if (next !== (t.status_id ?? 0)) payload.status_id = next
  }
  if (isKanbanTask.value && canChangeSprint.value) {
    const nextSprint = sprintPayloadId(sprintId.value)
    const currentSprint = t.sprint_id && t.sprint_id > 0 ? t.sprint_id : 0
    if (nextSprint !== currentSprint) payload.sprint_id = nextSprint
  }
  if (!Object.keys(payload).length) {
    const flushed = await flushDiscussion()
    if (!flushed) return false
    close()
    return true
  }
  saving.value = true
  try {
    const updated = await api.patchTask(taskId.value, { ...payload, ...versionGuard(t.version) })
    currentTask.value = updated
    notifySaved(updated, false)
    toast.push('Task saved', 'success')
    await activityLog.value?.refresh()
    const flushed = await flushDiscussion()
    if (!flushed) return false
    close()
    return true
  } catch (err) {
    if (staleTaskOf(err)) {
      await resolveStaleSave(err)
      return false
    }
    const msg = err instanceof APIError ? err.message : err instanceof Error ? err.message : 'Save failed'
    toast.push(msg, 'error')
    return false
  } finally {
    saving.value = false
  }
}

async function save(keepOpen = false): Promise<boolean> {
  if (mode.value === 'view') return false
  if (mode.value === 'edit' && !canEditDetails.value) {
    return saveStatusOnly()
  }
  if (mode.value === 'add' && !canCreateTask.value) {
    toast.push('Your role cannot create tasks', 'error')
    return false
  }
  if (readOnly.value) return false
  if (!title.value.trim()) return false
  if (!validateDescription()) return false
  if (isCompanyMode.value && !isSubtask.value && projectId.value === '') {
    toast.push('Choose a project for this task', 'error')
    return false
  }
  if (mode.value === 'add' && selectedProject.value && isArchivedProject(selectedProject.value)) {
    toast.push('Cannot add tasks to an archived project', 'error')
    return false
  }
  if (isKanbanTask.value && estimateInvalid.value) {
    toast.push('Estimates are in quarter hours, from 0 to 999.75', 'error')
    return false
  }
  saving.value = true
  try {
    const tagIds = await resolveTagIds()
    if (mode.value === 'add') {
      const created = await api.createTask({
        title: title.value.trim(),
        description: description.value,
        project_id: isSubtask.value
          ? undefined
          : projectId.value === ''
            ? null
            : Number(projectId.value),
        parent_id: isSubtask.value ? Number(parentId.value) : null,
        priority: priority.value,
        due_date: dueDate.value || undefined,
        start_date: startDate.value || undefined,
        tag_ids: tagIds,
        ...(isKanbanTask.value && statusId.value !== ''
          ? { status_id: Number(statusId.value) }
          : {}),
        ...(isKanbanTask.value ? { sprint_id: sprintPayloadId(sprintId.value) } : {}),
        ...(isKanbanTask.value && estimateHours.value !== ''
          ? { estimate_hours: Number(estimateHours.value) }
          : {}),
        // Unset leaves the project default ("new tasks need refinement") to the server.
        ...(isKanbanTask.value && refinementActive.value && refinement.value !== ''
          ? { refinement: refinement.value }
          : {}),
        ...(Object.keys(fieldsPayload()).length ? { fields: fieldsPayload() } : {}),
        ...(!isSubtask.value && recurrenceDraft.value.enabled
          ? { recurrence: recurrencePayload(recurrenceDraft.value) ?? undefined }
          : {}),
      })
      notifySaved(created, !keepOpen)
      toast.push(isSubtask.value ? 'Subtask created' : 'Task created', 'success')

      if (keepOpen) {
        // Clear title & description for next task while keeping project/due/parent pre-selected
        title.value = ''
        description.value = ''
        newTags.value = ''
        descriptionError.value = ''
        editingDescription.value = true
        await nextTick()
        autosizeDescription()
        titleInput.value?.focus()
        addFormBaseline.value = captureFormSnapshot()
      } else {
        resetForm()
      }
      return true
    }
    if (!taskId.value || !currentTask.value) return false
    // Send only what this person changed, so a save never rewrites fields
    // they didn't touch with what this form happened to load.
    const base = snapshotFromTask(currentTask.value)
    const mine = captureFormSnapshot()
    const changed = new Set(changedFormKeys(base, mine))
    const payload: Parameters<typeof api.patchTask>[1] = { ...versionGuard(currentTask.value.version) }
    if (changed.has('title')) payload.title = title.value.trim()
    if (changed.has('description')) payload.description = description.value
    if (changed.has('priority')) payload.priority = priority.value
    if (changed.has('selectedTagIds') || changed.has('newTags')) payload.tag_ids = tagIds
    if (changed.has('parentId')) {
      // 0 clears parent (promotes to root); JSON null is indistinguishable from omitted for **int.
      payload.parent_id = isSubtask.value ? Number(parentId.value) : 0
    }
    if (!isSubtask.value && changed.has('projectId')) {
      // 0 clears project (inbox); same convention as parent_id.
      payload.project_id = projectId.value === '' ? 0 : Number(projectId.value)
    }
    if (changed.has('dueDate')) {
      if (dueDate.value) payload.due_date = dueDate.value
      else payload.clear_due_date = true
    }
    if (changed.has('startDate')) payload.start_date = startDate.value || null
    if (isKanbanTask.value) {
      if (changed.has('statusId') && statusId.value !== '') payload.status_id = Number(statusId.value)
      if (changed.has('sprintId')) payload.sprint_id = sprintPayloadId(sprintId.value)
      // Re-saving an unchanged estimate derived from legacy points would detach it
      // from the project's points-to-hours rate.
      if (changed.has('estimateHours')) payload.estimate_hours = estimateHours.value === '' ? null : Number(estimateHours.value)
      if (changed.has('refinement')) payload.refinement = refinement.value
    }
    const fieldKeys = changedFieldKeys(base.customFields, mine.customFields)
    if (fieldKeys.length) {
      const all = fieldsPayload()
      payload.fields = Object.fromEntries(fieldKeys.map((k) => [k, all[k] ?? null]))
    }
    if (!isSubtask.value && recurrenceChanged()) {
      payload.recurrence = recurrencePayload(recurrenceDraft.value)
    }
    const updated = await api.patchTask(taskId.value, payload)
    currentTask.value = updated
    notifySaved(updated, false)
    toast.push('Task saved', 'success')
    editingDescription.value = false
    await activityLog.value?.refresh()
    const flushed = await flushDiscussion()
    if (!flushed) return false
    close()
    return true
  } catch (err) {
    if (staleTaskOf(err)) {
      await resolveStaleSave(err)
      return false
    }
    const msg = err instanceof APIError ? err.message : err instanceof Error ? err.message : 'Save failed'
    toast.push(msg, 'error')
    return false
  } finally {
    saving.value = false
  }
}

const closePromptOpen = ref(false)

async function guardClose(): Promise<boolean> {
  if (closePromptOpen.value || confirmState.open) return false
  if (!isLeaveDirty()) return true
  closePromptOpen.value = true
  try {
    const choice = await askUnsaved({
      title: 'Unsaved changes',
      message: 'You have unsaved changes. Save them before leaving?',
    })
    if (choice === 'stay') return false
    if (choice === 'discard') return true
    if (mode.value === 'view') {
      return flushDiscussion()
    }
    return save(false)
  } finally {
    closePromptOpen.value = false
  }
}

onMounted(() => {
  setCloseGuard(guardClose)
})

onUnmounted(() => {
  setCloseGuard(null)
})

function applyDuePreset(preset: string) {
  const today = new Date()
  if (preset === 'today') {
    dueDate.value = today.toISOString().slice(0, 10)
  } else if (preset === 'tomorrow') {
    today.setDate(today.getDate() + 1)
    dueDate.value = today.toISOString().slice(0, 10)
  } else if (preset === 'week') {
    today.setDate(today.getDate() + 7)
    dueDate.value = today.toISOString().slice(0, 10)
  } else if (preset === 'clear') {
    dueDate.value = ''
  }
}

function toggleTag(id: number, checked: boolean) {
  if (checked) {
    if (!selectedTagIds.value.includes(id)) selectedTagIds.value = [...selectedTagIds.value, id]
  } else {
    selectedTagIds.value = selectedTagIds.value.filter((x) => x !== id)
  }
}

watch(description, validateDescription)

watch(isKanbanTask, async () => {
  await nextTick()
  autosizeDescription()
})

async function onProjectChange() {
  const names = new Set(
    [...taskTags.value, ...allTags.value]
      .filter((t) => selectedTagIds.value.includes(t.id))
      .map((t) => t.name.toLowerCase()),
  )
  taskWorkflow.value = ''
  await loadStatusesForProject(projectId.value)
  await loadSprintsForProject(projectId.value)
  if (statusId.value !== '' && !statuses.value.some((s) => s.id === statusId.value)) {
    statusId.value = ''
  }
  const selectedSprint = formSprintId(sprintId.value)
  if (selectedSprint !== '' && !sprints.value.some((s) => s.id === selectedSprint)) {
    sprintId.value = ''
  }
  await loadTagsForProject(projectId.value)
  customFieldValues.value = {}
  selectedTagIds.value = allTags.value.filter((t) => names.has(t.name.toLowerCase())).map((t) => t.id)
  const previousParent = isSubtask.value ? Number(parentId.value) : null
  await loadParentCandidates(projectId.value)
  if (previousParent && !parentOptions.value.some((t) => t.id === previousParent)) {
    parentId.value = ''
    parentTitle.value = ''
  }
}

let loadSeq = 0

watch(
  () => ({
    isOpen: open.value,
    m: mode.value,
    id: taskId.value,
    due: defaultDueDate.value,
    proj: defaultProjectId.value,
    parent: defaultParentId.value,
    parentLabel: defaultParentTitle.value,
  }),
  async ({ isOpen, m, id, due, proj, parent, parentLabel }) => {
    if (!isOpen) {
      loading.value = false
      addFormBaseline.value = null
      return
    }
    // Show spinner immediately so the modal never flashes empty/stale form content.
    const seq = ++loadSeq
    loading.value = true
    try {
      await loadMeta()
      if (seq !== loadSeq) return
      if ((m === 'edit' || m === 'view') && id) {
        await loadTask(id)
      } else {
        resetForm()
        if (due) dueDate.value = due
        if (proj) projectId.value = Number(proj)
        else if (isCompanyMode.value) projectId.value = firstWritableProjectId.value
        if (parent) {
          parentId.value = parent
          parentTitle.value = parentLabel || ''
          await loadParentCandidates(projectId.value, parent)
          const p = rootTasks.value.find((t) => t.id === parent)
          parentTitle.value = parentLabel || p?.title || ''
          const inheritedProject = p?.project_id ?? ''
          if (inheritedProject !== projectId.value) {
            projectId.value = inheritedProject
            await loadParentCandidates(projectId.value, parent)
          }
        } else {
          await loadParentCandidates(projectId.value)
        }
        await loadStatusesForProject(projectId.value)
        await loadSprintsForProject(projectId.value)
        await loadTagsForProject(projectId.value)
      }
    } catch (err) {
      if (seq !== loadSeq) return
      toast.push(err instanceof APIError ? err.message : 'Failed to load task', 'error')
      close()
      return
    } finally {
      if (seq === loadSeq) loading.value = false
    }
    if (seq !== loadSeq || !open.value) return
    await nextTick()
    autosizeDescription()
    if (m === 'add') {
      addFormBaseline.value = captureFormSnapshot()
      titleInput.value?.focus()
    } else {
      addFormBaseline.value = null
    }
  },
  { immediate: true },
)

async function onParentChange() {
  if (!isSubtask.value) {
    parentTitle.value = ''
    return
  }
  const p = rootTasks.value.find((t) => t.id === Number(parentId.value))
  parentTitle.value = p?.title || ''
  if (p?.project_id) projectId.value = p.project_id
  else projectId.value = ''
  taskWorkflow.value = p?.project_workflow || ''
  await loadStatusesForProject(projectId.value)
  await loadSprintsForProject(projectId.value)
  if (p?.sprint_id) {
    const inherited = sprints.value.find((s) => s.id === p.sprint_id)
    if (canAssignSprint(inherited)) {
      sprintId.value = formSprintId(p.sprint_id)
    }
  }
}

function openRelated(id: number) {
  if (mode.value === 'view') openView(id)
  else openEdit(id)
}

async function claimCurrentTask() {
  if (!taskId.value || !canClaimTask.value) return
  claiming.value = true
  try {
    const task = await claimWithConfirm(
      { id: taskId.value, title: title.value, claimed_by: claimedBy.value, claimed_by_name: claimedByName.value },
      user.value?.id,
    )
    if (!task) return
    claimedBy.value = task.claimed_by ?? null
    claimedByName.value = task.claimed_by_name || ''
    toast.push('Task claimed', 'success')
    notifySaved(task, false)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not claim task', 'error')
  } finally {
    claiming.value = false
  }
}

async function unclaimCurrentTask() {
  if (!taskId.value || !canClaimTask.value) return
  claiming.value = true
  try {
    const task = await releaseTask(taskId.value, claimedBy.value)
    if (!task) return
    claimedBy.value = task.claimed_by ?? null
    claimedByName.value = task.claimed_by_name || ''
    toast.push('Task released', 'success')
    notifySaved(task, false)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not release task', 'error')
  } finally {
    claiming.value = false
  }
}

function applyReviewer(task: Task) {
  reviewerId.value = task.reviewer_id ?? null
  reviewerName.value = task.reviewer_name || ''
}

async function setReviewer() {
  const name = reviewerDraft.value.trim().replace(/^@/, '')
  if (!taskId.value || !name || !canEditDetails.value) return
  reviewerBusy.value = true
  try {
    const task = await api.setTaskReviewer(taskId.value, name)
    applyReviewer(task)
    reviewerDraft.value = ''
    toast.push('Reviewer set', 'success')
    notifySaved(task, false)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not set reviewer', 'error')
  } finally {
    reviewerBusy.value = false
  }
}

async function clearReviewer() {
  if (!taskId.value || !(canEditDetails.value || isMyReview.value)) return
  reviewerBusy.value = true
  try {
    const task = await api.clearTaskReviewer(taskId.value)
    applyReviewer(task)
    toast.push('Reviewer removed', 'success')
    notifySaved(task, false)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not remove reviewer', 'error')
  } finally {
    reviewerBusy.value = false
  }
}

async function archiveCurrentTask() {
  if (!taskId.value || !canArchiveTask.value || !currentTask.value) return
  const ok = await askConfirm({
    title: 'Archive task?',
    message: archiveConfirmMessage(currentTask.value),
    confirmLabel: 'Archive',
    warning: true,
  })
  if (!ok) return
  try {
    const updated = await api.archiveTask(taskId.value)
    toast.push('Task archived', 'info')
    notifySaved(updated, true)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Archive failed', 'error')
  }
}

async function restoreCurrentTask() {
  if (!taskId.value || !canRestoreTask.value) return
  try {
    const updated = await api.restoreTask(taskId.value)
    toast.push('Task restored', 'success')
    notifySaved(updated, true)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Restore failed', 'error')
  }
}

async function deleteCurrentTask() {
  if (!taskId.value || !canDeleteTask.value || !currentTask.value) return
  const childCount = currentTask.value.child_count ?? currentTask.value.children?.length ?? 0
  if (childCount > 0) {
    deleteDialogOpen.value = true
    return
  }
  const ok = await askConfirm({
    title: 'Delete task?',
    message: `Move “${currentTask.value.title}” to the trash? You can restore it from Trash.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  await runSidebarDelete({ mode: 'cascade' })
}

async function runSidebarDelete(opts: { mode: 'cascade' | 'reparent'; new_parent_id?: number | null }) {
  if (!taskId.value) return
  const deletedId = taskId.value
  try {
    await api.deleteTask(deletedId, opts)
    toast.push('Moved to trash', 'info')
    deleteDialogOpen.value = false
    notifyDeleted(deletedId, opts.mode)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

</script>

<template>
  <div
    v-if="open"
    id="taskModal"
    class="modal show d-block"
    :class="{ 'kanban-task-overlay': isKanbanTask }"
    style="background: rgba(0,0,0,0.5);"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    @click.self="onTaskOverlayClick"
  >
    <div
      class="modal-dialog ordryn-task-dialog"
      :class="
        isKanbanTask
          ? 'kanban-task-dialog'
          : 'modal-dialog-centered modal-lg modal-dialog-scrollable'
      "
    >
      <div
        class="modal-content border-0 shadow"
        :class="{ 'kanban-task-content': isKanbanTask }"
        style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
      >
        <div
          class="modal-header"
          :class="isKanbanTask ? 'kanban-task-header' : 'border-0 pb-0'"
        >
          <template v-if="isKanbanTask">
            <div class="d-flex align-items-center gap-2 flex-grow-1 min-w-0 flex-wrap">
              <h5 class="modal-title fw-bold mb-0">
                {{ kanbanHeaderTitle }}
                <a
                v-if="showTaskNumber"
                :href="taskPageHref"
                class="text-muted fw-normal task-page-link"
                title="Go to this task in its project"
                @click="goToTaskInProject"
              >#{{ taskId }}</a>
              </h5>
              <TaskWatchButton v-if="showTaskNumber && taskId" :task-id="taskId" />
            </div>
            <div class="task-header-actions">
              <button
                v-if="canClaimTask && !loading && (!claimedBy || claimedBy !== user?.id)"
                type="button"
                class="btn btn-sm btn-outline-primary task-header-btn"
                :disabled="claiming"
                @click="claimCurrentTask"
              >
                {{ claimedBy ? 'Take over' : 'Claim' }}
              </button>
              <button
                v-else-if="canClaimTask && !loading"
                type="button"
                class="btn btn-sm btn-outline-secondary task-header-btn"
                :disabled="claiming"
                @click="unclaimCurrentTask"
              >
                Release
              </button>
              <button
                v-if="canSaveTask && !loading && mode === 'add'"
                type="button"
                class="btn btn-sm btn-outline-primary task-header-btn"
                :disabled="saving || !title.trim()"
                @click="save(true)"
              >
                Save &amp; Add Another
              </button>
              <button
                v-if="canSaveTask && !loading"
                type="button"
                class="btn btn-sm btn-primary task-header-btn"
                :disabled="saving"
                @click="save(false)"
              >
                {{ saving ? 'Saving…' : submitText }}
              </button>
              <div v-if="showHeaderMenu" class="dropdown task-header-menu">
                <button
                  type="button"
                  class="btn btn-sm btn-outline-secondary"
                  data-bs-toggle="dropdown"
                  aria-expanded="false"
                  aria-label="More task actions"
                  title="More actions"
                >
                  <i class="bi bi-three-dots" />
                </button>
                <ul class="dropdown-menu dropdown-menu-end shadow-sm">
                  <li v-if="showTaskNumber">
                    <button type="button" class="dropdown-item" @click="copyPermalink">
                      <i class="bi bi-link-45deg me-2" />Copy link
                    </button>
                  </li>
                  <li v-if="canSaveAsTemplate">
                    <button type="button" class="dropdown-item" @click="saveTemplateOpen = true">
                      <i class="bi bi-files me-2" />Save as template
                    </button>
                  </li>
                  <li v-if="(showTaskNumber || canSaveAsTemplate) && (canToggleArchive || (canDeleteTask && !loading))">
                    <hr class="dropdown-divider" />
                  </li>
                  <li v-if="canToggleArchive">
                    <button
                      type="button"
                      class="dropdown-item"
                      :disabled="saving"
                      @click="taskIsArchived ? restoreCurrentTask() : archiveCurrentTask()"
                    >
                      <i class="bi me-2" :class="taskIsArchived ? 'bi-arrow-counterclockwise' : 'bi-archive'" />
                      {{ taskIsArchived ? 'Restore' : 'Archive' }}
                    </button>
                  </li>
                  <li v-if="canDeleteTask && !loading">
                    <button type="button" class="dropdown-item text-danger" :disabled="saving" @click="deleteCurrentTask">
                      <i class="bi bi-trash me-2" />Delete
                    </button>
                  </li>
                </ul>
              </div>
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary task-header-btn task-header-close"
                id="closeSidebar"
                aria-label="Close"
                @click="requestClose"
              >
                <i class="bi bi-x-lg" />
              </button>
            </div>
          </template>
          <template v-else>
            <h5 class="modal-title fw-bold d-flex align-items-center gap-2 flex-wrap">
              {{ sidebarTitle }}
              <a
                v-if="showTaskNumber"
                :href="taskPageHref"
                class="text-muted fw-normal task-page-link"
                title="Go to this task in its project"
                @click="goToTaskInProject"
              >#{{ taskId }}</a>
              <button
                v-if="showTaskNumber"
                type="button"
                class="btn btn-sm btn-outline-secondary"
                title="Copy link"
                aria-label="Copy task link"
                @click="copyPermalink"
              >
                <i class="bi bi-link-45deg" /> Copy link
              </button>
              <TaskWatchButton v-if="showTaskNumber && taskId" :task-id="taskId" />
              <button
                v-if="showTaskNumber && !isSubtask && !loading"
                type="button"
                class="btn btn-sm btn-outline-secondary"
                title="Save this task as a reusable template"
                @click="saveTemplateOpen = true"
              >
                <i class="bi bi-files" /> Save as template
              </button>
            </h5>
            <button type="button" class="btn-close" id="closeSidebar" aria-label="Close" @click="requestClose" />
          </template>
        </div>
        <div class="modal-body" :class="isKanbanTask ? 'kanban-task-body' : 'py-3'">
      <div v-if="loading" class="d-flex flex-column align-items-center justify-content-center gap-2 py-5" aria-busy="true">
        <div class="spinner-border text-primary" role="status">
          <span class="visually-hidden">Loading task…</span>
        </div>
        <p class="mb-0">Loading task…</p>
      </div>
      <form
        v-else
        id="newTaskForm"
        :class="{ 'kanban-task-form': isKanbanTask }"
        @submit.prevent="save(false)"
      >
        <div :class="{ 'kanban-task-head': isKanbanTask }">
        <div class="form-group" :class="{ 'kanban-title-group': isKanbanTask }">
          <label for="title" :class="{ 'visually-hidden': isKanbanTask }">Title:</label>
          <input
            id="title"
            ref="titleInput"
            v-model="title"
            type="text"
            class="form-control"
            :class="{ 'kanban-title-input': isKanbanTask }"
            :required="!readOnly"
            :readonly="readOnly"
            :disabled="readOnly"
            placeholder="Your Task Title"
          />
        </div>
        <div class="form-group">
          <div v-if="completed" class="alert alert-success py-2 mb-2">
            <i class="bi bi-check-circle" /> This task is completed
          </div>
          <div class="d-flex align-items-center justify-content-between gap-2">
            <label for="description" :class="{ 'kanban-section-label': isKanbanTask }" class="mb-0">Description:</label>
            <button
              v-if="!readOnly && !showDescriptionEditor"
              type="button"
              class="btn btn-sm btn-link p-0"
              @click="startEditDescription"
            >
              Edit
            </button>
            <button
              v-else-if="!readOnly && showDescriptionEditor && (mode !== 'add' || description.trim())"
              type="button"
              class="btn btn-sm btn-link p-0"
              @click="stopEditDescription"
            >
              Done
            </button>
          </div>
          <WysiwygEditor
            v-if="showDescriptionEditor"
            id="description"
            ref="descriptionEditor"
            v-model="description"
            :maxlength="MAX_DESCRIPTION"
            :rows="isKanbanTask ? 6 : 4"
            placeholder="Add a task description… Format with markdown, click 'Upload image' or paste/drop an image."
            @open-task="openRelated"
          />
          <div
            v-else
            id="description"
            class="task-description-view"
            tabindex="0"
            @paste="onDescriptionPaste"
            @dragover="onDescriptionDragOver"
            @drop="onDescriptionDrop"
          >
            <RichBody v-if="description.trim()" :body="description" @open-task="openRelated" />
            <button
              v-else-if="!readOnly"
              type="button"
              class="btn btn-link p-0 text-muted"
              @click="startEditDescription"
            >
              Add a description…
            </button>
            <span v-else class="text-muted">No description</span>
          </div>
          <div v-if="!readOnly && showDescriptionEditor" class="d-flex justify-content-between align-items-center mt-1 gap-2">
            <small class="form-hint">Supports Markdown. Use "Upload image" in the toolbar, or paste / drop an image.</small>
            <div class="d-flex align-items-center gap-2">
              <small class="text-muted"><span id="char-count">{{ charCount }}</span>/{{ MAX_DESCRIPTION }}</small>
            </div>
          </div>
          <!-- <div v-else-if="!readOnly && imageEnabled" class="form-hint mt-1">
            Paste or drop an image here, or click Edit to change the text.
          </div> -->
          <div v-if="descriptionError" id="description-error" class="invalid-feedback d-block">
            {{ descriptionError }}
          </div>
        </div>
        </div>
        <div :class="{ 'kanban-task-aside': isKanbanTask }">
        <!-- Kanban rail group headings; placed among the fields with CSS order. -->
        <template v-if="isKanbanTask">
          <h6 class="kanban-rail-heading kanban-order-h-details">Details</h6>
          <h6 v-if="mode === 'edit' || mode === 'view'" class="kanban-rail-heading kanban-order-h-people">People</h6>
          <h6 class="kanban-rail-heading kanban-order-h-dates">Dates</h6>
          <h6 class="kanban-rail-heading kanban-order-h-organize">Organize</h6>
        </template>
        <div class="form-group mt-2 kanban-prop kanban-order-parent">
          <label for="parent_id">Parent task</label>
          <ParentTaskCombobox
            v-model="parentId"
            input-id="parent_id"
            :options="parentOptions"
            :disabled="parentPickerDisabled"
            placeholder="Search tasks…"
            @change="onParentChange"
          />
          <small v-if="isSubtask" class="form-hint d-block mt-1">
            Subtask of {{ parentTitle || 'selected parent' }}. Project is inherited.
          </small>
        </div>
        <div class="form-group mt-2 kanban-prop kanban-order-project">
          <label for="project_id">Project</label>
          <select
            id="project_id"
            v-model="projectId"
            class="form-select"
            :required="isCompanyMode && !isSubtask"
            :disabled="readOnly || isSubtask || projectLockedToOwner"
            @change="onProjectChange"
          >
            <option v-if="!isCompanyMode" value="">No project</option>
            <option v-else-if="projectId === ''" value="" disabled>Choose a project</option>
            <option v-for="p in assignableProjects" :key="p.id" :value="p.id" :disabled="isArchivedProject(p)">{{ projectOptionLabel(p) }}</option>
          </select>
          <small v-if="projectLockedToOwner" class="form-hint d-block mt-1">
            Only the project owner can move this task to another project.
          </small>
          <small v-if="mode === 'add' && selectedProject && isArchivedProject(selectedProject)" class="text-warning">
            This project is archived, so new tasks cannot be added.
          </small>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-prop kanban-order-status">
          <label for="status_id">Status</label>
          <select id="status_id" v-model="statusId" class="form-select" :disabled="!canChangeStatus">
            <option v-if="!statuses.length" value="">No statuses</option>
            <option v-for="s in statuses" :key="s.id" :value="s.id" :disabled="statusOptionDisabled(s)">
              {{ s.name }}{{ s.is_done ? ' (done)' : '' }}{{ s.is_default ? ' (default)' : '' }}
            </option>
          </select>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-prop kanban-order-sprint">
          <label for="sprint_id">Sprint</label>
          <select
            id="sprint_id"
            class="form-select"
            :disabled="!canChangeSprint"
            :value="sprintId === '' ? '' : String(sprintId)"
            @change="onSprintChange"
          >
            <option value="">{{ selectedProject?.backlog_name || 'Backlog' }}</option>
            <option
              v-for="s in sprints"
              :key="s.id"
              :value="String(s.id)"
              :disabled="sprintSelectDisabled(s)"
            >
              {{ sprintOptionLabel(s, { activeSuffix: true, lockedSuffix: true }) }}
            </option>
          </select>
        </div>
        <div v-if="isKanbanTask && (mode === 'edit' || mode === 'view')" class="form-group mt-2 kanban-prop kanban-order-claim">
          <label class="d-block">Claimed by</label>
          <div class="d-flex align-items-center gap-2 flex-wrap">
            <span class="badge border" :class="claimedBy ? 'text-bg-primary' : 'text-bg-light text-muted'">
              <i class="bi bi-person me-1" />{{ claimerLabel }}
            </span>
          </div>
        </div>
        <div v-if="showReviewer" class="form-group mt-2 kanban-prop kanban-order-reviewer task-reviewer">
          <label class="d-block" for="task-reviewer-input">Reviewer</label>
          <div class="d-flex align-items-center gap-2 flex-wrap">
            <span class="badge border" :class="reviewerId ? 'text-bg-info' : 'text-bg-light text-muted'">
              <i class="bi bi-eye me-1" />{{ reviewerLabel }}
            </span>
            <button
              v-if="reviewerId && (canEditDetails || isMyReview)"
              type="button"
              class="btn btn-sm btn-link p-0"
              :disabled="reviewerBusy"
              @click="clearReviewer"
            >
              {{ isMyReview && !canEditDetails ? 'Step down' : 'Remove' }}
            </button>
          </div>
          <form v-if="canEditDetails" class="d-flex gap-2 mt-1" @submit.prevent="setReviewer">
            <UserSearchCombobox
              v-model="reviewerDraft"
              input-id="task-reviewer-input"
              :project-id="reviewerProjectId"
              :disabled="reviewerBusy"
            />
            <button
              type="submit"
              class="btn btn-sm btn-outline-primary text-nowrap"
              :disabled="reviewerBusy || !reviewerDraft.trim()"
            >
              {{ reviewerId ? 'Change' : 'Ask' }}
            </button>
          </form>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-prop kanban-order-estimate">
          <label for="estimate_hours">Estimate (hours)</label>
          <input
            id="estimate_hours"
            v-model="estimateHours"
            type="number"
            class="form-control"
            :class="{ 'is-invalid': estimateInvalid }"
            min="0"
            :max="MAX_ESTIMATE_HOURS"
            :step="ESTIMATE_STEP"
            inputmode="decimal"
            :disabled="readOnly"
            :readonly="readOnly"
            placeholder="e.g. 2.5"
            title="Quarter hours (0.25, 0.5, 0.75)"
            aria-describedby="estimate_hours_hint"
          />
          <small id="estimate_hours_hint" class="form-hint" :class="{ 'text-danger': estimateInvalid }">
            Quarter hours (0.25, 0.5, 0.75).
          </small>
        </div>
        <div v-if="showRefinementField" class="form-group mt-2 kanban-prop kanban-order-refinement">
          <label for="task_refinement">Refinement</label>
          <select
            id="task_refinement"
            v-model="refinement"
            class="form-select"
            :disabled="readOnly || !refinementActive"
          >
            <option value="">Not set</option>
            <option value="needs_refinement">Needs Refinement</option>
            <option value="refined">Refined</option>
          </select>
        </div>
        <div class="form-group mt-2 kanban-prop kanban-order-priority">
          <label for="priority">Priority</label>
          <select id="priority" v-model.number="priority" class="form-select" :disabled="readOnly">
            <option :value="0">None</option>
            <option :value="1">Low</option>
            <option :value="2">Medium</option>
            <option :value="3">High</option>
            </select>
        </div>
        <div v-if="projectId !== ''" class="kanban-order-fields">
          <TaskSidebarFields
            v-model="customFieldValues"
            :project-id="Number(projectId)"
            :read-only="readOnly"
            :compact="isKanbanTask"
            :reveal-key="taskId"
          />
        </div>
        <div class="form-group mt-2 kanban-prop kanban-order-start">
          <label for="start_date">Start date</label>
          <input
            id="start_date"
            v-model="startDate"
            type="date"
            class="form-control"
            :max="dueDate || undefined"
            :disabled="readOnly"
            :readonly="readOnly"
          />
        </div>
        <div class="form-group mt-2 kanban-prop kanban-order-due">
          <label for="due_date">Due date</label>
          <input id="due_date" v-model="dueDate" type="date" class="form-control" :disabled="readOnly" :readonly="readOnly" />
          <div v-if="!readOnly" class="btn-group btn-group-sm mt-1 flex-wrap due-presets" role="group" aria-label="Due date presets">
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('today')">Today</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('tomorrow')">Tomorrow</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('week')">+1 week</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('clear')">Clear</button>
          </div>
        </div>
        <div v-if="!isSubtask && (!readOnly || recurrenceDraft.enabled)" class="form-group mt-2 kanban-order-recurrence">
          <TaskRecurrenceEditor
            v-model="recurrenceDraft"
            :read-only="readOnly"
            :due-date="dueDate"
            :next-due="recurrenceChanged() ? '' : recurrenceDetail?.recurrence?.next_due"
          />
          <div v-if="recurrenceDetail && recurrenceDetail.series_id" class="small text-muted mt-1">
            <span v-if="recurrenceDetail.recurrence">Occurrence {{ recurrenceDetail.recurrence.occurrence }}</span>
            <span v-else>Part of a recurring series</span>
            <button
              v-if="recurrenceDetail.prev_task_id"
              type="button"
              class="btn btn-link btn-sm p-0 ms-2 align-baseline"
              @click="openRelated(recurrenceDetail.prev_task_id)"
            >Previous #{{ recurrenceDetail.prev_task_id }}</button>
            <button
              v-if="recurrenceDetail.next_task_id"
              type="button"
              class="btn btn-link btn-sm p-0 ms-2 align-baseline"
              @click="openRelated(recurrenceDetail.next_task_id)"
            >Next #{{ recurrenceDetail.next_task_id }}</button>
            <details v-if="recurrenceDetail.history.length > 1" class="mt-1">
              <summary>Series history ({{ recurrenceDetail.history.length }})</summary>
              <ul class="list-unstyled mb-0 mt-1">
                <li v-for="h in recurrenceDetail.history" :key="h.task_id">
                  <i class="bi me-1" :class="h.completed ? 'bi-check-circle text-success' : 'bi-circle'" />
                  <button type="button" class="btn btn-link btn-sm p-0 align-baseline" @click="openRelated(h.task_id)">#{{ h.task_id }}</button>
                  <span v-if="h.due_date"> · due {{ h.due_date }}</span>
                </li>
              </ul>
            </details>
          </div>
        </div>
        <div v-if="readOnly" class="form-group mt-2 kanban-order-tags">
          <label>Tags</label>
          <div v-if="taskTags.length" class="d-flex flex-wrap gap-1 mt-1">
            <span
              v-for="tag in taskTags"
              :key="tag.id"
              class="tag-chip"
              :style="{ backgroundColor: tag.color || '#6c757d' }"
            >{{ tag.name }}</span>
          </div>
          <p v-else class="text-muted small mb-0 mt-1">No tags</p>
        </div>
        <div v-else class="kanban-order-tags">
          <div v-if="pickerTags.length" class="form-group mt-2">
            <label>Tags (max 5)</label>
            <div v-if="isKanbanTask" class="kanban-tag-chips">
              <button
                v-for="tag in pickerTags"
                :key="tag.id"
                type="button"
                class="tag-chip kanban-tag-toggle"
                :class="{ 'is-selected': selectedTagIds.includes(tag.id) }"
                :style="
                  selectedTagIds.includes(tag.id)
                    ? { backgroundColor: tag.color || '#6c757d' }
                    : { borderColor: tag.color || '#6c757d', color: 'var(--ordryn-text)' }
                "
                :aria-pressed="selectedTagIds.includes(tag.id)"
                @click="toggleTag(tag.id, !selectedTagIds.includes(tag.id))"
              >
                {{ tag.name }}
              </button>
            </div>
            <template v-else>
              <div v-for="tag in pickerTags" :key="tag.id" class="form-check">
                <input
                  :id="`tag-${tag.id}`"
                  type="checkbox"
                  class="form-check-input"
                  :checked="selectedTagIds.includes(tag.id)"
                  @change="toggleTag(tag.id, ($event.target as HTMLInputElement).checked)"
                />
                <label class="form-check-label" :for="`tag-${tag.id}`">
                  <span class="tag-chip" :style="{ backgroundColor: tag.color || '#6c757d' }">{{ tag.name }}</span>
                </label>
              </div>
            </template>
          </div>
          <div class="form-group mt-2" v-if="canManageTags">
            <label for="new_tags">Add tags (comma-separated)</label>
            <input
              id="new_tags"
              v-model="newTags"
              type="text"
              class="form-control"
              placeholder="e.g. work, urgent"
              maxlength="200"
            />
            <small class="form-hint">New tag names are created on save (max 5 tags per task).</small>
          </div>
        </div>
        </div>

        <!--
          Task tools. On a kanban task they sit behind tabs under the description;
          elsewhere every pane shows, stacked, in this order.
        -->
        <div :class="{ 'kanban-task-rest': isKanbanTask }">
        <div v-if="toolTabs.length" class="kanban-tabs" role="tablist" aria-label="Task sections">
          <button
            v-for="(t, i) in toolTabs"
            :id="`task-tab-${t.key}`"
            :key="t.key"
            type="button"
            role="tab"
            class="kanban-tab"
            :class="{ active: activeToolTab === t.key }"
            :aria-selected="activeToolTab === t.key"
            :aria-controls="`task-pane-${t.key}`"
            :tabindex="activeToolTab === t.key ? 0 : -1"
            @click="selectToolTab(t.key)"
            @keydown="onToolTabKeydown($event, i)"
          >
            <i class="bi" :class="t.icon" aria-hidden="true" />
            <span>{{ t.label }}</span>
            <span v-if="t.badge" class="kanban-tab-count">{{ t.badge }}</span>
          </button>
        </div>

        <div v-if="(mode === 'edit' || mode === 'view') && taskId" v-show="toolPaneShown('related')" v-bind="toolPaneAttrs('related')">
          <div
            v-if="relatedParent || relatedChildren.length"
            class="form-group mt-2"
          >
            <label class="d-block">Related tasks</label>
            <div v-if="relatedParent" class="mb-1">
              <span class="form-hint d-block">Parent</span>
              <button
                type="button"
                class="btn btn-link text-start text-decoration-none p-0"
                @click="openRelated(relatedParent.id)"
              >
                {{ relatedParent.title }}
              </button>
            </div>
            <div v-if="relatedChildren.length">
              <span class="form-hint d-block">Subtasks</span>
              <ul class="list-unstyled mb-0">
                <li v-for="child in relatedChildren" :key="child.id">
                  <button
                    type="button"
                    class="btn btn-link text-start text-decoration-none p-0"
                    @click="openRelated(child.id)"
                  >
                    {{ child.title }}
                  </button>
                </li>
              </ul>
            </div>
          </div>
          <div class="form-group mt-2">
            <TaskLinksPanel
              ref="linksPanel"
              :task-id="taskId"
              :can-edit="canEditDetails"
              @open="openRelated"
              @summary="(s) => (panelSummary.links = s)"
            />
          </div>
        </div>
        <div
          v-if="(mode === 'edit' || mode === 'view') && taskId && currentTask?.project_id"
          v-show="toolPaneShown('agents')"
          class="form-group mt-2"
          v-bind="toolPaneAttrs('agents')"
        >
          <TaskAgentsPanel :task-id="taskId" @summary="(s) => (panelSummary.agents = s)" />
        </div>
        <div v-if="(mode === 'edit' || mode === 'view') && taskId" v-show="toolPaneShown('github')" v-bind="toolPaneAttrs('github')">
          <TaskGitHubPanel
            :task-id="taskId"
            :project-id="projectId === '' ? null : Number(projectId)"
            :initial-issue="currentTask?.github ?? null"
            :read-only="readOnly"
            @summary="(s) => (panelSummary.github = s)"
          />
        </div>
        <div v-if="(mode === 'edit' || mode === 'view') && taskId" v-show="toolPaneShown('files')" v-bind="toolPaneAttrs('files')">
          <TaskAttachmentsPanel
            :task-id="taskId"
            :can-edit="canEditDetails"
            :can-manage="projectId === '' || hasProjectPerm(selectedProject, PROJECT_PERMS.PROJECT_MANAGE)"
            @summary="(s) => (panelSummary.files = s)"
          />
        </div>
        <div v-if="(mode === 'edit' || mode === 'view') && taskId" v-show="toolPaneShown('time')" v-bind="toolPaneAttrs('time')">
          <TaskTimePanel
            :class="isKanbanTask ? 'form-group mt-3' : 'form-group mt-3 border-top pt-3'"
            :task-id="taskId"
            :initial-minutes="currentTask?.time_spent_minutes ?? 0"
            :can-log="canLogTime"
            @summary="(s) => (panelSummary.time = s)"
          />
        </div>
        <div v-if="showDiscussion && taskId" v-show="toolPaneShown('discussion')" v-bind="toolPaneAttrs('discussion')">
          <TaskDiscussion
            ref="discussionRef"
            :task-id="taskId"
            :project-id="currentTask?.project_id ?? null"
            :current-user-id="user?.id ?? null"
            :is-owner="discussionCanModerate"
            :can-edit-task="canEditDetails"
            :fill-height="isKanbanTask"
            @summary="(s) => (panelSummary.discussion = s)"
          />
        </div>

        <div v-if="canSaveTask && !isKanbanTask" class="task-header-actions mt-3">
          <button type="submit" class="btn btn-sm btn-primary task-header-btn" :disabled="saving">
            {{ saving ? 'Saving…' : submitText }}
          </button>
          <button
            v-if="mode === 'add'"
            type="button"
            class="btn btn-sm btn-outline-primary task-header-btn"
            :disabled="saving || !title.trim()"
            @click="save(true)"
          >
            Save &amp; Add Another
          </button>
          <button
            v-if="mode === 'edit' && ((taskIsArchived && canRestoreTask) || (!taskIsArchived && canArchiveTask))"
            type="button"
            class="btn btn-sm task-header-btn"
            :class="taskIsArchived ? 'btn-success' : 'btn-warning'"
            :disabled="saving"
            @click="taskIsArchived ? restoreCurrentTask() : archiveCurrentTask()"
          >
            {{ taskIsArchived ? 'Restore' : 'Archive' }}
          </button>
          <button
            v-if="canDeleteTask"
            type="button"
            class="btn btn-sm btn-danger task-header-btn"
            :disabled="saving"
            @click="deleteCurrentTask"
          >
            Delete
          </button>
        </div>

        <!-- On a kanban task the log mounts (and loads) the first time its tab opens. -->
        <div
          v-if="(mode === 'edit' || mode === 'view') && taskId && (!isKanbanTask || activityTabOpened)"
          v-show="toolPaneShown('activity')"
          v-bind="toolPaneAttrs('activity')"
        >
          <TaskActivityLog ref="activityLog" :task-id="taskId" :bare="isKanbanTask" />
        </div>
        </div>
      </form>
        </div>
      </div>
    </div>
  </div>
  <DeleteTaskDialog
    :open="deleteDialogOpen"
    :task="currentTask"
    :root-tasks="rootTasks"
    @cancel="deleteDialogOpen = false"
    @cascade="runSidebarDelete({ mode: 'cascade' })"
    @reparent="(id) => runSidebarDelete({ mode: 'reparent', new_parent_id: id })"
  />
  <SaveTemplateModal
    :open="saveTemplateOpen"
    :draft="templateDraft"
    :due-date="dueDate"
    :share-project-id="canCreateTask && projectId !== '' ? Number(projectId) : null"
    :share-project-name="selectedProject?.name || ''"
    @close="saveTemplateOpen = false"
  />
</template>

<style scoped>
.task-page-link {
  text-decoration: none;
}
.task-page-link:hover,
.task-page-link:focus-visible {
  text-decoration: underline;
}
textarea.task-description-input {
  height: auto;
  min-height: 80px;
  resize: vertical;
  overflow-y: hidden;
}

.task-description-view {
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.375rem;
  background: var(--ordryn-muted-bg, #f8f6ee);
  min-height: 80px;
  min-width: 0;
  overflow: hidden;
}

.kanban-task-head textarea.task-description-input {
  min-height: 160px;
}

.kanban-task-overlay {
  overflow: hidden;
  padding: 0;
}

.kanban-task-dialog {
  --bs-modal-width: min(1400px, 90vw);
  max-width: min(1400px, 90vw);
  width: 90vw;
  height: 90vh;
  max-height: 90vh;
  margin: 5vh auto;
}

.kanban-task-content {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.kanban-task-header.modal-header {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  border-bottom: 1px solid var(--ordryn-card-border, #dee2e6);
  padding: 0.75rem 1.25rem;
  flex-shrink: 0;
  gap: 0.75rem;
}

.task-header-actions {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: max-content;
  grid-template-rows: 32px;
  align-items: stretch;
  gap: 0.5rem;
  flex: 0 0 auto;
  height: 32px;
}

.task-header-actions > .task-header-btn.btn {
  --bs-btn-padding-y: 0;
  --bs-btn-padding-x: 0.75rem;
  --bs-btn-line-height: 1;
  --bs-btn-font-size: 0.875rem;
  --bs-btn-border-width: 1px;
  box-sizing: border-box;
  height: 32px !important;
  min-height: 0;
  max-height: 32px;
  margin: 0 !important;
  padding: 0 0.75rem !important;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.875rem;
  line-height: 1;
  white-space: nowrap;
}

.task-header-actions > .task-header-close.btn {
  --bs-btn-padding-x: 0;
  width: 32px;
  padding-left: 0 !important;
  padding-right: 0 !important;
}

.kanban-task-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.kanban-task-body > [aria-busy='true'] {
  flex: 1;
}

.kanban-task-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  grid-template-rows: auto minmax(0, 1fr);
  grid-template-areas:
    'head aside'
    'rest aside';
  flex: 1;
  min-height: 0;
  height: 100%;
}

.kanban-task-head {
  grid-area: head;
  padding: 1rem 1.5rem 0;
  min-width: 0;
}

.kanban-task-rest {
  grid-area: rest;
  overflow-y: auto;
  padding: 0 1.5rem 1.5rem;
  min-width: 0;
  min-height: 0;
}

.kanban-task-aside {
  grid-area: aside;
  overflow-y: auto;
  padding: 1rem 1rem 1.25rem;
  border-left: 1px solid var(--ordryn-card-border, #dee2e6);
  background: color-mix(in srgb, var(--ordryn-muted-bg, #f8f6ee) 55%, var(--ordryn-card-bg, #fff));
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.kanban-task-aside .form-group,
.kanban-task-aside :deep(.form-group) {
  margin-bottom: 0.85rem;
}

.kanban-task-aside label,
.kanban-task-aside .form-label,
.kanban-task-aside :deep(label),
.kanban-task-aside :deep(.form-label) {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--ordryn-muted, #64748b);
  margin-bottom: 0.2rem;
}

.kanban-task-aside .form-control,
.kanban-task-aside .form-select,
.kanban-task-aside :deep(.form-control),
.kanban-task-aside :deep(.form-select) {
  font-size: 0.875rem;
}

/* Rail order: grouped under the headings below. */
.kanban-task-aside .kanban-order-h-details { order: 0; }
.kanban-task-aside .kanban-order-status { order: 1; }
.kanban-task-aside .kanban-order-sprint { order: 2; }
.kanban-task-aside .kanban-order-priority { order: 3; }
.kanban-task-aside .kanban-order-estimate { order: 4; }
.kanban-task-aside .kanban-order-refinement { order: 5; }
.kanban-task-aside .kanban-order-project { order: 6; }
.kanban-task-aside .kanban-order-fields { order: 7; }
.kanban-task-aside .kanban-order-h-people { order: 10; }
.kanban-task-aside .kanban-order-claim { order: 11; }
.kanban-task-aside .kanban-order-reviewer { order: 12; }
.kanban-task-aside .kanban-order-h-dates { order: 20; }
.kanban-task-aside .kanban-order-start { order: 21; }
.kanban-task-aside .kanban-order-due { order: 22; }
.kanban-task-aside .kanban-order-recurrence { order: 23; }
.kanban-task-aside .kanban-order-h-organize { order: 30; }
.kanban-task-aside .kanban-order-parent { order: 31; }
.kanban-task-aside .kanban-order-tags { order: 32; }

.kanban-rail-heading {
  margin: 0.9rem 0 0.35rem;
  padding-top: 0.85rem;
  border-top: 1px solid var(--ordryn-card-border, #dee2e6);
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ordryn-muted, #64748b);
}

.kanban-rail-heading.kanban-order-h-details {
  margin-top: 0;
  padding-top: 0;
  border-top: 0;
}

/* Property rows: label on the left, value on the right. */
.kanban-task-aside .kanban-prop,
.kanban-task-aside :deep(.task-sidebar-fields .form-group) {
  display: grid;
  grid-template-columns: 6.5rem minmax(0, 1fr);
  column-gap: 0.5rem;
  align-items: center;
  margin: 0 !important;
  padding: 0.15rem 0;
}

.kanban-task-aside .kanban-prop > label,
.kanban-task-aside :deep(.task-sidebar-fields .form-group > label) {
  grid-column: 1;
  margin: 0;
  overflow-wrap: anywhere;
}

.kanban-task-aside .kanban-prop > :not(label),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > :not(label)) {
  grid-column: 2;
}

.kanban-task-aside :deep(.task-sidebar-fields .form-group > textarea),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-check) {
  grid-column: 1 / -1;
}

/* Long-text fields stack: label above a normal bordered box. */
.kanban-task-aside :deep(.task-sidebar-fields .form-group.cf-type-markdown) {
  row-gap: 0.25rem;
  padding: 0.35rem 0;
}

.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-check) {
  margin: 0.15rem 0;
}

.kanban-task-aside :deep(.task-sidebar-fields-add) {
  padding: 0.25rem 0;
}

.kanban-task-aside :deep(.task-sidebar-fields-add > .btn) {
  font-size: 0.8rem;
}

/* No number spinners in the rail; they read as clutter on empty values. */
.kanban-task-aside input[type='number'],
.kanban-task-aside :deep(input[type='number']) {
  appearance: textfield;
  -moz-appearance: textfield;
}

.kanban-task-aside input[type='number']::-webkit-inner-spin-button,
.kanban-task-aside input[type='number']::-webkit-outer-spin-button,
.kanban-task-aside :deep(input[type='number']::-webkit-inner-spin-button),
.kanban-task-aside :deep(input[type='number']::-webkit-outer-spin-button) {
  -webkit-appearance: none;
  margin: 0;
}

/* Inline-looking controls: no chrome until hovered or focused. */
.kanban-task-aside .kanban-prop > .form-select:not(.is-invalid),
.kanban-task-aside .kanban-prop > .form-control:not(.is-invalid),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-select),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-control:not(textarea)) {
  border-color: transparent;
  background-color: transparent;
  box-shadow: none;
  padding-top: 0.25rem;
  padding-bottom: 0.25rem;
  padding-left: 0.4rem;
}

.kanban-task-aside .kanban-prop > .form-select:not(.is-invalid):hover:not(:disabled),
.kanban-task-aside .kanban-prop > .form-control:not(.is-invalid):hover:not(:disabled),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-select:hover:not(:disabled)),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-control:hover:not(:disabled)) {
  border-color: var(--ordryn-card-border, #dee2e6);
  background-color: var(--ordryn-input-bg, var(--ordryn-card-bg, #fff));
}

.kanban-task-aside .kanban-prop > .form-select:not(.is-invalid):focus,
.kanban-task-aside .kanban-prop > .form-control:not(.is-invalid):focus,
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-select:focus),
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-control:focus) {
  border-color: var(--ordryn-accent, #2563eb);
  background-color: var(--ordryn-input-bg, var(--ordryn-card-bg, #fff));
}

/* Read-only values read as text, not as disabled dropdowns. */
.kanban-task-aside .kanban-prop > .form-select:disabled,
.kanban-task-aside :deep(.task-sidebar-fields .form-group > .form-select:disabled) {
  background-image: none;
  opacity: 1;
  color: var(--ordryn-text);
}

.kanban-task-aside #estimate_hours_hint:not(.text-danger) {
  display: none;
}

.kanban-task-aside .due-presets {
  justify-self: start;
}

.kanban-task-aside .due-presets .btn {
  font-size: 0.72rem;
  padding: 0.1rem 0.45rem;
}

/* Tool tabs under the description. */
.kanban-tabs {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  gap: 0.15rem;
  overflow-x: auto;
  scrollbar-width: none;
  margin: 0 -1.5rem;
  padding: 0.5rem 1.5rem 0;
  background: var(--ordryn-card-bg, #fff);
  border-bottom: 1px solid var(--ordryn-card-border, #dee2e6);
}

.kanban-tabs::-webkit-scrollbar {
  display: none;
}

.kanban-tab {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  flex-shrink: 0;
  margin-bottom: -1px;
  padding: 0.5rem 0.75rem;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  font-size: 0.875rem;
  font-weight: 500;
  white-space: nowrap;
  color: var(--ordryn-muted, #64748b);
  cursor: pointer;
}

.kanban-tab:hover {
  color: var(--ordryn-text);
}

.kanban-tab.active {
  color: var(--ordryn-text);
  border-bottom-color: var(--ordryn-accent, #2563eb);
}

.kanban-tab:focus-visible {
  outline: 2px solid var(--ordryn-accent, #2563eb);
  outline-offset: -2px;
  border-radius: 0.25rem 0.25rem 0 0;
}

.kanban-tab-count {
  padding: 0.05rem 0.4rem;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 600;
  background: var(--ordryn-muted-bg, #f1f5f9);
  color: var(--ordryn-muted, #64748b);
}

.kanban-tab.active .kanban-tab-count {
  background: color-mix(in srgb, var(--ordryn-accent, #2563eb) 15%, transparent);
  color: var(--ordryn-accent, #2563eb);
}

.kanban-tab-pane {
  padding-top: 0.75rem;
}

/* The tab already names the section. */
.kanban-tab-pane :deep(.task-discussion > h3) {
  display: none;
}

.kanban-tab-pane :deep(.task-discussion--fill) {
  margin-top: 0;
}

.kanban-tab-pane > :deep(.form-group:first-child),
.kanban-tab-pane > :deep(div:first-child > .form-group:first-child) {
  margin-top: 0 !important;
}

.kanban-tab-pane :deep(label:not(.form-label):not(.form-check-label)) {
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.03em;
  text-transform: uppercase;
  color: var(--ordryn-muted, #64748b);
}

.task-header-actions > .task-header-menu > .btn {
  width: 32px;
  height: 32px;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.task-header-menu .dropdown-menu {
  font-size: 0.875rem;
}

.kanban-title-group {
  margin-bottom: 0.75rem;
}

.kanban-title-input {
  font-size: 1.35rem;
  font-weight: 600;
  line-height: 1.3;
  border: 1px solid transparent;
  background: transparent;
  padding-left: 0.25rem;
  box-shadow: none;
}

.kanban-title-input:hover:not(:disabled):not([readonly]) {
  border-color: var(--ordryn-card-border, #dee2e6);
}

.kanban-title-input:focus {
  border-color: var(--ordryn-accent, #2563eb);
  background: var(--ordryn-input-bg, var(--ordryn-card-bg, #fff));
  box-shadow: none;
}

.kanban-section-label {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--ordryn-muted, #64748b);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

.kanban-tag-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  margin-top: 0.35rem;
}

.kanban-tag-toggle {
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  background: transparent;
  color: var(--ordryn-text);
  cursor: pointer;
}

.kanban-tag-toggle.is-selected {
  color: #fff;
  border-color: transparent;
}

.kanban-tag-toggle:focus-visible {
  outline: 2px solid var(--ordryn-accent, #2563eb);
  outline-offset: 2px;
}

@media (max-width: 991.98px) {
  .kanban-task-dialog,
  .ordryn-task-dialog {
    width: 100vw;
    max-width: 100vw;
    height: 100dvh;
    max-height: 100dvh;
    margin: 0;
  }

  .kanban-task-dialog .modal-content,
  .ordryn-task-dialog .modal-content {
    min-height: 100dvh;
    height: 100%;
    border-radius: 0;
  }

  .kanban-task-header.modal-header {
    flex-wrap: wrap;
    padding: 0.65rem 0.85rem;
    gap: 0.5rem;
  }

  .task-header-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
    height: auto;
    width: auto;
  }

  .task-header-actions > .task-header-btn.btn {
    height: 30px !important;
    max-height: 30px;
    font-size: 0.8rem;
    padding: 0 0.6rem !important;
  }

  .task-header-actions > .task-header-close.btn {
    width: 30px;
    padding: 0 !important;
  }

  .task-header-actions > .task-header-menu > .btn {
    width: 30px;
    height: 30px;
  }

  .kanban-tabs {
    margin: -0.85rem -1rem 0;
    padding: 0.35rem 1rem 0;
  }

  .kanban-task-body {
    overflow-y: auto;
    overflow-x: hidden;
    -webkit-overflow-scrolling: touch;
  }

  .kanban-task-form {
    display: flex;
    flex-direction: column;
    flex: none;
    width: 100%;
    min-height: auto;
    height: auto;
    grid-template-columns: none;
    grid-template-rows: none;
    grid-template-areas: none;
  }

  .kanban-task-head,
  .kanban-task-aside,
  .kanban-task-rest {
    grid-area: auto;
    width: 100%;
    min-height: auto;
    flex: none;
    overflow: visible;
  }

  .kanban-task-head {
    order: 1;
    padding: 0.85rem 1rem 0.5rem;
  }

  .kanban-task-aside {
    order: 2;
    border-left: none;
    border-top: 1px solid var(--ordryn-card-border, #dee2e6);
    padding: 0.85rem 1rem 1.25rem;
  }

  .kanban-task-rest {
    order: 3;
    border-top: 1px solid var(--ordryn-card-border, #dee2e6);
    padding: 0.85rem 1rem 2.5rem;
  }
}
</style>
