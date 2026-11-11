<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'

// Admin → Settings: switch the site to read-only for upgrades and migrations.
const { push } = useToast()
const { askConfirm } = useConfirm()
const { refresh: refreshSite } = useSite()
const enabled = ref(false)
const message = ref('')
const startedAt = ref<string | null>(null)
const saving = ref(false)

onMounted(async () => {
  try {
    const m = await api.getMaintenance()
    enabled.value = m.enabled
    message.value = m.message
    startedAt.value = m.started_at
  } catch {
    // Not an admin, or the endpoint is unavailable: hide nothing, the toggle just fails.
  }
})

async function save(next: boolean) {
  if (next) {
    const ok = await askConfirm({
      title: 'Turn on maintenance mode?',
      message: 'Everyone except admins will be able to read but not change anything until you turn it off. Background jobs keep running.',
      confirmLabel: 'Turn on',
      warning: true,
    })
    if (!ok) return
  }
  saving.value = true
  try {
    const m = await api.setMaintenance(next, message.value)
    enabled.value = m.enabled
    startedAt.value = m.started_at
    await refreshSite()
    push(next ? 'Maintenance mode is on' : 'Maintenance mode is off', next ? 'info' : 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card mb-4" :class="{ 'border-warning': enabled }">
    <div class="card-header d-flex align-items-center gap-2">
      <h2 class="h5 mb-0">Maintenance mode</h2>
      <span v-if="enabled" class="badge text-bg-warning">On</span>
    </div>
    <div class="card-body">
      <p class="small text-muted">
        Makes the site read-only for everyone but admins (for upgrades, migrations, or restores). People can still sign
        in and read; anything else gets a "try again shortly" message and a banner explains why.
      </p>
      <label class="form-label small" for="maintenance-message">Banner message</label>
      <input
        id="maintenance-message"
        v-model="message"
        class="form-control mb-2"
        maxlength="500"
        placeholder="Upgrading to the new version. Back in about 10 minutes."
      />
      <p v-if="enabled && startedAt" class="small text-muted mb-2">On since {{ new Date(startedAt).toLocaleString() }}.</p>
      <div class="d-flex gap-2">
        <button v-if="!enabled" type="button" class="btn btn-warning btn-sm" :disabled="saving" @click="save(true)">Turn on</button>
        <template v-else>
          <button type="button" class="btn btn-success btn-sm" :disabled="saving" @click="save(false)">Turn off</button>
          <button type="button" class="btn btn-outline-secondary btn-sm" :disabled="saving" @click="save(true)">Update message</button>
        </template>
      </div>
    </div>
  </div>
</template>
