<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import ProjectSharePanel from '@/components/ProjectSharePanel.vue'
import ProjectWorkflowPanel from '@/components/ProjectWorkflowPanel.vue'
import ProjectSprintsPanel from '@/components/ProjectSprintsPanel.vue'
import ProjectGitHubPanel from '@/components/ProjectGitHubPanel.vue'
import ProjectExtensionsPanel from '@/components/ProjectExtensionsPanel.vue'
import ProjectTagsPanel from '@/components/ProjectTagsPanel.vue'
import { isArchivedProject } from '@/utils/projectLabel'

type SettingsTab = 'details' | 'board' | 'sprints' | 'tags' | 'github' | 'extensions' | 'sharing'

const props = defineProps<{
  open: boolean
  project: Project | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
  changed: []
  'columns-changed': []
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const name = ref('')
const description = ref('')
const saving = ref(false)
const archiving = ref(false)
const tab = ref<SettingsTab>('details')
const isOwner = computed(() => (props.project?.role || 'owner') === 'owner')
const isKanban = computed(() => (props.project?.workflow_mode || 'classic') === 'kanban')

const tabs = computed(() => {
  const items: { id: SettingsTab; label: string }[] = [
    { id: 'details', label: 'Details' },
    { id: 'board', label: 'Board' },
  ]
  if (isKanban.value) items.push({ id: 'sprints', label: 'Sprints' })
  items.push({ id: 'tags', label: 'Tags' }, { id: 'github', label: 'GitHub' })
  if (isOwner.value) items.push({ id: 'extensions', label: 'Extensions' })
  items.push({ id: 'sharing', label: 'Sharing' })
  return items
})

watch(
  () => props.open,
  (open) => {
    if (!open || !props.project) return
    name.value = props.project.name
    description.value = props.project.description || ''
    tab.value = 'details'
  },
)

watch(
  () => props.project?.id,
  (id, prevId) => {
    if (!props.open || !props.project) return
    name.value = props.project.name
    description.value = props.project.description || ''
    if (prevId !== undefined && id !== prevId) tab.value = 'details'
  },
)

watch(isKanban, (kanban) => {
  if (!kanban && tab.value === 'sprints') tab.value = 'details'
})

watch(isOwner, (owner) => {
  if (!owner && tab.value === 'extensions') tab.value = 'details'
})

function close() {
  emit('close')
}

async function saveBasics() {
  if (!props.project || !name.value.trim() || !isOwner.value) return
  saving.value = true
  try {
    await api.updateProject(props.project.id, {
      name: name.value.trim(),
      description: description.value.trim(),
    })
    toast.push('Project updated', 'success')
    emit('saved')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to update project', 'error')
  } finally {
    saving.value = false
  }
}

function onPanelChanged() {
  emit('changed')
}

function onColumnsChanged() {
  emit('columns-changed')
}

async function archiveOrRestore() {
  if (!props.project || !isOwner.value) return
  const archived = isArchivedProject(props.project)
  if (!archived) {
    const ok = await askConfirm({
      title: 'Archive project?',
      message: `Archive “${props.project.name}”? It will move to the Archived section, its tasks will be tagged archived, and new tasks cannot be added until you restore it.`,
      confirmLabel: 'Archive',
    })
    if (!ok) return
  }
  archiving.value = true
  try {
    if (archived) {
      await api.restoreProject(props.project.id)
      toast.push('Project restored', 'success')
    } else {
      await api.archiveProject(props.project.id)
      toast.push('Project archived', 'info')
    }
    emit('saved')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update archive state', 'error')
  } finally {
    archiving.value = false
  }
}
</script>

<template>
  <div
    v-if="open && project"
    class="modal fade show d-block"
    style="background: rgba(0,0,0,0.5);"
    tabindex="-1"
    @click.self="close"
  >
    <div class="modal-dialog modal-dialog-centered modal-lg modal-dialog-scrollable">
      <div
        class="modal-content border-0 shadow"
        style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
      >
        <div class="modal-header border-0 pb-0">
          <h5 class="modal-title fw-bold">{{ isOwner ? 'Edit Project' : 'Project settings' }}</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="close" />
        </div>
        <div class="modal-body py-3">
          <nav class="mb-3" aria-label="Project settings sections">
            <ul class="nav nav-pills gap-1 flex-wrap">
              <li v-for="item in tabs" :key="item.id" class="nav-item">
                <button
                  type="button"
                  class="nav-link"
                  :class="{ active: tab === item.id }"
                  :aria-current="tab === item.id ? 'page' : undefined"
                  @click="tab = item.id"
                >
                  {{ item.label }}
                </button>
              </li>
            </ul>
          </nav>

          <div v-if="tab === 'details'">
            <div class="mb-3">
              <label for="edit-project-name" class="form-label small fw-bold">Project Name</label>
              <input
                id="edit-project-name"
                v-model="name"
                type="text"
                class="form-control"
                maxlength="50"
                placeholder="Project Name"
                :readonly="!isOwner"
              />
              <div class="d-flex justify-content-between">
                <small class="form-hint">Max 50 characters</small>
                <small class="text-muted">{{ name.length }}/50</small>
              </div>
            </div>

            <div class="mb-3">
              <label for="edit-project-description" class="form-label small fw-bold">Description</label>
              <textarea
                id="edit-project-description"
                v-model="description"
                class="form-control"
                rows="3"
                maxlength="1000"
                placeholder="Optional details about this project"
                :readonly="!isOwner"
              />
              <div class="d-flex justify-content-end">
                <small class="text-muted">{{ description.length }}/1000</small>
              </div>
            </div>

            <div v-if="isOwner" class="d-flex justify-content-between align-items-center gap-2 flex-wrap">
              <button
                type="button"
                class="btn btn-sm"
                :class="isArchivedProject(project) ? 'btn-outline-primary' : 'btn-outline-secondary'"
                :disabled="archiving"
                @click="archiveOrRestore"
              >
                <i :class="isArchivedProject(project) ? 'bi bi-arrow-counterclockwise' : 'bi bi-archive'" class="me-1" />
                {{ isArchivedProject(project) ? 'Restore project' : 'Archive project' }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-primary px-3"
                :disabled="saving || !name.trim()"
                @click="saveBasics"
              >
                Save details
              </button>
            </div>
            <p v-if="isArchivedProject(project)" class="small text-muted mt-2 mb-0">
              This project is archived. Existing tasks stay, but new tasks cannot be added until it is restored.
            </p>
          </div>

          <ProjectWorkflowPanel
            v-else-if="tab === 'board'"
            :project="project"
            @changed="onPanelChanged"
            @columns-changed="onColumnsChanged"
          />

          <ProjectSprintsPanel
            v-else-if="tab === 'sprints' && isKanban"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectTagsPanel
            v-else-if="tab === 'tags'"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectGitHubPanel
            v-else-if="tab === 'github'"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectExtensionsPanel
            v-else-if="tab === 'extensions' && isOwner"
            :project="project"
          />

          <ProjectSharePanel
            v-else-if="tab === 'sharing'"
            :project="project"
            @changed="onPanelChanged"
          />
        </div>
        <div class="modal-footer border-0 pt-0 justify-content-end">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="close">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>
