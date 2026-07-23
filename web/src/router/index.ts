import { createRouter, createWebHistory } from 'vue-router'
import { appBase } from '@/base'
import { useAuth } from '@/composables/useAuth'

const router = createRouter({
  history: createWebHistory(appBase()),
  scrollBehavior(to, _from, savedPosition) {
    if (savedPosition) return savedPosition
    if (to.hash) {
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
      path: '/docs/api/v1',
      name: 'api-docs',
      component: () => import('@/views/ApiDocsView.vue'),
    },
    {
      path: '/s/:token',
      name: 'share',
      component: () => import('@/views/ShareView.vue'),
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
      path: '/register',
      name: 'register',
      component: () => import('@/views/RegisterView.vue'),
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
      component: () => import('@/views/TasksView.vue'),
      meta: { requiresAuth: true },
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
      path: '/dashboard',
      name: 'dashboard',
      component: () => import('@/views/DashboardView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/views',
      name: 'views',
      component: () => import('@/views/SavedViewsView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/calendar',
      name: 'calendar',
      component: () => import('@/views/CalendarView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/calendar/:month(\\d{4}-\\d{2})',
      redirect: (to) => ({ name: 'calendar', query: { month: String(to.params.month) } }),
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
      meta: { requiresAuth: true, permission: 'admin' },
    },
    {
      path: '/invites',
      name: 'invites',
      component: () => import('@/views/InvitesView.vue'),
      meta: { requiresAuth: true, permission: 'createinvites' },
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
  if (to.meta.requiresAuth && !auth.isAuthenticated.value) {
    // Only preserve non-default destinations; `/` is the post-login default.
    if (to.fullPath && to.fullPath !== '/') {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
    return { name: 'login' }
  }
  if (to.meta.guest && auth.isAuthenticated.value) {
    // Preserve post-login redirects (e.g. /auth/device?user_code=…) so app SSO
    // is not diverted to the username-claim screen mid-flow.
    const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : null
    if (redirect) {
      return redirect
    }
    if (auth.needsUsernameClaim.value) {
      return { name: 'claim-username' }
    }
    return { name: 'tasks' }
  }
  if (
    auth.isAuthenticated.value &&
    auth.needsUsernameClaim.value &&
    to.meta.requiresAuth &&
    !to.meta.allowUsernameClaim
  ) {
    return { name: 'claim-username' }
  }
  if (to.name === 'claim-username' && auth.isAuthenticated.value && !auth.needsUsernameClaim.value) {
    return { name: 'tasks' }
  }
  const permission = to.meta.permission as string | undefined
  if (permission && !auth.hasPermission(permission)) {
    return { name: 'tasks' }
  }
  return true
})

export default router
