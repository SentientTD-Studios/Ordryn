<script setup lang="ts">
import { RouterLink, RouterView, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import ToastHost from '@/components/ToastHost.vue'

const { isAuthenticated, logout } = useAuth()
const { push } = useToast()
const router = useRouter()

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
  <div class="shell">
    <header class="topbar">
      <RouterLink class="brand" to="/">Ordryn</RouterLink>
      <nav v-if="isAuthenticated" class="nav">
        <RouterLink to="/">Tasks</RouterLink>
        <RouterLink to="/projects">Projects</RouterLink>
        <RouterLink to="/settings">Settings</RouterLink>
        <button type="button" class="linkish" @click="onLogout">Sign out</button>
      </nav>
    </header>
    <main class="main">
      <RouterView />
    </main>
    <ToastHost />
  </div>
</template>
