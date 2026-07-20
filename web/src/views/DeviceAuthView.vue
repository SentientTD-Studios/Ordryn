<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '@/api/client'
import type { DeviceStatus } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const route = useRoute()
const toast = useToast()
const codeInput = ref('')
const status = ref<DeviceStatus | null>(null)
const busy = ref(false)
const error = ref('')
const returningToApp = ref(false)

const userCode = computed(() => {
  const q = route.query.user_code
  return typeof q === 'string' ? q.trim() : codeInput.value.trim()
})

/** Only follow server-issued Ordryn deep links (never arbitrary URLs). */
function isSafeAppRedirect(uri: string | undefined): uri is string {
  if (!uri) return false
  try {
    const parsed = new URL(uri)
    return parsed.protocol === 'ordryn:' && parsed.hostname === 'auth-complete'
  } catch {
    return false
  }
}

function returnToApp(uri: string | undefined) {
  if (!isSafeAppRedirect(uri)) return false
  returningToApp.value = true
  window.location.assign(uri)
  return true
}

async function loadStatus() {
  error.value = ''
  status.value = null
  if (!userCode.value) return
  busy.value = true
  try {
    status.value = await api.deviceStatus(userCode.value)
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Failed to load request'
  } finally {
    busy.value = false
  }
}

async function approve() {
  busy.value = true
  try {
    const result = await api.deviceApprove(userCode.value)
    toast.push('Device approved', 'success')
    if (returnToApp(result.redirect_uri)) return
    await loadStatus()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Approve failed', 'error')
  } finally {
    busy.value = false
  }
}

async function deny() {
  busy.value = true
  try {
    const result = await api.deviceDeny(userCode.value)
    toast.push('Device denied', 'info')
    if (returnToApp(result.redirect_uri)) return
    await loadStatus()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Deny failed', 'error')
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  const q = route.query.user_code
  if (typeof q === 'string') {
    codeInput.value = q
    void loadStatus()
  }
})
</script>

<template>
  <section class="page narrow">
    <header class="page-head">
      <div>
        <h1>Approve device</h1>
        <p class="lede">Link an Android or API client with a one-time code.</p>
      </div>
    </header>

    <form class="stack" @submit.prevent="loadStatus">
      <label>
        User code
        <input v-model="codeInput" type="text" placeholder="ABCD-EFGH" required />
      </label>
      <button class="primary" type="submit" :disabled="busy">Look up</button>
    </form>

    <p v-if="error" class="muted danger">{{ error }}</p>
    <p v-if="returningToApp" class="muted">Returning to the app…</p>

    <div v-if="status && !returningToApp" class="stack">
      <p>
        Client <strong>{{ status.client_name || 'Unknown' }}</strong>
        is <strong>{{ status.status }}</strong>.
      </p>
      <div v-if="status.status === 'pending'" class="actions">
        <button type="button" class="primary" :disabled="busy" @click="approve">Approve</button>
        <button type="button" class="ghost danger" :disabled="busy" @click="deny">Deny</button>
      </div>
      <p v-else-if="status.redirect_uri" class="muted">
        You can close this window and return to the app.
      </p>
    </div>
  </section>
</template>
