<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type {
  Project,
  ProjectExtension,
  ProjectExtensionPatch,
  ProjectInboundWebhook,
  ProjectMember,
  ProjectStatus,
  Tag,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useSite } from '@/composables/useSite'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'
import { withBase } from '@/base'

const props = defineProps<{
  project: Project
}>()

const toast = useToast()
const { siteInfo } = useSite()
const inboundAllowed = computed(() => !!siteInfo.value?.enable_inbound_webhooks)
const loading = ref(false)
const extensions = ref<ProjectExtension[]>([])
const tags = ref<Tag[]>([])
const members = ref<ProjectMember[]>([])
const statuses = ref<ProjectStatus[]>([])
const inbound = ref<ProjectInboundWebhook | null>(null)
const inboundBusy = ref(false)
const shownInboundSecret = ref('')
const busyId = ref<string | null>(null)
const testBusyId = ref<string | null>(null)
const secretDraft = reactive<Record<string, string>>({})
const mentionDraft = reactive<Record<string, string>>({})
const shownSigning = reactive<Record<string, string>>({})
const shownCallback = reactive<Record<string, string>>({})
const sampleJSON = reactive<Record<string, string>>({})
const expanded = reactive<Record<string, boolean>>({})
const listIsOwner = ref(false)

const isOwner = computed(() => listIsOwner.value || (props.project.role || 'owner') === 'owner')
const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')

function hookNames(ext: ProjectExtension): string[] {
  return (ext.manifest.hooks || []).map((h) => h.on)
}

function hookLabel(ext: ProjectExtension, on: string): string {
  const hook = (ext.manifest.hooks || []).find((h) => h.on === on)
  return hook?.label || on
}

function hasUI(ext: ProjectExtension) {
  return !!ext.manifest.ui
}

function iconSrc(ext: ProjectExtension) {
  return ext.manifest.icon ? withBase(`/api/v1/extensions/${ext.id}/icon`) : ''
}

function uiSrc(ext: ProjectExtension) {
  return withBase(`/api/v1/projects/${props.project.id}/extensions/${ext.id}/ui`)
}

function canRotateCallback(ext: ProjectExtension) {
  return hasControl(ext, 'rotate_callback') || (ext.manifest.permissions || []).length > 0
}

function valueOf(src: FilterSource, key: string): string {
  return src.values?.[key] || ''
}

function setValue(src: FilterSource, key: string, value: string) {
  if (!src.values) src.values = {}
  src.values[key] = value
}

function memberLabel(m: ProjectMember): string {
  return m.user_name || m.email || String(m.user_id)
}

function settingScope(field: { scope?: string }) {
  return field.scope || 'site'
}

function settingsForForm(ext: ProjectExtension, member: boolean) {
  const scope = member ? 'member' : 'project'
  const all = ext.manifest.settings || []
  const fields = all.filter((f) => settingScope(f) === scope)
  if (!member || fields.length === 0) return fields
  const extras: typeof fields = []
  for (const key of [destKey(ext), 'ntfy_auth']) {
    const field = all.find((f) => f.key === key && f.type === 'secret')
    if (field && !fields.some((f) => f.key === field.key)) extras.push(field)
  }
  return extras.length ? [...extras, ...fields] : fields
}

function hasMemberForm(ext: ProjectExtension) {
  return !isKanban.value && settingsForForm(ext, true).length > 0
}

function hasControl(ext: ProjectExtension, name: string) {
  return (ext.manifest.controls || []).includes(name)
}

function hasSetting(ext: ProjectExtension, key: string) {
  return (ext.manifest.settings || []).some(
    (f) => f.key === key || (f.type === 'field_filter' && (key === 'field_key' || key === 'field_value' || key === 'field_filter')),
  )
}

type FilterSource = ProjectExtension['settings'] | NonNullable<ProjectExtension['member']>

function boolValue(src: FilterSource, key: string, member: boolean): boolean {
  switch (key) {
    case 'status_only':
      return !!src.status_only
    case 'skip_self':
      return member ? src.skip_self !== false : !!src.skip_self
    case 'claimed_only':
      return !!src.claimed_only
    case 'claimed_is_me':
      return !!(src as NonNullable<ProjectExtension['member']>).claimed_is_me
    default:
      return false
  }
}

function setBool(src: FilterSource, key: string, value: boolean) {
  switch (key) {
    case 'status_only':
      src.status_only = value
      break
    case 'skip_self':
      src.skip_self = value
      break
    case 'claimed_only':
      src.claimed_only = value
      break
    case 'claimed_is_me':
      (src as NonNullable<ProjectExtension['member']>).claimed_is_me = value
      break
  }
}

function stringValue(src: FilterSource, key: string): string {
  switch (key) {
    case 'quiet_hours_start':
      return src.quiet_hours_start || ''
    case 'quiet_hours_end':
      return src.quiet_hours_end || ''
    case 'digest':
      return src.digest || ''
    case 'field_key':
      return src.field_key || ''
    case 'field_value':
      return src.field_value || ''
    default:
      return ''
  }
}

function setString(src: FilterSource, key: string, value: string) {
  switch (key) {
    case 'quiet_hours_start':
      src.quiet_hours_start = value
      break
    case 'quiet_hours_end':
      src.quiet_hours_end = value
      break
    case 'digest':
      src.digest = value
      break
    case 'field_key':
      src.field_key = value
      break
    case 'field_value':
      src.field_value = value
      break
  }
}

function customFields(ext: ProjectExtension) {
  return ext.manifest.fields || []
}

function deliveryType(ext: ProjectExtension) {
  return ext.manifest.delivery?.type || ''
}

function destKey(ext: ProjectExtension) {
  return ext.manifest.delivery?.url_from || 'webhook_url'
}

function templateValue(ext: ProjectExtension, hook: string, member: boolean): string {
  const src = member ? ext.member : ext.settings
  return src?.templates?.[hook] ?? ext.manifest.templates?.[hook] ?? ''
}

function secretSet(ext: ProjectExtension, key: string, member: boolean): boolean {
  return member ? !!ext.member_secrets?.[key] : !!ext.secrets?.[key]
}

function draftKey(ext: ProjectExtension, key: string, member: boolean) {
  return `${ext.id}:${member ? 'me' : 'team'}:${key}`
}

function webhookPlaceholder(ext: ProjectExtension, key: string): string {
  if (key === 'ntfy_auth') return 'tk_…'
  switch (deliveryType(ext)) {
    case 'discord.webhook':
      return 'https://discord.com/api/webhooks/…'
    case 'slack.webhook':
      return 'https://hooks.slack.com/services/…'
    case 'teams.webhook':
      return 'https://prod-00.example.logic.azure.com/…'
    case 'googlechat.webhook':
      return 'https://chat.googleapis.com/v1/spaces/…/messages?key=…&token=…'
    case 'ntfy.webhook':
      return 'https://ntfy.sh/my-topic'
    default:
      return 'https://example.com/hooks/…'
  }
}

function toggleExpanded(id: string) {
  expanded[id] = !expanded[id]
}

function applyDefaults(list: ProjectExtension[]) {
  for (const e of list) {
    if (!(e.settings.triggers || []).length) e.settings.triggers = hookNames(e)
    if (!e.settings.templates) e.settings.templates = {}
    if (!e.settings.values) e.settings.values = {}
    if (!e.member) e.member = { enabled: false, triggers: hookNames(e), templates: {}, status_only: false, values: {} }
    if (!(e.member.triggers || []).length) e.member.triggers = hookNames(e)
    if (!e.member.templates) e.member.templates = {}
    if (!e.member.values) e.member.values = {}
    if (hasSetting(e, 'mention_map') && mentionDraft[e.id] === undefined) {
      mentionDraft[e.id] = e.settings.mention_map ? JSON.stringify(e.settings.mention_map, null, 2) : ''
    }
  }
}

let loadSeq = 0

async function load() {
  const seq = ++loadSeq
  const projectId = props.project.id
  loading.value = true
  try {
    const [res, tagList, memberList, statusList] = await Promise.all([
      api.listProjectExtensions(projectId),
      api.listTags({ project_id: projectId }).catch(() => [] as Tag[]),
      api.listProjectMembers(projectId).catch(() => [] as ProjectMember[]),
      api.listProjectStatuses(projectId).catch(() => [] as ProjectStatus[]),
    ])
    if (seq !== loadSeq) return
    extensions.value = res.extensions
    listIsOwner.value = !!res.is_owner
    tags.value = tagList || []
    members.value = memberList || []
    statuses.value = statusList || []
    applyDefaults(extensions.value)
    await loadInbound(projectId, seq)
  } catch (err) {
    if (seq !== loadSeq) return
    toast.push(err instanceof APIError ? err.message : 'Failed to load extensions', 'error')
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function loadInbound(projectId = props.project.id, seq?: number) {
  if (seq !== undefined && seq !== loadSeq) return
  if (!listIsOwner.value || !inboundAllowed.value) {
    if (seq !== undefined && seq !== loadSeq) return
    inbound.value = null
    shownInboundSecret.value = ''
    return
  }
  const next = await api.getProjectInbound(projectId).catch(() => null)
  if (seq !== undefined && seq !== loadSeq) return
  inbound.value = next
}

function replaceExtension(updated: ProjectExtension) {
  extensions.value = extensions.value.map((e) => (e.id === updated.id ? updated : e))
  applyDefaults(extensions.value)
  if (updated.signing_secret) shownSigning[updated.id] = updated.signing_secret
  if (updated.callback_token) shownCallback[updated.id] = updated.callback_token
}

function parseMentionMap(ext: ProjectExtension): Record<string, string> | undefined {
  const raw = mentionDraft[ext.id]?.trim()
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw) as Record<string, string>
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed
  } catch {
    toast.push('Mention map must be JSON like {"ada":"<@U123>"}', 'error')
    return undefined
  }
  toast.push('Mention map must be a JSON object', 'error')
  return undefined
}

function payloadFrom(ext: ProjectExtension, member: boolean, extra: ProjectExtensionPatch = {}): ProjectExtensionPatch | null {
  const src = member ? ext.member! : ext.settings
  const payload: ProjectExtensionPatch = {
    enabled: src.enabled,
    triggers: [...(src.triggers || [])],
    templates: { ...(src.templates || {}) },
    ...extra,
  }
  if (hasSetting(ext, 'status_only')) payload.status_only = src.status_only
  if (hasSetting(ext, 'skip_self')) payload.skip_self = member ? src.skip_self !== false : !!src.skip_self
  if (hasSetting(ext, 'min_priority')) payload.min_priority = src.min_priority || 0
  if (hasSetting(ext, 'tag_ids')) payload.tag_ids = [...(src.tag_ids || [])]
  if (hasSetting(ext, 'claimed_only')) payload.claimed_only = src.claimed_only
  if (member && hasSetting(ext, 'claimed_is_me')) payload.claimed_is_me = ext.member?.claimed_is_me
  if (hasSetting(ext, 'field_key') || hasSetting(ext, 'field_value') || hasSetting(ext, 'field_filter')) {
    payload.field_key = src.field_key || ''
    payload.field_value = src.field_value || ''
  }
  if (hasSetting(ext, 'quiet_hours_start')) payload.quiet_hours_start = src.quiet_hours_start || ''
  if (hasSetting(ext, 'quiet_hours_end')) payload.quiet_hours_end = src.quiet_hours_end || ''
  if (hasSetting(ext, 'digest')) payload.digest = src.digest || ''
  if (!member && hasSetting(ext, 'mention_map')) {
    const mentions = parseMentionMap(ext)
    if (mentions === undefined) return null
    payload.mention_map = mentions
  }
  payload.values = { ...(src.values || {}) }
  const key = destKey(ext)
  const destDraft = secretDraft[draftKey(ext, key, member)]?.trim()
  if (destDraft) payload.webhook_url = destDraft
  const ntfy = secretDraft[draftKey(ext, 'ntfy_auth', member)]?.trim()
  if (ntfy) payload.ntfy_auth = ntfy
  return payload
}

async function save(ext: ProjectExtension, member: boolean) {
  if (!ext.site_enabled) return
  const payload = payloadFrom(ext, member)
  if (!payload) return
  busyId.value = `${ext.id}:${member ? 'me' : 'team'}`
  try {
    const saved = member
      ? await api.patchProjectExtensionMe(props.project.id, ext.id, payload)
      : await api.patchProjectExtension(props.project.id, ext.id, payload)
    replaceExtension(saved)
    secretDraft[draftKey(ext, destKey(ext), member)] = ''
    secretDraft[draftKey(ext, 'ntfy_auth', member)] = ''
    if (customFields(saved).length) clearCustomFieldDefsCache(props.project.id)
    toast.push(member ? 'Notify-me settings saved' : 'Team webhook saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function rotateCallback(ext: ProjectExtension, member: boolean) {
  busyId.value = `${ext.id}:cb:${member ? 'me' : 'team'}`
  try {
    const payload: ProjectExtensionPatch = { rotate_callback: true }
    const saved = member
      ? await api.patchProjectExtensionMe(props.project.id, ext.id, payload)
      : await api.patchProjectExtension(props.project.id, ext.id, payload)
    replaceExtension(saved)
    toast.push('Callback token rotated — copy it now', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rotate failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function rotateSigning(ext: ProjectExtension, member: boolean) {
  busyId.value = `${ext.id}:sign:${member ? 'me' : 'team'}`
  try {
    const payload: ProjectExtensionPatch = { rotate_signing: true }
    const saved = member
      ? await api.patchProjectExtensionMe(props.project.id, ext.id, payload)
      : await api.patchProjectExtension(props.project.id, ext.id, payload)
    replaceExtension(saved)
    toast.push('Signing secret rotated — copy it now', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rotate failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function test(ext: ProjectExtension, member: boolean) {
  testBusyId.value = `${ext.id}:${member ? 'me' : 'team'}`
  try {
    const res = member
      ? await api.testProjectExtensionMe(props.project.id, ext.id)
      : await api.testProjectExtension(props.project.id, ext.id)
    if (res.sample_json) sampleJSON[ext.id] = res.sample_json
    toast.push(res.message || 'Test message sent', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testBusyId.value = null
  }
}

function toggleTrigger(ext: ProjectExtension, hook: string, checked: boolean, member: boolean) {
  const src = member ? ext.member! : ext.settings
  const cur = new Set(src.triggers || [])
  if (checked) cur.add(hook)
  else cur.delete(hook)
  src.triggers = [...cur]
}

function setTemplate(ext: ProjectExtension, hook: string, value: string, member: boolean) {
  const src = member ? ext.member! : ext.settings
  if (!src.templates) src.templates = {}
  src.templates[hook] = value
}

function toggleTag(ext: ProjectExtension, tagId: number, checked: boolean, member: boolean) {
  const src = member ? ext.member! : ext.settings
  const cur = new Set(src.tag_ids || [])
  if (checked) cur.add(tagId)
  else cur.delete(tagId)
  src.tag_ids = [...cur]
}

async function saveInbound() {
  if (!inbound.value) return
  inboundBusy.value = true
  try {
    inbound.value = await api.patchProjectInbound(props.project.id, {
      enabled: inbound.value.enabled,
      allow_create: inbound.value.allow_create,
      allow_comment: inbound.value.allow_comment,
    })
    if (inbound.value.secret) shownInboundSecret.value = inbound.value.secret
    toast.push('Inbound webhook saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    inboundBusy.value = false
  }
}

async function rotateInbound() {
  inboundBusy.value = true
  try {
    inbound.value = await api.patchProjectInbound(props.project.id, { rotate_secret: true })
    shownInboundSecret.value = inbound.value.secret || ''
    toast.push('Inbound secret rotated — copy it now', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rotate failed', 'error')
  } finally {
    inboundBusy.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    load()
  },
  { immediate: true },
)

watch(inboundAllowed, () => {
  void loadInbound(props.project.id)
})
</script>

<template>
  <div>
    <p v-if="loading" class="text-muted small mb-0">Loading extensions…</p>
    <template v-else>
    <div v-if="isOwner && inboundAllowed && inbound" class="card mb-3">
      <div class="card-header">
        <span class="h6 mb-0">Inbound webhook</span>
      </div>
      <div class="card-body">
        <p class="small text-muted">
          Public <code>POST</code> to create a task, add a comment, or (when an enabled extension
          declares them) complete a task or set a custom field. Sign with
          <code>X-Ordryn-Signature: sha256=…</code> or send <code>X-Ordryn-Webhook-Secret</code>.
        </p>
        <p class="text-break small"><code>{{ inbound.url }}</code></p>
        <div class="form-check mb-2">
          <input id="inbound-enabled" v-model="inbound.enabled" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="inbound-enabled">Enable inbound webhook</label>
        </div>
        <div class="form-check mb-2">
          <input id="inbound-create" v-model="inbound.allow_create" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="inbound-create">Allow create</label>
        </div>
        <div class="form-check mb-3">
          <input id="inbound-comment" v-model="inbound.allow_comment" class="form-check-input" type="checkbox" />
          <label class="form-check-label" for="inbound-comment">Allow comment</label>
        </div>
        <div v-if="shownInboundSecret" class="alert alert-warning">
          Secret (shown once): <code class="user-select-all">{{ shownInboundSecret }}</code>
        </div>
        <p v-else-if="inbound.secret_set" class="small text-success">Secret is stored.</p>
        <p v-if="inbound.last_error" class="small text-warning">Last delivery: {{ inbound.last_error }}</p>
        <p v-else-if="inbound.last_delivery_at" class="small text-muted">Last delivery: {{ inbound.last_delivery_at }}</p>
        <div class="d-flex flex-wrap gap-2">
          <button type="button" class="btn btn-sm btn-primary" :disabled="inboundBusy" @click="saveInbound">
            {{ inboundBusy ? 'Saving…' : 'Save' }}
          </button>
          <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="inboundBusy" @click="rotateInbound">
            Rotate secret
          </button>
        </div>
      </div>
    </div>

    <div v-if="!extensions.length" class="alert alert-secondary mb-0">
      No extensions are available for this project. A site admin must enable them in Admin →
      Extensions after copying a folder from <code>examples/extensions/</code> into
      <code>data/extensions/</code>.
    </div>

    <div v-for="ext in extensions" :key="ext.id" class="card mb-3">
      <button
        type="button"
        class="card-header d-flex flex-wrap align-items-center justify-content-between gap-2 text-start border-0 bg-transparent w-100"
        :aria-expanded="!!expanded[ext.id]"
        @click="toggleExpanded(ext.id)"
      >
        <span class="d-flex align-items-center gap-2">
          <i class="bi" :class="expanded[ext.id] ? 'bi-chevron-down' : 'bi-chevron-right'" aria-hidden="true" />
          <img v-if="iconSrc(ext)" :src="iconSrc(ext)" alt="" width="20" height="20" class="rounded" />
          <span class="h6 mb-0">{{ ext.name || ext.id }}</span>
        </span>
      </button>
      <div class="card-body pt-0">
        <div v-if="expanded[ext.id]">
          <p v-if="ext.manifest.description" class="small text-muted">{{ ext.manifest.description }}</p>
          <p v-if="ext.manifest.author || ext.manifest.license || ext.manifest.homepage" class="small text-muted">
            <span v-if="ext.manifest.author">{{ ext.manifest.author }}</span>
            <span v-if="ext.manifest.license"> · {{ ext.manifest.license }}</span>
            <a v-if="ext.manifest.homepage" :href="ext.manifest.homepage" target="_blank" rel="noopener noreferrer">Homepage</a>
          </p>
          <p v-if="!isOwner && !hasMemberForm(ext) && !customFields(ext).length" class="small text-muted mb-0">
            The project owner configures this extension.
          </p>

          <div v-if="customFields(ext).length" class="mb-3">
            <div class="fw-semibold mb-2">Custom fields</div>
            <ul class="small mb-0 ps-3">
              <li v-for="field in customFields(ext)" :key="field.key">
                {{ field.label }}
                <span class="text-muted">({{ field.type }})</span>
              </li>
            </ul>
          </div>

          <fieldset v-if="isOwner" :disabled="!ext.site_enabled" class="mb-4">
            <legend class="h6">Team channel</legend>
            <form @submit.prevent="save(ext, false)">
              <div class="d-flex flex-wrap align-items-center gap-3 mb-2">
                <div class="form-check mb-0">
                  <input :id="`proj-ext-${ext.id}-enabled`" v-model="ext.settings.enabled" class="form-check-input" type="checkbox" />
                  <label class="form-check-label" :for="`proj-ext-${ext.id}-enabled`">Enable for this project</label>
                </div>
                <button type="submit" class="btn btn-sm btn-primary" :disabled="busyId === `${ext.id}:team`">
                  {{ busyId === `${ext.id}:team` ? 'Saving…' : 'Save team' }}
                </button>
              </div>
              <div v-for="field in settingsForForm(ext, false)" :key="`team-${field.key}`" class="mb-3">
                <template v-if="field.type === 'secret'">
                  <label class="form-label" :for="`proj-ext-${ext.id}-team-${field.key}`">{{ field.label }}</label>
                  <input
                    :id="`proj-ext-${ext.id}-team-${field.key}`"
                    v-model="secretDraft[draftKey(ext, field.key, false)]"
                    type="password"
                    class="form-control"
                    autocomplete="off"
                    :placeholder="secretSet(ext, field.key, false) ? 'Set — leave blank to keep' : webhookPlaceholder(ext, field.key)"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'hook_select'">
                  <div class="fw-semibold mb-2">{{ field.label }}</div>
                  <div v-if="field.description" class="form-text mb-1">{{ field.description }}</div>
                  <div v-for="hook in hookNames(ext)" :key="`team-${hook}`" class="form-check">
                    <input
                      :id="`proj-ext-${ext.id}-hook-${hook}`"
                      class="form-check-input"
                      type="checkbox"
                      :checked="(ext.settings.triggers || []).includes(hook)"
                      @change="toggleTrigger(ext, hook, ($event.target as HTMLInputElement).checked, false)"
                    />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-hook-${hook}`">{{ hookLabel(ext, hook) }}</label>
                  </div>
                </template>
                <template v-else-if="field.type === 'bool'">
                  <div class="form-check">
                    <input
                      :id="`proj-ext-${ext.id}-team-${field.key}`"
                      class="form-check-input"
                      type="checkbox"
                      :checked="boolValue(ext.settings, field.key, false)"
                      @change="setBool(ext.settings, field.key, ($event.target as HTMLInputElement).checked)"
                    />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-team-${field.key}`">{{ field.label }}</label>
                  </div>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'select'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.settings, field.key)"
                    @change="setValue(ext.settings, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">None</option>
                    <option v-for="opt in field.options || []" :key="opt.value" :value="opt.value">
                      {{ opt.label || opt.value }}
                    </option>
                  </select>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'status'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.settings, field.key)"
                    @change="setValue(ext.settings, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">Any</option>
                    <option v-for="st in statuses" :key="st.id" :value="String(st.id)">{{ st.name }}</option>
                  </select>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'user'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.settings, field.key)"
                    @change="setValue(ext.settings, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">Anyone</option>
                    <option v-for="m in members" :key="m.user_id" :value="String(m.user_id)">{{ memberLabel(m) }}</option>
                  </select>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'string' || field.type === 'int'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    class="form-control form-control-sm"
                    :type="field.type === 'int' ? 'number' : 'text'"
                    :value="valueOf(ext.settings, field.key)"
                    @input="setValue(ext.settings, field.key, ($event.target as HTMLInputElement).value)"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'priority' || field.key === 'min_priority'">
                  <label class="form-label" :for="`proj-ext-${ext.id}-pri`">{{ field.label }}</label>
                  <select :id="`proj-ext-${ext.id}-pri`" v-model.number="ext.settings.min_priority" class="form-select form-select-sm">
                    <option :value="0">Any</option>
                    <option :value="1">Low+</option>
                    <option :value="2">Medium+</option>
                    <option :value="3">High</option>
                  </select>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'tag_ids' || field.key === 'tag_ids'">
                  <div v-if="tags.length">
                    <div class="fw-semibold mb-1">{{ field.label }}</div>
                    <div v-if="field.description" class="form-text mb-1">{{ field.description }}</div>
                    <div v-for="tag in tags" :key="tag.id" class="form-check">
                      <input
                        :id="`proj-ext-${ext.id}-tag-${tag.id}`"
                        class="form-check-input"
                        type="checkbox"
                        :checked="(ext.settings.tag_ids || []).includes(tag.id)"
                        @change="toggleTag(ext, tag.id, ($event.target as HTMLInputElement).checked, false)"
                      />
                      <label class="form-check-label" :for="`proj-ext-${ext.id}-tag-${tag.id}`">{{ tag.name }}</label>
                    </div>
                  </div>
                </template>
                <template v-else-if="field.type === 'time'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    :value="stringValue(ext.settings, field.key)"
                    type="time"
                    class="form-control form-control-sm"
                    @input="setString(ext.settings, field.key, ($event.target as HTMLInputElement).value)"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'digest' || field.key === 'digest'">
                  <label class="form-label">{{ field.label }}</label>
                  <select v-model="ext.settings.digest" class="form-select form-select-sm">
                    <option value="">Immediate</option>
                    <option value="hourly">Hourly</option>
                    <option value="daily">Daily</option>
                  </select>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'field_filter'">
                  <label class="form-label">{{ field.label }}</label>
                  <input v-model="ext.settings.field_key" class="form-control form-control-sm" placeholder="severity.level" />
                  <input v-model="ext.settings.field_value" class="form-control form-control-sm mt-1" placeholder="value (optional)" />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'mention_map'">
                  <label class="form-label">{{ field.label }}</label>
                  <textarea v-model="mentionDraft[ext.id]" class="form-control font-monospace" rows="2" placeholder='{"ada":"<@U123>"}' />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'string'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    :value="stringValue(ext.settings, field.key)"
                    class="form-control form-control-sm"
                    @input="setString(ext.settings, field.key, ($event.target as HTMLInputElement).value)"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
                <template v-else-if="field.type === 'int'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    :value="ext.settings.min_priority || 0"
                    type="number"
                    class="form-control form-control-sm"
                    @input="ext.settings.min_priority = Number(($event.target as HTMLInputElement).value)"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>
              </div>
              <div v-if="hookNames(ext).length" class="mb-3">
                <div class="fw-semibold mb-2">Messages</div>
                <p class="small text-muted">
                  Tokens: <code>{task}</code> <code>{name}</code> <code>{status}</code> <code>{old_status}</code>
                  <code>{project}</code> <code>{actor}</code> <code>{url}</code> <code>{id}</code> <code>{priority}</code>
                  <code>{comment}</code> <code>{claimed_by}</code> <code>{due_date}</code> <code>{sprint}</code> <code>{tags}</code>
                  <code>{mentions}</code> <code>{member}</code>
                </p>
                <div v-for="hook in hookNames(ext)" :key="`tmpl-${hook}`" class="mb-2">
                  <label class="form-label">{{ hookLabel(ext, hook) }}</label>
                  <textarea
                    class="form-control"
                    rows="2"
                    :value="templateValue(ext, hook, false)"
                    @input="setTemplate(ext, hook, ($event.target as HTMLTextAreaElement).value, false)"
                  />
                </div>
              </div>
              <div v-if="shownSigning[ext.id]" class="alert alert-warning">
                Signing secret (shown once): <code class="user-select-all">{{ shownSigning[ext.id] }}</code>
              </div>
              <p v-else-if="hasControl(ext, 'rotate_signing') && ext.signing_set" class="small text-success">Outbound HMAC signing secret is set.</p>
              <div v-if="shownCallback[ext.id]" class="alert alert-warning">
                Callback token (shown once): <code class="user-select-all">{{ shownCallback[ext.id] }}</code>
              </div>
              <p v-else-if="canRotateCallback(ext) && ext.callback_set" class="small text-success">Callback token is set. JSON webhooks include it as <code>callback_token</code>.</p>
              <div v-if="ext.settings.last_error" class="alert alert-warning">Last delivery error: {{ ext.settings.last_error }}</div>
              <div v-if="ext.deliveries?.length" class="small mb-2">
                <div class="fw-semibold">Recent deliveries</div>
                <ul class="mb-0 ps-3">
                  <li v-for="row in ext.deliveries" :key="row.id">
                    {{ row.created_at }} · {{ row.event }} · {{ row.status }}
                    <span v-if="row.http_code"> ({{ row.http_code }})</span>
                    <span v-if="row.error" class="text-warning"> {{ row.error }}</span>
                  </li>
                </ul>
              </div>
              <div v-if="hasControl(ext, 'send_test') || hasControl(ext, 'rotate_signing') || canRotateCallback(ext)" class="d-flex flex-wrap gap-2">
                <button
                  v-if="hasControl(ext, 'send_test')"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="testBusyId === `${ext.id}:team`"
                  @click="test(ext, false)"
                >
                  {{ testBusyId === `${ext.id}:team` ? 'Sending…' : 'Send test' }}
                </button>
                <button
                  v-if="hasControl(ext, 'rotate_signing')"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="!!busyId"
                  @click="rotateSigning(ext, false)"
                >
                  Rotate signing secret
                </button>
                <button
                  v-if="canRotateCallback(ext)"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="!!busyId"
                  @click="rotateCallback(ext, false)"
                >
                  Rotate callback token
                </button>
              </div>
              <pre v-if="hasControl(ext, 'sample_json') && sampleJSON[ext.id]" class="small bg-body-tertiary p-2 mt-3 mb-0 overflow-auto">{{ sampleJSON[ext.id] }}</pre>
            </form>
          </fieldset>

          <fieldset v-if="hasMemberForm(ext)" :disabled="!ext.site_enabled || !ext.settings.enabled" class="mb-0">
            <legend class="h6">Notify me</legend>
            <p class="small text-muted">
              Your own destination for this project.
              <span v-if="hasSetting(ext, 'skip_self')">Skip-self is on by default.</span>
            </p>
            <form @submit.prevent="save(ext, true)">
              <div class="d-flex flex-wrap align-items-center gap-3 mb-2">
                <div class="form-check mb-0">
                  <input :id="`proj-ext-${ext.id}-me-enabled`" v-model="ext.member!.enabled" class="form-check-input" type="checkbox" />
                  <label class="form-check-label" :for="`proj-ext-${ext.id}-me-enabled`">Enable my notifications</label>
                </div>
                <button type="submit" class="btn btn-sm btn-primary" :disabled="busyId === `${ext.id}:me`">
                  {{ busyId === `${ext.id}:me` ? 'Saving…' : 'Save mine' }}
                </button>
              </div>
              <div v-for="field in settingsForForm(ext, true)" :key="`me-${field.key}`" class="mb-3">
                <template v-if="field.type === 'secret'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    v-model="secretDraft[draftKey(ext, field.key, true)]"
                    type="password"
                    class="form-control"
                    autocomplete="off"
                    :placeholder="secretSet(ext, field.key, true) ? 'Set — leave blank to keep' : webhookPlaceholder(ext, field.key)"
                  />
                </template>
                <template v-else-if="field.type === 'hook_select'">
                  <div class="fw-semibold mb-2">{{ field.label }}</div>
                  <div v-for="hook in hookNames(ext)" :key="`me-${hook}`" class="form-check">
                    <input
                      class="form-check-input"
                      type="checkbox"
                      :checked="(ext.member?.triggers || []).includes(hook)"
                      @change="toggleTrigger(ext, hook, ($event.target as HTMLInputElement).checked, true)"
                    />
                    <label class="form-check-label">{{ hookLabel(ext, hook) }}</label>
                  </div>
                </template>
                <template v-else-if="field.type === 'bool'">
                  <div class="form-check">
                    <input
                      class="form-check-input"
                      type="checkbox"
                      :checked="boolValue(ext.member!, field.key, true)"
                      @change="setBool(ext.member!, field.key, ($event.target as HTMLInputElement).checked)"
                    />
                    <label class="form-check-label">{{ field.label }}</label>
                  </div>
                </template>
                <template v-else-if="field.type === 'select'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.member!, field.key)"
                    @change="setValue(ext.member!, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">None</option>
                    <option v-for="opt in field.options || []" :key="opt.value" :value="opt.value">
                      {{ opt.label || opt.value }}
                    </option>
                  </select>
                </template>
                <template v-else-if="field.type === 'status'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.member!, field.key)"
                    @change="setValue(ext.member!, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">Any</option>
                    <option v-for="st in statuses" :key="st.id" :value="String(st.id)">{{ st.name }}</option>
                  </select>
                </template>
                <template v-else-if="field.type === 'user'">
                  <label class="form-label">{{ field.label }}</label>
                  <select
                    class="form-select form-select-sm"
                    :value="valueOf(ext.member!, field.key)"
                    @change="setValue(ext.member!, field.key, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">Anyone</option>
                    <option v-for="m in members" :key="m.user_id" :value="String(m.user_id)">{{ memberLabel(m) }}</option>
                  </select>
                </template>
                <template v-else-if="field.type === 'priority' || field.key === 'min_priority'">
                  <label class="form-label">{{ field.label }}</label>
                  <select v-model.number="ext.member!.min_priority" class="form-select form-select-sm">
                    <option :value="0">Any</option>
                    <option :value="1">Low+</option>
                    <option :value="2">Medium+</option>
                    <option :value="3">High</option>
                  </select>
                </template>
                <template v-else-if="field.type === 'tag_ids' || field.key === 'tag_ids'">
                  <div v-if="tags.length">
                    <div class="fw-semibold mb-1">{{ field.label }}</div>
                    <div v-for="tag in tags" :key="`me-tag-${tag.id}`" class="form-check">
                      <input
                        class="form-check-input"
                        type="checkbox"
                        :checked="(ext.member?.tag_ids || []).includes(tag.id)"
                        @change="toggleTag(ext, tag.id, ($event.target as HTMLInputElement).checked, true)"
                      />
                      <label class="form-check-label">{{ tag.name }}</label>
                    </div>
                  </div>
                </template>
                <template v-else-if="field.type === 'time'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    :value="stringValue(ext.member!, field.key)"
                    type="time"
                    class="form-control form-control-sm"
                    @input="setString(ext.member!, field.key, ($event.target as HTMLInputElement).value)"
                  />
                </template>
                <template v-else-if="field.type === 'digest' || field.key === 'digest'">
                  <label class="form-label">{{ field.label }}</label>
                  <select v-model="ext.member!.digest" class="form-select form-select-sm">
                    <option value="">Immediate</option>
                    <option value="hourly">Hourly</option>
                    <option value="daily">Daily</option>
                  </select>
                </template>
                <template v-else-if="field.type === 'field_filter'">
                  <label class="form-label">{{ field.label }}</label>
                  <input v-model="ext.member!.field_key" class="form-control form-control-sm" placeholder="severity.level" />
                  <input v-model="ext.member!.field_value" class="form-control form-control-sm mt-1" placeholder="value (optional)" />
                </template>
                <template v-else-if="field.type === 'string'">
                  <label class="form-label">{{ field.label }}</label>
                  <input
                    :value="stringValue(ext.member!, field.key)"
                    class="form-control form-control-sm"
                    @input="setString(ext.member!, field.key, ($event.target as HTMLInputElement).value)"
                  />
                </template>
              </div>
              <div v-if="hookNames(ext).length" class="mb-3">
                <div class="fw-semibold mb-2">Messages</div>
                <div v-for="hook in hookNames(ext)" :key="`me-tmpl-${hook}`" class="mb-2">
                  <label class="form-label">{{ hookLabel(ext, hook) }}</label>
                  <textarea
                    class="form-control"
                    rows="2"
                    :value="templateValue(ext, hook, true)"
                    @input="setTemplate(ext, hook, ($event.target as HTMLTextAreaElement).value, true)"
                  />
                </div>
              </div>
              <div v-if="ext.member?.last_error" class="alert alert-warning">Last delivery error: {{ ext.member.last_error }}</div>
              <div v-if="ext.member_deliveries?.length" class="small mb-2">
                <div class="fw-semibold">Recent deliveries</div>
                <ul class="mb-0 ps-3">
                  <li v-for="row in ext.member_deliveries" :key="row.id">
                    {{ row.created_at }} · {{ row.event }} · {{ row.status }}
                    <span v-if="row.error" class="text-warning"> {{ row.error }}</span>
                  </li>
                </ul>
              </div>
              <div v-if="hasControl(ext, 'send_test') || hasControl(ext, 'rotate_signing') || canRotateCallback(ext)" class="d-flex flex-wrap gap-2">
                <button
                  v-if="hasControl(ext, 'send_test')"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="testBusyId === `${ext.id}:me`"
                  @click="test(ext, true)"
                >
                  {{ testBusyId === `${ext.id}:me` ? 'Sending…' : 'Send test' }}
                </button>
                <button
                  v-if="hasControl(ext, 'rotate_signing')"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="!!busyId"
                  @click="rotateSigning(ext, true)"
                >
                  Rotate my signing secret
                </button>
                <button
                  v-if="canRotateCallback(ext)"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="!!busyId"
                  @click="rotateCallback(ext, true)"
                >
                  Rotate my callback token
                </button>
              </div>
            </form>
          </fieldset>

          <div v-if="hasUI(ext)" class="mt-3">
            <div class="fw-semibold mb-2">Extension panel</div>
            <iframe
              class="w-100 border rounded"
              style="min-height: 280px; background: var(--bs-body-bg)"
              :src="uiSrc(ext)"
              sandbox="allow-scripts allow-forms allow-popups"
              referrerpolicy="no-referrer"
              title="Extension panel"
            />
          </div>
        </div>
      </div>
    </div>
    </template>
  </div>
</template>
