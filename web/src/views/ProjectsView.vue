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

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Projects</h1>
        <p class="lede">Organize tasks without leaving the SPA.</p>
      </div>
    </header>

    <form class="composer" @submit.prevent="create">
      <input v-model="name" type="text" placeholder="New project name" required maxlength="50" />
      <button class="primary" type="submit">Add project</button>
    </form>

    <ul class="plain-list">
      <li v-for="p in projects" :key="p.id" class="row">
        <template v-if="renameId === p.id">
          <input v-model="renameValue" type="text" maxlength="50" />
          <button type="button" class="primary" @click="saveRename">Save</button>
          <button type="button" class="ghost" @click="renameId = null">Cancel</button>
        </template>
        <template v-else>
          <span>{{ p.name }}</span>
          <button type="button" class="ghost" @click="beginRename(p)">Rename</button>
          <button type="button" class="ghost danger" @click="remove(p)">Delete</button>
        </template>
      </li>
      <li v-if="!projects.length" class="muted empty">No projects yet.</li>
    </ul>
  </section>
</template>
