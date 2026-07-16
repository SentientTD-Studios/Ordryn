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
const { push } = useToast()

async function onSubmit() {
  busy.value = true
  try {
    await api.forgotPassword(email.value.trim(), confirmEmail.value.trim())
    sent.value = true
    push('If an account exists, a reset link was sent', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Request failed', 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="auth-panel">
    <p class="eyebrow">Ordryn</p>
    <h1>Reset password</h1>
    <p class="lede">Enter your email twice. We will send a reset link if the account exists.</p>

    <p v-if="sent" class="muted">
      Check your inbox for a link (expires in 15 minutes).
      <RouterLink to="/login">Back to sign in</RouterLink>
    </p>

    <form v-else class="stack" @submit.prevent="onSubmit">
      <label>
        Email
        <input v-model="email" type="email" required autocomplete="username" />
      </label>
      <label>
        Confirm email
        <input v-model="confirmEmail" type="email" required autocomplete="username" />
      </label>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Sending…' : 'Send reset link' }}
      </button>
    </form>

    <p v-if="!sent" class="muted">
      Remembered it?
      <RouterLink to="/login">Sign in</RouterLink>
    </p>
  </section>
</template>
