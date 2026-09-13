<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Notification } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useTheme } from '@/composables/useTheme'
import { useToast } from '@/composables/useToast'
import { usePageActivity } from '@/composables/usePageActivity'

const emit = defineEmits<{
  'toggle-mobile-nav': []
}>()

const props = defineProps<{
  mobileNavOpen?: boolean
}>()

const { isAuthenticated, user, logout, hasPermission } = useAuth()
const { siteName, siteInfo } = useSite()
const { currentTheme, availableThemes, setTheme } = useTheme()
const { push } = useToast()
const router = useRouter()

const showChangelog = computed(() => siteInfo.value?.show_changelog !== false)
const canViewInvites = computed(
  () => hasPermission('admin') || hasPermission('createinvites') || !!siteInfo.value?.allow_user_invites,
)

const notifications = ref<Notification[]>([])
const unreadCount = ref(0)
const notifLoading = ref(false)

async function refreshUnreadCount() {
  if (!isAuthenticated.value) {
    unreadCount.value = 0
    return
  }
  try {
    const res = await api.unreadNotificationCount()
    unreadCount.value = res.unread_count
  } catch {
    /* ignore page-refresh errors */
  }
}

async function loadNotifications() {
  if (!isAuthenticated.value) return
  notifLoading.value = true
  try {
    const list = await api.listNotifications({ page: 1, per_page: 15 })
    notifications.value = list.notifications
    unreadCount.value = list.unread_count
  } catch {
    /* ignore */
  } finally {
    notifLoading.value = false
  }
}

async function onOpenNotifications() {
  await loadNotifications()
}

async function markOneRead(n: Notification) {
  if (n.read_at) return
  try {
    await api.markNotificationRead(n.id)
    n.read_at = new Date().toISOString()
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  } catch {
    /* ignore */
  }
}

async function markAllRead() {
  try {
    await api.markAllNotificationsRead()
    notifications.value = notifications.value.map((n) => ({
      ...n,
      read_at: n.read_at || new Date().toISOString(),
    }))
    unreadCount.value = 0
  } catch (err) {
    push(err instanceof Error ? err.message : 'Could not mark notifications read', 'error')
  }
}

async function openNotification(n: Notification) {
  await markOneRead(n)
  if (n.type === 'join_request') {
    await router.push({ name: 'admin-requests' })
    return
  }
  if (n.task_id) {
    await router.push({ name: 'task', params: { id: String(n.task_id) } })
    return
  }
  if (n.project_id) {
    await router.push({ name: 'tasks', query: { project: String(n.project_id) } })
  }
}

watch(isAuthenticated, (ok) => {
  if (ok) void refreshUnreadCount()
  else {
    notifications.value = []
    unreadCount.value = 0
  }
})

usePageActivity(() => {
  if (isAuthenticated.value) void refreshUnreadCount()
})

onMounted(() => {
  if (isAuthenticated.value) void refreshUnreadCount()
})

async function onLogout() {
  try {
    await logout()
    push('Signed out', 'info')
    await router.push({ name: 'login' })
  } catch (err) {
    push(err instanceof Error ? err.message : 'Logout failed', 'error')
  }
}
</script>

<template>
  <header class="ordryn-header border-bottom py-2 px-2 px-sm-3">
    <div class="d-flex align-items-center justify-content-between gap-2 min-w-0">
      <!-- Left: Mobile Toggle & Brand Logo -->
      <div class="d-flex align-items-center gap-2 gap-sm-3 min-w-0">
        <button
          type="button"
          class="btn btn-link text-decoration-none p-1 d-md-none oryryn-header-menu-btn"
          :aria-label="props.mobileNavOpen ? 'Close menu' : 'Open menu'"
          :aria-expanded="!!props.mobileNavOpen"
          aria-controls="ordryn-mobile-nav"
          @click="emit('toggle-mobile-nav')"
        >
          <i :class="props.mobileNavOpen ? 'bi bi-x-lg' : 'bi bi-list'" />
        </button>

        <RouterLink to="/" class="d-flex align-items-center gap-2 text-decoration-none min-w-0" style="color: var(--ordryn-text);">
          <div class="brand-logo-icon d-flex align-items-center justify-content-center rounded-3 px-2 py-1 flex-shrink-0" style="background: var(--ordryn-accent-light); color: var(--ordryn-accent); font-weight: 800;">
            <i class="bi bi-layers-half" style="font-size: 1.2rem;" />
          </div>
          <span class="fw-bold fs-5 tracking-tight text-truncate oryryn-brand-name">{{ siteName }}</span>
        </RouterLink>

        <!-- Top Navigation Links (Desktop) -->
        <nav class="d-none d-md-flex align-items-center gap-3 ms-4">
          <template v-if="showChangelog">
            <a
              href="#changelogModal"
              data-bs-toggle="modal"
              data-bs-target="#changelogModal"
              class="nav-link-item text-decoration-none small fw-medium"
              style="color: var(--ordryn-muted);"
            >Changelog</a>
          </template>
          <a
            href="#shortcutsModal"
            data-bs-toggle="modal"
            data-bs-target="#shortcutsModal"
            class="nav-link-item text-decoration-none small fw-medium"
            style="color: var(--ordryn-muted);"
          >Shortcuts</a>
          <RouterLink to="/docs/guide" class="nav-link-item text-decoration-none small fw-medium" style="color: var(--ordryn-muted);">How to use</RouterLink>
          <RouterLink to="/docs/api/v1" class="nav-link-item text-decoration-none small fw-medium" style="color: var(--ordryn-muted);">API</RouterLink>
          <template v-if="isAuthenticated">
            <RouterLink v-if="hasPermission('admin')" to="/admin" class="nav-link-item text-decoration-none small fw-medium" style="color: var(--ordryn-muted);">Admin</RouterLink>
            <RouterLink v-if="canViewInvites" to="/invites" class="nav-link-item text-decoration-none small fw-medium" style="color: var(--ordryn-muted);">Invites</RouterLink>
          </template>
        </nav>
      </div>

      <!-- Right Actions: Multi-Theme Dropdown & Auth Controls -->
      <div class="d-flex align-items-center gap-1 gap-sm-2 flex-shrink-0">
        <!-- Theme Picker Dropdown -->
        <div class="dropdown">
          <button
            class="btn btn-sm dropdown-toggle d-flex align-items-center gap-1 border-0 shadow-none"
            type="button"
            data-bs-toggle="dropdown"
            aria-expanded="false"
            aria-label="Theme"
            style="background: var(--ordryn-muted-bg); color: var(--ordryn-text);"
          >
            <i :class="availableThemes.find(t => t.id === currentTheme)?.icon || 'bi-palette'" />
            <span class="d-none d-sm-inline ms-1 capitalize">{{ currentTheme }}</span>
          </button>
          <ul class="dropdown-menu dropdown-menu-end shadow-sm border-0 mt-1">
            <li v-for="t in availableThemes" :key="t.id">
              <button
                class="dropdown-item d-flex align-items-center gap-2 small py-2"
                :class="{ active: currentTheme === t.id }"
                @click="setTheme(t.id)"
              >
                <i :class="t.icon" />
                <span>{{ t.label }}</span>
              </button>
            </li>
          </ul>
        </div>

        <template v-if="isAuthenticated">
          <div class="dropdown me-0">
            <button
              class="btn btn-sm btn-outline-secondary position-relative d-flex align-items-center"
              type="button"
              data-bs-toggle="dropdown"
              aria-expanded="false"
              aria-label="Notifications"
              title="Notifications"
              @click="onOpenNotifications"
            >
              <i class="bi bi-bell" />
              <span
                v-if="unreadCount > 0"
                class="position-absolute top-0 start-100 translate-middle badge rounded-pill text-bg-danger"
                style="font-size: 0.65rem;"
              >
                {{ unreadCount > 99 ? '99+' : unreadCount }}
              </span>
            </button>
            <div class="dropdown-menu dropdown-menu-end shadow border-0 p-0 notif-dropdown">
              <div class="d-flex align-items-center justify-content-between px-3 py-2 border-bottom">
                <strong class="small">Notifications</strong>
                <button
                  v-if="unreadCount > 0"
                  type="button"
                  class="btn btn-link btn-sm p-0 text-decoration-none"
                  @click.stop="markAllRead"
                >
                  Mark all read
                </button>
              </div>
              <div class="notif-list">
                <div v-if="notifLoading" class="px-3 py-3 text-muted small">Loading…</div>
                <div v-else-if="!notifications.length" class="px-3 py-3 text-muted small">
                  No notifications yet.
                </div>
                <button
                  v-for="n in notifications"
                  :key="n.id"
                  type="button"
                  class="dropdown-item notif-item text-wrap"
                  :class="{ 'notif-unread': !n.read_at }"
                  @click="openNotification(n)"
                >
                  <div class="fw-semibold small">{{ n.title }}</div>
                  <div class="small text-muted">{{ n.body }}</div>
                  <div v-if="n.actor_name" class="small text-muted mt-1">{{ n.actor_name }}</div>
                </button>
              </div>
            </div>
          </div>
          <span class="text-muted small d-none d-lg-inline me-1">{{ user?.user_name || user?.email }}</span>
          <RouterLink to="/settings" class="btn btn-outline-secondary btn-sm d-none d-md-flex align-items-center gap-1" title="Profile Settings">
            <img
              v-if="user?.avatar_url"
              :src="user.avatar_url"
              alt=""
              class="rounded-circle header-avatar-img"
              aria-hidden="true"
            />
            <i v-else class="bi bi-person-circle" />
            <span class="d-none d-sm-inline">Profile</span>
          </RouterLink>
          <button type="button" class="btn btn-outline-danger btn-sm d-none d-md-flex align-items-center gap-1" @click="onLogout">
            <i class="bi bi-box-arrow-right" />
            <span class="d-none d-sm-inline">Logout</span>
          </button>
        </template>
        <template v-else>
          <RouterLink to="/register" class="btn btn-outline-secondary btn-sm d-none d-sm-inline-block">Sign Up</RouterLink>
          <RouterLink to="/login" class="btn btn-primary btn-sm">Login</RouterLink>
        </template>
      </div>
    </div>
  </header>
</template>

<style scoped>
.ordryn-header {
  background-color: var(--ordryn-header-bg);
  border-color: var(--ordryn-header-border) !important;
  position: sticky;
  top: 0;
  z-index: 1050;
  padding-top: max(0.5rem, env(safe-area-inset-top));
}
.ordryn-header-menu-btn {
  color: var(--ordryn-text);
  font-size: 1.5rem;
  line-height: 1;
  min-width: 2.5rem;
  min-height: 2.5rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.ordryn-brand-name {
  max-width: min(42vw, 12rem);
}
.nav-link-item:hover {
  color: var(--ordryn-text) !important;
}
.brand-logo-icon {
  width: 32px;
  height: 32px;
}
.notif-dropdown {
  width: min(22rem, 92vw);
}
.notif-list {
  max-height: 20rem;
  overflow-y: auto;
}
.notif-item {
  white-space: normal;
  border-bottom: 1px solid color-mix(in srgb, var(--ordryn-muted) 18%, transparent);
  padding: 0.65rem 1rem;
}
.notif-unread {
  background: color-mix(in srgb, var(--ordryn-accent) 10%, transparent);
}
.header-avatar-img {
  width: 18px;
  height: 18px;
  object-fit: cover;
}
</style>
