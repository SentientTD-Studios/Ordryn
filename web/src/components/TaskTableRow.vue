<script setup lang="ts">
import type { Task } from '@/api/types'

defineProps<{
  task: Task
  selected: boolean
}>()

const emit = defineEmits<{
  'toggle-select': [checked: boolean]
  'toggle-complete': []
  'toggle-favorite': []
  edit: []
  remove: []
}>()

function priorityBadgeClass(priority: number) {
  if (priority === 1) return 'bg-secondary'
  if (priority === 2) return 'bg-warning text-dark'
  if (priority === 3) return 'bg-danger'
  return ''
}

function priorityLabel(priority: number) {
  if (priority === 1) return 'Low'
  if (priority === 2) return 'Med'
  if (priority === 3) return 'High'
  return ''
}
</script>

<template>
  <tr :id="`task-${task.id}`" class="task-row">
    <td class="text-center align-middle select-column">
      <input
        type="checkbox"
        class="form-check-input task-select"
        :checked="selected"
        :aria-label="`Select task ${task.title}`"
        @change="emit('toggle-select', ($event.target as HTMLInputElement).checked)"
      />
    </td>
    <td class="text-center align-middle drag-column">
      <span class="drag-handle" style="cursor: move"><i class="bi bi-grip-vertical" /></span>
    </td>
    <td class="title-column">
      <div class="d-flex align-items-center flex-wrap">
        <button
          type="button"
          class="btn btn-link p-0 me-2 favorite-btn"
          style="text-decoration: none"
          :aria-label="task.favorite ? 'Unstar task' : 'Star task'"
          @click="emit('toggle-favorite')"
        >
          <i
            :class="task.favorite ? 'bi bi-star-fill' : 'bi bi-star'"
            :style="task.favorite ? 'color: gold' : ''"
          />
        </button>
        <button
          type="button"
          class="btn btn-link p-0 task-toggle title-text text-start"
          :title="task.created_at ? `Created: ${task.created_at}` : undefined"
        >
          {{ task.title }}
        </button>
        <span v-if="task.project" class="badge bg-secondary project-badge ms-1">{{ task.project }}</span>
        <span
          v-if="priorityLabel(task.priority)"
          class="badge priority-badge ms-1"
          :class="priorityBadgeClass(task.priority)"
        >{{ priorityLabel(task.priority) }}</span>
      </div>
    </td>
    <td class="tags-column">
      <div v-if="task.tags?.length" class="tag-list">
        <span
          v-for="tag in task.tags"
          :key="tag.id"
          class="tag-chip"
          :style="{ backgroundColor: tag.color || '#6c757d' }"
        >{{ tag.name }}</span>
      </div>
    </td>
    <td class="desc-column">
      <div v-if="task.description" class="desc-preview text-muted small">
        {{ task.description.length > 120 ? `${task.description.slice(0, 120)}…` : task.description }}
      </div>
    </td>
    <td class="date-added" data-label="Due Date">
      {{ task.due_date || '' }}
    </td>
    <td class="actions-column">
      <div class="d-flex align-items-center gap-2 justify-content-start">
        <button
          type="button"
          class="badge status-column"
          :class="task.completed ? 'bg-success' : 'bg-danger text-white'"
          style="cursor: pointer; border: none"
          @click="emit('toggle-complete')"
        >
          <i :class="task.completed ? 'bi bi-toggle-on' : 'bi bi-toggle-off'" />
          {{ task.completed ? 'Complete' : 'Incomplete' }}
        </button>
        <button
          type="button"
          class="btn btn-link p-0 mx-2 edit-btn"
          style="text-decoration: none"
          aria-label="Edit task"
          @click="emit('edit')"
        >
          <i class="bi bi-pencil" />
        </button>
        <button
          type="button"
          class="btn btn-link p-0 delete-column"
          style="text-decoration: none"
          aria-label="Delete task"
          @click="emit('remove')"
        >
          <i class="bi bi-trash text-danger" />
        </button>
      </div>
    </td>
  </tr>
</template>
