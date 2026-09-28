<script setup lang="ts">
import type { ProjectMember, ProjectRoleDef } from '@/api/types'

const props = withDefaults(
  defineProps<{
    members: ProjectMember[]
    locked?: boolean
    editable?: boolean
    roles?: ProjectRoleDef[]
    emptyText?: string
  }>(),
  {
    locked: false,
    editable: false,
    roles: () => [],
    emptyText: 'No members on this project.',
  },
)

const emit = defineEmits<{
  'role-change': [userId: number, role: string]
  remove: [userId: number]
}>()

const canEdit = (member: ProjectMember) =>
  props.editable && !props.locked && member.role !== 'owner'
</script>

<template>
  <ul v-if="members.length" class="list-unstyled mb-0">
    <li v-for="m in members" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-1">
      <span>{{ m.user_name || m.email }}</span>
      <span class="badge text-bg-secondary">{{ m.role_name || m.role }}</span>
      <span v-if="m.inherited || (locked && m.role !== 'owner')" class="badge text-bg-info">from org</span>
      <template v-if="canEdit(m)">
        <select
          class="form-select form-select-sm w-auto"
          :value="m.role"
          :aria-label="`Change role for ${m.user_name || m.email}`"
          @change="emit('role-change', m.user_id, ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="r in roles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
        </select>
        <button class="btn btn-sm btn-outline-danger" type="button" @click="emit('remove', m.user_id)">
          Remove
        </button>
      </template>
    </li>
  </ul>
  <p v-else class="small text-muted mb-0">{{ emptyText }}</p>
</template>
