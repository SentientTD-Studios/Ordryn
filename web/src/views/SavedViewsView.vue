<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, SavedView, Tag } from '@/api/types'
import { APIError } from '@/api/types'
import { useTaskListFilters } from '@/composables/useTaskListFilters'
import { useToast } from '@/composables/useToast'

const views = ref<SavedView[]>([])
const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const loading = ref(true)
const saving = ref(false)
const name = ref('')
const status = ref('')
const due = ref('')
const project = ref('')
const priority = ref('')
const tag = ref('')
const sort = ref('')
const search = ref('')
const toast = useToast()
const router = useRouter()
const { applySavedView } = useTaskListFilters()

async function load() {
  loading.value = true
  try {
    const [viewList, projs, tagList] = await Promise.all([
      api.listSavedViews(),
      api.listProjects(),
      api.listTags(),
    ])
    views.value = viewList
    projects.value = projs
    tags.value = tagList
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load saved views', 'error')
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!name.value.trim()) return
  saving.value = true
  try {
    await api.createSavedView({
      name: name.value.trim(),
      filter: {
        status: status.value || undefined,
        due: due.value || undefined,
        project: project.value || undefined,
        priority: priority.value || undefined,
        tag: tag.value || undefined,
        sort: sort.value || undefined,
        search: search.value.trim() || undefined,
      },
    })
    name.value = ''
    search.value = ''
    toast.push('Saved view created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  } finally {
    saving.value = false
  }
}

async function apply(view: SavedView) {
  applySavedView(view.filter || {})
  await router.push({ name: 'tasks' })
}

async function remove(view: SavedView) {
  if (!confirm(`Delete saved view "${view.name}"?`)) return
  try {
    await api.deleteSavedView(view.id)
    toast.push('Deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

function statusLabel(value?: string) {
  if (value === 'incomplete') return 'Incomplete'
  if (value === 'complete') return 'Complete'
  return 'All statuses'
}

function dueLabel(value?: string) {
  if (value === 'today') return 'Today'
  if (value === 'overdue') return 'Overdue'
  if (value === 'week') return 'This week'
  if (value === 'none') return 'No date'
  return ''
}

function priorityLabel(value?: string) {
  if (value === '1') return 'Low'
  if (value === '2') return 'Medium'
  if (value === '3') return 'High'
  return ''
}

function projectLabel(value?: string) {
  if (!value) return ''
  if (value === '0' || value === 'none') return 'No project'
  const pid = parseInt(value, 10)
  const match = projects.value.find((p) => p.id === pid)
  return match?.name || `Project #${value}`
}

function tagLabel(value?: string) {
  if (!value) return ''
  const tid = parseInt(value, 10)
  const match = tags.value.find((t) => t.id === tid)
  return match?.name || `Tag #${value}`
}

function filterBadges(view: SavedView) {
  const f = view.filter || {}
  const badges: { text: string; class: string }[] = []
  if (f.status) badges.push({ text: statusLabel(f.status), class: 'bg-secondary' })
  if (f.due) badges.push({ text: dueLabel(f.due), class: 'bg-info text-dark' })
  if (f.project) badges.push({ text: projectLabel(f.project), class: 'bg-secondary' })
  if (f.priority) badges.push({ text: priorityLabel(f.priority), class: 'bg-warning text-dark' })
  if (f.tag) badges.push({ text: tagLabel(f.tag), class: 'bg-primary' })
  if (f.sort === 'priority') badges.push({ text: 'Sort: Priority', class: 'bg-light text-dark' })
  if (f.search) badges.push({ text: `Search: ${f.search}`, class: 'bg-light text-dark' })
  return badges
}

onMounted(load)
</script>

<template>
  <div class="container mt-3 mb-5">
    <div class="d-flex flex-wrap justify-content-between align-items-start gap-2 mb-4">
      <div>
        <h1 class="h3 mb-1">
          <i class="bi bi-bookmark me-2" aria-hidden="true" />
          Saved views
        </h1>
        <p class="text-muted mb-0">Named filter presets for your task list.</p>
      </div>
      <RouterLink to="/" class="btn btn-outline-secondary btn-sm">
        <i class="bi bi-arrow-left" /> Back to tasks
      </RouterLink>
    </div>

    <div class="card mb-4">
      <div class="card-header">
        <h2 class="h5 mb-0">Create a view</h2>
      </div>
      <div class="card-body">
        <form @submit.prevent="create">
          <div class="row g-3">
            <div class="col-md-6">
              <label class="form-label" for="view-name">Name</label>
              <input
                id="view-name"
                v-model="name"
                type="text"
                class="form-control"
                required
                maxlength="80"
                placeholder="e.g. Overdue work items"
              />
            </div>
            <div class="col-md-6">
              <label class="form-label" for="view-search">Search</label>
              <input
                id="view-search"
                v-model="search"
                type="search"
                class="form-control search-input"
                maxlength="500"
                placeholder="Optional text filter"
              />
            </div>
            <div class="col-sm-6 col-md-4">
              <label class="form-label" for="view-status">Status</label>
              <select id="view-status" v-model="status" class="form-select">
                <option value="">All statuses</option>
                <option value="incomplete">Incomplete</option>
                <option value="complete">Complete</option>
              </select>
            </div>
            <div class="col-sm-6 col-md-4">
              <label class="form-label" for="view-due">Due date</label>
              <select id="view-due" v-model="due" class="form-select">
                <option value="">Any</option>
                <option value="today">Today</option>
                <option value="overdue">Overdue</option>
                <option value="week">This week</option>
                <option value="none">No date</option>
              </select>
            </div>
            <div class="col-sm-6 col-md-4">
              <label class="form-label" for="view-priority">Priority</label>
              <select id="view-priority" v-model="priority" class="form-select">
                <option value="">All priorities</option>
                <option value="1">Low</option>
                <option value="2">Medium</option>
                <option value="3">High</option>
              </select>
            </div>
            <div v-if="projects.length" class="col-sm-6 col-md-4">
              <label class="form-label" for="view-project">Project</label>
              <select id="view-project" v-model="project" class="form-select">
                <option value="">All projects</option>
                <option value="0">No project</option>
                <option v-for="p in projects" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
              </select>
            </div>
            <div v-if="tags.length" class="col-sm-6 col-md-4">
              <label class="form-label" for="view-tag">Tag</label>
              <select id="view-tag" v-model="tag" class="form-select">
                <option value="">All tags</option>
                <option v-for="t in tags" :key="t.id" :value="String(t.id)">{{ t.name }}</option>
              </select>
            </div>
            <div class="col-sm-6 col-md-4">
              <label class="form-label" for="view-sort">Sort</label>
              <select id="view-sort" v-model="sort" class="form-select">
                <option value="">Manual order</option>
                <option value="priority">Priority (high first)</option>
              </select>
            </div>
          </div>
          <div class="mt-3">
            <button type="submit" class="btn btn-primary" :disabled="saving">
              <i class="bi bi-bookmark-plus me-1" />
              {{ saving ? 'Saving…' : 'Save view' }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <div class="card">
      <div class="card-header d-flex justify-content-between align-items-center">
        <h2 class="h5 mb-0">Your views</h2>
        <span v-if="views.length" class="badge bg-secondary">{{ views.length }}</span>
      </div>

      <p v-if="loading" class="card-body text-muted mb-0">Loading…</p>

      <ul v-else-if="views.length" class="list-group list-group-flush">
        <li
          v-for="view in views"
          :key="view.id"
          class="list-group-item d-flex flex-wrap gap-2 align-items-center py-3"
        >
          <div class="flex-grow-1 min-w-0">
            <div class="fw-semibold">
              <i class="bi bi-bookmark-fill me-1 text-warning" aria-hidden="true" />
              {{ view.name }}
            </div>
            <div v-if="filterBadges(view).length" class="d-flex flex-wrap gap-1 mt-2">
              <span
                v-for="(badge, idx) in filterBadges(view)"
                :key="idx"
                class="badge"
                :class="badge.class"
              >{{ badge.text }}</span>
            </div>
            <p v-else class="text-muted small mb-0 mt-1">All tasks (no filters)</p>
          </div>
          <div class="d-flex gap-2 flex-shrink-0">
            <button type="button" class="btn btn-sm btn-primary" @click="apply(view)">
              <i class="bi bi-box-arrow-in-right" /> Open
            </button>
            <button type="button" class="btn btn-sm btn-outline-danger" @click="remove(view)">
              <i class="bi bi-trash" /> Delete
            </button>
          </div>
        </li>
      </ul>

      <div v-else class="card-body text-center py-5">
        <i class="bi bi-bookmark empty-state-icon text-muted" style="font-size: 2.5rem" aria-hidden="true" />
        <h3 class="h5 mt-3">No saved views yet</h3>
        <p class="text-muted mb-0">Create a preset above to quickly return to a filtered task list.</p>
      </div>
    </div>
  </div>
</template>
