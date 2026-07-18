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
const error = ref('')

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
    error.value = err instanceof APIError ? err.message : 'Invalid or expired link'
  } finally {
    checking.value = false
  }
})

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    await api.resetPassword({
      id: id.value,
      token: token.value,
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })
    push('Password updated', 'success')
    await router.replace({ name: 'login' })
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Reset failed'
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
            <h2 class="card-title mb-0">Choose a new password</h2>
          </div>
          <div class="card-body">
            <p v-if="checking" class="text-muted">Validating reset link…</p>
            <div v-else-if="!valid" class="alert alert-warning">
              This reset link is invalid or expired.
              <div class="mt-2">
                <RouterLink to="/forgot-password">Request a new one</RouterLink>
              </div>
            </div>
            <template v-else>
              <p class="text-muted">Set a new password for {{ email }}.</p>
              <form @submit.prevent="onSubmit">
                <div class="mb-3">
                  <label for="reset-password" class="form-label">New password</label>
                  <input id="reset-password" v-model="newPassword" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
                </div>
                <div class="mb-3">
                  <label for="reset-confirm" class="form-label">Confirm password</label>
                  <input id="reset-confirm" v-model="confirmPassword" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
                </div>
                <div v-if="error" class="text-danger mb-3">{{ error }}</div>
                <button type="submit" class="btn btn-primary" :disabled="busy">
                  {{ busy ? 'Saving…' : 'Update password' }}
                </button>
              </form>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
