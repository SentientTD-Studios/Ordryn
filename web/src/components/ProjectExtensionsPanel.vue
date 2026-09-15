<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type {
  Project,
  ProjectExtension,
  ProjectExtensionPatch,
  ProjectInboundWebhook,
  Tag,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'

const props = defineProps<{
  project: Project
}>()

const toast = useToast()
const loading = ref(false)
const extensions = ref<ProjectExtension[]>([])
const tags = ref<Tag[]>([])
const inbound = ref<ProjectInboundWebhook | null>(null)
const inboundBusy = ref(false)
const shownInboundSecret = ref('')
const busyId = ref<string | null>(null)
const testBusyId = ref<string | null>(null)
const secretDraft = reactive<Record<string, string>>({})
const mentionDraft = reactive<Record<string, string>>({})
const shownSigning = reactive<Record<string, string>>({})
const sampleJSON = reactive<Record<string, string>>({})
const expanded = reactive<Record<string, boolean>>({})
const listIsOwner = ref(false)

const isOwner = computed(() => listIsOwner.value || (props.project.role || 'owner') === 'owner')

function hookNames(ext: ProjectExtension): string[] {
  return (ext.manifest.hooks || []).map((h) => h.on)
}

function projectFields(ext: ProjectExtension) {
  return (ext.manifest.settings || []).filter((f) => f.scope === 'project')
}

function customFields(ext: ProjectExtension) {
  return ext.manifest.fields || []
}

function isHookExtension(ext: ProjectExtension) {
  return hookNames(ext).length > 0 || !!ext.manifest.delivery
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
    if (!e.member) e.member = { enabled: false, triggers: hookNames(e), templates: {}, status_only: false }
    if (!(e.member.triggers || []).length) e.member.triggers = hookNames(e)
    if (!e.member.templates) e.member.templates = {}
    if (mentionDraft[e.id] === undefined) {
      mentionDraft[e.id] = e.settings.mention_map ? JSON.stringify(e.settings.mention_map, null, 2) : ''
    }
    if (e.sample_json) sampleJSON[e.id] = e.sample_json
  }
}

async function load() {
  loading.value = true
  try {
    const [res, tagList] = await Promise.all([
      api.listProjectExtensions(props.project.id),
      api.listTags({ project_id: props.project.id }).catch(() => [] as Tag[]),
    ])
    extensions.value = res.extensions
    listIsOwner.value = !!res.is_owner
    tags.value = tagList || []
    applyDefaults(extensions.value)
    if (listIsOwner.value) {
      inbound.value = await api.getProjectInbound(props.project.id)
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load extensions', 'error')
  } finally {
    loading.value = false
  }
}

function replaceExtension(updated: ProjectExtension) {
  extensions.value = extensions.value.map((e) => (e.id === updated.id ? updated : e))
  applyDefaults(extensions.value)
  if (updated.signing_secret) shownSigning[updated.id] = updated.signing_secret
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
    status_only: src.status_only,
    skip_self: src.skip_self,
    min_priority: src.min_priority || 0,
    tag_ids: [...(src.tag_ids || [])],
    claimed_only: src.claimed_only,
    field_key: src.field_key || '',
    field_value: src.field_value || '',
    quiet_hours_start: src.quiet_hours_start || '',
    quiet_hours_end: src.quiet_hours_end || '',
    digest: src.digest || '',
    ...extra,
  }
  if (member) payload.claimed_is_me = ext.member?.claimed_is_me
  else {
    const mentions = parseMentionMap(ext)
    if (mentions === undefined) return null
    payload.mention_map = mentions
  }
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
</script>

<template>
  <div>
    <p v-if="loading" class="text-muted small mb-0">Loading extensions…</p>
    <div v-else-if="!extensions.length" class="alert alert-secondary mb-0">
      No project extensions are loaded. A site admin can copy a folder from
      <code>examples/extensions/</code> into <code>data/extensions/</code> and restart the server.
    </div>

    <div v-else-if="isOwner && inbound" class="card mb-3">
      <div class="card-header">
        <span class="h6 mb-0">Inbound webhook</span>
      </div>
      <div class="card-body">
        <p class="small text-muted">
          Public <code>POST</code> to create a task or add a comment. Sign with
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

    <div v-for="ext in extensions" :key="ext.id" class="card mb-3">
      <button
        type="button"
        class="card-header d-flex flex-wrap align-items-center justify-content-between gap-2 text-start border-0 bg-transparent w-100"
        :aria-expanded="!!expanded[ext.id]"
        @click="toggleExpanded(ext.id)"
      >
        <span class="d-flex align-items-center gap-2">
          <i class="bi" :class="expanded[ext.id] ? 'bi-chevron-down' : 'bi-chevron-right'" aria-hidden="true" />
          <span class="h6 mb-0">{{ ext.name || ext.id }}</span>
        </span>
        <span v-if="ext.site_enabled" class="badge text-bg-success">Site enabled</span>
        <span v-else class="badge text-bg-secondary">Site disabled</span>
      </button>
      <div class="card-body pt-0">
        <div v-if="!ext.site_enabled" class="alert alert-warning">
          A site admin must enable this extension in Admin → Extensions before this project can
          {{ isHookExtension(ext) ? 'send notifications' : 'use its custom fields' }}.
        </div>

        <div v-if="expanded[ext.id]">
          <p v-if="ext.manifest.description" class="small text-muted">{{ ext.manifest.description }}</p>

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
              <div v-for="field in projectFields(ext)" :key="`team-${field.key}`" class="mb-3">
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
                <template v-else-if="field.type === 'bool' && field.key === 'status_only'">
                  <div class="form-check">
                    <input :id="`proj-ext-${ext.id}-status`" v-model="ext.settings.status_only" class="form-check-input" type="checkbox" />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-status`">{{ field.label }}</label>
                  </div>
                </template>
                <template v-else-if="field.type === 'hook_select'">
                  <div class="fw-semibold mb-2">{{ field.label }}</div>
                  <div v-for="hook in hookNames(ext)" :key="`team-${hook}`" class="form-check">
                    <input
                      :id="`proj-ext-${ext.id}-hook-${hook}`"
                      class="form-check-input"
                      type="checkbox"
                      :checked="(ext.settings.triggers || []).includes(hook)"
                      @change="toggleTrigger(ext, hook, ($event.target as HTMLInputElement).checked, false)"
                    />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-hook-${hook}`">{{ hook }}</label>
                  </div>
                </template>
              </div>
              <div class="form-check mb-2">
                <input :id="`proj-ext-${ext.id}-skip`" v-model="ext.settings.skip_self" class="form-check-input" type="checkbox" />
                <label class="form-check-label" :for="`proj-ext-${ext.id}-skip`">Skip events I caused</label>
              </div>
              <div class="form-check mb-2">
                <input :id="`proj-ext-${ext.id}-claimed`" v-model="ext.settings.claimed_only" class="form-check-input" type="checkbox" />
                <label class="form-check-label" :for="`proj-ext-${ext.id}-claimed`">Only claimed tasks</label>
              </div>
              <div class="mb-2">
                <label class="form-label" :for="`proj-ext-${ext.id}-pri`">Minimum priority</label>
                <select :id="`proj-ext-${ext.id}-pri`" v-model.number="ext.settings.min_priority" class="form-select form-select-sm">
                  <option :value="0">Any</option>
                  <option :value="1">Low+</option>
                  <option :value="2">Medium+</option>
                  <option :value="3">High</option>
                </select>
              </div>
              <div v-if="tags.length" class="mb-2">
                <div class="fw-semibold mb-1">Only these tags</div>
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
              <div class="row g-2 mb-2">
                <div class="col-md-6">
                  <label class="form-label">Quiet hours start</label>
                  <input v-model="ext.settings.quiet_hours_start" type="time" class="form-control form-control-sm" />
                </div>
                <div class="col-md-6">
                  <label class="form-label">Quiet hours end</label>
                  <input v-model="ext.settings.quiet_hours_end" type="time" class="form-control form-control-sm" />
                </div>
              </div>
              <div class="mb-2">
                <label class="form-label">Digest</label>
                <select v-model="ext.settings.digest" class="form-select form-select-sm">
                  <option value="">Immediate</option>
                  <option value="hourly">Hourly</option>
                  <option value="daily">Daily</option>
                </select>
              </div>
              <div class="mb-2">
                <label class="form-label">Custom field filter (key)</label>
                <input v-model="ext.settings.field_key" class="form-control form-control-sm" placeholder="severity.level" />
                <input v-model="ext.settings.field_value" class="form-control form-control-sm mt-1" placeholder="value (optional)" />
              </div>
              <div class="mb-3">
                <label class="form-label">Actor mention map (JSON)</label>
                <textarea v-model="mentionDraft[ext.id]" class="form-control font-monospace" rows="2" placeholder='{"ada":"<@U123>"}' />
              </div>
              <div v-if="hookNames(ext).length" class="mb-3">
                <div class="fw-semibold mb-2">Messages</div>
                <p class="small text-muted">
                  Tokens: <code>{task}</code> <code>{name}</code> <code>{status}</code> <code>{old_status}</code>
                  <code>{project}</code> <code>{actor}</code> <code>{url}</code> <code>{id}</code> <code>{priority}</code>
                  <code>{comment}</code> <code>{claimed_by}</code> <code>{due_date}</code> <code>{sprint}</code> <code>{tags}</code>
                </p>
                <div v-for="hook in hookNames(ext)" :key="`tmpl-${hook}`" class="mb-2">
                  <label class="form-label">{{ hook }}</label>
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
              <p v-else-if="ext.signing_set" class="small text-success">Outbound HMAC signing secret is set.</p>
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
              <div class="d-flex flex-wrap gap-2">
                <button
                  v-if="isHookExtension(ext)"
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="testBusyId === `${ext.id}:team`"
                  @click="test(ext, false)"
                >
                  {{ testBusyId === `${ext.id}:team` ? 'Sending…' : 'Send test' }}
                </button>
                <button type="button" class="btn btn-outline-secondary" :disabled="!!busyId" @click="rotateSigning(ext, false)">
                  Rotate signing secret
                </button>
              </div>
              <pre v-if="sampleJSON[ext.id]" class="small bg-body-tertiary p-2 mt-3 mb-0 overflow-auto">{{ sampleJSON[ext.id] }}</pre>
            </form>
          </fieldset>

          <fieldset v-if="isHookExtension(ext)" :disabled="!ext.site_enabled || !ext.settings.enabled" class="mb-0">
            <legend class="h6">Notify me</legend>
            <p class="small text-muted">Your own destination for this project. Skip-self is on by default.</p>
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
              <div v-for="field in projectFields(ext)" :key="`me-${field.key}`" class="mb-3">
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
                    <label class="form-check-label">{{ hook }}</label>
                  </div>
                </template>
                <template v-else-if="field.type === 'bool' && field.key === 'status_only'">
                  <div class="form-check">
                    <input v-model="ext.member!.status_only" class="form-check-input" type="checkbox" />
                    <label class="form-check-label">{{ field.label }}</label>
                  </div>
                </template>
              </div>
              <div class="form-check mb-2">
                <input
                  :checked="ext.member?.skip_self !== false"
                  class="form-check-input"
                  type="checkbox"
                  @change="ext.member!.skip_self = ($event.target as HTMLInputElement).checked"
                />
                <label class="form-check-label">Skip events I caused</label>
              </div>
              <div class="form-check mb-2">
                <input v-model="ext.member!.claimed_is_me" class="form-check-input" type="checkbox" />
                <label class="form-check-label">Only when claimed by me</label>
              </div>
              <div class="form-check mb-2">
                <input v-model="ext.member!.claimed_only" class="form-check-input" type="checkbox" />
                <label class="form-check-label">Only claimed tasks</label>
              </div>
              <div class="mb-2">
                <label class="form-label">Minimum priority</label>
                <select v-model.number="ext.member!.min_priority" class="form-select form-select-sm">
                  <option :value="0">Any</option>
                  <option :value="1">Low+</option>
                  <option :value="2">Medium+</option>
                  <option :value="3">High</option>
                </select>
              </div>
              <div class="row g-2 mb-2">
                <div class="col-md-6">
                  <label class="form-label">Quiet hours start</label>
                  <input v-model="ext.member!.quiet_hours_start" type="time" class="form-control form-control-sm" />
                </div>
                <div class="col-md-6">
                  <label class="form-label">Quiet hours end</label>
                  <input v-model="ext.member!.quiet_hours_end" type="time" class="form-control form-control-sm" />
                </div>
              </div>
              <div class="mb-2">
                <label class="form-label">Digest</label>
                <select v-model="ext.member!.digest" class="form-select form-select-sm">
                  <option value="">Immediate</option>
                  <option value="hourly">Hourly</option>
                  <option value="daily">Daily</option>
                </select>
              </div>
              <div v-if="hookNames(ext).length" class="mb-3">
                <div class="fw-semibold mb-2">Messages</div>
                <div v-for="hook in hookNames(ext)" :key="`me-tmpl-${hook}`" class="mb-2">
                  <label class="form-label">{{ hook }}</label>
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
              <div class="d-flex flex-wrap gap-2">
                <button type="button" class="btn btn-outline-secondary" :disabled="testBusyId === `${ext.id}:me`" @click="test(ext, true)">
                  {{ testBusyId === `${ext.id}:me` ? 'Sending…' : 'Send test' }}
                </button>
                <button type="button" class="btn btn-outline-secondary" :disabled="!!busyId" @click="rotateSigning(ext, true)">
                  Rotate my signing secret
                </button>
              </div>
            </form>
          </fieldset>
        </div>
      </div>
    </div>
  </div>
</template>
