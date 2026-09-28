<script setup lang="ts">
import type { ProjectMember } from '@/api/types'

defineProps<{
  members: ProjectMember[]
  locked?: boolean
  emptyText?: string
}>()
</script>

<template>
  <ul v-if="members.length" class="list-unstyled mb-0">
    <li v-for="m in members" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-1">
      <span>{{ m.user_name || m.email }}</span>
      <span class="badge text-bg-secondary">{{ m.role_name || m.role }}</span>
      <span v-if="m.inherited || (locked && m.role !== 'owner')" class="badge text-bg-info">from org</span>
    </li>
  </ul>
  <p v-else class="small text-muted mb-0">{{ emptyText || 'No members on this project.' }}</p>
</template>
