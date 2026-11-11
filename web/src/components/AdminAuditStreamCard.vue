<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { AuditStreamSettings } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

// Admin → Audit log: forward events to a SIEM as they happen.
const { push } = useToast()
const settings = ref<AuditStreamSettings | null>(null)
const open = ref(false)
const saving = ref(false)
const testing = ref(false)
const form = reactive({ enabled: false, url: '', secret: '', auth_header: '', include_project: false, include_task: false })

function apply(s: AuditStreamSettings) {
  settings.value = s
  form.enabled = s.enabled
  form.url = s.url
  form.secret = ''
  form.auth_header = ''
  form.include_project = s.include_project
  form.include_task = s.include_task
  open.value = open.value || s.enabled
}

onMounted(async () => {
  try {
    apply(await api.getAuditStream())
  } catch {
    settings.value = null
  }
})

async function save() {
  saving.value = true
  try {
    apply(
      await api.updateAuditStream({
        enabled: form.enabled,
        url: form.url,
        include_project: form.include_project,
        include_task: form.include_task,
        ...(form.secret ? { secret: form.secret } : {}),
        ...(form.auth_header ? { auth_header: form.auth_header } : {}),
      }),
    )
    push('Audit streaming saved', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    saving.value = false
  }
}

async function test() {
  testing.value = true
  try {
    await api.testAuditStream()
    push('Test event delivered', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Test failed', 'error')
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <div v-if="settings" class="card mb-3">
    <div class="card-header d-flex align-items-center gap-2">
      <button type="button" class="btn btn-link p-0 text-decoration-none fw-semibold" :aria-expanded="open" @click="open = !open">
        <i class="bi" :class="open ? 'bi-chevron-down' : 'bi-chevron-right'" aria-hidden="true" /> Stream to a SIEM
      </button>
      <span v-if="settings.enabled" class="badge text-bg-success">On</span>
      <span v-if="settings.last_error" class="badge text-bg-danger" :title="settings.last_error">Failing</span>
    </div>
    <form v-if="open" class="card-body" @submit.prevent="save">
      <p class="small text-muted">
        New audit events are POSTed as JSON batches (<code>{"site", "events": [...]}</code>) about every 15 seconds,
        signed with <code>X-Ordryn-Signature: sha256=HMAC(secret, timestamp + "." + body)</code>. Works with Splunk HEC,
        Datadog, Elastic, or any HTTPS collector. Failed batches are retried in order; nothing is skipped.
      </p>
      <div class="mb-2">
        <label class="form-label small" for="audit-stream-url">Endpoint (https)</label>
        <input id="audit-stream-url" v-model="form.url" class="form-control form-control-sm" placeholder="https://siem.example.com/collector" />
      </div>
      <div class="row g-2 mb-2">
        <div class="col-md-6">
          <label class="form-label small" for="audit-stream-secret">Signing secret</label>
          <input id="audit-stream-secret" v-model="form.secret" type="password" autocomplete="new-password" class="form-control form-control-sm"
            :placeholder="settings.secret_set ? 'Saved; type to replace' : 'Optional'" />
        </div>
        <div class="col-md-6">
          <label class="form-label small" for="audit-stream-auth">Authorization header</label>
          <input id="audit-stream-auth" v-model="form.auth_header" type="password" autocomplete="off" class="form-control form-control-sm"
            :placeholder="settings.auth_header_set ? 'Saved; type to replace' : 'Optional, e.g. Splunk <token>'" />
        </div>
      </div>
      <div class="form-check">
        <input id="audit-stream-project" v-model="form.include_project" type="checkbox" class="form-check-input" />
        <label class="form-check-label small" for="audit-stream-project">Also send project events (membership, settings, sharing)</label>
      </div>
      <div class="form-check mb-2">
        <input id="audit-stream-task" v-model="form.include_task" type="checkbox" class="form-check-input" />
        <label class="form-check-label small" for="audit-stream-task">Also send task history (busy sites: many events)</label>
      </div>
      <div class="form-check form-switch mb-2">
        <input id="audit-stream-enabled" v-model="form.enabled" type="checkbox" class="form-check-input" role="switch" />
        <label class="form-check-label small fw-semibold" for="audit-stream-enabled">Streaming is on</label>
      </div>
      <p v-if="settings.last_success_at" class="small text-muted mb-1">Last delivered {{ new Date(settings.last_success_at).toLocaleString() }}.</p>
      <p v-if="settings.last_error" class="small text-danger mb-2">Last error: {{ settings.last_error }}</p>
      <div class="d-flex gap-2">
        <button type="submit" class="btn btn-sm btn-primary" :disabled="saving">{{ saving ? 'Saving…' : 'Save' }}</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="testing || !settings.url" @click="test">
          {{ testing ? 'Sending…' : 'Send test event' }}
        </button>
      </div>
    </form>
  </div>
</template>
