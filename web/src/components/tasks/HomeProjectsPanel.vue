<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Project } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useProjectNav } from '@/composables/useProjectNav'
import { isProjectOwner } from '@/utils/projectLabel'
import { homeProjectOrder, projectInitials, projectTileHue } from '@/utils/projectNav'

/**
 * Project card grid shown above the task list on Home when the user turns on
 * Settings → Preferences → "Show projects above tasks on Home".
 */
const props = defineProps<{
  projects: Project[]
}>()

const emit = defineEmits<{
  select: [id: string]
  'add-project': []
}>()

/** Cards shown before "Show all" (two rows on a typical desktop width). */
const COLLAPSED_COUNT = 8

const { canCreateProjects } = useAuth()
const nav = useProjectNav()
const query = ref('')
const expanded = ref(false)

const pinIds = computed(() => nav.pinIds(props.projects))
const activeCount = computed(() => props.projects.filter((p) => !p.archived).length)
const archivedCount = computed(() => props.projects.length - activeCount.value)
const ordered = computed(() => homeProjectOrder(props.projects, pinIds.value, nav.recentIds.value, query.value))
const filtering = computed(() => query.value.trim() !== '')
const visible = computed(() =>
  filtering.value || expanded.value ? ordered.value : ordered.value.slice(0, COLLAPSED_COUNT),
)
const hiddenCount = computed(() => ordered.value.length - visible.value.length)

function meta(p: Project): string {
  const parts: string[] = []
  if (!isProjectOwner(p)) parts.push(p.role_name || p.role || 'member')
  if (p.organization_name) parts.push(p.organization_name)
  return parts.join(' · ')
}

function open(p: Project) {
  nav.touchRecent(p.id)
  emit('select', String(p.id))
}
</script>

<template>
  <section class="home-projects" aria-labelledby="home-projects-title">
    <header class="home-projects-header">
      <h2 id="home-projects-title" class="home-projects-title">
        Projects
        <span class="home-projects-count">{{ activeCount }}</span>
      </h2>
      <div class="home-projects-tools">
        <div class="home-projects-search">
          <i class="bi bi-funnel" aria-hidden="true" />
          <input
            v-model="query"
            type="search"
            class="form-control form-control-sm"
            placeholder="Filter projects"
            aria-label="Filter projects"
          >
        </div>
        <button
          v-if="canCreateProjects"
          type="button"
          class="btn btn-sm btn-primary text-nowrap"
          @click="emit('add-project')"
        >
          <i class="bi bi-plus-lg me-1" />New project
        </button>
        <RouterLink to="/projects" class="btn btn-sm btn-outline-secondary" title="Manage projects">
          <i class="bi bi-gear" /><span class="visually-hidden">Manage projects</span>
        </RouterLink>
      </div>
    </header>

    <p v-if="!projects.length" class="home-projects-empty">
      No projects yet.
      <button v-if="canCreateProjects" type="button" class="btn btn-link btn-sm p-0 align-baseline" @click="emit('add-project')">
        Create your first project
      </button>
    </p>
    <p v-else-if="!visible.length" class="home-projects-empty">
      No projects match “{{ query.trim() }}”.
    </p>

    <ul v-else class="home-projects-grid list-unstyled mb-0">
      <li v-for="p in visible" :key="p.id">
        <div
          class="home-project-card"
          :class="{ 'is-archived': p.archived }"
          role="button"
          tabindex="0"
          :aria-label="`Open ${p.name}`"
          @click="open(p)"
          @keydown.enter.prevent="open(p)"
          @keydown.space.prevent="open(p)"
        >
          <span class="home-project-tile" :style="{ '--tile-hue': projectTileHue(p.id) }" aria-hidden="true">
            {{ projectInitials(p.name) }}
          </span>
          <div class="home-project-body">
            <div class="home-project-name" :title="p.name">{{ p.name }}</div>
            <div class="home-project-meta">
              <i
                :class="p.workflow_mode === 'kanban' ? 'bi bi-kanban' : 'bi bi-list-check'"
                :title="p.workflow_mode === 'kanban' ? 'Board' : 'List'"
                aria-hidden="true"
              />
              <span v-if="p.archived" class="badge text-bg-secondary">Archived</span>
              <span v-if="meta(p)" class="text-truncate">{{ meta(p) }}</span>
            </div>
            <p v-if="p.description" class="home-project-desc" :title="p.description">{{ p.description }}</p>
          </div>
          <button
            type="button"
            class="ordryn-pin-btn home-project-pin"
            :class="{ 'is-pinned': pinIds.includes(p.id) }"
            :title="pinIds.includes(p.id) ? 'Unpin from sidebar' : 'Pin to sidebar'"
            :aria-label="pinIds.includes(p.id) ? `Unpin ${p.name}` : `Pin ${p.name}`"
            @click.stop="nav.togglePin(p.id, projects)"
            @keydown.enter.stop
            @keydown.space.stop
          >
            <i :class="pinIds.includes(p.id) ? 'bi bi-pin-angle-fill' : 'bi bi-pin-angle'" />
          </button>
        </div>
      </li>
    </ul>

    <footer v-if="hiddenCount > 0 || (expanded && !filtering) || (archivedCount && !filtering)" class="home-projects-footer">
      <button
        v-if="hiddenCount > 0"
        type="button"
        class="btn btn-link btn-sm p-0 text-decoration-none"
        @click="expanded = true"
      >
        Show all {{ ordered.length }} projects <i class="bi bi-chevron-down" />
      </button>
      <button
        v-else-if="expanded && !filtering && ordered.length > COLLAPSED_COUNT"
        type="button"
        class="btn btn-link btn-sm p-0 text-decoration-none"
        @click="expanded = false"
      >
        Show fewer <i class="bi bi-chevron-up" />
      </button>
      <button
        v-if="archivedCount && !filtering"
        type="button"
        class="btn btn-link btn-sm p-0 text-decoration-none text-muted ms-auto"
        @click="nav.openSwitcher()"
      >
        <i class="bi bi-archive me-1" />{{ archivedCount }} archived
      </button>
    </footer>
  </section>
</template>
