<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, RouterView, useRouter } from 'vue-router'
import { withBase } from '@/base'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useTheme } from '@/composables/useTheme'
import { useToast } from '@/composables/useToast'
import { api } from '@/api/client'
import ToastHost from '@/components/ToastHost.vue'
import TaskSidebar from '@/components/TaskSidebar.vue'

const changelogHref = withBase('/changelog')

const { isAuthenticated, user, logout, hasPermission } = useAuth()
const { siteInfo, siteName, refresh: refreshSite } = useSite()
const { theme, toggleTheme } = useTheme()
const { push } = useToast()
const router = useRouter()
const overdueCount = ref(0)

const showAnnouncement = computed(
  () =>
    !!siteInfo.value?.enable_global_announcement &&
    !!siteInfo.value?.global_announcement_text &&
    !siteInfo.value?.announcement_dismissed,
)

async function loadOverdue() {
  if (!isAuthenticated.value) return
  try {
    const stats = await api.dashboard()
    overdueCount.value = stats.overdue_count
  } catch {
    overdueCount.value = 0
  }
}

async function dismissAnnouncement() {
  try {
    await api.dismissAnnouncement()
    if (siteInfo.value) {
      siteInfo.value = { ...siteInfo.value, announcement_dismissed: true }
    }
  } catch {
    if (siteInfo.value) {
      siteInfo.value = { ...siteInfo.value, announcement_dismissed: true }
    }
  }
}

onMounted(() => {
  void loadOverdue()
  void refreshSite()
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
  <nav class="navbar navbar-expand-lg">
    <div class="container">
      <RouterLink class="navbar-brand" to="/">{{ siteName }}</RouterLink>
      <button
        class="navbar-toggler"
        type="button"
        data-bs-toggle="collapse"
        data-bs-target="#navbarNav"
        aria-controls="navbarNav"
        aria-expanded="false"
        aria-label="Toggle navigation"
      >
        <span class="navbar-toggler-icon"></span>
      </button>
      <div id="navbarNav" class="collapse navbar-collapse">
        <ul class="navbar-nav me-auto">
          <li class="nav-item">
            <RouterLink class="nav-link" to="/">Home</RouterLink>
          </li>
          <template v-if="isAuthenticated">
            <li class="nav-item">
              <RouterLink class="nav-link" to="/dashboard">
                Dashboard
                <span
                  v-if="overdueCount > 0"
                  class="badge bg-danger ms-1"
                  :title="`${overdueCount} overdue tasks`"
                >{{ overdueCount }}</span>
              </RouterLink>
            </li>
            <li class="nav-item">
              <RouterLink class="nav-link" to="/settings">Calendar</RouterLink>
            </li>
          </template>
          <li class="nav-item">
            <a class="nav-link" :href="changelogHref" target="_blank" rel="noopener">About</a>
          </li>
          <li class="nav-item">
            <RouterLink class="nav-link" to="/docs/api/v1">API</RouterLink>
          </li>
          <template v-if="isAuthenticated">
            <li class="nav-item">
              <RouterLink class="nav-link" to="/projects">Projects</RouterLink>
            </li>
            <li v-if="hasPermission('admin')" class="nav-item">
              <RouterLink class="nav-link" to="/admin">Admin</RouterLink>
            </li>
            <li v-if="hasPermission('createinvites')" class="nav-item">
              <RouterLink class="nav-link" to="/invites">Create Invite</RouterLink>
            </li>
          </template>
        </ul>
        <div class="d-flex align-items-center gap-3">
          <span class="me-2"><i class="bi bi-sun-fill"></i></span>
          <button
            type="button"
            class="theme-toggle"
            :class="{ active: theme === 'dark' }"
            aria-label="Toggle dark/light mode"
            @click="toggleTheme"
          />
          <span class="ms-2"><i class="bi bi-moon-fill"></i></span>
          <template v-if="isAuthenticated">
            <span class="text-muted me-2 navbar-user-email d-none d-md-inline">{{ user?.email }}</span>
            <RouterLink to="/settings" class="btn btn-outline-secondary btn-sm me-2" title="Profile settings">
              <i class="bi bi-person-circle"></i> Profile
            </RouterLink>
            <button type="button" class="btn btn-outline-danger btn-sm" @click="onLogout">
              <i class="bi bi-box-arrow-right"></i> Logout
            </button>
          </template>
          <template v-else>
            <RouterLink to="/register" class="btn btn-outline-secondary btn-sm me-2">
              <i class="bi bi-person-plus"></i> Sign Up
            </RouterLink>
            <RouterLink to="/login" class="btn btn-outline-primary btn-sm">
              <i class="bi bi-box-arrow-in-right"></i> Login
            </RouterLink>
          </template>
        </div>
      </div>
    </div>
  </nav>

  <div
    v-if="showAnnouncement && siteInfo?.global_announcement_text"
    id="global-announcement"
    class="global-announcement-wrapper"
  >
    <div class="container">
      <div class="alert alert-primary alert-dismissible fade show mb-0" role="alert">
        <i class="bi bi-megaphone-fill me-2" />
        <strong>{{ siteInfo.global_announcement_text }}</strong>
        <button
          type="button"
          class="btn-close no-invert"
          aria-label="Close"
          @click="dismissAnnouncement"
        />
      </div>
    </div>
  </div>

  <main class="site-main" data-page="spa">
    <RouterView />
  </main>

  <footer class="container text-center mt-5 app-footer">
    <hr>
    <p class="text-muted">
      Made with <i class="bi bi-heart-fill text-danger"></i> by xNifty |
      <a href="https://github.com/xNifty/GoTodo" target="_blank" rel="noopener noreferrer">Source</a> |
      <RouterLink to="/docs/api/v1">Documentation</RouterLink> |
      <a href="https://github.com/xNifty/GoTodo/issues" target="_blank" rel="noopener noreferrer">Report a Bug</a> |
      <a href="https://ordryn.com" target="_blank" rel="noopener noreferrer">Powered by Ordryn ©</a>
    </p>
  </footer>

  <ToastHost />
  <TaskSidebar v-if="isAuthenticated" />
</template>
