import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { User } from '@/api/types'

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
    user.value = await api.login(email, password)
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

  async function logout() {
    try {
      await api.logout()
    } finally {
      user.value = null
    }
  }

  async function updateProfile(
    payload: Partial<Pick<User, 'timezone' | 'items_per_page' | 'digest_enabled' | 'digest_hour' | 'allow_project_invites'>>,
  ) {
    user.value = await api.patchMe(payload)
    return user.value
  }

  async function claimUsername(user_name: string) {
    user.value = await api.claimUsername(user_name)
    return user.value
  }

  function hasPermission(permission: string) {
    return !!user.value?.permissions?.includes(permission)
  }

  return {
    user,
    loading,
    bootstrapped,
    isAuthenticated,
    needsUsernameClaim,
    hasPermission,
    refresh,
    login,
    register,
    logout,
    updateProfile,
    claimUsername,
  }
}
