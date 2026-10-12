<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { SavedView, SavedViewFilter } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  open: boolean
  /** The filters to save, already trimmed to what applies to the current view. */
  filter: SavedViewFilter
}>()

const emit = defineEmits<{
  close: []
  saved: [view: SavedView]
}>()

const toast = useToast()
const name = ref('')
const saving = ref(false)

watch(
  () => props.open,
  (open) => {
    if (open) name.value = ''
  },
)

async function save() {
  const trimmed = name.value.trim()
  if (!trimmed || saving.value) return
  saving.value = true
  try {
    const created = await api.createSavedView({ name: trimmed, filter: props.filter })
    toast.push('View saved successfully!', 'success')
    emit('saved', created)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to save view', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal fade show d-block" style="background: rgba(0,0,0,0.5);" tabindex="-1">
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <div class="modal-header border-0 pb-0">
          <h5 class="modal-title fw-bold">Save Current View</h5>
          <button type="button" class="btn-close" @click="emit('close')" />
        </div>
        <div class="modal-body py-3">
          <p class="text-muted small mb-3">Save your current active filters into a custom named View in the sidebar.</p>
          <div class="mb-3">
            <label for="new-view-name" class="form-label small fw-bold">View Name</label>
            <input
              id="new-view-name"
              v-model="name"
              type="text"
              class="form-control"
              placeholder="e.g., High Priority Work"
              @keyup.enter="save"
            />
          </div>
        </div>
        <div class="modal-footer border-0 pt-0 justify-content-end gap-2">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('close')">Cancel</button>
          <button type="button" class="btn btn-sm btn-primary px-3" :disabled="!name.trim() || saving" @click="save">Save View</button>
        </div>
      </div>
    </div>
  </div>
</template>
