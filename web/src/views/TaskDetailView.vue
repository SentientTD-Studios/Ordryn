<template>
  <div v-if="task" class="container mt-3">
    <p class="mb-3"><RouterLink to="/">← Tasks</RouterLink></p>
    <div class="card">
      <div class="card-header">
        <h1 class="h4 mb-0">Edit task</h1>
      </div>
      <div class="card-body">
        <form @submit.prevent="save">
          <div class="mb-3">
            <label class="form-label">Title</label>
            <input v-model="title" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label class="form-label">Description</label>
            <textarea v-model="description" class="form-control" rows="5" />
          </div>
          <div class="mb-3">
            <label class="form-label">Due date</label>
            <input v-model="dueDate" type="date" class="form-control" />
          </div>
          <div class="mb-3">
            <label class="form-label">Project</label>
            <select v-model="projectId" class="form-select">
              <option value="">No project</option>
              <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
          <div class="mb-3">
            <label class="form-label">Priority</label>
            <select v-model.number="priority" class="form-select">
              <option :value="0">None</option>
              <option :value="1">Low</option>
              <option :value="2">Medium</option>
              <option :value="3">High</option>
            </select>
          </div>
          <div class="form-check mb-3">
            <input id="task-favorite" v-model="favorite" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="task-favorite">Favorite</label>
          </div>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? 'Saving…' : 'Save' }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Task } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const task = ref<Task | null>(null)
const projects = ref<Project[]>([])
const title = ref('')
const description = ref('')
const dueDate = ref('')
const projectId = ref<number | ''>('')
const priority = ref(0)
const favorite = ref(false)
const busy = ref(false)

async function load() {
  const id = Number(route.params.id)
  try {
    const [t, projs] = await Promise.all([api.getTask(id), api.listProjects()])
    task.value = t
    projects.value = projs
    title.value = t.title
    description.value = t.description || ''
    dueDate.value = t.due_date || ''
    projectId.value = t.project_id ?? ''
    priority.value = t.priority
    favorite.value = t.favorite
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load task', 'error')
    await router.replace('/')
  }
}

async function save() {
  if (!task.value) return
  busy.value = true
  try {
    const payload: Parameters<typeof api.patchTask>[1] = {
      title: title.value.trim(),
      description: description.value,
      priority: Number(priority.value),
      favorite: favorite.value,
      project_id: projectId.value === '' ? null : Number(projectId.value),
    }
    if (dueDate.value) {
      payload.due_date = dueDate.value
    } else {
      payload.clear_due_date = true
    }
    task.value = await api.patchTask(task.value.id, payload)
    toast.push('Saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>
