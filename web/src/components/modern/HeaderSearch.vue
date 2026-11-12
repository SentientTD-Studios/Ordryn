<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Task } from '@/api/types'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { headerSearchItems, HEADER_SEARCH_PROJECTS, HEADER_SEARCH_TASKS, wantsLiveResults, type HeaderSearchItem } from '@/utils/headerSearch'
import { projectRoute, taskPageRoute } from '@/utils/projectRoute'
import { freeTextTerms, highlightSegments } from '@/utils/search'

// The header search box. As you type it shows live results that match task
// titles and ids; Enter (with nothing picked) runs the full search, which also
// looks at descriptions, tags and comments.

const router = useRouter()
const route = useRoute()
const sidebar = useTaskSidebar()

const query = ref('')
const focused = ref(false)
const highlight = ref(-1)
const tasks = ref<Task[]>([])
const projects = ref<Project[]>([])
const loading = ref(false)
// The query the shown results are for, so stale results never show under new text.
const resultsFor = ref('')
const inputEl = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)

const open = computed(() => focused.value && query.value.trim() !== '')
const fresh = computed(() => resultsFor.value === query.value.trim())
const items = computed(() =>
  headerSearchItems(query.value, fresh.value ? tasks.value : [], fresh.value ? projects.value : []),
)
const terms = computed(() => freeTextTerms(query.value))
const noTitleMatches = computed(
  () => fresh.value && !loading.value && wantsLiveResults(query.value) && !tasks.value.length && !projects.value.length,
)

let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0
watch(query, (raw) => {
  highlight.value = -1
  clearTimeout(timer)
  const q = raw.trim()
  if (!wantsLiveResults(q)) {
    seq++
    tasks.value = []
    projects.value = []
    resultsFor.value = q
    loading.value = false
    return
  }
  loading.value = true
  timer = setTimeout(async () => {
    const mine = ++seq
    try {
      const res = await api.search({ q, per_page: HEADER_SEARCH_TASKS, scope: 'titles' })
      if (mine !== seq) return
      tasks.value = res.tasks
      projects.value = res.projects.slice(0, HEADER_SEARCH_PROJECTS)
    } catch {
      if (mine !== seq) return
      tasks.value = []
      projects.value = []
    }
    resultsFor.value = q
    loading.value = false
  }, 150)
})
onBeforeUnmount(() => clearTimeout(timer))

function reset() {
  query.value = ''
  highlight.value = -1
  inputEl.value?.blur()
}

// Leaving the page (a result, or any other link) closes the dropdown.
watch(() => route.fullPath, () => {
  if (focused.value) reset()
})

function runItem(item: HeaderSearchItem | undefined) {
  if (!item) return
  reset()
  switch (item.kind) {
    case 'open-id':
      void router.push(taskPageRoute(item.taskId))
      break
    case 'task':
      void sidebar.openTask(item.task.id)
      break
    case 'project':
      void router.push(projectRoute(item.project))
      break
    case 'all':
      void router.push({ name: 'search', query: { q: item.query } })
      break
  }
}

function searchEverything() {
  const q = query.value.trim()
  reset()
  void router.push({ name: 'search', query: q ? { q } : {} })
}

function onKeydown(e: KeyboardEvent) {
  const n = items.value.length
  if (e.key === 'ArrowDown' && n) {
    e.preventDefault()
    highlight.value = (highlight.value + 1) % n
  } else if (e.key === 'ArrowUp' && n) {
    e.preventDefault()
    highlight.value = highlight.value <= 0 ? n - 1 : highlight.value - 1
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (highlight.value >= 0) runItem(items.value[highlight.value])
    else searchEverything()
    return
  } else if (e.key === 'Escape') {
    e.preventDefault()
    if (highlight.value >= 0) highlight.value = -1
    else reset()
    return
  } else {
    return
  }
  void nextTick(() => listEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
}

function taskDetail(t: Task): string {
  return [t.project || 'Personal', t.parent_title].filter(Boolean).join(' › ')
}
</script>

<template>
  <div class="position-relative" role="search">
    <i class="bi bi-search position-absolute top-50 start-0 translate-middle-y ms-3 text-muted" aria-hidden="true" />
    <input
      id="global-search"
      ref="inputEl"
      v-model="query"
      type="search"
      class="form-control form-control-sm ps-5 rounded-pill border-0"
      style="background: var(--ordryn-muted-bg); color: var(--ordryn-text);"
      placeholder="Search tasks, #id…"
      aria-label="Search tasks and projects"
      role="combobox"
      aria-autocomplete="list"
      aria-controls="header-search-results"
      :aria-expanded="open"
      :aria-activedescendant="open && items[highlight] ? `hs-${items[highlight]!.key}` : undefined"
      autocomplete="off"
      spellcheck="false"
      @focus="focused = true"
      @blur="focused = false"
      @keydown="onKeydown"
    />
    <div
      v-show="open"
      id="header-search-results"
      ref="listEl"
      class="dropdown-menu show w-100 shadow p-1 header-search-results"
      role="listbox"
      aria-label="Search results"
    >
      <div v-if="loading && !fresh" class="small text-muted px-2 py-2 d-flex align-items-center gap-2">
        <span class="spinner-border spinner-border-sm" aria-hidden="true" /> Searching titles…
      </div>
      <p v-else-if="noTitleMatches" class="small text-muted px-2 py-2 mb-0">No titles match.</p>
      <!-- mousedown.prevent keeps focus in the box so the click lands before blur closes the list -->
      <div
        v-for="(item, index) in items"
        :id="`hs-${item.key}`"
        :key="item.key"
        role="option"
        :aria-selected="index === highlight"
        class="header-search-item d-flex align-items-center gap-2 px-2 py-1 rounded"
        :class="{ active: index === highlight, 'border-top mt-1 pt-2': item.kind === 'all' }"
        @mousedown.prevent
        @mousemove="highlight = index"
        @click="runItem(item)"
      >
        <template v-if="item.kind === 'task'">
          <i class="bi flex-shrink-0" :class="item.task.completed ? 'bi-check-circle-fill text-success' : 'bi-circle text-muted'" :aria-label="item.task.completed ? 'Done' : 'Open'" />
          <span class="flex-grow-1 min-w-0">
            <span class="d-block text-truncate" :class="{ 'text-decoration-line-through opacity-75': item.task.completed }">
              <template v-for="(seg, i) in highlightSegments(item.task.title, terms)" :key="i">
                <mark v-if="seg.match" class="header-search-mark">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template>
              </template>
            </span>
            <span class="d-block small text-truncate header-search-detail">{{ taskDetail(item.task) }}</span>
          </span>
          <span class="small font-monospace header-search-detail flex-shrink-0">#{{ item.task.id }}</span>
        </template>
        <template v-else-if="item.kind === 'project'">
          <i class="bi bi-folder2 flex-shrink-0" aria-hidden="true" />
          <span class="flex-grow-1 text-truncate">
            <template v-for="(seg, i) in highlightSegments(item.project.name, terms)" :key="i">
              <mark v-if="seg.match" class="header-search-mark">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template>
            </template>
          </span>
          <span class="small header-search-detail flex-shrink-0">Project</span>
        </template>
        <template v-else-if="item.kind === 'open-id'">
          <i class="bi bi-hash flex-shrink-0" aria-hidden="true" />
          <span class="flex-grow-1 text-truncate">Open task #{{ item.taskId }}</span>
        </template>
        <template v-else>
          <i class="bi bi-search flex-shrink-0" aria-hidden="true" />
          <span class="flex-grow-1 text-truncate">Search everything for “{{ item.query }}”</span>
          <kbd class="small header-search-detail flex-shrink-0">Enter</kbd>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.header-search-results {
  top: calc(100% + 0.25rem);
  left: 0;
  min-width: min(28rem, 92vw);
  max-height: min(70vh, 30rem);
  overflow-y: auto;
  z-index: 1060;
}
.header-search-item {
  cursor: pointer;
  color: var(--ordryn-text);
}
.header-search-item.active {
  background: var(--bs-primary);
  color: #fff;
}
.header-search-detail {
  color: var(--ordryn-muted);
}
.header-search-item.active .header-search-detail {
  color: inherit;
  opacity: 0.8;
}
.header-search-mark {
  padding: 0;
  background: var(--ordryn-accent-light, #fff3cd);
  color: inherit;
  border-radius: 2px;
}
.header-search-item.active .header-search-mark {
  background: rgba(255, 255, 255, 0.25);
}
</style>
