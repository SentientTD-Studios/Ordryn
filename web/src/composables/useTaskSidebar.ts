import { ref } from 'vue'
import type { Task } from '@/api/types'

const open = ref(false)
const mode = ref<'add' | 'edit'>('add')
const taskId = ref<number | null>(null)
const lastSavedTask = ref<Task | null>(null)

export function useTaskSidebar() {
  function openAdd() {
    mode.value = 'add'
    taskId.value = null
    open.value = true
  }

  function openEdit(id: number) {
    mode.value = 'edit'
    taskId.value = id
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
    lastSavedTask,
    openAdd,
    openEdit,
    close,
    notifySaved,
  }
}
