<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useProjectNav } from '@/composables/useProjectNav'
import { projectNavLabel, isProjectOwner } from '@/utils/projectLabel'
import { searchProjects } from '@/utils/projectNav'
import { projectRoute } from '@/utils/projectRoute'

type Row =
  | { kind: 'none'; key: string }
  | { kind: 'project'; key: string; project: Project }

type Group = { label: string; rows: Row[] }

const route = useRoute()
const router = useRouter()
const { isAuthenticated } = useAuth()
const nav = useProjectNav()

const projects = ref<Project[]>([])
const loading = ref(false)
const loadError = ref(false)
const query = ref('')
const showArchived = ref(false)
const highlight = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
let returnFocus: HTMLElement | null = null

const pinIds = computed(() => nav.pinIds(projects.value))
const archivedCount = computed(() => projects.value.filter((p) => p.archived).length)

function projectRow(p: Project): Row {
  return { kind: 'project', key: `p-${p.id}`, project: p }
}

const groups = computed<Group[]>(() => {
  const q = query.value.trim()
  if (q) {
    const rows: Row[] = searchProjects(projects.value, q).map(projectRow)
    if ('no project'.includes(q.toLowerCase())) rows.unshift({ kind: 'none', key: 'none' })
    return rows.length ? [{ label: 'Results', rows }] : []
  }

  const pinnedSet = new Set(pinIds.value)
  const byId = new Map(projects.value.map((p) => [p.id, p]))
  const pinned = pinIds.value.map((id) => byId.get(id)).filter((p): p is Project => !!p && !p.archived)
  const rest = projects.value.filter((p) => !p.archived && !pinnedSet.has(p.id))
  const out: Group[] = []
  if (pinned.length) out.push({ label: 'Pinned', rows: pinned.map(projectRow) })
  const mine = rest.filter(isProjectOwner)
  const shared = rest.filter((p) => !isProjectOwner(p))
  out.push({ label: 'My projects', rows: [{ kind: 'none', key: 'none' }, ...mine.map(projectRow)] })
  if (shared.length) out.push({ label: 'Shared with me', rows: shared.map(projectRow) })
  if (showArchived.value) {
    const archived = projects.value.filter((p) => p.archived)
    if (archived.length) out.push({ label: 'Archived', rows: archived.map(projectRow) })
  }
  return out
})

const flatRows = computed(() => groups.value.flatMap((g) => g.rows))

function rowIndex(row: Row) {
  return flatRows.value.indexOf(row)
}

function secondaryText(p: Project): string {
  const parts: string[] = []
  if (p.organization_name) parts.push(p.organization_name)
  if (!isProjectOwner(p) && p.owner_user_name) parts.push(`@${p.owner_user_name}`)
  return parts.join(' · ')
}

async function load() {
  loading.value = projects.value.length === 0
  loadError.value = false
  try {
    projects.value = await api.listProjects()
  } catch {
    loadError.value = projects.value.length === 0
  } finally {
    loading.value = false
  }
}

function close() {
  nav.closeSwitcher()
}

function choose(row: Row | undefined) {
  if (!row) return
  const id = row.kind === 'none' ? '0' : String(row.project.id)
  if (row.kind === 'project') nav.touchRecent(row.project.id)
  close()
  const inPlace = nav.inPlaceSelectHandler()
  if (inPlace && (route.name === 'tasks' || route.name === 'project')) {
    inPlace(id)
    return
  }
  void router.push(row.kind === 'project' ? projectRoute(row.project) : projectRoute(id))
}

function scrollHighlightIntoView() {
  void nextTick(() => {
    listEl.value?.querySelector('.is-highlighted')?.scrollIntoView({ block: 'nearest' })
  })
}

function move(delta: number) {
  const n = flatRows.value.length
  if (!n) return
  highlight.value = (highlight.value + delta + n) % n
  scrollHighlightIntoView()
}

function onKeydown(e: KeyboardEvent) {
  if (!nav.switcherOpen.value) return
  switch (e.key) {
    case 'Escape':
      e.preventDefault()
      close()
      break
    case 'ArrowDown':
      e.preventDefault()
      move(1)
      break
    case 'ArrowUp':
      e.preventDefault()
      move(-1)
      break
    case 'Enter':
      if (e.target === inputEl.value) {
        e.preventDefault()
        choose(flatRows.value[highlight.value])
      }
      break
  }
}

function togglePin(p: Project) {
  void nav.togglePin(p.id, projects.value)
}

watch(query, () => {
  highlight.value = 0
})

watch(
  () => nav.switcherOpen.value,
  async (open) => {
    if (open) {
      if (!isAuthenticated.value) {
        close()
        return
      }
      returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      query.value = ''
      highlight.value = 0
      showArchived.value = false
      window.addEventListener('keydown', onKeydown, true)
      void load()
      await nextTick()
      inputEl.value?.focus()
    } else {
      window.removeEventListener('keydown', onKeydown, true)
      returnFocus?.focus?.()
      returnFocus = null
    }
  },
)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown, true)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="nav.switcherOpen.value" class="ordryn-switcher-backdrop" @mousedown.self="close">
      <div
        class="ordryn-switcher"
        role="dialog"
        aria-modal="true"
        aria-label="Find a project"
      >
        <div class="ordryn-switcher-search">
          <i class="bi bi-search" aria-hidden="true" />
          <input
            ref="inputEl"
            v-model="query"
            type="text"
            class="ordryn-switcher-input"
            placeholder="Find a project…"
            aria-label="Find a project"
            role="combobox"
            aria-expanded="true"
            aria-controls="ordryn-switcher-list"
            :aria-activedescendant="flatRows[highlight] ? `switcher-row-${flatRows[highlight]!.key}` : undefined"
            autocomplete="off"
            spellcheck="false"
          >
          <kbd class="d-none d-md-inline">Esc</kbd>
        </div>

        <div id="ordryn-switcher-list" ref="listEl" class="ordryn-switcher-list" role="listbox">
          <div v-if="loading" class="ordryn-switcher-empty">
            <span class="spinner-border spinner-border-sm me-2" aria-hidden="true" />Loading projects…
          </div>
          <div v-else-if="loadError" class="ordryn-switcher-empty">Could not load projects.</div>
          <div v-else-if="!flatRows.length" class="ordryn-switcher-empty">
            No projects match “{{ query.trim() }}”.
          </div>
          <template v-else>
            <div v-for="group in groups" :key="group.label" class="ordryn-switcher-group">
              <div class="ordryn-switcher-group-label">{{ group.label }}</div>
              <div
                v-for="row in group.rows"
                :id="`switcher-row-${row.key}`"
                :key="row.key"
                class="ordryn-switcher-row"
                :class="{ 'is-highlighted': rowIndex(row) === highlight }"
                role="option"
                :aria-selected="rowIndex(row) === highlight"
                @mousemove="highlight = rowIndex(row)"
                @click="choose(row)"
              >
                <template v-if="row.kind === 'none'">
                  <i class="bi bi-folder-x" aria-hidden="true" />
                  <span class="ordryn-switcher-name">No Project</span>
                  <span class="ordryn-switcher-meta">Tasks without a project</span>
                </template>
                <template v-else>
                  <i
                    :class="row.project.archived ? 'bi bi-archive' : 'bi bi-folder2-open'"
                    aria-hidden="true"
                  />
                  <span class="ordryn-switcher-name">{{ projectNavLabel(row.project) }}</span>
                  <span v-if="row.project.archived" class="badge text-bg-secondary">Archived</span>
                  <span class="ordryn-switcher-meta">{{ secondaryText(row.project) }}</span>
                  <button
                    type="button"
                    class="ordryn-pin-btn"
                    :class="{ 'is-pinned': pinIds.includes(row.project.id) }"
                    tabindex="-1"
                    :title="pinIds.includes(row.project.id) ? 'Unpin from sidebar' : 'Pin to sidebar'"
                    :aria-label="pinIds.includes(row.project.id) ? `Unpin ${row.project.name}` : `Pin ${row.project.name}`"
                    @click.stop="togglePin(row.project)"
                  >
                    <i :class="pinIds.includes(row.project.id) ? 'bi bi-pin-angle-fill' : 'bi bi-pin-angle'" />
                  </button>
                </template>
              </div>
            </div>
          </template>
        </div>

        <div class="ordryn-switcher-footer">
          <button
            v-if="archivedCount && !query.trim()"
            type="button"
            class="btn btn-link btn-sm p-0 text-decoration-none"
            @click="showArchived = !showArchived"
          >
            <i class="bi bi-archive me-1" />{{ showArchived ? 'Hide archived' : `Show archived (${archivedCount})` }}
          </button>
          <RouterLink to="/projects" class="btn btn-link btn-sm p-0 text-decoration-none" @click="close">
            <i class="bi bi-gear me-1" />Manage projects
          </RouterLink>
          <span class="ms-auto d-none d-md-inline text-muted small">
            <kbd>↑</kbd> <kbd>↓</kbd> to move · <kbd>Enter</kbd> to open
          </span>
        </div>
      </div>
    </div>
  </Teleport>
</template>
