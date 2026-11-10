import { ref } from 'vue'
import type { Task } from '@/api/types'
import { api } from '@/api/client'
import { hasAnyProjectWrite } from '@/utils/projectPerms'

const open = ref(false)
const mode = ref<'add' | 'edit' | 'view'>('add')
const taskId = ref<number | null>(null)
const defaultDueDate = ref('')
const defaultProjectId = ref<number | string | null>(null)
const defaultParentId = ref<number | null>(null)
const defaultParentTitle = ref('')
const defaultSprintId = ref<number | null | undefined>(undefined)
const lastSavedTask = ref<Task | null>(null)
const lastDeletedTask = ref<{ id: number; mode: 'cascade' | 'reparent' } | null>(null)

// One-shot pass for the next navigation to a task URL, which the router
// otherwise cancels in favour of opening the modal in place. Used when the app
// itself moves the URL to /p/{slug}/tasks/{id} for an already-open task.
let taskRoutePass: number | null = null

type CloseGuard = () => boolean | Promise<boolean>
let closeGuard: CloseGuard | null = null

/** Let the next navigation to this task's URL through the router's in-place guard. */
export function allowTaskRouteNavigation(id: number) {
  taskRoutePass = id
}

/** Router guard helper: true (once) when the app itself is moving to this task's URL. */
export function consumeTaskRoutePass(id: number): boolean {
  const ok = taskRoutePass === id
  taskRoutePass = null
  return ok
}

export function useTaskSidebar() {
  function openAdd(
    dueDate?: string,
    projectId?: number | string | null,
    parent?: { id: number; title?: string } | null,
    sprintId?: number | null,
  ) {
    mode.value = 'add'
    taskId.value = null
    // Ignore PointerEvent when bound as @click="openAdd"
    defaultDueDate.value = typeof dueDate === 'string' ? dueDate.trim() : ''
    defaultProjectId.value =
      (typeof projectId === 'number' || typeof projectId === 'string') && projectId !== '0'
        ? projectId
        : null
    defaultParentId.value = parent?.id ?? null
    defaultParentTitle.value = parent?.title?.trim() || ''
    defaultSprintId.value = sprintId === undefined ? undefined : sprintId
    open.value = true
  }

  function openEdit(id: number) {
    mode.value = 'edit'
    taskId.value = id
    defaultDueDate.value = ''
    defaultProjectId.value = null
    defaultParentId.value = null
    defaultParentTitle.value = ''
    defaultSprintId.value = undefined
    open.value = true
  }

  function openView(id: number) {
    mode.value = 'view'
    taskId.value = id
    defaultDueDate.value = ''
    defaultProjectId.value = null
    defaultParentId.value = null
    defaultParentTitle.value = ''
    defaultSprintId.value = undefined
    open.value = true
  }

  /** Open a task by id, in edit mode when the user can write to its project, view mode otherwise. */
  async function openTask(id: number): Promise<Task | null> {
    try {
      const [task, projects] = await Promise.all([api.getTask(id), api.listProjects()])
      const project = task.project_id != null ? projects.find((p) => p.id === task.project_id) : undefined
      if (project && hasAnyProjectWrite(project)) openEdit(id)
      else openView(id)
      return task
    } catch {
      openView(id)
      return null
    }
  }

  function close() {
    open.value = false
  }

  function setCloseGuard(fn: CloseGuard | null) {
    closeGuard = fn
  }

  async function requestClose() {
    if (closeGuard && !(await closeGuard())) return
    close()
  }

  function notifySaved(task: Task, closeDrawer = true) {
    lastSavedTask.value = task
    if (closeDrawer) {
      close()
    }
  }

  function notifyDeleted(id: number, deleteMode: 'cascade' | 'reparent' = 'cascade') {
    lastDeletedTask.value = { id, mode: deleteMode }
    close()
  }

  return {
    open,
    mode,
    taskId,
    defaultDueDate,
    defaultProjectId,
    defaultParentId,
    defaultParentTitle,
    defaultSprintId,
    lastSavedTask,
    lastDeletedTask,
    openAdd,
    openEdit,
    openView,
    openTask,
    close,
    requestClose,
    setCloseGuard,
    notifySaved,
    notifyDeleted,
  }
}
