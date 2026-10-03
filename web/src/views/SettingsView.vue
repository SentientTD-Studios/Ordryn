<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { APIError } from '@/api/types'
import type { User } from '@/api/types'
import ProfileSectionNav from '@/components/profile/ProfileSectionNav.vue'
import ProfileAccountSection from '@/components/profile/ProfileAccountSection.vue'
import ProfilePreferencesSection from '@/components/profile/ProfilePreferencesSection.vue'
import ProfileNotificationsSection from '@/components/profile/ProfileNotificationsSection.vue'
import ProfileIntegrationsSection from '@/components/profile/ProfileIntegrationsSection.vue'
import ProfileDataSection from '@/components/profile/ProfileDataSection.vue'
import ProfileDeveloperSection from '@/components/profile/ProfileDeveloperSection.vue'
import { resolveProfileSection, type ProfileSection } from '@/utils/profileSections'

const { user, updateProfile } = useAuth()
const { push } = useToast()
const { askConfirm } = useConfirm()
const route = useRoute()
const router = useRouter()

const timezone = ref('UTC')
const itemsPerPage = ref(15)
const allowProjectInvites = ref(true)
const busy = ref(false)

const activeSection = computed(() =>
  resolveProfileSection(route.hash, String(route.query.github || '')),
)

function applyUserPrefs(u: User) {
  timezone.value = u.timezone || 'UTC'
  itemsPerPage.value = u.items_per_page || 15
  allowProjectInvites.value = u.allow_project_invites !== false
}

const prefsDirty = computed(() => {
  const u = user.value
  if (!u) return false
  return (
    timezone.value.trim() !== (u.timezone || 'UTC') ||
    Number(itemsPerPage.value) !== (u.items_per_page || 15) ||
    allowProjectInvites.value !== (u.allow_project_invites !== false)
  )
})

watch(
  user,
  (u) => {
    if (!u) return
    applyUserPrefs(u)
  },
  { immediate: true },
)

async function confirmDiscardPreferences(): Promise<boolean> {
  if (!prefsDirty.value) return true
  const ok = await askConfirm({
    title: 'Unsaved preferences',
    message: 'You have unsaved preference changes. Discard them and leave this section?',
    confirmLabel: 'Discard',
    danger: true,
  })
  if (!ok) return false
  if (user.value) applyUserPrefs(user.value)
  return true
}

function selectSection(section: ProfileSection) {
  if (section === activeSection.value) return
  void router.replace({ hash: `#${section}` })
}

async function save() {
  busy.value = true
  try {
    await updateProfile({
      timezone: timezone.value.trim(),
      items_per_page: Number(itemsPerPage.value),
      allow_project_invites: allowProjectInvites.value,
    })
    push('Profile updated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    busy.value = false
  }
}

function onBeforeUnload(event: BeforeUnloadEvent) {
  if (!prefsDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteUpdate(async (to) => {
  if (!prefsDirty.value) return true
  const nextSection = resolveProfileSection(to.hash, String(to.query.github || ''))
  if (nextSection === 'preferences') return true
  return confirmDiscardPreferences()
})

onBeforeRouteLeave(async () => {
  if (!prefsDirty.value) return true
  return confirmDiscardPreferences()
})

onMounted(() => {
  document.body.classList.add('profile-page')
  window.addEventListener('beforeunload', onBeforeUnload)
  const gh = String(route.query.github || '')
  if (gh === 'connected') {
    push('GitHub connected', 'success')
    void router.replace({ name: 'settings', hash: '#integrations', query: {} })
  } else if (gh === 'error') {
    push('GitHub connection failed', 'error')
    void router.replace({ name: 'settings', hash: '#integrations', query: {} })
  }
})

onUnmounted(() => {
  document.body.classList.remove('profile-page')
  window.removeEventListener('beforeunload', onBeforeUnload)
})
</script>

<template>
  <div class="container mt-3 profile-settings">
    <h1 class="h3 mb-3">User Profile</h1>
    <div class="row g-3 g-md-4 align-items-start">
      <div class="col-md-3">
        <ProfileSectionNav :active="activeSection" @select="selectSection" />
      </div>
      <div class="col-md-9">
        <ProfileAccountSection
          v-if="activeSection === 'account'"
          :email="user?.email || ''"
          :username="user?.user_name || ''"
        />
        <ProfilePreferencesSection
          v-else-if="activeSection === 'preferences'"
          v-model:timezone="timezone"
          v-model:items-per-page="itemsPerPage"
          v-model:allow-project-invites="allowProjectInvites"
          :busy="busy"
          :dirty="prefsDirty"
          @save="save"
        />
        <ProfileNotificationsSection v-else-if="activeSection === 'notifications'" />
        <ProfileIntegrationsSection v-else-if="activeSection === 'integrations'" />
        <ProfileDataSection v-else-if="activeSection === 'data'" />
        <ProfileDeveloperSection v-else-if="activeSection === 'developer'" />
      </div>
    </div>
  </div>
</template>
