<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Task } from '@/api/types'
import TaskFieldBadges from '@/components/TaskFieldBadges.vue'
import type { ViewDensity } from '@/composables/useViewDensity'
import { hasImageMarkdown, previewWithoutImages } from '@/utils/taskCommentBody'

const props = withDefaults(
  defineProps<{
    task: Task
    selected: boolean
    density?: ViewDensity
    depth?: number
    showProjectPill?: boolean
    canWrite?: boolean
    expanded?: boolean
    focused?: boolean
    selecting?: boolean
  }>(),
  {
    density: 'comfortable',
    depth: 0,
    showProjectPill: true,
    canWrite: true,
    expanded: false,
    focused: false,
    selecting: false,
  },
)

const emit = defineEmits<{
  'toggle-select': [checked: boolean]
  'toggle-complete': []
  'toggle-expand': []
  'patch-task': [payload: { id: number; title?: string; description?: string }]
  'add-subtask': []
  edit: []
}>()

const isSubtask = () => props.depth > 0 || !!(props.task.parent_id && props.task.parent_id > 0)
const childTotal = () => props.task.child_count ?? props.task.children?.length ?? 0
const hasChildren = () => childTotal() > 0
const childProgress = () => {
  const total = childTotal()
  if (total <= 0) return ''
  const done = props.task.children_completed ?? 0
  return `${done}/${total}`
}

// Inline Editing State (Desktop) — description only; title opens task details
const isEditingDesc = ref(false)
const descVal = ref('')

function startEditDesc() {
  if (!props.canWrite) return
  descVal.value = props.task.description || ''
  isEditingDesc.value = true
}

function saveDesc() {
  if (!isEditingDesc.value) return
  isEditingDesc.value = false
  const trimmed = descVal.value.trim()
  if (trimmed !== (props.task.description || '')) {
    emit('patch-task', { id: props.task.id, description: trimmed })
  }
}

function cancelEditDesc() {
  isEditingDesc.value = false
}

const descriptionPreview = computed(() => {
  const text = props.task.description || ''
  if (hasImageMarkdown(text)) return previewWithoutImages(text)
  return text
})

function priorityLabel(priority: number) {
  if (priority === 1) return 'Low'
  if (priority === 2) return 'Med'
  if (priority === 3) return 'High'
  return ''
}

function formatDueDate(dateStr?: string): string {
  if (!dateStr) return ''
  const todayStr = new Date().toISOString().slice(0, 10)
  const currentYear = new Date().getFullYear().toString()
  const isOverdue = dateStr < todayStr
  const year = dateStr.slice(0, 4)
  if (isOverdue || year !== currentYear) {
    return dateStr
  }
  const dateObj = new Date(dateStr + 'T00:00:00')
  return dateObj.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

const isKanbanTask = () => props.task.project_workflow === 'kanban'

function formatMinutes(total: number) {
  if (total <= 0) return '0m'
  const h = Math.floor(total / 60)
  const m = total % 60
  if (h <= 0) return `${m}m`
  if (m <= 0) return `${h}h`
  return `${h}h ${m}m`
}
</script>

<template>
  <div
    :id="`task-card-${task.id}`"
    class="ordryn-task-card"
    tabindex="-1"
    :class="{
      'is-completed': task.completed,
      'is-nested': depth > 0,
      'has-children': hasChildren(),
      'is-expanded': expanded,
      'is-focused': focused,
      'density-comfortable': density === 'comfortable',
      'density-dense': density === 'dense',
    }"
  >
    <div
      v-if="depth > 0"
      class="nested-branch"
      aria-hidden="true"
    />

    <div class="d-flex align-items-center justify-content-between gap-2 flex-wrap flex-md-nowrap">
      <!-- Left Controls Container: Tight 5px gap moving everything slightly left -->
      <div class="d-flex align-items-center flex-grow-1 min-w-0" style="gap: 5px;">
        <!-- Expand / collapse subtasks (roots with children) -->
        <button
          v-if="depth === 0 && hasChildren()"
          type="button"
          class="btn btn-link p-0 m-0 border-0 text-decoration-none nest-toggle flex-shrink-0 d-inline-flex align-items-center justify-content-center"
          :aria-expanded="expanded"
          :aria-label="expanded ? 'Collapse subtasks' : 'Expand subtasks'"
          :title="expanded ? 'Collapse subtasks' : 'Expand subtasks'"
          @click="emit('toggle-expand')"
        >
          <i :class="expanded ? 'bi bi-chevron-down' : 'bi bi-chevron-right'" />
        </button>
        <span v-else-if="depth === 0" class="nest-toggle-spacer flex-shrink-0" aria-hidden="true" />

        <!-- Drag Handle -->
        <span
          class="drag-handle text-muted flex-shrink-0 d-none d-md-inline-flex align-items-center justify-content-center m-0 p-0"
          style="cursor: grab; opacity: 0.5;"
          title="Drag to reorder"
        >
          <i class="bi bi-grip-vertical" style="font-size: 1.15rem; line-height: 1;" />
        </span>

        <!-- Multi-Select Checkbox -->
        <div
          v-if="canWrite"
          class="hover-reveal flex-shrink-0 align-items-center justify-content-center m-0 p-0"
          :class="selecting || selected ? 'd-inline-flex is-visible' : 'd-none d-md-inline-flex'"
          title="Select task for bulk actions"
        >
          <input
            type="checkbox"
            class="cursor-pointer m-0 p-0"
            :checked="selected"
            style="width: 1.05rem; height: 1.05rem; cursor: pointer; accent-color: var(--ordryn-accent);"
            :aria-label="`Select task ${task.title}`"
            @change="emit('toggle-select', ($event.target as HTMLInputElement).checked)"
          />
        </div>

        <!-- Completion Checkmark Button -->
        <button
          type="button"
          class="completion-check-btn flex-shrink-0 d-inline-flex align-items-center justify-content-center m-0 p-0 border-0 bg-transparent"
          :title="task.completed ? 'Mark incomplete' : 'Mark complete'"
          :disabled="!canWrite"
          @click="emit('toggle-complete')"
        >
          <i v-if="task.completed" class="bi bi-check-circle-fill text-success" style="font-size: 1.15rem; line-height: 1;" />
          <span v-else class="position-relative d-inline-flex align-items-center justify-content-center">
            <i class="bi bi-circle" style="font-size: 1.15rem; line-height: 1;" />
            <i class="bi bi-check-lg text-success position-absolute top-50 start-50 translate-middle opacity-0 hover-check" style="font-size: 0.85rem;" />
          </span>
        </button>

        <!-- Title & Badges Container -->
        <div class="d-flex flex-column gap-1 min-w-0 flex-grow-1 ms-1">
          <div class="d-flex align-items-center gap-2 flex-wrap">
            <!-- Title — click opens task details -->
            <span
              class="task-title fw-semibold text-truncate"
              style="color: var(--ordryn-text); cursor: pointer;"
              title="Open task details"
              @click="emit('edit')"
            >
              {{ task.title }}
            </span>

            <!-- Subtask progress (also toggles expand) -->
            <button
              v-if="childProgress()"
              type="button"
              class="badge rounded-pill border px-2 py-1 small nest-progress-btn"
              :class="expanded ? 'nest-progress-btn-open' : 'nest-progress-btn-closed'"
              :title="expanded ? 'Collapse subtasks' : `Show ${childTotal()} subtask${childTotal() === 1 ? '' : 's'}`"
              @click="emit('toggle-expand')"
            >
              <i class="bi bi-diagram-3 me-1" />{{ childProgress() }}
              <span class="ms-1 opacity-75 d-none d-sm-inline">{{ expanded ? 'Hide' : 'Show' }}</span>
            </button>

            <!-- Project Badge Pill -->
            <span
              v-if="showProjectPill && task.project && !isSubtask()"
              class="badge rounded-pill bg-secondary bg-opacity-10 text-secondary border border-secondary border-opacity-20 px-2 py-1 small"
            >
              <i class="bi bi-folder2 me-1" />{{ task.project }}
            </span>
            <span
              v-if="depth === 0 && task.parent_title"
              class="badge rounded-pill bg-secondary bg-opacity-10 text-secondary border border-secondary border-opacity-20 px-2 py-1 small"
              :title="`Subtask of ${task.parent_title}`"
            >
              <i class="bi bi-diagram-3 me-1" />{{ task.parent_title }}
            </span>

            <!-- Kanban status -->
            <span
              v-if="isKanbanTask() && task.status_name"
              class="ordryn-badge ordryn-badge-status text-nowrap"
              title="Status"
            >
              {{ task.status_name }}
            </span>

            <!-- Kanban claimer -->
            <span
              v-if="isKanbanTask() && task.claimed_by_name"
              class="ordryn-badge text-nowrap text-bg-primary"
              title="Claimed by"
            >
              <i class="bi bi-person" />{{ task.claimed_by_name }}
            </span>

            <!-- Tag Badges -->
            <template v-if="task.tags && task.tags.length > 0">
              <span
                v-for="tag in task.tags"
                :key="tag.id"
                class="ordryn-badge ordryn-badge-tag"
              >
                {{ tag.name }}
              </span>
            </template>
            <TaskFieldBadges :task="task" />
          </div>

          <!-- Inline Description Editor or Preview (Comfortable mode only) -->
          <div v-if="density === 'comfortable'">
            <textarea
              v-if="isEditingDesc && canWrite"
              v-model="descVal"
              class="inline-edit-textarea mt-1"
              rows="2"
              @blur="saveDesc"
              @keyup.escape="cancelEditDesc"
            />
            <div
              v-else-if="task.description"
              class="task-description text-muted small text-break"
              :class="{ 'cursor-pointer': canWrite }"
              :title="canWrite ? 'Click to edit description' : undefined"
              @click="startEditDesc"
            >
              {{ descriptionPreview }}
            </div>
          </div>
        </div>
      </div>

      <!-- Right Group (Desktop): Due Date, Priority Badge, Edit/Delete Action Buttons -->
      <div class="d-none d-md-flex align-items-center gap-3 ms-auto flex-shrink-0">
        <!-- Smart Formatted Due Date (Icon hidden in Dense mode) -->
        <div v-if="task.due_date" class="d-flex align-items-center gap-1 text-muted small text-nowrap">
          <i v-if="density !== 'dense'" class="bi bi-calendar-event opacity-75" />
          <span>{{ formatDueDate(task.due_date) }}</span>
        </div>

        <!-- Kanban estimate / time logged -->
        <span
          v-if="isKanbanTask() && task.estimate_points != null"
          class="badge text-bg-light text-muted border text-nowrap"
          title="Estimate"
        >
          {{ task.estimate_points }} pts
        </span>
        <span
          v-if="isKanbanTask() && (task.time_spent_minutes ?? 0) > 0"
          class="d-flex align-items-center gap-1 text-muted small text-nowrap"
          title="Time logged"
        >
          <i v-if="density !== 'dense'" class="bi bi-clock opacity-75" />
          <span>{{ formatMinutes(task.time_spent_minutes ?? 0) }}</span>
        </span>

        <!-- Priority Pill -->
        <span
          v-if="priorityLabel(task.priority)"
          class="ordryn-badge text-nowrap"
          :class="{
            'ordryn-badge-priority-low': task.priority === 1,
            'ordryn-badge-priority-med': task.priority === 2,
            'ordryn-badge-priority-high': task.priority === 3,
          }"
        >
          Priority {{ priorityLabel(task.priority) }}
        </span>

        <!-- Edit / Delete Actions (Hover-revealed on Desktop, hidden if viewer/read-only) -->
        <div v-if="canWrite" class="d-flex align-items-center gap-1 action-buttons-group hover-reveal">
          <button
            v-if="!isSubtask()"
            type="button"
            class="btn btn-sm btn-icon text-muted hover-accent border-0 p-1"
            title="Add subtask"
            @click="emit('add-subtask')"
          >
            <i class="bi bi-node-plus" />
          </button>
          <button
            type="button"
            class="btn btn-sm btn-icon text-muted hover-accent border-0 p-1"
            title="Open task details editor"
            @click="emit('edit')"
          >
            <i class="bi bi-pencil" />
          </button>
        </div>
      </div>
    </div>

    <!-- Mobile Action Row (Placed below task on mobile screens) -->
    <div
      v-if="task.due_date || isKanbanTask() || priorityLabel(task.priority) || canWrite"
      class="d-md-none mobile-actions-wrapper"
    >
      <div class="d-flex align-items-center gap-2 justify-content-between w-100">
        <div class="d-flex align-items-center gap-2 flex-wrap min-w-0">
          <div v-if="task.due_date" class="text-muted small text-nowrap">
            <i v-if="density !== 'dense'" class="bi bi-calendar-event me-1" />{{ formatDueDate(task.due_date) }}
          </div>
          <span
            v-if="isKanbanTask() && task.estimate_points != null"
            class="badge text-bg-light text-muted border"
            title="Estimate"
          >
            {{ task.estimate_points }} pts
          </span>
          <span
            v-if="isKanbanTask() && (task.time_spent_minutes ?? 0) > 0"
            class="text-muted small text-nowrap"
            title="Time logged"
          >
            <i v-if="density !== 'dense'" class="bi bi-clock me-1" />{{ formatMinutes(task.time_spent_minutes ?? 0) }}
          </span>
          <span
            v-if="priorityLabel(task.priority)"
            class="ordryn-badge"
            :class="{
              'ordryn-badge-priority-low': task.priority === 1,
              'ordryn-badge-priority-med': task.priority === 2,
              'ordryn-badge-priority-high': task.priority === 3,
            }"
          >
            {{ priorityLabel(task.priority) }}
          </span>
          <TaskFieldBadges :task="task" />
        </div>
        <div v-if="canWrite" class="d-flex align-items-center gap-1 flex-shrink-0">
          <button
            v-if="!isSubtask()"
            type="button"
            class="btn btn-sm btn-outline-secondary oryryn-icon-btn"
            title="Add subtask"
            aria-label="Add subtask"
            @click="emit('add-subtask')"
          >
            <i class="bi bi-node-plus" />
          </button>
          <button
            type="button"
            class="btn btn-sm btn-outline-secondary oryryn-icon-btn"
            title="Open task details"
            aria-label="Open task details"
            @click="emit('edit')"
          >
            <i class="bi bi-pencil" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.completion-check-btn:hover .hover-check {
  opacity: 0.6 !important;
}
.btn-icon {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  transition: background-color 0.15s ease;
}
.btn-icon:hover {
  background-color: var(--ordryn-muted-bg);
}
.hover-danger:hover {
  background-color: rgba(239, 68, 68, 0.1) !important;
}
.nest-toggle {
  width: 1.25rem;
  height: 1.25rem;
  color: var(--ordryn-muted);
  line-height: 1;
}
.nest-toggle:hover {
  color: var(--ordryn-accent);
}
.nest-toggle-spacer {
  width: 1.25rem;
  height: 1.25rem;
}
.nest-progress-btn {
  cursor: pointer;
  font-weight: 600;
  background: color-mix(in srgb, var(--ordryn-accent) 14%, transparent);
  color: var(--ordryn-accent);
  border-color: color-mix(in srgb, var(--ordryn-accent) 35%, transparent) !important;
}
.nest-progress-btn:hover {
  background: color-mix(in srgb, var(--ordryn-accent) 22%, transparent);
}
.ordryn-icon-btn {
  min-width: 2.25rem;
  min-height: 2.25rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.25rem;
}
</style>
