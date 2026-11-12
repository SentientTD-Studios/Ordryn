<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { TimesheetReport, WorkloadReport } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const { push } = useToast()
const route = useRoute()
const router = useRouter()

type Tab = 'workload' | 'time'
const tab = ref<Tab>(route.query.tab === 'time' ? 'time' : 'workload')
watch(tab, (t) => void router.replace({ query: { ...route.query, tab: t } }))

function hours(minutes: number) {
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return h ? `${h}h${m ? ` ${m}m` : ''}` : `${m}m`
}

// --- Workload ---
const workload = ref<WorkloadReport | null>(null)
const maxOpen = computed(() => Math.max(1, ...(workload.value?.people.map((p) => p.claimed_open) ?? [1])))

async function loadWorkload() {
  try {
    workload.value = await api.getWorkload()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load workload', 'error')
  }
}

// --- Timesheet ---
function isoDay(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const today = new Date()
const from = ref(isoDay(new Date(today.getFullYear(), today.getMonth(), today.getDate() - 6)))
const to = ref(isoDay(today))
const personFilter = ref(0)
const projectFilter = ref(0)
const timesheet = ref<TimesheetReport | null>(null)
const loadingTime = ref(false)

const people = computed(() => {
  const m = new Map<number, string>()
  for (const e of timesheet.value?.entries ?? []) m.set(e.user_id, e.user_name)
  return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
})
const projects = computed(() => {
  const m = new Map<number, string>()
  for (const e of timesheet.value?.entries ?? []) m.set(e.project_id, e.project_name)
  return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
})

function setRange(preset: 'week' | 'last-week' | 'month' | 'last-month') {
  const d = new Date()
  if (preset === 'week') {
    const start = new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7))
    from.value = isoDay(start)
    to.value = isoDay(d)
  } else if (preset === 'last-week') {
    const start = new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7) - 7)
    from.value = isoDay(start)
    to.value = isoDay(new Date(start.getFullYear(), start.getMonth(), start.getDate() + 6))
  } else if (preset === 'month') {
    from.value = isoDay(new Date(d.getFullYear(), d.getMonth(), 1))
    to.value = isoDay(d)
  } else {
    from.value = isoDay(new Date(d.getFullYear(), d.getMonth() - 1, 1))
    to.value = isoDay(new Date(d.getFullYear(), d.getMonth(), 0))
  }
  void loadTime()
}

function params() {
  return { from: from.value, to: to.value, user_id: personFilter.value || undefined, project_id: projectFilter.value || undefined }
}

async function loadTime() {
  loadingTime.value = true
  try {
    timesheet.value = await api.getTimesheet(params())
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load the timesheet', 'error')
  } finally {
    loadingTime.value = false
  }
}

async function exportCSV() {
  try {
    await api.downloadTimesheet(params())
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Export failed', 'error')
  }
}

onMounted(() => {
  void loadWorkload()
  void loadTime()
})
</script>

<template>
  <div class="container py-3">
    <h1 class="h3 fw-bold mb-1">Reports</h1>
    <p class="text-muted small mb-3">Workload and logged time across the projects you can see.</p>

    <ul class="nav nav-tabs mb-3" role="tablist">
      <li class="nav-item" role="presentation">
        <button class="nav-link" :class="{ active: tab === 'workload' }" type="button" role="tab" :aria-selected="tab === 'workload'" @click="tab = 'workload'">Workload</button>
      </li>
      <li class="nav-item" role="presentation">
        <button class="nav-link" :class="{ active: tab === 'time' }" type="button" role="tab" :aria-selected="tab === 'time'" @click="tab = 'time'">Timesheet</button>
      </li>
    </ul>

    <section v-if="tab === 'workload'" aria-label="Workload">
      <p v-if="!workload" class="text-muted">Loading…</p>
      <div v-else-if="!workload.project_count" class="alert alert-secondary small">
        Workload covers projects where your role can <strong>View team workload</strong>. You don't have that on any
        project yet.
      </div>
      <template v-else>
        <p class="small text-muted">
          Open claimed work per person across {{ workload.project_count }} project{{ workload.project_count === 1 ? '' : 's' }}.
        </p>
        <div class="table-responsive">
          <table class="table align-middle">
            <thead>
              <tr class="small text-muted">
                <th scope="col">Person</th>
                <th scope="col" style="min-width: 12rem">Open claimed</th>
                <th scope="col" class="text-end">Points</th>
                <th scope="col" class="text-end">Overdue</th>
                <th scope="col" class="text-end">Projects</th>
                <th scope="col" class="text-end">Done (7 days)</th>
                <th scope="col" class="text-end">Logged (7 days)</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in workload.people" :key="p.user_id">
                <td class="fw-semibold">{{ p.user_name }}</td>
                <td>
                  <div class="d-flex align-items-center gap-2">
                    <div class="progress flex-grow-1" style="height: 0.5rem" role="presentation">
                      <div class="progress-bar" :class="{ 'bg-danger': p.claimed_overdue }" :style="{ width: `${(p.claimed_open / maxOpen) * 100}%` }" />
                    </div>
                    <span class="small">{{ p.claimed_open }}</span>
                  </div>
                </td>
                <td class="text-end">{{ p.claimed_points }}</td>
                <td class="text-end" :class="{ 'text-danger fw-semibold': p.claimed_overdue }">{{ p.claimed_overdue }}</td>
                <td class="text-end">{{ p.projects }}</td>
                <td class="text-end">{{ p.done_last_week }}</td>
                <td class="text-end">{{ hours(p.minutes_last_week) }}</td>
              </tr>
              <tr v-if="!workload.people.length">
                <td colspan="7" class="text-muted">Nobody has claimed or logged anything yet.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </section>

    <section v-else aria-label="Timesheet">
      <form class="row g-2 align-items-end mb-3" @submit.prevent="loadTime">
        <div class="col-auto">
          <label class="form-label small mb-0" for="ts-from">From</label>
          <input id="ts-from" v-model="from" type="date" class="form-control form-control-sm" />
        </div>
        <div class="col-auto">
          <label class="form-label small mb-0" for="ts-to">To</label>
          <input id="ts-to" v-model="to" type="date" class="form-control form-control-sm" />
        </div>
        <div class="col-auto">
          <label class="form-label small mb-0" for="ts-person">Person</label>
          <select id="ts-person" v-model.number="personFilter" class="form-select form-select-sm">
            <option :value="0">Everyone I can see</option>
            <option v-for="[id, name] in people" :key="id" :value="id">{{ name }}</option>
          </select>
        </div>
        <div class="col-auto">
          <label class="form-label small mb-0" for="ts-project">Project</label>
          <select id="ts-project" v-model.number="projectFilter" class="form-select form-select-sm">
            <option :value="0">All projects</option>
            <option v-for="[id, name] in projects" :key="id" :value="id">{{ name }}</option>
          </select>
        </div>
        <div class="col-auto d-flex gap-1">
          <button type="submit" class="btn btn-sm btn-primary" :disabled="loadingTime">Show</button>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="exportCSV">
            <i class="bi bi-download me-1" aria-hidden="true" />CSV
          </button>
        </div>
        <div class="col-12 d-flex flex-wrap gap-1 small">
          <button type="button" class="btn btn-link btn-sm p-0 me-2" @click="setRange('week')">This week</button>
          <button type="button" class="btn btn-link btn-sm p-0 me-2" @click="setRange('last-week')">Last week</button>
          <button type="button" class="btn btn-link btn-sm p-0 me-2" @click="setRange('month')">This month</button>
          <button type="button" class="btn btn-link btn-sm p-0" @click="setRange('last-month')">Last month</button>
        </div>
      </form>

      <p v-if="!timesheet" class="text-muted">Loading…</p>
      <template v-else>
        <p class="fw-semibold">
          {{ hours(timesheet.total_minutes) }} logged
          <span class="text-muted fw-normal small">({{ timesheet.entries.length }} entries, {{ timesheet.from }} to {{ timesheet.to }})</span>
        </p>
        <div v-if="timesheet.truncated" class="alert alert-warning small py-2">Showing the newest 5,000 entries. Narrow the range or export.</div>
        <p class="small text-muted">You see your own time everywhere, and everyone's on projects where you can view team workload.</p>
        <div class="table-responsive">
          <table class="table table-sm align-middle">
            <thead>
              <tr class="small text-muted">
                <th scope="col">When</th>
                <th scope="col">Person</th>
                <th scope="col">Project</th>
                <th scope="col">Task</th>
                <th scope="col" class="text-end">Time</th>
                <th scope="col">Note</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in timesheet.entries" :key="e.id">
                <td class="small text-nowrap">{{ new Date(e.logged_at).toLocaleString() }}</td>
                <td class="small">{{ e.user_name }}</td>
                <td class="small">{{ e.project_name }}</td>
                <td class="small"><RouterLink :to="`/tasks/${e.task_id}`">#{{ e.task_id }} {{ e.task_title }}</RouterLink></td>
                <td class="small text-end text-nowrap">{{ hours(e.minutes) }}</td>
                <td class="small text-muted">{{ e.note }}</td>
              </tr>
              <tr v-if="!timesheet.entries.length">
                <td colspan="6" class="text-muted">No time logged in this range.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </section>
  </div>
</template>
