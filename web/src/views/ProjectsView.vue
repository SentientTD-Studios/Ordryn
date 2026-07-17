<template>
  <div class="container mt-3">
    <h1>Projects</h1>
    <form class="row g-2 mb-4" @submit.prevent="create">
      <div class="col-sm-8">
        <input v-model="name" type="text" class="form-control" placeholder="New project name" required maxlength="50" />
      </div>
      <div class="col-sm-4">
        <button type="submit" class="btn btn-primary w-100">Add project</button>
      </div>
    </form>

    <ul class="list-group">
      <li v-for="p in projects" :key="p.id" class="list-group-item d-flex flex-wrap gap-2 align-items-center">
        <template v-if="renameId === p.id">
          <input v-model="renameValue" type="text" class="form-control form-control-sm" maxlength="50" />
          <button type="button" class="btn btn-sm btn-primary" @click="saveRename">Save</button>
          <button type="button" class="btn btn-sm btn-secondary" @click="renameId = null">Cancel</button>
        </template>
        <template v-else>
          <span class="flex-grow-1">{{ p.name }}</span>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="beginRename(p)">Rename</button>
          <button type="button" class="btn btn-sm btn-outline-danger" @click="remove(p)">Delete</button>
        </template>
      </li>
      <li v-if="!projects.length" class="list-group-item text-muted">No projects yet.</li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const projects = ref<Project[]>([])
const name = ref('')
const renameId = ref<number | null>(null)
const renameValue = ref('')
const toast = useToast()

async function load() {
  try {
    projects.value = await api.listProjects()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load projects', 'error')
  }
}

async function create() {
  if (!name.value.trim()) return
  try {
    await api.createProject(name.value.trim())
    name.value = ''
    toast.push('Project created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

function beginRename(p: Project) {
  renameId.value = p.id
  renameValue.value = p.name
}

async function saveRename() {
  if (renameId.value == null || !renameValue.value.trim()) return
  try {
    await api.renameProject(renameId.value, renameValue.value.trim())
    renameId.value = null
    toast.push('Renamed', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function remove(p: Project) {
  try {
    await api.deleteProject(p.id)
    toast.push('Project deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(load)
</script>
