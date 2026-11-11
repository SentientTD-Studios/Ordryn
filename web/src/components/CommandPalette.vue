<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, Task } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { showShortcutsHelp } from '@/composables/useKeyboardShortcuts'
import { useProjectNav } from '@/composables/useProjectNav'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useTheme } from '@/composables/useTheme'
import { rankItems, taskNumber, type PaletteItem } from '@/utils/commandPalette'
import { searchProjects } from '@/utils/projectNav'
import { projectRoute, taskPageRoute } from '@/utils/projectRoute'

type Item = PaletteItem & { icon: string; hint?: string; run: () => void }

const palette = useCommandPalette()
const route = useRoute()
const router = useRouter()
const { adminLanding, managesPeople, canCreateProjects } = useAuth()
const projectNav = useProjectNav()
const sidebar = useTaskSidebar()
const { toggleTheme } = useTheme()

const query = ref('')
const highlight = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
const projects = ref<Project[]>([])
const tasks = ref<Task[]>([])
const searching = ref(false)
let returnFocus: HTMLElement | null = null

const currentProjectId = computed(() => {
  const ref = route.params.ref
  if (typeof ref !== 'string') return null
  const p = projects.value.find((x) => String(x.id) === ref || x.slug === ref)
  return p?.id ?? null
})

function go(to: Parameters<typeof router.push>[0]) {
  return () => void router.push(to)
}

const actions = computed<Item[]>(() => {
  const list: Item[] = [
    {
      id: 'new-task', label: 'New task', keywords: 'create add todo', group: 'Actions', icon: 'bi-plus-lg', hint: 'n',
      run: () => sidebar.openAdd(undefined, currentProjectId.value),
    },
    { id: 'find-project', label: 'Find a project…', keywords: 'switch jump', group: 'Actions', icon: 'bi-folder2-open',
      run: () => projectNav.openSwitcher() },
    { id: 'theme', label: 'Toggle light/dark theme', keywords: 'dark light appearance', group: 'Actions', icon: 'bi-circle-half',
      run: () => toggleTheme() },
    { id: 'shortcuts', label: 'Keyboard shortcuts', keywords: 'help keys', group: 'Actions', icon: 'bi-keyboard', hint: '?',
      run: () => showShortcutsHelp() },
  ]
  if (canCreateProjects.value) {
    list.push({ id: 'new-project', label: 'New project', keywords: 'create add', group: 'Actions', icon: 'bi-folder-plus',
      run: go({ path: '/', query: { new_project: '1' } }) })
  }
  const pages: [string, string, string, string][] = [
    ['home', 'Home', '/', 'bi-house'],
    ['dashboard', 'Dashboard', '/dashboard', 'bi-speedometer2'],
    ['calendar', 'Calendar', '/calendar', 'bi-calendar3'],
    ['team', 'Team dashboard', '/team', 'bi-people'],
    ['projects', 'Projects', '/projects', 'bi-folder'],
    ['views', 'Saved views', '/views', 'bi-funnel'],
    ['trash', 'Trash', '/trash', 'bi-trash'],
    ['settings', 'Settings', '/settings', 'bi-gear'],
    ['guide', 'User guide', '/docs/guide', 'bi-book'],
    ['api-docs', 'API docs', '/docs/api/v2', 'bi-code-slash'],
  ]
  if (managesPeople.value) pages.push(['people', 'People', '/people', 'bi-person-badge'])
  if (adminLanding.value) pages.push(['admin', 'Admin', adminLanding.value, 'bi-shield-lock'])
  for (const [id, label, path, icon] of pages) {
    list.push({ id: `page-${id}`, label: `Go to ${label}`, keywords: label, group: 'Pages', icon, run: go(path) })
  }
  return list
})

const projectItems = computed<Item[]>(() =>
  searchProjects(projects.value.filter((p) => !p.archived), query.value)
    .slice(0, 6)
    .map((p) => ({ id: `project-${p.id}`, label: p.name, group: 'Projects', icon: 'bi-folder2', run: go(projectRoute(p)) })),
)

const taskItems = computed<Item[]>(() => {
  const out: Item[] = []
  const n = taskNumber(query.value)
  if (n !== null) {
    out.push({ id: `task-open-${n}`, label: `Open task #${n}`, group: 'Tasks', icon: 'bi-hash', run: go(taskPageRoute(n)) })
  }
  for (const t of tasks.value) {
    if (t.id === n) continue
    out.push({
      id: `task-${t.id}`, label: t.title, hint: `#${t.id}`, group: 'Tasks',
      icon: t.completed ? 'bi-check-circle' : 'bi-circle', run: () => sidebar.openView(t.id),
    })
  }
  return out
})

const items = computed<Item[]>(() => {
  const q = query.value.trim()
  const ranked = rankItems(actions.value, q)
  return q ? [...taskItems.value, ...projectItems.value, ...ranked] : ranked
})

const groups = computed(() => {
  const out: { label: string; items: { item: Item; index: number }[] }[] = []
  items.value.forEach((item, index) => {
    let g = out.find((x) => x.label === item.group)
    if (!g) out.push((g = { label: item.group, items: [] }))
    g.items.push({ item, index })
  })
  return out
})

let searchTimer: ReturnType<typeof setTimeout> | undefined
let searchSeq = 0
watch(query, (q) => {
  highlight.value = 0
  clearTimeout(searchTimer)
  const term = q.trim()
  if (term.length < 2 || taskNumber(term) !== null) {
    tasks.value = []
    return
  }
  searchTimer = setTimeout(async () => {
    const seq = ++searchSeq
    searching.value = true
    try {
      const res = await api.listTasks({ search: term, include_subtasks: 1, per_page: 8, status: 'incomplete' })
      if (seq === searchSeq) tasks.value = res.tasks.slice(0, 8)
    } catch {
      if (seq === searchSeq) tasks.value = []
    } finally {
      if (seq === searchSeq) searching.value = false
    }
  }, 180)
})

watch(
  () => palette.open.value,
  async (open) => {
    if (!open) {
      returnFocus?.focus?.()
      return
    }
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    query.value = ''
    tasks.value = []
    highlight.value = 0
    await nextTick()
    inputEl.value?.focus()
    try {
      projects.value = await api.listProjects()
    } catch {
      projects.value = []
    }
  },
)

function runAt(index: number) {
  const item = items.value[index]
  if (!item) return
  palette.close()
  item.run()
}

function onKeydown(e: KeyboardEvent) {
  const n = items.value.length
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    highlight.value = n ? (highlight.value + 1) % n : 0
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    highlight.value = n ? (highlight.value - 1 + n) % n : 0
  } else if (e.key === 'Enter') {
    e.preventDefault()
    runAt(highlight.value)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    palette.close()
    return
  } else {
    return
  }
  void nextTick(() => listEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
}
</script>

<template>
  <Teleport to="body">
    <div v-if="palette.open.value" class="modal fade show d-block" tabindex="-1" role="dialog" aria-modal="true" aria-label="Command palette" @click.self="palette.close()">
      <div class="modal-dialog modal-dialog-scrollable" style="margin-top: 12vh" @click.stop>
        <div class="modal-content">
          <div class="modal-header p-2">
            <i class="bi bi-search text-muted ms-2" aria-hidden="true" />
            <input
              ref="inputEl"
              v-model="query"
              type="text"
              class="form-control border-0 shadow-none"
              placeholder="Search tasks and projects, jump to #123, or run a command"
              role="combobox"
              aria-expanded="true"
              aria-controls="command-palette-list"
              :aria-activedescendant="items[highlight] ? `cmd-${items[highlight]!.id}` : undefined"
              autocomplete="off"
              spellcheck="false"
              @keydown="onKeydown"
            />
            <span v-if="searching" class="spinner-border spinner-border-sm text-muted me-2" role="status" aria-label="Searching" />
          </div>
          <div id="command-palette-list" ref="listEl" class="modal-body p-1" role="listbox" style="max-height: 55vh">
            <div v-for="g in groups" :key="g.label" class="mb-1">
              <div class="small text-muted text-uppercase px-2 pt-2 pb-1" style="font-size: 0.7rem">{{ g.label }}</div>
              <div
                v-for="{ item, index } in g.items"
                :id="`cmd-${item.id}`"
                :key="item.id"
                role="option"
                :aria-selected="index === highlight"
                class="d-flex align-items-center gap-2 px-2 py-1 rounded"
                :class="{ 'bg-primary text-white': index === highlight }"
                style="cursor: pointer"
                @mousemove="highlight = index"
                @click="runAt(index)"
              >
                <i class="bi" :class="item.icon" aria-hidden="true" />
                <span class="flex-grow-1 text-truncate">{{ item.label }}</span>
                <kbd v-if="item.hint" class="small" :class="{ 'opacity-75': index !== highlight }">{{ item.hint }}</kbd>
              </div>
            </div>
            <p v-if="!items.length" class="text-muted small px-2 py-3 mb-0">No matches.</p>
          </div>
          <div class="modal-footer py-1 small text-muted justify-content-start gap-3">
            <span><kbd>↑</kbd><kbd>↓</kbd> move</span>
            <span><kbd>Enter</kbd> open</span>
            <span><kbd>Esc</kbd> close</span>
          </div>
        </div>
      </div>
    </div>
    <div v-if="palette.open.value" class="modal-backdrop fade show" />
  </Teleport>
</template>
