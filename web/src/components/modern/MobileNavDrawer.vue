<script setup lang="ts">
import { computed, inject, ref, watch, type Ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, SavedView } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'
import { projectNavLabel, isArchivedProject, isProjectOwner } from '@/utils/projectLabel'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const route = useRoute()
const router = useRouter()
const { isAuthenticated, user, hasPermission, logout } = useAuth()
const { siteInfo } = useSite()
const { push } = useToast()
const overdueCount = inject<Ref<number>>('overdueCount', ref(0))
const pendingInviteCount = inject<Ref<number>>('pendingInviteCount', ref(0))

const projects = ref<Project[]>([])
const savedViews = ref<SavedView[]>([])
const projectsCollapsed = ref(false)
const archivedCollapsed = ref(true)
const viewsCollapsed = ref(false)

const showChangelog = computed(() => siteInfo.value?.show_changelog !== false)
const canViewInvites = computed(
  () => hasPermission('admin') || hasPermission('createinvites') || !!siteInfo.value?.allow_user_invites,
)
const activeProject = computed(() => (typeof route.query.project === 'string' ? route.query.project : ''))
const activeView = computed(() => (typeof route.query.view === 'string' ? route.query.view : ''))

const ownedProjects = computed(() =>
  projects.value.filter((p) => isProjectOwner(p) && !isArchivedProject(p)),
)
const sharedProjects = computed(() =>
  projects.value.filter((p) => !isProjectOwner(p) && !isArchivedProject(p)),
)
const archivedProjectList = computed(() => projects.value.filter(isArchivedProject))

async function loadLists() {
  if (!isAuthenticated.value) {
    projects.value = []
    savedViews.value = []
    return
  }
  try {
    const [projList, viewList] = await Promise.all([api.listProjects(), api.listSavedViews()])
    projects.value = projList
    savedViews.value = viewList
  } catch {
    /* drawer still works without lists */
  }
}

function close() {
  emit('close')
}

function selectProject(id: string) {
  void router.push({ path: '/', query: { project: id } })
  close()
}

function selectView(id: string) {
  void router.push({ path: '/', query: { view: id } })
  close()
}

async function onLogout() {
  close()
  try {
    await logout()
    push('Signed out', 'info')
    await router.push({ name: 'login' })
  } catch (err) {
    push(err instanceof Error ? err.message : 'Logout failed', 'error')
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) void loadLists()
  },
)

watch(isAuthenticated, (ok) => {
  if (!ok) {
    projects.value = []
    savedViews.value = []
  }
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="ordryn-mobile-nav-backdrop d-md-none"
      @click="close"
    />

    <nav
      id="ordryn-mobile-nav"
      class="ordryn-mobile-nav d-md-none"
      :class="{ 'is-open': open }"
      :aria-hidden="!open"
      aria-label="Mobile menu"
    >
      <div class="ordryn-mobile-nav-header">
        <span class="fw-bold">Menu</span>
        <button
          type="button"
          class="btn btn-link text-decoration-none p-1"
          style="color: var(--ordryn-sidebar-text);"
          aria-label="Close menu"
          @click="close"
        >
          <i class="bi bi-x-lg fs-5" />
        </button>
      </div>

      <div class="ordryn-mobile-nav-body">
        <ul class="sidebar-nav-list">
          <li class="sidebar-nav-item">
            <RouterLink
              to="/"
              class="sidebar-nav-link"
              :class="{ active: route.path === '/' && !activeProject && !activeView }"
              @click="close"
            >
              <i class="bi bi-house-door" />
              <span>Home</span>
            </RouterLink>
          </li>
          <template v-if="isAuthenticated">
            <li class="sidebar-nav-item">
              <RouterLink
                to="/dashboard"
                class="sidebar-nav-link"
                :class="{ active: route.path === '/dashboard' }"
                @click="close"
              >
                <i class="bi bi-grid-1x2" />
                <span>Dashboard</span>
                <span
                  v-if="overdueCount > 0"
                  class="badge bg-danger ms-auto px-2 py-1"
                  style="font-size: 0.7rem; font-weight: 600;"
                >{{ overdueCount }}</span>
              </RouterLink>
            </li>
            <li class="sidebar-nav-item">
              <RouterLink
                to="/calendar"
                class="sidebar-nav-link"
                :class="{ active: route.path === '/calendar' }"
                @click="close"
              >
                <i class="bi bi-calendar3" />
                <span>Calendar</span>
              </RouterLink>
            </li>
            <li class="sidebar-nav-item">
              <RouterLink
                to="/settings"
                class="sidebar-nav-link"
                :class="{ active: route.path === '/settings' }"
                @click="close"
              >
                <img
                  v-if="user?.avatar_url"
                  :src="user.avatar_url"
                  alt=""
                  class="rounded-circle drawer-avatar-img"
                  aria-hidden="true"
                />
                <i v-else class="bi bi-person-circle" />
                <span>Profile</span>
              </RouterLink>
            </li>
            <li class="sidebar-nav-item">
              <button type="button" class="sidebar-nav-link text-start w-100 border-0 bg-transparent" @click="onLogout">
                <i class="bi bi-box-arrow-right" />
                <span>Logout</span>
              </button>
            </li>
            <li v-if="hasPermission('admin')" class="sidebar-nav-item">
              <RouterLink
                to="/admin"
                class="sidebar-nav-link"
                :class="{ active: String(route.path).startsWith('/admin') }"
                @click="close"
              >
                <i class="bi bi-shield-lock" />
                <span>Admin</span>
              </RouterLink>
            </li>
            <li v-if="canViewInvites" class="sidebar-nav-item">
              <RouterLink
                to="/invites"
                class="sidebar-nav-link"
                :class="{ active: route.path === '/invites' }"
                @click="close"
              >
                <i class="bi bi-envelope-plus" />
                <span>Invites</span>
              </RouterLink>
            </li>
          </template>
        </ul>

        <template v-if="isAuthenticated">
          <div class="sidebar-section-header d-flex align-items-center justify-content-between">
            <RouterLink
              to="/projects"
              class="sidebar-section-link fw-bold text-uppercase d-flex align-items-center"
              @click="close"
            >
              <span>Projects</span>
              <span
                v-if="pendingInviteCount > 0"
                class="badge bg-danger ms-2 px-2 py-1"
                style="font-size: 0.7rem; font-weight: 600;"
              >{{ pendingInviteCount }}</span>
            </RouterLink>
            <button
              type="button"
              class="btn btn-sm text-muted p-0 border-0 me-1"
              :aria-label="projectsCollapsed ? 'Expand projects' : 'Collapse projects'"
              @click="projectsCollapsed = !projectsCollapsed"
            >
              <i :class="projectsCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" />
            </button>
          </div>

          <ul v-if="!projectsCollapsed" class="sidebar-nav-list">
            <li class="sidebar-nav-item">
              <a
                href="#"
                class="sidebar-nav-link"
                :class="{ active: activeProject === '0' }"
                @click.prevent="selectProject('0')"
              >
                <i class="bi bi-folder-x" />
                <span class="text-truncate">No Project</span>
              </a>
            </li>
            <li v-for="proj in ownedProjects" :key="proj.id" class="sidebar-nav-item">
              <a
                href="#"
                class="sidebar-nav-link"
                :class="{ active: activeProject === String(proj.id) }"
                @click.prevent="selectProject(String(proj.id))"
              >
                <i class="bi bi-folder2-open" />
                <span class="text-truncate">{{ projectNavLabel(proj) }}</span>
              </a>
            </li>
            <li v-for="proj in sharedProjects" :key="proj.id" class="sidebar-nav-item">
              <a
                href="#"
                class="sidebar-nav-link"
                :class="{ active: activeProject === String(proj.id) }"
                @click.prevent="selectProject(String(proj.id))"
              >
                <i class="bi bi-folder2" />
                <span class="text-truncate">{{ projectNavLabel(proj) }}</span>
              </a>
            </li>
            <li v-if="archivedProjectList.length" class="sidebar-nav-item">
              <button
                type="button"
                class="sidebar-nav-link text-start w-100 border-0 bg-transparent"
                @click="archivedCollapsed = !archivedCollapsed"
              >
                <i class="bi bi-archive" />
                <span>Archived</span>
                <i :class="archivedCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" class="ms-auto" />
              </button>
            </li>
            <li
              v-for="proj in archivedCollapsed ? [] : archivedProjectList"
              :key="'arch-' + proj.id"
              class="sidebar-nav-item"
            >
              <a
                href="#"
                class="sidebar-nav-link"
                :class="{ active: activeProject === String(proj.id) }"
                @click.prevent="selectProject(String(proj.id))"
              >
                <i class="bi bi-archive" />
                <span class="text-truncate">{{ projectNavLabel(proj) }}</span>
              </a>
            </li>
            <li class="sidebar-nav-item">
              <RouterLink to="/projects" class="sidebar-nav-link" @click="close">
                <i class="bi bi-plus-lg" />
                <span>Manage projects</span>
              </RouterLink>
            </li>
          </ul>

          <div class="sidebar-section-header d-flex align-items-center justify-content-between">
            <RouterLink
              to="/views"
              class="sidebar-section-link fw-bold text-uppercase"
              @click="close"
            >
              Views
            </RouterLink>
            <button
              type="button"
              class="btn btn-sm text-muted p-0 border-0 me-1"
              :aria-label="viewsCollapsed ? 'Expand views' : 'Collapse views'"
              @click="viewsCollapsed = !viewsCollapsed"
            >
              <i :class="viewsCollapsed ? 'bi bi-chevron-down' : 'bi bi-chevron-up'" />
            </button>
          </div>

          <ul v-if="!viewsCollapsed" class="sidebar-nav-list">
            <li v-for="view in savedViews" :key="view.id" class="sidebar-nav-item">
              <a
                href="#"
                class="sidebar-nav-link"
                :class="{ active: activeView === String(view.id) }"
                @click.prevent="selectView(String(view.id))"
              >
                <i class="bi bi-bookmark-star" />
                <span class="text-truncate">{{ view.name }}</span>
              </a>
            </li>
            <li class="sidebar-nav-item">
              <RouterLink to="/views" class="sidebar-nav-link" @click="close">
                <i class="bi bi-plus-lg" />
                <span>Manage views</span>
              </RouterLink>
            </li>
          </ul>
        </template>

        <div class="sidebar-section-header">
          <span class="fw-bold text-uppercase sidebar-text">More</span>
        </div>
        <ul class="sidebar-nav-list">
          <li v-if="showChangelog" class="sidebar-nav-item">
            <a
              href="#changelogModal"
              class="sidebar-nav-link"
              data-bs-toggle="modal"
              data-bs-target="#changelogModal"
              @click="close"
            >
              <i class="bi bi-journal-text" />
              <span>Changelog</span>
            </a>
          </li>
          <li class="sidebar-nav-item">
            <a
              href="#shortcutsModal"
              class="sidebar-nav-link"
              data-bs-toggle="modal"
              data-bs-target="#shortcutsModal"
              @click="close"
            >
              <i class="bi bi-keyboard" />
              <span>Shortcuts</span>
            </a>
          </li>
          <li class="sidebar-nav-item">
            <RouterLink to="/docs/guide" class="sidebar-nav-link" @click="close">
              <i class="bi bi-question-circle" />
              <span>How to use</span>
            </RouterLink>
          </li>
          <li class="sidebar-nav-item">
            <RouterLink to="/docs/api/v1" class="sidebar-nav-link" @click="close">
              <i class="bi bi-braces" />
              <span>API</span>
            </RouterLink>
          </li>
          <template v-if="!isAuthenticated">
            <li class="sidebar-nav-item">
              <RouterLink to="/login" class="sidebar-nav-link" @click="close">
                <i class="bi bi-box-arrow-in-right" />
                <span>Login</span>
              </RouterLink>
            </li>
            <li class="sidebar-nav-item">
              <RouterLink to="/register" class="sidebar-nav-link" @click="close">
                <i class="bi bi-person-plus" />
                <span>Sign up</span>
              </RouterLink>
            </li>
          </template>
        </ul>
      </div>
    </nav>
  </Teleport>
</template>

<style scoped>
.drawer-avatar-img {
  width: 18px;
  height: 18px;
  object-fit: cover;
}
</style>
