<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectExtension, ProjectExtensionPatch } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { clearCustomFieldDefsCache } from '@/composables/useCustomFieldDefs'

const props = defineProps<{
  project: Project
}>()

const toast = useToast()
const loading = ref(false)
const extensions = ref<ProjectExtension[]>([])
const busyId = ref<string | null>(null)
const testBusyId = ref<string | null>(null)
const webhookDraft = reactive<Record<string, string>>({})
const expanded = reactive<Record<string, boolean>>({})

const isOwner = computed(() => (props.project.role || 'owner') === 'owner')

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

function templateValue(ext: ProjectExtension, hook: string): string {
  return ext.settings.templates?.[hook] ?? ext.manifest.templates?.[hook] ?? ''
}

function secretSet(ext: ProjectExtension, key: string): boolean {
  return !!ext.secrets?.[key]
}

function toggleExpanded(id: string) {
  expanded[id] = !expanded[id]
}

function applyDefaults(list: ProjectExtension[]) {
  for (const e of list) {
    if (webhookDraft[e.id] === undefined) webhookDraft[e.id] = ''
    if (!(e.settings.triggers || []).length) {
      e.settings.triggers = hookNames(e)
    }
    if (!e.settings.templates) e.settings.templates = {}
  }
}

async function load() {
  if (!isOwner.value) return
  loading.value = true
  try {
    const res = await api.listProjectExtensions(props.project.id)
    extensions.value = res.extensions
    applyDefaults(extensions.value)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load extensions', 'error')
  } finally {
    loading.value = false
  }
}

function replaceExtension(updated: ProjectExtension) {
  extensions.value = extensions.value.map((e) => (e.id === updated.id ? updated : e))
  webhookDraft[updated.id] = ''
  applyDefaults(extensions.value)
}

async function save(ext: ProjectExtension) {
  if (!ext.site_enabled) return
  busyId.value = ext.id
  try {
    const payload: ProjectExtensionPatch = {
      enabled: ext.settings.enabled,
      triggers: [...(ext.settings.triggers || [])],
      templates: { ...(ext.settings.templates || {}) },
      status_only: ext.settings.status_only,
    }
    const draft = webhookDraft[ext.id]?.trim()
    if (draft) payload.webhook_url = draft
    const saved = await api.patchProjectExtension(props.project.id, ext.id, payload)
    replaceExtension(saved)
    if (customFields(saved).length) clearCustomFieldDefsCache(props.project.id)
    toast.push('Extension settings saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    busyId.value = null
  }
}

async function test(ext: ProjectExtension) {
  testBusyId.value = ext.id
  try {
    const res = await api.testProjectExtension(props.project.id, ext.id)
    toast.push(res.message || 'Test message sent', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testBusyId.value = null
  }
}

function toggleTrigger(ext: ProjectExtension, hook: string, checked: boolean) {
  const cur = new Set(ext.settings.triggers || [])
  if (checked) cur.add(hook)
  else cur.delete(hook)
  ext.settings.triggers = [...cur]
}

function setTemplate(ext: ProjectExtension, hook: string, value: string) {
  if (!ext.settings.templates) ext.settings.templates = {}
  ext.settings.templates[hook] = value
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
      No project extensions are loaded. A site admin can copy
      <code>examples/extensions/discord</code>, <code>examples/extensions/severity</code>, or
      <code>examples/extensions/fields-demo</code> into <code>data/extensions/</code> and restart the server.
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
        <fieldset :disabled="!ext.site_enabled">
          <form @submit.prevent="save(ext)">
            <div class="d-flex flex-wrap align-items-center gap-3 mb-2">
              <div class="form-check mb-0">
                <input
                  :id="`proj-ext-${ext.id}-enabled`"
                  v-model="ext.settings.enabled"
                  class="form-check-input"
                  type="checkbox"
                  @click.stop
                />
                <label class="form-check-label" :for="`proj-ext-${ext.id}-enabled`" @click.stop>Enable for this project</label>
              </div>
              <button type="submit" class="btn btn-sm btn-primary" :disabled="busyId === ext.id" @click.stop>
                {{ busyId === ext.id ? 'Saving…' : 'Save' }}
              </button>
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
                <p class="form-text mb-0 mt-2">
                  After enabling, these fields appear on this project’s tasks. Open a task to edit them.
                </p>
              </div>

              <div v-for="field in projectFields(ext)" :key="field.key" class="mb-3">
                <template v-if="field.type === 'secret'">
                  <label class="form-label" :for="`proj-ext-${ext.id}-${field.key}`">{{ field.label }}</label>
                  <input
                    :id="`proj-ext-${ext.id}-${field.key}`"
                    v-model="webhookDraft[ext.id]"
                    type="password"
                    class="form-control"
                    autocomplete="off"
                    :placeholder="secretSet(ext, field.key) ? 'Set — leave blank to keep' : 'https://discord.com/api/webhooks/…'"
                  />
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                  <div v-if="secretSet(ext, field.key)" class="form-text text-success">Webhook URL is stored.</div>
                </template>

                <template v-else-if="field.type === 'bool' && field.key === 'status_only'">
                  <div class="form-check">
                    <input
                      :id="`proj-ext-${ext.id}-${field.key}`"
                      v-model="ext.settings.status_only"
                      class="form-check-input"
                      type="checkbox"
                    />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-${field.key}`">{{ field.label }}</label>
                  </div>
                  <div v-if="field.description" class="form-text">{{ field.description }}</div>
                </template>

                <template v-else-if="field.type === 'hook_select'">
                  <div class="fw-semibold mb-2">{{ field.label }}</div>
                  <div v-if="field.description" class="form-text mb-2">{{ field.description }}</div>
                  <div v-for="hook in hookNames(ext)" :key="hook" class="form-check">
                    <input
                      :id="`proj-ext-${ext.id}-hook-${hook}`"
                      class="form-check-input"
                      type="checkbox"
                      :checked="(ext.settings.triggers || []).includes(hook)"
                      @change="toggleTrigger(ext, hook, ($event.target as HTMLInputElement).checked)"
                    />
                    <label class="form-check-label" :for="`proj-ext-${ext.id}-hook-${hook}`">{{ hook }}</label>
                  </div>
                </template>
              </div>

              <div v-if="hookNames(ext).length" class="mb-3">
                <div class="fw-semibold mb-2">Messages</div>
                <p class="small text-muted">
                  Tokens: <code>{task}</code> <code>{name}</code> <code>{status}</code> <code>{old_status}</code>
                  <code>{project}</code> <code>{actor}</code> <code>{url}</code> <code>{id}</code> <code>{priority}</code>
                </p>
                <div v-for="hook in hookNames(ext)" :key="`tmpl-${hook}`" class="mb-2">
                  <label class="form-label" :for="`proj-ext-${ext.id}-tmpl-${hook}`">{{ hook }}</label>
                  <textarea
                    :id="`proj-ext-${ext.id}-tmpl-${hook}`"
                    class="form-control"
                    rows="2"
                    :value="templateValue(ext, hook)"
                    @input="setTemplate(ext, hook, ($event.target as HTMLTextAreaElement).value)"
                  />
                </div>
              </div>

              <div v-if="isHookExtension(ext) && ext.settings.last_error" class="alert alert-warning">
                Last delivery error: {{ ext.settings.last_error }}
                <div v-if="ext.settings.last_delivery_at" class="small">{{ ext.settings.last_delivery_at }}</div>
              </div>
              <p v-else-if="isHookExtension(ext) && ext.settings.last_delivery_at" class="small text-muted">
                Last delivery: {{ ext.settings.last_delivery_at }}
              </p>

              <button
                v-if="isHookExtension(ext)"
                type="button"
                class="btn btn-outline-secondary"
                :disabled="testBusyId === ext.id"
                @click="test(ext)"
              >
                {{ testBusyId === ext.id ? 'Sending…' : 'Send test' }}
              </button>
            </div>
          </form>
        </fieldset>
      </div>
    </div>
  </div>
</template>
