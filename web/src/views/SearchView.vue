<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, SavedView, Task } from '@/api/types'
import { APIError } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import AppFooter from '@/components/AppFooter.vue'
import { useSidebarState } from '@/composables/useSidebarState'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useLiveUpdates } from '@/composables/useLiveUpdates'
import { projectRoute, taskPageRoute } from '@/utils/projectRoute'
import { freeTextTerms, highlightSegments, matchedInComments, searchExcerpt } from '@/utils/search'

const PER_PAGE = 25

const route = useRoute()
const router = useRouter()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()
const { openTask, lastSavedTask, lastDeletedTask } = useTaskSidebar()

const projects = ref<Project[]>([])
const savedViews = ref<SavedView[]>([])
const query = ref(typeof route.query.q === 'string' ? route.query.q : '')
const projectScope = ref(typeof route.query.project === 'string' ? route.query.project : '')
const tasks = ref<Task[]>([])
const matchedProjects = ref<Project[]>([])
const total = ref(0)
const page = ref(0)
const totalPages = ref(0)
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')
const searchedFor = ref('')
const inputEl = ref<HTMLInputElement | null>(null)
let seq = 0

const terms = computed(() => freeTextTerms(searchedFor.value))
const projectById = computed(() => new Map(projects.value.map((p) => [p.id, p])))
// Archived projects only appear in the picker once they're the current scope.
const scopeOptions = computed(() => projects.value.filter((p) => !p.archived || String(p.id) === projectScope.value))
const scopedProject = computed(() => projectById.value.get(Number(projectScope.value)) ?? null)
const hasMore = computed(() => page.value < totalPages.value)

const TIPS: { example: string; text: string }[] = [
  { example: '#123', text: 'open a task by its id' },
  { example: 'recieve', text: 'typos in titles are forgiven (words of 5+ letters)' },
  { example: '"exact phrase"', text: 'match words together' },
  { example: 'project:web', text: 'projects whose name contains "web"' },
  { example: 'tag:bug', text: 'tasks with a matching tag' },
  { example: 'status:review', text: 'workflow status name' },
  { example: 'is:open', text: 'or is:done' },
  { example: 'claimed:me', text: 'or claimed:none' },
  { example: 'is:archived', text: 'include archived tasks and projects' },
]

async function run(reset = true) {
  const q = query.value.trim()
  const mySeq = ++seq
  error.value = ''
  if (!q) {
    tasks.value = []
    matchedProjects.value = []
    total.value = 0
    page.value = 0
    totalPages.value = 0
    searchedFor.value = ''
    loading.value = false
    return
  }
  const nextPage = reset ? 1 : page.value + 1
  if (reset) loading.value = true
  else loadingMore.value = true
  try {
    const res = await api.search({
      q,
      project: projectScope.value ? Number(projectScope.value) : undefined,
      page: nextPage,
      per_page: PER_PAGE,
    })
    if (mySeq !== seq) return
    searchedFor.value = q
    if (reset) {
      tasks.value = res.tasks
      matchedProjects.value = res.projects
    } else {
      const seen = new Set(tasks.value.map((t) => t.id))
      tasks.value = [...tasks.value, ...res.tasks.filter((t) => !seen.has(t.id))]
    }
    total.value = res.total
    page.value = res.page
    totalPages.value = res.total_pages
  } catch (err) {
    if (mySeq !== seq) return
    if (err instanceof APIError && err.status === 404 && projectScope.value) {
      // The scoped project is gone or no longer shared: search everything instead.
      projectScope.value = ''
      return
    }
    error.value = err instanceof APIError ? err.message : 'Search failed'
  } finally {
    if (mySeq === seq) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

function syncUrl() {
  const q: Record<string, string> = {}
  if (query.value.trim()) q.q = query.value.trim()
  if (projectScope.value) q.project = projectScope.value
  void router.replace({ query: q })
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(query, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    syncUrl()
    void run()
  }, 250)
})

watch(projectScope, () => {
  syncUrl()
  void run()
})

// The header search box navigates here with a new ?q= while the page is open.
watch(
  () => route.query.q,
  (q) => {
    const next = typeof q === 'string' ? q : ''
    if (next.trim() !== query.value.trim()) query.value = next
  },
)

function submit() {
  clearTimeout(timer)
  syncUrl()
  void run()
}

function addTip(example: string) {
  const cur = query.value.trim()
  query.value = cur ? `${cur} ${example}` : example
  void nextTick(() => inputEl.value?.focus())
}

function projectFor(task: Task): Project | null {
  return task.project_id ? (projectById.value.get(task.project_id) ?? null) : null
}

function formatDue(date?: string) {
  if (!date) return ''
  const d = new Date(date + 'T00:00:00')
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function isOverdue(task: Task) {
  if (!task.due_date || task.completed) return false
  const today = new Date()
  const iso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
  return task.due_date < iso
}

function refresh() {
  if (searchedFor.value) void run()
}

watch(lastSavedTask, refresh)
watch(lastDeletedTask, (payload) => {
  if (!payload) return
  refresh()
})

let liveTimer: ReturnType<typeof setTimeout> | undefined
useLiveUpdates((_event, batch) => {
  if (batch.every((e) => e.type === 'task.commented')) return
  clearTimeout(liveTimer)
  liveTimer = setTimeout(refresh, 800)
})

onMounted(async () => {
  inputEl.value?.focus()
  inputEl.value?.select()
  void run()
  try {
    const [projList, viewList] = await Promise.all([api.listProjects(), api.listSavedViews()])
    projects.value = projList
    savedViews.value = viewList
  } catch {
    /* the sidebar and project names are optional here */
  }
})
</script>

<template>
  <div class="ordryn-main-layout">
    <ModernSidebar
      :collapsed="sidebarCollapsed"
      :projects="projects"
      :saved-views="savedViews"
      @toggle-collapse="toggleSidebar"
      @select-home="router.push('/')"
      @select-project="(id: string) => router.push(projectRoute(id))"
      @add-project="router.push('/projects')"
      @edit-project="() => router.push('/projects')"
      @add-view="router.push('/views/new')"
    />

    <div class="flex-grow-1 p-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div class="search-page">
        <h1 class="h3 fw-bold mb-3">Search</h1>

        <form class="d-flex flex-column flex-sm-row gap-2 mb-2" role="search" @submit.prevent="submit">
          <div class="position-relative flex-grow-1">
            <i class="bi bi-search position-absolute top-50 start-0 translate-middle-y ms-3 text-muted" aria-hidden="true" />
            <input
              id="site-search-page"
              ref="inputEl"
              v-model="query"
              type="search"
              class="form-control ps-5"
              placeholder="Search tasks by title, description, tag, or #id"
              aria-label="Search tasks and projects"
              autocomplete="off"
              spellcheck="false"
            />
          </div>
          <select v-model="projectScope" class="form-select search-scope" aria-label="Search in">
            <option value="">All projects</option>
            <option v-for="p in scopeOptions" :key="p.id" :value="String(p.id)">
              {{ p.name }}{{ p.archived ? ' (archived)' : '' }}
            </option>
          </select>
        </form>

        <details class="small text-muted mb-3">
          <summary class="search-tips-summary">Search tips</summary>
          <p class="mt-2 mb-2">
            Every word must match somewhere in a task's title, description, or tags. Partial words match too,
            and completed tasks and subtasks are included. Narrow it down with:
          </p>
          <div class="d-flex flex-wrap gap-2">
            <button
              v-for="tip in TIPS"
              :key="tip.example"
              type="button"
              class="btn btn-sm btn-outline-secondary search-tip"
              :title="`Add ${tip.example} to the search`"
              @click="addTip(tip.example)"
            >
              <code>{{ tip.example }}</code> <span class="text-muted">{{ tip.text }}</span>
            </button>
          </div>
        </details>

        <div v-if="error" class="alert alert-danger py-2">{{ error }}</div>

        <div v-if="!searchedFor && !loading" class="text-muted py-4">
          <p class="mb-1">Search every task and project you can open.</p>
          <p class="small mb-0">Tip: press <kbd>/</kbd> anywhere to jump to search, or <kbd>Ctrl</kbd>+<kbd>K</kbd> for quick results.</p>
        </div>

        <div v-else-if="loading && !tasks.length" class="text-muted py-4">
          <span class="spinner-border spinner-border-sm me-2" role="status" />Searching…
        </div>

        <template v-else-if="searchedFor">
          <section v-if="matchedProjects.length" class="mb-3" aria-label="Matching projects">
            <h2 class="h6 text-uppercase text-muted small fw-semibold mb-2">Projects</h2>
            <div class="d-flex flex-wrap gap-2">
              <RouterLink
                v-for="p in matchedProjects"
                :key="p.id"
                :to="projectRoute(p)"
                class="btn btn-sm btn-outline-primary d-inline-flex align-items-center gap-1"
              >
                <i class="bi bi-folder2" aria-hidden="true" />
                <template v-for="(seg, i) in highlightSegments(p.name, terms)" :key="i">
                  <mark v-if="seg.match" class="search-mark">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template>
                </template>
                <span v-if="p.archived" class="badge bg-secondary ms-1">Archived</span>
              </RouterLink>
            </div>
          </section>

          <h2 class="h6 text-uppercase text-muted small fw-semibold mb-2" aria-live="polite">
            {{ total }} {{ total === 1 ? 'task' : 'tasks' }}
            <template v-if="scopedProject"> in {{ scopedProject.name }}</template>
          </h2>

          <p v-if="!tasks.length" class="text-muted">
            No tasks match <strong>{{ searchedFor }}</strong>.
            <template v-if="scopedProject">
              <button type="button" class="btn btn-link btn-sm p-0 align-baseline" @click="projectScope = ''">
                Search all projects
              </button>
            </template>
          </p>

          <ul v-else class="list-group search-results mb-3">
            <li v-for="t in tasks" :key="t.id" class="list-group-item list-group-item-action p-0">
              <div class="d-flex align-items-start gap-2 px-3 py-2">
                <button
                  type="button"
                  class="search-hit flex-grow-1 min-w-0 text-start border-0 bg-transparent p-0"
                  @click="openTask(t.id)"
                >
                  <div class="d-flex align-items-center gap-2 min-w-0">
                    <i
                      class="bi flex-shrink-0"
                      :class="t.completed ? 'bi-check-circle-fill text-success' : 'bi-circle text-muted'"
                      :aria-label="t.completed ? 'Done' : 'Open'"
                    />
                    <span class="text-muted small font-monospace flex-shrink-0">#{{ t.id }}</span>
                    <span class="fw-medium text-truncate" :class="{ 'text-decoration-line-through text-muted': t.completed }">
                      <template v-for="(seg, i) in highlightSegments(t.title, terms)" :key="i">
                        <mark v-if="seg.match" class="search-mark">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template>
                      </template>
                    </span>
                  </div>
                  <div
                    v-if="searchExcerpt(t.description, terms)"
                    class="small text-muted text-truncate mt-1"
                  >
                    <template v-for="(seg, i) in highlightSegments(searchExcerpt(t.description, terms), terms)" :key="i">
                      <mark v-if="seg.match" class="search-mark">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template>
                    </template>
                  </div>
                  <div class="d-flex flex-wrap align-items-center gap-2 mt-1 small text-muted">
                    <span v-if="matchedInComments(t, terms)"><i class="bi bi-chat-left-text me-1" aria-hidden="true" />Matched in comments</span>
                    <span v-if="t.project"><i class="bi bi-folder2 me-1" aria-hidden="true" />{{ t.project }}</span>
                    <span v-else><i class="bi bi-person me-1" aria-hidden="true" />Personal</span>
                    <span v-if="t.parent_title" class="text-truncate" style="max-width: 16rem">
                      <i class="bi bi-diagram-2 me-1" aria-hidden="true" />{{ t.parent_title }}
                    </span>
                    <span v-if="t.status_name" class="badge rounded-pill text-bg-light border">{{ t.status_name }}</span>
                    <span v-if="t.claimed_by_name"><i class="bi bi-person-check me-1" aria-hidden="true" />{{ t.claimed_by_name }}</span>
                    <span v-if="t.due_date" :class="{ 'text-danger': isOverdue(t) }">
                      <i class="bi bi-calendar-event me-1" aria-hidden="true" />{{ formatDue(t.due_date) }}
                    </span>
                    <span
                      v-for="tag in t.tags"
                      :key="tag.id"
                      class="badge rounded-pill"
                      :style="{ backgroundColor: tag.color || '#6c757d' }"
                    >{{ tag.name }}</span>
                  </div>
                </button>
                <RouterLink
                  v-if="projectFor(t)"
                  :to="taskPageRoute(t.id, projectFor(t))"
                  class="btn btn-sm btn-link text-muted flex-shrink-0"
                  :title="`Open on the ${t.project} board`"
                  :aria-label="`Open task ${t.id} on the ${t.project} board`"
                >
                  <i class="bi bi-box-arrow-up-right" />
                </RouterLink>
              </div>
            </li>
          </ul>

          <div v-if="hasMore" class="text-center mb-3">
            <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="loadingMore" @click="run(false)">
              <span v-if="loadingMore" class="spinner-border spinner-border-sm me-1" />Show more
            </button>
          </div>
        </template>
      </div>
      <AppFooter />
    </div>
  </div>
</template>

<style scoped>
.search-page {
  max-width: 60rem;
}
.search-scope {
  max-width: 100%;
}
@media (min-width: 576px) {
  .search-scope {
    width: 14rem;
  }
}
.search-tips-summary {
  cursor: pointer;
  width: fit-content;
}
.search-tip {
  font-size: 0.8rem;
}
.search-hit {
  color: inherit;
}
.search-hit:focus-visible {
  outline: 2px solid var(--ordryn-accent, #0d6efd);
  outline-offset: 2px;
}
.search-mark {
  padding: 0;
  background: var(--ordryn-accent-light, #fff3cd);
  color: inherit;
  border-radius: 2px;
}
</style>
