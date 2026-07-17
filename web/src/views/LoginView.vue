<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const email = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
const { login } = useAuth()
const { push } = useToast()
const router = useRouter()
const route = useRoute()

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    await login(email.value.trim(), password.value)
    push('Welcome back', 'success')
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Login failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="container mt-5">
    <div class="row justify-content-center">
      <div class="col-md-6 col-lg-5">
        <div class="card">
          <div class="card-header">
            <h2 class="card-title mb-0">Login</h2>
          </div>
          <form @submit.prevent="onSubmit">
            <div class="card-body">
              <div class="mb-3">
                <label for="login-email" class="form-label">Email</label>
                <input
                  id="login-email"
                  v-model="email"
                  type="email"
                  class="form-control"
                  required
                  autocomplete="username"
                />
              </div>
              <div class="mb-3">
                <label for="login-password" class="form-label">Password</label>
                <input
                  id="login-password"
                  v-model="password"
                  type="password"
                  class="form-control"
                  required
                  autocomplete="current-password"
                />
              </div>
              <div v-if="error" class="text-danger mb-2">{{ error }}</div>
              <div class="mb-2">
                <RouterLink to="/forgot-password" class="text-decoration-none small">Forgot Password?</RouterLink>
              </div>
            </div>
            <div class="card-footer d-flex justify-content-between align-items-center">
              <RouterLink to="/register" class="small">Create an account</RouterLink>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Signing in…' : 'Login' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
