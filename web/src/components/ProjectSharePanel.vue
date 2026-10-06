<template>
  <div class="share-panel">
    <h4 class="h6">Members</h4>
    <p v-if="orgManaged" class="small text-muted">
      Members and roles are locked to
      <RouterLink to="/organizations">{{ project.organization_name || 'the organization' }}</RouterLink>.
      Change organization roles to update this board.
    </p>
    <p v-else-if="project.organization_id" class="small text-muted">
      Members were imported from
      <RouterLink to="/organizations">{{ project.organization_name || 'the organization' }}</RouterLink>
      as a starting point. You can change roles on this project without affecting the organization.
    </p>
    <ul v-if="members.length" class="list-unstyled mb-3">
      <li v-for="m in members" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-1">
        <span>{{ m.user_name || m.email }}</span>
        <span class="badge text-bg-secondary">{{ m.role_name || m.role }}</span>
        <span v-if="m.is_agent" class="badge text-bg-dark" title="Managed on the AI agents tab">
          <i class="bi bi-robot me-1" />AI agent
        </span>
        <span v-else-if="m.inherited" class="badge text-bg-info">from org</span>
        <template v-if="canEditMembers && canEditMember(m)">
          <select
            class="form-select form-select-sm w-auto"
            :value="m.role"
            @change="onRoleChange(m.user_id, ($event.target as HTMLSelectElement).value)"
          >
            <option v-if="m.role === 'owner'" value="owner" disabled>{{ m.role_name || 'Owner' }}</option>
            <option v-for="r in assignableRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
          </select>
          <button class="btn btn-sm btn-outline-danger" type="button" @click="removeMember(m.user_id)">
            Remove
          </button>
        </template>
      </li>
    </ul>
    <p v-else class="small text-muted mb-3">No members on this project.</p>

    <template v-if="canEditMembers">
      <h4 class="h6">Invite</h4>
      <p v-if="!assignableRoles.length" class="small text-muted mb-2">
        This project has no roles yet. Create one on the Roles tab, then invite people with it.
      </p>
      <form class="row g-2 align-items-end mb-3" @submit.prevent="sendInvite">
        <div class="col-sm-6">
          <label class="form-label small mb-0" for="invite-username">Username</label>
          <UserSearchCombobox v-model="inviteUsername" :exclude-usernames="excludeUsernames" />
        </div>
        <div class="col-sm-3">
          <label class="form-label small mb-0">Role</label>
          <select v-model="inviteRole" class="form-select form-select-sm" required>
            <option value="" disabled>Choose a role</option>
            <option v-for="r in assignableRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
          </select>
        </div>
        <div class="col-sm-3">
          <button class="btn btn-sm btn-primary w-100" type="submit" :disabled="!inviteRole">Invite</button>
        </div>
      </form>
    </template>

    <div v-if="invites.length" class="mb-3">
      <h4 class="h6">Pending invites</h4>
      <ul class="list-unstyled mb-0">
        <li v-for="inv in invites" :key="inv.id" class="d-flex justify-content-between align-items-center mb-1">
          <span class="small">{{ inv.user_name || inv.email }} ({{ assignableRoles.find((r) => r.slug === inv.role)?.name || inv.role }})</span>
          <button
            v-if="isOwner"
            class="btn btn-sm btn-link text-danger"
            type="button"
            @click="revokeInvite(inv.id)"
          >Revoke</button>
        </li>
      </ul>
    </div>

    <h4 class="h6">Read-only share link</h4>
    <p class="small text-muted mb-2">
      Anyone with the link can view tasks in this project. Revoke to make it private again.
    </p>
    <div class="mb-3">
      <button
        v-if="isOwner && !links.length"
        class="btn btn-sm btn-outline-primary"
        type="button"
        @click="createLink"
      >
        Create link
      </button>
      <p v-else-if="!isOwner && !links.length" class="small text-muted mb-0">No share link.</p>
      <ul v-else class="list-unstyled mb-2">
        <li
          v-for="link in links"
          :key="link.id"
          class="d-flex flex-wrap align-items-center gap-2 mb-2"
        >
          <code class="small text-truncate" style="max-width: 14rem">{{ link.url }}</code>
          <button class="btn btn-sm btn-outline-secondary" type="button" @click="copyLink(link.url)">
            Copy
          </button>
          <button
            v-if="isOwner"
            class="btn btn-sm btn-outline-danger"
            type="button"
            @click="revokeLink(link.id)"
          >
            Make private
          </button>
        </li>
      </ul>
      <button
        v-if="isOwner && links.length"
        class="btn btn-sm btn-link px-0"
        type="button"
        @click="createLink"
      >
        Create another link
      </button>
    </div>

    <div class="activity-section">
      <button
        class="btn btn-sm btn-link text-decoration-none px-0 activity-toggle"
        type="button"
        :aria-expanded="activityOpen"
        @click="activityOpen = !activityOpen"
      >
        <i class="bi" :class="activityOpen ? 'bi-chevron-down' : 'bi-chevron-right'" aria-hidden="true" />
        Activity
        <span v-if="events.length" class="text-muted fw-normal">({{ events.length }})</span>
      </button>
      <div v-if="activityOpen" class="activity-body mt-2">
        <ul class="list-unstyled small mb-0" v-if="events.length">
          <li v-for="ev in events" :key="ev.source + '-' + ev.id" class="mb-1 text-muted">
            <strong>{{ ev.label }}</strong>
            <span v-if="ev.actor_user_name || ev.actor_email"> · {{ ev.actor_user_name || ev.actor_email }}</span>
            · {{ formatTime(ev.created_at) }}
          </li>
        </ul>
        <p v-else class="small text-muted mb-0">No activity yet.</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Project, ProjectEvent, ProjectInvite, ProjectMember, ProjectRoleDef, ShareLink } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import UserSearchCombobox from '@/components/UserSearchCombobox.vue'
import { canManageProject } from '@/utils/projectPerms'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const members = ref<ProjectMember[]>([])
const invites = ref<ProjectInvite[]>([])
const links = ref<ShareLink[]>([])
const events = ref<ProjectEvent[]>([])
const inviteUsername = ref('')
const inviteRole = ref('')
const assignableRoles = ref<ProjectRoleDef[]>([])
const activityOpen = ref(false)
const toast = useToast()
const { askConfirm } = useConfirm()
const isOwner = computed(() => canManageProject(props.project))
const orgManaged = computed(() => !!props.project.org_managed && !!props.project.organization_id)
const canEditMembers = computed(() => isOwner.value && !orgManaged.value)
// Only an owner may change or remove another owner, and the project creator is never editable.
const actorIsOwner = computed(() => (props.project.role || 'owner') === 'owner')

function canEditMember(m: ProjectMember) {
  if (m.is_agent || m.user_id === props.project.owner_user_id) return false
  return m.role !== 'owner' || actorIsOwner.value
}

const excludeUsernames = computed(() => {
  const names: string[] = []
  for (const m of members.value) {
    if (m.user_name) names.push(m.user_name)
  }
  for (const inv of invites.value) {
    if (inv.user_name) names.push(inv.user_name)
  }
  return names
})

async function loadPanel() {
  try {
    members.value = await api.listProjectMembers(props.project.id)
  } catch (err) {
    members.value = []
    toast.push(err instanceof APIError ? err.message : 'Failed to load members', 'error')
  }
  try {
    const [inv, ln, ev, roles] = await Promise.all([
      api.listProjectInvites(props.project.id).catch(() => [] as ProjectInvite[]),
      api.listShareLinks('project', props.project.id).catch(() => [] as ShareLink[]),
      api.listProjectEvents(props.project.id).catch(() => [] as ProjectEvent[]),
      api.listProjectRoles(props.project.id).catch(() => ({ catalog: [], roles: [] })),
    ])
    invites.value = inv
    links.value = ln
    events.value = ev
    assignableRoles.value = roles.roles || []
    // No default role: there is no site-wide Editor anymore, so the manager picks one.
    if (inviteRole.value && !assignableRoles.value.some((r) => r.slug === inviteRole.value)) {
      inviteRole.value = ''
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load sharing', 'error')
  }
}

async function sendInvite() {
  if (!inviteRole.value) {
    toast.push('Choose a role for the invite', 'error')
    return
  }
  try {
    await api.createProjectInvite(props.project.id, inviteUsername.value.trim(), inviteRole.value)
    inviteUsername.value = ''
    toast.push('Invite sent', 'success')
    await loadPanel()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Invite failed', 'error')
  }
}

async function onRoleChange(userId: number, role: string) {
  if (!role || role === 'owner') return
  try {
    await api.updateProjectMember(props.project.id, userId, role)
    toast.push('Role updated', 'success')
    await loadPanel()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await loadPanel()
  }
}

async function removeMember(userId: number) {
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
    await loadPanel()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Remove failed', 'error')
  }
}

async function revokeInvite(id: number) {
  const ok = await askConfirm({
    title: 'Revoke invite?',
    message: 'Revoke this pending project invite?',
    confirmLabel: 'Revoke',
    danger: true,
  })
  if (!ok) return
  try {
    await api.revokeProjectInvite(props.project.id, id)
    toast.push('Invite revoked', 'info')
    await loadPanel()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Revoke failed', 'error')
  }
}

async function createLink() {
  try {
    const link = await api.createShareLink('project', props.project.id)
    await navigator.clipboard.writeText(link.url)
    toast.push('Share link created and copied', 'success')
    await loadPanel()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to create link', 'error')
  }
}

async function copyLink(url: string) {
  try {
    await navigator.clipboard.writeText(url)
    toast.push('Copied', 'success')
  } catch {
    toast.push(url, 'info')
  }
}

async function revokeLink(id: number) {
  const ok = await askConfirm({
    title: 'Make private?',
    message: 'Revoke this share link? Anyone with the URL will lose access.',
    confirmLabel: 'Make private',
    danger: true,
  })
  if (!ok) return
  try {
    await api.revokeShareLink(id)
    toast.push('Link revoked — project is private again for that URL', 'info')
    await loadPanel()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Revoke failed', 'error')
  }
}

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

watch(
  () => props.project.id,
  () => {
    activityOpen.value = false
    void loadPanel()
  },
)

onMounted(loadPanel)
</script>
