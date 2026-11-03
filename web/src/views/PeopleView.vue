<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { PeopleList, Person } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'
import { personLabel } from '@/utils/personLabel'

const toast = useToast()
const { askConfirm } = useConfirm()

const data = ref<PeopleList | null>(null)
const search = ref('')
const showInactive = ref(false)

const email = ref('')
const userName = ref('')
const firstName = ref('')
const lastName = ref('')
const teamId = ref<number | ''>('')
const siteRoleId = ref<number | ''>('')
const busy = ref(false)
const setPasswordLink = ref('')
const linkFor = ref('')

const teams = computed(() => data.value?.teams ?? [])
const namesRequired = computed(() => !!data.value?.names_required)
const defaultRoleLabel = computed(() => data.value?.site_roles.find((r) => r.name === 'user')?.label || 'User')

// Inline name editing (company mode).
const editingNameId = ref<number | null>(null)
const firstDraft = ref('')
const lastDraft = ref('')
const savingName = ref(false)
// Team managers must put new people on one of their teams; people managers may leave it empty.
const teamRequired = computed(() => !data.value?.manages_everyone)

const people = computed(() => {
  const q = search.value.trim().toLowerCase()
  return (data.value?.people ?? []).filter((p) => {
    if (!showInactive.value && !p.active) return false
    if (!q) return true
    return (
      p.user_name.toLowerCase().includes(q) ||
      p.full_name.toLowerCase().includes(q) ||
      p.email.toLowerCase().includes(q) ||
      p.teams.some((t) => t.name.toLowerCase().includes(q))
    )
  })
})
const inactiveCount = computed(() => (data.value?.people ?? []).filter((p) => !p.active).length)

async function load() {
  try {
    data.value = await api.listPeople()
    if (teamRequired.value && teamId.value === '' && teams.value.length === 1) {
      teamId.value = teams.value[0].id
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load people', 'error')
  }
}

async function createPerson() {
  busy.value = true
  setPasswordLink.value = ''
  try {
    const who = email.value.trim()
    const result = await api.createPerson({
      email: who,
      user_name: userName.value.trim(),
      ...(namesRequired.value ? { first_name: firstName.value.trim(), last_name: lastName.value.trim() } : {}),
      team_id: teamId.value === '' ? undefined : teamId.value,
      site_role_id: siteRoleId.value === '' ? undefined : siteRoleId.value,
    })
    if (result.set_password_url) {
      setPasswordLink.value = result.set_password_url
      linkFor.value = who
    }
    if (result.warning) {
      toast.push(result.warning, 'error')
    } else {
      toast.push(result.email_sent ? `Account created. ${who} was emailed a link to set a password.` : 'Account created', 'success')
    }
    email.value = ''
    userName.value = ''
    firstName.value = ''
    lastName.value = ''
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create the account', 'error')
  } finally {
    busy.value = false
  }
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(setPasswordLink.value)
    toast.push('Link copied', 'success')
  } catch {
    toast.push('Copy failed; select the link and copy it', 'error')
  }
}

function startEditName(person: Person) {
  editingNameId.value = person.id
  firstDraft.value = person.first_name
  lastDraft.value = person.last_name
}

async function saveName(person: Person) {
  savingName.value = true
  try {
    await api.setPersonName(person.id, { first_name: firstDraft.value.trim(), last_name: lastDraft.value.trim() })
    toast.push('Name saved', 'success')
    editingNameId.value = null
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save the name', 'error')
  } finally {
    savingName.value = false
  }
}

async function setActive(person: Person, active: boolean) {
  const who = personLabel(person)
  const ok = await askConfirm({
    title: active ? `Reactivate ${who}?` : `Deactivate ${who}?`,
    message: active
      ? `${who} will be able to sign in again with their existing password.`
      : `${who} will be signed out everywhere and can't sign in or use API keys. Their tasks, comments, and team memberships stay as they are, and you can reactivate them later.`,
    confirmLabel: active ? 'Reactivate' : 'Deactivate',
    danger: !active,
  })
  if (!ok) return
  try {
    await api.setPersonActive(person.id, active)
    toast.push(active ? `${who} reactivated` : `${who} deactivated`, active ? 'success' : 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3 mb-5">
    <h1>People</h1>
    <p class="text-muted">
      <template v-if="data && !data.can_create">Keep everyone's first and last names up to date.</template>
      <template v-else-if="data?.manages_everyone">Create accounts for new people and deactivate accounts when someone leaves.</template>
      <template v-else>Create accounts for new members of your teams and deactivate members who leave.</template>
      <template v-if="data?.can_create">Deactivated people can't sign in, but their work stays.</template>
    </p>

    <div v-if="data?.can_create" class="card mb-4">
      <div class="card-header"><h2 class="h6 mb-0">Add a person</h2></div>
      <div class="card-body">
        <p v-if="teamRequired && !teams.length" class="text-muted mb-0">
          You aren't on any team yet. Ask an admin to add you to the team you manage.
        </p>
        <form v-else @submit.prevent="createPerson">
          <div class="row g-3">
            <template v-if="namesRequired">
              <div class="col-md-6">
                <label for="person-first" class="form-label">First name</label>
                <input id="person-first" v-model="firstName" type="text" class="form-control" required maxlength="100" autocomplete="off" />
              </div>
              <div class="col-md-6">
                <label for="person-last" class="form-label">Last name</label>
                <input id="person-last" v-model="lastName" type="text" class="form-control" required maxlength="100" autocomplete="off" />
              </div>
            </template>
            <div class="col-md-6">
              <label for="person-email" class="form-label">Email</label>
              <input id="person-email" v-model="email" type="email" class="form-control" required autocomplete="off" />
            </div>
            <div class="col-md-6">
              <label for="person-username" class="form-label">Username</label>
              <input
                id="person-username"
                v-model="userName"
                type="text"
                class="form-control"
                required
                minlength="3"
                maxlength="32"
                pattern="[A-Za-z0-9_]+"
                autocomplete="off"
              />
            </div>
            <div v-if="teams.length" class="col-md-6">
              <label for="person-team" class="form-label">Team</label>
              <select id="person-team" v-model="teamId" class="form-select" :required="teamRequired">
                <option v-if="!teamRequired" value="">No team</option>
                <option v-for="t in teams" :key="t.id" :value="t.id">{{ t.name }}</option>
              </select>
            </div>
            <div v-if="data?.site_roles.length" class="col-md-6">
              <label for="person-site-role" class="form-label">Site role</label>
              <select id="person-site-role" v-model="siteRoleId" class="form-select">
                <option value="">Default ({{ defaultRoleLabel }})</option>
                <option v-for="r in data.site_roles" :key="r.id" :value="r.id">{{ r.label }}</option>
              </select>
            </div>
          </div>
          <p class="form-text mt-3 mb-2">
            They'll get a link to choose their own password. The link works for 7 days. If email isn't set up, the link
            appears here so you can send it to them.
          </p>
          <button class="btn btn-primary" type="submit" :disabled="busy">
            {{ busy ? 'Creating…' : 'Create account' }}
          </button>
        </form>

        <div v-if="setPasswordLink" class="alert alert-warning mt-3 mb-0">
          <p class="mb-2">
            Email isn't set up, so send this link to <strong>{{ linkFor }}</strong> yourself. Anyone with the link can set
            the password, so share it only with them.
          </p>
          <div class="input-group">
            <input class="form-control font-monospace" :value="setPasswordLink" readonly aria-label="Set-password link" />
            <button class="btn btn-outline-secondary" type="button" @click="copyLink">Copy</button>
          </div>
        </div>
      </div>
    </div>

    <div class="d-flex flex-wrap gap-3 align-items-end mb-3">
      <div style="max-width: 28rem" class="flex-grow-1">
        <label for="people-search" class="form-label">Search</label>
        <input
          id="people-search"
          v-model="search"
          type="search"
          class="form-control"
          placeholder="Name, username, email, or team"
          autocomplete="off"
        />
      </div>
      <div class="form-check mb-2">
        <input id="people-inactive" v-model="showInactive" class="form-check-input" type="checkbox" />
        <label class="form-check-label" for="people-inactive">Show deactivated ({{ inactiveCount }})</label>
      </div>
    </div>

    <div class="table-responsive">
      <table class="table align-middle">
        <thead>
          <tr>
            <th scope="col">Person</th>
            <th scope="col">Teams</th>
            <th scope="col">Site role</th>
            <th scope="col">Status</th>
            <th scope="col" class="text-end"><span class="visually-hidden">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in people" :key="p.id" :class="{ 'text-muted': !p.active }">
            <td>
              <form
                v-if="editingNameId === p.id"
                class="d-flex flex-wrap gap-1 align-items-center"
                @submit.prevent="saveName(p)"
              >
                <input
                  v-model="firstDraft"
                  class="form-control form-control-sm w-auto"
                  required
                  maxlength="100"
                  placeholder="First name"
                  :aria-label="`First name for ${personLabel(p)}`"
                />
                <input
                  v-model="lastDraft"
                  class="form-control form-control-sm w-auto"
                  required
                  maxlength="100"
                  placeholder="Last name"
                  :aria-label="`Last name for ${personLabel(p)}`"
                />
                <button class="btn btn-sm btn-primary" type="submit" :disabled="savingName">Save</button>
                <button class="btn btn-sm btn-outline-secondary" type="button" @click="editingNameId = null">Cancel</button>
              </form>
              <div v-else class="fw-semibold">
                {{ personLabel(p) }}
                <span v-if="p.is_owner" class="badge text-bg-primary ms-1">Owner</span>
                <button
                  v-if="p.can_edit_name"
                  class="btn btn-sm btn-link p-0 ms-1 align-baseline"
                  type="button"
                  :aria-label="`Edit name for ${personLabel(p)}`"
                  @click="startEditName(p)"
                >
                  <i class="bi bi-pencil" aria-hidden="true" />
                </button>
              </div>
              <div class="small text-muted">
                <template v-if="p.full_name && p.user_name">@{{ p.user_name }} · </template>{{ p.email }}
              </div>
            </td>
            <td>
              <span v-if="!p.teams.length" class="text-muted small">None</span>
              <span v-for="t in p.teams" :key="t.id" class="badge text-bg-light border me-1">
                {{ t.name }} · {{ t.role_name }}
              </span>
            </td>
            <td class="small">{{ p.role_name }}</td>
            <td>
              <span v-if="p.active" class="badge text-bg-success">Active</span>
              <span v-else class="badge text-bg-secondary">Deactivated</span>
            </td>
            <td class="text-end">
              <template v-if="p.can_manage">
                <button v-if="p.active" class="btn btn-sm btn-outline-danger" type="button" @click="setActive(p, false)">
                  Deactivate
                </button>
                <button v-else class="btn btn-sm btn-outline-secondary" type="button" @click="setActive(p, true)">
                  Reactivate
                </button>
              </template>
            </td>
          </tr>
          <tr v-if="data && !people.length">
            <td colspan="5" class="text-muted">No one matches.</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
