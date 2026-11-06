import { ref } from 'vue'
import { api } from '@/api/client'
import { APIError, type Project } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { pinIdsFromProjects, pushRecentId, togglePinId } from '@/utils/projectNav'

/**
 * Shared sidebar project navigation state: pinned projects (server-backed), recently opened
 * projects (this browser), and the project switcher dialog. The desktop sidebar, mobile drawer
 * and switcher all read from here so a pin made in one place shows up everywhere at once.
 */

const RECENT_KEY = 'ordryn_recent_projects'

function loadRecent(): number[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter((n): n is number => Number.isInteger(n) && n > 0) : []
  } catch {
    return []
  }
}

function saveRecent(ids: number[]) {
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(ids))
  } catch {
    /* ignore */
  }
}

/** Pins changed in this session; null means "use what the server sent with the project list". */
const pinOverride = ref<number[] | null>(null)
const recentIds = ref<number[]>(loadRecent())
const switcherOpen = ref(false)
let pinSaveSeq = 0

/** Lets the task list switch projects in place instead of via a route change. */
let selectHandler: ((id: string) => void) | null = null

export function useProjectNav() {
  const toast = useToast()

  function pinIds(projects: Project[]): number[] {
    return pinOverride.value ?? pinIdsFromProjects(projects)
  }

  async function setPins(ids: number[], projects: Project[]) {
    const previous = pinIds(projects)
    pinOverride.value = ids
    const seq = ++pinSaveSeq
    try {
      await api.setProjectPins(ids)
    } catch (err) {
      if (seq === pinSaveSeq) pinOverride.value = previous
      toast.push(err instanceof APIError ? err.message : 'Could not update pinned projects', 'error')
    }
  }

  function togglePin(projectId: number, projects: Project[]) {
    return setPins(togglePinId(pinIds(projects), projectId), projects)
  }

  function isPinned(projectId: number, projects: Project[]) {
    return pinIds(projects).includes(projectId)
  }

  function touchRecent(projectId: number) {
    if (!Number.isInteger(projectId) || projectId <= 0) return
    if (recentIds.value[0] === projectId) return
    recentIds.value = pushRecentId(recentIds.value, projectId)
    saveRecent(recentIds.value)
  }

  function openSwitcher() {
    switcherOpen.value = true
  }

  function closeSwitcher() {
    switcherOpen.value = false
  }

  function registerSelectHandler(fn: (id: string) => void) {
    selectHandler = fn
    return () => {
      if (selectHandler === fn) selectHandler = null
    }
  }

  function inPlaceSelectHandler() {
    return selectHandler
  }

  return {
    recentIds,
    switcherOpen,
    pinIds,
    setPins,
    togglePin,
    isPinned,
    touchRecent,
    openSwitcher,
    closeSwitcher,
    registerSelectHandler,
    inPlaceSelectHandler,
  }
}

/** Clears per-user state (call on sign-out) so the next account starts fresh. */
export function resetProjectNav() {
  pinOverride.value = null
  switcherOpen.value = false
  recentIds.value = []
  saveRecent([])
}
