<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AdminSecurityPolicy, AdminSSOSettings, Organization, SiteRole } from '@/api/types'
import { APIError } from '@/api/types'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'

const { push } = useToast()
const { isCompanyMode, refresh: refreshSite } = useSite()

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const loaded = ref<AdminSSOSettings | null>(null)
const teams = ref<Organization[]>([])

const form = reactive({
  enabled: false,
  label: '',
  issuer_url: '',
  client_id: '',
  client_secret: '',
  scopes: '',
  auto_create: false,
  allowed_domains: '',
  sso_only: false,
  groups_claim: '',
})
// Group → team rows; turned into team_map on save.
const teamRows = ref<{ group: string; team_id: number | null }[]>([])

const secretSet = computed(() => !!loaded.value?.client_secret_set)

function applyLoaded(s: AdminSSOSettings) {
  loaded.value = s
  form.enabled = s.enabled
  form.label = s.label
  form.issuer_url = s.issuer_url
  form.client_id = s.client_id
  form.client_secret = ''
  form.scopes = s.scopes
  form.auto_create = s.auto_create
  form.allowed_domains = s.allowed_domains.join(', ')
  form.sso_only = s.sso_only
  form.groups_claim = s.groups_claim
  teamRows.value = Object.entries(s.team_map).map(([group, team_id]) => ({ group, team_id }))
}

async function load() {
  loading.value = true
  try {
    applyLoaded(await api.getAdminSSO())
    if (isCompanyMode.value) {
      teams.value = (await api.listOrganizations()).filter((o) => !o.is_company)
    }
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load security settings', 'error')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const team_map: Record<string, number> = {}
    for (const row of teamRows.value) {
      if (row.group.trim() && row.team_id) team_map[row.group.trim()] = row.team_id
    }
    applyLoaded(
      await api.patchAdminSSO({
        enabled: form.enabled,
        label: form.label,
        issuer_url: form.issuer_url,
        client_id: form.client_id,
        ...(form.client_secret ? { client_secret: form.client_secret } : {}),
        scopes: form.scopes,
        auto_create: form.auto_create,
        allowed_domains: form.allowed_domains.split(/[\s,]+/).filter(Boolean),
        sso_only: form.sso_only,
        groups_claim: form.groups_claim,
        team_map,
      }),
    )
    await refreshSite()
    push('Single sign-on saved', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    saving.value = false
  }
}

async function test() {
  testing.value = true
  try {
    const res = await api.testAdminSSO()
    push(`Found the provider (${new URL(res.authorization_endpoint).host})`, 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testing.value = false
  }
}

async function copyRedirect() {
  if (!loaded.value) return
  try {
    await navigator.clipboard.writeText(loaded.value.redirect_uri)
    push('Copied', 'success')
  } catch {
    push('Copy failed', 'error')
  }
}

// --- Sign-in policy ---
const policy = ref<AdminSecurityPolicy | null>(null)
const roles = ref<SiteRole[]>([])
const savingPolicy = ref(false)
const policyForm = reactive({
  require_mfa: 'off' as AdminSecurityPolicy['require_mfa'],
  mfa_role_ids: [] as number[],
  min_password_length: 8,
  session_idle_minutes: 0,
  session_max_hours: 0,
  allowed_email_domains: '',
})

function applyPolicy(p: AdminSecurityPolicy) {
  policy.value = p
  policyForm.require_mfa = p.require_mfa
  policyForm.mfa_role_ids = [...p.mfa_role_ids]
  policyForm.min_password_length = p.min_password_length
  policyForm.session_idle_minutes = p.session_idle_minutes
  policyForm.session_max_hours = p.session_max_hours
  policyForm.allowed_email_domains = p.allowed_email_domains.join(', ')
}

async function loadPolicy() {
  try {
    const [p, r] = await Promise.all([api.getSecurityPolicy(), api.listSiteRoles()])
    applyPolicy(p)
    roles.value = r.roles
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load the sign-in policy', 'error')
  }
}

async function savePolicy() {
  savingPolicy.value = true
  try {
    applyPolicy(
      await api.patchSecurityPolicy({
        require_mfa: policyForm.require_mfa,
        mfa_role_ids: policyForm.mfa_role_ids,
        min_password_length: Number(policyForm.min_password_length),
        session_idle_minutes: Number(policyForm.session_idle_minutes),
        session_max_hours: Number(policyForm.session_max_hours),
        allowed_email_domains: policyForm.allowed_email_domains.split(/[\s,]+/).filter(Boolean),
      }),
    )
    push('Sign-in policy saved', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    savingPolicy.value = false
  }
}

onMounted(() => {
  void load()
  void loadPolicy()
})
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1 class="mb-1">Security</h1>
    <p class="text-muted small mb-3">How people sign in to this site.</p>

    <form v-if="policy" class="card mb-4" @submit.prevent="savePolicy">
      <div class="card-header">
        <h2 class="h5 mb-0">Sign-in policy</h2>
      </div>
      <div class="card-body">
        <fieldset class="mb-3">
          <legend class="form-label fw-semibold fs-6">Two-factor authentication</legend>
          <div class="form-check">
            <input id="mfa-off" v-model="policyForm.require_mfa" type="radio" value="off" class="form-check-input" />
            <label class="form-check-label" for="mfa-off">Optional</label>
          </div>
          <div class="form-check">
            <input id="mfa-all" v-model="policyForm.require_mfa" type="radio" value="all" class="form-check-input" />
            <label class="form-check-label" for="mfa-all">Required for everyone</label>
          </div>
          <div class="form-check">
            <input id="mfa-roles" v-model="policyForm.require_mfa" type="radio" value="roles" class="form-check-input" />
            <label class="form-check-label" for="mfa-roles">Required for these roles</label>
          </div>
          <div v-if="policyForm.require_mfa === 'roles'" class="ms-4 mt-1 d-flex flex-wrap gap-3">
            <div v-for="r in roles" :key="r.id" class="form-check">
              <input :id="`mfa-role-${r.id}`" v-model="policyForm.mfa_role_ids" type="checkbox" :value="r.id" class="form-check-input" />
              <label class="form-check-label" :for="`mfa-role-${r.id}`">{{ r.label }}</label>
            </div>
          </div>
          <div class="form-text">
            People who must use it can still sign in, but only reach their profile until they set it up, and can't turn
            it off. Single sign-on sessions are exempt: the identity provider handles the second factor.
          </div>
        </fieldset>

        <div class="row g-3 mb-3">
          <div class="col-sm-4">
            <label class="form-label" for="policy-min-pw">Minimum password length</label>
            <input id="policy-min-pw" v-model.number="policyForm.min_password_length" type="number" min="8" max="64" class="form-control" />
            <div class="form-text">Applies to new and changed passwords.</div>
          </div>
          <div class="col-sm-4">
            <label class="form-label" for="policy-idle">Sign out after inactivity (minutes)</label>
            <input id="policy-idle" v-model.number="policyForm.session_idle_minutes" type="number" min="0" class="form-control" />
            <div class="form-text">0 keeps people signed in while their session lasts.</div>
          </div>
          <div class="col-sm-4">
            <label class="form-label" for="policy-max">Maximum session length (hours)</label>
            <input id="policy-max" v-model.number="policyForm.session_max_hours" type="number" min="0" class="form-control" />
            <div class="form-text">0 uses the default ({{ policy.default_session_hours }} hours). Longer values can't extend it.</div>
          </div>
        </div>

        <label class="form-label" for="policy-domains">Email domains allowed for new accounts</label>
        <input id="policy-domains" v-model="policyForm.allowed_email_domains" class="form-control" placeholder="acme.com, acme.co.uk" />
        <div class="form-text">
          Applies to sign-up, invites, accounts made by people managers, and single sign-on. Existing accounts aren't
          affected. Leave empty to allow any domain.
        </div>
      </div>
      <div class="card-footer d-flex justify-content-end">
        <button type="submit" class="btn btn-primary" :disabled="savingPolicy">{{ savingPolicy ? 'Saving…' : 'Save policy' }}</button>
      </div>
    </form>

    <p v-if="loading" class="text-muted">Loading…</p>
    <form v-else class="card mb-4" @submit.prevent="save">
      <div class="card-header">
        <h2 class="h5 mb-0">Single sign-on (OpenID Connect)</h2>
      </div>
      <div class="card-body">
        <p class="small text-muted">
          Let people sign in with your identity provider: Google Workspace, Microsoft Entra ID, Okta, Authentik,
          Keycloak, or anything else that speaks OpenID Connect. Register an app with the provider, then paste its
          details here.
        </p>

        <div class="mb-3">
          <label class="form-label" for="sso-redirect">Redirect URI to register with the provider</label>
          <div class="input-group">
            <input id="sso-redirect" class="form-control font-monospace" :value="loaded?.redirect_uri" readonly />
            <button type="button" class="btn btn-outline-secondary" @click="copyRedirect">Copy</button>
          </div>
          <div class="form-text">Set <code>PUBLIC_URL</code> if this doesn't match the address people use.</div>
        </div>

        <div class="row g-3 mb-3">
          <div class="col-md-8">
            <label class="form-label" for="sso-issuer">Issuer URL</label>
            <input id="sso-issuer" v-model="form.issuer_url" class="form-control" placeholder="https://login.example.com/realms/acme" />
            <div class="form-text">
              Where <code>/.well-known/openid-configuration</code> lives, e.g. <code>https://accounts.google.com</code>.
            </div>
          </div>
          <div class="col-md-4">
            <label class="form-label" for="sso-label">Button label</label>
            <div class="input-group">
              <span class="input-group-text">Sign in with</span>
              <input id="sso-label" v-model="form.label" class="form-control" placeholder="SSO" maxlength="60" />
            </div>
          </div>
          <div class="col-md-6">
            <label class="form-label" for="sso-client-id">Client ID</label>
            <input id="sso-client-id" v-model="form.client_id" class="form-control" autocomplete="off" />
          </div>
          <div class="col-md-6">
            <label class="form-label" for="sso-client-secret">Client secret</label>
            <input
              id="sso-client-secret"
              v-model="form.client_secret"
              type="password"
              class="form-control"
              autocomplete="new-password"
              :placeholder="secretSet ? 'Saved; type to replace' : ''"
            />
          </div>
          <div class="col-md-6">
            <label class="form-label" for="sso-scopes">Extra scopes</label>
            <input id="sso-scopes" v-model="form.scopes" class="form-control" placeholder="email profile" />
            <div class="form-text"><code>openid</code> is always requested. Add <code>groups</code> if your provider needs it.</div>
          </div>
          <div class="col-md-6">
            <label class="form-label" for="sso-domains">Allowed email domains</label>
            <input id="sso-domains" v-model="form.allowed_domains" class="form-control" placeholder="acme.com, acme.co.uk" />
            <div class="form-text">Leave empty to allow any address the provider has verified.</div>
          </div>
        </div>

        <div class="form-check mb-2">
          <input id="sso-auto-create" v-model="form.auto_create" type="checkbox" class="form-check-input" />
          <label class="form-check-label" for="sso-auto-create">Create an account the first time someone signs in</label>
          <div class="form-text">
            Off: only people who already have an account (matched by verified email) can use SSO. New accounts get the
            default site role{{ isCompanyMode ? ' and join the company' : '' }}.
          </div>
        </div>
        <div class="form-check mb-3">
          <input id="sso-only" v-model="form.sso_only" type="checkbox" class="form-check-input" />
          <label class="form-check-label" for="sso-only">Require single sign-on</label>
          <div class="form-text">
            Turns off password sign-in and self-registration for everyone except admins, who keep their password so a
            provider outage can't lock you out.
          </div>
        </div>

        <fieldset v-if="isCompanyMode" class="border rounded p-3 mb-3">
          <legend class="h6 float-none w-auto px-1 mb-0">Teams from provider groups</legend>
          <p class="small text-muted">
            Each sign-in puts people on the teams mapped to their groups and takes them off mapped teams for groups
            they've left. Teams you don't map here are never changed.
          </p>
          <div class="mb-2" style="max-width: 20rem">
            <label class="form-label" for="sso-groups-claim">Groups claim</label>
            <input id="sso-groups-claim" v-model="form.groups_claim" class="form-control" placeholder="groups" />
          </div>
          <div v-for="(row, i) in teamRows" :key="i" class="d-flex gap-2 mb-2">
            <input v-model="row.group" class="form-control" placeholder="Provider group" :aria-label="`Group ${i + 1}`" />
            <select v-model="row.team_id" class="form-select" :aria-label="`Team for group ${i + 1}`">
              <option :value="null">Choose a team…</option>
              <option v-for="t in teams" :key="t.id" :value="t.id">{{ t.name }}</option>
            </select>
            <button type="button" class="btn btn-outline-danger" aria-label="Remove mapping" @click="teamRows.splice(i, 1)">
              <i class="bi bi-x-lg" aria-hidden="true" />
            </button>
          </div>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="teamRows.push({ group: '', team_id: null })">
            <i class="bi bi-plus-lg me-1" aria-hidden="true" />Map a group
          </button>
        </fieldset>

        <div class="form-check form-switch">
          <input id="sso-enabled" v-model="form.enabled" type="checkbox" class="form-check-input" role="switch" />
          <label class="form-check-label fw-semibold" for="sso-enabled">Single sign-on is on</label>
        </div>
      </div>
      <div class="card-footer d-flex gap-2 justify-content-end">
        <button type="button" class="btn btn-outline-secondary" :disabled="testing || !loaded?.issuer_url" @click="test">
          {{ testing ? 'Checking…' : 'Check saved issuer' }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? 'Saving…' : 'Save' }}</button>
      </div>
    </form>
  </div>
</template>
