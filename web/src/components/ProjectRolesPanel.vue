<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectPermInfo, ProjectRoleDef } from '@/api/types'
import { APIError } from '@/api/types'
import RolePermissionFields from '@/components/RolePermissionFields.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { canManageProject, slugifyRoleName } from '@/utils/projectPerms'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const { askConfirm } = useConfirm()
const catalog = ref<ProjectPermInfo[]>([])
const roles = ref<ProjectRoleDef[]>([])
const loading = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formSlug = ref('')
const formDescription = ref('')
const formPermissions = ref<string[]>([])
const slugTouched = ref(false)

const canManage = computed(() => canManageProject(props.project))
const siteRoles = computed(() => roles.value.filter((r) => !r.project_id))
const customRoles = computed(() => roles.value.filter((r) => r.project_id === props.project.id))
const editing = computed(() => customRoles.value.find((r) => r.id === editingId.value) || null)

async function load() {
  loading.value = true
  try {
    const data = await api.listProjectRoles(props.project.id)
    catalog.value = data.catalog || []
    roles.value = data.roles || []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load roles', 'error')
  } finally {
    loading.value = false
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

function startCreate() {
  resetForm()
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
  if (!canManage.value) return
  const name = formName.value.trim()
  if (!name) return
  saving.value = true
  try {
    if (editingId.value) {
      await api.updateProjectRole(props.project.id, editingId.value, {
        name,
        description: formDescription.value.trim(),
        permissions: formPermissions.value,
      })
      toast.push('Role updated', 'success')
    } else {
      await api.createProjectRole(props.project.id, {
        slug: formSlug.value.trim() || slugifyRoleName(name),
        name,
        description: formDescription.value.trim(),
        permissions: formPermissions.value,
      })
      toast.push('Role created', 'success')
    }
    resetForm()
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save role', 'error')
  } finally {
    saving.value = false
  }
}

async function deleteRole(role: ProjectRoleDef) {
  if (!canManage.value) return
  const ok = await askConfirm({
    title: 'Delete role?',
    message: `Delete “${role.name}”? Members still using it must be reassigned first.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteProjectRole(props.project.id, role.id)
    toast.push('Role deleted', 'info')
    if (editingId.value === role.id) resetForm()
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete role', 'error')
  }
}

watch(
  () => props.project.id,
  () => {
    resetForm()
    void load()
  },
)

onMounted(load)
</script>

<template>
  <div class="project-roles-panel">
    <h4 class="h6">Site roles</h4>
    <p class="small text-muted mb-2">
      Built-in and site-wide roles are defined by admins and can be assigned on this project.
      Create extra roles here when this board needs a permission mix the site templates do not cover.
    </p>
    <ul class="list-unstyled mb-3">
      <li v-for="role in siteRoles" :key="role.id" class="mb-2">
        <strong>{{ role.name }}</strong>
        <span v-if="role.is_system" class="badge text-bg-secondary ms-1">built-in</span>
        <div class="small text-muted">{{ role.description || role.slug }}</div>
        <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
      </li>
      <li v-if="!siteRoles.length && !loading" class="small text-muted">No site roles loaded.</li>
    </ul>

    <h4 class="h6">Project roles</h4>
    <ul class="list-unstyled mb-3">
      <li
        v-for="role in customRoles"
        :key="role.id"
        class="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-2 pb-2 border-bottom"
      >
        <div>
          <strong>{{ role.name }}</strong>
          <span class="badge text-bg-info ms-1">this project</span>
          <div class="small text-muted">{{ role.slug }} · {{ role.description || 'No description' }}</div>
          <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
        </div>
        <div v-if="canManage" class="d-flex gap-1">
          <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEdit(role)">Edit</button>
          <button class="btn btn-sm btn-outline-danger" type="button" @click="deleteRole(role)">Delete</button>
        </div>
      </li>
      <li v-if="!customRoles.length" class="small text-muted">No project-specific roles yet.</li>
    </ul>

    <form v-if="canManage" class="border rounded p-3" @submit.prevent="saveRole">
      <h4 class="h6">{{ editing ? `Edit ${editing.name}` : 'Create a project role' }}</h4>
      <p class="small text-muted">
        Pick from the core permission catalog. Empty permission lists are view-only, like Viewer.
      </p>
      <div class="mb-2">
        <label class="form-label small mb-0" for="project-role-name">Name</label>
        <input
          id="project-role-name"
          v-model="formName"
          type="text"
          class="form-control form-control-sm"
          maxlength="80"
          required
          placeholder="e.g. Developer II"
          @input="onNameInput"
        />
      </div>
      <div v-if="!editing" class="mb-2">
        <label class="form-label small mb-0" for="project-role-slug">Slug</label>
        <input
          id="project-role-slug"
          v-model="formSlug"
          type="text"
          class="form-control form-control-sm"
          maxlength="40"
          required
          placeholder="developer-ii"
          @input="slugTouched = true"
        />
        <small class="form-hint">Lowercase letters, digits, hyphen, or underscore. Cannot be changed later.</small>
      </div>
      <div class="mb-2">
        <label class="form-label small mb-0" for="project-role-desc">Description</label>
        <input
          id="project-role-desc"
          v-model="formDescription"
          type="text"
          class="form-control form-control-sm"
          maxlength="200"
        />
      </div>
      <RolePermissionFields v-model="formPermissions" :catalog="catalog" />
      <div class="d-flex gap-2">
        <button class="btn btn-sm btn-primary" type="submit" :disabled="saving || !formName.trim()">
          {{ editing ? 'Save role' : 'Create role' }}
        </button>
        <button v-if="editing" class="btn btn-sm btn-outline-secondary" type="button" @click="resetForm">
          Cancel
        </button>
      </div>
    </form>
  </div>
</template>
