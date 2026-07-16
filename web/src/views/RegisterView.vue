<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const email = ref('')
const password = ref('')
const confirm = ref('')
const invite = ref('')
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
const busy = ref(false)
const { register } = useAuth()
const { push } = useToast()
const router = useRouter()

async function onSubmit() {
  busy.value = true
  try {
    await register({
      email: email.value.trim(),
      password: password.value,
      confirm_password: confirm.value,
      timezone: timezone.value,
      invite_token: invite.value.trim() || undefined,
    })
    push('Account created', 'success')
    await router.replace('/')
  } catch (err) {
    const msg = err instanceof APIError ? err.message : 'Registration failed'
    push(msg, 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="auth-panel">
    <p class="eyebrow">Ordryn</p>
    <h1>Create account</h1>
    <p class="lede">Register against the local API. Invite token is required when invite-only is on.</p>
    <form class="stack" @submit.prevent="onSubmit">
      <label>
        Email
        <input v-model="email" type="email" required autocomplete="username" />
      </label>
      <label>
        Password
        <input v-model="password" type="password" required minlength="8" autocomplete="new-password" />
      </label>
      <label>
        Confirm password
        <input v-model="confirm" type="password" required minlength="8" autocomplete="new-password" />
      </label>
      <label>
        Timezone
        <input v-model="timezone" type="text" required />
      </label>
      <label>
        Invite token <span class="optional">(optional)</span>
        <input v-model="invite" type="text" autocomplete="off" />
      </label>
      <button class="primary" type="submit" :disabled="busy">
        {{ busy ? 'Creating…' : 'Create account' }}
      </button>
    </form>
    <p class="muted">
      Already registered?
      <RouterLink to="/login">Sign in</RouterLink>
    </p>
  </section>
</template>
