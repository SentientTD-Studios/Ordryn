<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api } from '@/api/client'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'

const props = defineProps<{
  pendingCount?: number
}>()

const route = useRoute()
const { hasPermission, hasSitePermission } = useAuth()
const { isCompanyMode } = useSite()
const fetchedCount = ref(0)

// Each section's site permission; admin implies all of them. Company sites have no
// invites or join requests.
const allLinks = [
  { name: 'admin', label: 'Settings', to: '/admin', perm: 'settings:manage' },
  { name: 'admin-extensions', label: 'Extensions', to: '/admin/extensions', perm: 'settings:manage' },
  { name: 'admin-requests', label: 'Requests', to: '/admin/requests', perm: 'admin', company: false },
  { name: 'admin-invites', label: 'Invites', to: '/admin/invites', perm: 'admin', company: false },
  { name: 'admin-users', label: 'Users', to: '/admin/users', perm: 'admin' },
  { name: 'admin-site-roles', label: 'Site roles', to: '/admin/site-roles', perm: 'roles:manage' },
  { name: 'admin-project-roles', label: 'Project roles', to: '/admin/roles', perm: 'admin' },
  { name: 'admin-audit', label: 'Audit log', to: '/admin/audit', perm: 'audit:view' },
  { name: 'admin-email-audit', label: 'Email log', to: '/admin/email-audit', perm: 'audit:view' },
  { name: 'admin-comment-audit', label: 'Comment history', to: '/admin/comment-audit', perm: 'audit:view' },
] as const

const links = computed(() =>
  allLinks.filter((l) => hasSitePermission(l.perm) && !(isCompanyMode.value && 'company' in l && !l.company)),
)

const activeName = computed(() => String(route.name || ''))
const badgeCount = computed(() => props.pendingCount ?? fetchedCount.value)

onMounted(async () => {
  if (props.pendingCount !== undefined || !hasPermission('admin') || isCompanyMode.value) return
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
