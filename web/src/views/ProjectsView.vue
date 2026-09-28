<template>
  <div class="container mt-4">
    <div class="row">
      <div class="col-md-8">
        <div v-if="pendingInvites.length" class="card mb-4 border-primary">
          <div class="card-header">
            <h3 class="mb-0 h5">Pending project invites</h3>
          </div>
          <div class="card-body">
            <div
              v-for="inv in pendingInvites"
              :key="inv.id"
              class="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2"
            >
              <div>
                <strong>{{ inv.project_name || 'Project' }}</strong>
                <span class="text-muted"> as {{ inv.role }}</span>
                <div class="small text-muted" v-if="inv.inviter_user_name || inv.inviter_email">
                  From {{ inv.inviter_user_name || inv.inviter_email }}
                </div>
              </div>
              <div class="d-flex gap-2">
                <button class="btn btn-sm btn-primary" type="button" @click="acceptInvite(inv)">Accept</button>
                <button class="btn btn-sm btn-outline-secondary" type="button" @click="declineInvite(inv)">
                  Decline
                </button>
              </div>
            </div>
          </div>
        </div>

        <div v-if="pendingOrgInvites.length" class="card mb-4 border-primary">
          <div class="card-header">
            <h3 class="mb-0 h5">Pending organization invites</h3>
          </div>
          <div class="card-body">
            <div
              v-for="inv in pendingOrgInvites"
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
                <button class="btn btn-sm btn-primary" type="button" @click="acceptOrgInvite(inv)">Accept</button>
                <button class="btn btn-sm btn-outline-secondary" type="button" @click="declineOrgInvite(inv)">
                  Decline
                </button>
              </div>
            </div>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h3 class="mb-0">Your Projects</h3>
          </div>
          <div class="card-body">
            <p class="text-muted">
              Create projects to organize your tasks. Share a project with household members or create a
              read-only link. Deleting a project will unassign tasks from it.
            </p>
            <div class="table-responsive">
              <table class="table table-striped projects-table">
                <thead>
                  <tr>
                    <th style="width: 2rem"></th>
                    <th>Name</th>
                    <th style="width: 100px">Role</th>
                    <th style="width: 280px">Actions</th>
                  </tr>
                </thead>
                <tbody ref="ownedTbodyEl">
                  <tr
                    v-for="p in ownedProjects"
                    :key="p.id"
                    class="project-owned-row"
                    :data-project-id="p.id"
                  >
                    <td class="align-middle">
                      <span
                        class="project-drag-handle text-muted"
                        title="Drag to reorder"
                        aria-label="Drag to reorder project"
                      >
                        <i class="bi bi-grip-vertical" />
                      </span>
                    </td>
                    <td data-label="Name">
                      <div class="d-flex align-items-start gap-1">
                        <div class="min-w-0 flex-grow-1">
                          <div class="d-flex align-items-center gap-1 flex-wrap">
                            <span class="project-name-display fw-semibold">{{ p.name }}</span>
                            <span v-if="p.organization_name" class="badge text-bg-info">
                              {{ p.organization_name }}
                              <i v-if="p.org_managed" class="bi bi-lock-fill" title="Roles locked to organization" />
                            </span>
                            <button
                              class="btn btn-sm btn-link edit-project-btn p-0"
                              type="button"
                              aria-label="Edit project"
                              title="Edit project"
                              @click="openEditProject(p)"
                            >
                              <i class="bi bi-pencil" />
                            </button>
                          </div>
                          <div v-if="p.description" class="small text-muted mt-1">{{ p.description }}</div>
                        </div>
                      </div>
                      <div v-if="sharePanelId === p.id" class="mt-3 bg-body-tertiary rounded p-2">
                        <ProjectSharePanel :project="p" @changed="load" />
                      </div>
                      <div v-if="boardPanelId === p.id" class="mt-3 bg-body-tertiary rounded p-2">
                        <ProjectWorkflowPanel :project="p" @changed="load" />
                      </div>
                    </td>
                    <td data-label="Role"><span class="badge text-bg-secondary">owner</span></td>
                    <td data-label="Actions">
                      <div class="d-flex flex-wrap gap-1">
                      <button
                        class="btn btn-sm btn-outline-primary"
                        type="button"
                        @click="toggleSharePanel(p.id)"
                      >
                        Share
                      </button>
                      <button
                        class="btn btn-sm btn-outline-secondary"
                        type="button"
                        @click="toggleBoardPanel(p.id)"
                      >
                        Board
                      </button>
                      <button
                        class="btn btn-sm btn-outline-secondary"
                        type="button"
                        title="Archive project"
                        aria-label="Archive project"
                        @click="archiveProject(p)"
                      >
                        <i class="bi bi-archive" />
                      </button>
                      <button
                        class="btn btn-sm btn-danger"
                        type="button"
                        aria-label="Delete project"
                        @click="removeProject(p)"
                      >
                        <i class="bi bi-trash" />
                      </button>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="!ownedProjects.length">
                    <td colspan="4" class="text-muted">No owned projects yet.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div class="card mt-4">
          <div class="card-header">
            <h3 class="mb-0 h5">Shared with me</h3>
          </div>
          <div class="card-body">
            <div class="table-responsive" v-if="sharedProjects.length">
              <table class="table table-striped">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Owner</th>
                    <th>Role</th>
                    <th style="width: 140px">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="p in sharedProjects" :key="p.id">
                    <td>
                      <div>{{ p.name }}</div>
                      <div v-if="p.description" class="small text-muted">{{ p.description }}</div>
                    </td>
                    <td class="text-muted small">{{ p.owner_user_name || p.owner_email }}</td>
                    <td><span class="badge text-bg-info">{{ p.role }}</span></td>
                    <td>
                      <button
                        class="btn btn-sm btn-outline-secondary me-1"
                        type="button"
                        title="Project settings"
                        aria-label="Project settings"
                        @click="openEditProject(p)"
                      >
                        <i class="bi bi-gear" />
                      </button>
                      <button
                        v-if="p.role !== 'owner'"
                        class="btn btn-sm btn-outline-secondary"
                        type="button"
                        @click="leaveProject(p)"
                      >
                        Leave
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="text-muted mb-0">No shared projects yet.</p>
          </div>
        </div>

        <div v-if="archivedProjectList.length" class="card mt-4">
          <div class="card-header">
            <h3 class="mb-0 h5">Archived</h3>
          </div>
          <div class="card-body">
            <p class="text-muted">
              Archived projects are hidden from the main list and cannot accept new tasks. Restore a
              project to use it again.
            </p>
            <div class="table-responsive">
              <table class="table table-striped">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Role</th>
                    <th style="width: 220px">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="p in archivedProjectList" :key="p.id">
                    <td>
                      <div class="d-flex align-items-center gap-2 flex-wrap">
                        <span class="fw-semibold">{{ p.name }}</span>
                        <span class="badge text-bg-secondary">archived</span>
                      </div>
                      <div v-if="p.description" class="small text-muted mt-1">{{ p.description }}</div>
                    </td>
                    <td><span class="badge text-bg-secondary">{{ p.role || 'owner' }}</span></td>
                    <td>
                      <div class="d-flex flex-wrap gap-1">
                        <button
                          class="btn btn-sm btn-outline-secondary"
                          type="button"
                          title="Project settings"
                          aria-label="Project settings"
                          @click="openEditProject(p)"
                        >
                          <i class="bi bi-gear" />
                        </button>
                        <button
                          v-if="isProjectOwner(p)"
                          class="btn btn-sm btn-outline-primary"
                          type="button"
                          @click="restoreProject(p)"
                        >
                          Restore
                        </button>
                        <button
                          v-if="isProjectOwner(p)"
                          class="btn btn-sm btn-danger"
                          type="button"
                          aria-label="Delete project"
                          @click="removeProject(p)"
                        >
                          <i class="bi bi-trash" />
                        </button>
                        <button
                          v-else
                          class="btn btn-sm btn-outline-secondary"
                          type="button"
                          @click="leaveProject(p)"
                        >
                          Leave
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      <div class="col-md-4">
        <div class="card">
          <div class="card-header">
            <h3 class="mb-0 h5">Create Project</h3>
          </div>
          <div class="card-body">
            <form @submit.prevent="createProject">
              <div class="mb-3">
                <label class="form-label" for="project-name">Name</label>
                <input
                  id="project-name"
                  v-model="name"
                  type="text"
                  class="form-control"
                  maxlength="50"
                  required
                />
                <div class="d-flex justify-content-between">
                  <small class="form-hint">Max 50 Characters</small>
                  <small class="text-muted">{{ name.length }}/50</small>
                </div>
              </div>
              <div class="mb-3">
                <label class="form-label" for="project-description">Description</label>
                <textarea
                  id="project-description"
                  v-model="description"
                  class="form-control"
                  rows="3"
                  maxlength="1000"
                  placeholder="Optional"
                />
                <div class="d-flex justify-content-end">
                  <small class="text-muted">{{ description.length }}/1000</small>
                </div>
              </div>
              <div v-if="manageableOrgs.length" class="mb-3">
                <OrgImportFields
                  :orgs="manageableOrgs"
                  v-model:organization-id="organizationId"
                  v-model:import-mode="importMode"
                  v-model:members="importMembers"
                  select-id="project-org"
                />
              </div>
              <div class="d-flex gap-2">
                <button class="btn btn-primary" type="submit" :disabled="!name.trim() || (organizationId > 0 && importMode === 'select' && !importMembers.length)">Create</button>
                <RouterLink class="btn btn-secondary" to="/">Cancel</RouterLink>
              </div>
            </form>
          </div>
        </div>
      </div>
    </div>

    <ProjectSettingsModal
      :open="showEditModal"
      :project="editingProject"
      @close="closeEditProject"
      @saved="onProjectSettingsSaved"
      @changed="load"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import Sortable from 'sortablejs'
import { api } from '@/api/client'
import type { Organization, OrganizationInvite, OrgImportMember, OrgImportMode, Project, ProjectInvite } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import { useConfirm } from '@/composables/useConfirm'
import ProjectSharePanel from '@/components/ProjectSharePanel.vue'
import ProjectWorkflowPanel from '@/components/ProjectWorkflowPanel.vue'
import ProjectSettingsModal from '@/components/ProjectSettingsModal.vue'
import OrgImportFields from '@/components/OrgImportFields.vue'
import { isArchivedProject, isProjectOwner } from '@/utils/projectLabel'

const projects = ref<Project[]>([])
const pendingInvites = ref<ProjectInvite[]>([])
const pendingOrgInvites = ref<OrganizationInvite[]>([])
const name = ref('')
const description = ref('')
const organizationId = ref(0)
const importMode = ref<OrgImportMode>('copy')
const importMembers = ref<OrgImportMember[]>([])
const orgs = ref<Organization[]>([])
const sharePanelId = ref<number | null>(null)
const boardPanelId = ref<number | null>(null)
const showEditModal = ref(false)
const editingProject = ref<Project | null>(null)
const ownedTbodyEl = ref<HTMLElement | null>(null)
let sortable: Sortable | null = null
const toast = useToast()
const auth = useAuth()
const { askConfirm } = useConfirm()

const ownedProjects = computed(() =>
  projects.value.filter((p) => isProjectOwner(p) && !isArchivedProject(p)),
)
const sharedProjects = computed(() =>
  projects.value.filter((p) => !isProjectOwner(p) && !isArchivedProject(p)),
)
const archivedProjectList = computed(() => projects.value.filter(isArchivedProject))
const manageableOrgs = computed(() => orgs.value.filter((o) => o.can_manage))

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

function collectOwnedIds(el: HTMLElement): number[] {
  return Array.from(el.querySelectorAll(':scope > tr.project-owned-row'))
    .map((node) => parseInt((node as HTMLElement).dataset.projectId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

async function persistOwnedOrder(orderedIds: number[]) {
  const current = ownedProjects.value.map((p) => p.id)
  if (
    orderedIds.length !== current.length ||
    orderedIds.every((id, i) => id === current[i])
  ) {
    return
  }
  const previous = [...projects.value]
  const owned = ownedProjects.value
  const rest = projects.value.filter((p) => !owned.some((o) => o.id === p.id))
  const byId = new Map(owned.map((p) => [p.id, p]))
  const nextOwned = orderedIds.map((id) => byId.get(id)!).filter(Boolean)
  projects.value = [...nextOwned, ...rest]
  try {
    await api.reorderProjects(orderedIds)
  } catch (err) {
    projects.value = previous
    toast.push(err instanceof APIError ? err.message : 'Could not reorder projects', 'error')
    await nextTick()
    initSortable()
  }
}

function initSortable() {
  destroySortable()
  if (!ownedTbodyEl.value || ownedProjects.value.length < 2) return
  sortable = Sortable.create(ownedTbodyEl.value, {
    handle: '.project-drag-handle',
    draggable: 'tr.project-owned-row',
    animation: 150,
    onStart() {
      sharePanelId.value = null
      boardPanelId.value = null
    },
    onEnd(evt) {
      const el = evt.to as HTMLElement
      void persistOwnedOrder(collectOwnedIds(el))
    },
  })
}

watch(ownedProjects, async () => {
  await nextTick()
  initSortable()
})

async function load() {
  try {
    const [p, invites, orgInvites, organizationList] = await Promise.all([
      api.listProjects(),
      api.listMyProjectInvites(),
      api.listMyOrganizationInvites().catch(() => [] as OrganizationInvite[]),
      api.listOrganizations().catch(() => []),
    ])
    projects.value = p
    pendingInvites.value = invites
    pendingOrgInvites.value = orgInvites
    orgs.value = organizationList
    if (editingProject.value) {
      const updated = p.find((x) => x.id === editingProject.value!.id)
      if (updated) editingProject.value = updated
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load projects', 'error')
  }
}

async function createProject() {
  if (!name.value.trim()) return
  try {
    await api.createProject(name.value.trim(), description.value.trim(), {
      organization_id: Number(organizationId.value) || null,
      org_import: importMode.value,
      org_import_members: importMembers.value,
    })
    name.value = ''
    description.value = ''
    organizationId.value = 0
    importMode.value = 'copy'
    importMembers.value = []
    toast.push('Project created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

function openEditProject(p: Project) {
  editingProject.value = p
  showEditModal.value = true
}

function closeEditProject() {
  showEditModal.value = false
  editingProject.value = null
}

async function onProjectSettingsSaved() {
  await load()
}

async function archiveProject(p: Project) {
  const ok = await askConfirm({
    title: 'Archive project?',
    message: `Archive “${p.name}”? It will move to the Archived section, its tasks will be tagged archived, and new tasks cannot be added until you restore it.`,
    confirmLabel: 'Archive',
  })
  if (!ok) return
  try {
    await api.archiveProject(p.id)
    if (sharePanelId.value === p.id) sharePanelId.value = null
    if (boardPanelId.value === p.id) boardPanelId.value = null
    toast.push('Project archived', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Archive failed', 'error')
  }
}

async function restoreProject(p: Project) {
  try {
    await api.restoreProject(p.id)
    toast.push('Project restored', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Restore failed', 'error')
  }
}

async function removeProject(p: Project) {
  const ok = await askConfirm({
    title: 'Delete project?',
    message: 'Delete this project? Tasks will be unassigned but not deleted.',
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteProject(p.id)
    if (sharePanelId.value === p.id) sharePanelId.value = null
    if (boardPanelId.value === p.id) boardPanelId.value = null
    if (editingProject.value?.id === p.id) closeEditProject()
    toast.push('Project deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

function toggleSharePanel(id: number) {
  sharePanelId.value = sharePanelId.value === id ? null : id
  if (sharePanelId.value != null) boardPanelId.value = null
}

function toggleBoardPanel(id: number) {
  boardPanelId.value = boardPanelId.value === id ? null : id
  if (boardPanelId.value != null) sharePanelId.value = null
}

async function leaveProject(p: Project) {
  const me = auth.user.value?.id
  if (!me) return
  const ok = await askConfirm({
    title: 'Leave project?',
    message: `Leave project “${p.name}”?`,
    confirmLabel: 'Leave',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeProjectMember(p.id, me)
    toast.push('Left project', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Leave failed', 'error')
  }
}

async function acceptInvite(inv: ProjectInvite) {
  try {
    await api.acceptProjectInvite(inv.id)
    toast.push('Joined project', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Accept failed', 'error')
  }
}

async function declineInvite(inv: ProjectInvite) {
  try {
    await api.declineProjectInvite(inv.id)
    toast.push('Invite declined', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Decline failed', 'error')
  }
}

async function acceptOrgInvite(inv: OrganizationInvite) {
  try {
    await api.acceptOrganizationInvite(inv.id)
    toast.push('Joined organization', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Accept failed', 'error')
  }
}

async function declineOrgInvite(inv: OrganizationInvite) {
  try {
    await api.declineOrganizationInvite(inv.id)
    toast.push('Invite declined', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Decline failed', 'error')
  }
}

onMounted(async () => {
  await load()
  await nextTick()
  initSortable()
})

onBeforeUnmount(() => {
  destroySortable()
})
</script>
