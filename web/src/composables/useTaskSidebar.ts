import { ref } from 'vue'
import type { Task } from '@/api/types'

const open = ref(false)
const mode = ref<'add' | 'edit'>('add')
const taskId = ref<number | null>(null)
const defaultDueDate = ref('')
const lastSavedTask = ref<Task | null>(null)

export function useTaskSidebar() {
  function openAdd(dueDate?: string) {
    mode.value = 'add'
    taskId.value = null
    // Ignore PointerEvent when bound as @click="openAdd"
    defaultDueDate.value = typeof dueDate === 'string' ? dueDate.trim() : ''
    open.value = true
  }

  function openEdit(id: number) {
    mode.value = 'edit'
    taskId.value = id
    defaultDueDate.value = ''
    open.value = true
  }

  function close() {
    open.value = false
  }

  function notifySaved(task: Task) {
    lastSavedTask.value = task
    close()
  }

  return {
    open,
    mode,
    taskId,
    defaultDueDate,
    lastSavedTask,
    openAdd,
    openEdit,
    close,
    notifySaved,
  }
}
