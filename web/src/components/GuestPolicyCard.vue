<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import { APIError, type GuestPolicy, type SiteRole } from '@/api/types'
import { useToast } from '@/composables/useToast'

// Company mode, site admins: what guest accounts (people outside the company) can reach.
const { push } = useToast()

// Site permissions a guest's role may not hold (everything site-wide but Create projects).
const BLOCKED = new Set([
  'admin', 'orgs:create', 'createinvites', 'users:manage', 'teams:manage', 'team:people', 'people:profile',
  'projects:view_all', 'audit:view', 'roles:manage', 'settings:manage', 'queries:site',
])

const policy = ref<GuestPolicy | null>(null)
const roles = ref<SiteRole[]>([])
const saving = ref(false)
const loadError = ref('')

const guestRoles = computed(() => roles.value.filter((r) => !r.permissions.some((p) => BLOCKED.has(p))))

onMounted(async () => {
  try {
    const [p, r] = await Promise.all([api.getGuestPolicy(), api.listSiteRoles()])
    policy.value = p
    roles.value = r.roles
  } catch (err) {
    loadError.value = err instanceof APIError ? err.message : 'Could not load the guest policy'
  }
})

async function save() {
  if (!policy.value) return
  saving.value = true
  try {
    const p = policy.value
    policy.value = await api.saveGuestPolicy({
      ...p,
      role_id: p.access_level === 'full' ? p.role_id : null,
    })
    push('Guest policy saved. Project access was updated.', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not save the guest policy', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card mb-4">
    <div class="card-header d-flex justify-content-between align-items-center">
      <h2 class="h5 mb-0">Guests</h2>
      <span v-if="policy?.guest_count" class="badge text-bg-secondary">{{ policy.guest_count }} guests</span>
    </div>
    <div class="card-body">
      <p class="small text-muted">
        Guest accounts are for people outside the company, such as clients or contractors. They aren't part of the
        company, so projects shared with everyone never reach them. Make someone a guest, or create a guest account,
        on the <RouterLink to="/people">People</RouterLink> page.
      </p>
      <div v-if="loadError" class="alert alert-warning py-2 mb-0">{{ loadError }}</div>
      <form v-else-if="policy" @submit.prevent="save">
        <div class="form-check form-switch mb-3">
          <input id="guest-enabled" v-model="policy.enabled" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="guest-enabled">Allow guest accounts</label>
          <div class="form-text">Turning this off stops new guests. Existing guests keep the access below.</div>
        </div>

        <fieldset class="mb-3">
          <legend class="form-label fs-6 mb-1">On projects shared with them, guests can</legend>
          <div class="form-check">
            <input id="guest-view" v-model="policy.access_level" class="form-check-input" type="radio" value="view" />
            <label class="form-check-label" for="guest-view">Only view, whatever the share says</label>
          </div>
          <div class="form-check">
            <input id="guest-full" v-model="policy.access_level" class="form-check-input" type="radio" value="full" />
            <label class="form-check-label" for="guest-full">Work like this role, at most:</label>
          </div>
          <select
            v-if="policy.access_level === 'full'"
            v-model="policy.role_id"
            class="form-select form-select-sm mt-1 ms-4"
            style="max-width: 20rem"
            aria-label="Guest role"
            required
          >
            <option :value="null" disabled>Choose a role</option>
            <option v-for="r in guestRoles" :key="r.id" :value="r.id">{{ r.label }}</option>
          </select>
          <div v-if="policy.access_level === 'full'" class="form-text ms-4">
            A share giving a weaker role still gives the weaker one. Only roles without site-wide permissions (other than
            creating projects) are listed.
          </div>
        </fieldset>

        <div class="form-check mb-2">
          <input id="guest-teams" v-model="policy.via_teams" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="guest-teams">Shares with a team reach guests on that team</label>
          <div class="form-text">Off: only projects shared with a guest personally.</div>
        </div>
        <div class="form-check mb-3">
          <input id="guest-directory" v-model="policy.directory" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="guest-directory">Guests can look up anyone's username</label>
          <div class="form-text">Off: they only find people on their own projects.</div>
        </div>

        <button
          class="btn btn-primary"
          type="submit"
          :disabled="saving || (policy.access_level === 'full' && !policy.role_id)"
        >
          {{ saving ? 'Saving…' : 'Save guest policy' }}
        </button>
      </form>
    </div>
  </div>
</template>
