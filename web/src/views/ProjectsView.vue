<template>
  <div class="container mt-4">
    <div class="row">
      <div class="col-md-8">
        <div class="card">
          <div class="card-header">
            <h3 class="mb-0">Your Projects</h3>
          </div>
          <div class="card-body">
            <p class="text-muted">
              Create projects to organize your tasks. Deleting a project will unassign tasks from it.
            </p>
            <div class="table-responsive">
              <table class="table table-striped projects-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th style="width: 120px">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="p in projects" :key="p.id">
                    <td data-label="Name">
                      <template v-if="renameProjectId === p.id">
                        <form
                          class="d-inline-flex align-items-center gap-1 flex-wrap"
                          @submit.prevent="saveRenameProject"
                        >
                          <input
                            v-model="renameProjectValue"
                            type="text"
                            class="form-control form-control-sm"
                            maxlength="50"
                            required
                            aria-label="Project name"
                          />
                          <button class="btn btn-sm btn-primary" type="submit" aria-label="Save project name">
                            <i class="bi bi-check" />
                          </button>
                          <button
                            class="btn btn-sm btn-secondary"
                            type="button"
                            aria-label="Cancel rename"
                            @click="renameProjectId = null"
                          >
                            <i class="bi bi-x" />
                          </button>
                        </form>
                      </template>
                      <template v-else>
                        <span class="project-name-display">{{ p.name }}</span>
                        <button
                          class="btn btn-sm btn-link edit-project-btn p-0 ms-1"
                          type="button"
                          aria-label="Rename project"
                          @click="beginRenameProject(p)"
                        >
                          <i class="bi bi-pencil" />
                        </button>
                      </template>
                    </td>
                    <td data-label="Actions">
                      <button
                        class="btn btn-sm btn-danger"
                        type="button"
                        aria-label="Delete project"
                        @click="removeProject(p)"
                      >
                        <i class="bi bi-trash" />
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!projects.length">
                    <td colspan="2" class="text-muted">No projects yet.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div class="card mt-4">
          <div class="card-header">
            <h3 class="mb-0 h5">Your Tags</h3>
          </div>
          <div class="card-body">
            <p class="text-muted small mb-3">
              Rename or delete tags. Deleting a tag removes it from all tasks.
            </p>
            <div class="table-responsive">
              <table class="table table-striped tags-table">
                <thead>
                  <tr>
                    <th>Tag</th>
                    <th style="width: 120px">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="tag in tags" :key="tag.id">
                    <td data-label="Tag">
                      <template v-if="renameTagId === tag.id">
                        <form
                          class="d-inline-flex align-items-center gap-1 flex-wrap"
                          @submit.prevent="saveRenameTag"
                        >
                          <input
                            v-model="renameTagValue"
                            type="text"
                            class="form-control form-control-sm"
                            maxlength="50"
                            required
                            aria-label="Tag name"
                          />
                          <button class="btn btn-sm btn-primary" type="submit" aria-label="Save tag name">
                            <i class="bi bi-check" />
                          </button>
                          <button
                            class="btn btn-sm btn-secondary"
                            type="button"
                            aria-label="Cancel rename"
                            @click="renameTagId = null"
                          >
                            <i class="bi bi-x" />
                          </button>
                        </form>
                      </template>
                      <template v-else>
                        <span class="tag-name-display">
                          <span class="tag-chip" :style="{ backgroundColor: tag.color }">{{ tag.name }}</span>
                        </span>
                        <button
                          class="btn btn-sm btn-link edit-tag-btn p-0 ms-1"
                          type="button"
                          aria-label="Rename tag"
                          @click="beginRenameTag(tag)"
                        >
                          <i class="bi bi-pencil" />
                        </button>
                      </template>
                    </td>
                    <td data-label="Actions">
                      <button
                        class="btn btn-sm btn-danger"
                        type="button"
                        :aria-label="`Delete tag ${tag.name}`"
                        @click="removeTag(tag)"
                      >
                        <i class="bi bi-trash" />
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!tags.length">
                    <td colspan="2" class="text-muted">No tags yet. Tags are created when you add them to tasks.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      <div class="col-md-4">
        <div class="card">
          <div class="card-header">
            <h5 class="mb-0">Create Project</h5>
          </div>
          <div class="card-body">
            <form id="createProjectForm" @submit.prevent="createProject">
              <div class="mb-3">
                <label class="form-label" for="project-name">Project Name</label>
                <input
                  id="project-name"
                  v-model="name"
                  class="form-control"
                  name="name"
                  maxlength="50"
                  required
                />
                <div class="d-flex justify-content-between align-items-center mt-1">
                  <small class="form-hint">Max 50 Characters</small>
                  <small class="text-muted">{{ name.length }}/50</small>
                </div>
              </div>
              <div class="d-flex gap-2">
                <button class="btn btn-primary" type="submit">Create</button>
                <RouterLink class="btn btn-secondary" to="/">Cancel</RouterLink>
              </div>
            </form>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Tag } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const projects = ref<Project[]>([])
const tags = ref<Tag[]>([])
const name = ref('')
const renameProjectId = ref<number | null>(null)
const renameProjectValue = ref('')
const renameTagId = ref<number | null>(null)
const renameTagValue = ref('')
const toast = useToast()

async function load() {
  try {
    const [p, t] = await Promise.all([api.listProjects(), api.listTags()])
    projects.value = p
    tags.value = t
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load projects', 'error')
  }
}

async function createProject() {
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

function beginRenameProject(p: Project) {
  renameProjectId.value = p.id
  renameProjectValue.value = p.name
  renameTagId.value = null
}

async function saveRenameProject() {
  if (renameProjectId.value == null || !renameProjectValue.value.trim()) return
  try {
    await api.renameProject(renameProjectId.value, renameProjectValue.value.trim())
    renameProjectId.value = null
    toast.push('Renamed', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function removeProject(p: Project) {
  if (!confirm('Delete this project? Tasks will be unassigned but not deleted.')) return
  try {
    await api.deleteProject(p.id)
    toast.push('Project deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

function beginRenameTag(tag: Tag) {
  renameTagId.value = tag.id
  renameTagValue.value = tag.name
  renameProjectId.value = null
}

async function saveRenameTag() {
  if (renameTagId.value == null || !renameTagValue.value.trim()) return
  try {
    await api.renameTag(renameTagId.value, renameTagValue.value.trim())
    renameTagId.value = null
    toast.push('Tag renamed', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function removeTag(tag: Tag) {
  if (!confirm('Delete this tag? It will be removed from all tasks.')) return
  try {
    await api.deleteTag(tag.id)
    toast.push('Tag deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(load)
</script>
