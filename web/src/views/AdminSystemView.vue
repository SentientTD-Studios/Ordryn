<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { SystemHealth } from '@/api/types'
import { APIError } from '@/api/types'
import AdminSubnav from '@/components/AdminSubnav.vue'
import {
  formatBytes,
  formatInterval,
  formatMs,
  formatSeconds,
  sortWorkers,
  timeAgo,
  workerLabel,
  workerStatusBadge,
} from '@/utils/systemHealth'

// Admin → System: a point-in-time look at the database pool, Redis, the server
// process, and the background workers. Backups, the audit stream, and
// automation rules show their own status on their pages.
const health = ref<SystemHealth | null>(null)
const loading = ref(false)
const error = ref('')

const workers = computed(() => (health.value ? sortWorkers(health.value.workers) : []))
const attentionCount = computed(
  () => workers.value.filter((w) => w.status === 'error' || w.status === 'late' || w.status === 'stuck').length,
)
const poolPercent = computed(() => {
  const db = health.value?.database
  if (!db || !db.max_conns) return 0
  return Math.round((db.acquired_conns / db.max_conns) * 100)
})
const poolBarClass = computed(() => {
  if (poolPercent.value >= 90) return 'bg-danger'
  if (poolPercent.value >= 60) return 'bg-warning'
  return 'bg-success'
})

async function refresh() {
  loading.value = true
  error.value = ''
  try {
    health.value = await api.getSystemHealth()
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Could not load system health'
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <div class="d-flex align-items-center flex-wrap gap-2 mb-3">
      <h1 class="mb-0 me-auto">System</h1>
      <span v-if="health" class="small text-muted">Checked {{ new Date(health.checked_at).toLocaleTimeString() }}</span>
      <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="loading" @click="refresh">
        <i class="bi bi-arrow-clockwise me-1" aria-hidden="true" />{{ loading ? 'Checking…' : 'Refresh' }}
      </button>
    </div>

    <div v-if="error" class="alert alert-danger">{{ error }}</div>

    <template v-if="health">
      <div class="row g-3 mb-4">
        <div class="col-12 col-md-4">
          <div class="card h-100" :class="{ 'border-danger': !health.database.ok }">
            <div class="card-header d-flex align-items-center gap-2">
              <h2 class="h6 mb-0 me-auto">Database</h2>
              <span class="badge" :class="health.database.ok ? 'text-bg-success' : 'text-bg-danger'">
                {{ health.database.ok ? 'Up' : 'Not answering' }}
              </span>
            </div>
            <div class="card-body small">
              <p class="mb-2">Ping {{ formatMs(health.database.ping_ms) }}</p>
              <div class="d-flex justify-content-between mb-1">
                <span>Connections in use</span>
                <span>{{ health.database.acquired_conns }} / {{ health.database.max_conns }}</span>
              </div>
              <div
                class="progress mb-2"
                role="progressbar"
                aria-label="Database connections in use"
                :aria-valuenow="poolPercent"
                aria-valuemin="0"
                aria-valuemax="100"
              >
                <div class="progress-bar" :class="poolBarClass" :style="{ width: poolPercent + '%' }" />
              </div>
              <p class="text-muted mb-0">
                {{ health.database.idle_conns }} idle ·
                {{ health.database.empty_acquire_count }} acquire{{ health.database.empty_acquire_count === 1 ? '' : 's' }}
                found no idle connection since start<template v-if="health.database.empty_acquire_count > 0">
                  (avg {{ (health.database.empty_acquire_wait_ms / health.database.empty_acquire_count).toFixed(1) }} ms)</template>
              </p>
            </div>
          </div>
        </div>

        <div class="col-12 col-md-4">
          <div class="card h-100" :class="{ 'border-danger': !health.redis.ok }">
            <div class="card-header d-flex align-items-center gap-2">
              <h2 class="h6 mb-0 me-auto">Redis</h2>
              <span class="badge" :class="health.redis.ok ? 'text-bg-success' : 'text-bg-danger'">
                {{ health.redis.ok ? 'Up' : 'Not answering' }}
              </span>
            </div>
            <div class="card-body small">
              <p class="mb-2">Ping {{ formatMs(health.redis.ping_ms) }}</p>
              <p v-if="health.redis.error" class="text-danger mb-2">{{ health.redis.error }}</p>
              <p class="text-muted mb-0">Used for sign-in, rate limits, sessions, and live updates.</p>
            </div>
          </div>
        </div>

        <div class="col-12 col-md-4">
          <div class="card h-100">
            <div class="card-header"><h2 class="h6 mb-0">Server process</h2></div>
            <div class="card-body small">
              <dl class="row mb-0">
                <dt class="col-6 fw-normal text-muted">Version</dt>
                <dd class="col-6 mb-1">{{ health.process.version }}</dd>
                <dt class="col-6 fw-normal text-muted">Up for</dt>
                <dd class="col-6 mb-1" :title="new Date(health.process.started_at).toLocaleString()">
                  {{ formatSeconds(health.process.uptime_seconds) }}
                </dd>
                <dt class="col-6 fw-normal text-muted">Memory (heap)</dt>
                <dd class="col-6 mb-1">{{ formatBytes(health.process.heap_alloc_bytes) }}</dd>
                <dt class="col-6 fw-normal text-muted">Goroutines</dt>
                <dd class="col-6 mb-1">{{ health.process.goroutines }}</dd>
                <dt class="col-6 fw-normal text-muted">Live-update streams</dt>
                <dd class="col-6 mb-0">{{ health.process.live_streams }}</dd>
              </dl>
            </div>
          </div>
        </div>
      </div>

      <div class="card mb-4">
        <div class="card-header d-flex align-items-center gap-2">
          <h2 class="h6 mb-0 me-auto">Background workers</h2>
          <span v-if="attentionCount" class="badge text-bg-warning">{{ attentionCount }} need attention</span>
        </div>
        <div class="table-responsive">
          <table class="table table-sm align-middle mb-0 small">
            <thead>
              <tr>
                <th scope="col">Worker</th>
                <th scope="col">Status</th>
                <th scope="col">Last run</th>
                <th scope="col">Took</th>
                <th scope="col">Failures</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="w in workers" :key="w.name">
                <td>
                  {{ workerLabel(w.name) }}
                  <div class="text-muted">{{ formatInterval(w.interval_seconds) }}</div>
                </td>
                <td>
                  <span class="badge" :class="workerStatusBadge(w.status).cls">{{ workerStatusBadge(w.status).label }}</span>
                  <div v-if="w.last_error" class="text-danger mt-1 text-break">{{ w.last_error }}</div>
                </td>
                <td :title="w.last_started_at ? new Date(w.last_started_at).toLocaleString() : ''">
                  {{ timeAgo(w.last_started_at) }}
                </td>
                <td>{{ w.runs ? formatMs(w.last_duration_ms) : '—' }}</td>
                <td>{{ w.failures }} / {{ w.runs }}</td>
              </tr>
              <tr v-if="!workers.length">
                <td colspan="5" class="text-muted">No background workers registered.</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="card-footer small text-muted">
          <strong>Late</strong> means no run has started for over twice the worker's interval; <strong>stuck</strong>
          means the current run has taken that long. Backup files, the audit stream, and automation rules show their own
          details on their pages.
        </div>
      </div>
    </template>
    <p v-else-if="loading" class="text-muted">Checking…</p>
  </div>
</template>
