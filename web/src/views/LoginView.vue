<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const email = ref('')
const password = ref('')
const busy = ref(false)
const { login } = useAuth()
const { push } = useToast()
const router = useRouter()
const route = useRoute()

async function onSubmit() {
  busy.value = true
  try {
    await login(email.value.trim(), password.value)
    push('Welcome back', 'success')
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (err) {
    const msg = err instanceof APIError ? err.message : 'Login failed'
    push(msg, 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="auth-panel">
    <p class="eyebrow">Ordryn</p>
    <h1>Sign in</h1>
    <p class="lede">Use your account to manage tasks over the JSON API.</p>
    <form class="stack" @submit.prevent="onSubmit">
      <label>
        Email
        <input v-model="email" type="email" required autocomplete="username" />
      </label>
      <label>
        Password
        <input v-model="password" type="password" required autocomplete="current-password" />
      </label>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Signing in…' : 'Sign in' }}
      </button>
    </form>
    <p class="muted">
      No account?
      <RouterLink to="/register">Create one</RouterLink>
      ·
      <RouterLink to="/forgot-password">Forgot password?</RouterLink>
    </p>
  </section>
</template>
