<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import Sortable from 'sortablejs'
import type { Project, SavedView } from '@/api/types'
import { projectNavLabel, projectOptionLabel, isArchivedProject, isProjectOwner } from '@/utils/projectLabel'

const props = defineProps<{
  collapsed: boolean
  projects: Project[]
  savedViews: SavedView[]
  activeProject?: string
  activeView?: string
}>()

const emit = defineEmits<{
  'toggle-collapse': []
  'close-mobile': []
  'select-home': []
  'select-project': [id: string]
  'select-view': [id: string]
  'add-project': []
  'edit-project': [project: Project]
  'reorder-projects': [projectIds: number[]]
  'add-view': []
}>()

const route = useRoute()

const overdueCount = inject<Ref<number>>('overdueCount', ref(0))
const pendingInviteCount = inject<Ref<number>>('pendingInviteCount', ref(0))

const projectsCollapsed = ref(false)
const viewsCollapsed = ref(false)
const archivedCollapsed = ref(true)
const ownedListEl = ref<HTMLElement | null>(null)
let sortable: Sortable | null = null

const ownedProjects = computed(() =>
  props.projects.filter((p) => isProjectOwner(p) && !isArchivedProject(p)),
)
const sharedProjects = computed(() =>
  props.projects.filter((p) => !isProjectOwner(p) && !isArchivedProject(p)),
)
const archivedProjectList = computed(() => props.projects.filter(isArchivedProject))

function isOwner(proj: Project) {
  return isProjectOwner(proj)
}

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

function collectOwnedIds(el: HTMLElement): number[] {
  return Array.from(el.querySelectorAll(':scope > .project-reorder-item'))
    .map((node) => parseInt((node as HTMLElement).dataset.projectId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

function initSortable() {
  destroySortable()
  if (!ownedListEl.value || ownedProjects.value.length < 2 || props.collapsed) return
  sortable = Sortable.create(ownedListEl.value, {
    handle: '.project-drag-handle',
    draggable: '.project-reorder-item',
    animation: 150,
    onEnd(evt) {
      const el = evt.to as HTMLElement
      const ids = collectOwnedIds(el)
      if (ids.length === ownedProjects.value.length) {
        emit('reorder-projects', ids)
      }
    },
  })
}

watch(
  [ownedProjects, () => props.collapsed, projectsCollapsed],
  async () => {
    await nextTick()
    initSortable()
  },
  { flush: 'post' },
)

onBeforeUnmount(() => {
  destroySortable()
})
</script>

<template>
  <aside
    class="ordryn-sidebar d-none d-md-flex"
    :class="{ collapsed: collapsed }"
  >
    <!-- Collapse Toggle Button (Desktop) -->
    <div class="d-none d-md-flex align-items-center justify-content-end mb-3">
      <button
        type="button"
        class="ordryn-sidebar-toggle-btn"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="emit('toggle-collapse')"
      >
        <i :class="collapsed ? 'bi bi-chevron-double-right' : 'bi bi-chevron-double-left'" />
      </button>
    </div>

    <!-- Main Navigation Items -->
    <ul class="sidebar-nav-list">
      <li class="sidebar-nav-item">
        <RouterLink
          to="/"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/' && !activeProject && !activeView }"
          data-tooltip="Home"
          title="Home"
          @click="emit('select-home'); emit('close-mobile')"
        >
          <i class="bi bi-house-door" />
          <span class="sidebar-text">Home</span>
        </RouterLink>
      </li>
      <li class="sidebar-nav-item">
        <RouterLink
          to="/dashboard"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/dashboard' }"
          data-tooltip="Dashboard"
          title="Dashboard"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-grid-1x2" />
          <span class="sidebar-text">Dashboard</span>
          <span
            v-if="overdueCount > 0"
            class="badge bg-danger ms-auto sidebar-text px-2 py-1"
            style="font-size: 0.7rem; font-weight: 600;"
            :title="`${overdueCount} overdue tasks`"
          >{{ overdueCount }}</span>
        </RouterLink>
      </li>
      <li class="sidebar-nav-item">
        <RouterLink
          to="/calendar"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/calendar' }"
          data-tooltip="Calendar"
          title="Calendar"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-calendar3" />
          <span class="sidebar-text">Calendar</span>
        </RouterLink>
      </li>
      <li class="sidebar-nav-item">
        <RouterLink
          to="/organizations"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/organizations' }"
          data-tooltip="Organizations"
          title="Organizations"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-diagram-3" />
          <span class="sidebar-text">Organizations</span>
        </RouterLink>
      </li>
    </ul>

    <!-- Section: Projects Header -->
    <div class="sidebar-section-header d-flex align-items-center justify-content-between">
      <RouterLink
        to="/projects"
        class="sidebar-section-link sidebar-text fw-bold text-uppercase d-flex align-items-center"
        data-tooltip="Projects"
        title="Manage Projects"
        @click="emit('close-mobile')"
      >
        <span>Projects</span>
        <span
          v-if="pendingInviteCount > 0"
          class="badge bg-danger ms-2 sidebar-text px-2 py-1"
          style="font-size: 0.7rem; font-weight: 600;"
          :title="`${pendingInviteCount} pending project invite${pendingInviteCount === 1 ? '' : 's'}`"
        >{{ pendingInviteCount }}</span>
        <i class="bi bi-box-arrow-up-right ms-1 opacity-75" style="font-size: 0.75rem;" />
      </RouterLink>

      <button
        type="button"
        class="btn btn-sm text-muted p-0 border-0 sidebar-text me-1"
        :title="projectsCollapsed ? 'Expand projects' : 'Collapse projects'"
        @click="projectsCollapsed = !projectsCollapsed"
      >
        <i :class="projectsCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" style="font-size: 0.8rem;" />
      </button>
    </div>

    <!-- Projects List (Collapsible) -->
    <ul v-if="!projectsCollapsed" class="sidebar-nav-list">
      <!-- No Project Option -->
      <li class="sidebar-nav-item">
        <a
          href="#"
          class="sidebar-nav-link"
          :class="{ active: activeProject === '0' }"
          data-tooltip="No Project"
          title="No Project"
          @click.prevent="emit('select-project', '0'); emit('close-mobile')"
        >
          <i class="bi bi-folder-x" />
          <span class="sidebar-text text-truncate">No Project</span>
        </a>
      </li>
    </ul>

    <!-- Owned projects (sortable) -->
    <ul
      v-if="!projectsCollapsed"
      ref="ownedListEl"
      class="sidebar-nav-list"
    >
      <li
        v-for="proj in ownedProjects"
        :key="proj.id"
        class="sidebar-nav-item position-relative project-reorder-item"
        :data-project-id="proj.id"
      >
        <div class="d-flex align-items-center justify-content-between w-100 min-w-0">
          <span
            class="project-drag-handle text-muted d-none d-md-inline-flex me-1 hover-reveal"
            title="Drag to reorder"
            aria-label="Drag to reorder project"
            @click.stop
          >
            <i class="bi bi-grip-vertical" style="font-size: 0.85rem;" />
          </span>
          <a
            href="#"
            class="sidebar-nav-link flex-grow-1 min-w-0"
            :class="{ active: activeProject === String(proj.id) }"
            :data-tooltip="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            :title="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            @click.prevent="emit('select-project', String(proj.id)); emit('close-mobile')"
          >
            <i class="bi bi-folder2-open" />
            <span class="sidebar-text text-truncate">{{ projectNavLabel(proj) }}</span>
          </a>

          <button
            v-if="isOwner(proj)"
            type="button"
            class="btn btn-sm text-muted p-0 border-0 hover-reveal d-none d-md-inline-block flex-shrink-0 me-2"
            title="Edit project"
            @click.stop="emit('edit-project', proj)"
          >
            <i class="bi bi-pencil" style="font-size: 0.85rem;" />
          </button>
        </div>
      </li>
    </ul>

    <!-- Shared projects (not reorderable) + add -->
    <ul v-if="!projectsCollapsed" class="sidebar-nav-list">
      <li
        v-for="proj in sharedProjects"
        :key="proj.id"
        class="sidebar-nav-item position-relative"
      >
        <div class="d-flex align-items-center justify-content-between w-100 min-w-0">
          <a
            href="#"
            class="sidebar-nav-link flex-grow-1 min-w-0"
            :class="{ active: activeProject === String(proj.id) }"
            :data-tooltip="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            :title="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            @click.prevent="emit('select-project', String(proj.id)); emit('close-mobile')"
          >
            <i class="bi bi-folder2-open" />
            <span class="sidebar-text text-truncate">{{ projectNavLabel(proj) }}</span>
          </a>

          <button
            type="button"
            class="btn btn-sm text-muted p-0 border-0 hover-reveal d-none d-md-inline-block flex-shrink-0 me-2"
            title="Project settings"
            @click.stop="emit('edit-project', proj)"
          >
            <i class="bi bi-pencil" style="font-size: 0.85rem;" />
          </button>
        </div>
      </li>

      <li class="sidebar-nav-item d-flex align-items-center">
        <button
          type="button"
          class="sidebar-nav-link text-start w-100 border-0 bg-transparent"
          data-tooltip="Create Project"
          title="Create a new Project"
          @click="emit('add-project')"
        >
          <i class="bi bi-plus-lg" />
          <span class="sidebar-text">Project</span>
        </button>
      </li>
    </ul>

    <div
      v-if="!projectsCollapsed && archivedProjectList.length"
      class="sidebar-section-header d-flex align-items-center justify-content-between"
    >
      <span class="sidebar-section-link sidebar-text fw-bold text-uppercase">Archived</span>
      <button
        type="button"
        class="btn btn-sm text-muted p-0 border-0 sidebar-text me-1"
        :title="archivedCollapsed ? 'Expand archived' : 'Collapse archived'"
        @click="archivedCollapsed = !archivedCollapsed"
      >
        <i :class="archivedCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" style="font-size: 0.8rem;" />
      </button>
    </div>
    <ul v-if="!projectsCollapsed && !archivedCollapsed && archivedProjectList.length" class="sidebar-nav-list">
      <li
        v-for="proj in archivedProjectList"
        :key="proj.id"
        class="sidebar-nav-item position-relative"
      >
        <div class="d-flex align-items-center justify-content-between w-100 min-w-0">
          <a
            href="#"
            class="sidebar-nav-link flex-grow-1 min-w-0"
            :class="{ active: activeProject === String(proj.id) }"
            :data-tooltip="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            :title="proj.description ? `${projectOptionLabel(proj)} — ${proj.description}` : projectOptionLabel(proj)"
            @click.prevent="emit('select-project', String(proj.id)); emit('close-mobile')"
          >
            <i class="bi bi-archive" />
            <span class="sidebar-text text-truncate">{{ projectNavLabel(proj) }}</span>
          </a>
          <button
            type="button"
            class="btn btn-sm text-muted p-0 border-0 hover-reveal d-none d-md-inline-block flex-shrink-0 me-2"
            title="Project settings"
            @click.stop="emit('edit-project', proj)"
          >
            <i class="bi bi-pencil" style="font-size: 0.85rem;" />
          </button>
        </div>
      </li>
    </ul>

    <!-- Section: Views Header -->
    <div class="sidebar-section-header d-flex align-items-center justify-content-between">
      <RouterLink
        to="/views"
        class="sidebar-section-link sidebar-text fw-bold text-uppercase"
        data-tooltip="Views"
        title="Manage Saved Views"
        @click="emit('close-mobile')"
      >
        <span>Views</span>
        <i class="bi bi-box-arrow-up-right ms-1 opacity-75" style="font-size: 0.75rem;" />
      </RouterLink>

      <button
        type="button"
        class="btn btn-sm text-muted p-0 border-0 sidebar-text me-1"
        :title="viewsCollapsed ? 'Expand views' : 'Collapse views'"
        @click="viewsCollapsed = !viewsCollapsed"
      >
        <i :class="viewsCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" style="font-size: 0.8rem;" />
      </button>
    </div>

    <!-- Views List (Collapsible) -->
    <ul v-if="!viewsCollapsed" class="sidebar-nav-list">
      <li v-for="view in savedViews" :key="view.id" class="sidebar-nav-item">
        <a
          href="#"
          class="sidebar-nav-link"
          :class="{ active: activeView === String(view.id) }"
          :data-tooltip="view.name"
          :title="view.name"
          @click.prevent="emit('select-view', String(view.id)); emit('close-mobile')"
        >
          <i class="bi bi-bookmark-star" />
          <span class="sidebar-text text-truncate">{{ view.name }}</span>
        </a>
      </li>
      <li class="sidebar-nav-item d-flex align-items-center">
        <button
          type="button"
          class="sidebar-nav-link text-start w-100 border-0 bg-transparent"
          data-tooltip="Save View"
          title="Save current filters as a new View"
          @click="emit('add-view')"
        >
          <i class="bi bi-plus-lg" />
          <span class="sidebar-text">View</span>
        </button>
      </li>
    </ul>
  </aside>
</template>
