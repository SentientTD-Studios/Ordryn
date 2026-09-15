<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectMod, ProjectModPatch } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  project: Project
}>()

const toast = useToast()
const loading = ref(false)
const mods = ref<ProjectMod[]>([])
const busyId = ref<string | null>(null)
const testBusyId = ref<string | null>(null)
const webhookDraft = reactive<Record<string, string>>({})

const isOwner = computed(() => (props.project.role || 'owner') === 'owner')

function hookNames(mod: ProjectMod): string[] {
  return (mod.manifest.hooks || []).map((h) => h.on)
}

function projectFields(mod: ProjectMod) {
  return (mod.manifest.settings || []).filter((f) => f.scope === 'project')
}

function templateValue(mod: ProjectMod, hook: string): string {
  return mod.settings.templates?.[hook] ?? mod.manifest.templates?.[hook] ?? ''
}

function secretSet(mod: ProjectMod, key: string): boolean {
  return !!mod.secrets?.[key]
}

function applyDefaults(list: ProjectMod[]) {
  for (const m of list) {
    if (webhookDraft[m.id] === undefined) webhookDraft[m.id] = ''
    if (!(m.settings.triggers || []).length) {
      m.settings.triggers = hookNames(m)
    }
    if (!m.settings.templates) m.settings.templates = {}
  }
}

async function load() {
  if (!isOwner.value) return
  loading.value = true
  try {
    const res = await api.listProjectMods(props.project.id)
    mods.value = res.mods
    applyDefaults(mods.value)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load mods', 'error')
  } finally {
    loading.value = false
  }
}

function replaceMod(updated: ProjectMod) {
  mods.value = mods.value.map((m) => (m.id === updated.id ? updated : m))
  webhookDraft[updated.id] = ''
  applyDefaults(mods.value)
}

async function save(mod: ProjectMod) {
  if (!mod.site_enabled) return
  busyId.value = mod.id
  try {
    const payload: ProjectModPatch = {
      enabled: mod.settings.enabled,
      triggers: [...(mod.settings.triggers || [])],
      templates: { ...(mod.settings.templates || {}) },
      status_only: mod.settings.status_only,
    }
    const draft = webhookDraft[mod.id]?.trim()
    if (draft) payload.webhook_url = draft
    const saved = await api.patchProjectMod(props.project.id, mod.id, payload)
    replaceMod(saved)
    toast.push('Mod settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function test(mod: ProjectMod) {
  testBusyId.value = mod.id
  try {
    const res = await api.testProjectMod(props.project.id, mod.id)
    toast.push(res.message || 'Test message sent', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testBusyId.value = null
  }
}

function toggleTrigger(mod: ProjectMod, hook: string, checked: boolean) {
  const cur = new Set(mod.settings.triggers || [])
  if (checked) cur.add(hook)
  else cur.delete(hook)
  mod.settings.triggers = [...cur]
}

function setTemplate(mod: ProjectMod, hook: string, value: string) {
  if (!mod.settings.templates) mod.settings.templates = {}
  mod.settings.templates[hook] = value
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
    <p v-if="loading" class="text-muted small mb-0">Loading mods…</p>
    <div v-else-if="!mods.length" class="alert alert-secondary mb-0">
      No project mods are loaded. A site admin can copy <code>examples/mods/discord</code> to
      <code>data/mods/discord</code> and restart the server.
    </div>

    <div v-for="mod in mods" :key="mod.id" class="card mb-3">
      <div class="card-header d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h3 class="h6 mb-0">{{ mod.name || mod.id }}</h3>
        <span v-if="mod.site_enabled" class="badge text-bg-success">Site enabled</span>
        <span v-else class="badge text-bg-secondary">Site disabled</span>
      </div>
      <div class="card-body">
        <div v-if="!mod.site_enabled" class="alert alert-warning">
          A site admin must enable this mod in Admin → Mods before this project can send notifications.
        </div>
        <fieldset :disabled="!mod.site_enabled">
          <form @submit.prevent="save(mod)">
            <div class="form-check mb-3">
              <input
                :id="`proj-mod-${mod.id}-enabled`"
                v-model="mod.settings.enabled"
                class="form-check-input"
                type="checkbox"
              />
              <label class="form-check-label" :for="`proj-mod-${mod.id}-enabled`">Enable for this project</label>
            </div>

            <div v-for="field in projectFields(mod)" :key="field.key" class="mb-3">
              <template v-if="field.type === 'secret'">
                <label class="form-label" :for="`proj-mod-${mod.id}-${field.key}`">{{ field.label }}</label>
                <input
                  :id="`proj-mod-${mod.id}-${field.key}`"
                  v-model="webhookDraft[mod.id]"
                  type="password"
                  class="form-control"
                  autocomplete="off"
                  :placeholder="secretSet(mod, field.key) ? 'Set — leave blank to keep' : 'https://discord.com/api/webhooks/…'"
                />
                <div v-if="secretSet(mod, field.key)" class="form-text text-success">Webhook URL is stored.</div>
              </template>

              <template v-else-if="field.type === 'bool' && field.key === 'status_only'">
                <div class="form-check">
                  <input
                    :id="`proj-mod-${mod.id}-${field.key}`"
                    v-model="mod.settings.status_only"
                    class="form-check-input"
                    type="checkbox"
                  />
                  <label class="form-check-label" :for="`proj-mod-${mod.id}-${field.key}`">{{ field.label }}</label>
                </div>
              </template>

              <template v-else-if="field.type === 'hook_select'">
                <div class="fw-semibold mb-2">{{ field.label }}</div>
                <div v-for="hook in hookNames(mod)" :key="hook" class="form-check">
                  <input
                    :id="`proj-mod-${mod.id}-hook-${hook}`"
                    class="form-check-input"
                    type="checkbox"
                    :checked="(mod.settings.triggers || []).includes(hook)"
                    @change="toggleTrigger(mod, hook, ($event.target as HTMLInputElement).checked)"
                  />
                  <label class="form-check-label" :for="`proj-mod-${mod.id}-hook-${hook}`">{{ hook }}</label>
                </div>
              </template>
            </div>

            <div v-if="hookNames(mod).length" class="mb-3">
              <div class="fw-semibold mb-2">Messages</div>
              <p class="small text-muted">
                Tokens: <code>{task}</code> <code>{name}</code> <code>{status}</code> <code>{old_status}</code>
                <code>{project}</code> <code>{actor}</code> <code>{url}</code> <code>{id}</code> <code>{priority}</code>
              </p>
              <div v-for="hook in hookNames(mod)" :key="`tmpl-${hook}`" class="mb-2">
                <label class="form-label" :for="`proj-mod-${mod.id}-tmpl-${hook}`">{{ hook }}</label>
                <textarea
                  :id="`proj-mod-${mod.id}-tmpl-${hook}`"
                  class="form-control"
                  rows="2"
                  :value="templateValue(mod, hook)"
                  @input="setTemplate(mod, hook, ($event.target as HTMLTextAreaElement).value)"
                />
              </div>
            </div>

            <div v-if="mod.settings.last_error" class="alert alert-warning">
              Last delivery error: {{ mod.settings.last_error }}
              <div v-if="mod.settings.last_delivery_at" class="small">{{ mod.settings.last_delivery_at }}</div>
            </div>
            <p v-else-if="mod.settings.last_delivery_at" class="small text-muted">
              Last delivery: {{ mod.settings.last_delivery_at }}
            </p>

            <div class="d-flex flex-wrap gap-2">
              <button type="submit" class="btn btn-primary" :disabled="busyId === mod.id">
                {{ busyId === mod.id ? 'Saving…' : 'Save' }}
              </button>
              <button type="button" class="btn btn-outline-secondary" :disabled="testBusyId === mod.id" @click="test(mod)">
                {{ testBusyId === mod.id ? 'Sending…' : 'Send test' }}
              </button>
            </div>
          </form>
        </fieldset>
      </div>
    </div>
  </div>
</template>
