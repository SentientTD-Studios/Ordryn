<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { AdminUser, SiteRole } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { personLabel } from '@/utils/personLabel'

const toast = useToast()
const { askConfirm } = useConfirm()
const { user: me, refresh: refreshMe } = useAuth()
// A company deactivates accounts when people leave; a public site bans them.
const { isCompanyMode } = useSite()
const banVerb = computed(() => (isCompanyMode.value ? 'Deactivate' : 'Ban'))
const unbanVerb = computed(() => (isCompanyMode.value ? 'Reactivate' : 'Unban'))
const users = ref<AdminUser[]>([])
const siteRoles = ref<SiteRole[]>([])
const search = ref('')
const editingUsernameId = ref<number | null>(null)
const editUsernameValue = ref('')

const filteredUsers = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter((u) => {
    const name = `${u.user_name || ''} ${u.full_name || ''}`.toLowerCase()
    const email = (u.email || '').toLowerCase()
    return name.includes(q) || email.includes(q)
  })
})

async function load() {
  try {
    const [list, roles] = await Promise.all([api.listAdminUsers(), api.listSiteRoles()])
    users.value = list
    siteRoles.value = roles.roles || []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load users', 'error')
  }
}

function grantsAdmin(roleId: number) {
  return !!siteRoles.value.find((r) => r.id === roleId)?.permissions.includes('admin')
}

// The owner outranks admins: only the owner changes admins, and nobody changes the owner.
const amOwner = computed(() => !!me.value?.is_site_owner)

function lockedReason(user: AdminUser): string {
  if (user.id === me.value?.id) return "You can't change your own role or account here"
  if (user.is_owner) return "The site owner's account can't be changed"
  if (grantsAdmin(user.role_id) && !amOwner.value) return 'Only the site owner can change admins'
  return ''
}

async function transferOwnership(user: AdminUser) {
  const who = personLabel(user)
  const ok = await askConfirm({
    title: `Make ${who} the site owner?`,
    message: `${who} becomes the one account above every admin, and only they can hand ownership back. You stay an admin, but you'll no longer be able to change other admins.`,
    confirmLabel: 'Transfer ownership',
    danger: true,
  })
  if (!ok) return
  try {
    await api.transferSiteOwnership(user.id)
    toast.push(`${who} is now the site owner`, 'success')
    await refreshMe()
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not transfer ownership', 'error')
  }
}

async function changeRole(user: AdminUser, event: Event) {
  const select = event.target as HTMLSelectElement
  const roleId = Number(select.value)
  const next = siteRoles.value.find((r) => r.id === roleId)
  if (!next || roleId === user.role_id) return
  const who = personLabel(user)
  // Gaining or losing admin is the one change worth a second look.
  if (grantsAdmin(roleId) !== grantsAdmin(user.role_id)) {
    const ok = await askConfirm({
      title: grantsAdmin(roleId) ? 'Make admin?' : 'Remove admin?',
      message: grantsAdmin(roleId)
        ? `${who} will be able to change every site setting, role, and account.`
        : `${who} will lose access to the admin area.`,
      confirmLabel: grantsAdmin(roleId) ? 'Make admin' : 'Remove admin',
      danger: true,
    })
    if (!ok) {
      select.value = String(user.role_id)
      return
    }
  }
  try {
    await api.setAdminUserRole(user.id, roleId)
    toast.push(`${who} is now ${next.label}`, 'success')
    await load()
  } catch (err) {
    select.value = String(user.role_id)
    toast.push(err instanceof APIError ? err.message : 'Could not change role', 'error')
  }
}

async function toggleBan(user: AdminUser) {
  const verb = user.is_banned ? unbanVerb.value : banVerb.value
  const ok = await askConfirm({
    title: `${verb} user?`,
    message: user.is_banned
      ? `${verb} ${user.email}?`
      : `${verb} ${user.email}? They will be signed out and can no longer sign in.`,
    confirmLabel: verb,
    danger: !user.is_banned,
  })
  if (!ok) return
  try {
    if (user.is_banned) {
      await api.unbanUser(user.id)
      toast.push(isCompanyMode.value ? 'User reactivated' : 'User unbanned', 'success')
    } else {
      await api.banUser(user.id)
      toast.push(isCompanyMode.value ? 'User deactivated' : 'User banned', 'info')
    }
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

function startEditUsername(user: AdminUser) {
  editingUsernameId.value = user.id
  editUsernameValue.value = user.user_name || ''
}

function cancelEditUsername() {
  editingUsernameId.value = null
  editUsernameValue.value = ''
}

async function saveUsername(user: AdminUser) {
  const next = editUsernameValue.value.trim()
  if (!next) {
    toast.push('Username is required', 'error')
    return
  }
  try {
    await api.setAdminUsername(user.id, next)
    toast.push('Username updated', 'success')
    cancelEditUsername()
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to update username', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Users</h1>
    <p class="text-muted">
      Search, rename, {{ isCompanyMode ? 'deactivate' : 'ban' }}, or change the
      <RouterLink to="/admin/site-roles">site role</RouterLink> of accounts on this site. The site owner is the one
      account above admins: only the owner can make, change, or {{ isCompanyMode ? 'deactivate' : 'ban' }} admins.
    </p>

    <div class="mb-3" style="max-width: 28rem">
      <label for="admin-user-search" class="form-label">Search</label>
      <div class="position-relative">
        <i class="bi bi-search position-absolute top-50 start-0 translate-middle-y ms-3 text-muted" />
        <input
          id="admin-user-search"
          v-model="search"
          type="search"
          class="form-control ps-5"
          placeholder="Username or email"
          autocomplete="off"
        />
      </div>
    </div>

    <div class="card">
      <ul class="list-group list-group-flush">
        <li v-for="user in filteredUsers" :key="user.id" class="list-group-item">
          <div class="d-flex justify-content-between align-items-start gap-3 flex-wrap">
            <div class="flex-grow-1">
              <template v-if="editingUsernameId === user.id">
                <div class="input-group input-group-sm" style="max-width: 280px">
                  <input
                    v-model="editUsernameValue"
                    type="text"
                    class="form-control"
                    minlength="3"
                    maxlength="32"
                    pattern="[A-Za-z0-9_]+"
                    @keyup.enter="saveUsername(user)"
                  />
                  <button type="button" class="btn btn-primary" @click="saveUsername(user)">Save</button>
                  <button type="button" class="btn btn-outline-secondary" @click="cancelEditUsername">Cancel</button>
                </div>
              </template>
              <template v-else>
                <strong>{{ personLabel(user) }}</strong>
                <span v-if="user.full_name && user.user_name" class="small text-muted ms-1">@{{ user.user_name }}</span>
                <span v-if="user.is_owner" class="badge text-bg-primary ms-2">Owner</span>
                <button
                  v-if="user.id === me?.id || !lockedReason(user)"
                  type="button"
                  class="btn btn-link btn-sm py-0"
                  @click="startEditUsername(user)"
                >
                  Edit username
                </button>
              </template>
              <div class="text-muted small">
                {{ user.email }}
                <span v-if="user.is_banned">· {{ isCompanyMode ? 'deactivated' : 'banned' }}</span>
              </div>
            </div>
            <div class="d-flex align-items-center gap-2">
              <label class="visually-hidden" :for="`admin-user-role-${user.id}`">Site role</label>
              <select
                :id="`admin-user-role-${user.id}`"
                class="form-select form-select-sm"
                style="width: auto"
                :value="user.role_id"
                :disabled="!!lockedReason(user)"
                :title="lockedReason(user) || 'Site role'"
                @change="changeRole(user, $event)"
              >
                <option
                  v-for="role in siteRoles"
                  :key="role.id"
                  :value="role.id"
                  :disabled="role.id !== user.role_id && grantsAdmin(role.id) && !amOwner"
                >
                  {{ role.label }}
                </option>
              </select>
              <button
                v-if="amOwner && !user.is_owner && !user.is_banned && grantsAdmin(user.role_id)"
                type="button"
                class="btn btn-sm btn-outline-primary"
                @click="transferOwnership(user)"
              >
                Make owner
              </button>
              <button
                v-if="!lockedReason(user)"
                type="button"
                class="btn btn-sm"
                :class="user.is_banned ? 'btn-outline-secondary' : 'btn-outline-danger'"
                @click="toggleBan(user)"
              >
                {{ user.is_banned ? unbanVerb : banVerb }}
              </button>
            </div>
          </div>
        </li>
        <li v-if="!filteredUsers.length" class="list-group-item text-muted">
          {{ users.length ? 'No users match that search.' : 'No users found.' }}
        </li>
      </ul>
    </div>
  </div>
</template>
