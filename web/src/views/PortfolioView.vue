<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { PortfolioProject, ProjectHealth, ProjectStatusUpdate } from '@/api/types'
import { APIError } from '@/api/types'
import RichBody from '@/components/RichBody.vue'
import { useToast } from '@/composables/useToast'
import { projectRoute } from '@/utils/projectRoute'

const { push } = useToast()

const HEALTH: Record<ProjectHealth, { label: string; badge: string }> = {
  on_track: { label: 'On track', badge: 'text-bg-success' },
  at_risk: { label: 'At risk', badge: 'text-bg-warning' },
  off_track: { label: 'Off track', badge: 'text-bg-danger' },
}

const projects = ref<PortfolioProject[]>([])
const loading = ref(true)
const includeArchived = ref(false)
const search = ref('')
const healthFilter = ref<'' | ProjectHealth | 'none'>('')
type SortKey = 'name' | 'overdue' | 'open' | 'health' | 'updated'
const sortKey = ref<SortKey>('health')

const healthRank: Record<string, number> = { off_track: 0, at_risk: 1, on_track: 2 }

const rows = computed(() => {
  const q = search.value.trim().toLowerCase()
  const list = projects.value.filter((p) => {
    if (q && !p.name.toLowerCase().includes(q) && !p.owner_name.toLowerCase().includes(q)) return false
    if (healthFilter.value === 'none') return !p.latest_update
    if (healthFilter.value) return p.latest_update?.health === healthFilter.value
    return true
  })
  const by: Record<SortKey, (a: PortfolioProject, b: PortfolioProject) => number> = {
    name: (a, b) => a.name.localeCompare(b.name),
    overdue: (a, b) => b.overdue - a.overdue,
    open: (a, b) => b.open - a.open,
    health: (a, b) =>
      (healthRank[a.latest_update?.health ?? ''] ?? 3) - (healthRank[b.latest_update?.health ?? ''] ?? 3) ||
      b.overdue - a.overdue,
    updated: (a, b) => (b.latest_update?.created_at ?? '').localeCompare(a.latest_update?.created_at ?? ''),
  }
  return [...list].sort(by[sortKey.value])
})

const totals = computed(() => {
  const t = { onTrack: 0, atRisk: 0, offTrack: 0, none: 0, overdue: 0 }
  for (const p of projects.value) {
    t.overdue += p.overdue
    const h = p.latest_update?.health
    if (h === 'on_track') t.onTrack++
    else if (h === 'at_risk') t.atRisk++
    else if (h === 'off_track') t.offTrack++
    else t.none++
  }
  return t
})

async function load() {
  loading.value = true
  try {
    projects.value = (await api.getPortfolio(includeArchived.value)).projects
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load the portfolio', 'error')
  } finally {
    loading.value = false
  }
}

function completion(p: PortfolioProject) {
  const total = p.open + p.done
  return total ? Math.round((p.done / total) * 100) : 0
}

function ago(iso: string) {
  const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000)
  if (days <= 0) return 'today'
  if (days === 1) return 'yesterday'
  if (days < 30) return `${days} days ago`
  return new Date(iso).toLocaleDateString()
}

// --- Status update panel ---
const panel = ref<PortfolioProject | null>(null)
const history = ref<ProjectStatusUpdate[]>([])
const draft = reactive({ health: 'on_track' as ProjectHealth, body: '' })
const posting = ref(false)

async function openPanel(p: PortfolioProject) {
  panel.value = p
  draft.health = p.latest_update?.health ?? 'on_track'
  draft.body = ''
  history.value = []
  try {
    history.value = (await api.listProjectStatusUpdates(p.id)).updates
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load updates', 'error')
  }
}

async function post() {
  if (!panel.value) return
  posting.value = true
  try {
    const u = await api.postProjectStatusUpdate(panel.value.id, draft.health, draft.body)
    history.value = [u, ...history.value]
    panel.value.latest_update = u
    draft.body = ''
    push('Status update posted', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Post failed', 'error')
  } finally {
    posting.value = false
  }
}

async function remove(u: ProjectStatusUpdate) {
  if (!panel.value) return
  try {
    await api.deleteProjectStatusUpdate(panel.value.id, u.id)
    history.value = history.value.filter((x) => x.id !== u.id)
    panel.value.latest_update = history.value[0] ?? null
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="container-fluid py-3" style="max-width: 90rem">
    <div class="d-flex flex-wrap align-items-end justify-content-between gap-2 mb-3">
      <div>
        <h1 class="h3 fw-bold mb-1">Portfolio</h1>
        <p class="text-muted small mb-0">Every project you can open, with its numbers and the lead's latest status update.</p>
      </div>
      <div class="d-flex flex-wrap gap-2 small">
        <span class="badge text-bg-danger">{{ totals.offTrack }} off track</span>
        <span class="badge text-bg-warning">{{ totals.atRisk }} at risk</span>
        <span class="badge text-bg-success">{{ totals.onTrack }} on track</span>
        <span class="badge text-bg-secondary">{{ totals.none }} no update</span>
        <span class="badge text-bg-light border">{{ totals.overdue }} overdue tasks</span>
      </div>
    </div>

    <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
      <input v-model="search" type="search" class="form-control form-control-sm" style="max-width: 16rem" placeholder="Filter projects or owners" aria-label="Filter projects" />
      <select v-model="healthFilter" class="form-select form-select-sm" style="max-width: 12rem" aria-label="Filter by health">
        <option value="">Any health</option>
        <option value="off_track">Off track</option>
        <option value="at_risk">At risk</option>
        <option value="on_track">On track</option>
        <option value="none">No update yet</option>
      </select>
      <select v-model="sortKey" class="form-select form-select-sm" style="max-width: 12rem" aria-label="Sort">
        <option value="health">Sort: needs attention</option>
        <option value="overdue">Sort: most overdue</option>
        <option value="open">Sort: most open</option>
        <option value="updated">Sort: latest update</option>
        <option value="name">Sort: name</option>
      </select>
      <div class="form-check form-switch ms-sm-auto">
        <input id="portfolio-archived" v-model="includeArchived" class="form-check-input" type="checkbox" role="switch" @change="load" />
        <label class="form-check-label small" for="portfolio-archived">Include archived</label>
      </div>
    </div>

    <p v-if="loading" class="text-muted">Loading…</p>
    <div v-else class="table-responsive">
      <table class="table align-middle">
        <thead>
          <tr class="small text-muted">
            <th scope="col">Project</th>
            <th scope="col">Health</th>
            <th scope="col" class="text-end">Open</th>
            <th scope="col" class="text-end">Overdue</th>
            <th scope="col" class="text-end">Due in 7 days</th>
            <th scope="col" class="text-end">Unclaimed</th>
            <th scope="col" class="text-end">Done (7 days)</th>
            <th scope="col" style="min-width: 8rem">Progress</th>
            <th scope="col" style="min-width: 16rem">Latest update</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in rows" :key="p.id">
            <td>
              <RouterLink :to="projectRoute(p)" class="fw-semibold">{{ p.name }}</RouterLink>
              <span v-if="p.archived" class="badge text-bg-secondary ms-1">Archived</span>
              <div class="small text-muted">
                {{ p.owner_name }}<template v-if="p.organization_name"> · {{ p.organization_name }}</template>
              </div>
            </td>
            <td>
              <span v-if="p.latest_update" class="badge" :class="HEALTH[p.latest_update.health].badge">
                {{ HEALTH[p.latest_update.health].label }}
              </span>
              <span v-else class="text-muted small">—</span>
            </td>
            <td class="text-end">{{ p.open }}</td>
            <td class="text-end" :class="{ 'text-danger fw-semibold': p.overdue }">{{ p.overdue }}</td>
            <td class="text-end">{{ p.due_this_week }}</td>
            <td class="text-end">{{ p.unclaimed }}</td>
            <td class="text-end">{{ p.done_last_week }}</td>
            <td>
              <div class="progress" role="progressbar" :aria-valuenow="completion(p)" aria-valuemin="0" aria-valuemax="100" :aria-label="`${completion(p)}% done`" style="height: 0.5rem">
                <div class="progress-bar" :style="{ width: `${completion(p)}%` }" />
              </div>
              <div class="small text-muted">{{ completion(p) }}% of {{ p.open + p.done }}</div>
            </td>
            <td>
              <div v-if="p.latest_update" class="small text-truncate" style="max-width: 22rem" :title="p.latest_update.body">
                {{ p.latest_update.body || 'No note' }}
              </div>
              <div class="small text-muted d-flex gap-2 align-items-center">
                <span v-if="p.latest_update">{{ p.latest_update.author_name }}, {{ ago(p.latest_update.created_at) }}</span>
                <button type="button" class="btn btn-link btn-sm p-0" @click="openPanel(p)">
                  {{ p.can_post_update ? 'Post update' : 'History' }}
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="9" class="text-muted">No projects match.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <Teleport to="body">
      <div v-if="panel" class="offcanvas offcanvas-end show" tabindex="-1" role="dialog" aria-labelledby="status-panel-title" style="visibility: visible">
        <div class="offcanvas-header">
          <h5 id="status-panel-title" class="offcanvas-title">{{ panel.name }}: status</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="panel = null" />
        </div>
        <div class="offcanvas-body">
          <form v-if="panel.can_post_update" class="mb-4" @submit.prevent="post">
            <div class="btn-group w-100 mb-2" role="group" aria-label="Health">
              <template v-for="(meta, key) in HEALTH" :key="key">
                <input :id="`health-${key}`" v-model="draft.health" type="radio" class="btn-check" :value="key" />
                <label class="btn btn-outline-secondary btn-sm" :for="`health-${key}`">{{ meta.label }}</label>
              </template>
            </div>
            <textarea v-model="draft.body" class="form-control mb-2" rows="4" maxlength="4000" placeholder="What changed, what's next, what's blocking you (Markdown)" aria-label="Update" />
            <button type="submit" class="btn btn-primary btn-sm" :disabled="posting">{{ posting ? 'Posting…' : 'Post update' }}</button>
          </form>
          <h6 class="text-muted small text-uppercase">History</h6>
          <p v-if="!history.length" class="small text-muted">No updates yet.</p>
          <div v-for="u in history" :key="u.id" class="border-start border-3 ps-2 mb-3" :class="{
            'border-success': u.health === 'on_track',
            'border-warning': u.health === 'at_risk',
            'border-danger': u.health === 'off_track',
          }">
            <div class="small d-flex gap-2 align-items-center">
              <span class="badge" :class="HEALTH[u.health].badge">{{ HEALTH[u.health].label }}</span>
              <span class="text-muted">{{ u.author_name }} · {{ new Date(u.created_at).toLocaleString() }}</span>
              <button v-if="panel.can_post_update" type="button" class="btn btn-link btn-sm text-danger p-0 ms-auto" @click="remove(u)">Delete</button>
            </div>
            <RichBody v-if="u.body" :body="u.body" class="small mt-1" />
          </div>
        </div>
      </div>
      <div v-if="panel" class="offcanvas-backdrop fade show" @click="panel = null" />
    </Teleport>
  </div>
</template>
