<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AdminExtension, AdminExtensionPatch, ExtensionSettingField } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'

const toast = useToast()
const loading = ref(false)
const extensions = ref<AdminExtension[]>([])
const busyId = ref<string | null>(null)
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

function toggleExpanded(id: string) {
  expanded[id] = !expanded[id]
}

async function load() {
  loading.value = true
  try {
    const res = await api.listAdminExtensions()
    extensions.value = res.extensions
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load extensions', 'error')
  } finally {
    loading.value = false
  }
}

function replaceExtension(updated: AdminExtension) {
  extensions.value = extensions.value.map((e) => (e.id === updated.id ? updated : e))
}

async function save(ext: AdminExtension) {
  if (ext.status !== 'loaded') return
  busyId.value = ext.id
  try {
    const payload: AdminExtensionPatch = { enabled: ext.settings.enabled }
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

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Extensions</h1>
    <p class="text-muted">
      Drop a folder in <code>data/extensions/</code> with <code>manifest.json</code>, then restart.
      Copy a notification example from <code>examples/extensions/</code> (Discord, Slack, Teams,
      Google Chat, generic webhook, ntfy, email relay, due-dates, comments, claimed, activity,
      join-requests), or
      <code>severity</code> / <code>estimate</code> / <code>fields-demo</code> for custom fields.
    </p>

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
            <p v-if="hasProjectSettings(ext)" class="small text-muted">
              Webhook, triggers, and message templates are set per project in Project settings → Extensions.
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
              <div class="fw-semibold">{{ field.label }}</div>
              <div v-if="field.description" class="form-text">{{ field.description }}</div>
            </div>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
