<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { EmailAuditList } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import AdminSubnav from '@/components/AdminSubnav.vue'

const toast = useToast()
const loading = ref(false)
const result = ref<EmailAuditList>({ items: [], total: 0, limit: 50, offset: 0 })

const status = ref('failed')
const trigger = ref('')
const emailQuery = ref('')
const fromDate = ref('')
const toDate = ref('')
const page = ref(0)
const pageSize = 50

const triggerOptions = [
  { value: '', label: 'All triggers' },
  { value: 'password_reset', label: 'Password reset' },
  { value: 'password_changed', label: 'Password changed' },
  { value: 'site_invite', label: 'Site invite' },
  { value: 'join_request', label: 'Join request' },
  { value: 'project_invite', label: 'Project invite' },
] as const

const totalPages = computed(() => Math.max(1, Math.ceil(result.value.total / pageSize)))
const pageLabel = computed(() => {
  if (result.value.total === 0) return 'No rows'
  const start = result.value.offset + 1
  const end = result.value.offset + result.value.items.length
  return `${start}–${end} of ${result.value.total}`
})

function triggerLabel(value: string) {
  return triggerOptions.find((opt) => opt.value === value)?.label || value
}

function statusBadge(value: string) {
  if (value === 'failed') return 'bg-danger'
  if (value === 'not_configured') return 'bg-warning text-dark'
  if (value === 'rate_limited') return 'bg-info text-dark'
  if (value === 'sent') return 'bg-success'
  return 'bg-secondary'
}

function statusLabel(value: string) {
  if (value === 'not_configured') return 'Not configured'
  if (value === 'rate_limited') return 'Rate limited'
  if (value === 'failed') return 'Failed'
  if (value === 'sent') return 'Sent'
  return value
}

function formatWhen(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    result.value = await api.listAdminEmailAudit({
      status: status.value,
      trigger: trigger.value,
      q: emailQuery.value.trim(),
      from: fromDate.value,
      to: toDate.value,
      limit: pageSize,
      offset: page.value * pageSize,
    })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load email log', 'error')
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 0
  void load()
}

function prevPage() {
  if (page.value <= 0) return
  page.value -= 1
  void load()
}

function nextPage() {
  if (page.value + 1 >= totalPages.value) return
  page.value += 1
  void load()
}

watch([status, trigger], applyFilters)

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Email log</h1>
    <p class="text-muted">
      Outbound email attempts for the current retention window. Message bodies are not stored.
    </p>

    <form class="row g-3 align-items-end mb-3" @submit.prevent="applyFilters">
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="email-audit-status">Status</label>
        <select id="email-audit-status" v-model="status" class="form-select">
          <option value="failed">Failed</option>
          <option value="not_configured">Not configured</option>
          <option value="rate_limited">Rate limited</option>
          <option value="sent">Sent</option>
          <option value="">All</option>
        </select>
      </div>
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="email-audit-trigger">Trigger</label>
        <select id="email-audit-trigger" v-model="trigger" class="form-select">
          <option v-for="opt in triggerOptions" :key="opt.value || 'all'" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="email-audit-q">Recipient</label>
        <input
          id="email-audit-q"
          v-model="emailQuery"
          type="search"
          class="form-control"
          placeholder="email substring"
          autocomplete="off"
        />
      </div>
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="email-audit-from">From</label>
        <input id="email-audit-from" v-model="fromDate" type="date" class="form-control" />
      </div>
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="email-audit-to">To</label>
        <input id="email-audit-to" v-model="toDate" type="date" class="form-control" />
      </div>
      <div class="col-sm-6 col-md-3">
        <button type="submit" class="btn btn-primary" :disabled="loading">
          {{ loading ? 'Loading…' : 'Search' }}
        </button>
      </div>
    </form>

    <div class="card">
      <div class="table-responsive">
        <table class="table table-hover mb-0">
          <thead>
            <tr>
              <th>When</th>
              <th>Trigger</th>
              <th>Recipient</th>
              <th>Status</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!loading && result.items.length === 0">
              <td colspan="5" class="text-muted py-4 text-center">No matching email attempts.</td>
            </tr>
            <tr v-for="row in result.items" :key="row.id">
              <td class="text-nowrap small">{{ formatWhen(row.created_at) }}</td>
              <td>{{ triggerLabel(row.trigger) }}</td>
              <td>{{ row.to_email || '—' }}</td>
              <td>
                <span class="badge" :class="statusBadge(row.status)">{{ statusLabel(row.status) }}</span>
                <div v-if="row.provider" class="text-muted small">{{ row.provider }}</div>
              </td>
              <td class="small" style="max-width: 28rem; white-space: pre-wrap; word-break: break-word">
                {{ row.error || '—' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="card-footer d-flex justify-content-between align-items-center gap-2">
        <span class="text-muted small">{{ pageLabel }}</span>
        <div class="btn-group btn-group-sm">
          <button type="button" class="btn btn-outline-secondary" :disabled="page <= 0 || loading" @click="prevPage">
            Previous
          </button>
          <button
            type="button"
            class="btn btn-outline-secondary"
            :disabled="page + 1 >= totalPages || loading"
            @click="nextPage"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
