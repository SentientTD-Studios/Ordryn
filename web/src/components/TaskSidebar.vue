<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, ProjectSprint, ProjectStatus, Tag, Task, TaskEvent, TaskGitHubIssue, TaskTimeEntry } from '@/api/types'
import { APIError } from '@/api/types'
import ParentTaskCombobox from '@/components/ParentTaskCombobox.vue'
import DeleteTaskDialog from '@/components/DeleteTaskDialog.vue'
import TaskDiscussion from '@/components/TaskDiscussion.vue'
import WysiwygEditor from '@/components/WysiwygEditor.vue'
import RichBody from '@/components/RichBody.vue'
import { useImageUpload } from '@/composables/useImageUpload'
import { insertMarkdownAtCursor } from '@/utils/taskCommentBody'
import { dropHasFiles, imageFileFromClipboard, imageFileFromDrop, shouldIgnoreTaskOverlayClose } from '@/utils/imageUpload'
import { useAuth } from '@/composables/useAuth'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { projectOptionLabel, isArchivedProject, isProjectOwner } from '@/utils/projectLabel'
import { sprintLockedForUser, sprintOptionLabel } from '@/utils/sprintLabel'
import { useLiveUpdates, isOwnFocusedLiveEvent, type LiveEvent } from '@/composables/useLiveUpdates'
import { assignableTags, archiveConfirmMessage, isArchivedTask, isProtectedTag } from '@/utils/tags'

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
  notifySaved,
  notifyDeleted,
  openEdit,
  openView,
} = useTaskSidebar()
const toast = useToast()
const { askConfirm } = useConfirm()
const { user } = useAuth()
const { enabled: imageEnabled, uploadImageFile } = useImageUpload()
const router = useRouter()
const showTaskNumber = computed(() => (mode.value === 'edit' || mode.value === 'view') && !!taskId.value)

async function copyPermalink() {
  if (!taskId.value) return
  const href = router.resolve({ name: 'task', params: { id: String(taskId.value) } }).href
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
const events = ref<TaskEvent[]>([])
const eventsLoaded = ref(false)
const eventsLoading = ref(false)
const descriptionError = ref('')
const statuses = ref<ProjectStatus[]>([])
const sprints = ref<ProjectSprint[]>([])
const timeEntries = ref<TaskTimeEntry[]>([])
const timeSpentMinutes = ref(0)
const newEntryMinutes = ref<number | ''>('')
const newEntryNote = ref('')
const addingTime = ref(false)
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
const selectedTagIds = ref<number[]>([])
const taskTags = ref<Tag[]>([])
const newTags = ref('')
const completed = ref(false)
const statusId = ref<number | ''>('')
const sprintId = ref<number | ''>('')
const estimatePoints = ref<number | ''>('')
const claimedBy = ref<number | null>(null)
const claimedByName = ref('')
const claiming = ref(false)
const githubIssue = ref<TaskGitHubIssue | null>(null)
const projectHasGitHub = ref(false)
const githubIssueRef = ref('')
const githubBusy = ref(false)
const isSubtask = computed(() => parentId.value !== '' && Number(parentId.value) > 0)
const selectedProject = computed(() => {
  if (projectId.value === '') return null
  return projects.value.find((p) => p.id === Number(projectId.value)) ?? null
})
const assignableProjects = computed(() =>
  projects.value.filter((p) => !isArchivedProject(p) || p.id === Number(projectId.value)),
)
/** Viewers may open via edit entry points; treat their project role as read-only. */
const readOnly = computed(() => {
  if (mode.value === 'view') return true
  if (mode.value === 'edit' && selectedProject.value?.role === 'viewer') return true
  return false
})
const canManageTags = computed(() => {
  if (readOnly.value) return false
  const role = selectedProject.value?.role
  if (!role) return true
  return role === 'owner' || role === 'editor'
})
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
const isKanbanTask = computed(() => {
  if (taskWorkflow.value === 'kanban') return true
  return (selectedProject.value?.workflow_mode || 'classic') === 'kanban'
})
const sidebarTitle = computed(() => {
  if (readOnly.value) return 'View Task'
  if (mode.value === 'edit') return 'Edit Task'
  return 'Add Task'
})
const kanbanHeaderTitle = computed(() => (mode.value === 'add' ? 'Add Task' : 'Task'))
const submitText = computed(() => (mode.value === 'edit' ? 'Save Task' : 'Add Task'))
const charCount = computed(() => description.value.length)
const editingDescription = ref(false)
const showDescriptionEditor = computed(() => {
  if (readOnly.value) return false
  if (editingDescription.value) return true
  if (mode.value === 'add' && !description.value.trim()) return true
  return false
})
const timeSpentLabel = computed(() => formatMinutes(timeSpentMinutes.value))
const showDiscussion = computed(
  () => (mode.value === 'edit' || mode.value === 'view') && !!currentTask.value?.project_id,
)
const discussionIsOwner = computed(() => selectedProject.value?.role === 'owner')
const discussionRef = ref<{ reload: () => Promise<void> } | null>(null)
const claimerLabel = computed(() => {
  if (!claimedBy.value) return 'Unclaimed'
  if (user.value?.id && claimedBy.value === user.value.id) return 'You'
  return claimedByName.value || `User #${claimedBy.value}`
})

function formatMinutes(total: number) {
  if (total <= 0) return '0m'
  const h = Math.floor(total / 60)
  const m = total % 60
  if (h <= 0) return `${m}m`
  if (m <= 0) return `${h}h`
  return `${h}h ${m}m`
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
    favorite: false,
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
  if (next.body.length > 1000) {
    toast.push('Description would exceed 1000 characters', 'error')
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
  close()
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
  selectedTagIds.value = []
  taskTags.value = []
  newTags.value = ''
  completed.value = false
  descriptionError.value = ''
  events.value = []
  eventsLoaded.value = false
  statusId.value = ''
  sprintId.value = ''
  estimatePoints.value = ''
  claimedBy.value = null
  claimedByName.value = ''
  statuses.value = []
  sprints.value = []
  timeEntries.value = []
  timeSpentMinutes.value = 0
  newEntryMinutes.value = ''
  newEntryNote.value = ''
  taskWorkflow.value = ''
  githubIssue.value = null
  projectHasGitHub.value = false
  githubIssueRef.value = ''
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

async function loadTimeEntries(id: number) {
  try {
    const entries = await api.listTimeEntries(id)
    timeEntries.value = entries
    timeSpentMinutes.value = entries.reduce((sum, e) => sum + e.minutes, 0)
  } catch {
    timeEntries.value = []
  }
}

async function refreshProjectGitHub(pid: number | '') {
  projectHasGitHub.value = false
  if (pid === '' || pid == null) return
  try {
    const link = await api.getProjectGitHub(Number(pid))
    projectHasGitHub.value = !!link.linked
  } catch {
    projectHasGitHub.value = false
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
  taskTags.value = task.tags ? [...task.tags] : []
  selectedTagIds.value = taskTags.value.map((t) => t.id)
  newTags.value = ''
  completed.value = task.completed
  descriptionError.value = ''
  events.value = []
  eventsLoaded.value = false
  statusId.value = task.status_id ?? ''
  sprintId.value = formSprintId(task.sprint_id)
  estimatePoints.value = task.estimate_points ?? ''
  claimedBy.value = task.claimed_by ?? null
  claimedByName.value = task.claimed_by_name || ''
  timeSpentMinutes.value = task.time_spent_minutes ?? 0
  taskWorkflow.value = task.project_workflow || ''
  githubIssue.value = task.github ?? null
  githubIssueRef.value = ''
  await loadStatusesForProject(projectId.value)
  await loadSprintsForProject(projectId.value)
  await loadTagsForProject(projectId.value)
  await refreshProjectGitHub(projectId.value)
  if (taskWorkflow.value === 'kanban' || isKanbanTask.value) {
    await loadTimeEntries(id)
  } else {
    timeEntries.value = []
  }
}

function sameIdSet(a: number[], b: number[]) {
  if (a.length !== b.length) return false
  const seen = new Set(a)
  return b.every((id) => seen.has(id))
}

function isFormDirty(): boolean {
  if (mode.value !== 'edit' || !currentTask.value) return false
  const t = currentTask.value
  const project = t.project_id ?? ''
  const parent = t.parent_id ?? ''
  const due = t.due_date || ''
  const status = t.status_id ?? ''
  const sprint = formSprintId(t.sprint_id)
  const estimate = t.estimate_points ?? ''
  const tagIds = (t.tags || []).map((x) => x.id)
  return (
    title.value !== t.title ||
    (description.value || '') !== (t.description || '') ||
    projectId.value !== project ||
    parentId.value !== parent ||
    priority.value !== t.priority ||
    dueDate.value !== due ||
    completed.value !== t.completed ||
    statusId.value !== status ||
    formSprintId(sprintId.value) !== sprint ||
    estimatePoints.value !== estimate ||
    newTags.value.trim() !== '' ||
    !sameIdSet(selectedTagIds.value, tagIds)
  )
}

useLiveUpdates(async (event: LiveEvent) => {
  if (!open.value || !taskId.value) return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  if (event.type === 'task.commented') {
    if (!event.task_id || event.task_id === taskId.value) {
      await discussionRef.value?.reload()
    }
    return
  }
  if (event.type === 'project.updated') {
    if (currentTask.value?.project_id && event.project_id === currentTask.value.project_id) {
      await loadMeta()
      await loadTagsForProject(projectId.value)
      await loadStatusesForProject(projectId.value)
      await loadSprintsForProject(projectId.value)
    }
    return
  }
  if (event.task_id && event.task_id !== taskId.value) return
  if (event.type === 'task.deleted') {
    toast.push('This task was deleted in another session', 'info')
    close()
    return
  }
  if (mode.value === 'add') return
  if (isFormDirty()) {
    toast.push('This task was updated in another session. Save or discard to avoid overwriting.', 'info')
    return
  }
  try {
    await loadTask(taskId.value)
    if (eventsLoaded.value) await loadEvents(true)
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
  if (description.value.length > 1000) {
    descriptionError.value = 'Description must be 1000 characters or fewer.'
    return false
  }
  descriptionError.value = ''
  return true
}

async function save(keepOpen = false) {
  if (readOnly.value) return
  if (!title.value.trim()) return
  if (!validateDescription()) return
  if (mode.value === 'add' && selectedProject.value && isArchivedProject(selectedProject.value)) {
    toast.push('Cannot add tasks to an archived project', 'error')
    return
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
        tag_ids: tagIds,
        ...(isKanbanTask.value && statusId.value !== ''
          ? { status_id: Number(statusId.value) }
          : {}),
        ...(isKanbanTask.value ? { sprint_id: sprintPayloadId(sprintId.value) } : {}),
        ...(isKanbanTask.value && estimatePoints.value !== ''
          ? { estimate_points: Number(estimatePoints.value) }
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
      } else {
        resetForm()
      }
      return
    }
    if (!taskId.value) return
    const payload: Parameters<typeof api.patchTask>[1] = {
      title: title.value.trim(),
      description: description.value,
      priority: priority.value,
      tag_ids: tagIds,
      // 0 clears parent (promotes to root); JSON null is indistinguishable from omitted for **int.
      parent_id: isSubtask.value ? Number(parentId.value) : 0,
    }
    if (!isSubtask.value) {
      // 0 clears project (inbox); same convention as parent_id.
      payload.project_id = projectId.value === '' ? 0 : Number(projectId.value)
    }
    if (dueDate.value) payload.due_date = dueDate.value
    else payload.clear_due_date = true
    if (isKanbanTask.value) {
      if (statusId.value !== '') payload.status_id = Number(statusId.value)
      payload.sprint_id = sprintPayloadId(sprintId.value)
      payload.estimate_points =
        estimatePoints.value === '' ? null : Number(estimatePoints.value)
    }
    const updated = await api.patchTask(taskId.value, payload)
    notifySaved(updated, true)
    toast.push('Task saved', 'success')
    editingDescription.value = false
    if (eventsLoaded.value) await loadEvents(true)
  } catch (err) {
    const msg = err instanceof APIError ? err.message : err instanceof Error ? err.message : 'Save failed'
    toast.push(msg, 'error')
  } finally {
    saving.value = false
  }
}

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

async function loadEvents(force = false) {
  if (!taskId.value || eventsLoading.value) return
  if (eventsLoaded.value && !force) return
  eventsLoading.value = true
  try {
    events.value = await api.listTaskEvents(taskId.value)
    eventsLoaded.value = true
  } catch {
    events.value = []
  } finally {
    eventsLoading.value = false
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
  await refreshProjectGitHub(projectId.value)
  await loadTagsForProject(projectId.value)
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
        await refreshProjectGitHub(projectId.value)
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
      titleInput.value?.focus()
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
  await refreshProjectGitHub(projectId.value)
}

function openRelated(id: number) {
  if (mode.value === 'view') openView(id)
  else openEdit(id)
}

async function claimCurrentTask() {
  if (!taskId.value || readOnly.value) return
  claiming.value = true
  try {
    const task = await api.claimTask(taskId.value)
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
  if (!taskId.value || readOnly.value) return
  claiming.value = true
  try {
    const task = await api.unclaimTask(taskId.value)
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

async function createGitHubIssue() {
  if (!taskId.value || readOnly.value) return
  githubBusy.value = true
  try {
    githubIssue.value = await api.createTaskGitHubIssue(taskId.value)
    toast.push(`Created GitHub issue #${githubIssue.value.issue_number}`, 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create GitHub issue', 'error')
  } finally {
    githubBusy.value = false
  }
}

async function linkGitHubIssue() {
  if (!taskId.value || readOnly.value || !githubIssueRef.value.trim()) return
  githubBusy.value = true
  try {
    githubIssue.value = await api.linkTaskGitHubIssue(taskId.value, githubIssueRef.value.trim())
    githubIssueRef.value = ''
    toast.push(`Linked GitHub issue #${githubIssue.value.issue_number}`, 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not link GitHub issue', 'error')
  } finally {
    githubBusy.value = false
  }
}

async function unlinkGitHubIssue() {
  if (!taskId.value || readOnly.value) return
  const ok = await askConfirm({
    title: 'Unlink GitHub issue?',
    message: 'The GitHub issue itself is not deleted.',
    confirmLabel: 'Unlink',
    danger: true,
  })
  if (!ok) return
  githubBusy.value = true
  try {
    await api.unlinkTaskGitHubIssue(taskId.value)
    githubIssue.value = null
    toast.push('GitHub issue unlinked', 'info')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not unlink GitHub issue', 'error')
  } finally {
    githubBusy.value = false
  }
}

async function archiveCurrentTask() {
  if (!taskId.value || readOnly.value || !currentTask.value) return
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
  if (!taskId.value || readOnly.value) return
  try {
    const updated = await api.restoreTask(taskId.value)
    toast.push('Task restored', 'success')
    notifySaved(updated, true)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Restore failed', 'error')
  }
}

async function deleteCurrentTask() {
  if (!taskId.value || readOnly.value || !currentTask.value) return
  const childCount = currentTask.value.child_count ?? currentTask.value.children?.length ?? 0
  if (childCount > 0) {
    deleteDialogOpen.value = true
    return
  }
  const ok = await askConfirm({
    title: 'Delete task?',
    message: `Permanently delete “${currentTask.value.title}”? This cannot be undone.`,
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
    toast.push('Task deleted', 'info')
    deleteDialogOpen.value = false
    notifyDeleted(deletedId, opts.mode)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

async function addTimeEntry() {
  if (!taskId.value || readOnly.value) return
  const minutes = Number(newEntryMinutes.value)
  if (!Number.isFinite(minutes) || minutes <= 0) {
    toast.push('Enter minutes greater than 0', 'error')
    return
  }
  addingTime.value = true
  try {
    await api.addTimeEntry(taskId.value, minutes, newEntryNote.value.trim())
    newEntryMinutes.value = ''
    newEntryNote.value = ''
    toast.push('Time logged', 'success')
    await loadTimeEntries(taskId.value)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not add time entry', 'error')
  } finally {
    addingTime.value = false
  }
}

async function removeTimeEntry(entryId: number) {
  if (!taskId.value || readOnly.value) return
  const ok = await askConfirm({
    title: 'Delete time entry?',
    message: 'Remove this time entry?',
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTimeEntry(taskId.value, entryId)
    toast.push('Time entry deleted', 'info')
    await loadTimeEntries(taskId.value)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete time entry', 'error')
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
      class="modal-dialog oryryn-task-dialog"
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
                <span v-if="showTaskNumber" class="text-muted fw-normal">#{{ taskId }}</span>
              </h5>
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
            </div>
            <div class="task-header-actions">
              <button
                v-if="!readOnly && !loading && mode === 'edit' && (!claimedBy || claimedBy !== user?.id)"
                type="button"
                class="btn btn-sm btn-outline-primary task-header-btn"
                :disabled="claiming"
                @click="claimCurrentTask"
              >
                {{ claimedBy ? 'Take over' : 'Claim' }}
              </button>
              <button
                v-else-if="!readOnly && !loading && mode === 'edit'"
                type="button"
                class="btn btn-sm btn-outline-secondary task-header-btn"
                :disabled="claiming"
                @click="unclaimCurrentTask"
              >
                Release
              </button>
              <button
                v-if="!readOnly && !loading && mode === 'edit'"
                type="button"
                class="btn btn-sm task-header-btn"
                :class="taskIsArchived ? 'btn-success' : 'btn-warning'"
                :disabled="saving"
                @click="taskIsArchived ? restoreCurrentTask() : archiveCurrentTask()"
              >
                {{ taskIsArchived ? 'Restore' : 'Archive' }}
              </button>
              <button
                v-if="!readOnly && !loading && mode === 'edit'"
                type="button"
                class="btn btn-sm btn-danger task-header-btn"
                :disabled="saving"
                @click="deleteCurrentTask"
              >
                Delete
              </button>
              <button
                v-if="!readOnly && !loading"
                type="button"
                class="btn btn-sm btn-primary task-header-btn"
                :disabled="saving"
                @click="save(false)"
              >
                {{ saving ? 'Saving…' : submitText }}
              </button>
              <button
                v-if="!readOnly && !loading && mode === 'add'"
                type="button"
                class="btn btn-sm btn-outline-primary task-header-btn"
                :disabled="saving || !title.trim()"
                @click="save(true)"
              >
                Save &amp; Add Another
              </button>
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary task-header-btn task-header-close"
                id="closeSidebar"
                aria-label="Close"
                @click="close"
              >
                <i class="bi bi-x-lg" />
              </button>
            </div>
          </template>
          <template v-else>
            <h5 class="modal-title fw-bold d-flex align-items-center gap-2 flex-wrap">
              {{ sidebarTitle }}
              <span v-if="showTaskNumber" class="text-muted fw-normal">#{{ taskId }}</span>
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
            </h5>
            <button type="button" class="btn-close" id="closeSidebar" aria-label="Close" @click="close" />
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
            :maxlength="1000"
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
              <small class="text-muted"><span id="char-count">{{ charCount }}</span>/1000</small>
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
        <div class="form-group mt-2 kanban-order-parent">
          <label for="parent_id">Parent task (optional):</label>
          <ParentTaskCombobox
            v-model="parentId"
            input-id="parent_id"
            :options="parentOptions"
            :disabled="parentPickerDisabled"
            placeholder="Type to search parent tasks…"
            @change="onParentChange"
          />
          <small v-if="isSubtask" class="form-hint d-block mt-1">
            Subtask of {{ parentTitle || 'selected parent' }}. Project is inherited.
          </small>
        </div>
        <div
          v-if="(mode === 'edit' || mode === 'view') && (relatedParent || relatedChildren.length)"
          class="form-group mt-2 kanban-order-related"
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
        <div class="form-group mt-2 kanban-order-project">
          <label for="project_id">Project (optional):</label>
          <select
            id="project_id"
            v-model="projectId"
            class="form-select"
            :disabled="readOnly || isSubtask || projectLockedToOwner"
            @change="onProjectChange"
          >
            <option value="">No project</option>
            <option v-for="p in assignableProjects" :key="p.id" :value="p.id" :disabled="isArchivedProject(p)">{{ projectOptionLabel(p) }}</option>
          </select>
          <small v-if="projectLockedToOwner" class="form-hint d-block mt-1">
            Only the project owner can move this task to another project.
          </small>
          <small v-if="mode === 'add' && selectedProject && isArchivedProject(selectedProject)" class="text-warning">
            This project is archived, so new tasks cannot be added.
          </small>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-order-status">
          <label for="status_id">Status:</label>
          <select id="status_id" v-model="statusId" class="form-select" :disabled="readOnly">
            <option v-if="!statuses.length" value="">No statuses</option>
            <option v-for="s in statuses" :key="s.id" :value="s.id">
              {{ s.name }}{{ s.is_done ? ' (done)' : '' }}{{ s.is_default ? ' (default)' : '' }}
            </option>
          </select>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-order-sprint">
          <label for="sprint_id">Sprint:</label>
          <select
            id="sprint_id"
            class="form-select"
            :disabled="readOnly"
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
        <div v-if="isKanbanTask && (mode === 'edit' || mode === 'view')" class="form-group mt-2 kanban-order-claim">
          <label class="d-block">Claimed by</label>
          <div class="d-flex align-items-center gap-2 flex-wrap">
            <span class="badge border" :class="claimedBy ? 'text-bg-primary' : 'text-bg-light text-muted'">
              <i class="bi bi-person me-1" />{{ claimerLabel }}
            </span>
          </div>
        </div>
        <div v-if="isKanbanTask" class="form-group mt-2 kanban-order-estimate">
          <label for="estimate_points">Estimate (points):</label>
          <input
            id="estimate_points"
            v-model="estimatePoints"
            type="number"
            class="form-control"
            min="0"
            max="100"
            step="1"
            :disabled="readOnly"
            :readonly="readOnly"
            placeholder="0–100"
          />
        </div>
        <div
          v-if="(mode === 'edit' || mode === 'view') && projectHasGitHub"
          class="form-group mt-3 kanban-order-github"
        >
          <label class="d-block">GitHub issue</label>
          <div v-if="githubIssue" class="d-flex flex-column gap-2">
            <div class="d-flex flex-wrap align-items-center gap-2">
              <a
                :href="githubIssue.issue_url"
                target="_blank"
                rel="noopener noreferrer"
                class="fw-semibold"
              >
                #{{ githubIssue.issue_number }}
                <span v-if="githubIssue.issue_title"> — {{ githubIssue.issue_title }}</span>
              </a>
              <span
                class="badge"
                :class="githubIssue.issue_state === 'closed' ? 'text-bg-secondary' : 'text-bg-success'"
              >
                {{ githubIssue.issue_state }}
              </span>
            </div>
            <p v-if="githubIssue.last_sync_error" class="text-danger small mb-0">
              Sync error: {{ githubIssue.last_sync_error }}
            </p>
            <button
              v-if="!readOnly"
              type="button"
              class="btn btn-sm btn-outline-secondary align-self-start"
              :disabled="githubBusy"
              @click="unlinkGitHubIssue"
            >
              Unlink
            </button>
          </div>
          <div v-else-if="!readOnly" class="d-flex flex-column gap-2">
            <button
              type="button"
              class="btn btn-sm btn-outline-primary align-self-start"
              :disabled="githubBusy"
              @click="createGitHubIssue"
            >
              {{ githubBusy ? 'Working…' : 'Create GitHub issue' }}
            </button>
            <div class="input-group input-group-sm">
              <input
                v-model="githubIssueRef"
                type="text"
                class="form-control"
                placeholder="Issue # or URL"
                :disabled="githubBusy"
              />
              <button
                type="button"
                class="btn btn-outline-secondary"
                :disabled="githubBusy || !githubIssueRef.trim()"
                @click="linkGitHubIssue"
              >
                Link existing
              </button>
            </div>
            <div class="form-hint mb-0">
              Creates or links an issue in the project’s GitHub repository. Does not import issues as tasks.
            </div>
          </div>
          <p v-else class="text-muted small mb-0">No linked GitHub issue.</p>
        </div>
        <div class="form-group mt-2 kanban-order-priority">
          <label for="priority">Priority:</label>
          <select id="priority" v-model.number="priority" class="form-select" :disabled="readOnly">
            <option :value="0">None</option>
            <option :value="1">Low</option>
            <option :value="2">Medium</option>
            <option :value="3">High</option>
          </select>
        </div>
        <div class="form-group mt-2 kanban-order-due">
          <label for="due_date">Due Date (optional):</label>
          <input id="due_date" v-model="dueDate" type="date" class="form-control" :disabled="readOnly" :readonly="readOnly" />
          <div v-if="!readOnly" class="btn-group btn-group-sm mt-1 flex-wrap" role="group" aria-label="Due date presets">
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('today')">Today</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('tomorrow')">Tomorrow</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('week')">+1 week</button>
            <button type="button" class="btn btn-outline-secondary" @click="applyDuePreset('clear')">Clear</button>
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

        <div
          v-if="isKanbanTask && (mode === 'edit' || mode === 'view')"
          class="form-group mt-3 border-top pt-3 kanban-order-time"
        >
          <label class="d-block">Time tracking</label>
          <p class="small text-muted mb-2">
            Total logged: <strong>{{ timeSpentLabel }}</strong>
            <span v-if="timeSpentMinutes">({{ timeSpentMinutes }} min)</span>
          </p>
          <form v-if="!readOnly" class="row g-2 align-items-end mb-2" @submit.prevent="addTimeEntry">
            <div class="col-4">
              <label class="form-label small mb-0" for="time_minutes">Minutes</label>
              <input
                id="time_minutes"
                v-model="newEntryMinutes"
                type="number"
                class="form-control form-control-sm"
                min="1"
                max="1440"
                required
              />
            </div>
            <div class="col-5">
              <label class="form-label small mb-0" for="time_note">Note</label>
              <input
                id="time_note"
                v-model="newEntryNote"
                type="text"
                class="form-control form-control-sm"
                maxlength="200"
                placeholder="Optional"
              />
            </div>
            <div class="col-3">
              <button class="btn btn-sm btn-outline-primary w-100" type="submit" :disabled="addingTime">
                Add
              </button>
            </div>
          </form>
          <ul v-if="timeEntries.length" class="list-unstyled small mb-0">
            <li
              v-for="entry in timeEntries"
              :key="entry.id"
              class="d-flex justify-content-between align-items-start gap-2 mb-1"
            >
              <span>
                <strong>{{ entry.minutes }}m</strong>
                <span v-if="entry.note"> — {{ entry.note }}</span>
                <span class="text-muted"> · {{ entry.user_name || entry.user_email || 'user' }}</span>
              </span>
              <button
                v-if="!readOnly"
                class="btn btn-sm btn-link text-danger p-0"
                type="button"
                @click="removeTimeEntry(entry.id)"
              >
                Delete
              </button>
            </li>
          </ul>
          <p v-else class="small text-muted mb-0">No time entries yet.</p>
        </div>
        </div>

        <div :class="{ 'kanban-task-rest': isKanbanTask }">
        <TaskDiscussion
          v-if="showDiscussion && taskId"
          ref="discussionRef"
          :task-id="taskId"
          :project-id="currentTask?.project_id ?? null"
          :current-user-id="user?.id ?? null"
          :is-owner="discussionIsOwner"
          :fill-height="isKanbanTask"
        />

        <div v-if="!readOnly && !isKanbanTask" class="task-header-actions mt-3">
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
            v-if="mode === 'edit'"
            type="button"
            class="btn btn-sm task-header-btn"
            :class="taskIsArchived ? 'btn-success' : 'btn-warning'"
            :disabled="saving"
            @click="taskIsArchived ? restoreCurrentTask() : archiveCurrentTask()"
          >
            {{ taskIsArchived ? 'Restore' : 'Archive' }}
          </button>
          <button
            v-if="mode === 'edit'"
            type="button"
            class="btn btn-sm btn-danger task-header-btn"
            :disabled="saving"
            @click="deleteCurrentTask"
          >
            Delete
          </button>
        </div>

        <details
          v-if="mode === 'edit' || mode === 'view'"
          class="task-timeline mt-3"
          @toggle="(e) => { if ((e.target as HTMLDetailsElement).open) void loadEvents() }"
        >
          <summary class="mb-2">Activity</summary>
          <div class="task-timeline-body">
            <p v-if="eventsLoading" class="text-muted small mb-0">Loading activity…</p>
            <ul v-else-if="events.length" class="list-unstyled small mb-0">
              <li v-for="ev in events" :key="ev.id" class="mb-1">
                <span class="text-muted">{{ ev.created_at }}</span> — {{ ev.label }}
                <span v-if="ev.actor_user_name || ev.actor_email">
                  · {{ ev.actor_user_name || ev.actor_email }}
                </span>
              </li>
            </ul>
            <p v-else class="text-muted small mb-0">No activity recorded.</p>
          </div>
        </details>
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
</template>

<style scoped>
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
  grid-template-columns: minmax(0, 1fr) 340px;
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

.kanban-task-aside .kanban-order-status,
.kanban-task-aside :deep(.kanban-order-status) { order: 1; }
.kanban-task-aside .kanban-order-sprint,
.kanban-task-aside :deep(.kanban-order-sprint) { order: 2; }
.kanban-task-aside .kanban-order-claim,
.kanban-task-aside :deep(.kanban-order-claim) { order: 3; }
.kanban-task-aside .kanban-order-project,
.kanban-task-aside :deep(.kanban-order-project) { order: 4; }
.kanban-task-aside .kanban-order-parent,
.kanban-task-aside :deep(.kanban-order-parent) { order: 5; }
.kanban-task-aside .kanban-order-related,
.kanban-task-aside :deep(.kanban-order-related) { order: 6; }
.kanban-task-aside .kanban-order-priority,
.kanban-task-aside :deep(.kanban-order-priority) { order: 7; }
.kanban-task-aside .kanban-order-estimate,
.kanban-task-aside :deep(.kanban-order-estimate) { order: 8; }
.kanban-task-aside .kanban-order-due,
.kanban-task-aside :deep(.kanban-order-due) { order: 9; }
.kanban-task-aside .kanban-order-tags,
.kanban-task-aside :deep(.kanban-order-tags) { order: 10; }
.kanban-task-aside .kanban-order-github,
.kanban-task-aside :deep(.kanban-order-github) { order: 11; }
.kanban-task-aside .kanban-order-time,
.kanban-task-aside :deep(.kanban-order-time) { order: 12; }

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
  .oryryn-task-dialog:not(.kanban-task-dialog) {
    width: 100vw;
    max-width: 100vw;
    height: 100dvh;
    max-height: 100dvh;
    margin: 0;
  }

  .oryryn-task-dialog:not(.kanban-task-dialog) .modal-content {
    min-height: 100dvh;
    border-radius: 0;
  }

  .kanban-task-body {
    overflow-y: auto;
  }

  .kanban-task-form {
    display: flex;
    flex-direction: column;
    height: auto;
  }

  .kanban-task-head,
  .kanban-task-rest,
  .kanban-task-aside {
    overflow: visible;
  }

  .kanban-task-head { order: 1; }
  .kanban-task-rest { order: 2; }
  .kanban-task-aside {
    order: 3;
    border-left: none;
    border-top: 1px solid var(--ordryn-card-border, #dee2e6);
  }
}
</style>
