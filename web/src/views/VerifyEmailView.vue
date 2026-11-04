<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'

// 'password': link looks complete; ask for the sign-up password to confirm.
// 'confirmed': confirmed but not signed in (second factor or deactivated account).
// 'failed': the link itself is unusable; offer a new one.
type State = 'password' | 'confirmed' | 'failed'

const route = useRoute()
const router = useRouter()
const { push } = useToast()
const { verifyEmail } = useAuth()
const state = ref<State>('password')
const error = ref('')
const password = ref('')
const busy = ref(false)
const email = ref('')
const resending = ref(false)
const linkID = ref('')
const linkToken = ref('')

function queryString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

onMounted(() => {
  linkID.value = queryString(route.query.id)
  linkToken.value = queryString(route.query.token)
  if (!linkID.value || !linkToken.value) {
    state.value = 'failed'
    error.value = 'This confirmation link is incomplete. Copy the whole link from the email, or request a new one below.'
  }
})

async function confirm() {
  busy.value = true
  error.value = ''
  try {
    const user = await verifyEmail(linkID.value, linkToken.value, password.value)
    if (user) {
      push('Your email is confirmed. Welcome!', 'success')
      await router.replace('/')
      return
    }
    state.value = 'confirmed'
  } catch (err) {
    if (err instanceof APIError && err.code === 'invalid_credentials') {
      // The link still works; let them try the password again.
      error.value = err.message
    } else {
      state.value = 'failed'
      error.value = err instanceof APIError ? err.message : 'Could not confirm your email.'
    }
  } finally {
    busy.value = false
  }
}

async function resend() {
  resending.value = true
  try {
    await api.resendVerification(email.value.trim())
    push('If that address is waiting for confirmation, a new link is on its way', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not send a new link', 'error')
  } finally {
    resending.value = false
  }
}
</script>

<template>
  <div class="container mt-5">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-header">
            <h2 class="card-title mb-0">Confirm your email</h2>
          </div>
          <div class="card-body">
            <form v-if="state === 'password'" @submit.prevent="confirm">
              <p class="text-muted">Enter the password you chose when you signed up to confirm your email and sign in.</p>
              <div class="mb-3">
                <label for="verify-email-password" class="form-label">Password</label>
                <input
                  id="verify-email-password"
                  v-model="password"
                  type="password"
                  class="form-control"
                  required
                  autocomplete="current-password"
                />
              </div>
              <div v-if="error" class="text-danger mb-2">{{ error }}</div>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Confirming…' : 'Confirm and sign in' }}
              </button>
            </form>

            <template v-else-if="state === 'confirmed'">
              <div class="alert alert-success" role="alert">
                Your email is confirmed. Sign in with your email and password to continue.
              </div>
              <RouterLink to="/login" class="btn btn-primary">Sign in</RouterLink>
            </template>

            <template v-else>
              <div class="alert alert-warning" role="alert">{{ error }}</div>
              <form @submit.prevent="resend">
                <div class="mb-3">
                  <label for="verify-email-address" class="form-label">Email</label>
                  <input
                    id="verify-email-address"
                    v-model="email"
                    type="email"
                    class="form-control"
                    required
                    autocomplete="username"
                  />
                </div>
                <button type="submit" class="btn btn-primary" :disabled="resending">
                  {{ resending ? 'Sending…' : 'Send a new link' }}
                </button>
              </form>
              <p class="mt-3 mb-0 text-muted">
                <RouterLink to="/login">Back to sign in</RouterLink>
              </p>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
