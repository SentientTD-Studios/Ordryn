<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Task } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const total = ref(0)
const title = ref('')
const projectId = ref<number | ''>('')
const search = ref('')
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
  for (const key of ['status', 'due', 'completed', 'priority', 'tag', 'sort', 'project']) {
    const v = q[key]
    if (typeof v === 'string' && v) params[key] = v
  }
  const qSearch = q.search
  if (typeof qSearch === 'string' && qSearch) params.search = qSearch
  if (!params.status && !params.completed) params.status = 'incomplete'
  return params
})

const favoriteTasks = computed(() => tasks.value.filter((t) => t.favorite))
const regularTasks = computed(() => tasks.value.filter((t) => !t.favorite))
const completedCount = computed(() => tasks.value.filter((t) => t.completed).length)
const incompleteCount = computed(() => tasks.value.filter((t) => !t.completed).length)
const allSelected = computed(
  () => tasks.value.length > 0 && selected.value.length === tasks.value.length,
)
const hasFilters = computed(() => Object.keys(route.query).length > 0)

async function load() {
  loading.value = true
  selected.value = []
  try {
    const [list, projs] = await Promise.all([api.listTasks(filterParams.value), api.listProjects()])
    tasks.value = list.tasks
    total.value = list.total
    projects.value = projs
    search.value = typeof route.query.search === 'string' ? route.query.search : ''
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

async function toggleFavorite(task: Task) {
  try {
    await api.patchTask(task.id, { favorite: !task.favorite })
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeTask(task: Task) {
  if (!confirm(`Delete "${task.title}"?`)) return
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

function applySearch() {
  const query = { ...route.query }
  if (search.value.trim()) query.search = search.value.trim()
  else delete query.search
  void router.push({ name: 'tasks', query })
}

function setFilter(key: string, value: string) {
  const query = { ...route.query }
  if (value) query[key] = value
  else delete query[key]
  void router.push({ name: 'tasks', query })
}

function clearFilters() {
  void router.push({ name: 'tasks' })
}

function priorityBadgeClass(priority: number) {
  if (priority === 1) return 'bg-secondary'
  if (priority === 2) return 'bg-warning text-dark'
  if (priority === 3) return 'bg-danger'
  return ''
}

function priorityLabel(priority: number) {
  if (priority === 1) return 'Low'
  if (priority === 2) return 'Med'
  if (priority === 3) return 'High'
  return ''
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
  <div class="container mt-3">
    <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
      <form class="d-flex flex-grow-1 gap-2" @submit.prevent="applySearch">
        <input
          v-model="search"
          type="search"
          class="form-control search-input"
          placeholder="Search tasks…"
          aria-label="Search tasks"
        />
        <button type="submit" class="btn btn-outline-primary">
          <i class="bi bi-search"></i>
        </button>
      </form>
      <button type="button" class="btn btn-success" data-bs-toggle="modal" data-bs-target="#addTaskModal">
        <i class="bi bi-plus-lg"></i> Add Task
      </button>
      <button v-if="undoToken" type="button" class="btn btn-outline-secondary btn-sm" @click="undoDelete">
        Undo delete
      </button>
    </div>

    <div class="d-flex flex-wrap gap-2 mb-3">
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="setFilter('due', '')">All due</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="setFilter('due', 'today')">Today</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="setFilter('due', 'overdue')">Overdue</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="setFilter('due', 'week')">This week</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="setFilter('due', 'none')">No date</button>
      <button v-if="hasFilters" type="button" class="btn btn-sm btn-outline-primary" @click="clearFilters">
        <i class="bi bi-x-circle"></i> Clear filters
      </button>
    </div>

    <div v-if="selected.length" class="alert alert-info d-flex flex-wrap gap-2 align-items-center">
      <span>{{ selected.length }} selected</span>
      <button type="button" class="btn btn-sm btn-outline-primary" @click="bulk('complete')">Complete</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="bulk('incomplete')">Incomplete</button>
      <button type="button" class="btn btn-sm btn-outline-danger" @click="bulk('delete')">Delete</button>
    </div>

    <p v-if="loading" class="text-muted">Loading…</p>

    <div v-else-if="tasks.length || hasFilters" class="row justify-content-center mb-5">
      <div>
        <div class="mb-3">
          <span class="badge bg-primary me-2">Tasks: {{ total }}</span>
          <span class="badge bg-success me-2">Completed: {{ completedCount }}</span>
          <span class="badge bg-warning text-dark">Incomplete: {{ incompleteCount }}</span>
          <small class="task-count-hint ms-2">Task count excludes starred items</small>

          <table class="table table-striped table-bordered w-100 mb-3 mt-3">
            <thead>
              <tr>
                <th style="width: 32px">
                  <input
                    type="checkbox"
                    class="form-check-input"
                    :checked="allSelected"
                    aria-label="Select all tasks on this page"
                    @change="toggleSelectAll(($event.target as HTMLInputElement).checked)"
                  />
                </th>
                <th style="width: 32px" />
                <th class="description-column">Title</th>
                <th class="tags-column">Tags</th>
                <th class="description-column">Description</th>
                <th class="date-added">Due Date (if set)</th>
                <th class="actions-column">Status</th>
              </tr>
            </thead>
            <tbody v-if="favoriteTasks.length">
              <tr class="starred-section-label">
                <td colspan="7" class="py-1 px-2 border-0">
                  <small><i class="bi bi-star-fill"></i> Starred tasks (always visible)</small>
                </td>
              </tr>
            </tbody>
            <tbody>
              <tr
                v-for="task in [...favoriteTasks, ...regularTasks]"
                :id="`task-${task.id}`"
                :key="task.id"
                class="task-row"
              >
                <td class="text-center align-middle select-column">
                  <input
                    type="checkbox"
                    class="form-check-input task-select"
                    :checked="selected.includes(task.id)"
                    :aria-label="`Select task ${task.title}`"
                    @change="toggleSelect(task.id, ($event.target as HTMLInputElement).checked)"
                  />
                </td>
                <td class="text-center align-middle drag-column">
                  <span class="drag-handle" style="cursor: move"><i class="bi bi-grip-vertical" /></span>
                </td>
                <td class="title-column">
                  <div class="d-flex align-items-center flex-wrap">
                    <button
                      type="button"
                      class="btn btn-link p-0 me-2 favorite-btn"
                      style="text-decoration: none"
                      :aria-label="task.favorite ? 'Unstar task' : 'Star task'"
                      @click="toggleFavorite(task)"
                    >
                      <i :class="task.favorite ? 'bi bi-star-fill' : 'bi bi-star'" :style="task.favorite ? 'color: gold' : ''" />
                    </button>
                    <RouterLink :to="`/tasks/${task.id}`" class="btn btn-link p-0 task-toggle title-text text-start">
                      {{ task.title }}
                    </RouterLink>
                    <span v-if="task.project" class="badge bg-secondary project-badge ms-1">{{ task.project }}</span>
                    <span
                      v-if="priorityLabel(task.priority)"
                      class="badge priority-badge ms-1"
                      :class="priorityBadgeClass(task.priority)"
                    >{{ priorityLabel(task.priority) }}</span>
                  </div>
                </td>
                <td class="tags-column">
                  <div v-if="task.tags?.length" class="tag-list">
                    <span
                      v-for="tag in task.tags"
                      :key="tag.id"
                      class="tag-chip"
                      :style="{ backgroundColor: tag.color || '#6c757d' }"
                    >{{ tag.name }}</span>
                  </div>
                </td>
                <td class="desc-column">
                  <div v-if="task.description" class="desc-preview text-muted small">
                    {{ task.description.length > 120 ? `${task.description.slice(0, 120)}…` : task.description }}
                  </div>
                </td>
                <td class="date-added" :data-label="'Due Date'">
                  {{ task.due_date || '' }}
                </td>
                <td class="actions-column">
                  <div class="d-flex align-items-center gap-2 justify-content-start">
                    <button
                      type="button"
                      class="badge status-column"
                      :class="task.completed ? 'bg-success' : 'bg-danger text-white'"
                      style="cursor: pointer; border: none"
                      @click="toggleComplete(task)"
                    >
                      <i :class="task.completed ? 'bi bi-toggle-on' : 'bi bi-toggle-off'" />
                      {{ task.completed ? 'Complete' : 'Incomplete' }}
                    </button>
                    <RouterLink
                      :to="`/tasks/${task.id}`"
                      class="btn btn-link p-0 mx-2 edit-btn"
                      style="text-decoration: none"
                      aria-label="Edit task"
                    >
                      <i class="bi bi-pencil" />
                    </RouterLink>
                    <button
                      type="button"
                      class="btn btn-link p-0 delete-column"
                      style="text-decoration: none"
                      aria-label="Delete task"
                      @click="removeTask(task)"
                    >
                      <i class="bi bi-trash text-danger" />
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="!tasks.length">
                <td colspan="7" class="text-center py-4 text-muted">No tasks match this filter.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div v-else class="card text-center empty-state-card">
      <div class="card-body">
        <i class="bi bi-clipboard-check empty-state-icon" aria-hidden="true" />
        <h3 class="mt-3">Add your first Todo!</h3>
        <p class="text-muted">Get started by creating a task.</p>
        <button type="button" class="btn btn-success" data-bs-toggle="modal" data-bs-target="#addTaskModal">
          <i class="bi bi-plus-lg"></i> Add Task
        </button>
      </div>
    </div>
  </div>

  <div id="addTaskModal" class="modal fade" tabindex="-1" aria-hidden="true">
    <div class="modal-dialog">
      <div class="modal-content">
        <form @submit.prevent="createTask">
          <div class="modal-header">
            <h5 class="modal-title">Add Task</h5>
            <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close" />
          </div>
          <div class="modal-body">
            <div class="mb-3">
              <label class="form-label" for="new-task-title">Title</label>
              <input id="new-task-title" v-model="title" type="text" class="form-control" required />
            </div>
            <div class="mb-3">
              <label class="form-label" for="new-task-project">Project</label>
              <select id="new-task-project" v-model="projectId" class="form-select">
                <option value="">No project</option>
                <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
              </select>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
            <button type="submit" class="btn btn-primary" :disabled="busy">Add Task</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
