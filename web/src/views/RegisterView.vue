<script setup lang="ts">
import { ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { api } from '@/api/client'
import { APIError } from '@/api/types'

const email = ref('')
const userName = ref('')
const password = ref('')
const confirm = ref('')
const invite = ref('')
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
const busy = ref(false)
const error = ref('')
const usernameHint = ref('')
const usernameOk = ref<boolean | null>(null)
const { register } = useAuth()
const { push } = useToast()
const router = useRouter()

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
    await register({
      email: email.value.trim(),
      user_name: userName.value.trim(),
      password: password.value,
      confirm_password: confirm.value,
      timezone: timezone.value,
      invite_token: invite.value.trim() || undefined,
    })
    push('Account created', 'success')
    await router.replace('/')
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Registration failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="container mt-3">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-body">
            <h1 class="card-title">Sign Up</h1>
            <form @submit.prevent="onSubmit">
              <div class="mb-3">
                <label for="signup-email" class="form-label">Email</label>
                <input id="signup-email" v-model="email" type="email" class="form-control" required autocomplete="username" />
              </div>
              <div class="mb-3">
                <label for="signup-username" class="form-label">Username</label>
                <input
                  id="signup-username"
                  v-model="userName"
                  type="text"
                  class="form-control"
                  required
                  minlength="3"
                  maxlength="32"
                  pattern="[A-Za-z0-9_]+"
                  autocomplete="nickname"
                />
                <div class="form-text">
                  3–32 characters; letters, numbers, and underscores only.
                  <strong>Usernames cannot be changed later except by an administrator.</strong>
                </div>
                <div
                  v-if="usernameHint"
                  class="small mt-1"
                  :class="usernameOk ? 'text-success' : 'text-danger'"
                >
                  {{ usernameHint }}
                </div>
              </div>
              <div class="mb-3">
                <label for="signup-password" class="form-label">Password</label>
                <input id="signup-password" v-model="password" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
              </div>
              <div class="mb-3">
                <label for="signup-confirm" class="form-label">Confirm password</label>
                <input id="signup-confirm" v-model="confirm" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
              </div>
              <div class="mb-3">
                <label for="signup-timezone" class="form-label">Timezone</label>
                <input id="signup-timezone" v-model="timezone" type="text" class="form-control" required />
              </div>
              <div class="mb-3">
                <label for="signup-invite" class="form-label">Invite token <span class="text-muted">(optional)</span></label>
                <input id="signup-invite" v-model="invite" type="text" class="form-control" autocomplete="off" />
              </div>
              <div v-if="error" class="text-danger mb-3">{{ error }}</div>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Creating…' : 'Create account' }}
              </button>
            </form>
            <p class="mt-3 mb-0 text-muted">
              Already registered?
              <RouterLink to="/login">Sign in</RouterLink>
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
