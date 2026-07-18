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
const error = ref('')
const { register } = useAuth()
const { push } = useToast()
const router = useRouter()

async function onSubmit() {
  busy.value = true
  error.value = ''
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
    error.value = err instanceof APIError ? err.message : 'Registration failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="container mt-3">
    <div class="row justify-content-center">
      <div class="col-md-6">
        <div class="card">
          <div class="card-body">
            <h1 class="card-title">Sign Up</h1>
            <form @submit.prevent="onSubmit">
              <div class="mb-3">
                <label for="signup-email" class="form-label">Email</label>
                <input id="signup-email" v-model="email" type="email" class="form-control" required autocomplete="username" />
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
                <input id="signup-timezone" v-model="timezone" type="text" class="form-control" required />
              </div>
              <div class="mb-3">
                <label for="signup-invite" class="form-label">Invite token <span class="text-muted">(optional)</span></label>
                <input id="signup-invite" v-model="invite" type="text" class="form-control" autocomplete="off" />
              </div>
              <div v-if="error" class="text-danger mb-3">{{ error }}</div>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                {{ busy ? 'Creating…' : 'Create account' }}
              </button>
            </form>
            <p class="mt-3 mb-0 text-muted">
              Already registered?
              <RouterLink to="/login">Sign in</RouterLink>
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
