<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { AdminSettings, AdminSettingsPatch } from '@/api/types'
import { APIError } from '@/api/types'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'
import TimezoneSelect from '@/components/TimezoneSelect.vue'
import AdminSubnav from '@/components/AdminSubnav.vue'

const toast = useToast()
const { refresh: refreshSite } = useSite()
const busy = ref(false)
const emailBusy = ref(false)
const mailgunApiKeyInput = ref('')
const smtpPasswordInput = ref('')
const githubOAuthSecretInput = ref('')
const githubBusy = ref(false)
const imageBusy = ref(false)
const imageTestBusy = ref(false)
const imageTestResult = ref<{ ok: boolean; message: string } | null>(null)
const s3SecretInput = ref('')
const settings = reactive<AdminSettings>({
  site_name: '',
  default_timezone: 'UTC',
  show_changelog: true,
  site_version: '',
  enable_registration: true,
  invite_only: false,
  enable_join_requests: false,
  meta_description: '',
  enable_global_announcement: false,
  global_announcement_text: '',
  enable_api: false,
  email_provider: '',
  email_from_address: '',
  email_from_name: '',
  email_mailgun_domain: '',
  email_mailgun_api_key_set: false,
  email_smtp_host: '',
  email_smtp_port: 587,
  email_smtp_username: '',
  email_smtp_password_set: false,
  email_smtp_tls: true,
  email_audit_retention_days: 7,
  github_oauth_client_id: '',
  github_oauth_client_secret_set: false,
  github_oauth_configured: false,
  image_hosting_provider: '',
  image_max_bytes: 5 * 1024 * 1024,
  image_s3_endpoint: '',
  image_s3_region: '',
  image_s3_bucket: '',
  image_s3_access_key: '',
  image_s3_secret_key_set: false,
  image_s3_public_url: '',
  image_s3_force_path_style: true,
  image_local_path: 'data/uploads',
})

async function load() {
  try {
    const s = await api.getAdminSettings()
    Object.assign(settings, s)
    mailgunApiKeyInput.value = ''
    smtpPasswordInput.value = ''
    githubOAuthSecretInput.value = ''
    s3SecretInput.value = ''
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load admin data', 'error')
  }
}

async function saveSettings() {
  busy.value = true
  try {
    const saved = await api.patchAdminSettings({
      site_name: settings.site_name,
      default_timezone: settings.default_timezone,
      show_changelog: settings.show_changelog,
      enable_registration: settings.enable_registration,
      invite_only: settings.invite_only,
      enable_join_requests: settings.enable_join_requests,
      meta_description: settings.meta_description,
      enable_global_announcement: settings.enable_global_announcement,
      global_announcement_text: settings.global_announcement_text,
      enable_api: settings.enable_api,
    })
    Object.assign(settings, saved)
    await refreshSite()
    toast.push('Settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busy.value = false
  }
}

async function saveEmailSettings() {
  emailBusy.value = true
  try {
    const payload: AdminSettingsPatch = {
      email_provider: settings.email_provider || '',
      email_from_address: settings.email_from_address,
      email_from_name: settings.email_from_name,
      email_mailgun_domain: settings.email_mailgun_domain,
      email_smtp_host: settings.email_smtp_host,
      email_smtp_port: settings.email_smtp_port,
      email_smtp_username: settings.email_smtp_username,
      email_smtp_tls: settings.email_smtp_tls,
      email_audit_retention_days: settings.email_audit_retention_days,
    }
    if (mailgunApiKeyInput.value !== '') {
      payload.email_mailgun_api_key = mailgunApiKeyInput.value
    }
    if (smtpPasswordInput.value !== '') {
      payload.email_smtp_password = smtpPasswordInput.value
    }
    const saved = await api.patchAdminSettings(payload)
    Object.assign(settings, saved)
    mailgunApiKeyInput.value = ''
    smtpPasswordInput.value = ''
    toast.push('Email settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    emailBusy.value = false
  }
}

async function saveGitHubOAuthSettings() {
  githubBusy.value = true
  try {
    const payload: AdminSettingsPatch = {
      github_oauth_client_id: settings.github_oauth_client_id,
    }
    if (githubOAuthSecretInput.value !== '') {
      payload.github_oauth_client_secret = githubOAuthSecretInput.value
    }
    const saved = await api.patchAdminSettings(payload)
    Object.assign(settings, saved)
    githubOAuthSecretInput.value = ''
    await refreshSite()
    toast.push('GitHub OAuth settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    githubBusy.value = false
  }
}

const imageMaxMB = computed({
  get() {
    const n = Number(settings.image_max_bytes) || 5 * 1024 * 1024
    if (n >= 1 && n <= 50) return n
    return Math.max(1, Math.round(n / (1024 * 1024)))
  },
  set(v: number) {
    const mb = Math.min(50, Math.max(1, Number(v) || 1))
    settings.image_max_bytes = mb * 1024 * 1024
  },
})

function imageHostingFormPayload(): AdminSettingsPatch {
  const mb = Math.min(50, Math.max(1, Number(imageMaxMB.value) || 1))
  settings.image_max_bytes = mb * 1024 * 1024
  const payload: AdminSettingsPatch = {
    image_hosting_provider: settings.image_hosting_provider || '',
    image_max_bytes: settings.image_max_bytes,
    image_s3_endpoint: (settings.image_s3_endpoint || '').trim(),
    image_s3_region: (settings.image_s3_region || '').trim(),
    image_s3_bucket: (settings.image_s3_bucket || '').trim(),
    image_s3_access_key: (settings.image_s3_access_key || '').trim(),
    image_s3_public_url: (settings.image_s3_public_url || '').trim(),
    image_s3_force_path_style: !!settings.image_s3_force_path_style,
    image_local_path: (settings.image_local_path || '').trim(),
  }
  const secret = s3SecretInput.value.trim()
  if (secret !== '') {
    payload.image_s3_secret_key = secret
  }
  return payload
}

async function saveImageHostingSettings() {
  imageBusy.value = true
  try {
    const saved = await api.patchAdminSettings(imageHostingFormPayload())
    Object.assign(settings, saved)
    s3SecretInput.value = ''
    await refreshSite()
    toast.push('Image hosting settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    imageBusy.value = false
  }
}

async function testImageHosting() {
  imageTestBusy.value = true
  imageTestResult.value = null
  try {
    const result = await api.testImageHosting(imageHostingFormPayload())
    imageTestResult.value = { ok: result.ok, message: result.message }
    toast.push(result.message, result.ok ? 'success' : 'error')
  } catch (err) {
    const message = err instanceof APIError ? err.message : 'Connection test failed'
    imageTestResult.value = { ok: false, message }
    toast.push(message, 'error')
  } finally {
    imageTestBusy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Admin</h1>
    <div class="card mb-4">
      <div class="card-header"><h2 class="h5 mb-0">Site settings</h2></div>
      <div class="card-body">
        <form @submit.prevent="saveSettings">
          <div class="mb-3">
            <label class="form-label">Site name</label>
            <input v-model="settings.site_name" type="text" class="form-control" required />
          </div>
          <div class="mb-3">
            <label for="admin-default-timezone" class="form-label">Default timezone</label>
            <TimezoneSelect id="admin-default-timezone" v-model="settings.default_timezone" required />
          </div>
          <div class="mb-3">
            <label class="form-label">Meta description</label>
            <textarea v-model="settings.meta_description" class="form-control" rows="2" />
          </div>
          <div class="form-check mb-2">
            <input id="admin-registration" v-model="settings.enable_registration" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-registration">Enable registration</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-invite-only" v-model="settings.invite_only" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-invite-only">Invite only</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-join-requests" v-model="settings.enable_join_requests" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-join-requests">Enable join requests</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-changelog" v-model="settings.show_changelog" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-changelog">Show changelog</label>
          </div>
          <div class="form-check mb-2">
            <input id="admin-api" v-model="settings.enable_api" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-api">Enable external REST API (API keys &amp; Android)</label>
          </div>
          <p class="text-muted small">The web app always uses the JSON API with your session cookie. This toggle controls Bearer access for scripts and mobile clients.</p>
          <div class="form-check mb-2">
            <input id="admin-announcement" v-model="settings.enable_global_announcement" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="admin-announcement">Global announcement</label>
          </div>
          <div class="mb-3">
            <label class="form-label">Announcement text</label>
            <textarea v-model="settings.global_announcement_text" class="form-control" rows="2" maxlength="500" />
          </div>
          <p class="text-muted">Site Version: {{ settings.site_version || '—' }} <span class="small">(running binary)</span></p>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? 'Saving…' : 'Save settings' }}
          </button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h2 class="h5 mb-0">GitHub OAuth</h2></div>
      <div class="card-body">
        <p class="text-muted small">
          Optional. When configured, users can connect GitHub via OAuth in Settings.
          Create an OAuth App on GitHub with callback
          <code>/api/v1/auth/github/callback</code>
          (include your site base path if applicable). Users can always connect with a personal access token instead.
        </p>
        <form @submit.prevent="saveGitHubOAuthSettings">
          <div class="mb-3">
            <label class="form-label" for="github-oauth-client-id">Client ID</label>
            <input
              id="github-oauth-client-id"
              v-model="settings.github_oauth_client_id"
              type="text"
              class="form-control"
              autocomplete="off"
            />
          </div>
          <div class="mb-3">
            <label class="form-label" for="github-oauth-client-secret">Client secret</label>
            <input
              id="github-oauth-client-secret"
              v-model="githubOAuthSecretInput"
              type="password"
              class="form-control"
              autocomplete="new-password"
              :placeholder="
                settings.github_oauth_client_secret_set
                  ? '•••• configured (leave blank to keep)'
                  : 'Enter client secret'
              "
            />
          </div>
          <p class="small mb-3">
            Status:
            <span v-if="settings.github_oauth_configured" class="text-success">Configured</span>
            <span v-else class="text-muted">Not configured</span>
          </p>
          <button type="submit" class="btn btn-primary" :disabled="githubBusy">
            {{ githubBusy ? 'Saving…' : 'Save GitHub OAuth' }}
          </button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h2 class="h5 mb-0">Email</h2></div>
      <div class="card-body">
        <form @submit.prevent="saveEmailSettings">
          <div class="mb-3">
            <label class="form-label" for="email-provider">Provider</label>
            <select id="email-provider" v-model="settings.email_provider" class="form-select">
              <option value="">Disabled</option>
              <option value="mailgun">Mailgun</option>
              <option value="smtp">SMTP</option>
            </select>
          </div>

          <template v-if="settings.email_provider === 'mailgun' || settings.email_provider === 'smtp'">
            <div class="mb-3">
              <label class="form-label" for="email-from-name">From name</label>
              <input id="email-from-name" v-model="settings.email_from_name" type="text" class="form-control" placeholder="GoTodo" />
            </div>
            <div class="mb-3">
              <label class="form-label" for="email-from-address">From address</label>
              <input
                id="email-from-address"
                v-model="settings.email_from_address"
                type="email"
                class="form-control"
                placeholder="noreply@ordryn.com"
                required
              />
              <div class="form-text">
                Shown to recipients. May differ from the Mailgun or SMTP sending domain.
              </div>
            </div>
          </template>

          <template v-if="settings.email_provider === 'mailgun'">
            <div class="mb-3">
              <label class="form-label" for="email-mailgun-domain">Mailgun domain</label>
              <input
                id="email-mailgun-domain"
                v-model="settings.email_mailgun_domain"
                type="text"
                class="form-control"
                placeholder="mydomain.com"
                required
              />
              <div class="form-text">Sending domain used by Mailgun (signing/routing), not necessarily the From address.</div>
            </div>
            <div class="mb-3">
              <label class="form-label" for="email-mailgun-key">Mailgun API key</label>
              <input
                id="email-mailgun-key"
                v-model="mailgunApiKeyInput"
                type="password"
                class="form-control"
                autocomplete="new-password"
                :placeholder="settings.email_mailgun_api_key_set ? '•••• configured (leave blank to keep)' : 'Enter API key'"
              />
            </div>
          </template>

          <template v-if="settings.email_provider === 'smtp'">
            <div class="mb-3">
              <label class="form-label" for="email-smtp-host">SMTP host</label>
              <input id="email-smtp-host" v-model="settings.email_smtp_host" type="text" class="form-control" placeholder="smtp.example.com" required />
            </div>
            <div class="mb-3">
              <label class="form-label" for="email-smtp-port">SMTP port</label>
              <input id="email-smtp-port" v-model.number="settings.email_smtp_port" type="number" class="form-control" min="1" max="65535" required />
              <div class="form-text">Use 587 with STARTTLS, or 465 for implicit TLS.</div>
            </div>
            <div class="mb-3">
              <label class="form-label" for="email-smtp-username">SMTP username</label>
              <input id="email-smtp-username" v-model="settings.email_smtp_username" type="text" class="form-control" required />
            </div>
            <div class="mb-3">
              <label class="form-label" for="email-smtp-password">SMTP password</label>
              <input
                id="email-smtp-password"
                v-model="smtpPasswordInput"
                type="password"
                class="form-control"
                autocomplete="new-password"
                :placeholder="settings.email_smtp_password_set ? '•••• configured (leave blank to keep)' : 'Enter password'"
              />
            </div>
            <div class="form-check mb-3">
              <input id="email-smtp-tls" v-model="settings.email_smtp_tls" class="form-check-input" type="checkbox" />
              <label class="form-check-label" for="email-smtp-tls">Use STARTTLS (ignored for port 465, which always uses TLS)</label>
            </div>
          </template>

          <p v-if="!settings.email_provider" class="text-muted small">
            Outbound email is disabled. Password resets and notifications will not send until a provider is configured.
          </p>

          <div class="mb-3">
            <label class="form-label" for="email-audit-retention">Audit log retention (days)</label>
            <input
              id="email-audit-retention"
              v-model.number="settings.email_audit_retention_days"
              type="number"
              class="form-control"
              min="1"
              max="90"
              required
              style="max-width: 8rem"
            />
            <div class="form-text">
              Outbound email attempts are kept this many days, then deleted automatically.
              <RouterLink to="/admin/email-audit">View email log</RouterLink>
            </div>
          </div>

          <button type="submit" class="btn btn-primary" :disabled="emailBusy">
            {{ emailBusy ? 'Saving…' : 'Save email settings' }}
          </button>
        </form>
      </div>
    </div>

    <div class="card mb-4">
      <div class="card-header"><h2 class="h5 mb-0">Image hosting</h2></div>
      <div class="card-body">
        <p class="text-muted small">
          Optional. When configured, signed-in users can upload JPEG, PNG, GIF, or WebP images
          for task descriptions and comments. Use S3 for AWS, Cloudflare R2, DigitalOcean Spaces,
          or other S3-compatible APIs. Local uploads are stored on this server and served from
          <code>/uploads/</code>.
        </p>
        <form novalidate @submit.prevent="saveImageHostingSettings">
          <div class="mb-3">
            <label class="form-label" for="image-provider">Provider</label>
            <select id="image-provider" v-model="settings.image_hosting_provider" class="form-select">
              <option value="">Disabled</option>
              <option value="s3">S3-compatible (AWS, Cloudflare R2, DigitalOcean Spaces, MinIO)</option>
              <option value="local">Local uploads</option>
            </select>
          </div>
          <div class="mb-3">
            <label class="form-label" for="image-max-mb">Maximum image size (MB)</label>
            <input
              id="image-max-mb"
              v-model.number="imageMaxMB"
              type="number"
              class="form-control"
              min="1"
              max="50"
              required
              style="max-width: 8rem"
            />
            <div class="form-text">Applies to all providers. Allowed range is 1–50 MB.</div>
          </div>

          <template v-if="settings.image_hosting_provider === 's3'">
            <div class="mb-3">
              <label class="form-label" for="image-s3-endpoint">API endpoint</label>
              <input
                id="image-s3-endpoint"
                v-model="settings.image_s3_endpoint"
                type="text"
                inputmode="url"
                autocomplete="off"
                class="form-control"
                placeholder="https://nyc3.digitaloceanspaces.com"
                required
              />
              <div class="form-text">
                S3 API host only — do not include the bucket in the path. Examples:
                <code>https://s3.us-east-1.amazonaws.com</code>,
                <code>https://&lt;accountid&gt;.r2.cloudflarestorage.com</code>,
                <code>https://nyc3.digitaloceanspaces.com</code>.
                Cloudflare's dashboard copies
                <code>https://&lt;accountid&gt;.r2.cloudflarestorage.com/&lt;bucket&gt;</code>;
                drop the bucket segment.
              </div>
            </div>
            <div class="mb-3">
              <label class="form-label" for="image-s3-region">Region</label>
              <input
                id="image-s3-region"
                v-model="settings.image_s3_region"
                type="text"
                class="form-control"
                placeholder="auto"
                required
              />
              <div class="form-text">Use <code>auto</code> for Cloudflare R2.</div>
            </div>
            <div class="mb-3">
              <label class="form-label" for="image-s3-bucket">Bucket</label>
              <input
                id="image-s3-bucket"
                v-model="settings.image_s3_bucket"
                type="text"
                class="form-control"
                required
              />
            </div>
            <div class="mb-3">
              <label class="form-label" for="image-s3-access-key">Access key</label>
              <input
                id="image-s3-access-key"
                v-model="settings.image_s3_access_key"
                type="text"
                class="form-control"
                autocomplete="off"
                required
              />
            </div>
            <div class="mb-3">
              <label class="form-label" for="image-s3-secret">Secret key</label>
              <input
                id="image-s3-secret"
                v-model="s3SecretInput"
                type="password"
                class="form-control"
                autocomplete="new-password"
                :placeholder="
                  settings.image_s3_secret_key_set
                    ? '•••• configured (leave blank to keep)'
                    : 'Enter secret key'
                "
              />
            </div>
            <div class="mb-3">
              <label class="form-label" for="image-s3-public-url">Public URL</label>
              <input
                id="image-s3-public-url"
                v-model="settings.image_s3_public_url"
                type="text"
                inputmode="url"
                autocomplete="off"
                class="form-control"
                placeholder="https://cdn.example.com"
                required
              />
              <div class="form-text">
                Base URL used in markdown. For Spaces this is often the CDN endpoint; for R2, the public development URL or a custom domain.
              </div>
            </div>
            <div class="form-check mb-3">
              <input
                id="image-s3-path-style"
                v-model="settings.image_s3_force_path_style"
                class="form-check-input"
                type="checkbox"
              />
              <label class="form-check-label" for="image-s3-path-style">
                Use path-style URLs (recommended for R2, Spaces, and MinIO)
              </label>
            </div>
          </template>

          <template v-if="settings.image_hosting_provider === 'local'">
            <div class="mb-3">
              <label class="form-label" for="image-local-path">Upload directory</label>
              <input
                id="image-local-path"
                v-model="settings.image_local_path"
                type="text"
                class="form-control"
                placeholder="data/uploads"
              />
              <div class="form-text">Relative to the process working directory. Default is <code>data/uploads</code>.</div>
            </div>
          </template>

          <p v-if="!settings.image_hosting_provider" class="text-muted small">
            Image uploads are disabled until a provider is selected.
          </p>

          <div
            v-if="imageTestResult"
            class="alert"
            :class="imageTestResult.ok ? 'alert-success' : 'alert-danger'"
            role="status"
          >
            {{ imageTestResult.message }}
          </div>

          <button type="submit" class="btn btn-primary" :disabled="imageBusy">
            {{ imageBusy ? 'Saving…' : 'Save image hosting' }}
          </button>
          <button
            type="button"
            class="btn btn-outline-secondary ms-2"
            :disabled="imageTestBusy || !settings.image_hosting_provider"
            @click="testImageHosting"
          >
            {{ imageTestBusy ? 'Testing…' : 'Test connection' }}
          </button>
          <div class="form-text mt-2">
            Test connection uploads a tiny image and deletes it. It uses the values on this form,
            including an unsaved secret key.
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
