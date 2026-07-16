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

<template>
  <section v-if="task" class="page narrow">
    <p class="muted"><RouterLink to="/">← Tasks</RouterLink></p>
    <h1>Edit task</h1>
    <form class="stack" @submit.prevent="save">
      <label>
        Title
        <input v-model="title" type="text" required />
      </label>
      <label>
        Description
        <textarea v-model="description" rows="5" />
      </label>
      <label>
        Due date
        <input v-model="dueDate" type="date" />
      </label>
      <label>
        Project
        <select v-model="projectId">
          <option value="">No project</option>
          <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </label>
      <label>
        Priority
        <select v-model.number="priority">
          <option :value="0">None</option>
          <option :value="1">Low</option>
          <option :value="2">Medium</option>
          <option :value="3">High</option>
        </select>
      </label>
      <label class="inline">
        <input v-model="favorite" type="checkbox" />
        Favorite
      </label>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Saving…' : 'Save' }}
      </button>
    </form>
  </section>
</template>
