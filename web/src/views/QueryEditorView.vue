<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { QueryDefinition, QueryNode, QueryOptions, QueryResults, QuerySortKey, SavedView, SavedViewScope } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import QueryGroupEditor from '@/components/query/QueryGroupEditor.vue'
import QueryResultsTable from '@/components/query/QueryResultsTable.vue'
import {
  audienceIcon,
  audienceLabel,
  cloneDefinition,
  definitionProblems,
  emptyDefinition,
  pruneEmptyGroups,
} from '@/utils/queryDefinition'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const { askConfirm } = useConfirm()

const view = ref<SavedView | null>(null)
const options = ref<QueryOptions | null>(null)
const name = ref('')
const def = ref<QueryDefinition>(emptyDefinition())
/**
 * Who can open the query: "personal", "site", or "org:<id>". Which projects
 * it searches is part of the definition's scope, not of who can open it.
 */
const audience = ref('personal')
const savedSnapshot = ref('')
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const running = ref(false)
const results = ref<QueryResults | null>(null)
const runError = ref('')
const page = ref(1)
const editorOpen = ref(true)

const isNew = computed(() => route.name === 'query-new')
const viewId = computed(() => (isNew.value ? null : Number(route.params.id)))
const canEdit = computed(() => isNew.value || !!view.value?.can_edit)
const audienceScope = computed<SavedViewScope>(() =>
  audience.value === 'site' ? 'site' : audience.value.startsWith('org:') ? 'organization' : 'personal',
)
const audienceOrgId = computed(() => (audience.value.startsWith('org:') ? Number(audience.value.slice(4)) || null : null))
const audienceInfo = computed(() => ({
  scope: audienceScope.value,
  organization_name:
    options.value?.organizations.find((o) => o.id === audienceOrgId.value)?.name || view.value?.organization_name || '',
}))
const isShared = computed(() => audienceScope.value !== 'personal')
/** Only the owner (or the author of a new query) picks who can open it. */
const canChooseAudience = computed(() => isNew.value || (!!view.value?.is_owner && canEdit.value))
/** The audience the saved copy has, kept as a choice even if you could no longer pick it. */
const audienceChoices = computed(() => {
  const out: { value: string; label: string }[] = [{ value: 'personal', label: 'Myself' }]
  for (const o of options.value?.organizations ?? []) out.push({ value: `org:${o.id}`, label: `Members of ${o.name}` })
  if (options.value?.can_share_site) out.push({ value: 'site', label: 'All users site-wide' })
  if (!out.some((c) => c.value === audience.value)) out.push({ value: audience.value, label: audienceLabel(audienceInfo.value) })
  return out
})
const audienceHelp = computed(() => {
  if (audienceScope.value === 'site') return 'Everyone on the site can run it. Only people who manage site-wide queries can change it.'
  if (audienceScope.value === 'organization')
    return "The organization's members can run it; you, and people who manage the organization's queries, can change it."
  return 'Only you can see it.'
})

function audienceOf(v: SavedView): string {
  if (v.scope === 'site') return 'site'
  if (v.scope === 'organization' && v.organization_id) return `org:${v.organization_id}`
  return 'personal'
}

/** "All my projects", "Organizations", or "Selected projects". */
const scopeMode = computed(() =>
  def.value.scope.organizations?.length ? 'orgs' : def.value.scope.projects.length ? 'projects' : 'all',
)

function setScopeMode(mode: 'all' | 'orgs' | 'projects') {
  const opts = options.value
  if (mode === 'all') {
    def.value.scope = { ...def.value.scope, projects: [], organizations: [] }
  } else if (mode === 'orgs') {
    const first = audienceOrgId.value ?? opts?.organizations[0]?.id
    def.value.scope = { ...def.value.scope, projects: [], organizations: first ? [first] : [] }
  } else {
    def.value.scope = { ...def.value.scope, organizations: [], projects: (opts?.projects ?? []).slice(0, 1).map((p) => p.id) }
  }
}

function toggleScopeOrg(id: number, on: boolean) {
  const set = new Set(def.value.scope.organizations ?? [])
  if (on) set.add(id)
  else set.delete(id)
  def.value.scope.organizations = [...set]
}

const snapshot = () => JSON.stringify({ n: name.value.trim(), d: def.value, a: audience.value })
const dirty = computed(() => snapshot() !== savedSnapshot.value)
const problems = computed(() => definitionProblems(def.value, options.value?.limits))
const fields = computed(() => options.value?.fields ?? [])

const columnChoices = computed(() => {
  const used = new Set(def.value.columns)
  return (options.value?.columns ?? []).filter((c) => !used.has(c.value))
})
const sortChoices = computed(() => fields.value.filter((f) => f.sortable))
const columnLabel = (c: string) =>
  options.value?.columns.find((x) => x.value === c)?.label ?? (c.startsWith('cf:') ? `${c.slice(3)} (unavailable)` : c)

const unavailableNote = computed(() => {
  const u = results.value?.unavailable
  if (!u) return ''
  const parts: string[] = []
  if (u.projects?.length) parts.push(`${u.projects.length} project${u.projects.length === 1 ? '' : 's'}`)
  if (u.organizations?.length)
    parts.push(`${u.organizations.length} organization${u.organizations.length === 1 ? '' : 's'}`)
  if (u.sprints?.length) parts.push(`${u.sprints.length} sprint${u.sprints.length === 1 ? '' : 's'}`)
  if (u.users?.length) parts.push(`${u.users.length} ${u.users.length === 1 ? 'person' : 'people'}`)
  if (u.custom_fields?.length) parts.push(`${u.custom_fields.length} custom field${u.custom_fields.length === 1 ? '' : 's'}`)
  return parts.join(', ')
})

async function loadOptions() {
  options.value = await api.queryOptions()
}

async function load() {
  loading.value = true
  loadError.value = ''
  results.value = null
  runError.value = ''
  page.value = 1
  try {
    if (isNew.value) {
      view.value = null
      name.value = ''
      def.value = draftFromHistory() ?? emptyDefinition()
      await loadOptions()
      audience.value = audienceFromRoute()
      editorOpen.value = true
    } else {
      const v = await api.getSavedView(viewId.value!)
      view.value = v
      audience.value = audienceOf(v)
      name.value = v.name
      def.value = cloneDefinition(v.definition)
      await loadOptions()
      // Open queries straight to their results; the editor is a click away.
      editorOpen.value = false
    }
    savedSnapshot.value = isNew.value ? '' : snapshot()
    if (!isNew.value) await run()
  } catch (err) {
    loadError.value = err instanceof APIError && err.status === 404 ? 'This query does not exist or you cannot open it.' : 'Failed to load the query.'
  } finally {
    loading.value = false
  }
}

/** A starting query handed over by "+ View" on the task list (current filters). */
function draftFromHistory(): QueryDefinition | null {
  try {
    const raw = (window.history.state as { draft?: string } | null)?.draft
    if (!raw) return null
    const d = JSON.parse(raw) as QueryDefinition
    return d && d.where && d.scope ? { ...emptyDefinition(), ...d } : null
  } catch {
    return null
  }
}

/** "?share=site" or "?share=org:<id>" opens a new query already shared. */
function audienceFromRoute(): string {
  const want = typeof route.query.share === 'string' ? route.query.share : ''
  if (want === 'site' && options.value?.can_share_site) return 'site'
  const org = want.startsWith('org:') ? Number(want.slice(4)) : NaN
  if (options.value?.organizations.some((o) => o.id === org)) return `org:${org}`
  return 'personal'
}

// Personal tasks without a project mean "the viewer's own", which rarely fits
// a shared query, so sharing a new query turns them off (it can be turned back on).
watch(audience, (to, from) => {
  if (!isNew.value || loading.value || to === from) return
  if (to !== 'personal' && from === 'personal') def.value.scope.include_personal = false
  else if (to === 'personal') def.value.scope.include_personal = true
})

watch(() => route.fullPath, (to, from) => {
  if (to !== from && (route.name === 'query' || route.name === 'query-new')) void load()
})

async function run(goToPage = page.value) {
  if (problems.value.length) {
    runError.value = problems.value[0]
    return
  }
  running.value = true
  runError.value = ''
  try {
    const clean = { ...def.value, where: pruneEmptyGroups(def.value.where) }
    // A saved query with no edits runs from the server copy, which reports
    // references you have lost access to instead of failing.
    if (view.value && !dirty.value) {
      results.value = await api.savedViewResults(view.value.id, goToPage)
    } else {
      results.value = await api.runQuery(clean, { page: goToPage })
    }
    page.value = goToPage
  } catch (err) {
    runError.value = err instanceof APIError ? err.message : 'The query failed.'
  } finally {
    running.value = false
  }
}

function setWhere(n: QueryNode) {
  def.value = { ...def.value, where: n }
}

function onSort(sort: QuerySortKey[]) {
  def.value = { ...def.value, sort }
  void run(1)
}

function toggleScopeProject(id: number, on: boolean) {
  const set = new Set(def.value.scope.projects)
  if (on) set.add(id)
  else set.delete(id)
  def.value.scope.projects = [...set]
}

function addColumn(c: string) {
  if (!c) return
  def.value.columns = [...def.value.columns, c]
}

function moveColumn(i: number, delta: number) {
  const cols = [...def.value.columns]
  const j = i + delta
  if (j < 0 || j >= cols.length) return
  ;[cols[i], cols[j]] = [cols[j], cols[i]]
  def.value.columns = cols
}

function removeColumn(i: number) {
  def.value.columns = def.value.columns.filter((_, k) => k !== i)
}

function addSortKey() {
  const used = new Set(def.value.sort.map((s) => s.field))
  const next = sortChoices.value.find((f) => !used.has(f.id))
  if (next) def.value.sort = [...def.value.sort, { field: next.id, dir: 'asc' }]
}

function payloadDefinition(): QueryDefinition {
  return { ...def.value, where: pruneEmptyGroups(def.value.where) }
}

async function save() {
  const trimmed = name.value.trim()
  if (!trimmed) {
    toast.push('Give the query a name first', 'error')
    return
  }
  if (problems.value.length) {
    toast.push(problems.value[0], 'error')
    return
  }
  saving.value = true
  try {
    if (isNew.value) {
      const created = await api.createSavedView({
        name: trimmed,
        definition: payloadDefinition(),
        scope: audienceScope.value,
        organization_id: audienceOrgId.value,
      })
      savedSnapshot.value = snapshot()
      toast.push('Query saved', 'success')
      await router.replace(`/views/${created.id}`)
    } else {
      const moved = audience.value !== audienceOf(view.value!)
      const updated = await api.updateSavedView(view.value!.id, {
        name: trimmed,
        definition: payloadDefinition(),
        ...(moved ? { scope: audienceScope.value, organization_id: audienceOrgId.value } : {}),
      })
      view.value = updated
      audience.value = audienceOf(updated)
      def.value = cloneDefinition(updated.definition)
      savedSnapshot.value = snapshot()
      toast.push('Query saved', 'success')
      await run(1)
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    saving.value = false
  }
}

/** Save a personal copy: handy for shared queries you cannot edit. */
async function saveCopy() {
  const base = name.value.trim() || 'Query'
  const copy = cloneDefinition(payloadDefinition())
  saving.value = true
  try {
    const created = await api.createSavedView({ name: `${base} (copy)`.slice(0, 80), definition: copy })
    savedSnapshot.value = snapshot()
    toast.push('Saved a personal copy', 'success')
    await router.push(`/views/${created.id}`)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save a copy', 'error')
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!view.value) return
  const ok = await askConfirm({
    title: 'Delete query?',
    message: view.value.shared
      ? `Delete the shared query “${view.value.name}”? ${audienceLabel(view.value)} will lose it.`
      : `Delete the query “${view.value.name}”?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteSavedView(view.value.id)
    savedSnapshot.value = snapshot()
    toast.push('Query deleted', 'info')
    await router.push('/views')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

async function toggleDashboard() {
  if (!view.value) return
  const on = !view.value.on_dashboard
  try {
    await api.setSavedViewOnDashboard(view.value.id, on)
    view.value = { ...view.value, on_dashboard: on }
    toast.push(on ? 'Pinned to your dashboard' : 'Removed from your dashboard', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not update the dashboard', 'error')
  }
}

async function exportCsv() {
  try {
    if (view.value && !dirty.value) await api.downloadSavedViewExport(view.value.id)
    else await api.downloadQueryExport(payloadDefinition())
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Export failed', 'error')
  }
}

async function confirmLeave(): Promise<boolean> {
  if (!dirty.value || !canEdit.value || loading.value || (isNew.value && !name.value.trim() && def.value.where.clauses?.length === 0)) {
    return true
  }
  return await askConfirm({
    title: 'Leave without saving?',
    message: 'Your changes to this query will be lost.',
    confirmLabel: 'Leave',
    danger: true,
  })
}

onBeforeRouteLeave(confirmLeave)
onBeforeRouteUpdate(confirmLeave)

onMounted(load)
</script>

<template>
  <div class="container-fluid mt-3 mb-5 query-editor">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
      <RouterLink to="/views" class="btn btn-sm btn-outline-secondary" title="All queries">
        <i class="bi bi-arrow-left" /> <span class="d-none d-sm-inline">Queries</span>
      </RouterLink>
      <h1 class="h4 mb-0 me-auto text-truncate">
        <i :class="isShared ? `${audienceIcon(audienceInfo)} me-1` : 'bi bi-funnel me-1'" aria-hidden="true" />
        {{ isNew ? 'New query' : view?.name || 'Query' }}
        <span v-if="isShared" class="badge text-bg-info fw-normal ms-1">Shared · {{ audienceLabel(audienceInfo) }}</span>
        <span v-if="dirty && !isNew && canEdit" class="badge text-bg-warning fw-normal ms-1">Unsaved changes</span>
      </h1>
      <div v-if="!loading && !loadError" class="d-flex flex-wrap gap-2">
        <button type="button" class="btn btn-sm btn-primary" :disabled="running" @click="run(1)">
          <span v-if="running" class="spinner-border spinner-border-sm me-1" aria-hidden="true" />
          <i v-else class="bi bi-play-fill" /> Run
        </button>
        <button v-if="canEdit" type="button" class="btn btn-sm btn-success" :disabled="saving || (!dirty && !isNew)" @click="save">
          <i class="bi bi-save" /> Save
        </button>
        <button v-if="!isNew" type="button" class="btn btn-sm btn-outline-secondary" :disabled="saving" @click="saveCopy">
          <i class="bi bi-files" /> {{ canEdit ? 'Save as copy' : 'Save a personal copy' }}
        </button>
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="exportCsv">
          <i class="bi bi-download" /> CSV
        </button>
        <button
          v-if="view"
          type="button"
          class="btn btn-sm"
          :class="view.on_dashboard ? 'btn-secondary' : 'btn-outline-secondary'"
          :aria-pressed="view.on_dashboard"
          @click="toggleDashboard"
        >
          <i class="bi bi-pin-angle" /> {{ view.on_dashboard ? 'On dashboard' : 'Pin to dashboard' }}
        </button>
        <button v-if="view && canEdit" type="button" class="btn btn-sm btn-outline-danger" @click="remove">
          <i class="bi bi-trash" /> <span class="visually-hidden">Delete</span>
        </button>
      </div>
    </div>

    <div v-if="loading" class="text-muted py-5 text-center">
      <span class="spinner-border spinner-border-sm me-2" aria-hidden="true" /> Loading…
    </div>
    <div v-else-if="loadError" class="alert alert-warning">{{ loadError }}</div>

    <template v-else-if="options">
      <p v-if="view && !canEdit" class="alert alert-light border small py-2">
        <i class="bi bi-lock me-1" aria-hidden="true" />
        Shared with {{ view.scope === 'site' ? 'everyone' : `the members of ${view.organization_name}` }} by
        {{ view.author_name || 'another user' }}.
        You can run and tweak it here, but
        {{ view.scope === 'site' ? 'only people who manage site-wide queries' : "only its owner and people who manage the organization's queries" }}
        can save changes. Use <strong>Save a personal copy</strong> to keep your version.
      </p>

      <div class="card mb-3">
        <div class="card-header d-flex align-items-center">
          <button
            type="button"
            class="btn btn-link p-0 text-reset text-decoration-none fw-semibold"
            :aria-expanded="editorOpen"
            aria-controls="query-editor-body"
            @click="editorOpen = !editorOpen"
          >
            <i :class="editorOpen ? 'bi bi-chevron-down' : 'bi bi-chevron-right'" aria-hidden="true" />
            Query definition
          </button>
        </div>
        <div v-show="editorOpen" id="query-editor-body" class="card-body">
          <div class="row g-3 mb-3">
            <div class="col-12 col-md-6">
              <label for="query-name" class="form-label small fw-semibold">Name</label>
              <input
                id="query-name"
                v-model="name"
                type="text"
                class="form-control form-control-sm"
                maxlength="80"
                placeholder="e.g. My open bugs due this week"
                :disabled="!canEdit"
              />
            </div>
            <div v-if="canChooseAudience" class="col-12 col-md-6">
              <label for="query-audience" class="form-label small fw-semibold">Who can use it</label>
              <select id="query-audience" v-model="audience" class="form-select form-select-sm">
                <option v-for="c in audienceChoices" :key="c.value" :value="c.value">{{ c.label }}</option>
              </select>
              <div class="form-text">
                {{ audienceHelp }}
                <template v-if="isShared">Each person runs it with their own access, so it never shows anyone a task they can't already open.</template>
              </div>
            </div>
          </div>

          <!-- Scope -->
          <fieldset class="mb-3">
            <legend class="small fw-semibold mb-1">Search in</legend>
            <div class="d-flex flex-wrap gap-3 mb-2">
              <div class="form-check">
                <input
                  id="scope-all"
                  class="form-check-input"
                  type="radio"
                  :checked="scopeMode === 'all'"
                  :disabled="!canEdit"
                  @change="setScopeMode('all')"
                />
                <label class="form-check-label small" for="scope-all">{{ isShared ? "All of the viewer's projects" : 'All my projects' }}</label>
              </div>
              <div v-if="options.organizations.length || scopeMode === 'orgs'" class="form-check">
                <input
                  id="scope-orgs"
                  class="form-check-input"
                  type="radio"
                  :checked="scopeMode === 'orgs'"
                  :disabled="!canEdit || options.organizations.length === 0"
                  @change="setScopeMode('orgs')"
                />
                <label class="form-check-label small" for="scope-orgs">Projects in organizations</label>
              </div>
              <div class="form-check">
                <input
                  id="scope-some"
                  class="form-check-input"
                  type="radio"
                  :checked="scopeMode === 'projects'"
                  :disabled="!canEdit || options.projects.length === 0"
                  @change="setScopeMode('projects')"
                />
                <label class="form-check-label small" for="scope-some">Selected projects</label>
              </div>
            </div>
            <div v-if="scopeMode === 'orgs'" class="scope-projects border rounded p-2 mb-2">
              <div v-for="o in options.organizations" :key="o.id" class="form-check">
                <input
                  :id="`scope-o-${o.id}`"
                  class="form-check-input"
                  type="checkbox"
                  :checked="(def.scope.organizations ?? []).includes(o.id)"
                  :disabled="!canEdit"
                  @change="toggleScopeOrg(o.id, ($event.target as HTMLInputElement).checked)"
                />
                <label class="form-check-label small" :for="`scope-o-${o.id}`">{{ o.name }}</label>
              </div>
              <div
                v-for="id in (def.scope.organizations ?? []).filter((id) => !options!.organizations.some((o) => o.id === id))"
                :key="`gone-o-${id}`"
                class="small text-warning"
              >
                An organization you don't belong to is in this list; it matches nothing for you.
                <button v-if="canEdit" type="button" class="btn btn-link btn-sm p-0" @click="toggleScopeOrg(id, false)">Remove</button>
              </div>
              <div class="form-text mt-1">Every project of these organizations that the person running the query can open.</div>
            </div>
            <div v-if="scopeMode === 'projects'" class="scope-projects border rounded p-2 mb-2">
              <div v-for="p in options.projects" :key="p.id" class="form-check">
                <input
                  :id="`scope-p-${p.id}`"
                  class="form-check-input"
                  type="checkbox"
                  :checked="def.scope.projects.includes(p.id)"
                  :disabled="!canEdit"
                  @change="toggleScopeProject(p.id, ($event.target as HTMLInputElement).checked)"
                />
                <label class="form-check-label small" :for="`scope-p-${p.id}`">
                  {{ p.name }}
                  <span v-if="p.archived" class="text-muted">(archived)</span>
                  <span v-if="p.view_only" class="text-muted">(view only)</span>
                </label>
              </div>
              <div
                v-for="id in def.scope.projects.filter((id) => !options!.projects.some((p) => p.id === id))"
                :key="`gone-${id}`"
                class="small text-warning"
              >
                A project you can no longer open is in this list; it matches nothing.
                <button v-if="canEdit" type="button" class="btn btn-link btn-sm p-0" @click="toggleScopeProject(id, false)">Remove</button>
              </div>
            </div>
            <div class="d-flex flex-wrap gap-3">
              <div class="form-check form-switch">
                <input id="scope-personal" v-model="def.scope.include_personal" class="form-check-input" type="checkbox" :disabled="!canEdit" />
                <label class="form-check-label small" for="scope-personal">{{ isShared ? "The viewer's tasks without a project" : 'My tasks without a project' }}</label>
              </div>
              <div class="form-check form-switch">
                <input id="scope-archived" v-model="def.scope.include_archived" class="form-check-input" type="checkbox" :disabled="!canEdit" />
                <label class="form-check-label small" for="scope-archived">Archived projects and tasks</label>
              </div>
              <div class="form-check form-switch">
                <input id="scope-viewonly" v-model="def.scope.exclude_view_only" class="form-check-input" type="checkbox" :disabled="!canEdit" />
                <label class="form-check-label small" for="scope-viewonly">Skip projects I can only view</label>
              </div>
            </div>
          </fieldset>

          <!-- Clauses -->
          <fieldset class="mb-3">
            <legend class="small fw-semibold mb-1">Clauses</legend>
            <QueryGroupEditor
              :node="def.where"
              :fields="fields"
              :options="options"
              :readonly="false"
              :max-depth="options.limits.depth"
              @update:node="setWhere"
            />
          </fieldset>

          <!-- Display -->
          <div class="row g-3">
            <div class="col-12 col-lg-6">
              <div class="small fw-semibold mb-1">Columns</div>
              <ol class="list-unstyled d-flex flex-wrap gap-1 mb-2" aria-label="Columns in order">
                <li v-for="(c, i) in def.columns" :key="c" class="badge text-bg-light border d-inline-flex align-items-center gap-1 fw-normal">
                  <button type="button" class="btn btn-link btn-sm p-0 text-reset" :aria-label="`Move ${columnLabel(c)} left`" :disabled="i === 0" @click="moveColumn(i, -1)">
                    <i class="bi bi-chevron-left" />
                  </button>
                  {{ columnLabel(c) }}
                  <button type="button" class="btn btn-link btn-sm p-0 text-reset" :aria-label="`Move ${columnLabel(c)} right`" :disabled="i === def.columns.length - 1" @click="moveColumn(i, 1)">
                    <i class="bi bi-chevron-right" />
                  </button>
                  <button type="button" class="btn btn-link btn-sm p-0 text-reset" :aria-label="`Remove column ${columnLabel(c)}`" @click="removeColumn(i)">
                    <i class="bi bi-x" />
                  </button>
                </li>
              </ol>
              <label for="query-add-column" class="visually-hidden">Add column</label>
              <select
                id="query-add-column"
                class="form-select form-select-sm w-auto"
                :disabled="columnChoices.length === 0 || def.columns.length >= (options.limits.columns ?? 25)"
                @change="addColumn(($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''"
              >
                <option value="">Add column…</option>
                <option v-for="c in columnChoices" :key="c.value" :value="c.value">{{ c.label }}</option>
              </select>
            </div>
            <div class="col-12 col-md-6 col-lg-3">
              <div class="small fw-semibold mb-1">Sort by</div>
              <div v-for="(s, i) in def.sort" :key="i" class="d-flex gap-1 mb-1">
                <label :for="`sort-f-${i}`" class="visually-hidden">Sort field {{ i + 1 }}</label>
                <select :id="`sort-f-${i}`" v-model="s.field" class="form-select form-select-sm">
                  <option v-for="f in sortChoices" :key="f.id" :value="f.id">{{ f.label }}</option>
                </select>
                <label :for="`sort-d-${i}`" class="visually-hidden">Sort direction {{ i + 1 }}</label>
                <select :id="`sort-d-${i}`" v-model="s.dir" class="form-select form-select-sm w-auto">
                  <option value="asc">↑ Asc</option>
                  <option value="desc">↓ Desc</option>
                </select>
                <button type="button" class="btn btn-sm btn-outline-secondary border-0" :aria-label="`Remove sort ${i + 1}`" @click="def.sort.splice(i, 1)">
                  <i class="bi bi-x-lg" />
                </button>
              </div>
              <button v-if="def.sort.length < (options.limits.sort_keys ?? 3)" type="button" class="btn btn-sm btn-outline-secondary" @click="addSortKey">
                <i class="bi bi-plus-lg" /> Sort key
              </button>
              <div v-if="def.sort.length === 0" class="form-text">Default: by ID.</div>
            </div>
            <div class="col-12 col-md-6 col-lg-3">
              <div class="small fw-semibold mb-1">Show results as</div>
              <div class="form-check">
                <input id="mode-flat" v-model="def.mode" class="form-check-input" type="radio" value="flat" />
                <label class="form-check-label small" for="mode-flat">Flat list of every matching task</label>
              </div>
              <div class="form-check">
                <input id="mode-tree" v-model="def.mode" class="form-check-input" type="radio" value="tree" />
                <label class="form-check-label small" for="mode-tree">Tree: matching subtasks under matching parents</label>
              </div>
            </div>
          </div>

          <ul v-if="problems.length" class="small text-danger mt-3 mb-0">
            <li v-for="p in problems" :key="p">{{ p }}</li>
          </ul>
        </div>
      </div>

      <div class="card">
        <div class="card-header d-flex align-items-center gap-2">
          <span class="fw-semibold">Results</span>
          <span v-if="results" class="badge text-bg-secondary">{{ results.total }}</span>
          <span v-if="results?.mode === 'tree'" class="small text-muted">top-level rows</span>
        </div>
        <div class="card-body">
          <div v-if="unavailableNote" class="alert alert-warning small py-2">
            <i class="bi bi-exclamation-triangle me-1" aria-hidden="true" />
            This query refers to {{ unavailableNote }} you can no longer access. Those parts match nothing.
          </div>
          <div v-if="runError" class="alert alert-danger small py-2" role="alert">{{ runError }}</div>
          <QueryResultsTable
            v-if="results"
            :results="results"
            :options="options"
            :sort="def.sort"
            :loading="running"
            @page="run($event)"
            @sort="onSort"
          />
          <p v-else-if="!runError" class="text-muted small mb-0">Build your query, then press <strong>Run</strong>.</p>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.scope-projects {
  max-height: 12rem;
  overflow-y: auto;
}
</style>
