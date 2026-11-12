import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { api } from '@/api/client'
import type { UserSearchHit } from '@/api/types'

export const USER_SEARCH_LIMIT = 10
export const USER_SEARCH_MIN_QUERY = 2
export const USER_SEARCH_DEBOUNCE_MS = 300
/** Server cap on team hits per query (include_teams). */
export const TEAM_SEARCH_LIMIT = 5

type CacheEntry = { names: string[]; complete: boolean }

const searchCache = new Map<string, CacheEntry>()
/** Team handles seen in results, keyed by `${projectId}:${handle}`. */
const teamHandles = new Set<string>()

function cacheKey(q: string, projectId?: number | null, includeTeams = false) {
  const scope = projectId && projectId > 0 ? `p${projectId}${includeTeams ? 't' : ''}` : 'g'
  return `${scope}:${q.trim().toLowerCase()}`
}

/** Whether name came back as a team handle for this project. */
export function isTeamHandle(name: string, projectId?: number | null) {
  return !!projectId && teamHandles.has(`${projectId}:${name.toLowerCase()}`)
}

/**
 * Usernames first, then team handles not shadowed by a username (people win
 * over teams with the same handle, as on the server).
 */
export function mergeSearchHits(hits: UserSearchHit[], projectId?: number | null): string[] {
  const users = hits.filter((h) => !h.is_team).map((h) => h.user_name)
  const taken = new Set(users.map((n) => n.toLowerCase()))
  const teams: string[] = []
  for (const h of hits) {
    if (!h.is_team || taken.has(h.user_name.toLowerCase())) continue
    taken.add(h.user_name.toLowerCase())
    teams.push(h.user_name)
    if (projectId) teamHandles.add(`${projectId}:${h.user_name.toLowerCase()}`)
  }
  return [...users, ...teams]
}

function filterPrefix(names: string[], key: string) {
  return names.filter((n) => n.toLowerCase().startsWith(key))
}

/** Cached names for q, or null if a network fetch is still needed. */
export function namesFromUserSearchCache(
  q: string,
  projectId?: number | null,
  includeTeams = false,
): string[] | null {
  const key = q.trim().toLowerCase()
  if (key.length < USER_SEARCH_MIN_QUERY) return []

  const exact = searchCache.get(cacheKey(q, projectId, includeTeams))
  if (exact) return filterPrefix(exact.names, key)

  for (let len = key.length - 1; len >= USER_SEARCH_MIN_QUERY; len--) {
    const entry = searchCache.get(cacheKey(key.slice(0, len), projectId, includeTeams))
    if (entry?.complete) return filterPrefix(entry.names, key)
  }
  return null
}

/**
 * Cache names for q. An entry is complete (reusable for longer queries) only if
 * neither the user list nor the team list hit its server cap.
 */
export function rememberUserSearch(
  q: string,
  names: string[],
  projectId?: number | null,
  includeTeams = false,
  teamCount = 0,
) {
  const userCount = names.length - teamCount
  searchCache.set(cacheKey(q, projectId, includeTeams), {
    names,
    complete: userCount < USER_SEARCH_LIMIT && teamCount < TEAM_SEARCH_LIMIT,
  })
}

export function useUserSearch(
  opts: {
    projectId?: MaybeRefOrGetter<number | null | undefined>
    excludeUsernames?: MaybeRefOrGetter<string[] | undefined>
    /** Also suggest @team handles (needs projectId). */
    includeTeams?: boolean
  } = {},
) {
  const includeTeams = !!opts.includeTeams
  const hits = ref<string[]>([])
  const loading = ref(false)
  const highlight = ref(0)

  function projectIdValue(): number | null {
    if (opts.projectId == null) return null
    const id = toValue(opts.projectId)
    return id && id > 0 ? id : null
  }

  const excludeSet = computed(() => {
    const names = opts.excludeUsernames ? (toValue(opts.excludeUsernames) ?? []) : []
    return new Set(names.map((n) => n.toLowerCase()).filter(Boolean))
  })

  const filtered = computed(() =>
    hits.value.filter((name) => !excludeSet.value.has(name.toLowerCase())),
  )

  let debounceTimer: number | undefined
  let searchGen = 0
  let abort: AbortController | undefined

  function abortInFlight() {
    abort?.abort()
    abort = undefined
  }

  function cancelPending() {
    window.clearTimeout(debounceTimer)
    abortInFlight()
    searchGen += 1
    loading.value = false
  }

  function applyLocal(names: string[]) {
    hits.value = names
    highlight.value = 0
    loading.value = false
  }

  function cachedNames(q: string): string[] | null {
    return namesFromUserSearchCache(q, projectIdValue(), includeTeams)
  }

  function isTeam(name: string) {
    return includeTeams && isTeamHandle(name, projectIdValue())
  }

  function scheduleSearch(raw: string) {
    window.clearTimeout(debounceTimer)
    const q = raw.trim()
    if (q.length < USER_SEARCH_MIN_QUERY) {
      cancelPending()
      hits.value = []
      return
    }

    const local = cachedNames(q)
    if (local) {
      cancelPending()
      applyLocal(local)
      return
    }

    loading.value = true
    debounceTimer = window.setTimeout(() => {
      void runSearch(q)
    }, USER_SEARCH_DEBOUNCE_MS)
  }

  async function runSearch(q: string) {
    const projectId = projectIdValue()
    const local = namesFromUserSearchCache(q, projectId, includeTeams)
    if (local) {
      applyLocal(local)
      return
    }

    const gen = ++searchGen
    abortInFlight()
    abort = new AbortController()
    const { signal } = abort
    loading.value = true
    try {
      const teams = includeTeams && !!projectId
      const results = await api.searchUsers(q, {
        signal,
        projectId: projectId ?? undefined,
        includeTeams: teams,
      })
      if (gen !== searchGen) return
      const names = mergeSearchHits(results, projectId)
      const teamCount = results.filter((h) => h.is_team).length
      rememberUserSearch(q, names, projectId, teams, teamCount)
      hits.value = names
      highlight.value = 0
    } catch {
      if (gen !== searchGen || signal.aborted) return
      hits.value = []
    } finally {
      if (gen === searchGen) loading.value = false
    }
  }

  return {
    hits,
    filtered,
    loading,
    highlight,
    scheduleSearch,
    cancelPending,
    applyLocal,
    cachedNames,
    isTeam,
  }
}
