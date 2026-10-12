<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { Task, TaskTemplate } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  /** Project the list is showing; personal templates create there too. */
  projectId: number | null
  /** Board sprint for new tasks (kanban only). */
  sprintId: number | null
}>()

const emit = defineEmits<{ created: [task: Task] }>()

const toast = useToast()
const { askConfirm } = useConfirm()
const templates = ref<TaskTemplate[]>([])
const loading = ref(false)
const busyId = ref<number | null>(null)

const projectTemplates = computed(() => templates.value.filter((t) => t.project_id != null))
const personalTemplates = computed(() => templates.value.filter((t) => t.project_id == null))

async function load() {
  loading.value = true
  try {
    templates.value = await api.listTaskTemplates(props.projectId)
  } catch (err) {
    templates.value = []
    toast.push(err instanceof APIError ? err.message : 'Could not load templates', 'error')
  } finally {
    loading.value = false
  }
}

async function apply(t: TaskTemplate) {
  if (busyId.value) return
  busyId.value = t.id
  try {
    const task = await api.applyTaskTemplate(t.id, {
      project_id: t.project_id ?? props.projectId,
      sprint_id: props.sprintId,
    })
    toast.push(`Created “${task.title}” from ${t.name}`, 'success')
    emit('created', task)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create the task', 'error')
  } finally {
    busyId.value = null
  }
}

async function remove(t: TaskTemplate) {
  const ok = await askConfirm({
    title: 'Delete template?',
    message: `Delete the template “${t.name}”? Tasks already made from it are not affected.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTaskTemplate(t.id)
    templates.value = templates.value.filter((x) => x.id !== t.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete the template', 'error')
  }
}
</script>

<template>
  <div class="dropdown">
    <button
      class="btn btn-sm btn-outline-secondary dropdown-toggle rounded-pill px-2 px-sm-3 py-1"
      type="button"
      data-bs-toggle="dropdown"
      data-bs-auto-close="outside"
      title="New task from a template"
      @click="load"
    >
      <i class="bi bi-files" />
      <span class="d-none d-sm-inline ms-1">Templates</span>
    </button>
    <div class="dropdown-menu dropdown-menu-end shadow-sm border-0 py-1 templates-menu">
      <p v-if="loading" class="dropdown-item-text small text-muted mb-0">Loading…</p>
      <template v-else-if="templates.length">
        <template v-for="group in [
          { label: 'This project', items: projectTemplates },
          { label: 'My templates', items: personalTemplates },
        ]" :key="group.label">
          <h6 v-if="group.items.length" class="dropdown-header">{{ group.label }}</h6>
          <div v-for="t in group.items" :key="t.id" class="d-flex align-items-center">
            <button
              type="button"
              class="dropdown-item small flex-grow-1 text-truncate"
              :disabled="busyId !== null"
              :title="t.subtasks.length ? `${t.title} (+${t.subtasks.length} subtasks)` : t.title"
              @click="apply(t)"
            >
              <span v-if="busyId === t.id" class="spinner-border spinner-border-sm me-1" />
              {{ t.name }}
              <span v-if="t.subtasks.length" class="text-muted">· {{ t.subtasks.length }} subtasks</span>
            </button>
            <button
              v-if="t.can_manage"
              type="button"
              class="btn btn-sm btn-link text-muted px-2"
              :aria-label="`Delete template ${t.name}`"
              @click="remove(t)"
            >
              <i class="bi bi-x-lg" />
            </button>
          </div>
        </template>
      </template>
      <p v-else class="dropdown-item-text small text-muted mb-0">
        No templates yet. Open a task and choose <strong>Save as template</strong>.
      </p>
    </div>
  </div>
</template>

<style scoped>
.templates-menu {
  min-width: 16rem;
  max-width: 90vw;
  max-height: 60vh;
  overflow-y: auto;
}
</style>
