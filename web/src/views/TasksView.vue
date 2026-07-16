<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Task } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const title = ref('')
const projectId = ref<number | ''>('')
const loading = ref(true)
const busy = ref(false)
const toast = useToast()
const undoToken = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    const [list, projs] = await Promise.all([
      api.listTasks({ status: 'incomplete', per_page: 50 }),
      api.listProjects(),
    ])
    tasks.value = list.tasks
    projects.value = projs
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load tasks', 'error')
  } finally {
    loading.value = false
  }
}

async function createTask() {
  if (!title.value.trim()) return
  busy.value = true
  try {
    await api.createTask({
      title: title.value.trim(),
      project_id: projectId.value === '' ? null : Number(projectId.value),
    })
    title.value = ''
    toast.push('Task created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  } finally {
    busy.value = false
  }
}

async function toggleComplete(task: Task) {
  try {
    await api.patchTask(task.id, { completed: !task.completed })
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeTask(task: Task) {
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

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Tasks</h1>
        <p class="lede">Incomplete tasks from `/api/v1`.</p>
      </div>
      <button v-if="undoToken" type="button" class="ghost" @click="undoDelete">Undo delete</button>
    </header>

    <form class="composer" @submit.prevent="createTask">
      <input v-model="title" type="text" placeholder="Add a task…" required />
      <select v-model="projectId">
        <option value="">No project</option>
        <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
      <button class="primary" type="submit" :disabled="busy">Add</button>
    </form>

    <p v-if="loading" class="muted">Loading…</p>
    <ul v-else class="task-list">
      <li v-for="task in tasks" :key="task.id" class="task-row">
        <button
          type="button"
          class="check"
          :aria-pressed="task.completed"
          @click="toggleComplete(task)"
        >
          {{ task.completed ? '✓' : '' }}
        </button>
        <div class="task-body">
          <RouterLink :to="`/tasks/${task.id}`" class="task-title">{{ task.title }}</RouterLink>
          <p v-if="task.project || task.due_date" class="meta">
            <span v-if="task.project">{{ task.project }}</span>
            <span v-if="task.due_date">Due {{ task.due_date }}</span>
          </p>
        </div>
        <button type="button" class="ghost danger" @click="removeTask(task)">Delete</button>
      </li>
      <li v-if="!tasks.length" class="muted empty">No incomplete tasks.</li>
    </ul>
  </section>
</template>
