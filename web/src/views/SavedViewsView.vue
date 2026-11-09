<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { QueryOptions, SavedView } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { audienceIcon, audienceLabel, countClauses, describeClause, isGroup } from '@/utils/queryDefinition'

const views = ref<SavedView[]>([])
const options = ref<QueryOptions | null>(null)
const loading = ref(true)
const filterText = ref('')
const toast = useToast()
const { askConfirm } = useConfirm()

const matches = (v: SavedView) => !filterText.value.trim() || v.name.toLowerCase().includes(filterText.value.trim().toLowerCase())

const personal = computed(() => views.value.filter((v) => !v.shared && matches(v)))

/** Shared queries grouped by audience: each organization, then site-wide. */
const sharedGroups = computed(() => {
  const groups = new Map<string, { key: string; label: string; icon: string; views: SavedView[] }>()
  for (const v of views.value) {
    if (!v.shared || !matches(v)) continue
    const key = v.scope === 'site' ? 'site' : `org:${v.organization_id}`
    if (!groups.has(key)) groups.set(key, { key, label: audienceLabel(v), icon: audienceIcon(v), views: [] })
    groups.get(key)!.views.push(v)
  }
  // The server lists organization queries before site ones.
  return [...groups.values()]
})

/** Where the caller can start a shared query. */
const shareTargets = computed(() => {
  const out = (options.value?.organizations ?? []).map((o) => ({ share: `org:${o.id}`, label: `Members of ${o.name}` }))
  if (options.value?.can_share_site) out.push({ share: 'site', label: 'All users site-wide' })
  return out
})

async function load() {
  loading.value = true
  try {
    const [list, opts] = await Promise.all([api.listSavedViews(), api.queryOptions()])
    views.value = list
    options.value = opts
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load queries', 'error')
  } finally {
    loading.value = false
  }
}

/** A short plain-language summary of a query's first clauses. */
function summary(v: SavedView): string {
  const d = v.definition
  const fields = options.value?.fields ?? []
  const top = (d.where.clauses ?? []).filter((c) => !isGroup(c)).slice(0, 2)
  const parts = top.map((c) => describeClause(c, fields, options.value ?? undefined))
  const total = countClauses(d.where)
  if (total > parts.length) parts.push(`+${total - parts.length} more`)
  if (parts.length === 0) return 'Every task in scope'
  return parts.join(' · ')
}

/** Which projects a query searches. */
function scopeLabel(v: SavedView): string {
  const orgs = v.definition.scope.organizations ?? []
  const n = v.definition.scope.projects.length
  const parts: string[] = []
  if (orgs.length === 1) {
    const o = options.value?.organizations.find((x) => x.id === orgs[0])
    parts.push(o ? `${o.name} projects` : '1 organization')
  } else if (orgs.length > 1) {
    parts.push(`${orgs.length} organizations`)
  }
  if (n === 1) {
    const p = options.value?.projects.find((x) => x.id === v.definition.scope.projects[0])
    parts.push(p ? p.name : '1 project')
  } else if (n > 1) {
    parts.push(`${n} projects`)
  }
  return parts.length ? parts.join(' + ') : 'All projects'
}

async function toggleDashboard(v: SavedView) {
  try {
    await api.setSavedViewOnDashboard(v.id, !v.on_dashboard)
    v.on_dashboard = !v.on_dashboard
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update the dashboard', 'error')
  }
}

async function remove(v: SavedView) {
  const ok = await askConfirm({
    title: 'Delete query?',
    message: v.shared
      ? `Delete the shared query “${v.name}”? ${audienceLabel(v)} will lose it.`
      : `Delete the query “${v.name}”?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteSavedView(v.id)
    views.value = views.value.filter((x) => x.id !== v.id)
    toast.push('Query deleted', 'info')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3 mb-5">
    <div class="d-flex flex-wrap justify-content-between align-items-start gap-2 mb-4">
      <div>
        <h1 class="h3 mb-1">
          <i class="bi bi-funnel me-2" aria-hidden="true" />
          Queries
        </h1>
        <p class="text-muted mb-0">
          Find tasks across your projects with AND/OR clauses, then save, share, export, or pin them to your dashboard.
          Results only ever include tasks you can open.
        </p>
      </div>
      <div class="d-flex gap-2">
        <RouterLink to="/views/new" class="btn btn-primary btn-sm">
          <i class="bi bi-plus-lg" /> New query
        </RouterLink>
      </div>
    </div>

    <div v-if="loading" class="text-muted">Loading…</div>
    <template v-else>
      <div class="mb-3" style="max-width: 20rem">
        <label for="query-filter" class="visually-hidden">Filter queries</label>
        <input id="query-filter" v-model="filterText" type="search" class="form-control form-control-sm" placeholder="Filter by name…" />
      </div>

      <section class="mb-4" aria-labelledby="my-queries-heading">
        <h2 id="my-queries-heading" class="h6 text-uppercase text-muted">My queries</h2>
        <p v-if="personal.length === 0" class="text-muted small">
          No personal queries yet. <RouterLink to="/views/new">Create one</RouterLink>, or use <strong>+ View</strong> in the
          sidebar to save your current task-list filters.
        </p>
        <ul v-else class="list-group">
          <li v-for="v in personal" :key="v.id" class="list-group-item d-flex flex-wrap align-items-center gap-2">
            <div class="me-auto min-w-0">
              <RouterLink :to="`/views/${v.id}`" class="fw-semibold">{{ v.name }}</RouterLink>
              <span class="badge text-bg-light border ms-2 fw-normal">{{ scopeLabel(v) }}</span>
              <span v-if="v.definition.mode === 'tree'" class="badge text-bg-light border ms-1 fw-normal">Tree</span>
              <div class="small text-muted text-truncate">{{ summary(v) }}</div>
            </div>
            <button
              type="button"
              class="btn btn-sm"
              :class="v.on_dashboard ? 'btn-secondary' : 'btn-outline-secondary'"
              :aria-pressed="v.on_dashboard"
              :title="v.on_dashboard ? 'Remove from dashboard' : 'Pin to dashboard'"
              @click="toggleDashboard(v)"
            >
              <i class="bi bi-pin-angle" /><span class="visually-hidden">Pin to dashboard</span>
            </button>
            <button type="button" class="btn btn-sm btn-outline-danger" :aria-label="`Delete ${v.name}`" @click="remove(v)">
              <i class="bi bi-trash" />
            </button>
          </li>
        </ul>
      </section>

      <section aria-labelledby="shared-queries-heading">
        <div class="d-flex align-items-center gap-2 mb-2">
          <h2 id="shared-queries-heading" class="h6 text-uppercase text-muted mb-0">Shared queries</h2>
          <div v-if="shareTargets.length" class="dropdown ms-auto">
            <button class="btn btn-sm btn-outline-primary dropdown-toggle" type="button" data-bs-toggle="dropdown" aria-expanded="false">
              <i class="bi bi-people" /> New shared query
            </button>
            <ul class="dropdown-menu dropdown-menu-end">
              <li v-for="t in shareTargets" :key="t.share">
                <RouterLink class="dropdown-item" :to="{ path: '/views/new', query: { share: t.share } }">{{ t.label }}</RouterLink>
              </li>
            </ul>
          </div>
        </div>
        <p v-if="sharedGroups.length === 0" class="text-muted small">
          Queries shared with your organizations, or with everyone, appear here. Any member can share a query with their
          organization; its owner and people with the <em>Manage shared queries</em> permission can change it.
        </p>
        <div v-for="g in sharedGroups" :key="g.key" class="mb-3">
          <div class="small fw-semibold mb-1"><i :class="`${g.icon} me-1`" aria-hidden="true" />{{ g.label }}</div>
          <ul class="list-group">
            <li v-for="v in g.views" :key="v.id" class="list-group-item d-flex flex-wrap align-items-center gap-2">
              <div class="me-auto min-w-0">
                <RouterLink :to="`/views/${v.id}`" class="fw-semibold">{{ v.name }}</RouterLink>
                <span class="badge text-bg-light border ms-2 fw-normal">{{ scopeLabel(v) }}</span>
                <span v-if="v.is_owner" class="small text-muted ms-2">by you</span>
                <span v-else-if="v.author_name" class="small text-muted ms-2">by {{ v.author_name }}</span>
                <span v-if="!v.can_edit" class="badge text-bg-light border ms-2 fw-normal"><i class="bi bi-lock" /> Read only</span>
                <div class="small text-muted text-truncate">{{ summary(v) }}</div>
              </div>
              <button
                type="button"
                class="btn btn-sm"
                :class="v.on_dashboard ? 'btn-secondary' : 'btn-outline-secondary'"
                :aria-pressed="v.on_dashboard"
                :title="v.on_dashboard ? 'Remove from dashboard' : 'Pin to dashboard'"
                @click="toggleDashboard(v)"
              >
                <i class="bi bi-pin-angle" /><span class="visually-hidden">Pin to dashboard</span>
              </button>
              <button v-if="v.can_edit" type="button" class="btn btn-sm btn-outline-danger" :aria-label="`Delete ${v.name}`" @click="remove(v)">
                <i class="bi bi-trash" />
              </button>
            </li>
          </ul>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.min-w-0 {
  min-width: 0;
}
</style>
