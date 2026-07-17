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
  <div class="container mt-3">
    <h1>Admin</h1>
    <div class="card mb-4">
      <div class="card-header"><h2 class="h5 mb-0">Site settings</h2></div>
      <div class="card-body">
        <form @submit.prevent="saveSettings">
          <div class="mb-3">
            <label class="form-label">Site name</label>
            <input v-model="settings.site_name" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label class="form-label">Default timezone</label>
            <input v-model="settings.default_timezone" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label class="form-label">Meta description</label>
            <textarea v-model="settings.meta_description" class="form-control" rows="2" />
          </div>
          <div class="form-check mb-2">
            <input id="admin-registration" v-model="settings.enable_registration" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-registration">Enable registration</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-invite-only" v-model="settings.invite_only" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-invite-only">Invite only</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-changelog" v-model="settings.show_changelog" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-changelog">Show changelog</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-api" v-model="settings.enable_api" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-api">Enable external REST API (API keys &amp; Android)</label>
          </div>
          <p class="text-muted small">The web app always uses the JSON API with your session cookie. This toggle controls Bearer access for scripts and mobile clients.</p>
          <div class="form-check mb-2">
            <input id="admin-announcement" v-model="settings.enable_global_announcement" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-announcement">Global announcement</label>
          </div>
          <div class="mb-3">
            <label class="form-label">Announcement text</label>
            <textarea v-model="settings.global_announcement_text" class="form-control" rows="2" maxlength="500" />
          </div>
          <p v-if="settings.site_version" class="text-muted">Binary version: {{ settings.site_version }}</p>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? 'Saving…' : 'Save settings' }}
          </button>
        </form>
      </div>
    </div>

    <div class="card">
      <div class="card-header"><h2 class="h5 mb-0">Users</h2></div>
      <ul class="list-group list-group-flush">
        <li v-for="user in users" :key="user.id" class="list-group-item d-flex justify-content-between align-items-center">
          <div>
            <strong>{{ user.user_name || user.email }}</strong>
            <div class="text-muted small">
              {{ user.email }}
              <span v-if="user.is_banned">· banned</span>
            </div>
          </div>
          <button type="button" class="btn btn-sm" :class="user.is_banned ? 'btn-outline-secondary' : 'btn-outline-danger'" @click="toggleBan(user)">
            {{ user.is_banned ? 'Unban' : 'Ban' }}
          </button>
        </li>
        <li v-if="!users.length" class="list-group-item text-muted">No users found.</li>
      </ul>
    </div>
  </div>
</template>
