<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

const route = useRoute()
const router = useRouter()
const { push } = useToast()

const token = ref('')
const id = ref('')
const email = ref('')
const valid = ref(false)
const checking = ref(true)
const newPassword = ref('')
const confirmPassword = ref('')
const busy = ref(false)
const done = ref(false)

onMounted(async () => {
  const t = route.query.token
  const i = route.query.id
  if (typeof t !== 'string' || typeof i !== 'string' || !t || !i) {
    checking.value = false
    return
  }
  token.value = t
  id.value = i
  try {
    const result = await api.validateResetToken(t, i)
    valid.value = result.valid
    email.value = result.email
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Invalid or expired link', 'error')
  } finally {
    checking.value = false
  }
})

async function onSubmit() {
  busy.value = true
  try {
    await api.resetPassword({
      id: id.value,
      token: token.value,
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })
    done.value = true
    push('Password updated', 'success')
    await router.replace({ name: 'login' })
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Reset failed', 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="auth-panel">
    <p class="eyebrow">Ordryn</p>
    <h1>Choose a new password</h1>

    <p v-if="checking" class="muted">Validating reset link…</p>

    <p v-else-if="!valid" class="muted">
      This reset link is invalid or expired.
      <RouterLink to="/forgot-password">Request a new one</RouterLink>
    </p>

    <p v-else-if="done" class="muted">
      Password updated.
      <RouterLink to="/login">Sign in</RouterLink>
    </p>

    <template v-else>
      <p class="lede">Set a new password for {{ email }}.</p>
      <form class="stack" @submit.prevent="onSubmit">
        <label>
          New password
          <input v-model="newPassword" type="password" required minlength="8" autocomplete="new-password" />
        </label>
        <label>
          Confirm password
          <input v-model="confirmPassword" type="password" required minlength="8" autocomplete="new-password" />
        </label>
        <button class="primary" type="submit" :disabled="busy">
          {{ busy ? 'Saving…' : 'Update password' }}
        </button>
      </form>
    </template>
  </section>
</template>
