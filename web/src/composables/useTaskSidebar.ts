import { ref } from 'vue'
import type { Task } from '@/api/types'

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

type CloseGuard = () => boolean | Promise<boolean>
let closeGuard: CloseGuard | null = null

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
    close,
    requestClose,
    setCloseGuard,
    notifySaved,
    notifyDeleted,
  }
}
