<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { api } from '@/api/client'
import { APIError } from '@/api/types'

const userName = ref('')
const busy = ref(false)
const error = ref('')
const usernameHint = ref('')
const usernameOk = ref<boolean | null>(null)
const { user, claimUsername, logout } = useAuth()
const { push } = useToast()
const router = useRouter()

userName.value = user.value?.user_name || ''

let availabilityTimer: ReturnType<typeof setTimeout> | null = null

watch(userName, (value) => {
  usernameOk.value = null
  usernameHint.value = ''
  if (availabilityTimer) clearTimeout(availabilityTimer)
  const trimmed = value.trim()
  if (!trimmed) return
  availabilityTimer = setTimeout(async () => {
    try {
      const result = await api.usernameAvailable(trimmed)
      usernameOk.value = result.valid && result.available
      if (!result.valid) {
        usernameHint.value = result.message || 'Invalid username'
      } else if (!result.available) {
        usernameHint.value = 'That username is already taken'
      } else {
        usernameHint.value = 'Username is available'
      }
    } catch {
      usernameHint.value = ''
    }
  }, 300)
})

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    await claimUsername(userName.value.trim())
    push('Username saved', 'success')
    await router.replace('/')
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Failed to set username'
  } finally {
    busy.value = false
  }
}

async function onLogout() {
  await logout()
  await router.replace('/login')
}
</script>

<template>
  <div class="container mt-5">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-body">
            <h1 class="card-title h3">Choose your username</h1>
            <p class="text-muted">
              Existing accounts need a username. You get <strong>one free change</strong> — after you save,
              only an administrator can change it.
            </p>
            <p v-if="user?.user_name" class="small">
              Current temporary username: <code>{{ user.user_name }}</code>
            </p>
            <form @submit.prevent="onSubmit">
              <div class="mb-3">
                <label for="claim-username" class="form-label">Username</label>
                <input
                  id="claim-username"
                  v-model="userName"
                  type="text"
                  class="form-control"
                  required
                  minlength="3"
                  maxlength="32"
                  pattern="[A-Za-z0-9_]+"
                  autocomplete="nickname"
                />
                <div class="form-text">3–32 characters; letters, numbers, and underscores only.</div>
                <div
                  v-if="usernameHint"
                  class="small mt-1"
                  :class="usernameOk ? 'text-success' : 'text-danger'"
                >
                  {{ usernameHint }}
                </div>
              </div>
              <div v-if="error" class="text-danger mb-3">{{ error }}</div>
              <div class="d-flex gap-2">
                <button type="submit" class="btn btn-primary" :disabled="busy">
                  {{ busy ? 'Saving…' : 'Save username' }}
                </button>
                <button type="button" class="btn btn-outline-secondary" :disabled="busy" @click="onLogout">
                  Sign out
                </button>
              </div>
            </form>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
