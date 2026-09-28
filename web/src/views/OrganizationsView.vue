<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Sortable from 'sortablejs'
import { api } from '@/api/client'
import type { Organization, OrganizationInvite, OrganizationMember, ProjectPermInfo, ProjectRoleDef } from '@/api/types'
import { APIError } from '@/api/types'
import RolePermissionFields from '@/components/RolePermissionFields.vue'
import UserSearchCombobox from '@/components/UserSearchCombobox.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { slugifyRoleName } from '@/utils/projectPerms'

const toast = useToast()
const { askConfirm } = useConfirm()

const orgs = ref<Organization[]>([])
const selectedId = ref<number | null>(null)
const org = computed(() => orgs.value.find((o) => o.id === selectedId.value) || null)
const members = ref<OrganizationMember[]>([])
const invites = ref<OrganizationInvite[]>([])
const myInvites = ref<OrganizationInvite[]>([])
const catalog = ref<ProjectPermInfo[]>([])
const roles = ref<ProjectRoleDef[]>([])
const saving = ref(false)
const newName = ref('')
const newDescription = ref('')
const editName = ref('')
const editDescription = ref('')
const inviteUsername = ref('')
const inviteRole = ref('editor')
const editingId = ref<number | null>(null)
const formName = ref('')
const formSlug = ref('')
const formDescription = ref('')
const formPermissions = ref<string[]>([])
const formCopyFromId = ref<number | null>(null)
const slugTouched = ref(false)
const roleListEl = ref<HTMLElement | null>(null)
let sortable: Sortable | null = null

const canManage = computed(() => !!org.value?.can_manage)
const siteRoles = computed(() => roles.value.filter((r) => !r.organization_id && !r.project_id))
const customRoles = computed(() => roles.value.filter((r) => r.organization_id === selectedId.value))
const editing = computed(() => customRoles.value.find((r) => r.id === editingId.value) || null)
const assignableRoles = computed(() => roles.value)
const excludeUsernames = computed(() => {
  const names = members.value.map((m) => m.user_name).filter(Boolean)
  for (const inv of invites.value) {
    if (inv.user_name) names.push(inv.user_name)
  }
  return names
})

async function loadOrgs() {
  try {
    const [organizationList, pending] = await Promise.all([
      api.listOrganizations(),
      api.listMyOrganizationInvites().catch(() => [] as OrganizationInvite[]),
    ])
    orgs.value = organizationList
    myInvites.value = pending
    if (selectedId.value && !orgs.value.some((o) => o.id === selectedId.value)) {
      selectedId.value = orgs.value[0]?.id ?? null
    } else if (!selectedId.value && orgs.value.length) {
      selectedId.value = orgs.value[0].id
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load organizations', 'error')
  }
}

async function loadDetail() {
  if (!selectedId.value) {
    members.value = []
    invites.value = []
    roles.value = []
    return
  }
  try {
    const [m, inv, roleData, fresh] = await Promise.all([
      api.listOrganizationMembers(selectedId.value),
      api.listOrganizationInvites(selectedId.value).catch(() => [] as OrganizationInvite[]),
      api.listOrganizationRoles(selectedId.value),
      api.getOrganization(selectedId.value),
    ])
    members.value = m
    invites.value = inv
    catalog.value = roleData.catalog || []
    roles.value = roleData.roles || []
    const idx = orgs.value.findIndex((o) => o.id === fresh.id)
    if (idx >= 0) orgs.value[idx] = fresh
    if (!inviteRole.value && assignableRoles.value.length) {
      inviteRole.value = assignableRoles.value.find((r) => r.slug === 'editor')?.slug || assignableRoles.value[0].slug
    }
    editName.value = fresh.name
    editDescription.value = fresh.description || ''
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load organization', 'error')
  }
}

async function createOrg() {
  if (!newName.value.trim()) return
  saving.value = true
  try {
    const created = await api.createOrganization(newName.value.trim(), newDescription.value.trim())
    newName.value = ''
    newDescription.value = ''
    toast.push('Organization created', 'success')
    await loadOrgs()
    selectedId.value = created.id
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create organization', 'error')
  } finally {
    saving.value = false
  }
}

async function saveOrg() {
  if (!selectedId.value || !canManage.value) return
  saving.value = true
  try {
    await api.updateOrganization(selectedId.value, {
      name: editName.value.trim(),
      description: editDescription.value.trim(),
    })
    toast.push('Organization updated', 'success')
    await loadOrgs()
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save organization', 'error')
  } finally {
    saving.value = false
  }
}

async function deleteOrg() {
  if (!org.value) return
  const ok = await askConfirm({
    title: 'Delete organization?',
    message: `Delete “${org.value.name}”? Linked projects become independent and keep their own members.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteOrganization(org.value.id)
    toast.push('Organization deleted', 'info')
    selectedId.value = null
    await loadOrgs()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete', 'error')
  }
}

async function inviteMember() {
  if (!selectedId.value || !inviteUsername.value.trim()) return
  try {
    await api.createOrganizationInvite(selectedId.value, inviteUsername.value.trim(), inviteRole.value)
    inviteUsername.value = ''
    toast.push('Invite sent', 'success')
    await loadDetail()
    await loadOrgs()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not send invite', 'error')
  }
}

async function revokeInvite(inviteId: number) {
  if (!selectedId.value) return
  try {
    await api.revokeOrganizationInvite(selectedId.value, inviteId)
    toast.push('Invite revoked', 'info')
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not revoke invite', 'error')
  }
}

async function acceptMyInvite(inv: OrganizationInvite) {
  try {
    await api.acceptOrganizationInvite(inv.id)
    toast.push('Joined organization', 'success')
    await loadOrgs()
    selectedId.value = inv.organization_id
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Accept failed', 'error')
  }
}

async function declineMyInvite(inv: OrganizationInvite) {
  try {
    await api.declineOrganizationInvite(inv.id)
    toast.push('Invite declined', 'info')
    await loadOrgs()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Decline failed', 'error')
  }
}

async function onMemberRoleChange(userId: number, role: string) {
  if (!selectedId.value || role === 'owner') return
  try {
    await api.updateOrganizationMember(selectedId.value, userId, role)
    toast.push('Role updated', 'success')
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await loadDetail()
  }
}

async function removeMember(userId: number) {
  if (!selectedId.value) return
  const ok = await askConfirm({
    title: 'Remove member?',
    message: 'Remove this person from the organization? They keep access to projects they were already imported onto.',
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeOrganizationMember(selectedId.value, userId)
    toast.push('Member removed', 'info')
    await loadDetail()
    await loadOrgs()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Remove failed', 'error')
  }
}

function resetRoleForm() {
  editingId.value = null
  formName.value = ''
  formSlug.value = ''
  formDescription.value = ''
  formPermissions.value = []
  formCopyFromId.value = null
  slugTouched.value = false
}

function startEditRole(role: ProjectRoleDef) {
  editingId.value = role.id
  formName.value = role.name
  formSlug.value = role.slug
  formDescription.value = role.description || ''
  formPermissions.value = [...(role.permissions || [])]
  formCopyFromId.value = null
  slugTouched.value = true
}

function startCopyRole(role: ProjectRoleDef) {
  editingId.value = null
  formName.value = `${role.name} (copy)`
  formSlug.value = slugifyRoleName(`${role.slug}-copy`)
  formDescription.value = role.description || ''
  formPermissions.value = [...(role.permissions || [])]
  formCopyFromId.value = role.id
  slugTouched.value = true
}

function onNameInput() {
  if (editingId.value || slugTouched.value) return
  formSlug.value = slugifyRoleName(formName.value)
}

async function saveRole() {
  if (!selectedId.value || !canManage.value) return
  const name = formName.value.trim()
  if (!name) return
  saving.value = true
  try {
    if (editingId.value) {
      await api.updateOrganizationRole(selectedId.value, editingId.value, {
        name,
        description: formDescription.value.trim(),
        permissions: formPermissions.value,
      })
      toast.push('Role updated', 'success')
    } else {
      await api.createOrganizationRole(selectedId.value, {
        slug: formSlug.value.trim() || slugifyRoleName(name),
        name,
        description: formDescription.value.trim(),
        permissions: formPermissions.value,
        copy_from_id: formCopyFromId.value || undefined,
      })
      toast.push('Role created', 'success')
    }
    resetRoleForm()
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save role', 'error')
  } finally {
    saving.value = false
  }
}

async function deleteRole(role: ProjectRoleDef) {
  if (!selectedId.value) return
  const ok = await askConfirm({
    title: 'Delete role?',
    message: `Delete “${role.name}”? Members still using it must be reassigned first.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteOrganizationRole(selectedId.value, role.id)
    toast.push('Role deleted', 'info')
    if (editingId.value === role.id) resetRoleForm()
    await loadDetail()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete role', 'error')
  }
}

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

function collectRoleIds(el: HTMLElement): number[] {
  return Array.from(el.querySelectorAll(':scope > .role-reorder-item'))
    .map((node) => parseInt((node as HTMLElement).dataset.roleId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

function initSortable() {
  destroySortable()
  if (!roleListEl.value || !canManage.value || customRoles.value.length < 2) return
  sortable = Sortable.create(roleListEl.value, {
    handle: '.role-drag-handle',
    draggable: '.role-reorder-item',
    animation: 150,
    async onEnd(evt) {
      const el = evt.to as HTMLElement
      const ids = collectRoleIds(el)
      if (!selectedId.value || ids.length !== customRoles.value.length) return
      try {
        await api.reorderOrganizationRoles(selectedId.value, ids)
      } catch (err) {
        toast.push(err instanceof APIError ? err.message : 'Could not reorder roles', 'error')
        await loadDetail()
      }
    },
  })
}

watch(selectedId, async () => {
  resetRoleForm()
  await loadDetail()
})

watch(customRoles, async () => {
  await nextTick()
  initSortable()
})

onMounted(async () => {
  await loadOrgs()
  await loadDetail()
})

onBeforeUnmount(destroySortable)
</script>

<template>
  <div class="container mt-4">
    <h1>Organizations</h1>
    <p class="text-muted">
      Groups of people and roles you can reuse when creating or attaching a project.
      Importing copies the current roster onto that board; you can then change membership and roles
      per project. New organization members are not added to existing imported projects.
    </p>

    <div v-if="myInvites.length" class="card mb-4 border-primary">
      <div class="card-header">
        <h2 class="mb-0 h5">Pending organization invites</h2>
      </div>
      <div class="card-body">
        <div
          v-for="inv in myInvites"
          :key="inv.id"
          class="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2"
        >
          <div>
            <strong>{{ inv.organization_name || 'Organization' }}</strong>
            <span class="text-muted"> as {{ inv.role }}</span>
            <div v-if="inv.inviter_user_name || inv.inviter_email" class="small text-muted">
              From {{ inv.inviter_user_name || inv.inviter_email }}
            </div>
          </div>
          <div class="d-flex gap-2">
            <button class="btn btn-sm btn-primary" type="button" @click="acceptMyInvite(inv)">Accept</button>
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="declineMyInvite(inv)">
              Decline
            </button>
          </div>
        </div>
      </div>
    </div>

    <div class="row">
      <div class="col-md-4">
        <div class="card mb-3">
          <div class="card-header"><h2 class="h6 mb-0">Your organizations</h2></div>
          <ul class="list-group list-group-flush">
            <li
              v-for="item in orgs"
              :key="item.id"
              class="list-group-item list-group-item-action"
              :class="{ active: item.id === selectedId }"
              role="button"
              @click="selectedId = item.id"
            >
              <div class="fw-semibold">{{ item.name }}</div>
              <div class="small" :class="item.id === selectedId ? 'text-white-50' : 'text-muted'">
                {{ item.member_count }} members · {{ item.project_count }} projects · {{ item.role_name || item.role }}
              </div>
            </li>
            <li v-if="!orgs.length" class="list-group-item text-muted">No organizations yet.</li>
          </ul>
        </div>
        <form class="card card-body" @submit.prevent="createOrg">
          <h2 class="h6">Create organization</h2>
          <input v-model="newName" class="form-control mb-2" maxlength="80" required placeholder="Name" />
          <textarea v-model="newDescription" class="form-control mb-2" rows="2" maxlength="1000" placeholder="Description (optional)" />
          <button class="btn btn-primary" type="submit" :disabled="saving || !newName.trim()">Create</button>
        </form>
      </div>

      <div class="col-md-8">
        <div v-if="org" class="card mb-3">
          <div class="card-body">
            <form v-if="canManage" class="mb-3" @submit.prevent="saveOrg">
              <div class="mb-2">
                <label class="form-label small mb-0" for="org-name">Name</label>
                <input id="org-name" v-model="editName" class="form-control" maxlength="80" required />
              </div>
              <div class="mb-2">
                <label class="form-label small mb-0" for="org-desc">Description</label>
                <textarea id="org-desc" v-model="editDescription" class="form-control" rows="2" maxlength="1000" />
              </div>
              <div class="d-flex gap-2">
                <button class="btn btn-sm btn-primary" type="submit" :disabled="saving">Save</button>
                <button v-if="org.role === 'owner'" class="btn btn-sm btn-outline-danger" type="button" @click="deleteOrg">
                  Delete organization
                </button>
              </div>
            </form>
            <div v-else>
              <h2 class="h5">{{ org.name }}</h2>
              <p class="text-muted">{{ org.description || 'No description' }}</p>
            </div>
          </div>
        </div>

        <div v-if="org" class="card mb-3">
          <div class="card-body">
            <h2 class="h6">Members</h2>
            <p class="small text-muted">These people are copied onto a project when you import this organization. Invites must be accepted before they are included in a later import.</p>
            <ul class="list-unstyled mb-3">
              <li v-for="m in members" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-2">
                <span>{{ m.user_name || m.email }}</span>
                <span class="badge text-bg-secondary">{{ m.role_name || m.role }}</span>
                <template v-if="canManage && m.role !== 'owner'">
                  <select
                    class="form-select form-select-sm w-auto"
                    :value="m.role"
                    @change="onMemberRoleChange(m.user_id, ($event.target as HTMLSelectElement).value)"
                  >
                    <option v-for="r in assignableRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
                  </select>
                  <button class="btn btn-sm btn-outline-danger" type="button" @click="removeMember(m.user_id)">Remove</button>
                </template>
              </li>
            </ul>
            <form v-if="canManage" class="row g-2 align-items-end mb-3" @submit.prevent="inviteMember">
              <div class="col-sm-6">
                <label class="form-label small mb-0">Username</label>
                <UserSearchCombobox v-model="inviteUsername" :exclude-usernames="excludeUsernames" />
              </div>
              <div class="col-sm-3">
                <label class="form-label small mb-0">Role</label>
                <select v-model="inviteRole" class="form-select form-select-sm">
                  <option v-for="r in assignableRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
                </select>
              </div>
              <div class="col-sm-3">
                <button class="btn btn-sm btn-primary w-100" type="submit">Invite</button>
              </div>
            </form>
            <div v-if="invites.length">
              <h3 class="h6">Pending invites</h3>
              <ul class="list-unstyled mb-0">
                <li v-for="inv in invites" :key="inv.id" class="d-flex justify-content-between align-items-center mb-1">
                  <span class="small">{{ inv.user_name || inv.email }} ({{ assignableRoles.find((r) => r.slug === inv.role)?.name || inv.role }})</span>
                  <button
                    v-if="canManage"
                    class="btn btn-sm btn-link text-danger"
                    type="button"
                    @click="revokeInvite(inv.id)"
                  >Revoke</button>
                </li>
              </ul>
            </div>
          </div>
        </div>

        <div v-if="org" class="card">
          <div class="card-body">
            <h2 class="h6">Roles</h2>
            <p class="small text-muted">
              Site templates apply here unless you add an organization role with the same slug.
              Drag organization roles to change their order. Copy an existing role to start from its permissions.
            </p>
            <h3 class="h6">Site roles</h3>
            <ul class="list-unstyled mb-3">
              <li v-for="role in siteRoles" :key="role.id" class="mb-2">
                <strong>{{ role.name }}</strong>
                <span v-if="role.is_system" class="badge text-bg-secondary ms-1">built-in</span>
                <button v-if="canManage" class="btn btn-sm btn-link py-0" type="button" @click="startCopyRole(role)">Copy</button>
                <div class="small text-muted">{{ role.description || role.slug }}</div>
              </li>
            </ul>
            <h3 class="h6">Organization roles</h3>
            <ul ref="roleListEl" class="list-unstyled mb-3">
              <li
                v-for="role in customRoles"
                :key="role.id"
                class="role-reorder-item d-flex flex-wrap align-items-start justify-content-between gap-2 mb-2 pb-2 border-bottom"
                :data-role-id="role.id"
              >
                <div class="d-flex gap-2">
                  <span v-if="canManage" class="role-drag-handle text-muted" title="Drag to reorder"><i class="bi bi-grip-vertical" /></span>
                  <div>
                    <strong>{{ role.name }}</strong>
                    <span class="badge text-bg-info ms-1">this org</span>
                    <div class="small text-muted">{{ role.slug }} · {{ role.description || 'No description' }}</div>
                    <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
                  </div>
                </div>
                <div v-if="canManage" class="d-flex gap-1">
                  <button class="btn btn-sm btn-outline-secondary" type="button" @click="startCopyRole(role)">Copy</button>
                  <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEditRole(role)">Edit</button>
                  <button class="btn btn-sm btn-outline-danger" type="button" @click="deleteRole(role)">Delete</button>
                </div>
              </li>
              <li v-if="!customRoles.length" class="small text-muted">No organization-specific roles yet.</li>
            </ul>
            <form v-if="canManage" class="border rounded p-3" @submit.prevent="saveRole">
              <h3 class="h6">{{ editing ? `Edit ${editing.name}` : formCopyFromId ? 'Create a copy' : 'Create an organization role' }}</h3>
              <div class="mb-2">
                <label class="form-label small mb-0">Name</label>
                <input v-model="formName" class="form-control form-control-sm" maxlength="80" required @input="onNameInput" />
              </div>
              <div v-if="!editing" class="mb-2">
                <label class="form-label small mb-0">Slug</label>
                <input v-model="formSlug" class="form-control form-control-sm" maxlength="40" required @input="slugTouched = true" />
              </div>
              <div class="mb-2">
                <label class="form-label small mb-0">Description</label>
                <input v-model="formDescription" class="form-control form-control-sm" maxlength="200" />
              </div>
              <RolePermissionFields v-model="formPermissions" :catalog="catalog" />
              <div class="d-flex gap-2">
                <button class="btn btn-sm btn-primary" type="submit" :disabled="saving || !formName.trim()">
                  {{ editing ? 'Save role' : 'Create role' }}
                </button>
                <button v-if="editing || formCopyFromId" class="btn btn-sm btn-outline-secondary" type="button" @click="resetRoleForm">
                  Cancel
                </button>
              </div>
            </form>
          </div>
        </div>
        <p v-else class="text-muted">Select or create an organization.</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.role-drag-handle {
  cursor: grab;
}
.role-drag-handle:active {
  cursor: grabbing;
}
</style>
