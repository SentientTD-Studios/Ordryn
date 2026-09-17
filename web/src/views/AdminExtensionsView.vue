<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AdminExtension, AdminExtensionPatch, ExtensionSettingField } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'
import { withBase } from '@/base'

const toast = useToast()
const loading = ref(false)
const extensions = ref<AdminExtension[]>([])
const busyId = ref<string | null>(null)
const testBusyId = ref<string | null>(null)
const expanded = reactive<Record<string, boolean>>({})
const siteSecretDraft = reactive<Record<string, string>>({})

const emptyHint = computed(() => extensions.value.length === 0)

function siteFields(ext: AdminExtension): ExtensionSettingField[] {
  return (ext.manifest.settings || []).filter((f) => !f.scope || f.scope === 'site')
}

function siteSecretFields(ext: AdminExtension): ExtensionSettingField[] {
  return siteFields(ext).filter((f) => f.type === 'secret')
}

function hasProjectSettings(ext: AdminExtension): boolean {
  return (ext.manifest.settings || []).some((f) => f.scope === 'project')
}

function customFields(ext: AdminExtension) {
  return ext.manifest.fields || []
}

function hookNames(ext: AdminExtension): string[] {
  return (ext.manifest.hooks || []).map((h) => h.on)
}

function hookLabel(ext: AdminExtension, on: string): string {
  const hook = (ext.manifest.hooks || []).find((h) => h.on === on)
  return hook?.label || on
}

function hasControl(ext: AdminExtension, name: string) {
  return (ext.manifest.controls || []).includes(name)
}

function triggerChecked(ext: AdminExtension, hook: string): boolean {
  const cur = ext.settings.triggers || []
  return cur.includes('*') || cur.includes(hook)
}

function toggleTrigger(ext: AdminExtension, hook: string, checked: boolean) {
  const names = hookNames(ext)
  let cur = new Set(ext.settings.triggers || [])
  if (hook === '*') {
    ext.settings.triggers = checked ? ['*'] : []
    return
  }
  if (cur.has('*')) {
    cur = new Set(names)
  }
  if (checked) cur.add(hook)
  else cur.delete(hook)
  cur.delete('*')
  ext.settings.triggers = [...cur]
}

function templateValue(ext: AdminExtension, hook: string): string {
  return ext.settings.templates?.[hook] ?? ext.manifest.templates?.[hook] ?? ''
}

function setTemplate(ext: AdminExtension, hook: string, value: string) {
  if (!ext.settings.templates) ext.settings.templates = {}
  ext.settings.templates[hook] = value
}

function toggleExpanded(id: string) {
  expanded[id] = !expanded[id]
}

async function load() {
  loading.value = true
  try {
    const res = await api.listAdminExtensions()
    extensions.value = res.extensions
    for (const ext of extensions.value) {
      if (!ext.settings.templates) ext.settings.templates = {}
      if (!ext.settings.triggers) ext.settings.triggers = []
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load extensions', 'error')
  } finally {
    loading.value = false
  }
}

function replaceExtension(updated: AdminExtension) {
  if (!updated.settings.templates) updated.settings.templates = {}
  if (!updated.settings.triggers) updated.settings.triggers = []
  extensions.value = extensions.value.map((e) => (e.id === updated.id ? updated : e))
}

async function save(ext: AdminExtension) {
  if (ext.status !== 'loaded') return
  busyId.value = ext.id
  try {
    const payload: AdminExtensionPatch = {
      enabled: ext.settings.enabled,
      triggers: [...(ext.settings.triggers || [])],
      templates: { ...(ext.settings.templates || {}) },
    }
    const draft = siteSecretDraft[ext.id]?.trim()
    if (draft) payload.webhook_url = draft
    const saved = await api.patchAdminExtension(ext.id, payload)
    replaceExtension(saved)
    siteSecretDraft[ext.id] = ''
    if (customFields(saved).length) clearCustomFieldDefsCache()
    toast.push('Extension settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function test(ext: AdminExtension) {
  testBusyId.value = ext.id
  try {
    const res = await api.testAdminExtension(ext.id)
    toast.push(res.message || 'Test message sent', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testBusyId.value = null
  }
}

async function retryDelivery(ext: AdminExtension, row: { id: number }) {
  busyId.value = `${ext.id}:retry:${row.id}`
  try {
    await api.retryAdminExtension(ext.id, row.id)
    toast.push('Delivery queued for retry', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Retry failed', 'error')
  } finally {
    busyId.value = null
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Extensions</h1>
    <p v-if="loading" class="text-muted">Loading…</p>
    <div v-else-if="emptyHint" class="alert alert-secondary">
      No extensions loaded. Copy a folder from <code>examples/extensions/</code> into
      <code>data/extensions/</code> and restart the server.
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
          <img
            v-if="ext.manifest.icon"
            :src="withBase(`/api/v2/extensions/${ext.id}/icon`)"
            alt=""
            width="20"
            height="20"
            class="rounded"
          />
          <span class="h5 mb-0">{{ ext.name || ext.id }}</span>
        </span>
        <span v-if="ext.status === 'loaded'" class="badge text-bg-success">Loaded {{ ext.version }}</span>
        <span v-else class="badge text-bg-danger">Failed</span>
      </button>
      <div class="card-body pt-0">
        <div v-if="ext.status !== 'loaded'" class="alert alert-danger mb-0">{{ ext.error || 'This extension failed to load.' }}</div>
        <form v-else @submit.prevent="save(ext)">
          <div class="d-flex flex-wrap align-items-center gap-3 mb-2">
            <div class="form-check mb-0">
              <input
                :id="`ext-${ext.id}-enabled`"
                v-model="ext.settings.enabled"
                class="form-check-input"
                type="checkbox"
                @click.stop
              />
              <label class="form-check-label" :for="`ext-${ext.id}-enabled`" @click.stop>Enable</label>
            </div>
            <button type="submit" class="btn btn-sm btn-primary" :disabled="busyId === ext.id" @click.stop>
              {{ busyId === ext.id ? 'Saving…' : 'Save' }}
            </button>
          </div>

          <div v-if="expanded[ext.id]">
            <p v-if="ext.manifest.description" class="small text-muted">{{ ext.manifest.description }}</p>
            <p v-if="ext.manifest.author || ext.manifest.license || ext.manifest.homepage" class="small text-muted">
              <span v-if="ext.manifest.author">{{ ext.manifest.author }}</span>
              <span v-if="ext.manifest.license"> · {{ ext.manifest.license }}</span><br />
              <span v-if="ext.manifest.homepage"><a v-if="ext.manifest.homepage" :href="ext.manifest.homepage" target="_blank" rel="noopener noreferrer">Homepage</a></span>
            </p>
            <p v-if="hasProjectSettings(ext)" class="small text-muted">
              Webhook, triggers, and message templates for project destinations are set per project in Project settings → Extensions.
            </p>
            <p v-else-if="customFields(ext).length" class="small text-muted">
              Project owners enable this per board in Project settings → Extensions. Fields then appear on that project’s tasks.
            </p>
            <p v-else class="small text-muted">
              This extension is configured here in Admin (site-wide), not on each project.
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
            <div v-for="field in siteSecretFields(ext)" :key="field.key" class="mb-3">
              <label class="form-label" :for="`ext-${ext.id}-${field.key}`">{{ field.label }}</label>
              <input
                :id="`ext-${ext.id}-${field.key}`"
                v-model="siteSecretDraft[ext.id]"
                type="password"
                class="form-control"
                autocomplete="off"
                :placeholder="ext.secrets?.[field.key] ? 'Set — leave blank to keep' : 'https://example.com/hooks/…'"
              />
              <div v-if="field.description" class="form-text">{{ field.description }}</div>
              <div v-if="ext.secrets?.[field.key]" class="form-text text-success">URL is stored.</div>
            </div>
            <div v-for="field in siteFields(ext).filter((f) => f.type !== 'secret')" :key="field.key" class="mb-3">
              <template v-if="field.type === 'hook_select'">
                <div class="fw-semibold mb-2">{{ field.label }}</div>
                <div v-if="field.description" class="form-text mb-1">{{ field.description }}</div>
                <p class="small text-muted">Empty triggers still deliver all site events. Choose “All declared events” or specific hooks to restrict them.</p>
                <div class="form-check">
                  <input
                    class="form-check-input"
                    type="checkbox"
                    :checked="(ext.settings.triggers || []).includes('*')"
                    @change="toggleTrigger(ext, '*', ($event.target as HTMLInputElement).checked)"
                  />
                  <label class="form-check-label">All declared events</label>
                </div>
                <div v-for="hook in hookNames(ext)" :key="hook" class="form-check">
                  <input
                    class="form-check-input"
                    type="checkbox"
                    :checked="triggerChecked(ext, hook)"
                    @change="toggleTrigger(ext, hook, ($event.target as HTMLInputElement).checked)"
                  />
                  <label class="form-check-label">{{ hookLabel(ext, hook) }}</label>
                </div>
              </template>
              <template v-else>
                <div class="fw-semibold">{{ field.label }}</div>
                <div v-if="field.description" class="form-text">{{ field.description }}</div>
              </template>
            </div>
            <div v-if="hookNames(ext).length && !hasProjectSettings(ext)" class="mb-3">
              <div class="fw-semibold mb-2">Messages</div>
              <div class="mb-2">
                <label class="form-label">Default message (all events)</label>
                <textarea
                  class="form-control"
                  rows="2"
                  :value="templateValue(ext, '*')"
                  @input="setTemplate(ext, '*', ($event.target as HTMLTextAreaElement).value)"
                />
              </div>
              <div v-for="hook in hookNames(ext)" :key="`tmpl-${hook}`" class="mb-2">
                <label class="form-label">{{ hookLabel(ext, hook) }}</label>
                <textarea
                  class="form-control"
                  rows="2"
                  :value="templateValue(ext, hook)"
                  @input="setTemplate(ext, hook, ($event.target as HTMLTextAreaElement).value)"
                />
              </div>
            </div>
            <div v-if="ext.settings.last_error" class="alert alert-warning">Last delivery error: {{ ext.settings.last_error }}</div>
            <div v-if="ext.deliveries?.length" class="small mb-2">
              <div class="fw-semibold">Recent deliveries</div>
              <ul class="mb-0 ps-3">
                <li v-for="row in ext.deliveries" :key="row.id">
                  {{ row.created_at }} · {{ row.event }} · {{ row.status }}
                  <span v-if="row.error" class="text-warning"> {{ row.error }}</span>
                  <button
                    v-if="row.status === 'failed' || row.status === 'dead'"
                    type="button"
                    class="btn btn-link btn-sm py-0"
                    :disabled="!!busyId"
                    @click="retryDelivery(ext, row)"
                  >Retry</button>
                </li>
              </ul>
            </div>
            <button
              v-if="hasControl(ext, 'send_test') && !hasProjectSettings(ext)"
              type="button"
              class="btn btn-sm btn-outline-secondary"
              :disabled="testBusyId === ext.id"
              @click="test(ext)"
            >
              {{ testBusyId === ext.id ? 'Sending…' : 'Send test' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
