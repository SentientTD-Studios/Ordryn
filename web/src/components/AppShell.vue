<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch, provide } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useKeyboardShortcuts } from '@/composables/useKeyboardShortcuts'
import { startLiveUpdates, stopLiveUpdates } from '@/composables/useLiveUpdates'
import { usePageActivity } from '@/composables/usePageActivity'
import { api } from '@/api/client'
import ModernHeader from '@/components/modern/ModernHeader.vue'
import MobileNavDrawer from '@/components/modern/MobileNavDrawer.vue'
import AppFooter from '@/components/AppFooter.vue'
import ToastHost from '@/components/ToastHost.vue'
import GoToTopButton from '@/components/GoToTopButton.vue'
import TaskSidebar from '@/components/TaskSidebar.vue'
import ChangelogModal from '@/components/ChangelogModal.vue'
import ShortcutsModal from '@/components/ShortcutsModal.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const route = useRoute()
const { isAuthenticated } = useAuth()
const { siteInfo, refresh: refreshSite } = useSite()
const { initKeyboardShortcuts, destroyKeyboardShortcuts } = useKeyboardShortcuts()
const overdueCount = ref(0)
const pendingInviteCount = ref(0)
const mobileNavOpen = ref(false)

provide('overdueCount', overdueCount)
provide('pendingInviteCount', pendingInviteCount)

const showAnnouncement = computed(
  () =>
    !!siteInfo.value?.enable_global_announcement &&
    !!siteInfo.value?.global_announcement_text &&
    !siteInfo.value?.announcement_dismissed,
)

const showChangelog = computed(() => siteInfo.value?.show_changelog !== false)

async function loadOverdue() {
  if (!isAuthenticated.value) return
  try {
    const stats = await api.dashboard()
    overdueCount.value = stats.overdue_count
  } catch {
    overdueCount.value = 0
  }
}

async function loadPendingInvites() {
  if (!isAuthenticated.value) {
    pendingInviteCount.value = 0
    return
  }
  try {
    const [projectInvites, orgInvites] = await Promise.all([
      api.listMyProjectInvites(),
      api.listMyOrganizationInvites().catch(() => []),
    ])
    pendingInviteCount.value = projectInvites.length + orgInvites.length
  } catch {
    pendingInviteCount.value = 0
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

function toggleMobileNav() {
  mobileNavOpen.value = !mobileNavOpen.value
}

function closeMobileNav() {
  mobileNavOpen.value = false
}

function onMobileNavKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && mobileNavOpen.value) {
    e.preventDefault()
    closeMobileNav()
  }
}

watch(mobileNavOpen, (open) => {
  document.body.classList.toggle('ordryn-mobile-nav-open', open)
})

watch(
  () => route.fullPath,
  () => {
    closeMobileNav()
  },
)

watch(
  isAuthenticated,
  (authed) => {
    if (authed) {
      void loadOverdue()
      void loadPendingInvites()
      startLiveUpdates()
    } else {
      overdueCount.value = 0
      pendingInviteCount.value = 0
      stopLiveUpdates()
    }
  },
  { immediate: true },
)

usePageActivity(() => {
  if (!isAuthenticated.value) return
  void loadOverdue()
  void loadPendingInvites()
})

onMounted(() => {
  initKeyboardShortcuts()
  void refreshSite()
  window.addEventListener('keydown', onMobileNavKeydown)
})

onUnmounted(() => {
  destroyKeyboardShortcuts()
  stopLiveUpdates()
  window.removeEventListener('keydown', onMobileNavKeydown)
  document.body.classList.remove('ordryn-mobile-nav-open')
})
</script>

<template>
  <div class="ordryn-app-shell">
    <ModernHeader :mobile-nav-open="mobileNavOpen" @toggle-mobile-nav="toggleMobileNav" />
    <MobileNavDrawer :open="mobileNavOpen" @close="closeMobileNav" />

    <div
      v-if="showAnnouncement && siteInfo?.global_announcement_text"
      id="global-announcement"
      class="global-announcement-wrapper"
    >
      <div class="container py-2">
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

    <main class="site-main flex-grow-1 d-flex flex-column" data-page="spa">
      <RouterView />
    </main>

    <!-- Global Footer on all pages EXCEPT sidebar layout pages (/, /dashboard, /calendar) -->
    <AppFooter v-if="!['/', '/dashboard', '/calendar'].includes(route.path)" class="container" />

    <ToastHost />
    <GoToTopButton />
    <ConfirmModal />
    <TaskSidebar v-if="isAuthenticated && route.name !== 'share'" />
    <ChangelogModal v-if="showChangelog" />
    <ShortcutsModal />
  </div>
</template>
