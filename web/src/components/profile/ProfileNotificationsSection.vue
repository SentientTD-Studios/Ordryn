<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { NotificationPreference } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const { push } = useToast()
const prefs = ref<NotificationPreference[]>([])
const loading = ref(true)
const saving = ref<string | null>(null)

async function load() {
  try {
    prefs.value = (await api.getNotificationPreferences()).preferences
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load notification settings', 'error')
  } finally {
    loading.value = false
  }
}

// Each switch saves on change; the switch reverts if the save fails.
async function toggle(pref: NotificationPreference, enabled: boolean) {
  saving.value = pref.type
  pref.enabled = enabled
  try {
    prefs.value = (await api.updateNotificationPreferences({ [pref.type]: enabled })).preferences
  } catch (err) {
    pref.enabled = !enabled
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    saving.value = null
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Notifications</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small mb-3">
        Choose which notifications you receive. Changes save immediately. Security emails such as password
        resets and password-change confirmations are always sent.
      </p>
      <div v-if="loading" class="text-muted small">Loading…</div>
      <div v-for="pref in prefs" :key="pref.type" class="form-check form-switch mb-3">
        <input
          :id="`notif-pref-${pref.type}`"
          class="form-check-input"
          type="checkbox"
          role="switch"
          :checked="pref.enabled"
          :disabled="saving === pref.type"
          @change="toggle(pref, ($event.target as HTMLInputElement).checked)"
        />
        <label class="form-check-label" :for="`notif-pref-${pref.type}`">{{ pref.label }}</label>
        <div class="form-text">{{ pref.description }}</div>
      </div>
    </div>
  </div>
</template>
