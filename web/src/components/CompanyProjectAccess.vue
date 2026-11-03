<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectAccess, ProjectMember, ProjectShare } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'
import UserSearchCombobox from '@/components/UserSearchCombobox.vue'
import { personLabel } from '@/utils/personLabel'

// Company mode: a project is shared with teams and people. A share gives a role on
// this project, each person's own site role, or view only.
const props = defineProps<{ project: Project; members: ProjectMember[] }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const { askConfirm } = useConfirm()

const access = ref<ProjectAccess | null>(null)
const shareKind = ref<'team' | 'person'>('team')
const teamId = ref<number | ''>('')
const username = ref('')
// 'own' = each person's own site role, 'view' = view only, otherwise a role id.
type AccessChoice = 'own' | 'view' | number
const choice = ref<AccessChoice>('own')
const busy = ref(false)

const canManage = computed(() => !!access.value?.can_manage)
const sharedTeamIds = computed(
  () => new Set((access.value?.shares ?? []).filter((s) => s.kind === 'team').map((s) => s.team_id)),
)
const availableTeams = computed(() => (access.value?.teams ?? []).filter((t) => !sharedTeamIds.value.has(t.id)))
const people = computed(() => props.members.filter((m) => !m.is_agent))

const roles = computed(() => access.value?.roles ?? [])

function shareChoice(s: ProjectShare): AccessChoice {
  if (s.level === 'view') return 'view'
  return s.role_id ?? 'own'
}

function choiceLabel(s: ProjectShare) {
  if (s.level === 'view') return 'View only'
  return s.role_label || 'Their own role'
}

function choicePayload(c: AccessChoice): { level: 'full' | 'view'; role_id?: number } {
  if (c === 'view') return { level: 'view' }
  if (c === 'own') return { level: 'full' }
  return { level: 'full', role_id: c }
}

function parseChoice(raw: string): AccessChoice {
  return raw === 'view' || raw === 'own' ? raw : Number(raw)
}

async function load() {
  try {
    access.value = await api.listProjectAccess(props.project.id)
  } catch (err) {
    access.value = null
    toast.push(err instanceof APIError ? err.message : 'Failed to load sharing', 'error')
  }
}

async function share() {
  busy.value = true
  try {
    access.value = await api.shareProject(props.project.id, {
      team_id: shareKind.value === 'team' && teamId.value !== '' ? teamId.value : undefined,
      username: shareKind.value === 'person' ? username.value.trim() : undefined,
      ...choicePayload(choice.value),
    })
    toast.push('Shared', 'success')
    teamId.value = ''
    username.value = ''
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not share the project', 'error')
  } finally {
    busy.value = false
  }
}

async function changeAccess(s: ProjectShare, next: AccessChoice) {
  if (next === shareChoice(s)) return
  try {
    access.value = await api.shareProject(props.project.id, {
      team_id: s.kind === 'team' ? s.team_id : undefined,
      username: s.kind === 'person' ? s.user_name : undefined,
      ...choicePayload(next),
    })
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not change access', 'error')
    await load()
  }
}

async function unshare(s: ProjectShare) {
  const ok = await askConfirm({
    title: `Stop sharing with ${s.name}?`,
    message:
      s.kind === 'team'
        ? `Members of ${s.name} lose access unless the project is also shared with them another way.`
        : `${s.name} loses access unless the project is also shared with one of their teams.`,
    confirmLabel: 'Stop sharing',
    danger: true,
  })
  if (!ok) return
  try {
    await api.unshareProject(props.project.id, s.id)
    toast.push('Sharing removed', 'info')
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not remove sharing', 'error')
  }
}

watch(() => props.project.id, load)
onMounted(load)
</script>

<template>
  <div>
    <h4 class="h6">Shared with</h4>
    <p class="small text-muted">
      Each share gives a role on this project, each person's own site role, or view only. Someone reached by more
      than one share gets the strongest. The creator and site admins always have access.
    </p>
    <ul v-if="access?.shares.length" class="list-unstyled mb-3">
      <li v-for="s in access.shares" :key="s.id" class="d-flex flex-wrap align-items-center gap-2 mb-2">
        <i class="bi" :class="s.kind === 'team' ? 'bi-people' : 'bi-person'" aria-hidden="true" />
        <span>{{ s.name }}</span>
        <span v-if="s.kind === 'team'" class="small text-muted">
          {{ s.is_company ? 'everyone' : `${s.member_count} ${s.member_count === 1 ? 'member' : 'members'}` }}
        </span>
        <template v-if="canManage">
          <select
            class="form-select form-select-sm w-auto"
            :value="shareChoice(s)"
            :aria-label="`Access for ${s.name}`"
            @change="changeAccess(s, parseChoice(($event.target as HTMLSelectElement).value))"
          >
            <option value="own">Their own role</option>
            <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.label }}</option>
            <option value="view">View only</option>
          </select>
          <button class="btn btn-sm btn-outline-danger" type="button" @click="unshare(s)">Stop sharing</button>
        </template>
        <span v-else class="badge text-bg-secondary">{{ choiceLabel(s) }}</span>
      </li>
    </ul>
    <p v-else class="small text-muted mb-3">Not shared yet: only the creator and site admins can open it.</p>

    <form v-if="canManage" class="row g-2 align-items-end mb-4" @submit.prevent="share">
      <div class="col-sm-3">
        <label class="form-label small mb-0" for="share-kind">Share with</label>
        <select id="share-kind" v-model="shareKind" class="form-select form-select-sm">
          <option value="team">A team</option>
          <option value="person">A person</option>
        </select>
      </div>
      <div class="col-sm-4">
        <template v-if="shareKind === 'team'">
          <label class="form-label small mb-0" for="share-team">Team</label>
          <select id="share-team" v-model="teamId" class="form-select form-select-sm" required>
            <option value="" disabled>{{ availableTeams.length ? 'Choose a team' : 'Shared with every team' }}</option>
            <option v-for="t in availableTeams" :key="t.id" :value="t.id">
              {{ t.name }}{{ t.is_company ? ' (everyone)' : '' }}
            </option>
          </select>
        </template>
        <template v-else>
          <label class="form-label small mb-0">Username</label>
          <UserSearchCombobox v-model="username" />
        </template>
      </div>
      <div class="col-sm-3">
        <label class="form-label small mb-0" for="share-level">Role on this project</label>
        <select id="share-level" v-model="choice" class="form-select form-select-sm">
          <option value="own">Their own role</option>
          <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.label }}</option>
          <option value="view">View only</option>
        </select>
      </div>
      <div class="col-sm-2">
        <button
          class="btn btn-sm btn-primary w-100"
          type="submit"
          :disabled="busy || (shareKind === 'team' ? teamId === '' : !username.trim())"
        >
          Share
        </button>
      </div>
    </form>

    <h4 class="h6">People with access</h4>
    <ul v-if="people.length" class="list-unstyled mb-3">
      <li v-for="m in people" :key="m.user_id" class="d-flex flex-wrap align-items-center gap-2 mb-1">
        <span>{{ personLabel(m) }}</span>
        <span class="badge text-bg-secondary">{{ m.role_name || m.role }}</span>
        <span v-if="m.user_id === project.owner_user_id" class="small text-muted">creator</span>
      </li>
    </ul>
    <p v-else class="small text-muted mb-3">Nobody yet besides site admins.</p>
  </div>
</template>
