<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api } from '@/api/client'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

type State = 'checking' | 'confirmed' | 'failed'

const route = useRoute()
const { push } = useToast()
const state = ref<State>('checking')
const error = ref('')
const email = ref('')
const resending = ref(false)

function queryString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

onMounted(async () => {
  const id = queryString(route.query.id)
  const token = queryString(route.query.token)
  if (!id || !token) {
    state.value = 'failed'
    error.value = 'This confirmation link is incomplete. Copy the whole link from the email, or request a new one below.'
    return
  }
  try {
    await api.verifyEmail(id, token)
    state.value = 'confirmed'
  } catch (err) {
    state.value = 'failed'
    error.value = err instanceof APIError ? err.message : 'Could not confirm your email.'
  }
})

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
            <p v-if="state === 'checking'" class="text-muted mb-0">Confirming your email…</p>

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
