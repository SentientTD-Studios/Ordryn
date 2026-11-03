<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { useSite } from '@/composables/useSite'
import { APIError, isMFARequired } from '@/api/types'
import {
  isDeviceAuthPath,
  resolvePostLoginRedirect,
  stashDeviceAuthReturn,
  takeDeviceAuthReturn,
} from '@/deviceAuthReturn'

const email = ref('')
const password = ref('')
const mfaCode = ref('')
const mfaRequired = ref(false)
const busy = ref(false)
const error = ref('')
const { login, verifyMFA } = useAuth()
const { push } = useToast()
const { enableJoinRequests, isCompanyMode } = useSite()
const router = useRouter()
const route = useRoute()

onMounted(() => {
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : null
  if (isDeviceAuthPath(redirect)) {
    stashDeviceAuthReturn(redirect)
  }
})

async function finishLogin() {
  push('Welcome back', 'success')
  const queryRedirect = typeof route.query.redirect === 'string' ? route.query.redirect : null
  const target = resolvePostLoginRedirect(queryRedirect, '/')
  if (isDeviceAuthPath(target)) {
    takeDeviceAuthReturn()
  }
  await router.replace(target)
}

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    if (mfaRequired.value) {
      await verifyMFA(mfaCode.value.trim())
      await finishLogin()
      return
    }
    const result = await login(email.value.trim(), password.value)
    if (isMFARequired(result)) {
      mfaRequired.value = true
      return
    }
    await finishLogin()
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Login failed'
  } finally {
    busy.value = false
  }
}

function backToPassword() {
  mfaRequired.value = false
  mfaCode.value = ''
  error.value = ''
}
</script>

<template>
  <div class="container mt-5">
    <div class="row justify-content-center">
      <div class="col-md-6 col-lg-5">
        <div class="card">
          <div class="card-header">
            <h2 class="card-title mb-0">{{ mfaRequired ? 'Two-factor authentication' : 'Login' }}</h2>
          </div>
          <form @submit.prevent="onSubmit">
            <div class="card-body">
              <template v-if="!mfaRequired">
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
              </template>
              <template v-else>
                <p class="text-muted">
                  Enter the 6-digit code from your authenticator app, or one of your recovery codes.
                </p>
                <div class="mb-3">
                  <label for="login-mfa" class="form-label">Authentication code</label>
                  <input
                    id="login-mfa"
                    v-model="mfaCode"
                    type="text"
                    class="form-control"
                    required
                    autocomplete="one-time-code"
                    autocapitalize="characters"
                    spellcheck="false"
                  />
                </div>
                <div v-if="error" class="text-danger mb-2">{{ error }}</div>
              </template>
            </div>
            <div class="card-footer d-flex justify-content-between align-items-center">
              <span v-if="!mfaRequired && isCompanyMode" class="small text-muted">
                Need an account? Ask your manager.
              </span>
              <span v-else-if="!mfaRequired" class="small">
                <RouterLink to="/register">Create an account</RouterLink>
                <template v-if="enableJoinRequests">
                  ·
                  <RouterLink to="/join">Request to join</RouterLink>
                </template>
              </span>
              <button v-else type="button" class="btn btn-link text-decoration-none px-0" @click="backToPassword">
                Back
              </button>
              <button type="submit" class="btn btn-primary" :disabled="busy">
                <template v-if="mfaRequired">
                  {{ busy ? 'Verifying…' : 'Verify' }}
                </template>
                <template v-else>
                  {{ busy ? 'Signing in…' : 'Login' }}
                </template>
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
