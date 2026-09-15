<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AdminMod, AdminModPatch, AdminModSettingField } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import AdminSubnav from '@/components/AdminSubnav.vue'

const toast = useToast()
const loading = ref(false)
const mods = ref<AdminMod[]>([])
const busyId = ref<string | null>(null)
const expanded = reactive<Record<string, boolean>>({})

const emptyHint = computed(() => mods.value.length === 0)

function siteFields(mod: AdminMod): AdminModSettingField[] {
  return (mod.manifest.settings || []).filter((f) => !f.scope || f.scope === 'site')
}

function hasProjectSettings(mod: AdminMod): boolean {
  return (mod.manifest.settings || []).some((f) => f.scope === 'project')
}

function toggleExpanded(id: string) {
  expanded[id] = !expanded[id]
}

async function load() {
  loading.value = true
  try {
    const res = await api.listAdminMods()
    mods.value = res.mods
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load mods', 'error')
  } finally {
    loading.value = false
  }
}

function replaceMod(updated: AdminMod) {
  mods.value = mods.value.map((m) => (m.id === updated.id ? updated : m))
}

async function save(mod: AdminMod) {
  if (mod.status !== 'loaded') return
  busyId.value = mod.id
  try {
    const payload: AdminModPatch = { enabled: mod.settings.enabled }
    const saved = await api.patchAdminMod(mod.id, payload)
    replaceMod(saved)
    toast.push('Mod settings saved', 'success')
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
    <h1>Mods</h1>
    <p class="text-muted">
      Drop a folder in <code>data/mods/</code> with <code>manifest.json</code>, then restart.
      Copy <code>examples/mods/discord</code> to enable Discord notifications. Channel, triggers,
      and messages are configured by project owners under Project settings → Mods.
    </p>

    <p v-if="loading" class="text-muted">Loading…</p>
    <div v-else-if="emptyHint" class="alert alert-secondary">
      No mods loaded. Copy <code>examples/mods/discord</code> to <code>data/mods/discord</code> and restart the server.
    </div>

    <div v-for="mod in mods" :key="mod.id" class="card mb-3">
      <button
        type="button"
        class="card-header d-flex flex-wrap align-items-center justify-content-between gap-2 text-start border-0 bg-transparent w-100"
        :aria-expanded="!!expanded[mod.id]"
        @click="toggleExpanded(mod.id)"
      >
        <span class="d-flex align-items-center gap-2">
          <i class="bi" :class="expanded[mod.id] ? 'bi-chevron-down' : 'bi-chevron-right'" aria-hidden="true" />
          <span class="h5 mb-0">{{ mod.name || mod.id }}</span>
        </span>
        <span v-if="mod.status === 'loaded'" class="badge text-bg-success">Loaded {{ mod.version }}</span>
        <span v-else class="badge text-bg-danger">Failed</span>
      </button>
      <div class="card-body pt-0">
        <div v-if="mod.status !== 'loaded'" class="alert alert-danger mb-0">{{ mod.error || 'This mod failed to load.' }}</div>
        <form v-else @submit.prevent="save(mod)">
          <div class="d-flex flex-wrap align-items-center gap-3 mb-2">
            <div class="form-check mb-0">
              <input
                :id="`mod-${mod.id}-enabled`"
                v-model="mod.settings.enabled"
                class="form-check-input"
                type="checkbox"
                @click.stop
              />
              <label class="form-check-label" :for="`mod-${mod.id}-enabled`" @click.stop>Enable</label>
            </div>
            <button type="submit" class="btn btn-sm btn-primary" :disabled="busyId === mod.id" @click.stop>
              {{ busyId === mod.id ? 'Saving…' : 'Save' }}
            </button>
          </div>

          <div v-if="expanded[mod.id]">
            <p v-if="hasProjectSettings(mod)" class="small text-muted">
              Webhook, triggers, and message templates are set per project in Project settings → Mods.
            </p>
            <div v-for="field in siteFields(mod)" :key="field.key" class="mb-3">
              <div class="fw-semibold">{{ field.label }}</div>
              <div class="form-text">Site setting ({{ field.type }}).</div>
            </div>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
