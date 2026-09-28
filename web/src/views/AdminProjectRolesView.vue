<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ProjectPermInfo, ProjectRoleDef } from '@/api/types'
import { APIError } from '@/api/types'
import AdminSubnav from '@/components/AdminSubnav.vue'
import RolePermissionFields from '@/components/RolePermissionFields.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { PROJECT_PERMS, slugifyRoleName } from '@/utils/projectPerms'

const toast = useToast()
const { askConfirm } = useConfirm()
const catalog = ref<ProjectPermInfo[]>([])
const roles = ref<ProjectRoleDef[]>([])
const saving = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formSlug = ref('')
const formDescription = ref('')
const formPermissions = ref<string[]>([])
const slugTouched = ref(false)

const editing = computed(() => roles.value.find((r) => r.id === editingId.value) || null)
const ownerLocked = computed(() => editing.value?.slug === 'owner')

async function load() {
  try {
    const data = await api.listAdminProjectRoles()
    catalog.value = data.catalog || []
    roles.value = data.roles || []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load roles', 'error')
  }
}

function resetForm() {
  editingId.value = null
  formName.value = ''
  formSlug.value = ''
  formDescription.value = ''
  formPermissions.value = []
  slugTouched.value = false
}

function startEdit(role: ProjectRoleDef) {
  editingId.value = role.id
  formName.value = role.name
  formSlug.value = role.slug
  formDescription.value = role.description || ''
  formPermissions.value = [...(role.permissions || [])]
  slugTouched.value = true
}

function onNameInput() {
  if (editingId.value || slugTouched.value) return
  formSlug.value = slugifyRoleName(formName.value)
}

async function saveRole() {
  const name = formName.value.trim()
  if (!name) return
  saving.value = true
  try {
    const permissions = ownerLocked.value
      ? Object.values(PROJECT_PERMS)
      : formPermissions.value
    if (editingId.value) {
      await api.updateAdminProjectRole(editingId.value, {
        name,
        description: formDescription.value.trim(),
        permissions,
      })
      toast.push('Role updated', 'success')
    } else {
      await api.createAdminProjectRole({
        slug: formSlug.value.trim() || slugifyRoleName(name),
        name,
        description: formDescription.value.trim(),
        permissions,
      })
      toast.push('Site role created', 'success')
    }
    resetForm()
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save role', 'error')
  } finally {
    saving.value = false
  }
}

async function deleteRole(role: ProjectRoleDef) {
  if (role.is_system) return
  const ok = await askConfirm({
    title: 'Delete site role?',
    message: `Delete “${role.name}”? It cannot be removed while members or invites still use it.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteAdminProjectRole(role.id)
    toast.push('Role deleted', 'info')
    if (editingId.value === role.id) resetForm()
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete role', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Project roles</h1>
    <p class="text-muted">
      Site-wide roles that project owners can assign. Built-in Owner, Editor, and Viewer stay available.
      Add roles such as Developer or QA, or change their permission catalog. Projects may also create
      extra roles for that board only.
    </p>

    <ul class="list-unstyled mb-4">
      <li
        v-for="role in roles"
        :key="role.id"
        class="card mb-2"
      >
        <div class="card-body py-3 d-flex flex-wrap justify-content-between gap-2">
          <div>
            <strong>{{ role.name }}</strong>
            <span class="badge text-bg-secondary ms-1">{{ role.slug }}</span>
            <span v-if="role.is_system" class="badge text-bg-info ms-1">built-in</span>
            <div class="small text-muted">{{ role.description || 'No description' }}</div>
            <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
          </div>
          <div class="d-flex gap-1 align-items-start">
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEdit(role)">Edit</button>
            <button
              v-if="!role.is_system"
              class="btn btn-sm btn-outline-danger"
              type="button"
              @click="deleteRole(role)"
            >
              Delete
            </button>
          </div>
        </div>
      </li>
      <li v-if="!roles.length" class="text-muted">No roles found.</li>
    </ul>

    <form class="card card-body" @submit.prevent="saveRole">
      <h2 class="h5">{{ editing ? `Edit ${editing.name}` : 'Create a site role' }}</h2>
      <div class="mb-2">
        <label class="form-label small mb-0" for="admin-role-name">Name</label>
        <input
          id="admin-role-name"
          v-model="formName"
          type="text"
          class="form-control"
          maxlength="80"
          required
          placeholder="e.g. QA"
          @input="onNameInput"
        />
      </div>
      <div v-if="!editing" class="mb-2">
        <label class="form-label small mb-0" for="admin-role-slug">Slug</label>
        <input
          id="admin-role-slug"
          v-model="formSlug"
          type="text"
          class="form-control"
          maxlength="40"
          required
          placeholder="qa"
          @input="slugTouched = true"
        />
      </div>
      <div class="mb-2">
        <label class="form-label small mb-0" for="admin-role-desc">Description</label>
        <input
          id="admin-role-desc"
          v-model="formDescription"
          type="text"
          class="form-control"
          maxlength="200"
        />
      </div>
      <p v-if="ownerLocked" class="small text-muted">Owner always has every permission.</p>
      <RolePermissionFields v-else v-model="formPermissions" :catalog="catalog" />
      <div class="d-flex gap-2">
        <button class="btn btn-primary" type="submit" :disabled="saving || !formName.trim()">
          {{ editing ? 'Save role' : 'Create role' }}
        </button>
        <button v-if="editing" class="btn btn-outline-secondary" type="button" @click="resetForm">Cancel</button>
      </div>
    </form>
  </div>
</template>
