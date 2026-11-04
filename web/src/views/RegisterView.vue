<script setup lang="ts">
import { ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { useSite } from '@/composables/useSite'
import { api } from '@/api/client'
import { APIError, isVerificationPending } from '@/api/types'
import TimezoneSelect from '@/components/TimezoneSelect.vue'

function queryString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

const route = useRoute()
const emailFromQuery = queryString(route.query.email)
const inviteFromQuery = queryString(route.query.invite)
const emailLocked = emailFromQuery !== ''
const inviteLocked = inviteFromQuery !== ''

const email = ref(emailFromQuery)
const userName = ref('')
const password = ref('')
const confirm = ref('')
const invite = ref(inviteFromQuery)
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
const busy = ref(false)
const error = ref('')
const usernameHint = ref('')
const usernameOk = ref<boolean | null>(null)
// Set once open sign-up has emailed a confirmation link.
const pendingEmail = ref('')
const pendingMessage = ref('')
const resending = ref(false)
const { register } = useAuth()
const { push } = useToast()
const { enableJoinRequests } = useSite()
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
    const result = await register({
      email: email.value.trim(),
      user_name: userName.value.trim(),
      password: password.value,
      confirm_password: confirm.value,
      timezone: timezone.value,
      invite_token: invite.value.trim() || undefined,
    })
    if (isVerificationPending(result)) {
      pendingEmail.value = email.value.trim()
      pendingMessage.value = result.message
      password.value = ''
      confirm.value = ''
      return
    }
    push('Account created', 'success')
    await router.replace('/')
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Registration failed'
  } finally {
    busy.value = false
  }
}

async function resendConfirmation() {
  resending.value = true
  try {
    await api.resendVerification(pendingEmail.value)
    push('If that address is waiting for confirmation, a new link is on its way', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not send a new link', 'error')
  } finally {
    resending.value = false
  }
}
</script>

<template>
  <div class="container mt-3">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-body">
            <h1 class="card-title">{{ pendingEmail ? 'Check your email' : 'Sign Up' }}</h1>
            <div v-if="pendingEmail">
              <p>{{ pendingMessage }}</p>
              <p class="text-muted small">
                We sent it to <strong>{{ pendingEmail }}</strong>. The link expires in 48 hours.
                You can sign in once you've confirmed your address.
              </p>
              <div class="d-flex flex-wrap gap-2">
                <RouterLink to="/login" class="btn btn-primary">Go to sign in</RouterLink>
                <button type="button" class="btn btn-outline-secondary" :disabled="resending" @click="resendConfirmation">
                  {{ resending ? 'Sending…' : 'Send a new link' }}
                </button>
              </div>
            </div>
            <form v-else @submit.prevent="onSubmit">
              <div class="mb-3">
                <label for="signup-email" class="form-label">Email</label>
                <input
                  id="signup-email"
                  v-model="email"
                  type="email"
                  class="form-control"
                  :class="{ 'bg-body-secondary': emailLocked }"
                  required
                  autocomplete="username"
                  :readonly="emailLocked"
                />
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
                <TimezoneSelect id="signup-timezone" v-model="timezone" required />
              </div>
              <div class="mb-3">
                <label for="signup-invite" class="form-label">
                  Invite token
                  <span v-if="!inviteLocked" class="text-muted">(optional)</span>
                </label>
                <input
                  id="signup-invite"
                  v-model="invite"
                  type="text"
                  class="form-control"
                  :class="{ 'bg-body-secondary': inviteLocked }"
                  autocomplete="off"
                  :readonly="inviteLocked"
                />
              </div>
              <div v-if="error" class="text-danger mb-3">{{ error }}</div>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Creating…' : 'Create account' }}
              </button>
            </form>
            <p v-if="!pendingEmail" class="mt-3 mb-0 text-muted">
              Already registered?
              <RouterLink to="/login">Sign in</RouterLink>
              <template v-if="enableJoinRequests">
                ·
                <RouterLink to="/join">Request to join</RouterLink>
              </template>
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
