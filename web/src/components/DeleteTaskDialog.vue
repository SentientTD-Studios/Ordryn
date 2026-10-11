<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Task } from '@/api/types'
import ParentTaskCombobox from '@/components/ParentTaskCombobox.vue'

const props = defineProps<{
  open: boolean
  task: Task | null
  rootTasks: Task[]
}>()

const emit = defineEmits<{
  cancel: []
  cascade: []
  reparent: [newParentId: number | null]
}>()

const mode = ref<'cascade' | 'reparent'>('cascade')
const newParentId = ref<number | ''>('')

const childCount = computed(() => props.task?.child_count ?? props.task?.children?.length ?? 0)

const otherRoots = computed(() =>
  props.rootTasks.filter((t) => t.id !== props.task?.id && !t.parent_id),
)

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      mode.value = 'cascade'
      newParentId.value = ''
    }
  },
)

function confirm() {
  if (mode.value === 'cascade') {
    emit('cascade')
    return
  }
  emit('reparent', newParentId.value === '' ? null : Number(newParentId.value))
}
</script>

<template>
  <div
    v-if="open && task"
    class="modal fade show d-block"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    style="background: rgba(0, 0, 0, 0.45)"
    @click.self="emit('cancel')"
  >
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content" style="background: var(--ordryn-card-bg); color: var(--ordryn-text)">
        <div class="modal-header border-secondary-subtle">
          <h5 class="modal-title">Delete “{{ task.title }}”?</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="emit('cancel')" />
        </div>
        <div class="modal-body">
          <p class="mb-3">
            This moves the task to the trash, where you can restore it later. This task has
            <strong>{{ childCount }}</strong> subtask{{ childCount === 1 ? '' : 's' }}.
          </p>
          <div class="form-check mb-2">
            <input id="delete-cascade" v-model="mode" class="form-check-input" type="radio" value="cascade" />
            <label class="form-check-label" for="delete-cascade">
              Delete this task and all {{ childCount }} subtask{{ childCount === 1 ? '' : 's' }} (restorable together)
            </label>
          </div>
          <div class="form-check mb-2">
            <input id="delete-reparent" v-model="mode" class="form-check-input" type="radio" value="reparent" />
            <label class="form-check-label" for="delete-reparent">
              Move subtasks, then delete this task
            </label>
          </div>
          <div v-if="mode === 'reparent'" class="ms-4 mt-2">
            <label for="reparent-target" class="form-label small">Move subtasks to</label>
            <ParentTaskCombobox
              v-model="newParentId"
              input-id="reparent-target"
              :options="otherRoots"
              placeholder="Type to search parent tasks or projects…"
            />
          </div>
        </div>
        <div class="modal-footer border-secondary-subtle">
          <button type="button" class="btn btn-outline-secondary" @click="emit('cancel')">Cancel</button>
          <button type="button" class="btn btn-danger" @click="confirm">
            {{ mode === 'cascade' ? 'Delete all' : 'Move & delete' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
