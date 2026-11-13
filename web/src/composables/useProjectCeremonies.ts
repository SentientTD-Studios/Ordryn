import { computed, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { api } from '@/api/client'
import type { CeremonyKind, CeremonySettings } from '@/api/types'
import { LIVE_RESYNC, subscribeLiveUpdates } from '@/composables/useLiveUpdates'

const cache = new Map<number, CeremonySettings>()
const inflight = new Map<number, Promise<CeremonySettings | null>>()
const cacheEpoch = ref(0)
let watchingLive = false

/** Settings changes publish project.updated; a reconnect may have missed some. */
function watchLiveInvalidation() {
  if (watchingLive) return
  watchingLive = true
  subscribeLiveUpdates((event) => {
    if (event.type === LIVE_RESYNC) clearProjectCeremoniesCache()
    else if (event.type === 'project.updated' && event.project_id && cache.has(event.project_id)) {
      clearProjectCeremoniesCache(event.project_id)
    }
  })
}

/** Drop cached settings (one project, or all) so open views refetch. */
export function clearProjectCeremoniesCache(projectId?: number) {
  if (projectId) {
    cache.delete(projectId)
    inflight.delete(projectId)
  } else {
    cache.clear()
    inflight.clear()
  }
  cacheEpoch.value++
}

/** Store fresh settings (e.g. from a PATCH response) and notify open views. */
export function primeProjectCeremonies(projectId: number, settings: CeremonySettings) {
  cache.set(projectId, settings)
  inflight.delete(projectId)
  cacheEpoch.value++
}

export async function loadProjectCeremonies(projectId: number): Promise<CeremonySettings | null> {
  const hit = cache.get(projectId)
  if (hit) return hit
  const pending = inflight.get(projectId)
  if (pending) return pending
  const epoch = cacheEpoch.value
  const req = api
    .getCeremonySettings(projectId)
    .then((s) => {
      if (epoch === cacheEpoch.value) cache.set(projectId, s)
      return s
    })
    .catch(() => null)
    .finally(() => inflight.delete(projectId))
  inflight.set(projectId, req)
  return req
}

/** Ceremony switches for a project, cached across components. Null until loaded or without access. */
export function useProjectCeremonies(projectId: MaybeRefOrGetter<number | null | undefined>) {
  watchLiveInvalidation()
  const settings = ref<CeremonySettings | null>(null)
  let seq = 0

  watch(
    () => [toValue(projectId), cacheEpoch.value] as const,
    async ([id]) => {
      const n = ++seq
      if (!id) {
        settings.value = null
        return
      }
      const next = await loadProjectCeremonies(id)
      if (n === seq) settings.value = next
    },
    { immediate: true },
  )

  const isActive = (kind: CeremonyKind) => computed(() => !!settings.value?.active[kind])
  const canFacilitate = (kind: CeremonyKind) => computed(() => !!settings.value?.can_facilitate[kind])

  return {
    settings,
    refinementActive: isActive('refinement'),
    canFacilitateRefinement: canFacilitate('refinement'),
    reload: () => {
      const id = toValue(projectId)
      if (id) clearProjectCeremoniesCache(id)
    },
  }
}
