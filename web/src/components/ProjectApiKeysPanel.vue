<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectAPIKey } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import {
  EXPIRY_PRESETS,
  PROJECT_API_KEY_SCOPES,
  expiresAtFromPreset,
  keyExpiryLabel,
  scopeLabel,
  type ExpiryPreset,
  type ProjectAPIKeyScope,
} from '@/utils/projectApiKeys'

const props = defineProps<{
  project: Project
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const keys = ref<ProjectAPIKey[]>([])
const loading = ref(false)
const creating = ref(false)
const apiEnabled = ref(true)
const keyName = ref('')
const scopes = ref<ProjectAPIKeyScope[]>(['tasks:read'])
const expiry = ref<ExpiryPreset>('90')
const minted = ref<{ name: string; key: string } | null>(null)

const canCreate = computed(() => !!keyName.value.trim() && scopes.value.length > 0 && !creating.value)

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

async function load() {
  loading.value = true
  try {
    const [list, health] = await Promise.all([
      api.listProjectAPIKeys(props.project.id),
      api.health().catch(() => null),
    ])
    keys.value = list
    apiEnabled.value = health ? health.api_enabled : true
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load API keys', 'error')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    minted.value = null
    void load()
  },
  { immediate: true },
)

async function createKey() {
  if (!canCreate.value) return
  creating.value = true
  try {
    const created = await api.createProjectAPIKey(props.project.id, {
      name: keyName.value.trim(),
      scopes: scopes.value,
      expires_at: expiresAtFromPreset(expiry.value),
    })
    minted.value = { name: created.name, key: created.key }
    keyName.value = ''
    toast.push('API key created — copy it now', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  } finally {
    creating.value = false
  }
}

async function copyMinted() {
  if (!minted.value) return
  try {
    await navigator.clipboard.writeText(minted.value.key)
    toast.push('Copied', 'success')
  } catch {
    toast.push('Copy failed — select the key and copy it manually', 'error')
  }
}

async function revokeKey(key: ProjectAPIKey) {
  const ok = await askConfirm({
    title: 'Revoke API key?',
    message: `Revoke “${key.name}”? Scripts and integrations using it will stop working.`,
    confirmLabel: 'Revoke',
    danger: true,
  })
  if (!ok) return
  try {
    await api.revokeProjectAPIKey(props.project.id, key.id)
    if (minted.value?.name === key.name) minted.value = null
    toast.push('API key revoked', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Revoke failed', 'error')
  }
}
</script>

<template>
  <div>
    <h4 class="h6 mb-2">API keys</h4>
    <p class="small text-muted mb-3">
      Keys for scripts and tools like Zapier or n8n. A key only works inside this project, only for the scopes you
      pick, and acts as you. It stops working if you lose manage access to this project.
    </p>
    <div v-if="!apiEnabled" class="alert alert-warning small py-2">
      The REST API is turned off for this site, so these keys won't work until an administrator enables it.
    </div>

    <form class="mb-3" @submit.prevent="createKey">
      <div class="row g-2 mb-2">
        <div class="col-sm-8">
          <label class="form-label small mb-1" for="project-key-name">Name</label>
          <input
            id="project-key-name"
            v-model="keyName"
            type="text"
            class="form-control form-control-sm"
            placeholder="e.g. Zapier intake"
            maxlength="80"
            required
          />
        </div>
        <div class="col-sm-4">
          <label class="form-label small mb-1" for="project-key-expiry">Expires</label>
          <select id="project-key-expiry" v-model="expiry" class="form-select form-select-sm">
            <option v-for="p in EXPIRY_PRESETS" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </div>
      </div>
      <fieldset class="mb-2">
        <legend class="form-label small mb-1">Scopes</legend>
        <div v-for="s in PROJECT_API_KEY_SCOPES" :key="s.id" class="form-check">
          <input :id="`project-key-scope-${s.id}`" v-model="scopes" class="form-check-input" type="checkbox" :value="s.id" />
          <label class="form-check-label small" :for="`project-key-scope-${s.id}`">
            <code>{{ s.id }}</code> — {{ s.help }}
          </label>
        </div>
      </fieldset>
      <button type="submit" class="btn btn-sm btn-primary" :disabled="!canCreate">Create key</button>
    </form>

    <div v-if="minted" class="alert alert-success small">
      <p class="fw-semibold mb-1">New key “{{ minted.name }}” — shown once</p>
      <div class="d-flex gap-2 align-items-center">
        <code class="text-break flex-grow-1">{{ minted.key }}</code>
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="copyMinted">Copy</button>
      </div>
    </div>

    <p v-if="loading" class="small text-muted mb-0">Loading keys…</p>
    <ul v-else class="list-group">
      <li v-for="key in keys" :key="key.id" class="list-group-item">
        <div class="d-flex flex-wrap gap-2 align-items-center mb-1">
          <span class="fw-semibold">{{ key.name }}</span>
          <span v-if="key.expired" class="badge text-bg-danger">Expired</span>
          <span class="flex-grow-1" />
          <button type="button" class="btn btn-sm btn-outline-danger" @click="revokeKey(key)">Revoke</button>
        </div>
        <div class="d-flex flex-wrap gap-1 mb-1">
          <span v-for="s in key.scopes" :key="s" class="badge text-bg-secondary" :title="s">{{ scopeLabel(s) }}</span>
        </div>
        <div class="small text-muted">
          <code>{{ key.key_prefix }}</code>
          · Created {{ formatTime(key.created_at) }}<template v-if="key.created_by"> by {{ key.created_by }}</template>
          · Last used {{ key.last_used_at ? formatTime(key.last_used_at) : 'never' }}
          · Expires: {{ keyExpiryLabel(key.expires_at) }}
        </div>
      </li>
      <li v-if="!keys.length" class="list-group-item text-muted">No API keys for this project.</li>
    </ul>
  </div>
</template>
