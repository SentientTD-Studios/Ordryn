<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api } from '@/api/client'

const props = defineProps<{
  pendingCount?: number
}>()

const route = useRoute()
const fetchedCount = ref(0)

const links = [
  { name: 'admin', label: 'Settings', to: '/admin' },
  { name: 'admin-mods', label: 'Mods', to: '/admin/mods' },
  { name: 'admin-requests', label: 'Requests', to: '/admin/requests' },
  { name: 'admin-invites', label: 'Invites', to: '/admin/invites' },
  { name: 'admin-users', label: 'Users', to: '/admin/users' },
  { name: 'admin-email-audit', label: 'Email log', to: '/admin/email-audit' },
  { name: 'admin-comment-audit', label: 'Comment history', to: '/admin/comment-audit' },
] as const

const activeName = computed(() => String(route.name || ''))
const badgeCount = computed(() => props.pendingCount ?? fetchedCount.value)

onMounted(async () => {
  if (props.pendingCount !== undefined) return
  try {
    const list = await api.listAdminJoinRequests()
    fetchedCount.value = list.filter((r) => r.status === 'pending').length
  } catch {
    fetchedCount.value = 0
  }
})
</script>

<template>
  <nav class="admin-subnav mb-3" aria-label="Admin sections">
    <ul class="nav nav-pills gap-1">
      <li v-for="link in links" :key="link.name" class="nav-item">
        <RouterLink
          class="nav-link"
          :class="{ active: activeName === link.name }"
          :to="link.to"
        >
          {{ link.label }}
          <span
            v-if="link.name === 'admin-requests' && badgeCount > 0"
            class="badge rounded-pill text-bg-danger ms-1"
          >{{ badgeCount > 99 ? '99+' : badgeCount }}</span>
        </RouterLink>
      </li>
    </ul>
  </nav>
</template>
