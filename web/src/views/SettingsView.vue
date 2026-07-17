<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { api } from '@/api/client'
import type { APIKey, CalendarInfo, Tag } from '@/api/types'
import { APIError } from '@/api/types'

const { user, updateProfile } = useAuth()
const { push } = useToast()
const userName = ref('')
const timezone = ref('UTC')
const itemsPerPage = ref(15)
const digestEnabled = ref(false)
const digestHour = ref(8)
const busy = ref(false)

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')

const tags = ref<Tag[]>([])
const tagName = ref('')
const renameTagId = ref<number | null>(null)
const renameTagValue = ref('')

const calendar = ref<CalendarInfo | null>(null)
const icsFile = ref<File | null>(null)
const keys = ref<APIKey[]>([])
const keyName = ref('')
const mintedKey = ref('')

watch(
  user,
  (u) => {
    if (!u) return
    userName.value = u.user_name || ''
    timezone.value = u.timezone || 'UTC'
    itemsPerPage.value = u.items_per_page || 15
    digestEnabled.value = u.digest_enabled
    digestHour.value = u.digest_hour
  },
  { immediate: true },
)

async function loadExtras() {
  try {
    const [t, c, k] = await Promise.all([api.listTags(), api.getCalendar(), api.listAPIKeys()])
    tags.value = t
    calendar.value = c
    keys.value = k
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load settings extras', 'error')
  }
}

async function save() {
  busy.value = true
  try {
    await updateProfile({
      user_name: userName.value.trim(),
      timezone: timezone.value.trim(),
      items_per_page: Number(itemsPerPage.value),
      digest_enabled: digestEnabled.value,
      digest_hour: Number(digestHour.value),
    })
    push('Profile updated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    busy.value = false
  }
}

async function changePassword() {
  try {
    await api.changePassword({
      current_password: currentPassword.value,
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    push('Password updated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Password change failed', 'error')
  }
}

async function createTag() {
  if (!tagName.value.trim()) return
  try {
    await api.createTag(tagName.value.trim())
    tagName.value = ''
    tags.value = await api.listTags()
    push('Tag created', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

function beginRenameTag(tag: Tag) {
  renameTagId.value = tag.id
  renameTagValue.value = tag.name
}

async function saveRenameTag() {
  if (renameTagId.value == null || !renameTagValue.value.trim()) return
  try {
    await api.renameTag(renameTagId.value, renameTagValue.value.trim())
    renameTagId.value = null
    tags.value = await api.listTags()
    push('Tag renamed', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function removeTag(tag: Tag) {
  try {
    await api.deleteTag(tag.id)
    tags.value = await api.listTags()
    push('Tag deleted', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

async function regenerateCalendar() {
  try {
    calendar.value = await api.regenerateCalendar()
    push('Calendar token regenerated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Regenerate failed', 'error')
  }
}

function onIcsFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  icsFile.value = input.files?.[0] ?? null
}

async function syncCalendar() {
  if (!icsFile.value) return
  try {
    const result = await api.syncCalendar(icsFile.value)
    push(`Updated ${result.updated} task due dates`, 'success')
    icsFile.value = null
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Calendar sync failed', 'error')
  }
}

async function exportTasks(format: 'json' | 'csv') {
  try {
    await api.downloadExport(format)
    push(`Exported ${format.toUpperCase()}`, 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Export failed', 'error')
  }
}

async function createKey() {
  if (!keyName.value.trim()) return
  try {
    const created = await api.createAPIKey(keyName.value.trim())
    mintedKey.value = created.key
    keyName.value = ''
    keys.value = await api.listAPIKeys()
    push('API key created — copy it now', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

async function revokeKey(key: APIKey) {
  try {
    await api.revokeAPIKey(key.id)
    keys.value = await api.listAPIKeys()
    push('API key revoked', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Revoke failed', 'error')
  }
}

onMounted(() => {
  document.body.classList.add('profile-page')
  void loadExtras()
})

onUnmounted(() => {
  document.body.classList.remove('profile-page')
})
</script>

<template>
  <div class="container mt-3">
    <div class="card mb-4">
      <div class="card-header">
        <h2 class="card-title mb-0">User Profile</h2>
      </div>
      <div class="card-body">
        <form @submit.prevent="save">
          <div class="mb-3">
            <label class="form-label fw-bold">Email</label>
            <input type="text" class="form-control-plaintext" :value="user?.email || ''" readonly tabindex="-1" />
          </div>
          <div class="mb-3">
            <label for="profile-name" class="form-label">Display name</label>
            <input id="profile-name" v-model="userName" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label for="profile-timezone" class="form-label">Timezone</label>
            <input id="profile-timezone" v-model="timezone" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label for="profile-per-page" class="form-label">Tasks per page</label>
            <select id="profile-per-page" v-model.number="itemsPerPage" class="form-select">
              <option :value="10">10</option>
              <option :value="15">15</option>
              <option :value="25">25</option>
              <option :value="50">50</option>
            </select>
          </div>
          <div class="form-check mb-3">
            <input id="profile-digest" v-model="digestEnabled" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="profile-digest">Daily email digest</label>
          </div>
          <div class="mb-3">
            <label for="profile-digest-hour" class="form-label">Digest hour (0–23)</label>
            <input id="profile-digest-hour" v-model.number="digestHour" type="number" class="form-control" min="0" max="23" />
          </div>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? 'Saving…' : 'Save profile' }}
          </button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h3 class="card-title mb-0">Change password</h3></div>
      <div class="card-body">
        <form @submit.prevent="changePassword">
          <div class="mb-3">
            <label class="form-label">Current password</label>
            <input v-model="currentPassword" type="password" class="form-control" required autocomplete="current-password" />
          </div>
          <div class="mb-3">
            <label class="form-label">New password</label>
            <input v-model="newPassword" type="password" class="form-control" required autocomplete="new-password" />
          </div>
          <div class="mb-3">
            <label class="form-label">Confirm new password</label>
            <input v-model="confirmPassword" type="password" class="form-control" required autocomplete="new-password" />
          </div>
          <button type="submit" class="btn btn-primary">Change password</button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h3 class="card-title mb-0">Tags</h3></div>
      <div class="card-body">
        <form class="row g-2 mb-3" @submit.prevent="createTag">
          <div class="col-sm-8">
            <input v-model="tagName" type="text" class="form-control" placeholder="New tag" required maxlength="40" />
          </div>
          <div class="col-sm-4">
            <button type="submit" class="btn btn-primary w-100">Add tag</button>
          </div>
        </form>
        <ul class="list-group">
          <li v-for="tag in tags" :key="tag.id" class="list-group-item d-flex flex-wrap gap-2 align-items-center">
            <template v-if="renameTagId === tag.id">
              <input v-model="renameTagValue" type="text" class="form-control form-control-sm" maxlength="40" />
              <button type="button" class="btn btn-sm btn-primary" @click="saveRenameTag">Save</button>
              <button type="button" class="btn btn-sm btn-secondary" @click="renameTagId = null">Cancel</button>
            </template>
            <template v-else>
              <span class="flex-grow-1">{{ tag.name }}</span>
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="beginRenameTag(tag)">Rename</button>
              <button type="button" class="btn btn-sm btn-outline-danger" @click="removeTag(tag)">Delete</button>
            </template>
          </li>
          <li v-if="!tags.length" class="list-group-item text-muted">No tags yet.</li>
        </ul>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h3 class="card-title mb-0">Calendar feed</h3></div>
      <div class="card-body">
        <p v-if="calendar" class="text-break"><code>{{ calendar.feed_url }}</code></p>
        <button type="button" class="btn btn-outline-secondary mb-3" @click="regenerateCalendar">Regenerate token</button>
        <form @submit.prevent="syncCalendar">
          <div class="mb-3">
            <label class="form-label">Sync due dates from ICS export</label>
            <input type="file" class="form-control" accept=".ics,text/calendar" @change="onIcsFileChange" />
          </div>
          <button type="submit" class="btn btn-outline-primary" :disabled="!icsFile">Sync calendar</button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h3 class="card-title mb-0">Export &amp; import</h3></div>
      <div class="card-body">
        <div class="d-flex flex-wrap gap-2">
          <button type="button" class="btn btn-primary" @click="exportTasks('json')">Export JSON</button>
          <button type="button" class="btn btn-outline-secondary" @click="exportTasks('csv')">Export CSV</button>
          <RouterLink to="/import" class="btn btn-outline-primary">Import CSV</RouterLink>
        </div>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h3 class="card-title mb-0">API keys</h3></div>
      <div class="card-body">
        <form class="row g-2 mb-3" @submit.prevent="createKey">
          <div class="col-sm-8">
            <input v-model="keyName" type="text" class="form-control" placeholder="Key name" required />
          </div>
          <div class="col-sm-4">
            <button type="submit" class="btn btn-primary w-100">Create key</button>
          </div>
        </form>
        <p v-if="mintedKey" class="text-break">New key (shown once): <code>{{ mintedKey }}</code></p>
        <ul class="list-group">
          <li v-for="key in keys" :key="key.id" class="list-group-item d-flex justify-content-between align-items-center">
            <div>
              <strong>{{ key.name }}</strong>
              <div class="text-muted small">{{ key.key_prefix }}…</div>
            </div>
            <button type="button" class="btn btn-sm btn-outline-danger" @click="revokeKey(key)">Revoke</button>
          </li>
          <li v-if="!keys.length" class="list-group-item text-muted">No API keys.</li>
        </ul>
      </div>
    </div>
  </div>
</template>
