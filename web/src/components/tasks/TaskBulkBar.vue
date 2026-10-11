<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Project, ProjectSprint, ProjectStatus, Tag } from '@/api/types'
import { activeProjects, projectOptionLabel } from '@/utils/projectLabel'
import { hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'
import { sprintLockedForUser, sprintOptionLabel } from '@/utils/sprintLabel'
import { formatLocalDate } from '@/utils/quickAdd'

const props = defineProps<{
  count: number
  /** The project the list is scoped to; null on Home. */
  project: Project | null
  projects: Project[]
  tags: Tag[]
  sprints: ProjectSprint[]
  statuses: ProjectStatus[]
  isKanban: boolean
  canMoveProject: boolean
}>()

const emit = defineEmits<{
  bulk: [action: string, extra?: Record<string, unknown>]
  deselect: []
}>()

const bulkProject = ref('')
const bulkTag = ref('')
const bulkPriority = ref('')
const bulkDate = ref('')
const bulkSprint = ref('')
const bulkStatus = ref('')

const canComplete = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_COMPLETE))
const canDelete = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_DELETE))
const canEditDetails = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_EDIT))
const canStatus = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_STATUS))
const canSprint = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_SPRINT))

function daysFromToday(n: number) {
  const d = new Date()
  d.setDate(d.getDate() + n)
  return formatLocalDate(d)
}

function setDue(dueDate: string) {
  emit('bulk', 'set_due_date', { due_date: dueDate })
}
</script>

<template>
  <div
    class="bulk-action-bar alert alert-info py-1.5 px-3 rounded-3 shadow-sm d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2"
  >
    <span class="fw-semibold small">{{ count }} task{{ count === 1 ? '' : 's' }} selected</span>
    <div class="d-flex align-items-center gap-2">
      <button v-if="canComplete" type="button" class="btn btn-xs btn-success rounded-pill" @click="emit('bulk', 'complete')">Complete</button>
      <button v-if="canComplete" type="button" class="btn btn-xs btn-outline-secondary rounded-pill" @click="emit('bulk', 'incomplete')">Incomplete</button>

      <div class="dropdown d-inline-block">
        <button class="btn btn-xs btn-outline-primary dropdown-toggle rounded-pill" type="button" data-bs-toggle="dropdown" data-bs-auto-close="outside">
          More Actions
        </button>
        <div
          class="dropdown-menu shadow-lg border p-3 rounded-3 mt-1"
          style="width: 280px; max-width: 90vw; background: var(--ordryn-card-bg); color: var(--ordryn-text); border-color: var(--ordryn-card-border) !important;"
        >
          <div class="mb-3">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Move to project...</label>
            <select v-model="bulkProject" class="form-select form-select-sm mb-2" :disabled="!canMoveProject">
              <option value="">Select project...</option>
              <option value="0">No Project</option>
              <option v-for="p in activeProjects(projects)" :key="p.id" :value="String(p.id)">{{ projectOptionLabel(p) }}</option>
            </select>
            <button
              type="button"
              class="btn btn-xs btn-outline-primary rounded-pill w-100"
              :disabled="!canMoveProject || bulkProject === ''"
              @click="emit('bulk', 'move_project', { project_id: bulkProject })"
            >
              Move
            </button>
            <small v-if="!canMoveProject" class="form-hint d-block mt-1">
              Only the project owner can move this task to another project.
            </small>
          </div>

          <div v-if="canEditDetails" class="mb-3 border-top pt-2">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Select tag...</label>
            <select v-model="bulkTag" class="form-select form-select-sm mb-2">
              <option value="">Select tag...</option>
              <option v-for="t in tags" :key="t.id" :value="String(t.id)">{{ t.name }}</option>
            </select>
            <div class="d-flex gap-2">
              <button
                type="button"
                class="btn btn-xs btn-outline-primary rounded-pill flex-grow-1"
                :disabled="!bulkTag"
                @click="emit('bulk', 'add_tag', { tag_id: parseInt(bulkTag, 10) })"
              >
                Add Tag
              </button>
              <button
                type="button"
                class="btn btn-xs btn-outline-secondary rounded-pill flex-grow-1"
                :disabled="!bulkTag"
                @click="emit('bulk', 'remove_tag', { tag_id: parseInt(bulkTag, 10) })"
              >
                Remove Tag
              </button>
            </div>
          </div>

          <div v-if="canEditDetails" class="mb-3 border-top pt-2">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Priority</label>
            <select v-model="bulkPriority" class="form-select form-select-sm mb-2">
              <option value="">Select priority...</option>
              <option value="3">High Priority</option>
              <option value="2">Medium Priority</option>
              <option value="1">Low Priority</option>
              <option value="0">No Priority</option>
            </select>
            <button
              type="button"
              class="btn btn-xs btn-outline-primary rounded-pill w-100"
              :disabled="bulkPriority === ''"
              @click="emit('bulk', 'set_priority', { priority: parseInt(bulkPriority, 10) })"
            >
              Set Priority
            </button>
          </div>

          <div v-if="isKanban && canSprint" class="mb-3 border-top pt-2">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Move to sprint...</label>
            <select v-model="bulkSprint" class="form-select form-select-sm mb-2">
              <option value="">Select sprint...</option>
              <option value="0">{{ project?.backlog_name || 'Backlog' }}</option>
              <option
                v-for="s in sprints"
                :key="s.id"
                :value="String(s.id)"
                :disabled="sprintLockedForUser(s, project?.role)"
              >
                {{ sprintOptionLabel(s, { activeSuffix: true, lockedSuffix: true }) }}
              </option>
            </select>
            <button
              type="button"
              class="btn btn-xs btn-outline-primary rounded-pill w-100"
              :disabled="bulkSprint === ''"
              @click="emit('bulk', 'set_sprint', { sprint_id: parseInt(bulkSprint, 10) })"
            >
              Move to sprint
            </button>
          </div>

          <div v-if="isKanban && canStatus" class="mb-3 border-top pt-2">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Move to column...</label>
            <select v-model="bulkStatus" class="form-select form-select-sm mb-2">
              <option value="">Select column...</option>
              <option v-for="st in statuses" :key="st.id" :value="String(st.id)">
                {{ st.name }}{{ st.is_done ? ' (done)' : '' }}{{ st.is_default ? ' (default)' : '' }}
              </option>
            </select>
            <button
              type="button"
              class="btn btn-xs btn-outline-primary rounded-pill w-100"
              :disabled="bulkStatus === ''"
              @click="emit('bulk', 'set_status', { status_id: parseInt(bulkStatus, 10) })"
            >
              Move to column
            </button>
          </div>

          <div v-if="canEditDetails" class="border-top pt-2">
            <label class="form-label text-muted small fw-bold text-uppercase mb-1 bulk-label">Due Date</label>
            <div class="btn-group btn-group-sm w-100 mb-2">
              <button type="button" class="btn btn-xs btn-outline-secondary" @click="setDue(daysFromToday(0))">Today</button>
              <button type="button" class="btn btn-xs btn-outline-secondary" @click="setDue(daysFromToday(1))">Tomorrow</button>
              <button type="button" class="btn btn-xs btn-outline-secondary" @click="setDue(daysFromToday(7))">+1 Wk</button>
              <button type="button" class="btn btn-xs btn-outline-secondary" @click="setDue('')">Clear</button>
            </div>
            <input v-model="bulkDate" type="date" class="form-control form-control-sm mb-2" />
            <div class="d-flex gap-2">
              <button
                type="button"
                class="btn btn-xs btn-outline-primary rounded-pill flex-grow-1"
                :disabled="!bulkDate"
                @click="setDue(bulkDate)"
              >
                Set Due
              </button>
              <button type="button" class="btn btn-xs btn-outline-secondary rounded-pill flex-grow-1" @click="setDue('')">
                Clear Due
              </button>
            </div>
          </div>
        </div>
      </div>

      <button v-if="canDelete" type="button" class="btn btn-xs btn-danger rounded-pill" @click="emit('bulk', 'delete')">Delete</button>
      <button type="button" class="btn btn-xs btn-link text-muted" @click="emit('deselect')">Deselect</button>
    </div>
  </div>
</template>

<style scoped>
.btn-xs {
  padding: 0.15rem 0.5rem;
  font-size: 0.75rem;
}
.bulk-label {
  font-size: 0.7rem;
}
</style>
