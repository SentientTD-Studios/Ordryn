<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { AdminUser, AuditEntry, AuditFacets, AuditList, AuditQuery, AuditSource } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import AdminSubnav from '@/components/AdminSubnav.vue'
import {
  auditChanges,
  auditDetails,
  auditEventLabel,
  auditSourceLabels,
  humanizeEventType,
} from '@/utils/auditLog'

const toast = useToast()
const loading = ref(false)
const result = ref<AuditList>({ items: [], total: 0, limit: 50, offset: 0 })
const facets = ref<AuditFacets>({ event_types: [], projects: [] })
const users = ref<AdminUser[]>([])

const userId = ref<number | ''>('')
const projectId = ref<number | ''>('')
const source = ref<AuditSource | ''>('')
const eventType = ref('')
const sinceDate = ref('')
const untilDate = ref('')
const page = ref(0)
const pageSize = 50

const retentionDays = ref(0)
const retentionBusy = ref(false)

const sourceOptions: { value: AuditSource | ''; label: string }[] = [
  { value: '', label: 'All sources' },
  { value: 'task', label: 'Tasks' },
  { value: 'project', label: 'Projects' },
  { value: 'comment', label: 'Comments' },
  { value: 'admin', label: 'Admin actions' },
  { value: 'email', label: 'Email' },
]

const eventTypeOptions = computed(() => {
  const seen = new Set<string>()
  const out: { value: string; label: string }[] = []
  for (const et of facets.value.event_types) {
    if (source.value && et.source !== source.value) continue
    if (seen.has(et.event_type)) continue
    seen.add(et.event_type)
    out.push({ value: et.event_type, label: humanizeEventType(et.event_type) })
  }
  return out.sort((a, b) => a.label.localeCompare(b.label))
})

const currentQuery = computed<AuditQuery>(() => ({
  user_id: userId.value,
  project_id: projectId.value,
  source: source.value,
  event_type: eventType.value,
  since: sinceDate.value,
  until: untilDate.value,
}))

const exportURL = computed(() => api.adminAuditExportURL(currentQuery.value))

const totalPages = computed(() => Math.max(1, Math.ceil(result.value.total / pageSize)))
const pageLabel = computed(() => {
  if (result.value.total === 0) return 'No rows'
  const start = result.value.offset + 1
  const end = result.value.offset + result.value.items.length
  return `${start}–${end} of ${result.value.total}`
})

function formatWhen(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function actorLabel(row: AuditEntry) {
  if (row.source === 'email') return 'System'
  if (!row.actor_user_id) return 'System'
  return row.actor_user_name || row.actor_email || `User #${row.actor_user_id}`
}

function sourceBadge(src: string) {
  switch (src) {
    case 'admin':
      return 'text-bg-danger'
    case 'project':
      return 'text-bg-primary'
    case 'comment':
      return 'text-bg-info'
    case 'email':
      return 'text-bg-warning'
    default:
      return 'text-bg-secondary'
  }
}

function taskLinkID(row: AuditEntry): number | null {
  if (row.source === 'task' && row.target_id) return row.target_id
  if (row.source === 'comment') {
    const id = Number(row.metadata.task_id)
    return Number.isFinite(id) && id > 0 ? id : null
  }
  return null
}

function targetLabel(row: AuditEntry) {
  if (row.target_label) return row.target_label
  if (row.target_id) return `${row.target_type} #${row.target_id}`
  return row.target_type || '—'
}

async function load() {
  loading.value = true
  try {
    result.value = await api.listAdminAudit({
      ...currentQuery.value,
      limit: pageSize,
      offset: page.value * pageSize,
    })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load audit log', 'error')
  } finally {
    loading.value = false
  }
}

async function loadFilters() {
  const [f, u, s] = await Promise.allSettled([
    api.getAdminAuditFacets(),
    api.listAdminUsers(),
    api.getAdminSettings(),
  ])
  if (f.status === 'fulfilled') facets.value = f.value
  if (u.status === 'fulfilled') {
    users.value = [...u.value].sort((a, b) =>
      (a.user_name || a.email).localeCompare(b.user_name || b.email),
    )
  }
  if (s.status === 'fulfilled') retentionDays.value = s.value.audit_retention_days ?? 0
}

function applyFilters() {
  page.value = 0
  void load()
}

function resetFilters() {
  userId.value = ''
  projectId.value = ''
  source.value = ''
  eventType.value = ''
  sinceDate.value = ''
  untilDate.value = ''
  applyFilters()
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

async function saveRetention() {
  retentionBusy.value = true
  try {
    const saved = await api.patchAdminSettings({ audit_retention_days: retentionDays.value })
    retentionDays.value = saved.audit_retention_days
    toast.push('Audit retention saved', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    retentionBusy.value = false
  }
}

watch(source, () => {
  if (eventType.value && !eventTypeOptions.value.some((o) => o.value === eventType.value)) {
    eventType.value = ''
  }
})
watch([userId, projectId, source, eventType], applyFilters)

onMounted(() => {
  void load()
  void loadFilters()
})
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Audit log</h1>
    <p class="text-muted">
      Who did what, and when, across tasks, projects, comments, admin actions and outbound email.
    </p>

    <form class="row g-3 align-items-end mb-3" @submit.prevent="applyFilters">
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-user">User</label>
        <select id="audit-user" v-model="userId" class="form-select">
          <option value="">Anyone</option>
          <option v-for="u in users" :key="u.id" :value="u.id">
            {{ u.user_name || u.email }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-project">Project</label>
        <select id="audit-project" v-model="projectId" class="form-select">
          <option value="">All projects</option>
          <option v-for="p in facets.projects" :key="p.id" :value="p.id">
            {{ p.name }}{{ p.archived ? ' (archived)' : '' }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-source">Source</label>
        <select id="audit-source" v-model="source" class="form-select">
          <option v-for="opt in sourceOptions" :key="opt.value || 'all'" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-event">Event</label>
        <select id="audit-event" v-model="eventType" class="form-select">
          <option value="">All events</option>
          <option v-for="opt in eventTypeOptions" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-since">From</label>
        <input id="audit-since" v-model="sinceDate" type="date" class="form-control" />
      </div>
      <div class="col-sm-6 col-md-4 col-lg-2">
        <label class="form-label" for="audit-until">To</label>
        <input id="audit-until" v-model="untilDate" type="date" class="form-control" />
      </div>
      <div class="col-12 d-flex flex-wrap gap-2">
        <button type="submit" class="btn btn-primary" :disabled="loading">
          {{ loading ? 'Loading…' : 'Search' }}
        </button>
        <button type="button" class="btn btn-outline-secondary" :disabled="loading" @click="resetFilters">
          Reset
        </button>
        <a class="btn btn-outline-secondary ms-auto" :href="exportURL" download>
          <i class="bi bi-download me-1" aria-hidden="true"></i>Export CSV
        </a>
      </div>
    </form>

    <div class="card">
      <div class="table-responsive">
        <table class="table table-hover mb-0 align-top">
          <thead>
            <tr>
              <th>When</th>
              <th>Who</th>
              <th>What</th>
              <th>Target</th>
              <th>Before → after</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!loading && result.items.length === 0">
              <td colspan="5" class="text-muted py-4 text-center">No matching audit events.</td>
            </tr>
            <tr v-for="row in result.items" :key="row.key">
              <td class="text-nowrap small">{{ formatWhen(row.created_at) }}</td>
              <td class="small">
                <div>{{ actorLabel(row) }}</div>
                <div v-if="row.actor_user_id && row.actor_email && row.actor_user_name" class="text-muted">
                  {{ row.actor_email }}
                </div>
              </td>
              <td>
                <span class="badge me-1" :class="sourceBadge(row.source)">
                  {{ auditSourceLabels[row.source] || row.source }}
                </span>
                <span>{{ auditEventLabel(row) }}</span>
              </td>
              <td class="small" style="max-width: 18rem; word-break: break-word">
                <RouterLink v-if="taskLinkID(row)" :to="`/tasks/${taskLinkID(row)}`">
                  {{ targetLabel(row) }}
                </RouterLink>
                <span v-else>{{ targetLabel(row) }}</span>
                <div v-if="row.project_name && row.source !== 'project'" class="text-muted">
                  {{ row.project_name }}
                </div>
              </td>
              <td class="small" style="max-width: 28rem; word-break: break-word">
                <div v-for="(c, i) in auditChanges(row)" :key="`c${i}`">
                  <span v-if="c.field" class="text-muted">{{ c.field }}: </span>
                  <span class="text-danger-emphasis" style="white-space: pre-wrap">{{ c.from }}</span>
                  <span class="text-muted mx-1">→</span>
                  <span class="text-success-emphasis" style="white-space: pre-wrap">{{ c.to }}</span>
                </div>
                <div v-for="d in auditDetails(row)" :key="d.key" class="text-muted">
                  {{ d.key }}: {{ d.value }}
                </div>
                <span v-if="auditChanges(row).length === 0 && auditDetails(row).length === 0" class="text-muted">—</span>
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

    <form class="card mt-4" @submit.prevent="saveRetention">
      <div class="card-body">
        <h2 class="h5">Retention</h2>
        <label class="form-label" for="audit-retention">Keep audit history for (days)</label>
        <div class="d-flex flex-wrap gap-2 align-items-start">
          <input
            id="audit-retention"
            v-model.number="retentionDays"
            type="number"
            class="form-control"
            min="0"
            max="3650"
            required
            style="max-width: 8rem"
          />
          <button type="submit" class="btn btn-primary" :disabled="retentionBusy">
            {{ retentionBusy ? 'Saving…' : 'Save' }}
          </button>
        </div>
        <div class="form-text">
          0 keeps history forever. Otherwise task, project, comment-revision and admin events older than this
          are deleted hourly, which also removes them from task activity and comment history.
          Email rows follow the separate email log retention in
          <RouterLink to="/admin">Settings</RouterLink>.
        </div>
      </div>
    </form>
  </div>
</template>
