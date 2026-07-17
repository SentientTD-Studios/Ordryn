<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AdminSettings, AdminUser } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const users = ref<AdminUser[]>([])
const busy = ref(false)
const settings = reactive<AdminSettings>({
  site_name: '',
  default_timezone: 'UTC',
  show_changelog: true,
  site_version: '',
  enable_registration: true,
  invite_only: false,
  meta_description: '',
  enable_global_announcement: false,
  global_announcement_text: '',
  enable_api: false,
})

async function load() {
  try {
    const [s, u] = await Promise.all([api.getAdminSettings(), api.listAdminUsers()])
    Object.assign(settings, s)
    users.value = u
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load admin data', 'error')
  }
}

async function saveSettings() {
  busy.value = true
  try {
    const saved = await api.patchAdminSettings({ ...settings })
    Object.assign(settings, saved)
    toast.push('Settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busy.value = false
  }
}

async function toggleBan(user: AdminUser) {
  try {
    if (user.is_banned) {
      await api.unbanUser(user.id)
      toast.push('User unbanned', 'success')
    } else {
      await api.banUser(user.id)
      toast.push('User banned', 'info')
    }
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Admin</h1>
        <p class="lede">Site settings and user moderation.</p>
      </div>
    </header>

    <form class="stack" @submit.prevent="saveSettings">
      <h2>Site settings</h2>
      <label>
        Site name
        <input v-model="settings.site_name" type="text" required />
      </label>
      <label>
        Default timezone
        <input v-model="settings.default_timezone" type="text" required />
      </label>
      <label>
        Meta description
        <textarea v-model="settings.meta_description" rows="2" />
      </label>
      <label class="inline">
        <input v-model="settings.enable_registration" type="checkbox" />
        Enable registration
      </label>
      <label class="inline">
        <input v-model="settings.invite_only" type="checkbox" />
        Invite only
      </label>
      <label class="inline">
        <input v-model="settings.show_changelog" type="checkbox" />
        Show changelog
      </label>
      <label class="inline">
        <input v-model="settings.enable_api" type="checkbox" />
        Enable external REST API (API keys &amp; Android)
      </label>
      <p class="muted">The web app always uses the JSON API with your session cookie. This toggle controls Bearer access for scripts and mobile clients.</p>
      <label class="inline">
        <input v-model="settings.enable_global_announcement" type="checkbox" />
        Global announcement
      </label>
      <label>
        Announcement text
        <textarea v-model="settings.global_announcement_text" rows="2" maxlength="500" />
      </label>
      <p v-if="settings.site_version" class="muted">Binary version: {{ settings.site_version }}</p>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Saving…' : 'Save settings' }}
      </button>
    </form>

    <h2>Users</h2>
    <ul class="plain-list">
      <li v-for="user in users" :key="user.id" class="row">
        <div class="task-body">
          <strong>{{ user.user_name || user.email }}</strong>
          <p class="meta muted">
            {{ user.email }}
            <span v-if="user.is_banned">· banned</span>
          </p>
        </div>
        <button type="button" class="ghost" :class="{ danger: !user.is_banned }" @click="toggleBan(user)">
          {{ user.is_banned ? 'Unban' : 'Ban' }}
        </button>
      </li>
      <li v-if="!users.length" class="muted empty">No users found.</li>
    </ul>
  </section>
</template>
