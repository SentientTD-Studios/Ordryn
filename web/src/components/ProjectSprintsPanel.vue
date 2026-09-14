<template>
  <div v-if="isKanban" class="sprints-panel">
    <h4 class="h6 mb-2">Sprints</h4>
    <p class="small text-muted mb-3">
      Name a sprint, optionally describe it, and give it a date range. After the
      lock date, only the project owner can add tasks. Ranges cannot overlap;
      a sprint may start the day after another ends. The board can switch between
      sprints; tasks with no sprint stay in the {{ backlogName.toLowerCase() }}.
    </p>

    <div v-if="isOwner" class="card mb-3 border">
      <div class="card-body p-2">
        <h5 class="h6 mb-2">Automatic next sprint</h5>
        <p class="small text-muted mb-2">
          When the current dated sprint ends, create the next one only if that sprint still has tasks.
          Empty sprints are not followed automatically.
        </p>
        <form class="d-flex flex-column gap-2" @submit.prevent="saveAutoSprint">
          <div class="form-check">
            <input
              :id="`auto-create-sprint-${project.id}`"
              v-model="autoCreateEnabled"
              type="checkbox"
              class="form-check-input"
            />
            <label class="form-check-label small" :for="`auto-create-sprint-${project.id}`">
              Auto-create next sprint when the current sprint ends
            </label>
          </div>
          <div class="d-flex flex-wrap gap-2">
            <div>
              <label class="form-label small mb-0" :for="`auto-sprint-length-${project.id}`">Sprint length (days)</label>
              <input
                :id="`auto-sprint-length-${project.id}`"
                v-model.number="autoLengthDays"
                type="number"
                class="form-control form-control-sm"
                min="1"
                max="365"
                placeholder="30"
                :required="autoCreateEnabled"
              />
            </div>
            <div>
              <label class="form-label small mb-0" :for="`auto-sprint-lock-${project.id}`">Lock days before end</label>
              <input
                :id="`auto-sprint-lock-${project.id}`"
                v-model="autoLockDays"
                type="number"
                class="form-control form-control-sm"
                min="0"
                :max="typeof autoLengthDays === 'number' && autoLengthDays > 0 ? autoLengthDays - 1 : 364"
                placeholder="optional"
              />
            </div>
          </div>
          <small class="form-hint">
            Length is inclusive. Lock date is optional; leave blank to keep new sprints open.
          </small>
          <small v-if="autoSprintPreview" class="text-muted">{{ autoSprintPreview }}</small>
          <div>
            <button class="btn btn-sm btn-primary" type="submit" :disabled="savingAuto">Save auto-sprint settings</button>
          </div>
        </form>
      </div>
    </div>

    <!-- Backlog system sprint -->
    <div class="card mb-3 border bg-light-subtle">
      <div class="card-body p-2">
        <template v-if="editingBacklog">
          <form class="d-flex flex-column gap-2" @submit.prevent="saveBacklogEdit">
            <label class="small fw-bold mb-0" :for="`backlog-name-input-${project.id}`">System sprint name</label>
            <input
              :id="`backlog-name-input-${project.id}`"
              v-model="editBacklogName"
              type="text"
              class="form-control form-control-sm"
              maxlength="60"
              required
              aria-label="Backlog sprint name"
            />
            <label class="small fw-bold mb-0" :for="`backlog-desc-input-${project.id}`">Description (optional)</label>
            <input
              :id="`backlog-desc-input-${project.id}`"
              v-model="editBacklogDescription"
              type="text"
              class="form-control form-control-sm"
              :maxlength="maxSprintDescription"
              placeholder="e.g. Unscheduled ideas and tasks"
              aria-label="Backlog sprint description"
            />
            <div class="d-flex justify-content-between">
              <small class="form-hint">Max {{ maxSprintDescription }} characters</small>
              <small class="text-muted">{{ editBacklogDescription.length }}/{{ maxSprintDescription }}</small>
            </div>
            <small class="form-hint">Tasks with no assigned date range belong to this sprint. Dates cannot be set.</small>
            <div class="d-flex gap-1">
              <button class="btn btn-sm btn-primary" type="submit" :disabled="savingBacklog">Save</button>
              <button class="btn btn-sm btn-secondary" type="button" @click="cancelBacklogEdit">Cancel</button>
            </div>
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
                  aria-label="Edit backlog sprint"
                  @click="beginBacklogEdit"
                >
                  <i class="bi bi-pencil" />
                </button>
              </div>
              <div v-if="backlogDescription" class="small text-muted text-break">
                {{ backlogDescription }}
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
            <div class="form-check">
              <input
                :id="`edit-sprint-dateless-${s.id}`"
                v-model="editDateless"
                type="checkbox"
                class="form-check-input"
              />
              <label class="form-check-label small" :for="`edit-sprint-dateless-${s.id}`">
                Dateless sprint (no date range or lock date)
              </label>
            </div>
            <div v-if="!editDateless" class="d-flex flex-wrap gap-2">
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
            <small v-if="!editDateless" class="form-hint">Lock date is optional. After that day, only you can add tasks.</small>
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
              <span v-if="!s.start_date || !s.end_date" class="badge text-bg-secondary">dateless</span>
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
              <template v-if="s.start_date && s.end_date">
                {{ formatRange(s.start_date, s.end_date) }}
                <template v-if="s.lock_date"> · locks {{ s.lock_date }}</template>
                ·
              </template>
              {{ s.task_count }} task{{ s.task_count === 1 ? '' : 's' }}
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
      <div class="col-12">
        <div class="form-check">
          <input
            :id="`new-sprint-dateless-${project.id}`"
            v-model="newDateless"
            type="checkbox"
            class="form-check-input"
          />
          <label class="form-check-label small" :for="`new-sprint-dateless-${project.id}`">
            Dateless sprint (e.g. Icebox)
          </label>
        </div>
      </div>
      <template v-if="!newDateless">
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
      </template>
      <div v-else class="col-12">
        <small class="form-hint">Dateless sprints have no start or end dates and do not lock. Ideal for iceboxes or secondary backlogs.</small>
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
const newDateless = ref(false)
const newStart = ref('')
const newEnd = ref('')
const newLock = ref('')
const editId = ref<number | null>(null)
const editName = ref('')
const editDescription = ref('')
const editDateless = ref(false)
const editStart = ref('')
const editEnd = ref('')
const editLock = ref('')

const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const isOwner = computed(() => (props.project.role || 'owner') === 'owner')

const backlogName = computed(() => props.project.backlog_name || 'Backlog')
const backlogDescription = computed(() => props.project.backlog_description || '')
const editingBacklog = ref(false)
const editBacklogName = ref('')
const editBacklogDescription = ref('')
const savingBacklog = ref(false)
const autoCreateEnabled = ref(false)
const autoLengthDays = ref<number | ''>('')
const autoLockDays = ref<string>('')
const savingAuto = ref(false)

function syncAutoSprintForm() {
  autoCreateEnabled.value = !!props.project.auto_create_next_sprint
  autoLengthDays.value = props.project.auto_sprint_length_days ?? ''
  autoLockDays.value =
    props.project.auto_sprint_lock_days_before == null ? '' : String(props.project.auto_sprint_lock_days_before)
}

function addUTCDays(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

const autoSprintPreview = computed(() => {
  const length = typeof autoLengthDays.value === 'number' ? autoLengthDays.value : Number(autoLengthDays.value)
  if (!Number.isInteger(length) || length < 1) return ''
  const lockRaw = autoLockDays.value.trim()
  const lockDays = lockRaw === '' ? null : Number(lockRaw)
  if (lockDays != null && (!Number.isInteger(lockDays) || lockDays < 0 || lockDays >= length)) return ''
  const start = addUTCDays('2026-08-31', 1)
  const end = addUTCDays(start, length - 1)
  if (lockDays == null) {
    return `Example: a sprint ending 2026-08-31 is followed by ${start} – ${end} with no lock date.`
  }
  const lock = addUTCDays(end, -lockDays)
  return `Example: a sprint ending 2026-08-31 is followed by ${start} – ${end}, locking on ${lock}.`
})

async function saveAutoSprint() {
  const lengthRaw = autoLengthDays.value === '' ? null : Number(autoLengthDays.value)
  const lockRaw = autoLockDays.value.trim()
  const lockDays = lockRaw === '' ? null : Number(lockRaw)
  if (autoCreateEnabled.value) {
    if (lengthRaw == null || !Number.isInteger(lengthRaw) || lengthRaw < 1 || lengthRaw > 365) {
      toast.push('Sprint length is required when auto-create is enabled (1–365 days)', 'error')
      return
    }
    if (lockDays != null && (!Number.isInteger(lockDays) || lockDays < 0 || lockDays >= lengthRaw)) {
      toast.push('Lock days before end must be less than sprint length', 'error')
      return
    }
  } else if (lengthRaw != null && (!Number.isInteger(lengthRaw) || lengthRaw < 1 || lengthRaw > 365)) {
    toast.push('Sprint length must be between 1 and 365 days', 'error')
    return
  } else if (lengthRaw != null && lockDays != null && (!Number.isInteger(lockDays) || lockDays < 0 || lockDays >= lengthRaw)) {
    toast.push('Lock days before end must be less than sprint length', 'error')
    return
  } else if (lockDays != null && !Number.isInteger(lockDays)) {
    toast.push('Lock days before end must be a whole number', 'error')
    return
  }
  savingAuto.value = true
  try {
    await api.updateProject(props.project.id, {
      auto_create_next_sprint: autoCreateEnabled.value,
      auto_sprint_length_days: lengthRaw,
      auto_sprint_lock_days_before: lockDays,
    })
    toast.push('Auto-sprint settings saved', 'success')
    await loadSprints()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save auto-sprint settings', 'error')
  } finally {
    savingAuto.value = false
  }
}

function beginBacklogEdit() {
  editBacklogName.value = backlogName.value
  editBacklogDescription.value = backlogDescription.value
  editingBacklog.value = true
}

function cancelBacklogEdit() {
  editingBacklog.value = false
  editBacklogName.value = ''
  editBacklogDescription.value = ''
}

async function saveBacklogEdit() {
  const trimmed = editBacklogName.value.trim()
  if (!trimmed) {
    toast.push('Backlog sprint name is required', 'error')
    return
  }
  savingBacklog.value = true
  try {
    await api.updateProject(props.project.id, {
      backlog_name: trimmed,
      backlog_description: editBacklogDescription.value.trim(),
    })
    editingBacklog.value = false
    toast.push('System sprint updated', 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update system sprint', 'error')
  } finally {
    savingBacklog.value = false
  }
}

function formatRange(start?: string | null, end?: string | null) {
  if (!start || !end) return ''
  return `${start} – ${end}`
}

function datesOverlap(aStart: string, aEnd: string, bStart: string, bEnd: string): boolean {
  return aStart <= bEnd && bStart <= aEnd
}

function overlappingSprint(start: string, end: string, exceptId?: number): ProjectSprint | undefined {
  return sprints.value.find(
    (s) =>
      (exceptId == null || s.id !== exceptId) &&
      !!s.start_date &&
      !!s.end_date &&
      datesOverlap(start, end, s.start_date, s.end_date),
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
  editDateless.value = !s.start_date || !s.end_date
  editStart.value = s.start_date || ''
  editEnd.value = s.end_date || ''
  editLock.value = s.lock_date || ''
}

async function addSprint() {
  if (!newName.value.trim()) return
  if (!newDateless.value) {
    if (!newStart.value || !newEnd.value) return
    if (newEnd.value < newStart.value) {
      toast.push('End date must be on or after start date', 'error')
      return
    }
    const hit = overlappingSprint(newStart.value, newEnd.value)
    if (hit) {
      toast.push(overlapMessage(hit), 'error')
      return
    }
  }
  adding.value = true
  try {
    await api.createProjectSprint(props.project.id, {
      name: newName.value.trim(),
      description: newDescription.value.trim(),
      start_date: newDateless.value ? null : newStart.value,
      end_date: newDateless.value ? null : newEnd.value,
      lock_date: newDateless.value ? null : newLock.value || null,
    })
    newName.value = ''
    newDescription.value = ''
    newStart.value = ''
    newEnd.value = ''
    newLock.value = ''
    newDateless.value = false
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
  if (!editName.value.trim()) return
  if (!editDateless.value) {
    if (!editStart.value || !editEnd.value) {
      toast.push('Start and end dates are required for a dated sprint', 'error')
      return
    }
    if (editEnd.value < editStart.value) {
      toast.push('End date must be on or after start date', 'error')
      return
    }
    const hit = overlappingSprint(editStart.value, editEnd.value, s.id)
    if (hit) {
      toast.push(overlapMessage(hit), 'error')
      return
    }
  }
  saving.value = true
  try {
    if (editDateless.value) {
      await api.updateProjectSprint(props.project.id, s.id, {
        name: editName.value.trim(),
        description: editDescription.value.trim(),
        start_date: '',
        end_date: '',
        lock_date: null,
      })
    } else {
      await api.updateProjectSprint(props.project.id, s.id, {
        name: editName.value.trim(),
        description: editDescription.value.trim(),
        start_date: editStart.value,
        end_date: editEnd.value,
        lock_date: editLock.value || null,
      })
    }
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
    syncAutoSprintForm()
  },
  { immediate: true },
)

watch(
  () =>
    [
      props.project.auto_create_next_sprint,
      props.project.auto_sprint_length_days,
      props.project.auto_sprint_lock_days_before,
    ] as const,
  () => {
    syncAutoSprintForm()
  },
)
</script>
