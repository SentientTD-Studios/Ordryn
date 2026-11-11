import { createRouter, createWebHistory } from 'vue-router'
import { appBase } from '@/base'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'
import { consumeTaskRoutePass, useTaskSidebar } from '@/composables/useTaskSidebar'
import { api } from '@/api/client'
import { isDeviceAuthPath, stashDeviceAuthReturn } from '@/deviceAuthReturn'
import { isProfileSectionHash } from '@/utils/profileSections'

const IndexView = () => import('@/views/IndexView.vue')

const router = createRouter({
  history: createWebHistory(appBase()),
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition
    if (to.hash) {
      if (to.name === 'settings' && isProfileSectionHash(to.hash)) {
        if (from.name === 'settings') return false
        return { top: 0 }
      }
      return {
        el: to.hash,
        behavior: 'smooth',
        top: 80,
      }
    }
    return { top: 0 }
  },
  routes: [
    {
      path: '/docs/guide',
      name: 'user-guide',
      component: () => import('@/views/UserGuideView.vue'),
    },
    {
      path: '/docs/api/v2',
      name: 'api-docs',
      component: () => import('@/views/ApiDocsView.vue'),
    },
    {
      path: '/docs/api/v1',
      redirect: { name: 'api-docs' },
    },
    {
      path: '/s/:token',
      name: 'share',
      component: () => import('@/views/ShareView.vue'),
    },
    {
      // Password-gated public feedback board; works signed in or out.
      path: '/feedback/:slug',
      name: 'feedback-board',
      component: () => import('@/views/FeedbackBoardView.vue'),
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/SetupView.vue'),
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { guest: true },
    },
    {
      path: '/forgot-password',
      name: 'forgot-password',
      component: () => import('@/views/ForgotPasswordView.vue'),
      meta: { guest: true },
    },
    {
      path: '/reset-password',
      name: 'reset-password',
      component: () => import('@/views/ResetPasswordView.vue'),
      meta: { guest: true },
    },
    {
      // Opened from the confirmation email; works signed in or out.
      path: '/verify-email',
      name: 'verify-email',
      component: () => import('@/views/VerifyEmailView.vue'),
    },
    {
      path: '/register',
      name: 'register',
      component: () => import('@/views/RegisterView.vue'),
      meta: { guest: true },
    },
    {
      path: '/join',
      name: 'join',
      component: () => import('@/views/JoinRequestView.vue'),
      meta: { guest: true },
    },
    {
      path: '/claim-username',
      name: 'claim-username',
      component: () => import('@/views/ClaimUsernameView.vue'),
      meta: { requiresAuth: true, allowUsernameClaim: true },
    },
    {
      path: '/',
      name: 'tasks',
      component: IndexView,
      // Views that use the sidebar layout render AppFooter themselves.
      meta: { ownFooter: true },
    },
    {
      // A project's board, by slug (or id / former slug, which redirect to the current slug).
      // Shares the home component so switching projects doesn't remount the board.
      path: '/p/:ref',
      name: 'project',
      component: IndexView,
      meta: { ownFooter: true, requiresAuth: true },
    },
    {
      // A task's link: its project's board with the task open as a modal. The id
      // decides the task; a stale project in the path is corrected when it loads.
      path: '/p/:ref/tasks/:id(\\d+)',
      name: 'project-task',
      component: IndexView,
      meta: { ownFooter: true, requiresAuth: true },
    },
    {
      path: '/tasks/:id',
      name: 'task',
      component: () => import('@/views/TaskDetailView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/projects',
      name: 'projects',
      component: () => import('@/views/ProjectsView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/organizations',
      name: 'organizations',
      component: () => import('@/views/OrganizationsView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      component: () => import('@/views/DashboardView.vue'),
      meta: { requiresAuth: true, ownFooter: true },
    },
    {
      path: '/team',
      name: 'team',
      component: () => import('@/views/TeamDashboardView.vue'),
      meta: { requiresAuth: true, ownFooter: true },
    },
    {
      path: '/views',
      name: 'views',
      component: () => import('@/views/SavedViewsView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/views/new',
      name: 'query-new',
      component: () => import('@/views/QueryEditorView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/views/:id(\\d+)',
      name: 'query',
      component: () => import('@/views/QueryEditorView.vue'),
      meta: { requiresAuth: true },
    },
    {
      // A project's request form, shared by link.
      path: '/f/:token',
      name: 'request-form',
      component: () => import('@/views/RequestFormView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/calendar',
      name: 'calendar',
      component: () => import('@/views/CalendarView.vue'),
      meta: { requiresAuth: true, ownFooter: true },
    },
    {
      path: '/trash',
      name: 'trash',
      component: () => import('@/views/TrashView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/calendar/:month(\\d{4}-\\d{2})',
      redirect: (to) => ({ name: 'calendar', query: { month: String(to.params.month) } }),
    },
    {
      path: '/people',
      name: 'people',
      component: () => import('@/views/PeopleView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/SettingsView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/import',
      name: 'import',
      component: () => import('@/views/ImportView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/auth/device',
      name: 'device-auth',
      component: () => import('@/views/DeviceAuthView.vue'),
      // App browser SSO must complete without the username-claim gate taking over.
      meta: { requiresAuth: true, allowUsernameClaim: true },
    },
    {
      path: '/admin',
      name: 'admin',
      component: () => import('@/views/AdminView.vue'),
      meta: { requiresAuth: true, permission: 'settings:manage' },
    },
    {
      path: '/admin/extensions',
      name: 'admin-extensions',
      component: () => import('@/views/AdminExtensionsView.vue'),
      meta: { requiresAuth: true, permission: 'settings:manage' },
    },
    {
      path: '/admin/mods',
      redirect: '/admin/extensions',
    },
    {
      path: '/admin/requests',
      name: 'admin-requests',
      component: () => import('@/views/AdminJoinRequestsView.vue'),
      meta: { requiresAuth: true, permission: 'admin' },
    },
    {
      path: '/admin/users',
      name: 'admin-users',
      component: () => import('@/views/AdminUsersView.vue'),
      meta: { requiresAuth: true, permission: 'admin' },
    },
    {
      path: '/admin/roles',
      name: 'admin-project-roles',
      component: () => import('@/views/AdminProjectRolesView.vue'),
      meta: { requiresAuth: true, permission: 'admin' },
    },
    {
      path: '/admin/site-roles',
      name: 'admin-site-roles',
      component: () => import('@/views/AdminSiteRolesView.vue'),
      meta: { requiresAuth: true, permission: 'roles:manage' },
    },
    {
      path: '/admin/audit',
      name: 'admin-audit',
      component: () => import('@/views/AdminAuditView.vue'),
      meta: { requiresAuth: true, permission: 'audit:view' },
    },
    {
      path: '/admin/email-audit',
      name: 'admin-email-audit',
      component: () => import('@/views/AdminEmailAuditView.vue'),
      meta: { requiresAuth: true, permission: 'audit:view' },
    },
    {
      path: '/admin/comment-audit',
      name: 'admin-comment-audit',
      component: () => import('@/views/AdminCommentAuditView.vue'),
      meta: { requiresAuth: true, permission: 'audit:view' },
    },
    {
      path: '/admin/invites',
      name: 'admin-invites',
      component: () => import('@/views/AdminInvitesView.vue'),
      meta: { requiresAuth: true, permission: 'admin' },
    },
    {
      path: '/invites',
      name: 'invites',
      component: () => import('@/views/InvitesView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuth()
  if (!auth.bootstrapped.value) {
    await auth.refresh()
  }
  const site = useSite()
  if (!site.loaded.value) {
    await site.refresh()
  }
  // A fresh install has no accounts, so every page leads to first-time setup until it's done.
  if (site.setupRequired.value) {
    return to.name === 'setup' ? true : { name: 'setup' }
  }
  if (to.name === 'setup') {
    return { name: auth.isAuthenticated.value ? 'tasks' : 'login' }
  }
  if (to.meta.requiresAuth && !auth.isAuthenticated.value) {
    // Only preserve non-default destinations; `/` is the post-login default.
    if (to.fullPath && to.fullPath !== '/') {
      if (isDeviceAuthPath(to.fullPath)) {
        stashDeviceAuthReturn(to.fullPath)
      }
      return { name: 'login', query: { redirect: to.fullPath } }
    }
    return { name: 'login' }
  }
  if (to.meta.guest && auth.isAuthenticated.value) {
    // Preserve post-login redirects (e.g. /auth/device?user_code=…) so app SSO
    // is not diverted to the username-claim screen mid-flow.
    const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : null
    if (redirect && isDeviceAuthPath(redirect)) {
      stashDeviceAuthReturn(redirect)
      return redirect
    }
    if (redirect && redirect.startsWith('/') && !redirect.startsWith('//')) {
      return redirect
    }
    if (auth.needsUsernameClaim.value) {
      return { name: 'claim-username' }
    }
    return { name: 'tasks' }
  }
  // allowUsernameClaim = route may be visited while a free username claim is still pending
  // (claim screen itself, and /auth/device so app browser SSO is not taken over).
  if (
    auth.isAuthenticated.value &&
    auth.needsUsernameClaim.value &&
    (to.meta.requiresAuth || to.name === 'tasks') &&
    !to.meta.allowUsernameClaim
  ) {
    return { name: 'claim-username' }
  }
  if (to.name === 'claim-username' && auth.isAuthenticated.value && !auth.needsUsernameClaim.value) {
    return { name: 'tasks' }
  }
  // Company accounts come only from people managers: no signup, join requests, or invites.
  const companyClosed = ['register', 'join', 'invites', 'admin-invites', 'admin-requests']
  if (site.isCompanyMode.value && companyClosed.includes(String(to.name))) {
    return { name: auth.isAuthenticated.value ? 'tasks' : 'login' }
  }
  if (to.name === 'invites') {
    const canInvite =
      auth.hasPermission('admin') ||
      auth.hasPermission('createinvites') ||
      !!site.siteInfo.value?.allow_user_invites
    if (!canInvite) {
      return { name: 'tasks' }
    }
  }

  if (to.name === 'people' && !auth.managesPeople.value) {
    return { name: 'tasks' }
  }

  // Site permissions: admin implies every one.
  const permission = to.meta.permission as string | undefined
  if (permission && !auth.hasSitePermission(permission)) {
    return { name: 'tasks' }
  }
  return true
})

// Task links always end in the task modal, never a separate page:
// - in the app, following one opens the modal over the current page, so closing
//   it returns the user to where they were;
// - loading /p/{slug}/tasks/{id} shows that project's board with the task open;
// - /tasks/{id} is the short form and redirects there (tasks without a project
//   open over home).
router.beforeEach(async (to, from) => {
  if (to.name !== 'task' && to.name !== 'project-task') return true
  const id = Number(to.params.id)
  if (!(id > 0) || !useAuth().isAuthenticated.value) return true
  // The app moving the URL for a task that's already open.
  if (consumeTaskRoutePass(id)) return true
  const inApp = from.matched.length > 0 && from.name !== 'share'
  if (inApp) {
    void useTaskSidebar().openTask(id)
    return false
  }
  if (to.name === 'project-task') return true
  try {
    const task = await api.getTask(id)
    if (task.project_id != null) {
      const project = await api.getProject(task.project_id).catch(() => null)
      return {
        name: 'project-task',
        params: { ref: project?.slug || String(task.project_id), id: String(id) },
        query: to.query,
        hash: to.hash,
      }
    }
  } catch {
    // Unknown or hidden task: the modal shows the error.
  }
  return { name: 'tasks', query: { ...to.query, task: String(id) }, hash: to.hash }
})

export default router
