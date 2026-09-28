<script setup lang="ts">
import { computed } from 'vue'
import type { ProjectPermInfo } from '@/api/types'

const props = defineProps<{
  catalog: ProjectPermInfo[]
  modelValue: string[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const groups = computed(() => {
  const byGroup = new Map<string, ProjectPermInfo[]>()
  for (const item of props.catalog) {
    const key = item.group || 'Other'
    const list = byGroup.get(key) || []
    list.push(item)
    byGroup.set(key, list)
  }
  return [...byGroup.entries()]
})

function isChecked(id: string) {
  return props.modelValue.includes(id)
}

function toggle(id: string, checked: boolean) {
  if (props.disabled) return
  const next = new Set(props.modelValue)
  if (checked) next.add(id)
  else next.delete(id)
  emit('update:modelValue', [...next])
}
</script>

<template>
  <div class="role-perm-fields">
    <div v-for="[group, items] in groups" :key="group" class="mb-3">
      <h5 class="h6 mb-2">{{ group }}</h5>
      <div v-for="item in items" :key="item.id" class="form-check">
        <input
          :id="`perm-${item.id}`"
          class="form-check-input"
          type="checkbox"
          :checked="isChecked(item.id)"
          :disabled="disabled"
          @change="toggle(item.id, ($event.target as HTMLInputElement).checked)"
        />
        <label class="form-check-label" :for="`perm-${item.id}`">
          <span class="fw-semibold">{{ item.label }}</span>
          <span class="d-block small text-muted">{{ item.description }}</span>
        </label>
      </div>
    </div>
    <p v-if="!catalog.length" class="small text-muted mb-0">No permissions available.</p>
  </div>
</template>
