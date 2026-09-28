<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import Sortable from 'sortablejs'
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
const formCopyFromId = ref<number | null>(null)
const slugTouched = ref(false)
const roleListEl = ref<HTMLElement | null>(null)
let sortable: Sortable | null = null

const canManage = computed(() => canManageProject(props.project))
const orgManaged = computed(() => !!props.project.org_managed && !!props.project.organization_id)
const orgRoles = computed(() => roles.value.filter((r) => !!r.organization_id && !r.project_id))
const overriddenSlugs = computed(() => new Set(orgRoles.value.map((r) => r.slug)))
const siteRoles = computed(() =>
  roles.value.filter((r) => !r.project_id && !r.organization_id && !overriddenSlugs.value.has(r.slug)),
)
const customRoles = computed(() => roles.value.filter((r) => r.project_id === props.project.id))
const editing = computed(() => customRoles.value.find((r) => r.id === editingId.value) || null)
const canEditProjectRoles = computed(() => canManage.value && !orgManaged.value)

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
  formCopyFromId.value = null
  slugTouched.value = false
}

function startEdit(role: ProjectRoleDef) {
  editingId.value = role.id
  formName.value = role.name
  formSlug.value = role.slug
  formDescription.value = role.description || ''
  formPermissions.value = [...(role.permissions || [])]
  formCopyFromId.value = null
  slugTouched.value = true
}

function startCopy(role: ProjectRoleDef) {
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
  if (!canEditProjectRoles.value) return
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
        copy_from_id: formCopyFromId.value || undefined,
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
  if (!canEditProjectRoles.value) return
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
  if (!roleListEl.value || !canEditProjectRoles.value || customRoles.value.length < 2) return
  sortable = Sortable.create(roleListEl.value, {
    handle: '.role-drag-handle',
    draggable: '.role-reorder-item',
    animation: 150,
    async onEnd(evt) {
      const ids = collectRoleIds(evt.to as HTMLElement)
      if (ids.length !== customRoles.value.length) return
      try {
        await api.reorderProjectRoles(props.project.id, ids)
      } catch (err) {
        toast.push(err instanceof APIError ? err.message : 'Could not reorder roles', 'error')
        await load()
      }
    },
  })
}

watch(
  () => props.project.id,
  () => {
    resetForm()
    void load()
  },
)

watch(customRoles, async () => {
  await nextTick()
  initSortable()
})

onMounted(load)
onBeforeUnmount(destroySortable)
</script>

<template>
  <div class="project-roles-panel">
    <div v-if="orgManaged" class="alert alert-info py-2 small">
      Roles for this project are locked to
      <RouterLink :to="'/organizations'">{{ project.organization_name || 'the organization' }}</RouterLink>.
      Organization permission changes apply here automatically.
    </div>
    <div v-else-if="project.organization_id" class="alert alert-info py-2 small">
      Members were imported from
      <RouterLink :to="'/organizations'">{{ project.organization_name || 'the organization' }}</RouterLink>.
      Organization roles stay available to assign, and you can still create roles just for this project.
    </div>

    <h4 class="h6">Site roles</h4>
    <p class="small text-muted mb-2">
      Built-in and site-wide roles are defined by admins and can be assigned on this project.
    </p>
    <ul class="list-unstyled mb-3">
      <li v-for="role in siteRoles" :key="role.id" class="mb-2">
        <strong>{{ role.name }}</strong>
        <span v-if="role.is_system" class="badge text-bg-secondary ms-1">built-in</span>
        <button
          v-if="canEditProjectRoles"
          class="btn btn-sm btn-link py-0"
          type="button"
          @click="startCopy(role)"
        >
          Copy
        </button>
        <div class="small text-muted">{{ role.description || role.slug }}</div>
        <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
      </li>
      <li v-if="!siteRoles.length && !loading" class="small text-muted">No site roles loaded.</li>
    </ul>

    <template v-if="orgRoles.length">
      <h4 class="h6">Organization roles</h4>
      <ul class="list-unstyled mb-3">
        <li v-for="role in orgRoles" :key="role.id" class="mb-2">
          <strong>{{ role.name }}</strong>
          <span class="badge text-bg-info ms-1">organization</span>
          <span v-if="role.overrides_site" class="badge text-bg-warning ms-1">customized</span>
          <button
            v-if="canEditProjectRoles"
            class="btn btn-sm btn-link py-0"
            type="button"
            @click="startCopy(role)"
          >
            Copy
          </button>
          <div class="small text-muted">{{ role.description || role.slug }}</div>
          <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
        </li>
      </ul>
    </template>

    <template v-if="!orgManaged">
      <h4 class="h6">Project roles</h4>
      <ul ref="roleListEl" class="list-unstyled mb-3">
        <li
          v-for="role in customRoles"
          :key="role.id"
          class="role-reorder-item d-flex flex-wrap align-items-start justify-content-between gap-2 mb-2 pb-2 border-bottom"
          :data-role-id="role.id"
        >
          <div class="d-flex gap-2">
            <span v-if="canEditProjectRoles" class="role-drag-handle text-muted" title="Drag to reorder">
              <i class="bi bi-grip-vertical" />
            </span>
            <div>
              <strong>{{ role.name }}</strong>
              <span class="badge text-bg-info ms-1">this project</span>
              <div class="small text-muted">{{ role.slug }} · {{ role.description || 'No description' }}</div>
              <div class="small text-muted">{{ (role.permissions || []).join(', ') || 'no write permissions' }}</div>
            </div>
          </div>
          <div v-if="canEditProjectRoles" class="d-flex gap-1">
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="startCopy(role)">Copy</button>
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEdit(role)">Edit</button>
            <button class="btn btn-sm btn-outline-danger" type="button" @click="deleteRole(role)">Delete</button>
          </div>
        </li>
        <li v-if="!customRoles.length" class="small text-muted">No project-specific roles yet.</li>
      </ul>

      <form v-if="canEditProjectRoles" class="border rounded p-3" @submit.prevent="saveRole">
        <h4 class="h6">
          {{ editing ? `Edit ${editing.name}` : formCopyFromId ? 'Create a copy' : 'Create a project role' }}
        </h4>
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
          <button v-if="editing || formCopyFromId" class="btn btn-sm btn-outline-secondary" type="button" @click="resetForm">
            Cancel
          </button>
        </div>
      </form>
    </template>
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
