<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { SavedView } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const views = ref<SavedView[]>([])
const name = ref('')
const status = ref('incomplete')
const due = ref('')
const search = ref('')
const toast = useToast()
const router = useRouter()

async function load() {
  try {
    views.value = await api.listSavedViews()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load saved views', 'error')
  }
}

async function create() {
  if (!name.value.trim()) return
  try {
    await api.createSavedView({
      name: name.value.trim(),
      filter: {
        status: status.value || undefined,
        due: due.value || undefined,
        search: search.value.trim() || undefined,
      },
    })
    name.value = ''
    search.value = ''
    toast.push('Saved view created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

async function apply(view: SavedView) {
  const q: Record<string, string> = {}
  for (const [k, v] of Object.entries(view.filter || {})) {
    if (v) q[k] = String(v)
  }
  await router.push({ name: 'tasks', query: q })
}

async function remove(view: SavedView) {
  try {
    await api.deleteSavedView(view.id)
    toast.push('Deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Saved views</h1>
        <p class="lede">Named filter presets for the task list.</p>
      </div>
    </header>

    <form class="stack" @submit.prevent="create">
      <label>
        Name
        <input v-model="name" type="text" required maxlength="80" />
      </label>
      <label>
        Status
        <select v-model="status">
          <option value="incomplete">Incomplete</option>
          <option value="completed">Completed</option>
          <option value="">Any</option>
        </select>
      </label>
      <label>
        Due
        <select v-model="due">
          <option value="">Any</option>
          <option value="today">Today</option>
          <option value="overdue">Overdue</option>
          <option value="upcoming">Upcoming</option>
        </select>
      </label>
      <label>
        Search
        <input v-model="search" type="text" placeholder="Optional text filter" />
      </label>
      <button class="primary" type="submit">Save view</button>
    </form>

    <ul class="plain-list">
      <li v-for="view in views" :key="view.id" class="row">
        <div class="task-body">
          <strong>{{ view.name }}</strong>
          <p class="meta muted">
            {{ view.filter.status || 'any status' }}
            <span v-if="view.filter.due">· {{ view.filter.due }}</span>
            <span v-if="view.filter.search">· “{{ view.filter.search }}”</span>
          </p>
        </div>
        <button type="button" class="ghost" @click="apply(view)">Open</button>
        <button type="button" class="ghost danger" @click="remove(view)">Delete</button>
      </li>
      <li v-if="!views.length" class="muted empty">No saved views yet.</li>
    </ul>
  </section>
</template>
