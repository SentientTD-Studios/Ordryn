import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { SetupPayload, User } from '@/api/types'
import { isMFARequired } from '@/api/types'

const user = ref<User | null>(null)
const loading = ref(false)
const bootstrapped = ref(false)

export function useAuth() {
  const isAuthenticated = computed(() => user.value !== null)
  const needsUsernameClaim = computed(() => !!user.value?.username_change_available)

  async function refresh() {
    loading.value = true
    try {
      user.value = await api.me()
    } catch {
      user.value = null
    } finally {
      loading.value = false
      bootstrapped.value = true
    }
  }

  async function login(email: string, password: string) {
    const result = await api.login(email, password)
    if (isMFARequired(result)) {
      return result
    }
    user.value = result
    return user.value
  }

  async function verifyMFA(code: string) {
    user.value = await api.verifyMFA(code)
    return user.value
  }

  async function register(payload: {
    email: string
    password: string
    confirm_password: string
    user_name: string
    timezone?: string
    invite_token?: string
  }) {
    user.value = await api.register(payload)
    return user.value
  }

  async function completeSetup(payload: SetupPayload) {
    user.value = await api.completeSetup(payload)
    return user.value
  }

  async function logout() {
    try {
      await api.logout()
    } finally {
      user.value = null
    }
  }

  async function updateProfile(
    payload: Partial<Pick<User, 'timezone' | 'items_per_page' | 'allow_project_invites'>>,
  ) {
    user.value = await api.patchMe(payload)
    return user.value
  }

  async function claimUsername(user_name: string) {
    user.value = await api.claimUsername(user_name)
    return user.value
  }

  async function uploadAvatar(file: Blob | File) {
    user.value = await api.uploadAvatar(file)
    return user.value
  }

  async function deleteAvatar() {
    user.value = await api.deleteAvatar()
    return user.value
  }

  function hasPermission(permission: string) {
    return !!user.value?.permissions?.includes(permission)
  }

  /** A site permission; admin implies every one. */
  function hasSitePermission(permission: string) {
    return hasPermission('admin') || hasPermission(permission)
  }

  // The first admin page the user can open, or null for none. Company-mode roles can
  // hold parts of admin (settings, roles, audit logs) without being admins.
  const adminLanding = computed(() => {
    if (hasPermission('admin') || hasPermission('settings:manage')) return '/admin'
    if (hasPermission('roles:manage')) return '/admin/site-roles'
    if (hasPermission('audit:view')) return '/admin/audit'
    return null
  })

  // Site admins can always create; everyone else needs the permission on their site role.
  const canCreateProjects = computed(() => hasPermission('admin') || hasPermission('projects:create'))
  const canCreateOrganizations = computed(() => hasPermission('admin') || hasPermission('orgs:create'))
  const managesPeople = computed(() => !!user.value?.manages_people)

  return {
    user,
    loading,
    bootstrapped,
    isAuthenticated,
    needsUsernameClaim,
    hasPermission,
    hasSitePermission,
    adminLanding,
    canCreateProjects,
    canCreateOrganizations,
    managesPeople,
    refresh,
    login,
    verifyMFA,
    register,
    completeSetup,
    logout,
    updateProfile,
    claimUsername,
    uploadAvatar,
    deleteAvatar,
  }
}
