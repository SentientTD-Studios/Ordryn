<template>
  <div class="share-panel">
    <h4 class="h6">Members</h4>
    <ul class="list-unstyled mb-3">
      <li v-for="m in members" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-1">
        <span>{{ m.email }}</span>
        <span class="badge text-bg-secondary">{{ m.role }}</span>
        <template v-if="m.role !== 'owner'">
          <select
            class="form-select form-select-sm w-auto"
            :value="m.role"
            @change="onRoleChange(m.user_id, ($event.target as HTMLSelectElement).value)"
          >
            <option value="editor">editor</option>
            <option value="viewer">viewer</option>
          </select>
          <button class="btn btn-sm btn-outline-danger" type="button" @click="removeMember(m.user_id)">
            Remove
          </button>
        </template>
      </li>
    </ul>

    <h4 class="h6">Invite</h4>
    <form class="row g-2 align-items-end mb-3" @submit.prevent="sendInvite">
      <div class="col-sm-6">
        <label class="form-label small mb-0">Username</label>
        <input
          v-model="inviteUsername"
          type="text"
          class="form-control form-control-sm"
          autocomplete="username"
          minlength="3"
          maxlength="32"
          pattern="[A-Za-z0-9_]+"
          required
        />
      </div>
      <div class="col-sm-3">
        <label class="form-label small mb-0">Role</label>
        <select v-model="inviteRole" class="form-select form-select-sm">
          <option value="editor">editor</option>
          <option value="viewer">viewer</option>
        </select>
      </div>
      <div class="col-sm-3">
        <button class="btn btn-sm btn-primary w-100" type="submit">Invite</button>
      </div>
    </form>

    <div v-if="invites.length" class="mb-3">
      <h4 class="h6">Pending invites</h4>
      <ul class="list-unstyled mb-0">
        <li v-for="inv in invites" :key="inv.id" class="d-flex justify-content-between align-items-center mb-1">
          <span class="small">{{ inv.user_name || inv.email }} ({{ inv.role }})</span>
          <button class="btn btn-sm btn-link text-danger" type="button" @click="revokeInvite(inv.id)">Revoke</button>
        </li>
      </ul>
    </div>

    <h4 class="h6">Read-only share link</h4>
    <p class="small text-muted mb-2">
      Anyone with the link can view tasks in this project. Revoke to make it private again.
    </p>
    <div class="mb-3">
      <button
        v-if="!links.length"
        class="btn btn-sm btn-outline-primary"
        type="button"
        @click="createLink"
      >
        Create link
      </button>
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
          <button class="btn btn-sm btn-outline-danger" type="button" @click="revokeLink(link.id)">
            Make private
          </button>
        </li>
      </ul>
      <button
        v-if="links.length"
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
            <span v-if="ev.actor_email"> · {{ ev.actor_email }}</span>
            · {{ formatTime(ev.created_at) }}
          </li>
        </ul>
        <p v-else class="small text-muted mb-0">No activity yet.</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectEvent, ProjectInvite, ProjectMember, ShareLink } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const members = ref<ProjectMember[]>([])
const invites = ref<ProjectInvite[]>([])
const links = ref<ShareLink[]>([])
const events = ref<ProjectEvent[]>([])
const inviteUsername = ref('')
const inviteRole = ref<'editor' | 'viewer'>('editor')
const activityOpen = ref(false)
const toast = useToast()
const { askConfirm } = useConfirm()

async function loadPanel() {
  try {
    const [m, inv, ln, ev] = await Promise.all([
      api.listProjectMembers(props.project.id),
      api.listProjectInvites(props.project.id),
      api.listShareLinks('project', props.project.id),
      api.listProjectEvents(props.project.id),
    ])
    members.value = m
    invites.value = inv
    links.value = ln
    events.value = ev
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load sharing', 'error')
  }
}

async function sendInvite() {
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
  if (role !== 'editor' && role !== 'viewer') return
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
