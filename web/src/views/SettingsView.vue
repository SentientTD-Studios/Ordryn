<script setup lang="ts">
import { ref, watch } from 'vue'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const { user, updateProfile } = useAuth()
const { push } = useToast()
const userName = ref('')
const timezone = ref('UTC')
const itemsPerPage = ref(15)
const digestEnabled = ref(false)
const digestHour = ref(8)
const busy = ref(false)

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
</script>

<template>
  <section class="page narrow">
    <h1>Settings</h1>
    <p class="lede">Profile fields via `PATCH /api/v1/me`.</p>
    <form class="stack" @submit.prevent="save">
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
    <p v-if="user" class="muted">Signed in as {{ user.email }}</p>
  </section>
</template>
