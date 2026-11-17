<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

// Project settings → Details: offer this project as a starting point for new ones.
const props = defineProps<{ projectId: number; canManage: boolean }>()
const { push } = useToast()
const isTemplate = ref(false)
const loaded = ref(false)
const saving = ref(false)

onMounted(async () => {
  try {
    isTemplate.value = (await api.getProjectTemplateFlag(props.projectId)).is_template
  } catch {
    // Leave the switch off.
  } finally {
    loaded.value = true
  }
})

async function toggle() {
  saving.value = true
  const next = !isTemplate.value
  try {
    isTemplate.value = (await api.setProjectTemplateFlag(props.projectId, next)).is_template
    push(next ? 'People can now start projects from this one' : 'No longer a template', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="loaded && (canManage || isTemplate)" class="mb-3">
    <div class="form-check form-switch">
      <input
        :id="`project-template-${projectId}`"
        type="checkbox"
        class="form-check-input"
        role="switch"
        :checked="isTemplate"
        :disabled="!canManage || saving"
        @change="toggle"
      />
      <label class="form-check-label small fw-bold" :for="`project-template-${projectId}`">Use as a template</label>
    </div>
    <div class="form-text">
      Anyone who can open this project can start a new project from it. That copies its board columns, roles, tags,
      sprint settings, and automation rules, and optionally its open tasks. Members and history aren't copied.
    </div>
  </div>
</template>
