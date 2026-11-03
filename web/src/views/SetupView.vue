<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'
import type { SetupRegistration, SiteMode } from '@/api/types'
import TimezoneSelect from '@/components/TimezoneSelect.vue'

const { completeSetup } = useAuth()
const site = useSite()
const { push } = useToast()
const router = useRouter()

const setupToken = ref('')
const email = ref('')
const userName = ref('')
const password = ref('')
const confirm = ref('')
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
const siteName = ref(site.siteName.value)
const siteMode = ref<SiteMode>('multi')
const registration = ref<SetupRegistration>('invite')
const enableAPI = ref(false)
const busy = ref(false)
const error = ref('')

async function onSubmit() {
  busy.value = true
  error.value = ''
  try {
    await completeSetup({
      setup_token: setupToken.value.trim(),
      email: email.value.trim(),
      user_name: userName.value.trim(),
      password: password.value,
      confirm_password: confirm.value,
      timezone: timezone.value,
      site_name: siteName.value.trim(),
      site_mode: siteMode.value,
      registration: registration.value,
      enable_api: enableAPI.value,
    })
    await site.refresh()
    push('Setup complete. You are signed in as the admin.', 'success')
    await router.replace({ name: 'admin' })
  } catch (err) {
    if (err instanceof APIError && err.code === 'setup_complete') {
      await site.refresh()
      await router.replace({ name: 'login' })
      return
    }
    error.value = err instanceof APIError ? err.message : 'Setup failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="container mt-3 mb-5">
    <div class="row justify-content-center">
      <div class="col-md-8 col-lg-7">
        <h1>Set up {{ site.siteName.value }}</h1>
        <p class="text-muted">
          Create the admin account and choose how this site works. You can change everything except the site mode
          later in Admin.
        </p>
        <form @submit.prevent="onSubmit">
          <div class="card mb-3">
            <div class="card-body">
              <h2 class="h5 card-title">Setup token</h2>
              <label for="setup-token" class="form-label">Token from the server log</label>
              <input
                id="setup-token"
                v-model="setupToken"
                type="text"
                class="form-control font-monospace"
                required
                autocomplete="off"
                spellcheck="false"
                placeholder="xxxx-xxxx-xxxx-xxxx"
              />
              <div class="form-text">
                The server prints this when it starts without any accounts. It proves you run this server.
              </div>
            </div>
          </div>

          <div class="card mb-3">
            <div class="card-body">
              <h2 class="h5 card-title">Your account</h2>
              <p class="small text-muted">
                You'll be the site owner: the one account above every admin. Only you can make, change, or deactivate
                admins, and you can hand ownership to another admin later.
              </p>
              <div class="mb-3">
                <label for="setup-email" class="form-label">Email</label>
                <input id="setup-email" v-model="email" type="email" class="form-control" required autocomplete="username" />
              </div>
              <div class="mb-3">
                <label for="setup-username" class="form-label">Username</label>
                <input
                  id="setup-username"
                  v-model="userName"
                  type="text"
                  class="form-control"
                  required
                  minlength="3"
                  maxlength="32"
                  pattern="[A-Za-z0-9_]+"
                  autocomplete="nickname"
                />
                <div class="form-text">3–32 characters; letters, numbers, and underscores only.</div>
              </div>
              <div class="row">
                <div class="col-sm-6 mb-3">
                  <label for="setup-password" class="form-label">Password</label>
                  <input id="setup-password" v-model="password" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
                </div>
                <div class="col-sm-6 mb-3">
                  <label for="setup-confirm" class="form-label">Confirm password</label>
                  <input id="setup-confirm" v-model="confirm" type="password" class="form-control" required minlength="8" autocomplete="new-password" />
                </div>
              </div>
              <div>
                <label for="setup-timezone" class="form-label">Timezone</label>
                <TimezoneSelect id="setup-timezone" v-model="timezone" required />
                <div class="form-text">Also used as the site's default timezone.</div>
              </div>
            </div>
          </div>

          <div class="card mb-3">
            <div class="card-body">
              <h2 class="h5 card-title">Site</h2>
              <div class="mb-3">
                <label for="setup-site-name" class="form-label">Site name</label>
                <input id="setup-site-name" v-model="siteName" type="text" class="form-control" />
              </div>

              <fieldset class="mb-3">
                <legend class="form-label fs-6">Site mode</legend>
                <div class="form-check mb-2">
                  <input id="setup-mode-multi" v-model="siteMode" class="form-check-input" type="radio" value="multi" />
                  <label class="form-check-label" for="setup-mode-multi">
                    <span class="fw-semibold">Multi-user</span>
                    <span class="d-block small text-muted">
                      Independent people and groups share the site. Anyone allowed to can create their own organizations.
                    </span>
                  </label>
                </div>
                <div class="form-check">
                  <input id="setup-mode-company" v-model="siteMode" class="form-check-input" type="radio" value="company" />
                  <label class="form-check-label" for="setup-mode-company">
                    <span class="fw-semibold">Company</span>
                    <span class="d-block small text-muted">
                      The site is one company. Everyone joins it automatically, every project belongs to it, and admins
                      create teams inside it.
                    </span>
                  </label>
                </div>
                <div class="form-text">
                  You can switch until the first project or organization is created. After that it's permanent.
                </div>
              </fieldset>

              <p v-if="siteMode === 'company'" class="small mb-3">
                <span class="fw-semibold d-block">Who can create an account</span>
                <span class="text-muted">
                  In a company, people don't sign themselves up. Admins create accounts, and so can roles you give the
                  "Manage people" permission or team managers with "Manage team members' accounts". They can also
                  deactivate accounts when someone leaves.
                </span>
              </p>
              <fieldset v-else class="mb-3">
                <legend class="form-label fs-6">Who can create an account</legend>
                <div class="form-check">
                  <input id="setup-reg-invite" v-model="registration" class="form-check-input" type="radio" value="invite" />
                  <label class="form-check-label" for="setup-reg-invite">Only people with an invite</label>
                </div>
                <div class="form-check">
                  <input id="setup-reg-open" v-model="registration" class="form-check-input" type="radio" value="open" />
                  <label class="form-check-label" for="setup-reg-open">Anyone who visits the site</label>
                </div>
                <div class="form-check">
                  <input id="setup-reg-closed" v-model="registration" class="form-check-input" type="radio" value="closed" />
                  <label class="form-check-label" for="setup-reg-closed">Nobody; admins add accounts</label>
                </div>
              </fieldset>

              <div class="form-check">
                <input id="setup-enable-api" v-model="enableAPI" class="form-check-input" type="checkbox" />
                <label class="form-check-label" for="setup-enable-api">
                  Enable the API
                  <span class="d-block small text-muted">Lets users create API keys for scripts, apps, and AI agents.</span>
                </label>
              </div>
            </div>
          </div>

          <p class="small text-muted">Email delivery, sign-in providers, and the rest are in Admin once you're signed in.</p>
          <div v-if="error" class="text-danger mb-3">{{ error }}</div>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? 'Setting up…' : 'Finish setup' }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>
