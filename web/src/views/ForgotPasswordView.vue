<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const email = ref('')
const confirmEmail = ref('')
const busy = ref(false)
const sent = ref(false)
const error = ref('')
const { push } = useToast()

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    await api.forgotPassword(email.value.trim(), confirmEmail.value.trim())
    sent.value = true
    push('If an account exists, a reset link was sent', 'success')
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Request failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="container mt-5">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-header">
            <h2 class="card-title mb-0">Forgot Password</h2>
          </div>
          <div class="card-body">
            <p class="text-muted">Enter your email address twice and we'll send you a link to reset your password.</p>
            <div v-if="sent" class="alert alert-success" role="alert">
              If an account was found, an email has been sent to reset your password.
              <div class="mt-2">
                <RouterLink to="/login">Back to sign in</RouterLink>
              </div>
            </div>
            <form v-else @submit.prevent="onSubmit">
              <div class="mb-3">
                <label for="forgot-email" class="form-label">Email</label>
                <input id="forgot-email" v-model="email" type="email" class="form-control" required autocomplete="username" />
              </div>
              <div class="mb-3">
                <label for="forgot-confirm-email" class="form-label">Confirm email</label>
                <input id="forgot-confirm-email" v-model="confirmEmail" type="email" class="form-control" required autocomplete="username" />
              </div>
              <div v-if="error" class="text-danger mb-3">{{ error }}</div>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Sending…' : 'Send reset link' }}
              </button>
            </form>
            <p v-if="!sent" class="mt-3 mb-0 text-muted">
              <RouterLink to="/login">Back to sign in</RouterLink>
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
