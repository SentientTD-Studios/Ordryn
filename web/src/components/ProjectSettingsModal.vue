<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Organization, OrgImportMember, OrgImportMode, Project, ProjectMember, ProjectRoleDef } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import ProjectSharePanel from '@/components/ProjectSharePanel.vue'
import ProjectWorkflowPanel from '@/components/ProjectWorkflowPanel.vue'
import ProjectSprintsPanel from '@/components/ProjectSprintsPanel.vue'
import ProjectGitHubPanel from '@/components/ProjectGitHubPanel.vue'
import ProjectExtensionsPanel from '@/components/ProjectExtensionsPanel.vue'
import ProjectTagsPanel from '@/components/ProjectTagsPanel.vue'
import ProjectRolesPanel from '@/components/ProjectRolesPanel.vue'
import ProjectApiKeysPanel from '@/components/ProjectApiKeysPanel.vue'
import OrgImportFields from '@/components/OrgImportFields.vue'
import ProjectMemberRoster from '@/components/ProjectMemberRoster.vue'
import { isArchivedProject } from '@/utils/projectLabel'
import { canManageProject } from '@/utils/projectPerms'

type SettingsTab = 'details' | 'board' | 'sprints' | 'tags' | 'github' | 'extensions' | 'api-keys' | 'sharing' | 'roles'

const props = defineProps<{
  open: boolean
  project: Project | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
  changed: []
  'columns-changed': []
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const name = ref('')
const description = ref('')
const saving = ref(false)
const archiving = ref(false)
const attachingOrg = ref(false)
const attachOrgId = ref(0)
const attachMode = ref<OrgImportMode>('copy')
const attachMembers = ref<OrgImportMember[]>([])
const projectMembers = ref<ProjectMember[]>([])
const assignableRoles = ref<ProjectRoleDef[]>([])
const orgs = ref<Organization[]>([])
const tab = ref<SettingsTab>('details')
const isOwner = computed(() => (props.project?.role || 'owner') === 'owner')
const canManage = computed(() => canManageProject(props.project))
const isKanban = computed(() => (props.project?.workflow_mode || 'classic') === 'kanban')
const orgLinked = computed(() => !!props.project?.organization_id)
const orgManaged = computed(() => orgLinked.value && !!props.project?.org_managed)
const manageableOrgs = computed(() => orgs.value.filter((o) => o.can_manage))
const attachDisabled = computed(() => {
  if (attachingOrg.value || !attachOrgId.value) return true
  if (attachMode.value === 'select' && !attachMembers.value.length) return true
  return false
})

const tabs = computed(() => {
  const items: { id: SettingsTab; label: string }[] = [
    { id: 'details', label: 'Details' },
    { id: 'board', label: 'Board' },
  ]
  if (isKanban.value) items.push({ id: 'sprints', label: 'Sprints' })
  items.push({ id: 'tags', label: 'Tags' }, { id: 'github', label: 'GitHub' })
  items.push({ id: 'extensions', label: 'Extensions' })
  if (canManage.value) items.push({ id: 'api-keys', label: 'API keys' })
  items.push({ id: 'sharing', label: 'Sharing' })
  if (canManage.value) items.push({ id: 'roles', label: 'Roles' })
  return items
})

watch(
  () => props.open,
  (open) => {
    if (!open || !props.project) return
    name.value = props.project.name
    description.value = props.project.description || ''
    attachOrgId.value = props.project.organization_id || 0
    attachMode.value = 'copy'
    attachMembers.value = []
    tab.value = 'details'
    void loadOrganizations()
    void loadProjectMembers()
  },
)

watch(
  () => [props.project?.id, props.project?.organization_id, props.project?.org_managed, props.project?.name],
  (next, prev) => {
    if (!props.open || !props.project) return
    name.value = props.project.name
    description.value = props.project.description || ''
    attachOrgId.value = props.project.organization_id || 0
    const prevId = Array.isArray(prev) ? prev[0] : undefined
    if (prevId !== undefined && next[0] !== prevId) tab.value = 'details'
    void loadProjectMembers()
  },
)

watch(isKanban, (kanban) => {
  if (!kanban && tab.value === 'sprints') tab.value = 'details'
})

function close() {
  emit('close')
}

async function loadOrganizations() {
  try {
    orgs.value = await api.listOrganizations()
  } catch {
    orgs.value = []
  }
}

async function loadProjectMembers() {
  if (!props.project) {
    projectMembers.value = []
    assignableRoles.value = []
    return
  }
  try {
    const [members, roles] = await Promise.all([
      api.listProjectMembers(props.project.id),
      api.listProjectRoles(props.project.id).catch(() => ({ catalog: [], roles: [] })),
    ])
    projectMembers.value = members
    assignableRoles.value = (roles.roles || []).filter((r) => r.slug !== 'owner')
  } catch {
    projectMembers.value = []
  }
}

async function onProjectMemberRoleChange(userId: number, role: string) {
  if (!props.project || !role || role === 'owner' || orgManaged.value) return
  try {
    await api.updateProjectMember(props.project.id, userId, role)
    toast.push('Role updated', 'success')
    await loadProjectMembers()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await loadProjectMembers()
  }
}

async function onProjectMemberRemove(userId: number) {
  if (!props.project || orgManaged.value) return
  const ok = await askConfirm({
    title: 'Remove member?',
    message: 'Remove this member from the project?',
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeProjectMember(props.project.id, userId)
    toast.push('Member removed', 'info')
    await loadProjectMembers()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Remove failed', 'error')
  }
}

async function saveBasics() {
  if (!props.project || !name.value.trim() || !isOwner.value) return
  saving.value = true
  try {
    await api.updateProject(props.project.id, {
      name: name.value.trim(),
      description: description.value.trim(),
    })
    toast.push('Project updated', 'success')
    emit('saved')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to update project', 'error')
  } finally {
    saving.value = false
  }
}

async function attachOrganization() {
  if (!props.project || !isOwner.value || !attachOrgId.value || orgLinked.value) return
  if (attachMode.value === 'select' && !attachMembers.value.length) return
  const org = manageableOrgs.value.find((o) => o.id === attachOrgId.value)
  const orgName = org?.name || 'this organization'
  const modeNote =
    attachMode.value === 'lock'
      ? ' All members will be imported and roles will stay locked to the organization.'
      : attachMode.value === 'select'
        ? ' Only the members you selected will be imported.'
        : ' All members will be imported, and you can still change roles here afterward.'
  const ok = await askConfirm({
    title: 'Attach organization?',
    message: `Attach “${orgName}” to this project? People who are not imported will be removed from the project.${modeNote}`,
    confirmLabel: 'Attach',
    danger: true,
  })
  if (!ok) return
  attachingOrg.value = true
  try {
    await api.updateProject(props.project.id, {
      organization_id: attachOrgId.value,
      org_import: attachMode.value,
      ...(attachMode.value === 'select' ? { org_import_members: attachMembers.value } : {}),
    })
    toast.push('Organization attached', 'success')
    emit('saved')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to attach organization', 'error')
  } finally {
    attachingOrg.value = false
  }
}

function onPanelChanged() {
  emit('changed')
}

function onColumnsChanged() {
  emit('columns-changed')
}

async function archiveOrRestore() {
  if (!props.project || !isOwner.value) return
  const archived = isArchivedProject(props.project)
  if (!archived) {
    const ok = await askConfirm({
      title: 'Archive project?',
      message: `Archive “${props.project.name}”? It will move to the Archived section, its tasks will be tagged archived, and new tasks cannot be added until you restore it.`,
      confirmLabel: 'Archive',
    })
    if (!ok) return
  }
  archiving.value = true
  try {
    if (archived) {
      await api.restoreProject(props.project.id)
      toast.push('Project restored', 'success')
    } else {
      await api.archiveProject(props.project.id)
      toast.push('Project archived', 'info')
    }
    emit('saved')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update archive state', 'error')
  } finally {
    archiving.value = false
  }
}
</script>

<template>
  <div
    v-if="open && project"
    class="modal fade show d-block"
    style="background: rgba(0,0,0,0.5);"
    tabindex="-1"
    @click.self="close"
  >
    <div class="modal-dialog modal-dialog-centered modal-lg modal-dialog-scrollable">
      <div
        class="modal-content border-0 shadow"
        style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
      >
        <div class="modal-header border-0 pb-0">
          <h5 class="modal-title fw-bold">{{ isOwner ? 'Edit Project' : 'Project settings' }}</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="close" />
        </div>
        <div class="modal-body py-3">
          <nav class="mb-3" aria-label="Project settings sections">
            <ul class="nav nav-pills gap-1 flex-wrap">
              <li v-for="item in tabs" :key="item.id" class="nav-item">
                <button
                  type="button"
                  class="nav-link"
                  :class="{ active: tab === item.id }"
                  :aria-current="tab === item.id ? 'page' : undefined"
                  @click="tab = item.id"
                >
                  {{ item.label }}
                </button>
              </li>
            </ul>
          </nav>

          <div v-if="tab === 'details'">
            <div class="mb-3">
              <label for="edit-project-name" class="form-label small fw-bold">Project Name</label>
              <input
                id="edit-project-name"
                v-model="name"
                type="text"
                class="form-control"
                maxlength="50"
                placeholder="Project Name"
                :readonly="!isOwner"
              />
              <div class="d-flex justify-content-between">
                <small class="form-hint">Max 50 characters</small>
                <small class="text-muted">{{ name.length }}/50</small>
              </div>
            </div>

            <div class="mb-3">
              <label for="edit-project-description" class="form-label small fw-bold">Description</label>
              <textarea
                id="edit-project-description"
                v-model="description"
                class="form-control"
                rows="3"
                maxlength="1000"
                placeholder="Optional details about this project"
                :readonly="!isOwner"
              />
              <div class="d-flex justify-content-end">
                <small class="text-muted">{{ description.length }}/1000</small>
              </div>
            </div>

            <div v-if="orgLinked" class="mb-3">
              <label class="form-label small fw-bold">Organization</label>
              <p class="small mb-2">
                Members were imported from
                <RouterLink to="/organizations">{{ project.organization_name || 'the organization' }}</RouterLink>.
                <template v-if="orgManaged">
                  Roles on this project are locked; organization role changes update members here.
                </template>
                <template v-else>
                  Change a member's role here, or on the Sharing tab. Organization role changes do not overwrite this board.
                </template>
              </p>
              <ProjectMemberRoster
                :members="projectMembers"
                :locked="orgManaged"
                :editable="canManage && !orgManaged"
                :roles="assignableRoles"
                @role-change="onProjectMemberRoleChange"
                @remove="onProjectMemberRemove"
              />
            </div>
            <div v-else-if="isOwner && manageableOrgs.length" class="mb-3">
              <OrgImportFields
                :orgs="manageableOrgs"
                v-model:organization-id="attachOrgId"
                v-model:import-mode="attachMode"
                v-model:members="attachMembers"
                select-id="edit-project-org"
              />
              <button
                type="button"
                class="btn btn-sm btn-outline-primary mt-2"
                :disabled="attachDisabled"
                @click="attachOrganization"
              >
                Attach organization
              </button>
            </div>

            <div v-if="isOwner" class="d-flex justify-content-between align-items-center gap-2 flex-wrap">
              <button
                type="button"
                class="btn btn-sm"
                :class="isArchivedProject(project) ? 'btn-outline-primary' : 'btn-outline-secondary'"
                :disabled="archiving"
                @click="archiveOrRestore"
              >
                <i :class="isArchivedProject(project) ? 'bi bi-arrow-counterclockwise' : 'bi bi-archive'" class="me-1" />
                {{ isArchivedProject(project) ? 'Restore project' : 'Archive project' }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-primary px-3"
                :disabled="saving || !name.trim()"
                @click="saveBasics"
              >
                Save details
              </button>
            </div>
            <p v-if="isArchivedProject(project)" class="small text-muted mt-2 mb-0">
              This project is archived. Existing tasks stay, but new tasks cannot be added until it is restored.
            </p>
          </div>

          <ProjectWorkflowPanel
            v-else-if="tab === 'board'"
            :project="project"
            @changed="onPanelChanged"
            @columns-changed="onColumnsChanged"
          />

          <ProjectSprintsPanel
            v-else-if="tab === 'sprints' && isKanban"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectTagsPanel
            v-else-if="tab === 'tags'"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectGitHubPanel
            v-else-if="tab === 'github'"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectExtensionsPanel
            v-else-if="tab === 'extensions'"
            :project="project"
          />

          <ProjectApiKeysPanel
            v-else-if="tab === 'api-keys' && canManage"
            :project="project"
          />

          <ProjectSharePanel
            v-else-if="tab === 'sharing'"
            :project="project"
            @changed="onPanelChanged"
          />

          <ProjectRolesPanel
            v-else-if="tab === 'roles' && canManage"
            :project="project"
            @changed="onPanelChanged"
          />
        </div>
        <div class="modal-footer border-0 pt-0 justify-content-end">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="close">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>
