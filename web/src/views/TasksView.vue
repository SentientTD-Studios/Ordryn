<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
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
const selected = ref<number[]>([])
const route = useRoute()
const router = useRouter()

const filterParams = computed(() => {
  const q = route.query
  const params: Record<string, string | number | undefined> = { per_page: 50 }
  for (const key of ['status', 'due', 'completed', 'priority', 'tag', 'sort', 'search', 'project']) {
    const v = q[key]
    if (typeof v === 'string' && v) params[key] = v
  }
  if (!params.status && !params.completed) params.status = 'incomplete'
  return params
})

const allSelected = computed(
  () => tasks.value.length > 0 && selected.value.length === tasks.value.length,
)

async function load() {
  loading.value = true
  selected.value = []
  try {
    const [list, projs] = await Promise.all([api.listTasks(filterParams.value), api.listProjects()])
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

async function bulk(action: string) {
  if (!selected.value.length) return
  try {
    const res = await api.bulkTasks({ action, task_ids: selected.value })
    if (res.undo_token) undoToken.value = res.undo_token
    toast.push(`Bulk ${action}: ${res.affected ?? selected.value.length}`, 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Bulk action failed', 'error')
  }
}

function clearFilters() {
  void router.push({ name: 'tasks' })
}

watch(
  () => route.query,
  () => {
    void load()
  },
)

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Tasks</h1>
        <p class="lede">Incomplete tasks from `/api/v1`.</p>
      </div>
      <div class="actions">
        <button v-if="Object.keys(route.query).length" type="button" class="ghost" @click="clearFilters">
          Clear filters
        </button>
        <button v-if="undoToken" type="button" class="ghost" @click="undoDelete">Undo delete</button>
      </div>
    </header>

    <form class="composer" @submit.prevent="createTask">
      <input v-model="title" type="text" placeholder="Add a task…" required />
      <select v-model="projectId">
        <option value="">No project</option>
        <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
      <button class="primary" type="submit" :disabled="busy">Add</button>
    </form>

    <div v-if="selected.length" class="bulk-bar">
      <span>{{ selected.length }} selected</span>
      <button type="button" class="ghost" @click="bulk('complete')">Complete</button>
      <button type="button" class="ghost" @click="bulk('incomplete')">Incomplete</button>
      <button type="button" class="ghost danger" @click="bulk('delete')">Delete</button>
    </div>

    <p v-if="loading" class="muted">Loading…</p>
    <ul v-else class="task-list">
      <li v-if="tasks.length" class="row select-all">
        <label class="inline">
          <input
            type="checkbox"
            :checked="allSelected"
            @change="toggleSelectAll(($event.target as HTMLInputElement).checked)"
          />
          Select all
        </label>
      </li>
      <li v-for="task in tasks" :key="task.id" class="task-row">
        <input
          type="checkbox"
          :checked="selected.includes(task.id)"
          @change="toggleSelect(task.id, ($event.target as HTMLInputElement).checked)"
        />
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
      <li v-if="!tasks.length" class="muted empty">No tasks match this view.</li>
    </ul>
  </section>
</template>
