<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
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

onMounted(loadExtras)
</script>

<template>
  <section class="page narrow">
    <h1>Settings</h1>
    <p class="lede">Profile, tags, calendar, export, and API keys.</p>

    <form class="stack" @submit.prevent="save">
      <h2>Profile</h2>
      <label>
        Display name
        <input v-model="userName" type="text" required />
      </label>
      <label>
        Timezone
        <input v-model="timezone" type="text" required />
      </label>
      <label>
        Tasks per page
        <select v-model.number="itemsPerPage">
          <option :value="10">10</option>
          <option :value="15">15</option>
          <option :value="25">25</option>
          <option :value="50">50</option>
        </select>
      </label>
      <label class="inline">
        <input v-model="digestEnabled" type="checkbox" />
        Daily email digest
      </label>
      <label>
        Digest hour (0–23)
        <input v-model.number="digestHour" type="number" min="0" max="23" />
      </label>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Saving…' : 'Save settings' }}
      </button>
    </form>

    <form class="stack" @submit.prevent="changePassword">
      <h2>Password</h2>
      <label>
        Current password
        <input v-model="currentPassword" type="password" required autocomplete="current-password" />
      </label>
      <label>
        New password
        <input v-model="newPassword" type="password" required autocomplete="new-password" />
      </label>
      <label>
        Confirm new password
        <input v-model="confirmPassword" type="password" required autocomplete="new-password" />
      </label>
      <button class="primary" type="submit">Change password</button>
    </form>

    <div class="stack">
      <h2>Tags</h2>
      <form class="composer" @submit.prevent="createTag">
        <input v-model="tagName" type="text" placeholder="New tag" required maxlength="40" />
        <button class="primary" type="submit">Add</button>
      </form>
      <ul class="plain-list">
        <li v-for="tag in tags" :key="tag.id" class="row">
          <template v-if="renameTagId === tag.id">
            <input v-model="renameTagValue" type="text" maxlength="40" />
            <button type="button" class="primary" @click="saveRenameTag">Save</button>
            <button type="button" class="ghost" @click="renameTagId = null">Cancel</button>
          </template>
          <template v-else>
            <span>{{ tag.name }}</span>
            <button type="button" class="ghost" @click="beginRenameTag(tag)">Rename</button>
            <button type="button" class="ghost danger" @click="removeTag(tag)">Delete</button>
          </template>
        </li>
        <li v-if="!tags.length" class="muted empty">No tags yet.</li>
      </ul>
    </div>

    <div class="stack">
      <h2>Calendar feed</h2>
      <p v-if="calendar" class="muted break">{{ calendar.feed_url }}</p>
      <div class="actions">
        <button type="button" class="ghost" @click="regenerateCalendar">Regenerate token</button>
      </div>
      <form class="stack" @submit.prevent="syncCalendar">
        <label>
          Sync due dates from ICS export
          <input type="file" accept=".ics,text/calendar" @change="onIcsFileChange" />
        </label>
        <button type="submit" class="ghost" :disabled="!icsFile">Sync calendar</button>
      </form>
    </div>

    <div class="stack">
      <h2>Export &amp; import</h2>
      <p class="muted">Download your tasks or import from CSV on the import page.</p>
      <div class="actions">
        <button type="button" class="primary" @click="exportTasks('json')">Export JSON</button>
        <button type="button" class="ghost" @click="exportTasks('csv')">Export CSV</button>
        <RouterLink to="/import">Import CSV</RouterLink>
      </div>
    </div>

    <div class="stack">
      <h2>API keys</h2>
      <form class="composer" @submit.prevent="createKey">
        <input v-model="keyName" type="text" placeholder="Key name" required />
        <button class="primary" type="submit">Create</button>
      </form>
      <p v-if="mintedKey" class="break">
        New key (shown once): <code>{{ mintedKey }}</code>
      </p>
      <ul class="plain-list">
        <li v-for="key in keys" :key="key.id" class="row">
          <div class="task-body">
            <strong>{{ key.name }}</strong>
            <p class="meta muted">{{ key.key_prefix }}…</p>
          </div>
          <button type="button" class="ghost danger" @click="revokeKey(key)">Revoke</button>
        </li>
        <li v-if="!keys.length" class="muted empty">No API keys.</li>
      </ul>
    </div>

    <p v-if="user" class="muted">Signed in as {{ user.email }}</p>
  </section>
</template>
