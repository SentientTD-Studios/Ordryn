<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { CalendarInfo, GitHubConnection, ProjectExtension, ProjectExtensionPatch } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'

const { push } = useToast()
const { askConfirm } = useConfirm()
const { siteInfo, refresh: refreshSite } = useSite()

const github = ref<GitHubConnection | null>(null)
const githubPAT = ref('')
const githubBusy = ref(false)
const githubOAuthEnabled = computed(() => !!siteInfo.value?.github_oauth_configured)

const calendar = ref<CalendarInfo | null>(null)
const icsFile = ref<File | null>(null)
const inbox = ref<ProjectExtension[]>([])
const inboxBusy = ref<string | null>(null)
const inboxDraft = reactive<Record<string, string>>({})
const inboxExpanded = reactive<Record<string, boolean>>({})

async function load() {
  try {
    const [c, g, ext] = await Promise.all([
      api.getCalendar(),
      api.getGitHubConnection(),
      api.listMyExtensions().catch(() => ({ extensions: [] as ProjectExtension[] })),
    ])
    calendar.value = c
    github.value = g
    inbox.value = (ext.extensions || []).filter(hasMemberSettings)
    for (const e of inbox.value) {
      if (!e.member) e.member = { enabled: false, triggers: (e.manifest.hooks || []).map((h) => h.on), templates: {}, status_only: false }
      if (!(e.member.triggers || []).length) e.member.triggers = (e.manifest.hooks || []).map((h) => h.on)
    }
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load integrations', 'error')
  }
}

function memberFields(ext: ProjectExtension) {
  return (ext.manifest.settings || []).filter((f) => f.scope === 'member')
}

function hasMemberSettings(ext: ProjectExtension) {
  return memberFields(ext).length > 0
}

function hasMemberSetting(ext: ProjectExtension, key: string) {
  return memberFields(ext).some(
    (f) =>
      f.key === key ||
      (f.type === 'hook_select' && key === 'triggers') ||
      (f.type === 'field_filter' && (key === 'field_key' || key === 'field_value')),
  )
}

function hasControl(ext: ProjectExtension, name: string) {
  return (ext.manifest.controls || []).includes(name)
}

function inboxPlaceholder(ext: ProjectExtension, key: string) {
  if (key === 'ntfy_auth') return 'tk_…'
  switch (ext.manifest.delivery?.type) {
    case 'ntfy.webhook':
      return 'https://ntfy.sh/my-topic'
    case 'discord.webhook':
      return 'https://discord.com/api/webhooks/…'
    default:
      return 'https://example.com/hooks/…'
  }
}

function hookNames(ext: ProjectExtension) {
  return (ext.manifest.hooks || []).map((h) => h.on)
}

function boolValue(ext: ProjectExtension, key: string): boolean {
  const src = ext.member
  if (!src) return false
  switch (key) {
    case 'status_only':
      return !!src.status_only
    case 'skip_self':
      return src.skip_self !== false
    case 'claimed_only':
      return !!src.claimed_only
    case 'claimed_is_me':
      return !!src.claimed_is_me
    default:
      return false
  }
}

function setBool(ext: ProjectExtension, key: string, value: boolean) {
  if (!ext.member) return
  switch (key) {
    case 'status_only':
      ext.member.status_only = value
      break
    case 'skip_self':
      ext.member.skip_self = value
      break
    case 'claimed_only':
      ext.member.claimed_only = value
      break
    case 'claimed_is_me':
      ext.member.claimed_is_me = value
      break
  }
}

async function saveInbox(ext: ProjectExtension) {
  inboxBusy.value = ext.id
  try {
    const payload: ProjectExtensionPatch = {
      enabled: ext.member?.enabled,
    }
    if (hasMemberSetting(ext, 'triggers')) payload.triggers = [...(ext.member?.triggers || [])]
    if (hasMemberSetting(ext, 'status_only')) payload.status_only = ext.member?.status_only
    if (hasMemberSetting(ext, 'skip_self')) payload.skip_self = ext.member?.skip_self !== false
    if (hasMemberSetting(ext, 'claimed_only')) payload.claimed_only = ext.member?.claimed_only
    if (hasMemberSetting(ext, 'claimed_is_me')) payload.claimed_is_me = ext.member?.claimed_is_me
    const draft = inboxDraft[ext.id]?.trim()
    if (draft) payload.webhook_url = draft
    const saved = await api.patchMyExtension(ext.id, payload)
    inbox.value = inbox.value.map((e) => (e.id === saved.id ? saved : e))
    inboxDraft[ext.id] = ''
    push('Inbox notifications saved', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    inboxBusy.value = null
  }
}

async function testInbox(ext: ProjectExtension) {
  inboxBusy.value = `${ext.id}:test`
  try {
    const res = await api.testMyExtension(ext.id)
    push(res.message || 'Test message sent', 'success')
    await load()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    inboxBusy.value = null
  }
}

function toggleInboxTrigger(ext: ProjectExtension, hook: string, checked: boolean) {
  const cur = new Set(ext.member?.triggers || [])
  if (checked) cur.add(hook)
  else cur.delete(hook)
  ext.member!.triggers = [...cur]
}

async function connectGitHubPAT() {
  if (!githubPAT.value.trim()) return
  githubBusy.value = true
  try {
    github.value = await api.connectGitHubPAT(githubPAT.value.trim())
    githubPAT.value = ''
    push('GitHub connected', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'GitHub connect failed', 'error')
  } finally {
    githubBusy.value = false
  }
}

async function connectGitHubOAuth() {
  githubBusy.value = true
  try {
    const { authorize_url } = await api.startGitHubOAuth()
    window.location.href = authorize_url
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not start GitHub OAuth', 'error')
    githubBusy.value = false
  }
}

async function disconnectGitHub() {
  const ok = await askConfirm({
    title: 'Disconnect GitHub?',
    message: 'You will need to reconnect before linking repositories or creating issues.',
    confirmLabel: 'Disconnect',
    danger: true,
  })
  if (!ok) return
  githubBusy.value = true
  try {
    await api.disconnectGitHub()
    github.value = { connected: false }
    push('GitHub disconnected', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Disconnect failed', 'error')
  } finally {
    githubBusy.value = false
  }
}

async function regenerateCalendar() {
  const ok = await askConfirm({
    title: 'Regenerate calendar link?',
    message: 'This invalidates the current calendar feed URL. Anyone using the old link will lose access.',
    confirmLabel: 'Regenerate',
    danger: true,
  })
  if (!ok) return
  try {
    calendar.value = await api.regenerateCalendar()
    push('Calendar token regenerated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Regenerate failed', 'error')
  }
}

function onIcsFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  icsFile.value = input.files?.[0] ?? null
}

async function syncCalendar() {
  if (!icsFile.value) return
  try {
    const result = await api.syncCalendar(icsFile.value)
    push(`Updated ${result.updated} task due dates`, 'success')
    icsFile.value = null
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Calendar sync failed', 'error')
  }
}

onMounted(() => {
  void refreshSite()
  void load()
})
</script>

<template>
  <div id="github-section" class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">GitHub</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small">
        Connect GitHub to link repositories on projects you own and create or attach issues from board tasks.
      </p>
      <div v-if="github?.connected" class="d-flex flex-wrap align-items-center gap-2 mb-3">
        <span class="badge text-bg-success">Connected as @{{ github.github_login }}</span>
        <span v-if="github.auth_method" class="text-muted small">via {{ github.auth_method }}</span>
        <button
          type="button"
          class="btn btn-sm btn-outline-danger"
          :disabled="githubBusy"
          @click="disconnectGitHub"
        >
          Disconnect
        </button>
      </div>
      <template v-else>
        <div v-if="githubOAuthEnabled" class="mb-3">
          <button
            type="button"
            class="btn btn-primary"
            :disabled="githubBusy"
            @click="connectGitHubOAuth"
          >
            {{ githubBusy ? 'Redirecting…' : 'Connect with GitHub' }}
          </button>
        </div>
        <form class="row g-2" @submit.prevent="connectGitHubPAT">
          <div class="col-12">
            <label class="form-label" for="github-pat">Personal access token</label>
            <input
              id="github-pat"
              v-model="githubPAT"
              type="password"
              class="form-control"
              autocomplete="off"
              placeholder="ghp_… or github_pat_…"
              :disabled="githubBusy"
            />
            <div class="form-text">
              Needs access to the repositories you will link (fine-grained: Contents metadata + Issues read/write; classic: <code>repo</code>).
            </div>
          </div>
          <div class="col-12">
            <button type="submit" class="btn btn-outline-primary" :disabled="githubBusy || !githubPAT.trim()">
              {{ githubBusy ? 'Connecting…' : 'Connect with token' }}
            </button>
          </div>
        </form>
      </template>
    </div>
  </div>

  <div v-if="inbox.length" id="inbox-hooks" class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Personal inbox notifications</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small">
        Destinations for tasks that are not in a project.
      </p>
      <div v-for="ext in inbox" :key="ext.id" class="border rounded p-3 mb-3">
        <button type="button" class="btn btn-link p-0 text-decoration-none" @click="inboxExpanded[ext.id] = !inboxExpanded[ext.id]">
          {{ ext.name || ext.id }}
        </button>
        <form v-if="inboxExpanded[ext.id]" class="mt-3" @submit.prevent="saveInbox(ext)">
          <fieldset>
            <div class="form-check mb-2">
              <input v-model="ext.member!.enabled" class="form-check-input" type="checkbox" />
              <label class="form-check-label">Enable for my inbox</label>
            </div>
            <div v-for="field in memberFields(ext)" :key="field.key" class="mb-2">
              <template v-if="field.type === 'secret'">
                <label class="form-label">{{ field.label }}</label>
                <input
                  v-model="inboxDraft[ext.id]"
                  type="password"
                  class="form-control"
                  autocomplete="off"
                  :placeholder="ext.member_secrets?.[field.key] ? 'Set — leave blank to keep' : inboxPlaceholder(ext, field.key)"
                />
                <div v-if="field.description" class="form-text">{{ field.description }}</div>
              </template>
              <template v-else-if="field.type === 'hook_select'">
                <div class="fw-semibold mb-1">{{ field.label }}</div>
                <div v-for="hook in hookNames(ext)" :key="hook" class="form-check">
                  <input
                    class="form-check-input"
                    type="checkbox"
                    :checked="(ext.member?.triggers || []).includes(hook)"
                    @change="toggleInboxTrigger(ext, hook, ($event.target as HTMLInputElement).checked)"
                  />
                  <label class="form-check-label">{{ hook }}</label>
                </div>
              </template>
              <template v-else-if="field.type === 'bool'">
                <div class="form-check">
                  <input
                    class="form-check-input"
                    type="checkbox"
                    :checked="boolValue(ext, field.key)"
                    @change="setBool(ext, field.key, ($event.target as HTMLInputElement).checked)"
                  />
                  <label class="form-check-label">{{ field.label }}</label>
                </div>
              </template>
            </div>
            <div class="d-flex flex-wrap gap-2 mt-3">
              <button type="submit" class="btn btn-sm btn-primary" :disabled="inboxBusy === ext.id">
                {{ inboxBusy === ext.id ? 'Saving…' : 'Save' }}
              </button>
              <button
                v-if="hasControl(ext, 'send_test')"
                type="button"
                class="btn btn-sm btn-outline-secondary"
                :disabled="!!inboxBusy"
                @click="testInbox(ext)"
              >
                Send test
              </button>
            </div>
            <p v-if="ext.member?.last_error" class="small text-warning mt-2 mb-0">{{ ext.member.last_error }}</p>
          </fieldset>
        </form>
      </div>
    </div>
  </div>

  <div id="calendar-feed" class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Calendar feed</h3>
    </div>
    <div class="card-body">
      <p v-if="calendar" class="text-break"><code>{{ calendar.feed_url }}</code></p>
      <button type="button" class="btn btn-outline-secondary mb-3" @click="regenerateCalendar">Regenerate token</button>
      <form @submit.prevent="syncCalendar">
        <div class="mb-3">
          <label class="form-label">Sync due dates from ICS export</label>
          <input type="file" class="form-control" accept=".ics,text/calendar" @change="onIcsFileChange" />
        </div>
        <button type="submit" class="btn btn-outline-primary" :disabled="!icsFile">Sync calendar</button>
      </form>
    </div>
  </div>
</template>
