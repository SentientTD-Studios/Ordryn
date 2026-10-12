<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskTemplateInput } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { formatLocalDate } from '@/utils/quickAdd'

const props = defineProps<{
  open: boolean
  /** The task as it is in the form now; name and due offset are filled here. */
  draft: Omit<TaskTemplateInput, 'name' | 'due_offset_days'>
  /** The task's due date, used to suggest a relative due date. */
  dueDate: string
  /** Project to share with, when the user may create tasks there. */
  shareProjectId: number | null
  shareProjectName: string
}>()

const emit = defineEmits<{ close: [] }>()

const toast = useToast()
const name = ref('')
const share = ref(false)
const dueOffset = ref<number | ''>('')
const saving = ref(false)

function daysUntil(date: string): number | '' {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return ''
  const today = new Date(`${formatLocalDate(new Date())}T00:00:00`)
  const due = new Date(`${date}T00:00:00`)
  const days = Math.round((due.getTime() - today.getTime()) / 86_400_000)
  return days >= 0 && days <= 365 ? days : ''
}

watch(
  () => props.open,
  (open) => {
    if (!open) return
    name.value = props.draft.title.slice(0, 80)
    share.value = false
    dueOffset.value = daysUntil(props.dueDate)
  },
)

async function save() {
  if (!name.value.trim() || saving.value) return
  saving.value = true
  try {
    await api.createTaskTemplate({
      ...props.draft,
      name: name.value.trim(),
      due_offset_days: dueOffset.value === '' ? null : Number(dueOffset.value),
      project_id: share.value ? props.shareProjectId : null,
    })
    toast.push(`Saved template “${name.value.trim()}”`, 'success')
    emit('close')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save the template', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal fade show d-block" style="background: rgba(0,0,0,0.5); z-index: 2000;" tabindex="-1">
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <div class="modal-header border-0 pb-0">
          <h5 class="modal-title fw-bold">Save as template</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
        </div>
        <div class="modal-body py-3">
          <p class="text-muted small mb-3">
            Saves the title, description, priority, tags<template v-if="draft.estimate_points != null">, estimate</template><template
              v-if="draft.subtasks.length"> and {{ draft.subtasks.length }} subtask{{ draft.subtasks.length === 1 ? '' : 's' }}</template>.
            Use it from <strong>Templates</strong> above the task list.
          </p>
          <div class="mb-3">
            <label for="template-name" class="form-label small fw-bold">Template name</label>
            <input id="template-name" v-model="name" type="text" class="form-control" maxlength="80" @keyup.enter="save" />
          </div>
          <div class="mb-3">
            <label for="template-due" class="form-label small fw-bold">Due date (optional)</label>
            <div class="input-group input-group-sm" style="max-width: 16rem;">
              <span class="input-group-text">Due in</span>
              <input id="template-due" v-model="dueOffset" type="number" min="0" max="365" class="form-control" placeholder="—" />
              <span class="input-group-text">days</span>
            </div>
            <div class="form-hint">Leave empty for no due date.</div>
          </div>
          <div v-if="shareProjectId" class="form-check">
            <input id="template-share" v-model="share" type="checkbox" class="form-check-input" />
            <label for="template-share" class="form-check-label small">
              Share with everyone in <strong>{{ shareProjectName }}</strong>
            </label>
          </div>
        </div>
        <div class="modal-footer border-0 pt-0 justify-content-end gap-2">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('close')">Cancel</button>
          <button type="button" class="btn btn-sm btn-primary px-3" :disabled="!name.trim() || saving" @click="save">
            Save template
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
