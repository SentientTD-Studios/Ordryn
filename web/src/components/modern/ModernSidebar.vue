<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import Sortable from 'sortablejs'
import type { Project, SavedView } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useProjectNav } from '@/composables/useProjectNav'
import { sidebarProjectSections } from '@/utils/projectNav'
import SidebarProjectLink from '@/components/modern/SidebarProjectLink.vue'

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
  'add-project': []
  'edit-project': [project: Project]
  'add-view': []
}>()

const route = useRoute()
const { canCreateProjects, managesPeople } = useAuth()
const { isCompanyMode } = useSite()

const overdueCount = inject<Ref<number>>('overdueCount', ref(0))
const pendingInviteCount = inject<Ref<number>>('pendingInviteCount', ref(0))

const nav = useProjectNav()

const projectsCollapsed = ref(false)
const viewsCollapsed = ref(false)
const pinnedListEl = ref<HTMLElement | null>(null)
let sortable: Sortable | null = null

const pinIds = computed(() => nav.pinIds(props.projects))
const sections = computed(() => sidebarProjectSections(props.projects, pinIds.value, nav.recentIds.value))
const hiddenCount = computed(() => {
  const shown = sections.value.pinned.length + sections.value.suggested.length + sections.value.recent.length
  return Math.max(0, props.projects.length - shown)
})
const shortcutHint = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl+K'

function selectProject(proj: Project) {
  nav.touchRecent(proj.id)
  emit('select-project', String(proj.id))
  emit('close-mobile')
}

function togglePin(proj: Project) {
  void nav.togglePin(proj.id, props.projects)
}

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

function collectPinnedIds(el: HTMLElement): number[] {
  return Array.from(el.querySelectorAll(':scope > .project-reorder-item'))
    .map((node) => parseInt((node as HTMLElement).dataset.projectId || '', 10))
    .filter((id) => !Number.isNaN(id))
}

function initSortable() {
  destroySortable()
  if (!pinnedListEl.value || sections.value.pinned.length < 2 || props.collapsed) return
  sortable = Sortable.create(pinnedListEl.value, {
    handle: '.project-drag-handle',
    draggable: '.project-reorder-item',
    animation: 150,
    onEnd(evt) {
      const from = evt.from as HTMLElement
      const ids = collectPinnedIds(from)
      // Put the DOM back where Vue left it; the new order re-renders from state.
      if (evt.oldIndex != null) {
        evt.item.remove()
        from.insertBefore(evt.item, from.children[evt.oldIndex] ?? null)
      }
      if (evt.oldIndex === evt.newIndex) return
      // Archived pins are hidden here but keep their place at the end of the list.
      const visible = new Set(ids)
      void nav.setPins([...ids, ...pinIds.value.filter((id) => !visible.has(id))], props.projects)
    },
  })
}

watch(
  [() => sections.value.pinned.length, () => props.collapsed, projectsCollapsed, pinnedListEl],
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
          to="/team"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/team' }"
          data-tooltip="Team"
          title="Team"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-people" />
          <span class="sidebar-text">Team</span>
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
          to="/trash"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/trash' }"
          data-tooltip="Trash"
          title="Trash"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-trash3" />
          <span class="sidebar-text">Trash</span>
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
          <span class="sidebar-text">{{ isCompanyMode ? 'Company & teams' : 'Organizations' }}</span>
        </RouterLink>
      </li>
      <li v-if="managesPeople" class="sidebar-nav-item">
        <RouterLink
          to="/people"
          class="sidebar-nav-link"
          :class="{ active: route.path === '/people' }"
          data-tooltip="People"
          title="People"
          @click="emit('close-mobile')"
        >
          <i class="bi bi-people" />
          <span class="sidebar-text">People</span>
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
          :title="`${pendingInviteCount} pending invite${pendingInviteCount === 1 ? '' : 's'}`"
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

    <template v-if="!projectsCollapsed">
      <!-- Find a project (opens the switcher) -->
      <ul class="sidebar-nav-list">
        <li class="sidebar-nav-item">
          <button
            type="button"
            class="sidebar-nav-link sidebar-find-project"
            data-tooltip="Find a project"
            :title="`Find a project (${shortcutHint})`"
            @click="nav.openSwitcher()"
          >
            <i class="bi bi-search" />
            <span class="sidebar-text">Find a project</span>
            <span class="sidebar-text sidebar-shortcut">{{ shortcutHint }}</span>
          </button>
        </li>
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

      <!-- Pinned (drag to reorder) -->
      <template v-if="sections.pinned.length">
        <div class="sidebar-subsection-label">Pinned</div>
        <ul ref="pinnedListEl" class="sidebar-nav-list">
          <SidebarProjectLink
            v-for="proj in sections.pinned"
            :key="proj.id"
            :project="proj"
            :active="activeProject === String(proj.id)"
            pinned
            draggable
            @select="selectProject(proj)"
            @toggle-pin="togglePin(proj)"
            @edit="emit('edit-project', proj)"
          />
        </ul>
      </template>

      <!-- Nothing pinned yet: show the top projects -->
      <ul v-if="sections.suggested.length" class="sidebar-nav-list">
        <SidebarProjectLink
          v-for="proj in sections.suggested"
          :key="proj.id"
          :project="proj"
          :active="activeProject === String(proj.id)"
          :pinned="false"
          @select="selectProject(proj)"
          @toggle-pin="togglePin(proj)"
          @edit="emit('edit-project', proj)"
        />
      </ul>

      <!-- Recently opened -->
      <template v-if="sections.recent.length">
        <div class="sidebar-subsection-label">Recent</div>
        <ul class="sidebar-nav-list">
          <SidebarProjectLink
            v-for="proj in sections.recent"
            :key="proj.id"
            :project="proj"
            :active="activeProject === String(proj.id)"
            :pinned="false"
            @select="selectProject(proj)"
            @toggle-pin="togglePin(proj)"
            @edit="emit('edit-project', proj)"
          />
        </ul>
      </template>

      <ul class="sidebar-nav-list">
        <li v-if="hiddenCount > 0" class="sidebar-nav-item">
          <button
            type="button"
            class="sidebar-nav-link text-start w-100 border-0 bg-transparent"
            data-tooltip="All projects"
            title="Browse all projects"
            @click="nav.openSwitcher()"
          >
            <i class="bi bi-three-dots" />
            <span class="sidebar-text">All projects ({{ projects.length }})</span>
          </button>
        </li>
        <li v-if="canCreateProjects" class="sidebar-nav-item d-flex align-items-center">
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
    </template>

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
        <RouterLink
          :to="`/views/${view.id}`"
          class="sidebar-nav-link"
          :class="{ active: activeView === String(view.id) || route.path === `/views/${view.id}` }"
          :data-tooltip="view.shared ? `${view.name} (${view.project_name})` : view.name"
          :title="view.shared ? `${view.name} — shared in ${view.project_name}` : view.name"
          @click="emit('close-mobile')"
        >
          <i :class="view.shared ? 'bi bi-people' : 'bi bi-bookmark-star'" />
          <span class="sidebar-text text-truncate">{{ view.name }}</span>
        </RouterLink>
      </li>
      <li class="sidebar-nav-item d-flex align-items-center">
        <button
          type="button"
          class="sidebar-nav-link text-start w-100 border-0 bg-transparent"
          data-tooltip="New query"
          title="New query (starts from the task list's current filters)"
          @click="emit('add-view')"
        >
          <i class="bi bi-plus-lg" />
          <span class="sidebar-text">View</span>
        </button>
      </li>
    </ul>
  </aside>
</template>
