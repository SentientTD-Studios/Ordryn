<template>
  <div v-if="isKanban" class="sprints-panel">
    <h4 class="h6 mb-2">Sprints</h4>
    <p class="small text-muted mb-3">
      Name a sprint, optionally describe it, and give it a date range. After the
      lock date, only the project owner can add tasks. Ranges cannot overlap;
      a sprint may start the day after another ends. The board can switch between
      sprints; tasks with no sprint stay in the {{ backlogName.toLowerCase() }}.
    </p>

    <!-- Backlog system sprint -->
    <div class="card mb-3 border bg-light-subtle">
      <div class="card-body p-2">
        <template v-if="editingBacklog">
          <form class="d-flex flex-column gap-2" @submit.prevent="saveBacklogEdit">
            <label class="small fw-bold mb-0" :for="`backlog-name-input-${project.id}`">System sprint name</label>
            <div class="d-flex align-items-center gap-2">
              <input
                :id="`backlog-name-input-${project.id}`"
                v-model="editBacklogName"
                type="text"
                class="form-control form-control-sm"
                maxlength="60"
                required
                aria-label="Backlog sprint name"
              />
              <button class="btn btn-sm btn-primary" type="submit" :disabled="savingBacklog">Save</button>
              <button class="btn btn-sm btn-secondary" type="button" @click="cancelBacklogEdit">Cancel</button>
            </div>
            <small class="form-hint">Tasks with no assigned date range belong to this sprint. Dates cannot be set.</small>
          </form>
        </template>
        <template v-else>
          <div class="d-flex align-items-center justify-content-between">
            <div>
              <div class="d-flex align-items-center gap-1 flex-wrap">
                <strong>{{ backlogName }}</strong>
                <span class="badge text-bg-secondary">system sprint</span>
                <button
                  v-if="isOwner"
                  class="btn btn-sm btn-link p-0"
                  type="button"
                  aria-label="Rename backlog sprint"
                  @click="beginBacklogEdit"
                >
                  <i class="bi bi-pencil" />
                </button>
              </div>
              <div class="small text-muted">
                Default destination for tasks not assigned to a dated sprint. Dates cannot be set.
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>

    <ul class="list-unstyled mb-3">
      <li
        v-for="s in sprints"
        :key="s.id"
        class="d-flex flex-wrap align-items-start gap-2 mb-2 pb-2 border-bottom"
      >
        <template v-if="editId === s.id">
          <form class="d-flex flex-column gap-2 flex-grow-1" @submit.prevent="saveEdit(s)">
            <input
              v-model="editName"
              type="text"
              class="form-control form-control-sm"
              maxlength="60"
              required
              aria-label="Sprint name"
            />
            <input
              v-model="editDescription"
              type="text"
              class="form-control form-control-sm"
              :maxlength="maxSprintDescription"
              placeholder="Short description (optional)"
              aria-label="Sprint description"
            />
            <div class="d-flex justify-content-between">
              <small class="form-hint">Max {{ maxSprintDescription }} characters</small>
              <small class="text-muted">{{ editDescription.length }}/{{ maxSprintDescription }}</small>
            </div>
            <div class="d-flex flex-wrap gap-2">
              <input
                v-model="editStart"
                type="date"
                class="form-control form-control-sm"
                required
                aria-label="Sprint start date"
              />
              <input
                v-model="editEnd"
                type="date"
                class="form-control form-control-sm"
                required
                aria-label="Sprint end date"
              />
              <input
                v-model="editLock"
                type="date"
                class="form-control form-control-sm"
                aria-label="Sprint lock date"
              />
            </div>
            <small class="form-hint">Lock date is optional. After that day, only you can add tasks.</small>
            <div class="d-flex gap-1">
              <button class="btn btn-sm btn-primary" type="submit" :disabled="saving">Save</button>
              <button class="btn btn-sm btn-secondary" type="button" @click="editId = null">Cancel</button>
            </div>
          </form>
        </template>
        <template v-else>
          <div class="min-w-0 flex-grow-1">
            <div class="d-flex align-items-center gap-1 flex-wrap">
              <strong>{{ s.name }}</strong>
              <span v-if="s.is_active" class="badge text-bg-success">active</span>
              <span v-if="s.is_locked" class="badge text-bg-warning">locked</span>
              <button
                v-if="isOwner"
                class="btn btn-sm btn-link p-0"
                type="button"
                aria-label="Edit sprint"
                @click="beginEdit(s)"
              >
                <i class="bi bi-pencil" />
              </button>
            </div>
            <div v-if="s.description" class="small text-muted text-break">{{ s.description }}</div>
            <div class="small text-muted">
              {{ formatRange(s.start_date, s.end_date) }}
              <template v-if="s.lock_date"> · locks {{ s.lock_date }}</template>
              · {{ s.task_count }} task{{ s.task_count === 1 ? '' : 's' }}
            </div>
          </div>
          <button
            v-if="isOwner"
            class="btn btn-sm btn-outline-danger"
            type="button"
            :disabled="deletingId === s.id"
            aria-label="Delete sprint"
            @click="removeSprint(s)"
          >
            <i class="bi bi-trash" />
          </button>
        </template>
      </li>
    </ul>

    <p v-if="!sprints.length" class="small text-muted mb-3">No sprints yet.</p>

    <form v-if="isOwner" class="row g-2 align-items-end" @submit.prevent="addSprint">
      <div class="col-12">
        <label class="form-label small mb-0" :for="`new-sprint-name-${project.id}`">New sprint</label>
        <input
          :id="`new-sprint-name-${project.id}`"
          v-model="newName"
          type="text"
          class="form-control form-control-sm"
          maxlength="60"
          required
          placeholder="Sprint name"
        />
      </div>
      <div class="col-12">
        <label class="form-label small mb-0" :for="`new-sprint-desc-${project.id}`">Description</label>
        <input
          :id="`new-sprint-desc-${project.id}`"
          v-model="newDescription"
          type="text"
          class="form-control form-control-sm"
          :maxlength="maxSprintDescription"
          placeholder="e.g. features required for v3.0.0 release"
        />
        <div class="d-flex justify-content-between">
          <small class="form-hint">Max {{ maxSprintDescription }} characters</small>
          <small class="text-muted">{{ newDescription.length }}/{{ maxSprintDescription }}</small>
        </div>
      </div>
      <div class="col-sm-6">
        <label class="form-label small mb-0" :for="`new-sprint-start-${project.id}`">Starts</label>
        <input
          :id="`new-sprint-start-${project.id}`"
          v-model="newStart"
          type="date"
          class="form-control form-control-sm"
          required
        />
      </div>
      <div class="col-sm-6">
        <label class="form-label small mb-0" :for="`new-sprint-end-${project.id}`">Ends</label>
        <input
          :id="`new-sprint-end-${project.id}`"
          v-model="newEnd"
          type="date"
          class="form-control form-control-sm"
          required
        />
      </div>
      <div class="col-sm-6">
        <label class="form-label small mb-0" :for="`new-sprint-lock-${project.id}`">Lock date</label>
        <input
          :id="`new-sprint-lock-${project.id}`"
          v-model="newLock"
          type="date"
          class="form-control form-control-sm"
        />
      </div>
      <div class="col-12">
        <small class="form-hint">After the lock date, only the project owner can add tasks. Leave blank to keep the sprint open.</small>
      </div>
      <div class="col-12">
        <button class="btn btn-sm btn-primary" type="submit" :disabled="adding">Add sprint</button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectSprint } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  project: Project
}>()

const emit = defineEmits<{
  changed: []
}>()

const toast = useToast()
const sprints = ref<ProjectSprint[]>([])
const adding = ref(false)
const saving = ref(false)
const deletingId = ref<number | null>(null)
const maxSprintDescription = 80
const newName = ref('')
const newDescription = ref('')
const newStart = ref('')
const newEnd = ref('')
const newLock = ref('')
const editId = ref<number | null>(null)
const editName = ref('')
const editDescription = ref('')
const editStart = ref('')
const editEnd = ref('')
const editLock = ref('')

const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const isOwner = computed(() => (props.project.role || 'owner') === 'owner')

const backlogName = computed(() => props.project.backlog_name || 'Backlog')
const editingBacklog = ref(false)
const editBacklogName = ref('')
const savingBacklog = ref(false)

function beginBacklogEdit() {
  editBacklogName.value = backlogName.value
  editingBacklog.value = true
}

function cancelBacklogEdit() {
  editingBacklog.value = false
  editBacklogName.value = ''
}

async function saveBacklogEdit() {
  const trimmed = editBacklogName.value.trim()
  if (!trimmed) {
    toast.push('Backlog sprint name is required', 'error')
    return
  }
  savingBacklog.value = true
  try {
    await api.updateProject(props.project.id, { backlog_name: trimmed })
    editingBacklog.value = false
    toast.push('System sprint renamed', 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not rename system sprint', 'error')
  } finally {
    savingBacklog.value = false
  }
}

function formatRange(start: string, end: string) {
  return `${start} – ${end}`
}

function datesOverlap(aStart: string, aEnd: string, bStart: string, bEnd: string): boolean {
  return aStart <= bEnd && bStart <= aEnd
}

function overlappingSprint(start: string, end: string, exceptId?: number): ProjectSprint | undefined {
  return sprints.value.find(
    (s) => (exceptId == null || s.id !== exceptId) && datesOverlap(start, end, s.start_date, s.end_date),
  )
}

function overlapMessage(hit: ProjectSprint): string {
  return `Dates overlap ${hit.name} (${formatRange(hit.start_date, hit.end_date)})`
}

async function loadSprints() {
  if (!isKanban.value) {
    sprints.value = []
    return
  }
  try {
    sprints.value = await api.listProjectSprints(props.project.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load sprints', 'error')
    sprints.value = []
  }
}

function beginEdit(s: ProjectSprint) {
  editId.value = s.id
  editName.value = s.name
  editDescription.value = s.description || ''
  editStart.value = s.start_date
  editEnd.value = s.end_date
  editLock.value = s.lock_date || ''
}

async function addSprint() {
  if (!newName.value.trim() || !newStart.value || !newEnd.value) return
  if (newEnd.value < newStart.value) {
    toast.push('End date must be on or after start date', 'error')
    return
  }
  const hit = overlappingSprint(newStart.value, newEnd.value)
  if (hit) {
    toast.push(overlapMessage(hit), 'error')
    return
  }
  adding.value = true
  try {
    await api.createProjectSprint(props.project.id, {
      name: newName.value.trim(),
      description: newDescription.value.trim(),
      start_date: newStart.value,
      end_date: newEnd.value,
      lock_date: newLock.value || null,
    })
    newName.value = ''
    newDescription.value = ''
    newStart.value = ''
    newEnd.value = ''
    newLock.value = ''
    toast.push('Sprint created', 'success')
    await loadSprints()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create sprint', 'error')
  } finally {
    adding.value = false
  }
}

async function saveEdit(s: ProjectSprint) {
  if (!editName.value.trim() || !editStart.value || !editEnd.value) return
  if (editEnd.value < editStart.value) {
    toast.push('End date must be on or after start date', 'error')
    return
  }
  const hit = overlappingSprint(editStart.value, editEnd.value, s.id)
  if (hit) {
    toast.push(overlapMessage(hit), 'error')
    return
  }
  saving.value = true
  try {
    await api.updateProjectSprint(props.project.id, s.id, {
      name: editName.value.trim(),
      description: editDescription.value.trim(),
      start_date: editStart.value,
      end_date: editEnd.value,
      lock_date: editLock.value || null,
    })
    editId.value = null
    toast.push('Sprint updated', 'success')
    await loadSprints()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update sprint', 'error')
  } finally {
    saving.value = false
  }
}

async function removeSprint(s: ProjectSprint) {
  deletingId.value = s.id
  try {
    await api.deleteProjectSprint(props.project.id, s.id)
    toast.push(`Sprint deleted; tasks moved to ${backlogName.value.toLowerCase()}`, 'success')
    await loadSprints()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete sprint', 'error')
  } finally {
    deletingId.value = null
  }
}

watch(
  () => [props.project.id, props.project.workflow_mode] as const,
  () => {
    void loadSprints()
  },
  { immediate: true },
)
</script>
