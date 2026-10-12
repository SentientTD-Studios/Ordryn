<script setup lang="ts">
import type { Project, ProjectSprint } from '@/api/types'
import { sprintOptionLabel } from '@/utils/sprintLabel'

defineProps<{
  project: Project | null
  sprints: ProjectSprint[]
  selectedSprint: ProjectSprint | null
  showSelectAll: boolean
  allSelected: boolean
  selectAllDisabled: boolean
}>()

const sprintKey = defineModel<string>({ required: true })

const emit = defineEmits<{
  change: []
  'toggle-select-all': [checked: boolean]
}>()
</script>

<template>
  <div
    class="d-flex flex-wrap align-items-center gap-2 mb-3 p-2 rounded-3 border shadow-xs"
    style="background: var(--ordryn-card-bg); border-color: var(--ordryn-card-border) !important;"
  >
    <label class="small fw-bold mb-0" for="board-sprint">Sprint</label>
    <select
      id="board-sprint"
      v-model="sprintKey"
      class="form-select form-select-sm"
      style="max-width: 28rem;"
      @change="emit('change')"
    >
      <option value="backlog">{{ project?.backlog_name || 'Backlog' }}</option>
      <option v-for="s in sprints" :key="s.id" :value="String(s.id)">
        {{ sprintOptionLabel(s, { activeSuffix: true, lockedSuffix: true }) }}
      </option>
    </select>
    <span v-if="selectedSprint" class="small text-muted">
      <template v-if="selectedSprint.description">{{ selectedSprint.description }} · </template>
      <template v-if="selectedSprint.start_date && selectedSprint.end_date">
        {{ selectedSprint.start_date }} – {{ selectedSprint.end_date }}
        <template v-if="selectedSprint.lock_date">
          · {{ selectedSprint.is_locked ? 'locked' : 'locks' }} {{ selectedSprint.lock_date }}
        </template>
        ·
      </template>
      <span v-else class="badge text-bg-secondary me-1">dateless</span>
      {{ selectedSprint.task_count }} task{{ selectedSprint.task_count === 1 ? '' : 's' }}
    </span>
    <span v-else class="small text-muted">
      <template v-if="project?.backlog_description">{{ project.backlog_description }} · </template>
      Tasks not assigned to a sprint
    </span>
    <div v-if="showSelectAll" class="form-check align-items-center m-0 p-0 ms-auto d-flex">
      <input
        id="select-all-board-tasks"
        type="checkbox"
        class="form-check-input m-0 cursor-pointer"
        :checked="allSelected"
        :disabled="selectAllDisabled"
        style="width: 0.95rem; height: 0.95rem;"
        @change="emit('toggle-select-all', ($event.target as HTMLInputElement).checked)"
      />
      <label for="select-all-board-tasks" class="form-check-label small text-muted cursor-pointer ms-1.5">
        Select all
      </label>
    </div>
  </div>
</template>
